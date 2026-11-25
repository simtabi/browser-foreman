package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/writeback"
)

// chromeNodes is Chrome's bookmarks as its extension sends them: A and B loose in Other bookmarks.
const chromeNodes = `[{"id":"0","title":"","children":[{"id":"1","title":"Bookmarks bar","children":[]},{"id":"2","title":"Other bookmarks","children":[{"id":"5","title":"A","url":"https://a.example/"},{"id":"6","title":"B","url":"https://b.example/"}]},{"id":"3","title":"Mobile bookmarks","children":[]}]}]`

func TestThePageCanPreviewAndRefusesAStaleApply(t *testing.T) {
	s := newServer(t, apiTok)
	h := s.Handler()
	chrome := paired(t, s, h, "Google Chrome")
	ctx, now := context.Background(), time.Now()
	n := func(id, kind, title, url string, kids ...*store.Node) *store.Node {
		return &store.Node{ID: id, Kind: kind, Title: title, URL: url, Children: kids}
	}
	base := store.Tree{Root: n("root", "folder", "", "", n("b1", "folder", "Bookmarks Bar", ""), n("b2", "bookmark", "A", "https://a.example/"), n("b3", "bookmark", "B", "https://b.example/"))}
	ours := store.Tree{Root: n("root", "folder", "", "", n("o1", "folder", "Bookmarks Bar", "", n("o2", "folder", "Reading", "", n("o3", "bookmark", "A", "https://a.example/"))))}
	for id, tr := range map[string]store.Tree{"export": base, "main": ours} {
		if _, err := writeback.Save(ctx, s.Data, id, id, "import", id, tr, now); err != nil {
			t.Fatal(err)
		}
	}
	// The extension: takes each command and, for a read, sends Chrome's bookmarks.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			var got struct{ Commands []struct{ ID, Kind string } }
			_ = json.Unmarshal(fromExtension(h, "GET", "/api/v1/extension/commands?wait=1", "", chrome).Body.Bytes(), &got)
			for _, c := range got.Commands {
				if c.Kind == "read-bookmarks" {
					fromExtension(h, "POST", "/api/v1/extension/bookmarks", `{"nodes":`+chromeNodes+`}`, chrome)
				}
				fromExtension(h, "POST", "/api/v1/extension/commands/"+c.ID+"/done", `{"ok":true}`, chrome)
			}
		}
	}()
	body := `{"workspace":"main","base":"export","browser":"Google Chrome","mode":"merge"}`
	w := send(h, "POST", "/api/v1/writeback/preview", body)
	var p struct {
		Report writeback.Report `json:"report"`
		Expect string           `json:"expect"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || len(p.Expect) != 64 || len(p.Report.Removed) != 1 || p.Report.Removed[0].Title != "B" || len(p.Report.Added) != 1 {
		t.Fatalf("preview %d %s", w.Code, w.Body)
	}
	stale := strings.Replace(body, `"mode":"merge"`, `"mode":"merge","expect":"`+strings.Repeat("0", 64)+`"`, 1)
	if w := send(h, "POST", "/api/v1/writeback/apply", stale); w.Code != 409 || !strings.Contains(w.Body.String(), "changed since the preview") {
		t.Fatalf("a stale preview was applied: %d %s", w.Code, w.Body)
	}
	replace := `{"workspace":"main","base":"export","browser":"Google Chrome","mode":"replace","expect":"` + p.Expect + `"}`
	if w := send(h, "POST", "/api/v1/writeback/apply", replace); w.Code != 400 && w.Code != 502 {
		t.Fatalf("an unconfirmed replace: %d %s", w.Code, w.Body)
	}
	for _, bad := range []string{`{"workspace":"main","base":"export","browser":"Google Chrome","mode":"sync"}`, `{"workspace":"","base":"export","browser":"Google Chrome","mode":"merge"}`} {
		if w := send(h, "POST", "/api/v1/writeback/preview", bad); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	// Only the owner: an extension's token cannot reach these routes.
	if w := fromExtension(h, "POST", "/api/v1/writeback/preview", body, chrome); w.Code != 403 && w.Code != 401 {
		t.Fatalf("an extension reached the preview: %d", w.Code)
	}
}
