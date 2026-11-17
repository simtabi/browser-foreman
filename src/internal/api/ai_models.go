package api

import (
	"context"
	"net/http"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/ai"
	"github.com/simtabi/browser-foreman/src/internal/config"
)

// aiModels lists the models a provider offers, for Settings' model dropdown: asked of the provider itself with
// the key saved in .env, so the key never reaches the page. A provider that cannot be asked (no key yet, Ollama not
// running) answers with its error and, for Anthropic, the default model, so the page can still offer it.
func (s *Server) aiModels(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	c := s.Cfg
	if s.Getenv != nil {
		if fresh, err := config.Load(s.ConfigPaths, s.Getenv); err == nil {
			c = fresh // the key or address may have changed in Settings since the server started
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	out := map[string]any{"provider": provider, "models": []ai.Model{}}
	var models []ai.Model
	var err error
	switch provider {
	case "anthropic":
		out["default"] = ai.DefaultAnthropicModel
		out["key_set"] = c.AI.AnthropicKey != ""
		if c.AI.AnthropicKey != "" { // without a key there is nobody to ask: offer the default at once
			models, err = ai.NewAnthropic(c.AI.AnthropicKey, "", "").Models(ctx)
		}
		if err != nil || len(models) == 0 {
			models = []ai.Model{{ID: ai.DefaultAnthropicModel, Name: "Claude Opus 5.5 (default)"}}
		}
	case "openai":
		out["key_set"] = c.AI.OpenAIKey != ""
		models, err = (&ai.OpenAI{URL: c.AI.OpenAIURL, Key: c.AI.OpenAIKey}).Models(ctx)
	case "ollama":
		models, err = (&ai.Ollama{URL: c.AI.OllamaURL}).Models(ctx)
	default:
		writeErr(w, 400, "provider: anthropic, openai or ollama")
		return
	}
	if models != nil {
		out["models"] = models
	}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSON(w, 200, out)
}
