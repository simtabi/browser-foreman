package api

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// With a page open (its event stream is a request that never ends by itself), stopping the server returns at once
// and cleanly; it used to wait the whole 5 s and then answer "context deadline exceeded".
func TestShutdownDoesNotWaitForTheEventStream(t *testing.T) {
	s := newServer(t, "tok")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.Listener = ln
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/api/v1/events", nil)
	req.Host = host
	req.Header.Set("Authorization", "Bearer tok")
	var resp *http.Response
	for range 50 {
		if resp, err = http.DefaultClient.Do(req); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("the event stream: %v %v", err, resp)
	}
	go func() { _, _ = bufio.NewReader(resp.Body).ReadString(0) }() // a page keeps reading
	defer resp.Body.Close()
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil || time.Since(start) > 2*time.Second {
			t.Fatalf("stopped after %v with %v", time.Since(start), err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Run did not stop")
	}
}
