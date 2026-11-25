package media

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func throughProxy(t *testing.T, e *egress) *http.Client {
	t.Helper()
	p, _ := url.Parse("http://" + e.addr())
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(p)}}
}

// A page in a snapshot may ask for anything; the proxy reaches only public addresses, plain or tunnelled.
func TestTheSnapshotProxyRefusesPrivateAddresses(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "router admin") }))
	defer inside.Close()
	e, err := startEgress(false)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	c := throughProxy(t, e)
	// Plain http, by address and by a name that resolves to loopback.
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(inside.URL, "http://"))
	for _, u := range []string{inside.URL + "/", "http://localhost:" + port + "/"} {
		resp, err := c.Get(u)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "router admin") {
				t.Fatalf("%s through the proxy: %d %s", u, resp.StatusCode, body)
			}
		}
	}
	// CONNECT, as https and websockets use.
	conn, err := net.Dial("tcp", e.addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "CONNECT "+strings.TrimPrefix(inside.URL, "http://")+" HTTP/1.1\r\nHost: x\r\n\r\n")
	line := make([]byte, 64)
	n, _ := conn.Read(line)
	if !strings.Contains(string(line[:n]), "502") {
		t.Fatalf("CONNECT to a private address: %q", line[:n])
	}
}

// With private addresses allowed (tests serving pages from loopback), the proxy passes requests and tunnels through.
func TestTheSnapshotProxyPassesWhatItAllows(t *testing.T) {
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "public page") }))
	defer site.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "plain page") }))
	defer plain.Close()
	e, err := startEgress(true)
	if err != nil {
		t.Fatal(err)
	}
	defer e.close()
	p, _ := url.Parse("http://" + e.addr())
	tr := site.Client().Transport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(p)
	for u, want := range map[string]string{site.URL: "public page", plain.URL: "plain page"} {
		resp, err := (&http.Client{Timeout: 5 * time.Second, Transport: tr}).Get(u)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != want {
			t.Fatalf("%s: %q", u, body)
		}
	}
}
