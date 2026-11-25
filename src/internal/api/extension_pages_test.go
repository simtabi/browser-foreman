package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/media"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func dataURL(t *testing.T, img image.Image) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

// A page saved from the extension's popup is added to its browser's own folder, with its picture, and nothing else
// in the workspace changes; an address already there is not added twice.
func TestAnExtensionSavesThePageItIsOn(t *testing.T) {
	s := seeded(t)
	s.Media = media.Store{Dir: t.TempDir()}
	h := s.Handler()
	tok := paired(t, s, h, "Google Chrome")
	page := image.NewRGBA(image.Rect(0, 0, 200, 120))
	for y := range 120 {
		for x := range 200 {
			c := color.RGBA{20, 120, 200, 255}
			if x%40 > 30 {
				c = color.RGBA{250, 250, 250, 255}
			}
			page.Set(x, y, c)
		}
	}
	save := func(url, title, pic string) (int, map[string]any) {
		b, _ := json.Marshal(map[string]string{"workspace": "main", "url": url, "title": title, "picture": pic})
		w := fromExtension(h, "POST", "/api/v1/extension/pages", string(b), tok)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	if code, out := save("https://pkg.go.dev/net/http", "http package", dataURL(t, page)); code != 201 {
		t.Fatalf("save %d %v", code, out)
	}
	tr, _, _ := s.Data.State(context.Background(), "main", 0)
	last := tr.Root.Children[len(tr.Root.Children)-1]
	if len(tr.Root.Children) != 3 || last.Title != "Saved from Google Chrome" || len(last.Children) != 1 ||
		last.Children[0].URL != "https://pkg.go.dev/net/http" || last.Children[0].Title != "http package" {
		t.Fatalf("tree %+v", last)
	}
	if !s.Media.HasThumb("https://pkg.go.dev/net/http") {
		t.Fatal("the picture was not kept")
	}
	// The second save goes into the same folder; an address the workspace holds anywhere is not added again.
	if code, _ := save("https://go.dev/doc/", "", ""); code != 201 {
		t.Fatal(code)
	}
	if code, out := save("https://go.dev/", "Go", ""); code != 200 || out["already"] != true || out["node"] != "go" {
		t.Fatalf("already: %d %v", code, out)
	}
	tr, _, _ = s.Data.State(context.Background(), "main", 0)
	if saved := tr.Root.Children[2]; len(tr.Root.Children) != 3 || len(saved.Children) != 2 || saved.Children[1].Title != "go.dev" {
		t.Fatalf("second save %+v", saved)
	}
	// Refused: not a web page, a picture that is not one, a missing workspace, and anyone but an extension.
	for _, c := range []struct {
		url, pic string
		want     int
	}{{"file:///etc/passwd", "", 400}, {"javascript:alert(1)", "", 400}, {"https://a.example/", "data:text/html;base64,PGI+", 400},
		{"https://a.example/", "data:image/png;base64,bm90IGFuIGltYWdl", 400}} {
		if code, out := save(c.url, "", c.pic); code != c.want {
			t.Errorf("%s %.20s: %d %v", c.url, c.pic, code, out)
		}
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/pages", `{"workspace":"nope","url":"https://a.example/"}`, tok); w.Code != 404 {
		t.Errorf("missing workspace: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/extension/pages", `{"workspace":"main","url":"https://a.example/"}`); w.Code != 401 {
		t.Errorf("the owner's own token is not an extension: %d", w.Code)
	}
	// A snapshot already kept for the page is not replaced by the extension's picture.
	thumb := filepath.Join(s.Media.Dir, "thumbs", media.ThumbKey("https://pkg.go.dev/net/http")+".jpg")
	before, _ := os.ReadFile(thumb)
	other := image.NewRGBA(image.Rect(0, 0, 200, 120))
	for i := range other.Pix {
		other.Pix[i] = byte(i * 7)
	}
	if code, _ := save("https://pkg.go.dev/net/http", "again", dataURL(t, other)); code != 200 {
		t.Fatal(code)
	}
	if after, _ := os.ReadFile(thumb); !bytes.Equal(before, after) {
		t.Error("the kept snapshot was replaced")
	}
	// A save the workspace refuses keeps no picture.
	x, _ := s.Data.Workspace(context.Background(), "main")
	x.Locked = true
	if err := s.Data.UpdateWorkspace(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if code, _ := save("https://locked.example/", "", dataURL(t, page)); code != 423 || s.Media.HasThumb("https://locked.example/") {
		t.Errorf("locked: %d, picture kept %v", code, s.Media.HasThumb("https://locked.example/"))
	}
}

// The toolbar badge asks about one address: its status, and nothing about where it is bookmarked.
func TestAnExtensionAsksAboutTheLinkItIsOn(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	tok := paired(t, s, h, "Firefox")
	_ = s.Data.SaveLinks(context.Background(), []store.LinkResult{{URL: "https://gone.example/", Status: "dead", Confirmed: true, Checked: time.Now(), Archive: "https://web.archive.org/x"}})
	w := fromExtension(h, "GET", "/api/v1/extension/link?url="+"https%3A%2F%2Fgone.example%2F", "", tok)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"dead"`) || !strings.Contains(w.Body.String(), `"confirmed":true`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "GET", "/api/v1/extension/link?url=https%3A%2F%2Fnew.example%2F", "", tok); !strings.Contains(w.Body.String(), `"status":"unchecked"`) {
		t.Fatalf("%s", w.Body)
	}
	if w := send(h, "GET", "/api/v1/extension/link?url=https%3A%2F%2Fgone.example%2F", ""); w.Code != 401 {
		t.Fatalf("not an extension: %d", w.Code)
	}
}
