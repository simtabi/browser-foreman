// Package exporter writes a bookmark tree in the two formats browsers read back: the Netscape bookmarks HTML every
// browser imports, and Chrome's Bookmarks JSON (for Chrome, Edge, Brave, Vivaldi, Opera and Arc). What the importer
// reads, this writes, so a round trip keeps folders, titles, URLs, dates and notes.
package exporter

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"strconv"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Netscape writes the tree as an exported-bookmarks HTML file. The top folders keep their names; a note is written
// as the <DD> after its item, which is where browsers and the importer look for it.
func Netscape(w io.Writer, t store.Tree) error {
	var b strings.Builder
	b.WriteString("<!DOCTYPE NETSCAPE-Bookmark-file-1>\n<!-- This is an automatically generated file.\n     It will be read and overwritten.\n     DO NOT EDIT! -->\n")
	b.WriteString(`<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">` + "\n<TITLE>Bookmarks</TITLE>\n<H1>Bookmarks</H1>\n<DL><p>\n")
	var walk func(n *store.Node, depth int)
	walk = func(n *store.Node, depth int) {
		pad := strings.Repeat("    ", depth)
		for _, c := range n.Children {
			added := ""
			if c.Added > 0 {
				added = ` ADD_DATE="` + strconv.FormatInt(c.Added, 10) + `"`
			}
			if c.Kind == store.KindFolder {
				bar := ""
				if depth == 1 && strings.EqualFold(c.Title, "Bookmarks Bar") {
					bar = ` PERSONAL_TOOLBAR_FOLDER="true"`
				}
				fmt.Fprintf(&b, "%s<DT><H3%s%s>%s</H3>\n", pad, added, bar, html.EscapeString(c.Title))
				note(&b, pad, c.Note)
				fmt.Fprintf(&b, "%s<DL><p>\n", pad)
				walk(c, depth+1)
				fmt.Fprintf(&b, "%s</DL><p>\n", pad)
				continue
			}
			fmt.Fprintf(&b, "%s<DT><A HREF=\"%s\"%s>%s</A>\n", pad, html.EscapeString(c.URL), added, html.EscapeString(c.Title))
			note(&b, pad, c.Note)
		}
	}
	if t.Root != nil {
		walk(t.Root, 1)
	}
	b.WriteString("</DL><p>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func note(b *strings.Builder, pad, text string) {
	if text != "" {
		fmt.Fprintf(b, "%s<DD>%s\n", pad, html.EscapeString(strings.ReplaceAll(text, "\n", " ")))
	}
}

const chromeEpochOffset = 11644473600 // seconds from 1601-01-01 to 1970-01-01

type chromeNode struct {
	Children  []*chromeNode `json:"children,omitempty"`
	DateAdded string        `json:"date_added"`
	GUID      string        `json:"guid,omitempty"`
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	URL       string        `json:"url,omitempty"`
}

// Chrome writes the tree as Chrome's Bookmarks JSON. The top folders map to Chrome's three roots by name (Bookmarks
// Bar, Other bookmarks, Mobile bookmarks); any other top folder goes inside Other bookmarks. Chrome recomputes the
// checksum, so it is left empty. Notes have no place in this format and are not written.
func Chrome(w io.Writer, t store.Tree) error {
	next := 0
	id := func() string { next++; return strconv.Itoa(next) }
	stamp := func(unix int64) string {
		if unix <= 0 {
			return "0"
		}
		return strconv.FormatInt((unix+chromeEpochOffset)*1_000_000, 10)
	}
	var conv func(n *store.Node) *chromeNode
	conv = func(n *store.Node) *chromeNode {
		c := &chromeNode{ID: id(), Name: n.Title, DateAdded: stamp(n.Added)}
		if n.Kind == store.KindBookmark {
			c.Type, c.URL = "url", n.URL
			return c
		}
		c.Type, c.Children = "folder", []*chromeNode{}
		for _, ch := range n.Children {
			c.Children = append(c.Children, conv(ch))
		}
		return c
	}
	roots := map[string]*chromeNode{}
	names := map[string]string{"bookmarks bar": "bookmark_bar", "bookmarks toolbar": "bookmark_bar", "favorites bar": "bookmark_bar",
		"other bookmarks": "other", "mobile bookmarks": "synced"}
	display := map[string]string{"bookmark_bar": "Bookmarks bar", "other": "Other bookmarks", "synced": "Mobile bookmarks"}
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		roots[key] = &chromeNode{ID: id(), Name: display[key], Type: "folder", DateAdded: "0", Children: []*chromeNode{}}
	}
	if t.Root != nil {
		for _, top := range t.Root.Children {
			key, ok := names[strings.ToLower(top.Title)]
			if top.Kind == store.KindFolder && ok {
				if top.Added > 0 {
					roots[key].DateAdded = stamp(top.Added)
				}
				for _, ch := range top.Children {
					roots[key].Children = append(roots[key].Children, conv(ch))
				}
				continue
			}
			roots["other"].Children = append(roots["other"].Children, conv(top))
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "   ")
	enc.SetEscapeHTML(false)
	return enc.Encode(map[string]any{"checksum": "", "roots": roots, "version": 1})
}
