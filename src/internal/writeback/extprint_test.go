package writeback

import (
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// extVector is the fixture both sides hash; the extension's test (web/src/tests/extension.test.mjs) pins the same
// value, so the two computations cannot drift apart.
func extVector() store.Tree {
	return tree(
		f("ext-1", "Bookmarks bar", b("ext-5", "A  title ", " https://a.example/x "), f("ext-6", "Dev", b("ext-7", "Café", "https://café.example/"))),
		f("ext-2", "Other bookmarks"),
		f("ext-3", "Mobile bookmarks"),
	)
}

const extVectorPrint = "6c6d0bc98edc01336972cf52b62c45cecfbd5d86e77eede6c212738b9bb6a758"

func TestExtFingerprintMatchesTheExtensions(t *testing.T) {
	got := ExtFingerprint(extVector())
	if got != extVectorPrint {
		t.Fatalf("fingerprint %s, want the value the extension computes, %s", got, extVectorPrint)
	}
	moved := extVector()
	moved.Root.Children[1].Children = append(moved.Root.Children[1].Children, moved.Root.Children[0].Children[0])
	moved.Root.Children[0].Children = moved.Root.Children[0].Children[1:]
	if ExtFingerprint(moved) == got {
		t.Fatal("a moved bookmark kept the fingerprint")
	}
}
