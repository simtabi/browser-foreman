package media

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/linkcheck"
)

// errPrivate is a connection the egress proxy refused because the address is not on the public internet.
var errPrivate = errors.New("refused: not an address on the public internet")

// egress is a proxy on loopback that every request of one headless Chrome visit goes through. Checking the
// bookmark's own host is not enough: the page can redirect, or load images, scripts and frames from anywhere, the
// router and this computer included. The proxy connects only to public addresses, checked when it connects, so a
// name that resolves to a private address (or changes its answer midway) is refused too.
type egress struct {
	ln       net.Listener
	srv      *http.Server
	dial     func(ctx context.Context, network, addr string) (net.Conn, error)
	upstream *http.Transport
	wg       sync.WaitGroup
}

// startEgress listens on a free loopback port. allowPrivate is for tests that serve pages from loopback.
func startEgress(allowPrivate bool) (*egress, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: 15 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip != nil && linkcheck.IsPrivate(ip) && !allowPrivate {
			return errPrivate
		}
		return nil
	}}
	e := &egress{ln: ln, dial: d.DialContext}
	e.upstream = &http.Transport{DialContext: e.dial, Proxy: nil, ResponseHeaderTimeout: 30 * time.Second, DisableCompression: true}
	e.srv = &http.Server{Handler: e, ReadHeaderTimeout: 10 * time.Second}
	e.wg.Add(1)
	go func() { defer e.wg.Done(); _ = e.srv.Serve(ln) }()
	return e, nil
}

// addr is host:port, for Chrome's --proxy-server.
func (e *egress) addr() string { return e.ln.Addr().String() }

func (e *egress) close() {
	_ = e.srv.Close()
	e.upstream.CloseIdleConnections()
	e.wg.Wait()
}

func (e *egress) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		e.tunnel(w, r)
		return
	}
	if r.URL.Scheme != "http" || r.URL.Host == "" { // a proxy is only ever asked for absolute http:// addresses
		http.Error(w, "not a proxy request", http.StatusBadRequest)
		return
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Proxy-Connection")
	resp, err := e.upstream.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// tunnel serves CONNECT (https and websockets): the address is checked when dialling, then bytes are copied.
func (e *egress) tunnel(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Host, ":") {
		http.Error(w, "CONNECT needs host:port", http.StatusBadRequest)
		return
	}
	up, err := e.dial(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		http.Error(w, "cannot tunnel", http.StatusInternalServerError)
		return
	}
	down, buf, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	_, _ = down.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	if n := buf.Reader.Buffered(); n > 0 { // bytes the client sent right after CONNECT
		b, _ := buf.Peek(n)
		_, _ = up.Write(b)
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, down); done <- struct{}{} }()
	go func() { _, _ = io.Copy(down, up); done <- struct{}{} }()
	<-done
	up.Close()
	down.Close()
}

// chromeEgressArgs send every Chrome request through the proxy, loopback included (<-loopback> undoes Chrome's own
// exception for it), and switch off what would go around it: QUIC and WebRTC's direct UDP.
func chromeEgressArgs(addr string) []string {
	return []string{"--proxy-server=http://" + addr, "--proxy-bypass-list=<-loopback>", "--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp"}
}
