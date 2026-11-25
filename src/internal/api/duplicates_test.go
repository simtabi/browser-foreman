package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/dupes"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestFindDuplicatesAndTrashTheCopies(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	ctx := context.Background()
	_, _ = s.Data.CreateWorkspace(ctx, "main", "Main", time.Now())
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "bar", Kind: store.KindFolder, Title: "Bookmarks Bar", Children: []*store.Node{
			{ID: "old", Kind: store.KindBookmark, Title: "Blog", URL: "http://blog.example/post"},
			{ID: "dev", Kind: store.KindFolder, Title: "Dev", Children: []*store.Node{
				{ID: "new", Kind: store.KindBookmark, Title: "Blog", URL: "https://blog.example.com/post"}}}}}}}}
	if _, err := s.Data.Commit(ctx, "main", 0, store.Change{Op: "import", Summary: "x", Tree: tr}, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = s.Data.SaveLinks(ctx, []store.LinkResult{{URL: "http://blog.example/post", Status: "moved", Final: "https://blog.example.com/post", Checked: time.Now()}})
	read := func() (out struct {
		Groups []dupes.Group
		Copies int
	}) {
		w := send(h, "GET", "/api/v1/workspaces/main/duplicates", "")
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	got := read()
	if len(got.Groups) != 1 || got.Copies != 1 || got.Groups[0].Keep != "new" || got.Groups[0].Reasons[0] != dupes.SameFinal {
		t.Fatalf("%+v", got)
	}
	// Trashing the copy as a duplicate of the kept one: the group is gone, and the copy is listed with the merged ones.
	if w := send(h, "POST", "/api/v1/workspaces/main/changes", `{"ops":[{"op":"trash","ids":["old"],"reason":"duplicate of new"}]}`); w.Code >= 300 {
		t.Fatalf("trash %d %s", w.Code, w.Body)
	}
	if got := read(); len(got.Groups) != 0 || got.Copies != 0 {
		t.Fatalf("after trashing %+v", got)
	}
	if b := send(h, "GET", "/api/v1/workspaces/main/duplicates.csv", "").Body.String(); !strings.Contains(b, "http://blog.example/post") {
		t.Fatalf("csv %s", b)
	}
	if w := send(h, "GET", "/api/v1/workspaces/nope/duplicates", ""); w.Code != 404 {
		t.Fatalf("unknown workspace %d", w.Code)
	}
	if w := do(h, "GET", "/api/v1/workspaces/main/duplicates", "", "127.0.0.1:8765", "", ""); w.Code != 401 {
		t.Fatalf("without a token: %d", w.Code)
	}
}
