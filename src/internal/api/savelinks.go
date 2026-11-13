package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/edit"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Limits on saving links, as the Python tool had them.
const (
	maxSaveLinks = 2000
	maxURLLen    = 4096
	maxTitleLen  = 500
)

// Link is an address to save, with its title.
type Link struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// saveLinks saves links (open tabs, usually) as a new folder of bookmarks, as one entry. The folder goes inside
// `parent`, a path of folder names separated by "/" ("Saved tabs" when left out; a missing folder on the way is made at
// the top), and its name is made unique there. Only http(s) links are taken.
func (s *Server) saveLinks(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	var in struct {
		Folder string  `json:"folder"`
		Parent *string `json:"parent"`
		Links  []Link  `json:"links"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Links) == 0 || len(in.Links) > maxSaveLinks {
		writeErr(w, 400, fmt.Sprintf("send 1 to %d links", maxSaveLinks))
		return
	}
	for i, l := range in.Links {
		u := strings.TrimSpace(l.URL)
		if (!strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://")) || len(u) > maxURLLen || len(l.Title) > maxTitleLen {
			writeErr(w, 400, fmt.Sprintf("link %d: only http(s) addresses up to %d characters, titles up to %d", i+1, maxURLLen, maxTitleLen))
			return
		}
		in.Links[i] = Link{u, strings.Join(strings.Fields(l.Title), " ")}
	}
	parentPath := "Saved tabs"
	if in.Parent != nil {
		parentPath = *in.Parent
	}
	var parts []string
	for _, p := range strings.Split(parentPath, "/") {
		if p = strings.TrimSpace(strings.ReplaceAll(p, "∕", "/")); p != "" {
			parts = append(parts, p)
		}
	}
	name := strings.Join(strings.Fields(in.Folder), " ")
	if name == "" {
		name = s.Now().Format("Saved 2006-01-02 15:04")
	}
	if len(parts) > 8 || len(name) > 200 || (len(parts) > 0 && strings.EqualFold(parts[0], "Trash")) {
		writeErr(w, 400, "a folder up to 8 deep, names up to 200 characters, not the Trash")
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	prev, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	next := cloneTree(prev)
	parent := next.Root
	for _, p := range parts {
		var found *store.Node
		for _, c := range parent.Children {
			if c.Kind == store.KindFolder && c.Title == p {
				found = c
				break
			}
		}
		if found == nil {
			found = &store.Node{ID: nodeID(), Kind: store.KindFolder, Title: p}
			parent.Children = append(parent.Children, found)
		}
		parent = found
	}
	taken := map[string]bool{}
	for _, c := range parent.Children {
		if c.Kind == store.KindFolder {
			taken[c.Title] = true
		}
	}
	unique := name
	for n := 2; taken[unique]; n++ {
		unique = fmt.Sprintf("%s (%d)", name, n)
	}
	added := s.Now().Unix()
	folder := &store.Node{ID: nodeID(), Kind: store.KindFolder, Title: unique}
	for _, l := range in.Links {
		t := l.Title
		if t == "" {
			t = l.URL
		}
		folder.Children = append(folder.Children, &store.Node{ID: nodeID(), Kind: store.KindBookmark, Title: t, URL: l.URL, Added: added})
	}
	parent.Children = append(parent.Children, folder)
	events, err := edit.Diff(prev, next)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	where := strings.Join(append(parts, unique), "/")
	summary := fmt.Sprintf("Saved %d links as bookmarks in %s", len(in.Links), where)
	s.commitWith(w, r, x, saw, given, given, store.Change{Op: "save-links", Summary: summary, Tree: next}, events, "changed",
		map[string]any{"folder": folder.ID, "path": where, "saved": len(in.Links)}, nil)
}

// cloneTree is a deep copy, so a change is worked out without touching what was read.
func cloneTree(t store.Tree) store.Tree {
	b, _ := json.Marshal(t)
	var out store.Tree
	_ = json.Unmarshal(b, &out)
	if out.Root == nil {
		out.Root = &store.Node{ID: "root", Kind: store.KindFolder}
	}
	return out
}
