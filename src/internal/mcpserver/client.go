package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// API talks to the running Browser Foreman server with a token, so every change made through MCP goes through the
// same rules, history, locks and events as any other.
type API struct {
	Base   string // http://127.0.0.1:8765
	Token  string
	Client *http.Client
}

func NewAPI(base, token string) *API {
	return &API{Base: strings.TrimRight(base, "/"), Token: token, Client: &http.Client{Timeout: 150 * time.Second}}
}

// apiError keeps the server's own message, which says what to do.
type apiError struct {
	Status int
	Msg    string
}

func (e *apiError) Error() string { return fmt.Sprintf("%s (HTTP %d)", e.Msg, e.Status) }

func (a *API) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.Base+"/api/v1/"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("Browser Foreman is not answering at %s: start it with `browser-foreman serve`", a.Base)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return &apiError{resp.StatusCode, e.Error}
	}
	if out == nil {
		return nil
	}
	if s, ok := out.(*string); ok {
		*s = string(raw)
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (a *API) get(ctx context.Context, path string, out any) error {
	return a.do(ctx, "GET", path, nil, out)
}

func ws(id string) string { return "workspaces/" + url.PathEscape(id) }
