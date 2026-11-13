package webui

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestPageFingerprintsItsAssets(t *testing.T) {
	page := `<script src="assets/js/theme.js"></script><link href="assets/css/tabler.min.css">` + dataMarker + `<script type="module" src="assets/js/app.js"></script>`
	fsys := fstest.MapFS{
		"index.html": {Data: []byte(page)}, "assets/js/theme.js": {Data: []byte("a")}, "assets/js/v1.js": {Data: []byte("b")},
		"assets/js/app.js": {Data: []byte("c")}, "assets/css/tabler.min.css": {Data: []byte("d")},
	}
	b, err := Page(fsys, []byte("{}"), "n")
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, name := range []string{"assets/js/theme.js?v=", "assets/js/v1.js?v=", "assets/js/app.js?v=", "assets/css/tabler.min.css?v="} {
		if !strings.Contains(out, `"`+name) {
			t.Errorf("%s not fingerprinted: %s", name, out)
		}
	}
	fsys["assets/js/app.js"] = &fstest.MapFile{Data: []byte("changed")}
	b2, _ := Page(fsys, []byte("{}"), "n")
	if string(b2) == out {
		t.Fatal("a changed script kept its address, so a browser could keep running the old one")
	}
}
