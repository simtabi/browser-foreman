package cli

import (
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// `browser-foreman open` asks the running server for a one-time link with the token from .env.
func TestPageLinkAsksTheServerWithTheToken(t *testing.T) {
	tok := rand.Text()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/local/links" || r.Header.Get("Authorization") != "Bearer "+tok {
			http.Error(w, `{"error":"no"}`, http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"url":"http://127.0.0.1:8765/open/abc"}`))
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	c := config.Defaults()
	c.APIToken = tok
	if link, err := pageLink(context.Background(), c, addr); err != nil || link != "http://127.0.0.1:8765/open/abc" {
		t.Fatalf("%q %v", link, err)
	}
	c.APIToken = rand.Text() // another token: the server refuses, and the error says so
	if _, err := pageLink(context.Background(), c, addr); err == nil || !strings.Contains(err.Error(), "gave no link") {
		t.Fatalf("%v", err)
	}
	c.Server.Mode = config.ModeAccounts // accounts mode has its own sign-in: the page itself
	if link, _ := pageLink(context.Background(), c, "10.0.0.5:8765"); link != "http://10.0.0.5:8765/" {
		t.Fatal(link)
	}
}
