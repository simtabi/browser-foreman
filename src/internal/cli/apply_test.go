package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/chromefile"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/writeback"
)

func TestChooseTakesTheFlagAsksOrSaysWhichFlag(t *testing.T) {
	opts := []choice{{"merge", "merge (recommended)"}, {"replace", "replace"}}
	if v, err := choose(nil, nil, false, "Mode", "mode", "REPLACE", opts); err != nil || v != "replace" {
		t.Fatalf("flag: %q %v", v, err)
	}
	if _, err := choose(nil, nil, false, "Mode", "mode", "sync", opts); err == nil || !strings.Contains(err.Error(), "merge, replace") {
		t.Fatalf("a wrong flag: %v", err)
	}
	if _, err := choose(nil, nil, false, "Mode", "mode", "", opts); err == nil || !strings.Contains(err.Error(), "--mode") {
		t.Fatalf("no terminal: %v", err)
	}
	var out bytes.Buffer
	if v, _ := choose(strings.NewReader("\n"), &out, true, "Mode", "mode", "", opts); v != "merge" || !strings.Contains(out.String(), "1. merge (recommended)") {
		t.Fatalf("enter takes the first: %q %s", v, out.String())
	}
	if v, _ := choose(strings.NewReader("2\n"), &out, true, "Mode", "mode", "", opts); v != "replace" {
		t.Fatalf("2: %q", v)
	}
	if _, err := choose(strings.NewReader("3\n"), &out, true, "Mode", "mode", "", opts); err == nil {
		t.Fatal("3 of 2 was taken")
	}
	if v, _ := choose(nil, nil, false, "Route", "route", "", opts[:1]); v != "merge" {
		t.Fatal("a single option is not taken")
	}
}

// env points the configuration and state at temporary folders and puts an export and a cleanup in the store.
func env(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("BROFO_SERVER_ADDR", "127.0.0.1:9") // nothing answers there: tests never reach a running server
	s, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	bm := func(id, title, url string) *store.Node {
		return &store.Node{ID: id, Kind: store.KindBookmark, Title: title, URL: url}
	}
	folder := func(id, title string, kids ...*store.Node) *store.Node {
		return &store.Node{ID: id, Kind: store.KindFolder, Title: title, Children: kids}
	}
	base := store.Tree{Root: folder("root", "", folder("b1", "Bookmarks Bar"), bm("b2", "A", "https://a.example/"), bm("b3", "B", "https://b.example/"))}
	ours := store.Tree{Root: folder("root", "", folder("o1", "Bookmarks Bar", folder("o2", "Reading", bm("o3", "A", "https://a.example/"))))}
	ctx, now := context.Background(), time.Now()
	if _, err := writeback.Save(ctx, s, "export", "Export", "import", "export", base, now); err != nil {
		t.Fatal(err)
	}
	if _, err := writeback.Save(ctx, s, "main", "Cleanup", "import", "cleanup", ours, now); err != nil {
		t.Fatal(err)
	}
}

// chromeProfile writes a profile's Bookmarks file holding A and B in Other bookmarks, plus C added later.
func chromeProfile(t *testing.T) string {
	t.Helper()
	url := func(id, name, u string) map[string]any {
		return map[string]any{"id": id, "name": name, "type": "url", "url": u}
	}
	folder := func(id, name string, kids ...any) map[string]any {
		return map[string]any{"id": id, "name": name, "type": "folder", "children": append([]any{}, kids...)}
	}
	roots := map[string]any{"bookmark_bar": folder("1", "Bookmarks bar"),
		"other":  folder("2", "Other bookmarks", url("5", "A", "https://a.example/"), url("6", "B", "https://b.example/"), url("7", "C", "https://c.example/")),
		"synced": folder("3", "Mobile bookmarks")}
	sum, _ := chromefile.Checksum(roots)
	b, _ := json.Marshal(map[string]any{"checksum": sum, "roots": roots, "version": 1})
	dir := filepath.Join(t.TempDir(), "Default")
	_ = os.Mkdir(dir, 0o700)
	path := filepath.Join(dir, "Bookmarks")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := Root()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestApplyIsADryRunUntilYesWithTheDryRunsFingerprint(t *testing.T) {
	env(t)
	path := chromeProfile(t)
	before, _ := os.ReadFile(path)
	args := []string{"apply", "--base", "export", "--file", path, "--mode", "merge", "--route", "file"}
	out, err := run(t, args...)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("the dry run wrote the file")
	}
	for _, want := range []string{"removed: 1", "B  (Other bookmarks)", "added: 1", "Reading/", "C  (Other bookmarks)", "--yes --fingerprint "} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry run does not say %q:\n%s", want, out)
		}
	}
	if out, err := run(t, append(args, "--yes", "--fingerprint", "stale")...); err == nil || !strings.Contains(err.Error(), "changed since the dry run") {
		t.Fatalf("a stale fingerprint: %v\n%s", err, out)
	}
	fp := regexp.MustCompile(`--fingerprint ([0-9a-f]{64})`).FindStringSubmatch(out)
	out, err = run(t, append(args, "--yes", "--fingerprint", fp[1])...)
	if err != nil || !strings.Contains(out, "Verified") {
		t.Fatalf("%v\n%s", err, out)
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), `"Reading"`) || !strings.Contains(string(written), "https://c.example/") || strings.Contains(string(written), "https://b.example/") {
		t.Fatalf("written: %s", written)
	}
	if out, _ := run(t, args...); !strings.Contains(out, "nothing: the profile already holds this") {
		t.Fatalf("a second dry run still plans changes:\n%s", out)
	}
}

func TestApplyAsksForWhatItIsNotGivenAndRefusesWithoutATerminal(t *testing.T) {
	env(t)
	path := chromeProfile(t)
	if _, err := run(t, "apply", "--base", "export", "--file", path, "--route", "file"); err == nil || !strings.Contains(err.Error(), "--mode") {
		t.Fatalf("no mode: %v", err)
	}
	if _, err := run(t, "apply", "--base", "export", "--file", path, "--mode", "merge"); err == nil || !strings.Contains(err.Error(), "--route") {
		t.Fatalf("no route: %v", err)
	}
	if _, err := run(t, "apply", "--base", "export", "--mode", "merge", "--route", "extension"); err == nil || !strings.Contains(err.Error(), "--read") {
		t.Fatalf("the extension route asks how to read the profile: %v", err)
	}
	if _, err := run(t, "apply", "--base", "export", "--mode", "merge", "--route", "extension", "--read", "extension"); err == nil || !strings.Contains(err.Error(), "google-chrome-live") {
		t.Fatalf("the extension route with no server and nothing read: %v", err)
	}
}

func TestTheExtensionRouteReadsTheFileOrTheLastExtensionRead(t *testing.T) {
	env(t)
	path := chromeProfile(t)
	out, err := run(t, "apply", "--base", "export", "--mode", "merge", "--route", "extension", "--read", "file", "--file", path, "--yes")
	if err != nil || !strings.Contains(out, "Saved as workspace google-chrome-target") {
		t.Fatalf("%v\n%s", err, out)
	}
	s, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := s.State(context.Background(), "google-chrome-target", 0)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for _, c := range n.Children {
			ids[c.Title] = c.ID
			walk(c)
		}
	}
	walk(target.Root)
	if ids["Bookmarks bar"] != "ext-1" || ids["A"] != "ext-5" || ids["C"] != "ext-7" || !strings.HasPrefix(ids["Reading"], "new-") {
		t.Fatalf("the target's ids are not the extension's: %v", ids)
	}
	// With no server running, the extension route uses what the extension read last, and says so.
	live := writeback.WithPrefix(must(writeback.ReadFile(path)), "ext-")
	if _, err := writeback.Save(context.Background(), s, "google-chrome-live", "Google Chrome", "import", "read", live, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	out, err = run(t, "apply", "--base", "export", "--mode", "merge", "--route", "extension", "--read", "extension")
	if err != nil || !strings.Contains(out, "read last (google-chrome-live") || !strings.Contains(out, "Against google-chrome-live") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestChoicesComeFromTheConfigurationUnlessAsk(t *testing.T) {
	env(t)
	path := chromeProfile(t)
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "browser-foreman")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20-writeback.json"), []byte(`{"writeback":{"mode":"merge","route":"file","read":"ask"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "apply", "--base", "export", "--file", path); err != nil || !strings.Contains(out, "What the write changes (merge)") {
		t.Fatalf("configured mode and route: %v\n%s", err, out)
	}
	if out, err := run(t, "apply", "--base", "export", "--file", path, "--mode", "replace"); err != nil || !strings.Contains(out, "(replace)") {
		t.Fatalf("a flag wins over the configuration: %v\n%s", err, out)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestAskExtensionToReadWaitsForItsAnswer(t *testing.T) {
	for _, final := range []string{"done", "failed"} {
		var posted map[string]any
		polls := 0
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/v1/extension/commands", func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&posted)
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, `{"error":"no token"}`, http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"command":{"id":"cmd_1","state":"queued"}}`))
		})
		mux.HandleFunc("GET /api/v1/extension/commands/cmd_1", func(w http.ResponseWriter, _ *http.Request) {
			polls++
			state := "sent"
			if polls == 2 {
				state = final
			}
			_, _ = w.Write([]byte(`{"command":{"id":"cmd_1","state":"` + state + `","error":"no access"}}`))
		})
		srv := httptest.NewServer(mux)
		err := askExtensionToRead(context.Background(), srv.URL, "tok", "Chrome", 10*time.Second)
		srv.Close()
		if posted["kind"] != "read-bookmarks" || posted["browser"] != "Chrome" || polls != 2 {
			t.Fatalf("%s: posted %v, polled %d times", final, posted, polls)
		}
		if (final == "done") != (err == nil) {
			t.Fatalf("%s: %v", final, err)
		}
	}
}

func TestTheExtensionsNameForABrowserIsUsedForItsWorkspaces(t *testing.T) {
	for _, in := range []string{"Chrome", "chrome", "Google Chrome"} {
		if p, e := namesOf(in); p != "Chrome" || e != "Google Chrome" {
			t.Errorf("%s: %q %q", in, p, e)
		}
	}
	if p, e := namesOf("Brave Browser"); p != "Brave" || e != "Brave Browser" {
		t.Errorf("Brave: %q %q", p, e)
	}
	if p, e := namesOf("Netscape"); p != "Netscape" || e != "Netscape" {
		t.Errorf("unknown: %q %q", p, e)
	}
}

func TestAWriteSendsItsPlanAndReturnsTheExtensionsCounts(t *testing.T) {
	var posted map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/extension/commands", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&posted)
		_, _ = w.Write([]byte(`{"command":{"id":"cmd_9","state":"queued"}}`))
	})
	mux.HandleFunc("GET /api/v1/extension/commands/cmd_9", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"command":{"id":"cmd_9","state":"done","result":{"created":3,"moved":2}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	res, err := runExtensionCommand(context.Background(), srv.URL, "tok", "Google Chrome",
		map[string]any{"kind": "write-bookmarks", "workspace": "google-chrome-target", "expect": strings.Repeat("a", 64)}, "write the plan", 5*time.Second)
	if err != nil || string(res) != `{"created":3,"moved":2}` {
		t.Fatalf("%s %v", res, err)
	}
	if posted["kind"] != "write-bookmarks" || posted["browser"] != "Google Chrome" || posted["workspace"] != "google-chrome-target" || len(posted["expect"].(string)) != 64 {
		t.Fatalf("posted %v", posted)
	}
}
