package importer

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"howett.net/plist"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Safari is Safari's Bookmarks.plist.
const Safari Format = "safari-plist"

// safariRoots name Safari's top-level lists here; the History list is not bookmarks and is left out.
var safariRoots = map[string]string{"BookmarksBar": "Bookmarks Bar", "BookmarksMenu": "Bookmarks Menu", "com.apple.ReadingList": "Reading List"}

type safariNode struct {
	Type     string            `plist:"WebBookmarkType"`
	Title    string            `plist:"Title"`
	URL      string            `plist:"URLString"`
	URIDict  map[string]string `plist:"URIDictionary"`
	Reading  map[string]any    `plist:"ReadingList"`
	Children []safariNode      `plist:"Children"`
}

func isPlist(b []byte) bool {
	return bytes.HasPrefix(b, []byte("bplist")) || bytes.Contains(b[:min(len(b), 400)], []byte("<plist"))
}

// ReadSafari reads Safari's Bookmarks.plist (binary or XML).
func ReadSafari(b []byte) (store.Tree, error) {
	var doc safariNode
	if _, err := plist.Unmarshal(b, &doc); err != nil {
		return store.Tree{}, fmt.Errorf("Safari bookmarks file: %w", err)
	}
	var id ids
	var conv func(n safariNode, top bool) *store.Node
	conv = func(n safariNode, top bool) *store.Node {
		switch n.Type {
		case "WebBookmarkTypeList":
			title := n.Title
			if top {
				if name, ok := safariRoots[title]; ok {
					title = name
				}
			}
			f := &store.Node{ID: id.next(), Kind: store.KindFolder, Title: clean(title)}
			for _, c := range n.Children {
				if k := conv(c, false); k != nil {
					f.Children = append(f.Children, k)
				}
			}
			return f
		case "WebBookmarkTypeLeaf":
			if strings.TrimSpace(n.URL) == "" {
				return nil
			}
			bm := &store.Node{ID: id.next(), Kind: store.KindBookmark, Title: clean(n.URIDict["title"]), URL: strings.TrimSpace(n.URL)}
			if t, ok := n.Reading["DateAdded"].(time.Time); ok && t.Unix() > 0 {
				bm.Added = t.Unix()
			}
			if p, ok := n.Reading["PreviewText"].(string); ok {
				bm.Description = clean(p)
			}
			return bm
		}
		return nil // proxies (History) and anything else
	}
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	for _, c := range doc.Children {
		if c.Type != "WebBookmarkTypeList" || c.Title == "History" {
			continue
		}
		root.Children = append(root.Children, conv(c, true))
	}
	if doc.Type == "" && len(root.Children) == 0 {
		return store.Tree{}, errors.New("not a Safari bookmarks file")
	}
	t := store.Tree{Root: root}
	return t, t.Validate()
}
