package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostAndNames(t *testing.T) {
	// An address carrying a user and password: both generated, so no credential is written down.
	creds := url.UserPassword(rand.Text()[:8], rand.Text())
	withCreds := (&url.URL{Scheme: "http", User: creds, Host: "docs.go.dev:8080", Path: "/x"}).String()
	for in, want := range map[string]string{
		"https://www.Example.com/a?b#c": "example.com", withCreds: "docs.go.dev", "javascript:x": "",
	} {
		if got := Host(in); got != want {
			t.Errorf("Host(%q) = %q, want %q", in, got, want)
		}
	}
	// The Python tool's names: sha1(host)[:12] for icons, sha1(url)[:16] for snapshots (hashlib.sha1(...).hexdigest()).
	if got := ThumbKey("https://go.dev/"); got != "c8683f51db7e5cbb" {
		t.Fatalf("thumb key %q", got)
	}
	if got := sum("go.dev", 12); got != "56aef353c139" {
		t.Fatalf("icon key %q", got)
	}
}

func pngURI(t *testing.T, n int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = byte(i * 7)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestIconsFromDataURIs(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if ok, err := s.SaveIconURI("go.dev", pngURI(t, 16)); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	name := s.Icon("go.dev")
	if !strings.HasSuffix(name, ".png") || len(name) != 16 {
		t.Fatalf("name %q", name)
	}
	if ok, _ := s.SaveIconURI("go.dev", pngURI(t, 32)); ok {
		t.Fatal("an icon was replaced")
	}
	for _, bad := range []string{"data:image/png;base64,AAAA", "data:text/html;base64," + base64.StdEncoding.EncodeToString(make([]byte, 100)), "https://x/icon.png", "data:image/png,raw"} {
		if ok, _ := s.SaveIconURI("bad.example", bad); ok {
			t.Errorf("kept %q", bad[:20])
		}
	}
	if p, ct, err := s.Path("icons", name); err != nil || ct != "image/png" || filepath.Base(p) != name {
		t.Fatalf("%s %s %v", p, ct, err)
	}
	for _, kind := range [][2]string{{"icons", "../x.png"}, {"thumbs", name}, {"icons", "0123456789ab.exe"}, {"other", name}} {
		if _, _, err := s.Path(kind[0], kind[1]); !errors.Is(err, ErrName) {
			t.Errorf("%v allowed", kind)
		}
	}
}

func fakeChrome(t *testing.T, ran *[]string) Runner {
	return func(_ context.Context, argv []string) error {
		*ran = argv
		for _, a := range argv {
			if p, ok := strings.CutPrefix(a, "--screenshot="); ok {
				img := striped(1280, 800)
				f, _ := os.Create(p)
				defer f.Close()
				return png.Encode(f, img)
			}
		}
		t.Fatal("no --screenshot")
		return nil
	}
}

func TestSnapKeepsASmallJPEG(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	var ran []string
	public := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("203.0.113.10")}, nil }
	sn := Snapper{Store: s, Chrome: "/x/chrome", Run: fakeChrome(t, &ran), Resolve: public}
	if err := sn.Snap(context.Background(), "https://go.dev/"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(ran, " ")
	for _, want := range []string{"--headless=new", "--user-data-dir=", "--incognito", "https://go.dev/", "--window-size=1280,800", "--virtual-time-budget=8000",
		"--host-resolver-rules=MAP go.dev 203.0.113.10"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %s in %s", want, joined)
		}
	}
	if !s.HasThumb("https://go.dev/") {
		t.Fatal("no thumbnail")
	}
	f, _ := os.Open(filepath.Join(s.Dir, "thumbs", ThumbKey("https://go.dev/")+".jpg"))
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil || img.Bounds().Dx() != 640 || img.Bounds().Dy() != 400 {
		t.Fatalf("%v %v", img.Bounds(), err)
	}
	if r, _, _, _ := img.At(10, 10).RGBA(); r>>8 < 180 {
		t.Fatalf("colour lost: %d", r>>8)
	}
	// The owner's snapshot settings reach Chrome: a taller window and a longer wait.
	tall := Snapper{Store: s, Chrome: "/x/chrome", Run: fakeChrome(t, &ran), Resolve: public, Width: 1440, Height: 2400, Wait: 15 * time.Second}
	if err := tall.Snap(context.Background(), "https://go.dev/"); err != nil || !strings.Contains(strings.Join(ran, " "), "--window-size=1440,2400 --virtual-time-budget=15000") {
		t.Fatalf("%v %v", err, ran)
	}
	private := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("192.168.1.10")}, nil }
	for _, u := range []string{"http://router.local/", "file:///etc/passwd", "javascript:alert(1)"} {
		sn.Resolve = private
		ran = nil
		if err := sn.Snap(context.Background(), u); !errors.Is(err, ErrNotPublic) || ran != nil {
			t.Errorf("%s: %v (ran %v)", u, err, ran != nil)
		}
	}
}

func TestIndexAgreesWithTheFolder(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if ok, err := s.SaveIconURI("go.dev", pngURI(t, 16)); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if err := os.MkdirAll(s.dir("thumbs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir("thumbs"), ThumbKey("https://go.dev/")+".jpg"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	x := s.Index()
	if got, want := x.Icon("go.dev"), s.Icon("go.dev"); got == "" || got != want {
		t.Fatalf("index icon %q, folder %q", got, want)
	}
	if x.Icon("example.com") != "" || x.Icon("") != "" {
		t.Fatal("the index found an icon that is not there")
	}
	if !x.HasThumb("https://go.dev/") || x.HasThumb("https://example.com/") {
		t.Fatal("the index disagrees with the snapshots folder")
	}
	if e := (Store{}).Index(); e.Icon("go.dev") != "" || e.HasThumb("https://go.dev/") {
		t.Fatal("a store with no folder held something")
	}
}

func TestPinRuleBracketsIPv6(t *testing.T) {
	if got := pinRule("example.com", net.ParseIP("2606:2800:220:1::1")); got != "MAP example.com [2606:2800:220:1::1]" {
		t.Fatal(got)
	}
}

func TestRenderOpensThePageInAThrowawayProfile(t *testing.T) {
	public := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("203.0.113.10")}, nil }
	var argv []string
	sn := Snapper{Chrome: "/x/chrome", Resolve: public, Dump: func(_ context.Context, a []string) ([]byte, error) {
		argv = a
		return []byte("<html><title>Hi</title></html>"), nil
	}}
	dom, err := sn.Render(context.Background(), "https://site.example/a?b=1")
	got := strings.Join(argv, " ")
	if err != nil || string(dom) != "<html><title>Hi</title></html>" || !strings.Contains(got, "--dump-dom https://site.example/a?b=1") ||
		!strings.Contains(got, "--host-resolver-rules=MAP site.example 203.0.113.10") || !strings.Contains(got, "--user-data-dir=") ||
		!strings.Contains(got, "--incognito") {
		t.Fatalf("%v %q %s", err, dom, got)
	}
	argv = nil
	sn.Resolve = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("10.0.0.7")}, nil }
	if _, err := sn.Render(context.Background(), "https://inside.example/"); err != ErrNotPublic || argv != nil {
		t.Fatalf("a private address was opened: %v %v", err, argv)
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

func TestBlankIsOneColourNearlyEdgeToEdge(t *testing.T) {
	one := image.NewRGBA(image.Rect(0, 0, 640, 400))
	for i := range one.Pix {
		one.Pix[i] = 255
	}
	spinner := image.NewRGBA(image.Rect(0, 0, 640, 400))
	copy(spinner.Pix, one.Pix)
	for y := 190; y < 210; y++ { // a small mark in the middle: still a page that drew nothing
		for x := 310; x < 330; x++ {
			spinner.Set(x, y, color.Black)
		}
	}
	for name, c := range map[string]struct {
		img  image.Image
		want bool
	}{"one colour": {one, true}, "a spinner": {spinner, true}, "a page": {striped(640, 400), false}, "transparent": {image.NewRGBA(image.Rect(0, 0, 40, 20)), true}} {
		if got := Blank(c.img); got != c.want {
			t.Errorf("%s: Blank = %v", name, got)
		}
	}
}

func TestABlankPictureIsTakenAgainAndNeverKept(t *testing.T) {
	public := func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("203.0.113.10")}, nil }
	draw := func(pics ...image.Image) (Runner, *[]string) {
		var waits []string
		return func(_ context.Context, argv []string) error {
			for _, a := range argv {
				if strings.HasPrefix(a, "--virtual-time-budget=") {
					waits = append(waits, a)
				}
			}
			pic := pics[min(len(waits), len(pics))-1]
			for _, a := range argv {
				if p, ok := strings.CutPrefix(a, "--screenshot="); ok {
					f, _ := os.Create(p)
					defer f.Close()
					return png.Encode(f, pic)
				}
			}
			return nil
		}, &waits
	}
	blank := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	s := Store{Dir: t.TempDir()}
	run, waits := draw(blank, striped(1280, 800))
	if err := (Snapper{Store: s, Chrome: "/x/chrome", Run: run, Resolve: public}).Snap(context.Background(), "https://go.dev/"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"--virtual-time-budget=8000", "--virtual-time-budget=24000"}; strings.Join(*waits, " ") != strings.Join(want, " ") || !s.HasThumb("https://go.dev/") {
		t.Fatalf("waits %v, kept %v", *waits, s.HasThumb("https://go.dev/"))
	}
	run, _ = draw(blank, blank)
	if err := (Snapper{Store: s, Chrome: "/x/chrome", Run: run, Resolve: public}).Snap(context.Background(), "https://example.org/"); !errors.Is(err, ErrBlank) || s.HasThumb("https://example.org/") {
		t.Fatalf("%v, kept %v", err, s.HasThumb("https://example.org/"))
	}
}
