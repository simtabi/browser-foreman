package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// Model is one model a provider offers, as Settings lists it to choose from.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Models asks Anthropic which models the key can use, newest first.
func (a *Anthropic) Models(ctx context.Context) ([]Model, error) {
	var out []Model
	pager := a.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	for pager.Next() {
		m := pager.Current()
		out = append(out, Model{ID: m.ID, Name: m.DisplayName})
	}
	return out, pager.Err()
}

// notChat are the parts of an OpenAI model id that mark one a chat job cannot use (embeddings, speech, images,
// moderation, the legacy completion models).
var notChat = []string{"embedding", "tts", "whisper", "dall-e", "davinci", "babbage", "moderation", "image", "audio",
	"realtime", "transcribe", "search", "computer-use"}

// Models asks the OpenAI API (or a server speaking it) which models the key can use, leaving out the kinds a chat
// job cannot use.
func (o *OpenAI) Models(ctx context.Context) ([]Model, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := o.getJSON(ctx, strings.TrimRight(o.URL, "/")+"/models", &body); err != nil {
		return nil, err
	}
	var out []Model
	for _, m := range body.Data {
		chat := true
		for _, p := range notChat {
			chat = chat && !strings.Contains(m.ID, p)
		}
		if chat {
			out = append(out, Model{ID: m.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (o *OpenAI) getJSON(ctx context.Context, url string, v any) error {
	if o.Key == "" {
		return errors.New("no OpenAI API key: add one first")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+o.Key)
	return doJSON(o.Client, req, v)
}

// Models lists the models pulled into the local Ollama.
func (o *Ollama) Models(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(o.URL, "/")+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := doJSON(o.Client, req, &body); err != nil {
		return nil, fmt.Errorf("Ollama is not answering at %s: %w", o.URL, err)
	}
	out := make([]Model, 0, len(body.Models))
	for _, m := range body.Models {
		out = append(out, Model{ID: m.Name})
	}
	return out, nil
}

// doJSON runs a short request (a list, not a job) and decodes its JSON answer.
func doJSON(c *http.Client, req *http.Request, v any) error {
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s answered %d: %s", req.URL.Host, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
}
