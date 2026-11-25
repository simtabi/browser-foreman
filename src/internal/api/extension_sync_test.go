package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func extTree(bar ...importer.ExtNode) []importer.ExtNode {
	return []importer.ExtNode{{ID: "0", Children: []importer.ExtNode{
		{ID: "1", Title: "Bookmarks bar", Children: bar},
		{ID: "2", Title: "Other bookmarks"},
	}}}
}

func bm(id, title string) importer.ExtNode {
	return importer.ExtNode{ID: id, Title: title, URL: "https://" + strings.ToLower(title) + ".example/"}
}

// A browser and the workspace read from it, changed apart, come back together: each side's own changes kept, what the
// browser removed in the workspace's Trash, and the browser's ids adopted after the merge is written into it.
func TestSyncMergesTheBrowserAndItsWorkspace(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	tok := paired(t, s, h, "Google Chrome")
	call := func(path string, body any) (int, map[string]any) {
		b, _ := json.Marshal(body)
		w := fromExtension(h, "POST", "/api/v1/extension/"+path, string(b), tok)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if code, out := call("bookmarks", map[string]any{"nodes": extTree(bm("10", "Go"), bm("11", "Rust"), bm("12", "Zig"))}); code != 201 {
		t.Fatalf("read %d %v", code, out)
	}
	const ws = "google-chrome-live"
	// The page renames Go; then the browser adds Odin and removes Zig.
	if w := send(h, "PATCH", "/api/v1/workspaces/"+ws+"/nodes/ext-10", `{"title":"Go (golang)"}`); w.Code >= 300 {
		t.Fatalf("rename %d %s", w.Code, w.Body)
	}
	browser := extTree(bm("10", "Go"), bm("11", "Rust"), bm("13", "Odin"))
	code, out := call("sync", map[string]any{"workspace": ws, "base": 0, "changed": time.Now().Add(time.Second).UnixMilli(), "nodes": browser})
	if code != 201 || out["removed"] != float64(1) {
		t.Fatalf("sync %d %v", code, out)
	}
	tr, e, _ := s.Data.State(context.Background(), ws, 0)
	var titles []string
	for _, n := range tr.Root.Children[0].Children {
		titles = append(titles, n.Title)
	}
	if strings.Join(titles, ",") != "Go (golang),Rust,Odin" || len(tr.Trash) != 1 || tr.Trash[0].Node.Title != "Zig" || tr.Trash[0].Reason != "removed in Google Chrome" || e.Op != "sync" {
		t.Fatalf("merged %v trash %d op %s", titles, len(tr.Trash), e.Op)
	}
	// The extension writes the merge into the browser and reads it back: the same, so adopting changes nothing.
	browser[0].Children[0].Children[0].Title = "Go (golang)"
	if code, out := call("sync/adopt", map[string]any{"workspace": ws, "seq": e.Seq, "nodes": browser}); code != 200 || out["seq"] != float64(e.Seq) {
		t.Fatalf("adopt %d %v", code, out)
	}
	// Nothing changed on either side: nothing is committed.
	if code, out := call("sync", map[string]any{"workspace": ws, "base": e.Seq, "nodes": browser}); code != 200 || out["seq"] != float64(e.Seq) {
		t.Fatalf("idle sync %d %v", code, out)
	}
	// An adopt that lost a race with another change is refused, so the extension syncs again.
	_ = send(h, "PATCH", "/api/v1/workspaces/"+ws+"/nodes/ext-11", `{"title":"Rust lang"}`)
	browser[0].Children[0].Children = append(browser[0].Children[0].Children, bm("14", "Nim"))
	if code, _ := call("sync/adopt", map[string]any{"workspace": ws, "seq": e.Seq, "nodes": browser}); code != 409 {
		t.Fatalf("stale adopt %d", code)
	}
	// Only a workspace read from this browser, and only from a base it has.
	_, _ = s.Data.CreateWorkspace(context.Background(), "work", "Work", time.Now())
	_, _ = s.Data.Commit(context.Background(), "work", 0, store.Change{Op: "import", Summary: "x", Tree: store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder}}}, time.Now())
	if code, _ := call("sync", map[string]any{"workspace": "work", "base": 1, "nodes": browser}); code != 403 {
		t.Fatalf("other workspace %d", code)
	}
	if code, _ := call("sync", map[string]any{"workspace": ws, "base": 99, "nodes": browser}); code != 400 {
		t.Fatalf("bad base %d", code)
	}
}
