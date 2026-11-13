// Package importer reads browsers' bookmark files into a store.Tree.
//
// Two formats cover every browser:
//
//   - Chrome's JSON "Bookmarks" file, used by Chrome, Edge, Brave, Vivaldi, Opera and Arc.
//   - The Netscape bookmark HTML file every browser exports (Firefox, Safari, Chrome's own export) and imports, and
//     Browser Foreman writes.
//
// Read detects which one it was given. Node ids are assigned in reading order ("n1", "n2", …), so the same file always
// reads to the same tree.
package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// MaxSize is the largest file Read accepts.
const MaxSize = 64 << 20

// Format names what Read found.
type Format string

const (
	Chrome   Format = "chrome-json"
	Netscape Format = "netscape-html"
)

// Read detects the format and returns the tree.
func Read(r io.Reader) (store.Tree, Format, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return store.Tree{}, "", err
	}
	if len(b) > MaxSize {
		return store.Tree{}, "", fmt.Errorf("the file is larger than %d MB", MaxSize>>20)
	}
	switch {
	case bytes.HasPrefix(b, sqliteMagic):
		t, err := ReadFirefoxBytes(b)
		return t, Firefox, err
	case isPlist(b):
		t, err := ReadSafari(b)
		return t, Safari, err
	}
	trimmed := bytes.TrimSpace(b)
	switch {
	case bytes.HasPrefix(trimmed, []byte("{")):
		t, err := ReadChrome(trimmed)
		return t, Chrome, err
	case bytes.Contains(bytes.ToUpper(trimmed[:min(len(trimmed), 512)]), []byte("NETSCAPE-BOOKMARK-FILE")) || bytes.Contains(bytes.ToUpper(trimmed), []byte("<DL")):
		t, err := ReadNetscape(trimmed)
		return t, Netscape, err
	}
	return store.Tree{}, "", errors.New("not a bookmarks file: expected an exported bookmarks HTML file, Chrome's Bookmarks (JSON), Firefox's places.sqlite or Safari's Bookmarks.plist")
}

type ids struct{ n int }

func (i *ids) next() string { i.n++; return "n" + strconv.Itoa(i.n) }

// ---- Chrome JSON ----

type chromeNode struct {
	Type      string       `json:"type"`
	Name      string       `json:"name"`
	URL       string       `json:"url"`
	DateAdded string       `json:"date_added"`
	Children  []chromeNode `json:"children"`
}

// chromeEpochOffset is the seconds from Chrome's epoch (1601-01-01, counted in microseconds) to Unix's (1970-01-01).
// Plain integer arithmetic: a time.Duration from 1601 overflows (it holds about 292 years of nanoseconds).
const chromeEpochOffset = 11644473600

func chromeTime(s string) int64 {
	us, err := strconv.ParseInt(s, 10, 64)
	if err != nil || us <= 0 {
		return 0
	}
	if unix := us/1_000_000 - chromeEpochOffset; unix > 0 {
		return unix
	}
	return 0
}

// ReadChrome reads Chrome's Bookmarks file. Its roots become the top-level folders: Bookmarks Bar, Other bookmarks,
// Mobile bookmarks (when not empty).
func ReadChrome(b []byte) (store.Tree, error) {
	var doc struct {
		Roots map[string]chromeNode `json:"roots"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return store.Tree{}, fmt.Errorf("Chrome bookmarks file: %w", err)
	}
	if len(doc.Roots) == 0 {
		return store.Tree{}, errors.New("Chrome bookmarks file has no roots")
	}
	var id ids
	var conv func(c chromeNode) *store.Node
	conv = func(c chromeNode) *store.Node {
		if c.Type == "url" {
			return &store.Node{ID: id.next(), Kind: store.KindBookmark, Title: clean(c.Name), URL: strings.TrimSpace(c.URL), Added: chromeTime(c.DateAdded)}
		}
		f := &store.Node{ID: id.next(), Kind: store.KindFolder, Title: clean(c.Name), Added: chromeTime(c.DateAdded)}
		for _, ch := range c.Children {
			f.Children = append(f.Children, conv(ch))
		}
		return f
	}
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	names := map[string]string{"bookmark_bar": "Bookmarks Bar", "other": "Other bookmarks", "synced": "Mobile bookmarks"}
	for _, key := range []string{"bookmark_bar", "other", "synced"} {
		r, ok := doc.Roots[key]
		if !ok || (key == "synced" && len(r.Children) == 0) {
			continue
		}
		f := conv(r)
		f.Title = names[key]
		root.Children = append(root.Children, f)
	}
	t := store.Tree{Root: root}
	return t, t.Validate()
}

// ---- Netscape HTML ----

// ReadNetscape reads an exported bookmarks HTML file: <DT><H3>folder</H3><DL>…</DL> for folders,
// <DT><A HREF="…" ADD_DATE="…">title</A> for bookmarks and <DD> for a description of the item before it.
func ReadNetscape(b []byte) (store.Tree, error) {
	z := html.NewTokenizer(bytes.NewReader(b))
	root := &store.Node{ID: "root", Kind: store.KindFolder}
	stack := []*store.Node{root}
	var id ids
	var pending *store.Node // the folder an H3 named, waiting for its <DL>
	var last *store.Node    // the last item, for a following <DD>
	var text strings.Builder
	inTitle, inDD := "", false
	var href, added string
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				t := store.Tree{Root: root}
				return t, t.Validate()
			}
			return store.Tree{}, z.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			attrs := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				attrs[strings.ToLower(string(k))] = string(v)
			}
			switch string(name) {
			case "h3":
				inTitle, inDD = "h3", false
				added = attrs["add_date"]
				text.Reset()
			case "a":
				inTitle, inDD = "a", false
				href, added = attrs["href"], attrs["add_date"]
				text.Reset()
			case "dl":
				// A list right after a folder's name holds that folder's items; the file's outer list (no name before
				// it) leaves its items in the root.
				if pending != nil {
					stack = append(stack, pending)
					pending = nil
				}
			case "dd":
				inDD = true
				text.Reset()
			case "dt":
				if inDD && last != nil {
					last.Note = clean(text.String())
				}
				inDD = false
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "h3":
				f := &store.Node{ID: id.next(), Kind: store.KindFolder, Title: clean(text.String()), Added: unix(added)}
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, f)
				pending, last, inTitle = f, f, ""
			case "a":
				bm := &store.Node{ID: id.next(), Kind: store.KindBookmark, Title: clean(text.String()), URL: strings.TrimSpace(href), Added: unix(added)}
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, bm)
				last, inTitle = bm, ""
			case "dl":
				if inDD && last != nil {
					last.Note = clean(text.String())
					inDD = false
				}
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
			}
		case html.TextToken:
			if inTitle != "" || inDD {
				text.Write(z.Text())
			}
		}
	}
}

func unix(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	if n > 1e12 { // some exporters write milliseconds or microseconds
		for n > 1e11 {
			n /= 1000
		}
	}
	return n
}

func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 1000 {
		s = s[:1000]
	}
	return s
}
