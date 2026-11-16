package notify

import (
	"bytes"
	"image/png"
	"testing"

	notifykit "github.com/simtabi/go-notifykit/src"
)

func TestSweepSaysWhatChanged(t *testing.T) {
	if _, ok := Sweep("Work", 0, 0, "127.0.0.1:8765"); ok {
		t.Fatal("a banner for a sweep that found nothing")
	}
	cases := []struct {
		broken, fixed int
		title, body   string
		level         notifykit.Level
	}{
		{9, 0, "9 links stopped working", "In Work. Open Browser Foreman to see them.", notifykit.Warn},
		{0, 3, "3 links work again", "In Work. Open Browser Foreman to see them.", notifykit.Info},
		{1, 2, "1 link stopped working", "In Work; 2 links work again. Open Browser Foreman to see them.", notifykit.Warn},
		{0, 1, "1 link works again", "In Work. Open Browser Foreman to see them.", notifykit.Info},
	}
	for _, c := range cases {
		n, ok := Sweep("Work", c.broken, c.fixed, "127.0.0.1:8765")
		if !ok || n.Title != c.title || n.Body != c.body || n.Level != c.level || n.Group != "linkcheck" || n.Open != "http://127.0.0.1:8765/" {
			t.Errorf("%+v: %+v", c, n)
		}
	}
	if n, _ := Sweep("Work", 1, 0, "192.168.1.5:8765"); n.Open != "" {
		t.Errorf("a click would open a page on another computer: %q", n.Open)
	}
}

func TestTheIconIsTheMark(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(Icon()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("%v", b)
	}
	at := func(x, y int) (uint32, uint32, uint32, uint32) { return img.At(x, y).RGBA() }
	if _, _, _, a := at(2, 2); a != 0 {
		t.Error("the rounded corner is filled")
	}
	if r, g, b, _ := at(100, 512); r>>8 != 0x06 || g>>8 != 0x6f || b>>8 != 0xd1 {
		t.Errorf("the background is %x %x %x", r>>8, g>>8, b>>8)
	}
	if r, g, b, _ := at(512, 400); r>>8 != 0xff || g>>8 != 0xff || b>>8 != 0xff {
		t.Error("the bookmark is not white")
	}
	if _, g, _, _ := at(512, 760); g>>8 != 0x6f {
		t.Error("the notch is filled") // (32, 47.5): below the notch's apex at 41
	}
}
