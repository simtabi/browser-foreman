package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	if _, err := run(t, "apply", "--base", "export", "--mode", "replace", "--route", "extension"); err == nil || !strings.Contains(err.Error(), "chrome-live") {
		t.Fatalf("the extension route with nothing read: %v", err)
	}
}
