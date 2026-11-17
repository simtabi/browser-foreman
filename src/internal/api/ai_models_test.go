package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/ai"
)

// Without a key the Anthropic list offers the default model at once, and OpenAI and Ollama are asked at the address
// saved in the files, with the key from .env, which never reaches the page.
func TestAIModelsForSettings(t *testing.T) {
	s, p := editableServer(t)
	h := s.Handler()
	start := time.Now()
	var got struct {
		Models  []ai.Model `json:"models"`
		Default string     `json:"default"`
		KeySet  bool       `json:"key_set"`
		Error   string     `json:"error"`
	}
	w := send(h, "GET", "/api/v1/ai/models?provider=anthropic", "")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got.KeySet || got.Default != ai.DefaultAnthropicModel || len(got.Models) != 1 || got.Models[0].ID != ai.DefaultAnthropicModel || time.Since(start) > 2*time.Second {
		t.Fatalf("no key: %+v in %v", got, time.Since(start))
	}

	key := strings.Repeat("o", 16)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "no", 401)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"tts-1"}]}`))
	}))
	defer up.Close()
	env := "BROFO_API_TOKEN=tok\nOPENAI_API_KEY=" + key + "\n"
	cfg := `{"ai":{"openai_url":"` + up.URL + `"}}`
	if err := os.WriteFile(filepath.Join(p.Config, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Config, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	w = send(h, "GET", "/api/v1/ai/models?provider=openai", "")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"id":"gpt-5"`) || strings.Contains(body, "tts-1") || strings.Contains(body, key) || !strings.Contains(body, `"key_set":true`) {
		t.Fatalf("openai: %d %s", w.Code, body)
	}
	if w := send(h, "GET", "/api/v1/ai/models?provider=mystery", ""); w.Code != 400 {
		t.Fatalf("unknown provider: %d", w.Code)
	}
}
