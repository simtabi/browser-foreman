// Package docscheck holds the guard for the README and docs/ authoring standard (the Simtabi and laranail docs
// standard): the README is a slim pointer with its sections in order and a grouped index, and every docs page has
// its title first and exactly one footer back to that index, at the right depth. It reads the real files.
package docscheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const root = "../../.." // the repository, from src/internal/docscheck

// minPages is the floor on pages inspected: a glob that stops matching must fail, not report a clean tree.
// Measured 2026-11-13 with `find docs -name '*.md' | wc -l`: 28.
const minPages = 25

func pages(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < minPages {
		t.Fatalf("inspected %d docs pages, fewer than the floor of %d: is the docs/ path right?", len(out), minPages)
	}
	return out
}

func TestEveryPageHasItsTitleFirstAndOneFooter(t *testing.T) {
	for _, p := range pages(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.TrimRight(string(b), "\n")
		rel, _ := filepath.Rel(filepath.Join(root, "docs"), p)
		up := "../"
		if strings.Contains(rel, string(filepath.Separator)) {
			up = "../../"
		}
		footer := "[← Docs index](" + up + "README.md#documentation)"
		lines := strings.Split(text, "\n")
		if !strings.HasPrefix(lines[0], "# ") {
			t.Errorf("%s: the first line is not its # title: %q", rel, lines[0])
		}
		if n := strings.Count(text, "Docs index]("); n != 1 {
			t.Errorf("%s: the docs index link appears %d times, want once (the footer)", rel, n)
		}
		if len(lines) < 3 || lines[len(lines)-1] != footer || lines[len(lines)-2] != "---" {
			t.Errorf("%s: does not end with ---, then %s", rel, footer)
		}
	}
}

func TestNoIndexPageNoADRsNoDecorativeEmoji(t *testing.T) {
	for _, bad := range []string{"docs/README.md", "docs/adr"} {
		if _, err := os.Stat(filepath.Join(root, bad)); err == nil {
			t.Errorf("%s exists: the index is the README's Documentation section; rationale lives in architecture.md", bad)
		}
	}
	emoji := regexp.MustCompile(`[\x{1F300}-\x{1FAFF}\x{2705}\x{274C}\x{26A0}\x{2B50}]`)
	for _, p := range append(pages(t), filepath.Join(root, "README.md")) {
		b, _ := os.ReadFile(p)
		if m := emoji.FindString(string(b)); m != "" {
			t.Errorf("%s: decorative emoji %q", p, m)
		}
	}
}

func TestTheReadmeIndexesEveryPageInOrder(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)
	// The spine, in order.
	at := -1
	for _, h := range []string{"## Install", "## Quick start guide and usage", "### Getting started", "### Usage",
		`## <a name="documentation"></a>Documentation`, "### Guides", "### Reference", "### Recipes", "## License"} {
		i := strings.Index(readme, "\n"+h+"\n")
		if i < 0 || i < at {
			t.Fatalf("README: %q is missing or out of order", h)
		}
		at = i
	}
	// Every page is listed, and every listed page exists.
	linked := map[string]bool{}
	for _, m := range regexp.MustCompile(`\]\((docs/[^)#]+\.md)\)`).FindAllStringSubmatch(readme, -1) {
		linked[m[1]] = true
		if _, err := os.Stat(filepath.Join(root, m[1])); err != nil {
			t.Errorf("README links %s, which does not exist", m[1])
		}
	}
	for _, p := range pages(t) {
		rel, _ := filepath.Rel(root, p)
		if !linked[filepath.ToSlash(rel)] {
			t.Errorf("README's Documentation section does not list %s", rel)
		}
	}
}
