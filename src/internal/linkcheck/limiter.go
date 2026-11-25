package linkcheck

import (
	"context"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Rule is how hard one site may be pressed: requests at once, and the gap between their starts.
type Rule struct {
	Concurrent int
	Gap        time.Duration
}

// Rules for sites known to rate-limit; everything else gets Default.
var (
	Rules = map[string]Rule{
		"github.com": {1, 1200 * time.Millisecond}, "githubusercontent.com": {1, time.Second}, "stackoverflow.com": {1, 2 * time.Second},
		"stackexchange.com": {1, 2 * time.Second}, "serverfault.com": {1, 2 * time.Second}, "superuser.com": {1, 2 * time.Second},
		"askubuntu.com": {1, 2 * time.Second}, "medium.com": {1, 1500 * time.Millisecond}, "youtube.com": {1, time.Second},
		"google.com": {1, 1500 * time.Millisecond}, "reddit.com": {1, 2 * time.Second}, "linkedin.com": {1, 3 * time.Second},
		"twitter.com": {1, 2 * time.Second}, "x.com": {1, 2 * time.Second}, "facebook.com": {1, 2 * time.Second},
	}
	Default = Rule{2, 300 * time.Millisecond}
)

const (
	maxBackoff      = 15 * time.Minute
	strikes         = 3 // network failures in a row that pause a site
	breakerCooldown = 5 * time.Minute
)

// hosted are domains under which every name is someone's own site (the private section of the public suffix list):
// alice.github.io and bob.github.io are two sites, and one rate-limiting must not pause the other.
var hosted = []string{"github.io", "gitlab.io", "netlify.app", "vercel.app", "pages.dev", "web.app", "firebaseapp.com",
	"herokuapp.com", "blogspot.com", "wordpress.com", "substack.com", "notion.site", "glitch.me", "surge.sh",
	"onrender.com", "fly.dev", "azurewebsites.net", "readthedocs.io"}

// Site is the registrable part of a host, so github.com and gist.github.com share one budget. On a hosting platform
// (hosted) it is the owner's name with the platform: alice.github.io.
func Site(rawURL string) string {
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return host // an address is its own site
	}
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	p := strings.Split(host, ".")
	for _, h := range hosted {
		if strings.HasSuffix(host, "."+h) {
			return strings.Join(p[len(p)-strings.Count(h, ".")-2:], ".")
		}
	}
	n := 2
	if len(p) > 2 && len(p[len(p)-1]) == 2 {
		switch p[len(p)-2] {
		case "co", "com", "org", "ac", "gov", "net":
			n = 3
		}
	}
	if len(p) < n {
		n = len(p)
	}
	return strings.Join(p[len(p)-n:], ".")
}

// Outcome is what a request did, as the limiter needs to know it.
type Outcome int

const (
	OK Outcome = iota
	RateLimited
	NetworkError
	OtherError
)

type siteState struct {
	sem     chan struct{}
	next    time.Time // earliest start of the next request
	cool    time.Time // paused until
	backoff time.Duration
	strikes int
}

// Limiter keeps every check polite per site: a cap on requests at once, spaced starts, a pause (doubling, up to 15
// minutes, honouring Retry-After) when a site says to slow down, and a 5-minute pause after 3 network failures in a
// row. A caller that would wait longer than MaxWait gets "try later" instead, so a paused site is never called dead.
type Limiter struct {
	mu      sync.Mutex
	sites   map[string]*siteState
	Rules   map[string]Rule
	Default Rule
	MaxWait time.Duration
	// MinBackoff is the first pause after a site asks to slow down; it doubles from there.
	MinBackoff time.Duration
	Now        func() time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{sites: map[string]*siteState{}, Rules: Rules, Default: Default, MaxWait: 45 * time.Second, MinBackoff: 30 * time.Second, Now: time.Now}
}

func (l *Limiter) rule(site string) Rule {
	if r, ok := l.Rules[site]; ok {
		return r
	}
	return l.Default
}

func (l *Limiter) state(site string) *siteState {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.sites[site]
	if s == nil {
		if len(l.sites) >= maxSites {
			l.forgetQuiet()
		}
		s = &siteState{sem: make(chan struct{}, max(1, l.rule(site).Concurrent))}
		l.sites[site] = s
	}
	return s
}

// maxSites is how many sites the limiter remembers before it forgets the quiet ones.
const maxSites = 20000

// forgetQuiet drops the sites that are in exactly the state a new one starts in: nothing in flight, no pause to wait
// out, no strikes, no backoff. Forgetting them changes nothing; a site that asked to slow down is always kept. l.mu
// is held.
func (l *Limiter) forgetQuiet() {
	now := l.Now()
	for k, s := range l.sites {
		if len(s.sem) == 0 && !now.Before(s.next) && !now.Before(s.cool) && s.strikes == 0 && s.backoff == 0 {
			delete(l.sites, k)
		}
	}
}

// Acquire waits for a slot for rawURL's site. It returns false when the site stays paused past MaxWait or ctx ends;
// otherwise the caller must call Release with the outcome.
func (l *Limiter) Acquire(ctx context.Context, rawURL string) (string, bool) {
	return l.AcquireFor(ctx, rawURL, l.MaxWait)
}

// AcquireFor is Acquire, waiting up to maxWait.
func (l *Limiter) AcquireFor(ctx context.Context, rawURL string, maxWait time.Duration) (string, bool) {
	site := Site(rawURL)
	s := l.state(site)
	deadline := l.Now().Add(maxWait)
	wait := time.NewTimer(maxWait)
	defer wait.Stop()
	select {
	case s.sem <- struct{}{}:
	case <-wait.C:
		return site, false
	case <-ctx.Done():
		return site, false
	}
	for {
		l.mu.Lock()
		now := l.Now()
		start := s.next
		if s.cool.After(start) {
			start = s.cool
		}
		if !start.After(now) {
			s.next = now.Add(l.rule(site).Gap)
			l.mu.Unlock()
			return site, true
		}
		l.mu.Unlock()
		if start.After(deadline) {
			<-s.sem
			return site, false
		}
		select {
		case <-time.After(min(start.Sub(now), 5*time.Second)):
		case <-ctx.Done():
			<-s.sem
			return site, false
		}
	}
}

// Release frees the slot and learns from what happened. retryAfter is the site's Retry-After, when it sent one.
func (l *Limiter) Release(site string, o Outcome, retryAfter time.Duration) {
	s := l.state(site)
	l.mu.Lock()
	now := l.Now()
	switch o {
	case RateLimited:
		b := min(max(s.backoff*2, l.MinBackoff), maxBackoff)
		if retryAfter > 0 {
			b = min(max(b, retryAfter), maxBackoff)
		}
		s.backoff, s.cool = b, now.Add(b)
	case NetworkError:
		s.strikes++
		if s.strikes >= strikes {
			s.cool, s.strikes = now.Add(breakerCooldown), 0
		}
	case OK:
		s.strikes, s.backoff = 0, 0
	default:
		s.strikes = 0
	}
	l.mu.Unlock()
	<-s.sem
}

// Interleave orders URLs round-robin by site, biggest sites first, so consecutive checks rarely hit the same site.
func Interleave(urls []string) []string {
	order := []string{}
	buckets := map[string][]string{}
	for _, u := range urls {
		s := Site(u)
		if _, ok := buckets[s]; !ok {
			order = append(order, s)
		}
		buckets[s] = append(buckets[s], u)
	}
	sort.SliceStable(order, func(i, j int) bool { return len(buckets[order[i]]) > len(buckets[order[j]]) })
	out := make([]string, 0, len(urls))
	for len(out) < len(urls) {
		for _, s := range order {
			if q := buckets[s]; len(q) > 0 {
				out = append(out, q[0])
				buckets[s] = q[1:]
			}
		}
	}
	return out
}
