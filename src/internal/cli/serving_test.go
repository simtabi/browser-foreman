package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/version"
)

// fake answers health as body says, and returns its address.
func fake(t *testing.T, body string) string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(s.Close)
	return strings.TrimPrefix(s.URL, "http://")
}

func health(v string, pid int) string {
	return fmt.Sprintf(`{"ok":true,"service":"browser-foreman","version":%q,"pid":%d}`, v, pid)
}

// freeAddr is a loopback address nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	l.Close()
	return a
}

func TestProbeTellsBrowserForemanFromOtherPrograms(t *testing.T) {
	ctx := context.Background()
	if o := probe(ctx, fake(t, health("v9", 42))); !o.Ours || o.Version != "v9" || o.PID != 42 {
		t.Errorf("a current build: %+v", o)
	}
	if o := probe(ctx, fake(t, `{"ok":true}`)); !o.Ours || o.Version != "" {
		t.Errorf("an older build: %+v", o)
	}
	if o := probe(ctx, fake(t, `<!doctype html><title>Grafana</title>`)); !o.Up || o.Ours {
		t.Errorf("another program: %+v", o)
	}
	if o := probe(ctx, fake(t, `{"ok":true,"service":"something-else"}`)); o.Ours {
		t.Errorf("another service's health: %+v", o)
	}
	if o := probe(ctx, freeAddr(t)); o.Up {
		t.Errorf("nothing: %+v", o)
	}
}

func cfgAt(t *testing.T, addr string) config.Config {
	c := config.Defaults()
	c.Server.Addr, c.Storage.Path = addr, t.TempDir()
	return c
}

func TestBindSaysTheSameBuildIsAlreadyRunning(t *testing.T) {
	var out bytes.Buffer
	addr := fake(t, health(version.Version, 4242))
	l, err := bind(context.Background(), cfgAt(t, addr), false, &out)
	if !errors.Is(err, errAlreadyRunning) || l != nil || !strings.Contains(out.String(), "already running at http://"+addr) || !strings.Contains(out.String(), "process 4242") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestBindRefusesAnotherBuildUnlessReplacing(t *testing.T) {
	var out bytes.Buffer
	for _, body := range []string{health("old-build", 4242), `{"ok":true}`} {
		_, err := bind(context.Background(), cfgAt(t, fake(t, body)), false, &out)
		if err == nil || !strings.Contains(err.Error(), "--replace") || !strings.Contains(err.Error(), "browser-foreman stop") {
			t.Errorf("%s: %v", body, err)
		}
	}
}

func TestBindMovesPastAnotherProgram(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0") // another program: it holds the port and answers nothing
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	var out bytes.Buffer
	l, err := bind(context.Background(), cfgAt(t, held.Addr().String()), false, &out)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, want, _ := net.SplitHostPort(held.Addr().String())
	if _, got, _ := net.SplitHostPort(l.Addr().String()); got == want || !strings.Contains(out.String(), "Port "+want+" is taken by") || !strings.Contains(out.String(), "uses "+l.Addr().String()) {
		t.Fatalf("listening on %s: %q", l.Addr(), out.String())
	}
}

func TestBindListensWhenThePortIsFree(t *testing.T) {
	addr := freeAddr(t)
	l, err := bind(context.Background(), cfgAt(t, addr), false, &bytes.Buffer{})
	if err != nil || l.Addr().String() != addr {
		t.Fatalf("%v %v", l, err)
	}
	l.Close()
}

func TestTheRecordedAddressLeadsToTheServer(t *testing.T) {
	ctx := context.Background()
	c := cfgAt(t, "127.0.0.1:8765")
	if got := serverAddr(ctx, c); got != c.Server.Addr {
		t.Fatalf("no record: %s", got)
	}
	moved := fake(t, health("v9", 1))
	if err := writeRuntime(c, moved, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := serverAddr(ctx, c); got != moved {
		t.Fatalf("recorded fallback: %s, want %s", got, moved)
	}
	if err := writeRuntime(c, freeAddr(t), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := serverAddr(ctx, c); got != c.Server.Addr {
		t.Fatalf("a stale record was followed: %s", got)
	}
	removeRuntime(c)
	if _, ok := readRuntime(c); ok {
		t.Fatal("this process's record was not removed")
	}
	other := runtimeInfo{Addr: moved, PID: os.Getpid() + 1}
	b := fmt.Sprintf(`{"addr":%q,"pid":%d}`, other.Addr, other.PID)
	if err := os.WriteFile(runtimePath(c), []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	removeRuntime(c)
	if _, ok := readRuntime(c); !ok {
		t.Fatal("another process's record was removed")
	}
}

func TestStopSignalsOnlyBrowserForeman(t *testing.T) {
	// The fake says it is this test process, which is not browser-foreman: stop must refuse to signal it.
	err := stopServer(context.Background(), fake(t, health("v9", os.Getpid())), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not browser-foreman") {
		t.Fatalf("%v", err)
	}
	if err := stopServer(context.Background(), fake(t, `<html>`), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "no Browser Foreman") {
		t.Fatalf("another program: %v", err)
	}
}

func TestServeOpensThePageWhenItShould(t *testing.T) {
	cases := []struct {
		setting            string
		open, noOpen, term bool
		want               bool
	}{
		{"auto", false, false, true, true},   // started from a terminal
		{"auto", false, false, false, false}, // a service or a script: never a browser popping up
		{"never", false, false, true, false},
		{"always", false, false, false, true},
		{"never", true, false, false, true},  // --open wins
		{"always", false, true, true, false}, // --no-open wins
		{"auto", true, true, true, false},    // both: --no-open wins
	}
	for _, c := range cases {
		if got := shouldOpen(c.setting, c.open, c.noOpen, c.term); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
	var out bytes.Buffer
	showPage(context.Background(), &out, "http://127.0.0.1:8765/open/abc", false) // false: tests never open a browser
	if out.String() != "Open Browser Foreman: http://127.0.0.1:8765/open/abc\n" {
		t.Fatalf("%q", out.String())
	}
}
