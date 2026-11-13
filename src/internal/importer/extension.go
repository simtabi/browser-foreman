package importer

import (
	"errors"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// ExtNode is a bookmark as a browser's extension API gives it (chrome.bookmarks / browser.bookmarks): a folder has
// Children, a bookmark a URL; DateAdded is milliseconds since 1970; Type is "separator" for Firefox's separators.
type ExtNode struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url,omitempty"`
	DateAdded int64     `json:"dateAdded,omitempty"`
	Type      string    `json:"type,omitempty"`
	Children  []ExtNode `json:"children,omitempty"`
}

// ExtPrefix marks a node that came from the browser: "ext-" + the browser's own id. Writing a workspace back into
// the browser matches nodes by it.
const ExtPrefix = "ext-"

// FromExtension turns the tree an extension read into a workspace tree. The browser's own root is dropped; its top
// folders (Bookmarks bar, Other bookmarks, Firefox's menu…) become the top folders. Separators are left out.
func FromExtension(roots []ExtNode) (store.Tree, error) {
	if len(roots) == 0 {
		return store.Tree{}, errors.New("no bookmarks were sent")
	}
	var conv func(n ExtNode, depth int) *store.Node
	conv = func(n ExtNode, depth int) *store.Node {
		if n.Type == "separator" || depth > 64 {
			return nil
		}
		node := &store.Node{ID: ExtPrefix + n.ID, Title: clean(n.Title)}
		if n.DateAdded > 0 {
			node.Added = n.DateAdded / 1000
		}
		if n.URL != "" {
			node.Kind, node.URL = store.KindBookmark, strings.TrimSpace(n.URL)
			return node
		}
		node.Kind = store.KindFolder
		for _, c := range n.Children {
			if x := conv(c, depth+1); x != nil {
				node.Children = append(node.Children, x)
			}
		}
		return node
	}
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	tops := roots
	if len(roots) == 1 && roots[0].URL == "" {
		tops = roots[0].Children // the browser's unnamed root
	}
	for _, t := range tops {
		if x := conv(t, 1); x != nil {
			root.Children = append(root.Children, x)
		}
	}
	tr := store.Tree{Root: root}
	return tr, tr.Validate()
}
