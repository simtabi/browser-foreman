package mcpserver

import (
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// A "/" in a title is part of that one title, not a step down: a top-level folder named "Work/Notes" must not count
// as inside a "Work" scope, which would let a client limited to Work write there.
func TestASlashInATitleDoesNotReachIntoAScope(t *testing.T) {
	tr := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder, Children: []*store.Node{
		{ID: "work", Kind: store.KindFolder, Title: "Work", Children: []*store.Node{
			{ID: "in", Kind: store.KindBookmark, Title: "Inside", URL: "https://a.example/"}}},
		{ID: "fake", Kind: store.KindFolder, Title: "Work/Notes", Children: []*store.Node{
			{ID: "out", Kind: store.KindBookmark, Title: "Outside", URL: "https://b.example/"}}},
	}}}
	at := paths(tr)
	scope := []string{"Work"}
	if !inside(at["in"], scope) {
		t.Fatalf("a bookmark inside Work is outside its scope: %q", at["in"])
	}
	if inside(at["fake"], scope) || inside(at["out"], scope) {
		t.Fatalf("a folder titled Work/Notes counts as inside Work: %q %q", at["fake"], at["out"])
	}
	if at["fake"] != "Work%2FNotes" {
		t.Fatalf("path %q", at["fake"])
	}
}
