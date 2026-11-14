package linkcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func site(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	page := func(title, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<html><head><title>%s</title><script>var x='page not found'</script></head><body>%s</body></html>", title, body)
		}
	}
	mux.HandleFunc("/ok", page("Welcome", "A real article about Go with plenty of words in it."))
	mux.HandleFunc("/gone", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "nope", 404) })
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/new-place", 301) })
	mux.HandleFunc("/new-place", page("New place", "Moved here."))
	mux.HandleFunc("/private", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://localhost.test/admin", 302) })
	mux.HandleFunc("/members", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login?next=x", 302) })
	mux.HandleFunc("/login", page("Sign in", "Sign in"))
	mux.HandleFunc("/soft", page("Page not found", "Sorry"))
	mux.HandleFunc("/parked", page("example.com is for sale", "Buy this domain"))
	mux.HandleFunc("/bot", page("Just a moment...", "Checking your browser"))
	mux.HandleFunc("/slow-down", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "", 429)
	})
	mux.HandleFunc("/deep/page", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", 302) })
	mux.HandleFunc("/{$}", page("Home", "Home page"))
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func checker() *Checker {
	c := New(5 * time.Second)
	c.AllowPrivate = true // the test site is on loopback
	c.Limiter.Default = Rule{4, 0}
	return c
}

func TestClassification(t *testing.T) {
	s := site(t)
	c := checker()
	cases := map[string]string{
		"/ok": "ok", "/gone": "dead page", "/old": "moved", "/members": "needs-login", "/soft": "dead page",
		"/parked": "dead host", "/bot": "unverified", "/deep/page": "unverified",
		"/account/unsubscribe?id=1": "skipped", "/private": "local",
	}
	for path, want := range cases {
		r := c.Check(context.Background(), s.URL+path)
		got := strings.TrimSpace(r.Status + " " + r.Kind)
		if got != want {
			t.Errorf("%s: %s (%s), want %s", path, got, r.Note, want)
		}
	}
	// A 429 pauses the whole site, so it runs last, on its own.
	if r := c.Check(context.Background(), s.URL+"/slow-down"); r.Status != StatusUnverified || r.Code != 429 {
		t.Fatalf("%+v", r)
	}
	if r := c.Check(context.Background(), s.URL+"/ok"); r.Status != StatusUnverified || !strings.Contains(r.Note, "try later") {
		t.Fatalf("a site that asked for a 60 s pause was visited again at once: %+v", r)
	}
	c = checker()
	r := c.Check(context.Background(), s.URL+"/old")
	if r.Final != s.URL+"/new-place" {
		t.Fatalf("final %q", r.Final)
	}
	if r := c.Check(context.Background(), s.URL+"/ok"); r.Final != "" || r.Code != 200 || r.Checked.IsZero() {
		t.Fatalf("%+v", r)
	}
}

func TestNeverReachesPrivateAddresses(t *testing.T) {
	var hits atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer s.Close()
	c := New(2 * time.Second) // the real configuration: AllowPrivate false
	for _, u := range []string{s.URL + "/", "http://localhost/", "http://192.168.1.1/", "http://169.254.169.254/latest/meta-data/", "http://[::1]/", "http://printer.local/"} {
		if r := c.Check(context.Background(), u); r.Status != StatusLocal {
			t.Errorf("%s: %s %s", u, r.Status, r.Note)
		}
	}
	// A public-looking name that resolves to loopback is refused at connect time, not by its name.
	if r := c.Check(context.Background(), strings.Replace(s.URL, "127.0.0.1", "localtest.me", 1)+"/"); r.Status == StatusOK {
		t.Errorf("a name resolving to loopback was visited: %+v", r)
	}
	if hits.Load() != 0 {
		t.Fatalf("the private server was reached %d times", hits.Load())
	}
}

func TestDeadDomains(t *testing.T) {
	c := New(5 * time.Second)
	if r := c.Check(context.Background(), "https://this-domain-does-not-exist-brofo.invalid/"); r.Status != StatusDead || r.Kind != "host" {
		t.Fatalf("%+v", r)
	}
}

func TestVerdicts(t *testing.T) {
	for _, c := range []struct{ title, text, want string }{
		{"404 Not Found", "", "soft-404"}, {"Page not found – Example", "", "soft-404"},
		{"How to handle a 404 page in Go", "A long article explaining things.", "ok"},
		{"", "This domain is for sale. Make an offer.", "parked"},
		{"Attention Required! | Cloudflare", "", "challenge"},
		{"Welcome to nginx!", "", "suspended"},
		{"Blog", "We couldn't find that page, sorry.", "suspect-404"},
	} {
		if got, _ := Verdict(c.title, c.text, "https://x/"); got != c.want {
			t.Errorf("%q / %q: %s, want %s", c.title, c.text, got, c.want)
		}
	}
}

func TestLimiterBacksOffAndInterleaves(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter()
	l.Now = func() time.Time { return now }
	l.MaxWait = 10 * time.Second
	s, ok := l.Acquire(context.Background(), "https://api.github.com/x")
	if !ok || s != "github.com" {
		t.Fatal(s, ok)
	}
	l.Release(s, RateLimited, 0)
	if _, ok := l.Acquire(context.Background(), "https://gist.github.com/y"); ok {
		t.Fatal("a rate-limited site (30 s pause) was let through within a 10 s wait")
	}
	now = now.Add(31 * time.Second)
	if s, ok := l.Acquire(context.Background(), "https://github.com/z"); !ok {
		t.Fatal("still paused after the backoff")
	} else {
		l.Release(s, OK, 0)
	}
	got := Interleave([]string{"https://a.com/1", "https://a.com/2", "https://a.com/3", "https://b.com/1", "https://c.co.uk/1"})
	if strings.Join(got, " ") != "https://a.com/1 https://b.com/1 https://c.co.uk/1 https://a.com/2 https://a.com/3" {
		t.Fatal(got)
	}
	if Site("https://news.bbc.co.uk/x") != "bbc.co.uk" || Site("https://www.Example.com") != "example.com" {
		t.Fatal(Site("https://news.bbc.co.uk/x"))
	}
}

func TestJobPauseResumeCancel(t *testing.T) {
	gate := make(chan struct{})
	var inside atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inside.Add(1)
		select {
		case <-gate:
		case <-r.Context().Done(): // the job was cancelled: let the server close
		}
	}))
	defer s.Close()
	c := checker()
	var urls []string
	for i := range 20 {
		urls = append(urls, fmt.Sprintf("%s/p%d", s.URL, i))
	}
	var got atomic.Int32
	j := c.Start("j1", append(urls, urls[0]), 2, func(r Result) Result { got.Add(1); return r })
	if j.Total != 20 {
		t.Fatalf("duplicates kept: %d", j.Total)
	}
	for inside.Load() < 2 { // both workers are inside a check
		time.Sleep(5 * time.Millisecond)
	}
	if !j.Pause() || j.View().State != Paused {
		t.Fatal("pause")
	}
	gate <- struct{}{}
	gate <- struct{}{} // the two checks under way finish
	time.Sleep(100 * time.Millisecond)
	if n, in := j.View().Checked, inside.Load(); n != 2 || in != 2 {
		t.Fatalf("checks kept starting while paused: %d finished, %d reached the site", n, in)
	}
	j.Resume()
	for range 3 {
		gate <- struct{}{}
	}
	for j.View().Checked < 5 {
		time.Sleep(5 * time.Millisecond)
	}
	j.Cancel()
	j.Wait()
	v := j.View()
	if v.State != Cancelled || !v.Done || v.Checked != 5 || int(got.Load()) != v.Checked || len(j.Results()) != v.Checked {
		t.Fatalf("%+v got %d", v, got.Load())
	}
}
