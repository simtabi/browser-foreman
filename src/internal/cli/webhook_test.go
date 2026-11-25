package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeAPI is the running server's API as the webhook and doctor commands use it, holding what they send.
func fakeAPI(t *testing.T, tok string) (seen *[]string, bodies map[string]map[string]any) {
	t.Helper()
	seen, bodies = &[]string{}, map[string]map[string]any{}
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			*seen = append(*seen, r.Method+" "+r.URL.Path)
			if r.Header.Get("Authorization") != "Bearer "+tok {
				http.Error(w, `{"error":"sign in"}`, http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"service":"browser-foreman","version":"x","pid":1}`))
	})
	mux.HandleFunc("GET /api/v1/webhooks", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"webhooks":[{"id":"wh_1","url":"https://hooks.example/x","events":["bookmark.*"],"active":true}]}`))
	}))
	mux.HandleFunc("POST /api/v1/webhooks", auth(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies["add"] = b
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"wh_2","url":"https://hooks.example/y","events":["*"],"active":true}`))
	}))
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "wh_1" {
			http.Error(w, `{"error":"no webhook with that id"}`, http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("POST /api/v1/webhooks/{id}/test", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") == "wh_down" {
			_, _ = w.Write([]byte(`{"ok":false,"status":502,"attempts":1,"error":"bad gateway"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"status":204,"attempts":1}`))
	}))
	mux.HandleFunc("GET /api/v1/browsers", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"installed":[{"name":"Safari","engine":"webkit","path":"/A/Safari.app"},{"name":"Google Chrome","engine":"chromium","path":"/A/Chrome.app"}]}`))
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("BROFO_SERVER_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("BROFO_API_TOKEN", tok)
	t.Setenv("BROFO_WEBHOOK_SECRET", "")
	return seen, bodies
}

func runCmd(c *cobra.Command, args ...string) (string, error) {
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestWebhookCommandsUseTheRunningServer(t *testing.T) {
	seen, bodies := fakeAPI(t, rand.Text())
	if out, err := runCmd(webhookCmd(), "list"); err != nil || !strings.Contains(out, "wh_1") || !strings.Contains(out, "bookmark.*") || !strings.Contains(out, "https://hooks.example/x") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	out, err := runCmd(webhookCmd(), "add", "https://hooks.example/y", "--events", "bookmark.*,tabs.snapshot.saved")
	if err != nil || !strings.Contains(out, "added wh_2") {
		t.Fatalf("add: %v\n%s", err, out)
	}
	made, _ := bodies["add"]["secret"].(string)
	if len(made) != 64 || !strings.Contains(out, made) {
		t.Fatalf("a made secret is 32 random bytes, shown once: %q\n%s", made, out)
	}
	if ev, _ := json.Marshal(bodies["add"]["events"]); string(ev) != `["bookmark.*","tabs.snapshot.saved"]` {
		t.Fatalf("events %s", ev)
	}
	given := rand.Text()
	t.Setenv("BROFO_WEBHOOK_SECRET", given)
	if out, err := runCmd(webhookCmd(), "add", "https://hooks.example/y"); err != nil || bodies["add"]["secret"] != given || strings.Contains(out, given) {
		t.Fatalf("a given secret is sent and never printed: %v\n%s", err, out)
	}
	if out, err := runCmd(webhookCmd(), "remove", "wh_1"); err != nil || !strings.Contains(out, "removed wh_1") {
		t.Fatalf("remove: %v\n%s", err, out)
	}
	if _, err := runCmd(webhookCmd(), "remove", "wh_9"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("remove unknown: %v", err)
	}
	if out, err := runCmd(webhookCmd(), "test", "wh_1"); err != nil || !strings.Contains(out, "answered 204") {
		t.Fatalf("test: %v\n%s", err, out)
	}
	if _, err := runCmd(webhookCmd(), "test", "wh_down"); err == nil || !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("test failing: %v", err)
	}
	want := "GET /api/v1/webhooks,POST /api/v1/webhooks,POST /api/v1/webhooks,DELETE /api/v1/webhooks/wh_1,DELETE /api/v1/webhooks/wh_9,POST /api/v1/webhooks/wh_1/test,POST /api/v1/webhooks/wh_down/test"
	if got := strings.Join(*seen, ","); got != want {
		t.Fatalf("requests\n got %s\nwant %s", got, want)
	}
}

func TestDoctorSaysWhatWorksAndWhatDoesNot(t *testing.T) {
	tok := rand.Text()
	fakeAPI(t, tok)
	out, err := runCmd(doctorCmd())
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ok    configuration:", "ok    storage:", "ok    API token: set", "ok    server: answering", "ok    page snapshots: 1 Chromium"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	t.Setenv("BROFO_API_TOKEN", rand.Text()) // not the server's: its API refuses, and doctor fails
	if out, err := runCmd(doctorCmd()); err == nil || !strings.Contains(out, "fail  server API:") {
		t.Fatalf("%v\n%s", err, out)
	}
	t.Setenv("BROFO_SERVER_ADDR", "127.0.0.1:9") // nothing there: a warning, not a failure
	if out, err := runCmd(doctorCmd()); err != nil || !strings.Contains(out, "warn  server: not running") {
		t.Fatalf("%v\n%s", err, out)
	}
}
