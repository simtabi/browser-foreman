package linkcheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestFrameRule(t *testing.T) {
	cases := []struct {
		xfo, csp, want string
	}{
		{"", "", ""},
		{"DENY", "", FrameRefused},
		{"sameorigin", "", FrameRefused},
		{"ALLOW-FROM https://x", "", ""}, // obsolete; browsers ignore it
		{"", "default-src 'self'; frame-ancestors 'none'", FrameRefused},
		{"", "frame-ancestors 'self' https://partner.example", FrameRefused},
		{"", "frame-ancestors *", ""},
		{"", "default-src 'self'", ""},
	}
	for _, c := range cases {
		h := http.Header{}
		if c.xfo != "" {
			h.Set("X-Frame-Options", c.xfo)
		}
		if c.csp != "" {
			h.Set("Content-Security-Policy", c.csp)
		}
		if got := frameRule(h); got != c.want {
			t.Errorf("XFO %q CSP %q: %q, want %q", c.xfo, c.csp, got, c.want)
		}
	}
}

func TestPageMeta(t *testing.T) {
	page := `<html><head><title>Plain</title>
<meta property="og:title" content="Go 1.27 is out">
<meta name="description" content="Release notes">
<meta property="og:description" content="What is new in Go 1.27">
<meta property="og:image" content="/img/card.png">
<meta property="og:site_name" content="The Go Blog">
<link rel="canonical" href="https://go.dev/blog/go1.27">
</head><body><meta property="og:image" content="https://late.example/x.png"></body></html>`
	m := pageMeta([]byte(page), "https://go.dev/blog/go1.27?utm_source=x")
	want := store.PageMeta{Title: "Go 1.27 is out", Description: "Release notes", Image: "https://go.dev/img/card.png",
		Canonical: "https://go.dev/blog/go1.27", Site: "The Go Blog"}
	if m == nil || *m != want {
		t.Fatalf("%+v", m)
	}
	if m := pageMeta([]byte(`<html><head><meta property="og:image" content="javascript:alert(1)"></head></html>`), "https://a.example/"); m != nil {
		t.Fatalf("a script address was kept: %+v", m)
	}
	if m := pageMeta([]byte(`<html><head><title>x</title></head></html>`), "https://a.example/"); m != nil {
		t.Fatalf("a page with no metadata: %+v", m)
	}
	// Only the head speaks for the page: a meta tag in the body (a user's post, say) is not the page's own.
	if m := pageMeta([]byte(`<html><head><title>x</title></head><body><meta property="og:image" content="https://a.example/u.png"></body></html>`), "https://a.example/"); m != nil {
		t.Fatalf("a body meta tag was read: %+v", m)
	}
}

func TestCheckRecordsFramingAndMeta(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/locked", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
		fmt.Fprint(w, `<html><head><title>Locked</title><meta property="og:title" content="Card"></head><body>Real words here.</body></html>`)
	})
	s := httptest.NewServer(mux)
	defer s.Close()
	r := checker().Check(context.Background(), s.URL+"/locked")
	if r.Status != StatusOK || r.Frame != FrameRefused || r.Meta == nil || r.Meta.Title != "Card" {
		t.Fatalf("%+v %+v", r, r.Meta)
	}
}

func TestADeadLinkIsConfirmedOnlyADayLater(t *testing.T) {
	t0 := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	dead := func(at time.Time) Result { return Result{URL: "u", Status: StatusDead, Kind: "page", Checked: at} }
	first := Settle(store.LinkResult{}, false, dead(t0))
	if first.Confirmed || !first.DeadSince.Equal(t0) {
		t.Fatalf("first sighting %+v", first)
	}
	soon := Settle(first, true, dead(t0.Add(3*time.Hour)))
	if soon.Confirmed || !soon.DeadSince.Equal(t0) {
		t.Fatalf("three hours later %+v", soon)
	}
	later := Settle(soon, true, dead(t0.Add(ConfirmAfter)))
	if !later.Confirmed || !later.DeadSince.Equal(t0) {
		t.Fatalf("a day later %+v", later)
	}
	later.Archive = "https://web.archive.org/web/2020/u"
	still := Settle(later, true, dead(t0.Add(ConfirmAfter+time.Hour)))
	if !still.Confirmed || still.Archive == "" {
		t.Fatalf("stays confirmed and keeps its archive copy %+v", still)
	}
	back := Settle(still, true, Result{URL: "u", Status: StatusOK, Checked: t0.Add(48 * time.Hour)})
	if back.Confirmed || !back.DeadSince.IsZero() || back.Archive != "" {
		t.Fatalf("working again %+v", back)
	}
	// A result stored before DeadSince existed counts from when it was checked.
	old := store.LinkResult{URL: "u", Status: StatusDead, Checked: t0.Add(-72 * time.Hour)}
	if r := Settle(old, true, dead(t0)); !r.Confirmed || !r.DeadSince.Equal(old.Checked) {
		t.Fatalf("legacy %+v", r)
	}
}

func TestSettleKeepsWhatADeadPageCannotSay(t *testing.T) {
	meta := &store.PageMeta{Title: "Before"}
	before := store.LinkResult{URL: "u", Status: StatusOK, Frame: FrameRefused, Meta: meta}
	r := Settle(before, true, Result{URL: "u", Status: StatusDead, Checked: time.Now()})
	if r.Meta != meta || r.Frame != FrameRefused {
		t.Fatalf("%+v", r)
	}
	// A working page that no longer refuses framing says so.
	if r := Settle(before, true, Result{URL: "u", Status: StatusOK, Checked: time.Now()}); r.Frame != "" {
		t.Fatalf("framing kept from before %+v", r)
	}
}

func TestWayback(t *testing.T) {
	answer := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/wayback/available", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != "https://gone.example/page?a=1" {
			http.Error(w, "wrong address", 400)
			return
		}
		fmt.Fprint(w, answer)
	})
	s := httptest.NewServer(mux)
	defer s.Close()
	c, end := checker(), s.URL+"/wayback/available"
	cases := map[string]string{
		`{"archived_snapshots":{"closest":{"available":true,"status":"200","url":"http://web.archive.org/web/2019/https://gone.example/page"}}}`: "https://web.archive.org/web/2019/https://gone.example/page",
		`{"archived_snapshots":{}}`: "",
		`{"archived_snapshots":{"closest":{"available":true,"status":"404","url":"http://web.archive.org/web/2019/x"}}}`: "",
		`{"archived_snapshots":{"closest":{"available":true,"status":"200","url":"https://evil.example/x"}}}`:            "",
		`not json`: "",
	}
	for body, want := range cases {
		answer = body
		if got := c.Wayback(context.Background(), end, "https://gone.example/page?a=1"); got != want {
			t.Errorf("%s: %q, want %q", body, got, want)
		}
	}
}

func TestFromRenderIsStrict(t *testing.T) {
	words := strings.Repeat("A real article with plenty of words in it. ", 10)
	page := func(title, body string) []byte {
		return []byte("<html><head><title>" + title + "</title></head><body><p>" + body + "</p></body></html>")
	}
	cases := []struct {
		name, title, body string
		ok                bool
		status            string
	}{
		{"an ordinary page", "Docs", words, true, StatusOK},
		{"a not-found page", "Page not found", "Sorry", true, StatusDead},
		{"a parked domain", "example.com is for sale", "Buy this domain", true, StatusDead},
		{"a bot check", "Just a moment...", "Checking your browser", false, ""},
		{"a bare error page", "403 Forbidden", words, false, ""},
		{"almost empty", "Docs", "Hello", false, ""},
	}
	for _, c := range cases {
		r, ok := FromRender(page(c.title, c.body), "https://site.example/p")
		if ok != c.ok || ok && r.Status != c.status {
			t.Errorf("%s: %v %+v", c.name, ok, r)
		}
	}
}
