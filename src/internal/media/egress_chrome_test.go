package media

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Drives a real headless Chrome through the egress proxy: a public page still renders, and a page that asks this
// computer for an image gets nothing from it. It starts Chrome and needs the internet, so it runs only when asked:
//
//	BROFO_CHROME_E2E=1 go -C src test -run TestRealChrome ./internal/media/
func TestRealChromeReachesOnlyPublicAddresses(t *testing.T) {
	if os.Getenv("BROFO_CHROME_E2E") == "" {
		t.Skip("set BROFO_CHROME_E2E=1 to drive a real Chrome")
	}
	chrome := FindChrome([]string{"/Applications/Google Chrome.app"})
	if chrome == "" {
		t.Skip("no Chrome installed")
	}
	var hits atomic.Int32
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer inside.Close()
	s := Snapper{Chrome: chrome, Wait: 3 * time.Second}
	ctx := context.Background()

	html, err := s.Render(ctx, "https://example.com/")
	if err != nil || !strings.Contains(string(html), "Example Domain") {
		t.Fatalf("a public page did not render through the proxy: %v %.200s", err, html)
	}

	// Every request goes through the proxy, this computer's own addresses included (Chrome would otherwise send
	// loopback directly), and the proxy refuses them. Without the proxy the same page reaches the server: the control
	// that proves the test can fail.
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(inside.URL, "http://"))
	page := "http://localhost:" + port + "/router-admin"
	e, err := startEgress(false)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	open := func(extra []string) int32 {
		hits.Store(0)
		args := append([]string{chrome, "--headless=new", "--disable-gpu", "--no-first-run", "--user-data-dir=" + t.TempDir(),
			"--virtual-time-budget=3000"}, extra...)
		cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		defer cancel()
		if _, err := DumpCommand(cctx, append(args, "--dump-dom", page)); err != nil {
			t.Fatal(err)
		}
		return hits.Load()
	}
	if n := open(nil); n == 0 {
		t.Fatal("control: without the proxy the page did not ask this computer for anything, so the test proves nothing")
	}
	if n := open(chromeEgressArgs(e.addr())); n != 0 {
		t.Fatalf("Chrome reached this computer %d times through the proxy", n)
	}
}
