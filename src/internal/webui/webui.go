// Package webui holds the web interface the binary serves: the page (web/src/template.html), its stylesheets, fonts
// and scripts, copied in by `make web` and embedded at build time. A binary built without them still runs; Built
// reports false and the server shows a short page saying how to build the interface.
package webui

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
)

//go:embed all:dist
var files embed.FS

// Assets is the dist folder: index.html at its root, assets/{css,fonts,js} below.
func Assets() fs.FS {
	sub, _ := fs.Sub(files, "dist")
	return sub
}

// Built reports whether fsys holds the interface (an index.html with the data script to replace).
func Built(fsys fs.FS) bool {
	b, err := fs.ReadFile(fsys, "index.html")
	return err == nil && bytes.Contains(b, []byte(dataMarker))
}

// dataMarker is the page's data script, which Page replaces with the server's own.
const dataMarker = "<script>\nconst DATA = /*DATA*/null;\nwindow.BF_DATA = DATA;\n</script>"

// Page returns fsys's index.html with its data script swapped for three that run in order: the boot data (bootJSON, already
// safe to embed in a script), v1.js (which turns it into the page's data and installs the save hooks), and the page's
// DATA constant. The inline scripts carry nonce, which the response's Content Security Policy names.
func Page(fsys fs.FS, bootJSON []byte, nonce string) ([]byte, error) {
	b, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		return nil, err
	}
	var s bytes.Buffer
	s.WriteString(`<script nonce="` + nonce + `">window.BF_V1 = `)
	s.Write(bootJSON)
	s.WriteString(";</script>\n<script src=\"assets/js/v1.js\"></script>\n")
	s.WriteString(`<script nonce="` + nonce + "\">\nconst DATA = window.BF_PAGE;\nwindow.BF_DATA = DATA;\n</script>")
	return fingerprint(fsys, bytes.Replace(b, []byte(dataMarker), s.Bytes(), 1)), nil
}

// fingerprint adds each script's and stylesheet's content hash to its address in the page (assets/js/app.js becomes
// assets/js/app.js?v=1a2b3c4d), so after an upgrade a browser can never run a script it kept from before.
func fingerprint(fsys fs.FS, page []byte) []byte {
	for _, name := range []string{"assets/js/theme.js", "assets/js/v1.js", "assets/js/app.js", "assets/css/tabler.min.css", "assets/css/tabler-icons.min.css"} {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		v := name + "?v=" + hex.EncodeToString(sum[:4])
		page = bytes.ReplaceAll(page, []byte(`"`+name+`"`), []byte(`"`+v+`"`))
	}
	return page
}
