package importer

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
)

// IconURIs collects the site icons an exported bookmarks HTML file carries (<A HREF=… ICON="data:…">), one per link
// address, so no site has to be asked for its icon. Other formats carry none.
func IconURIs(b []byte) map[string]string {
	out := map[string]string{}
	if !bytes.Contains(b, []byte("ICON=")) && !bytes.Contains(b, []byte("icon=")) {
		return out
	}
	z := html.NewTokenizer(bytes.NewReader(b))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, has := z.TagName()
			if string(name) != "a" || !has {
				continue
			}
			var href, icon string
			for {
				k, v, more := z.TagAttr()
				switch string(k) {
				case "href":
					href = string(v)
				case "icon":
					icon = string(v)
				}
				if !more {
					break
				}
			}
			if href != "" && strings.HasPrefix(icon, "data:image/") {
				if _, ok := out[href]; !ok {
					out[href] = icon
				}
			}
		}
	}
}
