package version

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The repository must never carry a user's data: bookmark and tab databases, workspace history, built pages with
// bookmarks embedded, favicon caches, or secrets. The tool keeps all of those in ~/.config and ~/.local/state; a copy
// in the repository would publish someone's browsing.
func TestNoPersonalDataInTheRepository(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	forbidden := func(rel string, d fs.DirEntry) string {
		base := d.Name()
		switch {
		case d.IsDir() && (base == "storage" || base == "favicons" || base == "workspaces" || base == "sessions"):
			return "a data folder"
		case base == ".env" || base == ".mcp.json":
			return "a secrets or local tool file"
		case strings.HasSuffix(base, ".sqlite3") || strings.HasSuffix(base, ".jsonl") || strings.HasSuffix(base, ".json.gz"):
			return "a database or history file"
		case rel == filepath.Join("public", "index.html"):
			return "a built page (it embeds bookmarks)"
		}
		return ""
	}
	seen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || rel == "build" || rel == filepath.Join("web", "dist")) {
			return filepath.SkipDir
		}
		seen++
		if why := forbidden(rel, d); why != "" {
			t.Errorf("%s is %s and must not be in the repository", rel, why)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 100 {
		t.Fatalf("only %d paths inspected: the walk is not seeing the repository", seen)
	}
	if _, err := os.Stat(filepath.Join(root, "README.md")); err != nil {
		t.Fatalf("not at the repository root: %v", err)
	}
}
