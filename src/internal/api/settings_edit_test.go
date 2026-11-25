package api

import (
	"context"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/config"
)

type namedProvider struct{ name string }

func (p namedProvider) Name() string { return p.name }
func (p namedProvider) Answer(context.Context, ai.Job) (ai.Results, error) {
	return ai.Results{}, nil
}

func editableServer(t *testing.T) (*Server, config.Paths) {
	t.Helper()
	s := newServer(t, apiTok)
	p := config.Paths{Config: t.TempDir(), State: t.TempDir()}
	if err := os.WriteFile(filepath.Join(p.Config, ".env"), []byte("# mine\nBROFO_API_TOKEN="+apiTok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.ConfigPaths, s.Getenv = p, func(string) string { return "" }
	s.NewAI = func(c config.AI) ai.Provider {
		if c.Provider == "none" {
			return nil
		}
		return namedProvider{c.Provider + " " + c.Model}
	}
	return s, p
}

func TestSettingsEditFromThisComputerOnly(t *testing.T) {
	s, p := editableServer(t)
	h := s.Handler()
	key := rand.Text()
	body := `{"values":{"OPENAI_API_KEY":"` + key + `","ai.provider":"openai","ai.model":"gpt-x"}}`

	// From another computer: refused, nothing written.
	r := httptest.NewRequest("PUT", "/api/v1/settings", strings.NewReader(body))
	r.Host, r.RemoteAddr = host, "192.0.2.7:5000"
	r.Header.Set("Authorization", "Bearer "+apiTok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("remote save: %d %s", w.Code, w.Body)
	}

	r = httptest.NewRequest("PUT", "/api/v1/settings", strings.NewReader(body))
	r.Host, r.RemoteAddr = host, "127.0.0.1:5000"
	r.Header.Set("Authorization", "Bearer "+apiTok)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), key) {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	// Lists stay lists: the page reads restart.length (a null here once showed "Not saved" after a save).
	if !strings.Contains(w.Body.String(), `"restart":[]`) || !strings.Contains(w.Body.String(), `"local":true`) {
		t.Fatalf("restart must be [] when nothing needs a restart: %s", w.Body)
	}
	// The provider changed without a restart.
	if pr := s.provider(); pr == nil || pr.Name() != "openai gpt-x" {
		t.Fatalf("provider %v", pr)
	}
	env, _ := os.ReadFile(filepath.Join(p.Config, ".env"))
	if !strings.HasPrefix(string(env), "# mine\n") || !strings.Contains(string(env), "OPENAI_API_KEY="+key) {
		t.Fatalf(".env:\n%s", env)
	}
	// Listing never shows the key, and says it is set.
	got := send(h, "GET", "/api/v1/settings/fields", "").Body.String()
	if strings.Contains(got, key) || !strings.Contains(got, `"key":"OPENAI_API_KEY"`) {
		t.Fatalf("fields: %s", got)
	}
	// A risky setting in safe mode: 403.
	r = httptest.NewRequest("PUT", "/api/v1/settings", strings.NewReader(`{"values":{"server.addr":"127.0.0.1:9"},"confirm":true}`))
	r.Host, r.RemoteAddr = host, "127.0.0.1:5000"
	r.Header.Set("Authorization", "Bearer "+apiTok)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("server.addr in safe mode: %d %s", w.Code, w.Body)
	}
}
