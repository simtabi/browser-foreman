package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

	handler http.Handler // set by InProcess
}

func NewAPI(base, token string) *API {
	return &API{Base: strings.TrimRight(base, "/"), Token: token, Client: &http.Client{Timeout: 150 * time.Second}}
}

// InProcess calls the API handler h directly, with no port: for when no server is running, so the tools still work
// on the store, as the Python server's did. Requests carry the token and come to host (the server's own address), so
// the handler applies the same checks as over the network.
func InProcess(h http.Handler, host, token string) *API {
	a := NewAPI("http://"+host, token)
	a.handler = h
	a.Client.Transport = handlerTransport{h}
	return a
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	in := r.Clone(r.Context())
	in.Host = r.URL.Host
	in.RemoteAddr = "127.0.0.1:0"
	in.RequestURI = r.URL.RequestURI()
	if in.Body == nil {
		in.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, in)
	return rec.Result(), nil
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
