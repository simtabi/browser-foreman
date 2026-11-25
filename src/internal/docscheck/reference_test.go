package docscheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The reference pages must keep up with the code: every route the server registers is named in docs/ (the REST
// table indexes them, and a route may be described on its tool's page), and every event the server publishes is in
// the events table. Both read the real source, count what they read, and fail below a floor.

var (
	routeRe = regexp.MustCompile(`HandleFunc\("(?:GET|POST|PUT|PATCH|DELETE) /api/v1(/[^"]*)"`)
	eventRe = regexp.MustCompile(`Publish\("([a-z]+(?:\.[a-z_]+)+)"`)
)

// sourceMatches walks src/internal (tests left out) and collects re's first group.
func sourceMatches(t *testing.T, re *regexp.Regexp) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, "src", "internal"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			found[m[1]] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func allDocs(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, p := range pages(t) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// Measured 2026-11-17: 93 distinct route paths (110 with their methods), 35 events.
const (
	minRoutes = 85
	minEvents = 30
)

func TestEveryRouteIsDocumented(t *testing.T) {
	routes := sourceMatches(t, routeRe)
	if len(routes) < minRoutes {
		t.Fatalf("found %d routes, fewer than the floor of %d: is the pattern right?", len(routes), minRoutes)
	}
	docs := allDocs(t)
	for r, file := range routes {
		// The whole path, so "/resume" written alone does not stand for every route that ends in it.
		if !strings.Contains(docs, r) {
			t.Errorf("%s (registered in %s) is not in docs/", r, file)
		}
	}
}

func TestEveryEventIsInTheEventsTable(t *testing.T) {
	events := sourceMatches(t, eventRe)
	if len(events) < minEvents {
		t.Fatalf("found %d events, fewer than the floor of %d: is the pattern right?", len(events), minEvents)
	}
	b, err := os.ReadFile(filepath.Join(root, "docs", "tools", "events.md"))
	if err != nil {
		t.Fatal(err)
	}
	for e, file := range events {
		if !strings.Contains(string(b), "`"+e+"`") {
			t.Errorf("event %s (published in %s) is not in docs/tools/events.md", e, file)
		}
	}
}
