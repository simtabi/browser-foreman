// Package linkcheck checks bookmarked links the way a careful visitor would: once each, without credentials, a few at a
// time per site, never on this computer or the local network, and never following links that act when visited
// (sign-out, unsubscribe, password reset…). Check classifies one URL; Run checks many.
package linkcheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Statuses, as the web interface names them.
const (
	StatusOK         = "ok"          // the page answers at the same address
	StatusMoved      = "moved"       // it redirects; Final is the new address
	StatusLogin      = "needs-login" // it sends visitors to a sign-in page
	StatusUnverified = "unverified"  // could not tell (timeouts, bot checks, rate limits, TLS trouble)
	StatusDead       = "dead"        // gone: Kind says whether the page or the whole site
	StatusLocal      = "local"       // on this computer, the local network or a staging name: not visited
	StatusSkipped    = "skipped"     // looks like an action link: not visited
)

// Result is what one check found; the store keeps the latest per URL.
type Result = store.LinkResult

var (
	loginRe  = regexp.MustCompile(`(?i)(login|log-in|signin|sign-in|sign_in|/auth|/sso|oauth|/account/|accounts\.|session/new|idp\.|cas/)`)
	actionRe = regexp.MustCompile(`(?i)(unsubscribe|logout|logoff|signout|confirm|activate|verify|reset|delete|remove|token=|key=|apikey|/invite)`)
	localRe  = regexp.MustCompile(`(?i)(\.test$|\.local$|\.localhost$|\.internal$|\.lan$|\.home\.arpa$|^localhost$|^stg-|^staging\.)`)
)

// UserAgent is what checks send: a current desktop browser, since many sites refuse anything else.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36"

var errPrivate = errors.New("refused: a local or private address")

// privateIP covers every address a check must never reach: loopback, private ranges, link-local (cloud metadata),
// multicast, unspecified, and the shared and benchmarking ranges.
func privateIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
		if ip[0] == 100 && ip[1]&0xc0 == 64 || ip[0] == 198 && (ip[1] == 18 || ip[1] == 19) || ip[0] == 0 || ip[0] >= 240 {
			return true
		}
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// Checker visits links. The zero value is not usable; call New.
type Checker struct {
	Client  *http.Client
	Limiter *Limiter
	Now     func() time.Time
	// AllowPrivate lets checks reach loopback and private addresses. Tests set it to talk to httptest servers; the
	// server never does.
	AllowPrivate bool
	// Patience is how long a run waits, at its end, for a site that was pausing (rate limited, or failing on the
	// network) before calling its links "try later": just past the longest pause, so a run finishes them.
	Patience time.Duration
}

// New returns a Checker whose connections are refused, at dial time, to any private address: a bookmark, or a
// redirect from one, can never make it reach the router, a printer or a service on this computer, whatever the
// name resolves to by then.
func New(timeout time.Duration) *Checker {
	c := &Checker{Limiter: NewLimiter(), Now: time.Now, Patience: maxBackoff + time.Minute}
	dialer := &net.Dialer{Timeout: timeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip != nil && privateIP(ip) && !c.AllowPrivate {
			return errPrivate
		}
		return nil
	}}
	transport := &http.Transport{
		DialContext: dialer.DialContext, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	c.Client = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("more than 10 redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return fmt.Errorf("refused: a redirect to %s:", req.URL.Scheme)
		}
		if actionRe.MatchString(req.URL.String()) {
			return errors.New("refused: a redirect to an action link")
		}
		if localRe.MatchString(req.URL.Hostname()) {
			return errPrivate
		}
		return nil
	}}
	return c
}

// Check classifies one URL. It never returns an error: what went wrong is the result.
func (c *Checker) Check(ctx context.Context, raw string) Result {
	r, _ := c.checkWaiting(ctx, raw, c.Limiter.MaxWait)
	return r
}

// How one visit ended, beyond its result.
type attempt int

const (
	settled  attempt = iota
	paused           // the site stayed paused: the result is only "try later", and a patient retry may do better
	retrying         // a timeout or a dropped connection: worth one more try
)

// checkWaiting is Check, waiting up to wait for a paused site; it reports whether the site stayed paused.
func (c *Checker) checkWaiting(ctx context.Context, raw string, wait time.Duration) (Result, bool) {
	r, a := c.check(ctx, raw, wait)
	if a == retrying && ctx.Err() == nil {
		r, a = c.check(ctx, raw, wait) // one more try, as a person would
	}
	r.URL, r.Checked = raw, c.Now().UTC()
	if r.Final == raw {
		r.Final = ""
	}
	return r, a == paused
}

func (c *Checker) check(ctx context.Context, raw string, wait time.Duration) (Result, attempt) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Result{Status: StatusSkipped, Note: "not a web link"}, settled
	}
	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(strings.Trim(host, "[]")); localRe.MatchString(host) || ip != nil && privateIP(ip) && !c.AllowPrivate {
		return Result{Status: StatusLocal, Note: "local or staging host"}, settled
	}
	if actionRe.MatchString(raw) {
		return Result{Status: StatusSkipped, Note: "looks like an action link; not visited"}, settled
	}
	site, ok := c.Limiter.AcquireFor(ctx, raw, wait)
	if !ok {
		return Result{Status: StatusUnverified, Note: site + " is pausing after rate limiting; try later"}, paused
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		c.Limiter.Release(site, OtherError, 0)
		return Result{Status: StatusSkipped, Note: "not a valid URL"}, settled
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := c.Client.Do(req)
	if err != nil {
		res, outcome := classifyErr(err)
		c.Limiter.Release(site, outcome, 0)
		if outcome == NetworkError && res.Status == StatusUnverified {
			return res, retrying
		}
		return res, settled
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode == 429 || resp.StatusCode == 503 {
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		c.Limiter.Release(site, RateLimited, time.Duration(secs)*time.Second)
		return Result{Status: StatusUnverified, Code: resp.StatusCode, Note: fmt.Sprintf("HTTP %d: the site asked us to slow down; try later", resp.StatusCode)}, paused
	}
	c.Limiter.Release(site, OK, 0)
	final := resp.Request.URL.String()
	switch {
	case resp.StatusCode == 404 || resp.StatusCode == 410:
		return Result{Status: StatusDead, Kind: "page", Code: resp.StatusCode, Final: final, Note: "HTTP " + strconv.Itoa(resp.StatusCode)}, settled
	case resp.StatusCode == 401:
		return Result{Status: StatusLogin, Code: 401, Final: final, Note: "HTTP 401"}, settled
	case resp.StatusCode >= 400:
		return Result{Status: StatusUnverified, Code: resp.StatusCode, Final: final, Note: "HTTP " + strconv.Itoa(resp.StatusCode)}, settled
	}
	res := classify(raw, resp.StatusCode, final)
	if res.Status == StatusOK || res.Status == StatusMoved {
		res.Frame = frameRule(resp.Header)
		if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "html") {
			res.Meta = pageMeta(body, final)
			title, text := pageText(body)
			switch v, why := Verdict(title, text, final); v {
			case "soft-404":
				return Result{Status: StatusDead, Kind: "page", Code: resp.StatusCode, Final: final, Note: why}, settled
			case "parked", "suspended":
				return Result{Status: StatusDead, Kind: "host", Code: resp.StatusCode, Final: final, Note: why}, settled
			case "challenge":
				return Result{Status: StatusUnverified, Code: resp.StatusCode, Final: final, Note: why}, settled
			case "suspect-404":
				res.Note = why
			}
		}
	}
	return res, settled
}

func sameAddress(a, b *url.URL) bool {
	strip := func(h string) string { return strings.TrimPrefix(strings.ToLower(h), "www.") }
	return strip(a.Host) == strip(b.Host) && strings.TrimRight(a.Path, "/") == strings.TrimRight(b.Path, "/")
}

func classify(raw string, code int, final string) Result {
	a, _ := url.Parse(raw)
	b, err := url.Parse(final)
	if err != nil {
		return Result{Status: StatusOK, Code: code}
	}
	if loginRe.MatchString(b.Path+b.Host) && !loginRe.MatchString(raw) {
		return Result{Status: StatusLogin, Code: code, Final: final, Note: "redirects to a sign-in page"}
	}
	if sameAddress(a, b) {
		return Result{Status: StatusOK, Code: code, Final: final}
	}
	if strings.Trim(a.Path, "/") != "" && strings.Trim(b.Path, "/") == "" {
		return Result{Status: StatusUnverified, Code: code, Final: final, Note: "redirects to the home page; the page may be gone"}
	}
	return Result{Status: StatusMoved, Code: code, Final: final, Note: "redirects"}
}

func classifyErr(err error) (Result, Outcome) {
	var dns *net.DNSError
	var opErr *net.OpError
	switch {
	case errors.Is(err, errPrivate):
		return Result{Status: StatusLocal, Note: "it leads to a local or private address; not visited"}, OtherError
	case strings.Contains(err.Error(), "refused: a redirect"):
		return Result{Status: StatusSkipped, Note: "it redirects to an action link or a non-web address; not followed"}, OtherError
	case errors.As(err, &dns) && dns.IsNotFound:
		return Result{Status: StatusDead, Kind: "host", Note: "the domain does not exist"}, OtherError
	case errors.Is(err, syscall.ECONNREFUSED):
		return Result{Status: StatusDead, Kind: "host", Note: "the server refuses connections"}, NetworkError
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &opErr) && opErr.Timeout() || strings.Contains(err.Error(), "Timeout"):
		return Result{Status: StatusUnverified, Note: "timed out"}, NetworkError
	case strings.Contains(strings.ToLower(err.Error()), "certificate") || strings.Contains(err.Error(), "tls:"):
		return Result{Status: StatusUnverified, Note: "TLS problem: " + short(err.Error(), 120)}, OtherError
	}
	return Result{Status: StatusUnverified, Note: short(err.Error(), 160)}, NetworkError
}

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// pageText pulls the title and the start of the visible text out of an HTML page's first bytes.
func pageText(b []byte) (title, text string) {
	z := html.NewTokenizer(bytes.NewReader(b))
	var t strings.Builder
	inTitle, skip := false, 0
	for t.Len() < 2000 {
		switch z.Next() {
		case html.ErrorToken:
			return strings.TrimSpace(title), strings.TrimSpace(space.ReplaceAllString(t.String(), " "))
		case html.StartTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				inTitle = true
			case "script", "style", "noscript", "template":
				skip++
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "title":
				inTitle = false
			case "script", "style", "noscript", "template":
				if skip > 0 {
					skip--
				}
			}
		case html.TextToken:
			if inTitle {
				title += string(z.Text())
			} else if skip == 0 {
				t.Write(z.Text())
				t.WriteByte(' ')
			}
		}
	}
	return strings.TrimSpace(title), strings.TrimSpace(space.ReplaceAllString(t.String(), " "))
}

// IsPrivate reports whether an address is loopback, private, link-local or otherwise not on the public internet: the
// addresses a check (or a snapshot) never visits.
func IsPrivate(ip net.IP) bool { return privateIP(ip) }
