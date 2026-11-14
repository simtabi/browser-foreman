package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // Chrome writes its screenshot as PNG
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
)

// Runner runs Chrome; tests replace it.
type Runner func(ctx context.Context, argv []string) error

// Snapper takes page snapshots with Chrome, headless, in a profile made for each page and removed after it, so no
// account, cookie or password of the owner's is ever used.
type Snapper struct {
	Store   Store
	Chrome  string // the browser's executable
	Run     Runner
	Resolve func(ctx context.Context, host string) ([]net.IP, error)
	Timeout time.Duration
	// Width and Height are the window the page is drawn in (0: 1280 by 800); Wait is how long the page may run
	// before the picture (0: 8 seconds). The kept picture is half the window's size.
	Width, Height int
	Wait          time.Duration
}

// RunCommand is the Runner that starts the process.
func RunCommand(ctx context.Context, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(out.Bytes()))
	}
	return nil
}

// ErrNotPublic: snapshots are only taken of pages on the public internet.
var ErrNotPublic = errors.New("not a public web page: only http(s) addresses on the internet are snapped")

// Snap takes one page's snapshot and keeps it as a small JPEG.
func (s Snapper) Snap(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ErrNotPublic
	}
	resolve := s.Resolve
	if resolve == nil {
		resolve = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	ips, err := resolve(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%s does not resolve", u.Hostname())
	}
	for _, ip := range ips {
		if linkcheck.IsPrivate(ip) {
			return ErrNotPublic
		}
	}
	if s.Chrome == "" {
		return errors.New("no Chrome or Chromium browser was found to take snapshots with")
	}
	if s.Store.Dir == "" {
		return errors.New("no folder is set for snapshots")
	}
	dir, err := os.MkdirTemp("", "brofo-snap-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	shot := filepath.Join(dir, "shot.png")
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	w, h, wait := s.Width, s.Height, s.Wait
	if w == 0 || h == 0 {
		w, h = 1280, 800
	}
	if wait == 0 {
		wait = 8 * time.Second
	}
	if timeout < wait+20*time.Second {
		timeout = wait + 20*time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := []string{s.Chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--mute-audio", "--no-first-run",
		"--no-default-browser-check", "--disable-extensions", "--disable-sync", "--incognito",
		"--user-data-dir=" + filepath.Join(dir, "profile"), fmt.Sprintf("--window-size=%d,%d", w, h), fmt.Sprintf("--virtual-time-budget=%d", wait.Milliseconds()),
		// Chrome would resolve the host again, and a hostile name server could then answer with a private address
		// (DNS rebinding): pin the host to the public address just checked.
		"--host-resolver-rules=" + pinRule(u.Hostname(), ips[0]),
		"--screenshot=" + shot, u.String()}
	run := s.Run
	if run == nil {
		run = RunCommand
	}
	if err := run(cctx, argv); err != nil {
		return fmt.Errorf("Chrome could not take it: %w", err)
	}
	f, err := os.Open(shot)
	if err != nil {
		return errors.New("Chrome wrote no picture")
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("Chrome's picture is unreadable: %w", err)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, half(img), &jpeg.Options{Quality: 72}); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Store.dir("thumbs"), 0o700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Store.dir("thumbs"), ThumbKey(raw)+".jpg"), b.Bytes())
}

// half shrinks a picture to half its size, averaging each 2x2 block.
func half(src image.Image) image.Image {
	r := src.Bounds()
	w, h := r.Dx()/2, r.Dy()/2
	if w == 0 || h == 0 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var rs, gs, bs uint32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					cr, cg, cb, _ := src.At(r.Min.X+2*x+dx, r.Min.Y+2*y+dy).RGBA()
					rs, gs, bs = rs+cr, gs+cg, bs+cb
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(rs/4>>8), uint8(gs/4>>8), uint8(bs/4>>8), 255
		}
	}
	return dst
}

// FindChrome returns the executable of an installed Chromium browser to take snapshots with ("" when none):
// paths are the installed apps' locations (macOS: the .app bundle).
func FindChrome(paths []string) string {
	for _, p := range paths {
		for _, c := range []string{
			filepath.Join(p, "Contents", "MacOS", filepath.Base(p[:len(p)-len(filepath.Ext(p))])), p,
		} {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
		}
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	return ""
}

// pinRule maps one host to an address in Chrome's --host-resolver-rules form (an IPv6 address in brackets).
func pinRule(host string, ip net.IP) string {
	a := ip.String()
	if ip.To4() == nil {
		a = "[" + a + "]"
	}
	return "MAP " + host + " " + a
}
