// Package notify gives Browser Foreman its desktop notifications through go-notifykit, so a banner carries its own
// name and icon and nothing reaches the screen under `go test` or in CI.
package notify

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"sync"
	"time"

	notifykit "github.com/simtabi/go-notifykit/src"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// AppID is what macOS lists the notifications under (System Settings › Notifications), with ".notifier" appended.
const AppID = "com.simtabi.browser-foreman"

// New is the notifier every part of Browser Foreman shares: the sweep's banner, `browser-foreman notify` and the
// listener serve runs. notify.desktop: false turns it off; BROFO_NOTIFY_DESKTOP and NOTIFYKIT_DESKTOP do too. log
// receives one line per fallback or failure (serve's output, so a login service's log shows them); nil drops them.
func New(c config.Config, log io.Writer) (*notifykit.Notifier, error) {
	return notifykit.New(notifykit.Config{
		AppName:     "browser-foreman",
		AppID:       AppID,
		Icon:        Icon(),
		EnvPrefix:   "BROFO",
		Disabled:    !c.Notify.Desktop,
		MinInterval: 30 * time.Second,
		Log:         log,
	})
}

// maxSocket is the longest socket path go-notifykit's Listen accepts (Unix sockets allow 104 bytes on macOS).
const maxSocket = 100

// ListenProblem says why the listener cannot start, in terms the user can act on, or "" when it can. Banners show
// either way; only `browser-foreman notify ping` and scripts need the listener.
func ListenProblem(n *notifykit.Notifier) string {
	ep, err := n.Endpoint()
	if err != nil {
		return err.Error()
	}
	if len(ep.Socket) > maxSocket {
		return fmt.Sprintf("its socket path %s is %d bytes, longer than the %d a Unix socket allows; set XDG_STATE_HOME "+
			"to a shorter folder. Banners still show; `browser-foreman notify ping` cannot reach this server", ep.Socket, len(ep.Socket), maxSocket)
	}
	return ""
}

// Sweep is the one banner for a scheduled re-check, or false when it found nothing new. addr is where the page is,
// which a click opens.
func Sweep(workspace string, broken, fixed int, addr string) (notifykit.Notification, bool) {
	if broken == 0 && fixed == 0 {
		return notifykit.Notification{}, false
	}
	links := func(n int) string {
		if n == 1 {
			return "1 link"
		}
		return fmt.Sprintf("%d links", n)
	}
	works := func(n int) string {
		if n == 1 {
			return "1 link works again"
		}
		return links(n) + " work again"
	}
	var title, body string
	switch {
	case broken > 0:
		title = links(broken) + " stopped working"
		body = "In " + workspace + "."
		if fixed > 0 {
			body = fmt.Sprintf("In %s; %s.", workspace, works(fixed))
		}
	default:
		title = works(fixed)
		body = "In " + workspace + "."
	}
	level := notifykit.Info
	if broken > 0 {
		level = notifykit.Warn
	}
	n := notifykit.Notification{Title: title, Body: body + " Open Browser Foreman to see them.", Group: "linkcheck", Level: level}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
			n.Open = "http://" + addr + "/" // only the page on this computer: go-notifykit refuses anything else
		}
	}
	return n, true
}

var (
	iconOnce sync.Once
	iconPNG  []byte
)

// Icon is the page's logo mark (a white bookmark on a blue rounded square) as a 1024px PNG.
func Icon() []byte {
	iconOnce.Do(func() {
		var b bytes.Buffer
		_ = png.Encode(&b, drawMark(1024))
		iconPNG = b.Bytes()
	})
	return iconPNG
}

// The mark in the favicon's 64-unit coordinates (web/src/template.html): a 64x64 square with corners of 14, and the
// bookmark M22 14h20a4 4 0 0 1 4 4v32l-14-9-14 9V18a4 4 0 0 1 4-4z.
var blue = color.NRGBA{0x06, 0x6f, 0xd1, 0xff}

func inRounded(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx, cy := x, y
	switch {
	case x < x0+r:
		cx = x0 + r
	case x > x1-r:
		cx = x1 - r
	}
	switch {
	case y < y0+r:
		cy = y0 + r
	case y > y1-r:
		cy = y1 - r
	}
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
}

func inBookmark(x, y float64) bool {
	// the body down to the notch, its top corners rounded by 4 (the rectangle runs on to 54 so only the top rounds)
	if !inRounded(x, y, 18, 14, 46, 54, 4) || y > 50 {
		return false
	}
	// the notch: below the lines from (18,50) and (46,50) up to (32,41)
	if y > 41 {
		edge := 41 + (50-41)*abs(x-32)/14
		return y <= edge
	}
	return true
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// drawMark renders the mark at size x size, 4x4 supersampled.
func drawMark(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	unit := 64.0 / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var bg, white int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) * unit
					y := (float64(py) + (float64(sy)+0.5)/ss) * unit
					if !inRounded(x, y, 0, 0, 64, 64, 14) {
						continue
					}
					if inBookmark(x, y) {
						white++
					} else {
						bg++
					}
				}
			}
			cover := bg + white
			if cover == 0 {
				continue
			}
			mix := func(c uint8) uint8 { return uint8((int(c)*bg + 255*white) / cover) }
			img.SetNRGBA(px, py, color.NRGBA{mix(blue.R), mix(blue.G), mix(blue.B), uint8(255 * cover / (ss * ss))})
		}
	}
	return img
}
