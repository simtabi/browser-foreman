package edit

import (
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func types(ev []Event) string {
	var s []string
	for _, e := range ev {
		s = append(s, e.Type+":"+e.Data["id"].(string))
	}
	return strings.Join(s, " ")
}

// Diff of what Apply produced must name the same changes Apply reported.
func TestDiffAgreesWithApply(t *testing.T) {
	cases := [][]Op{
		{{Op: "create", Parent: "dev", Kind: "bookmark", Title: s("Docs"), URL: s("https://pkg.go.dev/")}},
		{{Op: "update", ID: "go", Title: s("Golang")}},
		{{Op: "move", IDs: []string{"mdn"}, Parent: "other"}},
		{{Op: "move", IDs: []string{"mdn"}, Parent: "bar", Index: i(0)}},
		{{Op: "trash", IDs: []string{"dev"}}},
	}
	for _, ops := range cases {
		next, want, err := Apply(sample(), ops, time.Now(), ids())
		if err != nil {
			t.Fatal(err)
		}
		got, err := Diff(sample(), next)
		if err != nil {
			t.Fatal(err)
		}
		if types(got) != types(want) {
			t.Errorf("%s: diff says %q, apply said %q", ops[0].Op, types(got), types(want))
		}
	}
}

func TestDiffRestoreDeleteAndNoise(t *testing.T) {
	trashed, _ := run(t, sample(), Op{Op: "trash", IDs: []string{"go", "dev"}})
	back, _ := run(t, trashed, Op{Op: "restore", IDs: []string{"go"}})
	if got, _ := Diff(trashed, back); types(got) != "bookmark.restored:go" {
		t.Fatal(types(got))
	}
	gone, _ := run(t, trashed, Op{Op: "purge"})
	if got, _ := Diff(trashed, gone); len(got) != 1 || got[0].Type != "trash.emptied" {
		t.Fatal(types(got))
	}
	one, _ := run(t, trashed, Op{Op: "purge", IDs: []string{"go"}})
	if got, _ := Diff(trashed, one); types(got) != "bookmark.deleted:go" {
		t.Fatal(types(got))
	}
	if got, _ := Diff(sample(), sample()); len(got) != 0 {
		t.Fatal("an unchanged tree produced events:", types(got))
	}
	// Removing the first item shifts its siblings; they did not move.
	if got, _ := Diff(sample(), func() store.Tree {
		x, _ := run(t, sample(), Op{Op: "move", IDs: []string{"go"}, Parent: "other"})
		return x
	}()); types(got) != "bookmark.moved:go" {
		t.Fatal(types(got))
	}
}

func TestDiffRefusesUnsafeNewURLs(t *testing.T) {
	next, _ := run(t, sample(), Op{Op: "update", ID: "go", Title: s("x")})
	next.Root.Children[0].Children[0].URL = "javascript:alert(1)"
	if _, err := Diff(sample(), next); err == nil || !IsRefusal(err) {
		t.Fatal("a javascript: URL got through a whole-tree save")
	}
	bad := sample()
	bad.Root.Children[0].Children = append(bad.Root.Children[0].Children, bad.Root.Children[0].Children[0])
	if _, err := Diff(sample(), bad); err == nil {
		t.Fatal("a tree with a repeated id was accepted")
	}
}
