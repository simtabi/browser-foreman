package tabs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// literalBlock is an LZ4 block holding src as literals only: valid LZ4 any decoder must read.
func literalBlock(src []byte) []byte {
	var b bytes.Buffer
	n := len(src)
	if n < 15 {
		b.WriteByte(byte(n << 4))
	} else {
		b.WriteByte(0xF0)
		for r := n - 15; ; r -= 255 {
			if r < 255 {
				b.WriteByte(byte(r))
				break
			}
			b.WriteByte(255)
		}
	}
	b.Write(src)
	return b.Bytes()
}

func TestLZ4MatchesAndDamage(t *testing.T) {
	// "abc" then a match 3 back, 9 long, overlapping what it copies.
	got, err := lz4Block([]byte{0x35, 'a', 'b', 'c', 3, 0}, 12)
	if err != nil || string(got) != "abcabcabcabc" {
		t.Fatalf("%q %v", got, err)
	}
	for _, bad := range [][]byte{{0x35, 'a', 'b', 'c', 9, 0}, {0x35, 'a', 'b', 'c', 0, 0}, {0xF0}} {
		if _, err := lz4Block(bad, 12); err == nil {
			t.Errorf("%v decoded", bad)
		}
	}
}

func writeMozLz4(t *testing.T, path string, v any) {
	t.Helper()
	j, _ := json.Marshal(v)
	var b bytes.Buffer
	b.WriteString("mozLz40\x00")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(j)))
	b.Write(literalBlock(j))
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFirefoxSession(t *testing.T) {
	prof := filepath.Join(t.TempDir(), "abc.default")
	writeMozLz4(t, filepath.Join(prof, "sessionstore-backups", "recovery.jsonlz4"), map[string]any{"windows": []any{
		map[string]any{"selected": 2, "tabs": []any{
			map[string]any{"index": 2, "entries": []any{map[string]any{"url": "https://old.example/", "title": "Old"}, map[string]any{"url": "https://go.dev/", "title": "Go"}}},
			map[string]any{"index": 1, "pinned": true, "entries": []any{map[string]any{"url": "https://mail.example/", "title": "Mail"}}},
			map[string]any{"index": 1, "hidden": true, "entries": []any{map[string]any{"url": "https://hidden.example/"}}},
		}},
		map[string]any{"isPrivate": true, "tabs": []any{map[string]any{"index": 1, "entries": []any{map[string]any{"url": "https://secret.example/"}}}}},
	}})
	r := Sessions{Dirs: func(browsers.Browser) []string { return []string{prof} }}
	got := r.Read(context.Background(), browsers.Browser{Name: "Firefox", Engine: "gecko"})
	if got.Error != "" || !got.Running || len(got.Windows) != 1 {
		t.Fatalf("%+v", got)
	}
	w := got.Windows[0]
	if len(w.Tabs) != 2 || w.Tabs[0].URL != "https://go.dev/" || !w.Tabs[1].Pinned || w.Active != 2 || w.Mode != "normal" {
		t.Fatalf("window %+v (the current entry of each tab, hidden and private ones left out)", w)
	}
	// A session file not written for a while: the browser is not running.
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(prof, "sessionstore-backups", "recovery.jsonlz4"), old, old)
	if got := r.Read(context.Background(), browsers.Browser{Name: "Firefox", Engine: "gecko"}); got.Running || len(got.Windows) != 0 {
		t.Fatalf("stale session read as running: %+v", got)
	}
}

// snss builds a Chromium session file command by command.
type snss struct{ bytes.Buffer }

func newSNSS() *snss {
	s := &snss{}
	s.WriteString("SNSS")
	_ = binary.Write(s, binary.LittleEndian, int32(3))
	return s
}

func (s *snss) cmd(id byte, payload []byte) {
	_ = binary.Write(s, binary.LittleEndian, uint16(len(payload)+1))
	s.WriteByte(id)
	s.Write(payload)
}

func ints(v ...int32) []byte {
	var b bytes.Buffer
	for _, x := range v {
		_ = binary.Write(&b, binary.LittleEndian, x)
	}
	return b.Bytes()
}

func nav(tab, index int32, url, title string) []byte {
	var p bytes.Buffer
	p.Write(ints(tab, index, int32(len(url))))
	p.WriteString(url)
	p.Write(make([]byte, pad4(len(url))-len(url)))
	u := utf16.Encode([]rune(title))
	p.Write(ints(int32(len(u))))
	for _, c := range u {
		_ = binary.Write(&p, binary.LittleEndian, c)
	}
	p.Write(make([]byte, pad4(2*len(u))-2*len(u)))
	p.Write(ints(0, 0)) // the rest of a navigation, which is not read
	return append(ints(int32(p.Len())), p.Bytes()...)
}

func TestChromiumSession(t *testing.T) {
	s := newSNSS()
	s.cmd(cmdSetTabWindow, ints(1, 10))
	s.cmd(cmdSetTabIndexInWindow, ints(10, 1))
	s.cmd(cmdUpdateTabNavigation, nav(10, 0, "https://old.example/", "Old"))
	s.cmd(cmdUpdateTabNavigation, nav(10, 1, "https://go.dev/", "Go — the language"))
	s.cmd(cmdSetSelectedNavIndex, ints(10, 1))
	s.cmd(cmdSetTabWindow, ints(1, 11))
	s.cmd(cmdSetTabIndexInWindow, ints(11, 0))
	s.cmd(cmdUpdateTabNavigation, nav(11, 0, "https://mail.example/", "Mail"))
	s.cmd(cmdSetPinnedState, append(ints(11), 1, 0, 0, 0))
	s.cmd(cmdSetTabWindow, ints(1, 12)) // closed
	s.cmd(cmdUpdateTabNavigation, nav(12, 0, "https://closed.example/", "Closed"))
	s.cmd(cmdTabClosed, ints(12, 0, 0))
	s.cmd(cmdSetTabWindow, ints(2, 20)) // its window closed
	s.cmd(cmdUpdateTabNavigation, nav(20, 0, "https://gone.example/", "Gone"))
	s.cmd(cmdWindowClosed, ints(2, 0, 0))
	s.cmd(cmdSetSelectedTabInIndex, ints(1, 1))
	b := s.Bytes()
	b = append(b, 40, 0, 6) // a command cut short, as when the browser is writing
	ws, err := parseSNSS(b)
	if err != nil || len(ws) != 1 {
		t.Fatalf("%+v %v", ws, err)
	}
	w := ws[0]
	if len(w.Tabs) != 2 || w.Tabs[0].URL != "https://mail.example/" || !w.Tabs[0].Pinned || w.Tabs[1].Title != "Go — the language" || w.Active != 2 || w.ID != "1" {
		t.Fatalf("window %+v", w)
	}
	if _, err := parseSNSS([]byte("nope")); err == nil {
		t.Fatal("not a session file, read anyway")
	}
	// The newest session file of a profile is read.
	prof := t.TempDir()
	_ = os.MkdirAll(filepath.Join(prof, "Sessions"), 0o700)
	_ = os.WriteFile(filepath.Join(prof, "Sessions", "Session_1"), []byte("SNSS\x03\x00\x00\x00"), 0o600)
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(filepath.Join(prof, "Sessions", "Session_1"), old, old)
	_ = os.WriteFile(filepath.Join(prof, "Sessions", "Session_2"), b, 0o600)
	got := Sessions{Dirs: func(browsers.Browser) []string { return []string{prof} }}.Read(context.Background(), browsers.Browser{Name: "Google Chrome", Engine: "chromium"})
	if !got.Running || len(got.Windows) != 1 || got.Via != "session file" {
		t.Fatalf("%+v", got)
	}
}
