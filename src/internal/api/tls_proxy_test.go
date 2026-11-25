package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// selfSigned writes a certificate and key made for this run (never a fixed key) and returns their paths.
func selfSigned(t *testing.T) (certFile, keyFile string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	_ = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return certFile, keyFile
}

// With a certificate the server speaks https, so nothing crosses the network in clear text.
func TestTheServerServesHTTPSWithACertificate(t *testing.T) {
	cert, key := selfSigned(t)
	s := newServer(t, "tok")
	s.Cfg.Server.TLSCert, s.Cfg.Server.TLSKey = cert, key
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Listener = ln
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	defer func() { cancel(); <-done }()
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // our own throwaway cert
	var resp *http.Response
	for range 50 {
		if resp, err = c.Get("https://" + ln.Addr().String() + "/api/v1/health"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || resp.StatusCode != 200 || resp.TLS == nil {
		t.Fatalf("https health: %v %+v", err, resp)
	}
	resp.Body.Close()
}

// X-Forwarded-* is believed only from a listed proxy: from anyone else it is a header any client can set.
func TestOnlyATrustedProxyIsBelieved(t *testing.T) {
	s := newServer(t, "tok")
	s.Cfg.Server.TrustedProxy = []string{"10.0.0.0/8"}
	req := func(remote string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Add("X-Forwarded-For", "203.0.113.50, 198.51.100.7")
		return r
	}
	if r := req("10.1.2.3:4000"); !s.secure(r) || s.clientKey(r) != "198.51.100.7" {
		t.Fatalf("through the proxy: secure %v, client %q", s.secure(r), s.clientKey(r))
	}
	if r := req("192.0.2.9:4000"); s.secure(r) || s.clientKey(r) != "192.0.2.9" {
		t.Fatalf("a spoofed header from elsewhere was believed: secure %v, client %q", s.secure(r), s.clientKey(r))
	}
}

// accounts mode beyond this computer, with nothing encrypting the traffic, says so when the server starts.
func TestPlainHTTPForOtherComputersIsWarnedAbout(t *testing.T) {
	s := newServer(t, "tok")
	s.Cfg.Server.Mode, s.Cfg.Server.Addr = config.ModeAccounts, "0.0.0.0:8765"
	if !strings.Contains(s.clearTextWarning(), "clear text") {
		t.Fatal("no warning for accounts mode on 0.0.0.0 over plain http")
	}
	for _, fix := range []func(){
		func() { s.Cfg.Server.TrustedProxy = []string{"10.0.0.1"} },
		func() { s.Cfg.Server.TrustedProxy, s.Cfg.Server.Addr = nil, "127.0.0.1:8765" },
	} {
		fix()
		if w := s.clearTextWarning(); w != "" {
			t.Fatalf("warned anyway: %s", w)
		}
	}
}
