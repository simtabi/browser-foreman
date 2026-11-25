package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/media"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func iconURI(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 3)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestIconsSnapshotsAndTheirRoutes(t *testing.T) {
	s := withUI(seeded(t))
	page := signedIn(t, s) // the page's own <img> loads carry its sign-in
	s.Media = media.Store{Dir: t.TempDir()}
	shots := 0
	s.Snapper = &media.Snapper{Store: s.Media, Chrome: "/x/chrome",
		Resolve: func(_ context.Context, host string) ([]net.IP, error) {
			if host == "intranet.example" {
				return []net.IP{net.ParseIP("10.0.0.2")}, nil
			}
			return []net.IP{net.ParseIP("203.0.113.10")}, nil
		},
		Run: func(_ context.Context, argv []string) error {
			shots++
			for _, a := range argv {
				if p, ok := strings.CutPrefix(a, "--screenshot="); ok {
					f, _ := os.Create(p)
					defer f.Close()
					return png.Encode(f, striped(80, 40))
				}
			}
			return nil
		}}
	h := s.Handler()
	// Icons come with an imported export; the page names them.
	file := `<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><p><DT><A HREF="https://www.go.dev/" ICON="` + iconURI(t) + `">Go</A>
<DT><A HREF="http://intranet.example/">Intranet</A></DL><p>`
	_ = send(h, "POST", "/api/v1/workspaces", `{"id":"pics"}`)
	if w := send(h, "POST", "/api/v1/workspaces/pics/import", file); w.Code != 201 {
		t.Fatalf("import %d %s", w.Code, w.Body)
	}
	icon := s.Media.Icon("go.dev")
	if icon == "" {
		t.Fatal("the icon in the export was not kept")
	}
	if w := get(h, "/favicons/"+icon, page); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("icon %d %v", w.Code, w.Header())
	}
	for _, bad := range []string{"/favicons/..%2Fx.png", "/favicons/nothere.png", "/thumbs/" + icon} {
		if w := get(h, bad, page); w.Code != 404 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}

	// Snapshots: public pages only, in the background.
	w := send(h, "POST", "/api/v1/snapshots", `{"workspace":"pics"}`)
	var started struct {
		Job struct {
			ID    string
			Total int
		}
	}
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	if w.Code != 202 || started.Job.Total != 2 {
		t.Fatalf("start %d %s", w.Code, w.Body)
	}
	var job struct {
		Job struct {
			State                 string
			Done, Skipped, Failed int
		}
	}
	for i := 0; i < 100; i++ {
		_ = json.Unmarshal(send(h, "GET", "/api/v1/snapshots/"+started.Job.ID, "").Body.Bytes(), &job)
		if job.Job.State != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Job.State != "done" || job.Job.Done != 1 || job.Job.Skipped != 1 || shots != 1 {
		t.Fatalf("job %+v, browser started %d times (the intranet page must never be visited)", job.Job, shots)
	}
	if !s.Media.HasThumb("https://www.go.dev/") {
		t.Fatal("no snapshot kept")
	}
	if w := get(h, "/thumbs/"+media.ThumbKey("https://www.go.dev/")+".jpg", page); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb %d", w.Code)
	}
	// Without force, pages that have one are not taken again: a small one is looked at in the background and kept
	// when it is not blank.
	before := shots
	w = send(h, "POST", "/api/v1/snapshots", `{"workspace":"pics"}`)
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	if w.Code != 202 || started.Job.Total != 2 {
		t.Fatalf("second run %s", w.Body)
	}
	var kept struct {
		Job struct {
			State      string
			Kept, Done int
		}
	}
	for i := 0; i < 100 && kept.Job.State != "done"; i++ {
		time.Sleep(20 * time.Millisecond)
		_ = json.Unmarshal(send(h, "GET", "/api/v1/snapshots/"+started.Job.ID, "").Body.Bytes(), &kept)
	}
	if kept.Job.Kept != 1 || kept.Job.Done != 0 || shots != before {
		t.Fatalf("second run %+v, browser started %d more times", kept.Job, shots-before)
	}
	// A blank one is taken again.
	blank := filepath.Join(s.Media.Dir, "thumbs", media.ThumbKey("https://www.go.dev/")+".jpg")
	var bb bytes.Buffer
	_ = jpeg.Encode(&bb, image.NewRGBA(image.Rect(0, 0, 40, 20)), nil)
	if err := os.WriteFile(blank, bb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(send(h, "POST", "/api/v1/snapshots", `{"workspace":"pics"}`).Body.Bytes(), &started)
	kept.Job.State = ""
	for i := 0; i < 100 && kept.Job.State != "done"; i++ {
		time.Sleep(20 * time.Millisecond)
		_ = json.Unmarshal(send(h, "GET", "/api/v1/snapshots/"+started.Job.ID, "").Body.Bytes(), &kept)
	}
	if kept.Job.Done != 1 || kept.Job.Kept != 0 || shots != before+1 {
		t.Fatalf("blank one %+v, browser started %d more times", kept.Job, shots-before)
	}
	// The page is told which pictures there are.
	if body := get(h, "/?ws=pics", signedIn(t, s)).Body.String(); !strings.Contains(body, `"icons":{"go.dev":"`+icon+`"}`) || !strings.Contains(body, media.ThumbKey("https://www.go.dev/")) {
		t.Fatalf("boot media missing: %s", body[strings.Index(body, `"media"`):][:200])
	}
	// A folder chosen in Preferences is where pictures are then read from: the snapshot in the default folder is no
	// longer served, and one placed in the chosen folder is.
	key := media.ThumbKey("https://www.go.dev/") + ".jpg"
	other := t.TempDir()
	if w := send(h, "PUT", "/api/v1/preferences", `{"snapshot_dir":`+strconv.Quote(other)+`}`); w.Code != 200 {
		t.Fatalf("set the folder: %d %s", w.Code, w.Body)
	}
	if w := get(h, "/thumbs/"+key, page); w.Code != 404 {
		t.Fatalf("still served from the default folder: %d", w.Code)
	}
	b, _ := os.ReadFile(filepath.Join(s.Media.Dir, "thumbs", key))
	_ = os.MkdirAll(filepath.Join(other, "thumbs"), 0o700)
	_ = os.WriteFile(filepath.Join(other, "thumbs", key), b, 0o600)
	if w := get(h, "/thumbs/"+key, page); w.Code != 200 {
		t.Fatalf("not served from the chosen folder: %d", w.Code)
	}
	if w := send(h, "PUT", "/api/v1/preferences", `{"snapshot_dir":"relative/x"}`); w.Code != 400 {
		t.Fatalf("a relative folder was accepted: %d", w.Code)
	}
	_ = send(h, "PUT", "/api/v1/preferences", `{"snapshot_dir":""}`)
	// In accounts mode the pictures need a signed-in person.
	s.Cfg.Server.Mode = "accounts"
	if w := get(h, "/favicons/"+icon, nil); w.Code != 401 {
		t.Fatalf("anonymous icon in accounts mode: %d", w.Code)
	}
	p := pw(t)
	addUser(t, s, "vi", store.RoleViewer, p)
	_, vi := signIn(t, h, "vi", p)
	if w := vi.do(h, "GET", "/favicons/"+icon, "", false); w.Code != 200 {
		t.Fatalf("signed-in icon: %d", w.Code)
	}
}

// striped is a picture that is not blank: red, with a white band every 64 pixels. Tests use it as what Chrome draws.
func striped(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{200, 10, 10, 255}
			if x%64 >= 48 {
				c = color.RGBA{250, 250, 250, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}
