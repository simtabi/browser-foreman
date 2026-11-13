package importer

import (
	"encoding/json"
	"testing"
)

func TestFromExtension(t *testing.T) {
	// Chrome's shape: an unnamed root "0", then Bookmarks bar "1" and Other bookmarks "2".
	var chrome []ExtNode
	_ = json.Unmarshal([]byte(`[{"id":"0","title":"","children":[
		{"id":"1","title":"Bookmarks bar","children":[{"id":"5","title":"Go","url":"https://go.dev/","dateAdded":1700000000123},
			{"id":"6","title":"Dev","children":[{"id":"7","title":"GitHub","url":"https://github.com/"}]}]},
		{"id":"2","title":"Other bookmarks","children":[]}]}]`), &chrome)
	tr, err := FromExtension(chrome)
	if err != nil {
		t.Fatal(err)
	}
	bar := tr.Root.Children[0]
	if len(tr.Root.Children) != 2 || bar.ID != "ext-1" || bar.Children[0].ID != "ext-5" || bar.Children[0].Added != 1700000000 || bar.Children[1].Children[0].URL != "https://github.com/" {
		t.Fatalf("%+v", bar)
	}
	// Firefox's shape, with a separator.
	var firefox []ExtNode
	_ = json.Unmarshal([]byte(`[{"id":"root________","title":"","children":[{"id":"toolbar_____","title":"Bookmarks Toolbar","children":[
		{"id":"a1","title":"Go","url":"https://go.dev/"},{"id":"s1","type":"separator","title":""}]}]}]`), &firefox)
	tr, err = FromExtension(firefox)
	if err != nil || len(tr.Root.Children[0].Children) != 1 {
		t.Fatalf("%+v %v", tr.Root.Children[0], err)
	}
	if _, err := FromExtension(nil); err == nil {
		t.Fatal("nothing sent was accepted")
	}
}
