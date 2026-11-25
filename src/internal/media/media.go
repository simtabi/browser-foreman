// Package media keeps the pictures beside bookmarks: each site's icon, taken from the bookmarks files themselves (no
// site is asked for it), and a snapshot of each page, taken by the installed Chrome running headless in a throwaway
// profile. File names follow the Python tool's, so its icons and snapshots can be copied in as they are:
// icons/<sha1(host)[:12]>.<ext> and thumbs/<sha1(url)[:16]>.jpg.
package media

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/simtabi/browser-foreman/src/internal/atomicfile"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Store is where the pictures are kept.
type Store struct{ Dir string }

// iconTypes are the image types an icon may be, by extension.
var iconTypes = map[string]string{"png": "image/png", "ico": "image/x-icon", "gif": "image/gif", "jpg": "image/jpeg", "svg": "image/svg+xml", "webp": "image/webp"}
var mimeExt = map[string]string{"image/png": "png", "image/x-icon": "ico", "image/vnd.microsoft.icon": "ico", "image/gif": "gif",
	"image/jpeg": "jpg", "image/svg+xml": "svg", "image/webp": "webp"}

var (
	iconName  = regexp.MustCompile(`^[0-9a-f]{12}\.(png|ico|gif|jpg|svg|webp)$`)
	thumbName = regexp.MustCompile(`^[0-9a-f]{16}\.jpg$`)
)

// Host is a link's host as icons are keyed: lower case, without "www.".
func Host(u string) string {
	_, rest, ok := strings.Cut(u, "://")
	if !ok {
		return ""
	}
	h, _, _ := strings.Cut(rest, "/")
	h, _, _ = strings.Cut(h, "?")
	h, _, _ = strings.Cut(h, "#")
	if i := strings.LastIndex(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return strings.TrimPrefix(strings.ToLower(h), "www.")
}

func sum(s string, n int) string { b := sha1.Sum([]byte(s)); return hex.EncodeToString(b[:])[:n] }

// ThumbKey is a page's snapshot name without the extension.
func ThumbKey(u string) string { return sum(u, 16) }

func (s Store) dir(kind string) string { return filepath.Join(s.Dir, kind) }

// Icon is the file name of a host's icon, "" when there is none. A Store with no Dir holds nothing.
func (s Store) Icon(host string) string {
	if host == "" || s.Dir == "" {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(s.dir("icons"), sum(host, 12)+".*"))
	for _, p := range m {
		if iconName.MatchString(filepath.Base(p)) {
			return filepath.Base(p)
		}
	}
	return ""
}

// Index is what a Store holds, read once: each icon by its host's name stem, and each snapshot's key. Use it to look up
// many bookmarks at once (Icon and HasThumb read the folder per call).
type Index struct {
	icons  map[string]string // name stem (sum(host, 12)) -> file
	thumbs map[string]int64  // ThumbKey -> size in bytes
	dir    string            // the snapshots folder
}

// Index reads the icons and snapshots folders once. A Store with no Dir, or a missing folder, holds nothing.
func (s Store) Index() Index {
	x := Index{icons: map[string]string{}, thumbs: map[string]int64{}, dir: s.dir("thumbs")}
	if s.Dir == "" {
		return x
	}
	if es, err := os.ReadDir(s.dir("icons")); err == nil {
		for _, e := range es {
			if n := e.Name(); !e.IsDir() && iconName.MatchString(n) {
				stem := strings.TrimSuffix(n, filepath.Ext(n))
				if _, dup := x.icons[stem]; !dup || n < x.icons[stem] {
					x.icons[stem] = n // the same pick as Icon: the first match in name order
				}
			}
		}
	}
	if es, err := os.ReadDir(s.dir("thumbs")); err == nil {
		for _, e := range es {
			if n := e.Name(); !e.IsDir() && strings.HasSuffix(n, ".jpg") {
				size := int64(-1) // unknown: decoded if it is ever asked about
				if info, err := e.Info(); err == nil {
					size = info.Size()
				}
				x.thumbs[strings.TrimSuffix(n, ".jpg")] = size
			}
		}
	}
	return x
}

// Icon is the file name of a host's icon in the index, "" when there is none.
func (x Index) Icon(host string) string {
	if host == "" {
		return ""
	}
	return x.icons[sum(host, 12)]
}

// HasThumb reports whether the index holds a snapshot of a page.
func (x Index) HasThumb(u string) bool {
	_, ok := x.thumbs[ThumbKey(u)]
	return ok
}

// HasThumb reports whether a page has a snapshot.
func (s Store) HasThumb(u string) bool {
	if s.Dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(s.dir("thumbs"), ThumbKey(u)+".jpg"))
	return err == nil
}

// SaveIconURI keeps a host's icon from a data: URI, unless it already has one. Blank placeholders (under 60 bytes,
// which Chrome stores for sites with no icon) and anything but an image are skipped. It reports whether it wrote one.
func (s Store) SaveIconURI(host, uri string) (bool, error) {
	if host == "" || s.Dir == "" || s.Icon(host) != "" {
		return false, nil
	}
	head, data, ok := strings.Cut(strings.TrimPrefix(uri, "data:"), ",")
	if !ok || !strings.HasPrefix(uri, "data:") || !strings.HasSuffix(head, ";base64") {
		return false, nil
	}
	ext, ok := mimeExt[strings.TrimSuffix(head, ";base64")]
	if !ok {
		return false, nil
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(b) < 60 || len(b) > 256<<10 {
		return false, nil
	}
	if err := os.MkdirAll(s.dir("icons"), 0o700); err != nil {
		return false, err
	}
	return true, writeAtomic(filepath.Join(s.dir("icons"), sum(host, 12)+"."+ext), b)
}

// ErrName: not a picture this store holds.
var ErrName = errors.New("no such picture")

// Path finds a picture by kind ("icons" or "thumbs") and file name, with its content type.
func (s Store) Path(kind, name string) (string, string, error) {
	switch {
	case s.Dir == "":
		return "", "", ErrName
	case kind == "icons" && iconName.MatchString(name):
		return filepath.Join(s.dir(kind), name), iconTypes[name[strings.LastIndex(name, ".")+1:]], nil
	case kind == "thumbs" && thumbName.MatchString(name):
		return filepath.Join(s.dir(kind), name), "image/jpeg", nil
	}
	return "", "", ErrName
}

func writeAtomic(path string, b []byte) error { return atomicfile.Write(path, b, 0o600) }
