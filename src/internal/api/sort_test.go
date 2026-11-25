package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/sorting"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestSortPlansThenAppliesAsOneEntry(t *testing.T) {
	s := newServer(t, apiTok)
	s.Cfg.Storage.Path = t.TempDir()
	h := s.Handler()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "main", "Main", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "bar", Kind: store.KindFolder, Title: "Bar"},
		{ID: "a", Kind: store.KindBookmark, Title: "PHP manual", URL: "https://www.php.net/manual"},
		{ID: "b", Kind: store.KindBookmark, Title: "Somewhere", URL: "https://nowhere.example/"}}}}
	if _, err := s.Data.Commit(ctx, "main", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/sort", `{}`); w.Code != 409 || !strings.Contains(w.Body.String(), SortRulesFile) {
		t.Fatalf("no rules yet: %d %s", w.Code, w.Body)
	}
	rules := sorting.Rules{Bar: "Bar", Review: sorting.Review{Unsorted: "Bar/_Review/Unsorted"},
		Topics: []sorting.Topic{{Host: `php\.net$`, Topic: "php"}}, TopicFolders: map[string]string{"php": "Bar/PHP"}}
	b, _ := json.Marshal(rules)
	if err := os.WriteFile(filepath.Join(s.Cfg.Storage.Path, SortRulesFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Plan  sorting.Plan
		Entry *store.Entry
	}
	w := send(h, "POST", "/api/v1/workspaces/main/sort", `{}`)
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Plan.Moves) != 2 || out.Entry != nil {
		t.Fatalf("plan %d %s", w.Code, w.Body)
	}
	if x, _ := s.Data.Workspace(ctx, "main"); x.Head != 1 {
		t.Fatal("a dry run changed the workspace")
	}
	// Only the ticked destination: a dry run limited to it shows one move.
	w = send(h, "POST", "/api/v1/workspaces/main/sort", `{"folders":["Bar/PHP"]}`)
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Plan.Moves) != 1 || out.Plan.Moves[0].ID != "a" {
		t.Fatalf("limited %d %s", w.Code, w.Body)
	}
	w = send(h, "POST", "/api/v1/workspaces/main/sort", `{"apply":true}`)
	out.Entry = nil
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code >= 300 || out.Entry == nil || out.Entry.Op != "sort" || out.Entry.Summary != "Sorted 2 bookmarks into folders (2 new folders)" {
		t.Fatalf("apply %d %s", w.Code, w.Body)
	}
	got, _, _ := s.Data.State(ctx, "main", 0)
	var placed []string
	var walk func(n *store.Node, path string)
	walk = func(n *store.Node, path string) {
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				walk(c, path+"/"+c.Title)
			} else {
				placed = append(placed, path+":"+c.ID)
			}
		}
	}
	walk(got.Root, "")
	if strings.Join(placed, ",") != "/Bar/PHP:a,/Bar/_Review/Unsorted:b" {
		t.Fatalf("tree %v", placed)
	}
	// Sorted already: nothing to do, no entry.
	w = send(h, "POST", "/api/v1/workspaces/main/sort", `{"apply":true}`)
	out.Entry, out.Plan = nil, sorting.Plan{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Plan.Moves) != 0 || out.Entry != nil {
		t.Fatalf("again %d %s", w.Code, w.Body)
	}
	// Bad rules are named, not applied.
	_ = os.WriteFile(filepath.Join(s.Cfg.Storage.Path, SortRulesFile), []byte(`{"topics":[{"text":"(","topic":"x"}]}`), 0o600)
	if w := send(h, "POST", "/api/v1/workspaces/main/sort", `{}`); w.Code != 400 || !strings.Contains(w.Body.String(), "topic 1 text") {
		t.Fatalf("bad rules %d %s", w.Code, w.Body)
	}
	if w := do(h, "POST", "/api/v1/workspaces/main/sort", `{}`, "127.0.0.1:8765", "", ""); w.Code != 401 {
		t.Fatalf("without a token: %d", w.Code)
	}
}

func TestSortSummaryCounts(t *testing.T) {
	one := sorting.Plan{Moves: []sorting.Move{{ID: "a"}}, New: []string{"X"}}
	if got := SortSummary(one); got != "Sorted 1 bookmark into folders (1 new folder)" {
		t.Fatal(got)
	}
	if got := SortSummary(sorting.Plan{Moves: make([]sorting.Move, 3)}); got != "Sorted 3 bookmarks into folders (0 new folders)" {
		t.Fatal(got)
	}
}
