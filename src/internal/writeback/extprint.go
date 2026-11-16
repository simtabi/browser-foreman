package writeback

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// ExtFingerprint identifies a browser's bookmarks as its extension sees them, computed the same way by the extension
// (web/src/extension/core.ts extFingerprint) over chrome.bookmarks.getTree(): every node below the browser's own
// root, depth first, as "id TAB title TAB url NEWLINE" with the title's spaces collapsed and the address trimmed,
// hashed with SHA-256. t is a tree the extension read (ids "ext-" + the browser's). A write refuses a browser whose
// fingerprint is no longer the plan's.
func ExtFingerprint(t store.Tree) string {
	var b strings.Builder
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for _, c := range n.Children {
			b.WriteString(strings.TrimPrefix(c.ID, importer.ExtPrefix))
			b.WriteByte('\t')
			b.WriteString(strings.Join(strings.Fields(c.Title), " "))
			b.WriteByte('\t')
			b.WriteString(strings.TrimSpace(c.URL))
			b.WriteByte('\n')
			walk(c)
		}
	}
	if t.Root != nil {
		walk(t.Root)
	}
	s := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(s[:])
}
