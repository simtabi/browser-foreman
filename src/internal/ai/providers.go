package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// ---- Anthropic ----

// DefaultAnthropicModel is the model a job uses unless ai.model names another.
const DefaultAnthropicModel = "claude-opus-5-5"

// Anthropic answers jobs through Anthropic's Messages API, with the answer held to the job's JSON schema.
type Anthropic struct {
	client anthropic.Client
	model  string
	effort string
}

// NewAnthropic is the provider. key is ANTHROPIC_API_KEY ("" lets the SDK find credentials itself, such as a profile
// from `ant auth login`); effort is low, medium or high ("" leaves the model's default). opts are for tests.
func NewAnthropic(key, model, effort string, opts ...option.RequestOption) *Anthropic {
	if key != "" {
		opts = append([]option.RequestOption{option.WithAPIKey(key)}, opts...)
	}
	if model == "" {
		model = DefaultAnthropicModel
	}
	return &Anthropic{client: anthropic.NewClient(opts...), model: model, effort: effort}
}

func (*Anthropic) Name() string { return "anthropic" }

// fallbackModels accept server-side refusal fallbacks ("default" routes by refusal category).
var fallbackModels = map[string]bool{"claude-opus-5-5": true, "claude-opus-5": true, "claude-sonnet-5-5": true, "claude-fable-5-2": true, "claude-fable-5-1": true}

func (a *Anthropic) Answer(ctx context.Context, j Job) (Results, error) {
	system, user, err := Prompt(j)
	if err != nil {
		return Results{}, err
	}
	p := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(user))},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffort(a.effort),
			Format: anthropic.BetaJSONOutputFormatParam{Schema: Schema(j.Kind)},
		},
	}
	if fallbackModels[a.model] {
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	resp, err := a.client.Beta.Messages.New(ctx, p)
	if err != nil {
		var apierr *anthropic.Error
		if errors.As(err, &apierr) {
			switch apierr.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				return Results{}, errors.New("Anthropic refused the credentials: set ANTHROPIC_API_KEY in Browser Foreman's .env, or sign in with `ant auth login`")
			case http.StatusNotFound:
				return Results{}, fmt.Errorf("Anthropic has no model %q: check ai.model", a.model)
			case http.StatusTooManyRequests:
				return Results{}, errors.New("Anthropic's rate limit was reached: the job stays queued; run it again later")
			case http.StatusBadRequest:
				return Results{}, fmt.Errorf("Anthropic could not take the request (ai.model %q, ai.effort %q): %v", a.model, a.effort, apierr)
			}
		}
		return Results{}, fmt.Errorf("Anthropic did not answer: %w", err)
	}
	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return Results{}, fmt.Errorf("the model declined this job (%s)", resp.StopDetails.Category)
	case anthropic.BetaStopReasonMaxTokens:
		return Results{}, errors.New("the answer was cut off before it finished")
	}
	var text strings.Builder
	for _, b := range resp.Content {
		if t, ok := b.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	return Parse(j, text.String())
}

// ---- Ollama ----

// Ollama answers jobs with a model running in a local Ollama (https://ollama.com), held to the job's JSON schema.
type Ollama struct {
	URL    string // http://127.0.0.1:11434 by default
	Model  string
	Client *http.Client
}

func (*Ollama) Name() string { return "ollama" }

func (o *Ollama) Answer(ctx context.Context, j Job) (Results, error) {
	if o.Model == "" {
		return Results{}, errors.New("set ai.model to an Ollama model you have pulled (`ollama list` shows them)")
	}
	system, user, err := Prompt(j)
	if err != nil {
		return Results{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"model":    o.Model,
		"stream":   false,
		"format":   Schema(j.Kind),
		"options":  map[string]any{"temperature": 0},
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.URL, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return Results{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c := o.Client
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Minute} // a local model on a laptop can be slow
	}
	resp, err := c.Do(req)
	if err != nil {
		return Results{}, fmt.Errorf("Ollama is not answering at %s (is it running? `ollama serve`): %w", o.URL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK {
		if out.Error == "" {
			out.Error = resp.Status
		}
		return Results{}, fmt.Errorf("Ollama: %s", out.Error)
	}
	return Parse(j, out.Message.Content)
}

// ---- OpenAI ----

// OpenAI answers jobs through OpenAI's Chat Completions API, with the answer held to the job's JSON schema in strict
// mode. URL may name any server that speaks the same API.
type OpenAI struct {
	URL    string // https://api.openai.com/v1 by default
	Key    string // OPENAI_API_KEY
	Model  string // required: OpenAI's models change often, so it is never guessed
	Client *http.Client
}

func (*OpenAI) Name() string { return "openai" }

func (o *OpenAI) Answer(ctx context.Context, j Job) (Results, error) {
	if o.Model == "" {
		return Results{}, errors.New("set ai.model to an OpenAI model your key can use")
	}
	if o.Key == "" {
		return Results{}, errors.New("set OPENAI_API_KEY in Browser Foreman's .env (an API key from platform.openai.com: a ChatGPT subscription does not include one)")
	}
	system, user, err := Prompt(j)
	if err != nil {
		return Results{}, err
	}
	body, _ := json.Marshal(map[string]any{
		"model":    o.Model,
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "browser_foreman_" + j.Kind, "strict": true, "schema": Schema(j.Kind)}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.URL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Results{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.Key)
	c := o.Client
	if c == nil {
		c = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := c.Do(req)
	if err != nil {
		return Results{}, fmt.Errorf("OpenAI did not answer at %s: %w", o.URL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return Results{}, errors.New("OpenAI refused the key: check OPENAI_API_KEY")
	case resp.StatusCode == http.StatusTooManyRequests:
		return Results{}, errors.New("OpenAI's rate limit or quota was reached: the job can be made again later")
	case resp.StatusCode != http.StatusOK:
		msg := resp.Status
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return Results{}, fmt.Errorf("OpenAI (ai.model %q): %s", o.Model, msg)
	case len(out.Choices) == 0:
		return Results{}, errors.New("OpenAI answered with nothing")
	case out.Choices[0].Message.Refusal != "":
		return Results{}, fmt.Errorf("the model declined this job: %s", out.Choices[0].Message.Refusal)
	case out.Choices[0].FinishReason == "length":
		return Results{}, errors.New("the answer was cut off before it finished")
	}
	return Parse(j, out.Choices[0].Message.Content)
}
