package atomicfile

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteReplacesWithTheModeAndLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	st, _ := os.Stat(p)
	if string(b) != "new" || st.Mode().Perm() != 0o600 {
		t.Fatalf("%q %v", b, st.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("left behind: %v", ents)
	}
}

// Writers at once never share a temporary file, so none ends up with another's bytes or a missing file.
func TestConcurrentWritersEachWriteWhole(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Write(p, []byte(strings.Repeat(string(rune('a'+i)), 4096)), 0o600); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	b, _ := os.ReadFile(p)
	if len(b) != 4096 || strings.Count(string(b), string(b[0])) != 4096 {
		t.Fatalf("a mixed or short file: %d bytes", len(b))
	}
}
