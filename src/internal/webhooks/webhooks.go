// Package webhooks delivers events to URLs that subscribed to them ("callbacks"). Each delivery is a JSON POST, signed
// with the subscription's secret:
//
//	X-Brofo-Event: bookmark.created
//	X-Brofo-Delivery: 42-3            (event id, attempt)
//	X-Brofo-Signature: t=1731400000,v1=<hex HMAC-SHA256 of "t.body">
//
// A receiver recomputes the HMAC over "<t>.<raw body>" with the shared secret, compares in constant time and refuses
// a t more than five minutes old (see Verify). Failed deliveries are retried with exponential backoff.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/simtabi/browser-foreman/src/internal/atomicfile"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/events"
)

// Subscription sends the events whose type matches one of Events ("*" for all, "bookmark.*" for a family) to URL.
type Subscription struct {
	ID      string    `json:"id"`
	URL     string    `json:"url"`
	Events  []string  `json:"events"`
	Secret  string    `json:"secret"`
	Created time.Time `json:"created"`
	Active  bool      `json:"active"`
}

// Matches reports whether the subscription wants an event of this type.
func (s Subscription) Matches(typ string) bool {
	for _, p := range s.Events {
		if p == "*" || p == typ || (strings.HasSuffix(p, ".*") && strings.HasPrefix(typ, strings.TrimSuffix(p, "*"))) {
			return true
		}
	}
	return false
}

// CheckURL refuses targets that are not http(s) or that point at link-local or cloud metadata addresses, where a
// webhook could be used to reach something it should not. Loopback is allowed: local callbacks are a normal use.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("webhook url must be http(s)://host/…")
	}
	if u.User != nil {
		return fmt.Errorf("webhook url must not carry a username or password; use the signing secret")
	}
	host := u.Hostname()
	if strings.EqualFold(host, "metadata.google.internal") {
		return fmt.Errorf("webhook url %q is a cloud metadata address", host)
	}
	if ip := net.ParseIP(host); ip != nil && refusedIP(ip) {
		return fmt.Errorf("webhook url %q is a link-local, multicast or unspecified address", host)
	}
	return nil
}

// refusedIP is what a webhook may never reach: link-local addresses (the cloud metadata service lives at
// 169.254.169.254), multicast and unspecified ones. Loopback and the local network stay allowed.
func refusedIP(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

var errRefusedAddress = errors.New("refused: webhooks may not reach link-local, multicast or unspecified addresses")

// NewClient is the client webhooks are delivered with. CheckURL only sees the address as typed; this applies the same
// rule to every address actually connected to, so a name that resolves to the metadata service is refused too. It
// never follows a redirect: the receiver's own answer is the answer.
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip != nil && refusedIP(ip) {
			return errRefusedAddress
		}
		return nil
	}}
	return &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// maxWait is the longest wait between two tries of one delivery.
const maxWait = 5 * time.Minute

// nextWait doubles a wait, up to maxWait.
func nextWait(w time.Duration) time.Duration { return min(2*w, maxWait) }

// Sign returns the X-Brofo-Signature value for body at time t.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks a signature header against body: the HMAC must match and t must be within tolerance of now.
func Verify(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return errors.New("signature header must be t=<unix>,v1=<hex>")
	}
	if d := now.Sub(time.Unix(sec, 0)); d > tolerance || d < -tolerance {
		return errors.New("signature is too old or from the future")
	}
	want := Sign(secret, time.Unix(sec, 0), body)
	if !hmac.Equal([]byte(want), []byte("t="+ts+",v1="+sig)) {
		return errors.New("signature does not match")
	}
	return nil
}

// Store keeps subscriptions in one JSON file (0600; it holds the signing secrets).
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(dir string) *Store { return &Store{path: filepath.Join(dir, "webhooks.json")} }

func (s *Store) List() ([]Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Get returns one subscription by id, and whether there is one.
func (s *Store) Get(id string) (Subscription, bool, error) {
	all, err := s.List()
	for _, x := range all {
		if x.ID == id {
			return x, true, err
		}
	}
	return Subscription{}, false, err
}

func (s *Store) read() ([]Subscription, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Subscription
	return out, json.Unmarshal(b, &out)
}

func (s *Store) write(all []Subscription) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(all, "", "  ")
	return atomicfile.Write(s.path, append(b, '\n'), 0o600)
}

// Add stores a new subscription after checking its URL and event patterns.
func (s *Store) Add(sub Subscription) (Subscription, error) {
	if err := CheckURL(sub.URL); err != nil {
		return sub, err
	}
	if len(sub.Events) == 0 {
		return sub, errors.New("a webhook needs at least one event type (or \"*\")")
	}
	if len(sub.Secret) < 16 {
		return sub, errors.New("a webhook secret must be at least 16 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return sub, err
	}
	sub.Active = true
	all = append(all, sub)
	return sub, s.write(all)
}

// Remove deletes a subscription by id; it reports whether one was there.
func (s *Store) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return false, err
	}
	kept := all[:0]
	for _, x := range all {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	if len(kept) == len(all) {
		return false, nil
	}
	return true, s.write(kept)
}

// Delivery is the record of one event sent to one subscription.
type Delivery struct {
	Subscription string    `json:"subscription"`
	Event        uint64    `json:"event"`
	Type         string    `json:"type"`
	Attempts     int       `json:"attempts"`
	Status       int       `json:"status"` // the last HTTP status, 0 when no answer
	OK           bool      `json:"ok"`
	Error        string    `json:"error,omitempty"`
	At           time.Time `json:"at"`
}

// Dispatcher delivers each event to every matching subscription, retrying with exponential backoff.
type Dispatcher struct {
	Store    *Store
	Client   *http.Client
	Attempts int
	Backoff  time.Duration // first wait; doubles each try
	Now      func() time.Time
	Log      func(Delivery)
}

// Deliver sends one event to every matching active subscription and returns what happened to each.
func (d *Dispatcher) Deliver(ctx context.Context, e events.Event) []Delivery {
	subs, err := d.Store.List()
	if err != nil {
		return []Delivery{{Error: err.Error(), Event: e.ID, At: d.Now()}}
	}
	body, _ := json.Marshal(e)
	var out []Delivery
	for _, s := range subs {
		if !s.Active || !s.Matches(e.Type) {
			continue
		}
		rec := d.send(ctx, s, e, body, true)
		if d.Log != nil {
			d.Log(rec)
		}
		out = append(out, rec)
	}
	return out
}

// queueSize is how many events wait for one webhook while its receiver is busy or down. It is well above the event
// bus's own buffer (256, in api.Run), so a healthy receiver never loses an event the bus kept, even through a burst
// such as a link check reporting thousands of changed links.
const queueSize = 1024

type job struct {
	sub  Subscription
	e    events.Event
	body []byte
}

// Run delivers the events from ch until ctx ends or ch closes. Each webhook gets its own queue and worker, so a slow
// or dead receiver delays only its own deliveries, retries included. When its queue is full, the event is recorded
// (through Log) as not sent rather than dropped without a trace.
// A webhook's queue is made the first time it gets an event; one removed later keeps an idle worker until Run ends.
func (d *Dispatcher) Run(ctx context.Context, ch <-chan events.Event) {
	queues := map[string]chan job{}
	var wg sync.WaitGroup
	defer func() {
		for _, q := range queues {
			close(q)
		}
		wg.Wait()
	}()
	for {
		var e events.Event
		var ok bool
		select {
		case <-ctx.Done():
			return
		case e, ok = <-ch:
			if !ok {
				return
			}
		}
		subs, err := d.Store.List()
		if err != nil {
			d.log(Delivery{Event: e.ID, Type: e.Type, Error: err.Error(), At: d.Now()})
			continue
		}
		body, _ := json.Marshal(e)
		for _, s := range subs {
			if !s.Active || !s.Matches(e.Type) {
				continue
			}
			q := queues[s.ID]
			if q == nil {
				q = make(chan job, queueSize)
				queues[s.ID] = q
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := range q {
						d.log(d.send(ctx, j.sub, j.e, j.body, true))
					}
				}()
			}
			select {
			case q <- job{s, e, body}:
			default:
				d.log(Delivery{Subscription: s.ID, Event: e.ID, Type: e.Type, At: d.Now(),
					Error: "not sent: this webhook's queue is full (its receiver is slow or down)"})
			}
		}
	}
}

func (d *Dispatcher) log(rec Delivery) {
	if d.Log != nil {
		d.Log(rec)
	}
}

// SendOne delivers one event to one subscription, whatever its event patterns (the API's test button).
func (d *Dispatcher) SendOne(ctx context.Context, s Subscription, e events.Event) Delivery {
	body, _ := json.Marshal(e)
	rec := d.send(ctx, s, e, body, false) // the test button: s was just read
	if d.Log != nil {
		d.Log(rec)
	}
	return rec
}

// send delivers one event to s. queued: it waited in s's queue, so s is read again before each attempt.
func (d *Dispatcher) send(ctx context.Context, s Subscription, e events.Event, body []byte, queued bool) Delivery {
	rec := Delivery{Subscription: s.ID, Event: e.ID, Type: e.Type}
	wait := d.Backoff
	for attempt := 1; attempt <= d.Attempts; attempt++ {
		// The webhook as it is now: an event queued before it was removed or turned off is not sent, and one queued
		// before its address or secret changed goes to the new address, signed with the new secret.
		if queued && d.Store != nil {
			cur, ok, err := d.Store.Get(s.ID)
			if err != nil || !ok || !cur.Active {
				rec.Error = "not sent: the webhook was removed or turned off"
				if err != nil {
					rec.Error = "not sent: " + err.Error()
				}
				break
			}
			s = cur
		}
		rec.Attempts = attempt
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
		if err != nil {
			rec.Error = err.Error()
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "browser-foreman-webhooks")
		req.Header.Set("X-Brofo-Event", e.Type)
		req.Header.Set("X-Brofo-Delivery", fmt.Sprintf("%d-%d", e.ID, attempt))
		req.Header.Set("X-Brofo-Signature", Sign(s.Secret, d.Now(), body))
		resp, err := d.Client.Do(req)
		if err == nil {
			rec.Status = resp.StatusCode
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				rec.OK, rec.Error = true, ""
				break
			}
			rec.Error = "answered " + resp.Status
			if resp.StatusCode >= 300 && resp.StatusCode < 400 {
				rec.Error += ": redirects are not followed"
				break
			}
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				break // the receiver refused it; sending it again will not help
			}
		} else {
			rec.Status, rec.Error = 0, err.Error()
		}
		if attempt < d.Attempts {
			select {
			case <-ctx.Done():
				rec.Error = ctx.Err().Error()
				rec.At = d.Now()
				return rec
			case <-time.After(wait):
			}
			wait = nextWait(wait)
		}
	}
	rec.At = d.Now()
	return rec
}
