package tabs

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

type platform struct {
	mu  sync.Mutex
	ran [][]string
}

func (p *platform) Installed() []browsers.Browser {
	var out []browsers.Browser
	for _, b := range browsers.Known {
		if b.Name == "Google Chrome" || b.Name == "Safari" || b.Name == "Firefox" || b.Name == "Orion" {
			b.Path = "/Applications/" + b.Name + ".app"
			out = append(out, b)
		}
	}
	return out
}
func (p *platform) DefaultBundle() string                      { return "" }
func (p *platform) Icon(browsers.Browser, int) ([]byte, error) { return nil, nil }
func (p *platform) Launch(_ context.Context, argv []string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ran = append(p.ran, argv)
	return nil
}

// reader answers from a fixed table; a browser not in it is not running.
type reader struct {
	mu    sync.Mutex
	state map[string][]Window
	asked []string
}

func (r *reader) Read(_ context.Context, b browsers.Browser) Browser {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, b.Name)
	w, ok := r.state[b.Name]
	return Browser{Browser: b.Name, Running: ok, Windows: w}
}

type closer struct{ got []CloseItem }

func (c *closer) Close(_ context.Context, _ string, items []CloseItem) (int, []string, error) {
	c.got = append(c.got, items...)
	return len(items), nil, nil
}

func service(t *testing.T) (*Service, *reader, *platform, *closer) {
	t.Helper()
	data, err := store.Open("json", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &reader{state: map[string][]Window{
		"Google Chrome": {
			{Index: 1, ID: "11", Mode: "normal", Tabs: []Tab{{URL: "https://go.dev/", Title: "Go", ID: "1"}, {URL: "chrome://settings", Title: "Settings", ID: "2"}}},
			{Index: 2, ID: "12", Mode: "incognito", Tabs: []Tab{{URL: "https://private.example/", Title: "Secret", ID: "3"}}},
		},
	}}
	p := &platform{}
	c := &closer{}
	clock := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	return &Service{Data: data, Browsers: &browsers.Service{Platform: p}, Reader: r, Closer: c,
		Now: func() time.Time { clock = clock.Add(time.Second); return clock }}, r, p, c
}

func TestPrivateWindowsAreRedacted(t *testing.T) {
	s, r, _, _ := service(t)
	live, _, err := s.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(r.asked)
	// Firefox is read from its session file; Orion can be read neither way and must not be asked.
	if strings.Join(r.asked, ",") != "Firefox,Google Chrome,Safari" {
		t.Fatalf("asked %v", r.asked)
	}
	b, _ := json.Marshal(live)
	if strings.Contains(string(b), "private.example") || strings.Contains(string(b), "Secret") {
		t.Fatal("a private page left the reader")
	}
	if live.Summary.IncognitoTabs != 1 || live.Summary.Tabs != 2 || live.Summary.Running != 1 {
		t.Fatalf("%+v", live.Summary)
	}
	saved, _ := s.Take(context.Background(), "manual", false)
	doc, _ := s.Data.TabSnapshot(context.Background(), saved.ID)
	if strings.Contains(string(doc.Windows), "private.example") {
		t.Fatal("a private page was stored")
	}
	p := store.Defaults()
	p.IncludeIncognito = true
	_ = store.SavePrefs(context.Background(), s.Data, p)
	if live, _, _ := s.Live(context.Background()); !strings.Contains(mustJSON(live), "private.example") {
		t.Fatal("the owner chose to keep private windows")
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestSnapshotsOnlyWhenSomethingChanged(t *testing.T) {
	s, r, _, _ := service(t)
	ctx := context.Background()
	a, _ := s.Take(ctx, "auto", false)
	if !a.Stored {
		t.Fatal(a.Why)
	}
	r.state["Google Chrome"][0].Tabs[0].Title = "Go (new title)"
	if b, _ := s.Take(ctx, "auto", false); b.Stored {
		t.Fatal("a title change made a new snapshot")
	}
	r.state["Google Chrome"][0].Tabs = append(r.state["Google Chrome"][0].Tabs, Tab{URL: "https://pkg.go.dev/"})
	if c, _ := s.Take(ctx, "auto", false); !c.Stored {
		t.Fatal("a new tab did not make a snapshot")
	}
	r.state = map[string][]Window{}
	if d, _ := s.Take(ctx, "auto", false); d.Stored || !strings.Contains(d.Why, "no browser windows") {
		t.Fatalf("%+v", d)
	}
}

func TestRestoreAndClose(t *testing.T) {
	s, _, p, c := service(t)
	ctx := context.Background()
	saved, _ := s.Take(ctx, "manual", false)
	res, err := s.Restore(ctx, saved.ID, nil)
	if err != nil || len(res) != 1 || !res[0].OK {
		t.Fatalf("%+v %v", res, err)
	}
	// One window: the private one is redacted and cannot come back; chrome:// pages are not reopened.
	if got := strings.Join(p.ran[0], " "); got != "open -na /Applications/Google Chrome.app --args --new-window https://go.dev/" {
		t.Fatal(got)
	}
	out, err := s.CloseTabs(ctx, []CloseItem{{Browser: "Google Chrome", WindowID: "11", TabID: "1", URL: "https://go.dev/"}})
	if err != nil || out.Closed != 1 || out.UndoSnapshot == "" || len(c.got) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := s.Data.TabSnapshot(ctx, out.UndoSnapshot); err != nil {
		t.Fatal("no snapshot before closing:", err)
	}
	if _, err := s.CloseTabs(ctx, nil); err == nil {
		t.Fatal("an empty close was accepted")
	}
}

func TestTimelineAndFirstSeen(t *testing.T) {
	s, r, _, _ := service(t)
	ctx := context.Background()
	first, _ := s.Take(ctx, "auto", false)
	r.state["Google Chrome"][0].Tabs = []Tab{{URL: "https://go.dev/", Title: "Go"}, {URL: "https://pkg.go.dev/", Title: "Pkg"}}
	_, _ = s.Take(ctx, "auto", false)
	r.state["Google Chrome"][0].Tabs = []Tab{{URL: "https://pkg.go.dev/", Title: "Pkg"}}
	_, _ = s.Take(ctx, "auto", false)
	steps, err := s.Timeline(ctx, 30, nil, 40)
	if err != nil || len(steps) != 3 {
		t.Fatalf("%d %v", len(steps), err)
	}
	if steps[0].ID != first.ID || steps[1].OpenedN != 1 || steps[1].Opened[0].URL != "https://pkg.go.dev/" || steps[1].ClosedN != 1 || steps[2].ClosedN != 1 || steps[2].Closed[0].URL != "https://go.dev/" {
		t.Fatalf("%+v", steps)
	}
	// days 0 is "All": every snapshot kept, not none (the Timeline's All showed an empty chart).
	if all, err := s.Timeline(ctx, 0, nil, 40); err != nil || len(all) != 3 {
		t.Fatalf("days 0 listed %d snapshots, want 3: %v", len(all), err)
	}
	if none, _ := s.Timeline(ctx, 30, []string{"Safari"}, 40); len(none) != 0 {
		t.Fatalf("Safari had no windows: %+v", none)
	}
	seen, _ := s.FirstSeen(ctx, []string{"https://go.dev/", "https://pkg.go.dev/", "https://private.example/", "https://never.example/"})
	if !seen["https://go.dev/"].Equal(first.State.At) || seen["https://pkg.go.dev/"].Equal(first.State.At) || len(seen) != 2 {
		t.Fatalf("%v", seen)
	}
	rows, _ := s.Rows(ctx, 10, nil)
	if len(rows) != 3 || rows[0].Summary.Tabs != 1 || rows[0].Browsers[0] != "Google Chrome" {
		t.Fatalf("%+v", rows)
	}
}

func TestRestorePlanSkipsRedactedWindows(t *testing.T) {
	st := State{Browsers: []Browser{{Browser: "Google Chrome", Windows: []Window{
		{Mode: "normal", Tabs: []Tab{{URL: "https://go.dev/"}}},
		{Mode: "incognito", Redacted: true, Tabs: []Tab{{URL: "https://private.example/"}}}, // pages must never come back
	}}}}
	plan := RestorePlan(st, nil)
	if len(plan) != 1 || plan[0].URLs[0] != "https://go.dev/" {
		t.Fatalf("%+v", plan)
	}
}

// The read script's real answers: Chrome gives ids as strings, Safari as numbers and no mode.
func TestParseReadTakesRealAnswers(t *testing.T) {
	chrome := ParseRead("Google Chrome", true, []byte(`{"running":true,"windows":[{"index":1,"id":"1873301","mode":"normal","active":3,
		"tabs":[{"url":"https://go.dev/","title":"Go","id":"1873302"},{"url":"https://b.example/","title":"B","id":"tab-7F3A"}]},{"index":2,"id":"1873400","mode":"incognito","active":1,"tabs":[]}]}`))
	if chrome.Error != "" || len(chrome.Windows) != 2 || chrome.Windows[0].ID != "1873301" || chrome.Windows[0].Tabs[0].ID != "1873302" || chrome.Windows[0].Tabs[1].ID != "tab-7F3A" || chrome.Windows[1].Mode != "incognito" {
		t.Fatalf("%+v", chrome)
	}
	safari := ParseRead("Safari", false, []byte(`{"running":true,"windows":[{"index":1,"id":4512,"active":2,"tabs":[{"url":"https://a.example/","title":"A","id":null}]}]}`))
	if safari.Error != "" || safari.Windows[0].ID != "4512" || safari.Windows[0].Mode != "normal" || safari.Windows[0].Tabs[0].ID != "" {
		t.Fatalf("%+v", safari)
	}
	if bad := ParseRead("Google Chrome", true, []byte("execution error")); bad.Error == "" {
		t.Fatal("a broken answer was accepted")
	}
}

func TestExtensionReportsStandInForReading(t *testing.T) {
	s, r, _, _ := service(t)
	ctx := context.Background()
	s.Push(Browser{Browser: "Google Chrome", Windows: []Window{{Index: 1, Mode: "normal", Tabs: []Tab{{URL: "https://from.extension/", Group: "g1"}},
		Groups: []Group{{ID: "g1", Title: "Work"}}}}})
	s.Push(Browser{Browser: "Firefox", Windows: []Window{{Index: 1, Mode: "incognito", Tabs: []Tab{{URL: "https://private.firefox/"}}}}})
	r.asked = nil
	live, _, _ := s.Live(ctx)
	for _, n := range r.asked {
		if n == "Google Chrome" {
			t.Fatal("Chrome was read through automation although its extension had just reported")
		}
	}
	got := mustJSON(live)
	if !strings.Contains(got, "from.extension") || !strings.Contains(got, `"via":"extension"`) || strings.Contains(got, "private.firefox") {
		t.Fatalf("%s", got)
	}
	if live.Summary.IncognitoTabs != 1 {
		t.Fatalf("Firefox's private window, sent by its extension, is counted and redacted: %+v", live.Summary)
	}
}
