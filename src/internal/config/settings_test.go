package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T, configJSON, env string) Paths {
	t.Helper()
	p := Paths{Config: t.TempDir(), State: t.TempDir()}
	if configJSON != "" {
		if err := os.WriteFile(filepath.Join(p.Config, "config.json"), []byte(configJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if env != "" {
		if err := os.WriteFile(filepath.Join(p.Config, ".env"), []byte(env), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func noEnv(string) string { return "" }

func find(fs []Field, key string) Field {
	for _, f := range fs {
		if f.Key == key {
			return f
		}
	}
	return Field{}
}

func TestDescribeNeverShowsASecret(t *testing.T) {
	secret := "sk-" + rand.Text()
	p := setup(t, `{"ai":{"provider":"openai","model":"gpt-x"}}`, "# keys\nOPENAI_API_KEY="+secret+"\n")
	fs, mode, err := Describe(p, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "safe" {
		t.Fatalf("mode %q", mode)
	}
	b, _ := json.Marshal(fs)
	if strings.Contains(string(b), secret) {
		t.Fatal("a secret's value reached the description")
	}
	k := find(fs, "OPENAI_API_KEY")
	if !k.Set || k.Source != ".env" || !k.Editable || k.Type != "secret" {
		t.Fatalf("%+v", k)
	}
	if m := find(fs, "ai.model"); m.Value != "gpt-x" || m.Source != "config.json" || !m.Live {
		t.Fatalf("%+v", m)
	}
	if a := find(fs, "server.addr"); a.Editable || !a.Risky || a.Locked == "" {
		t.Fatalf("server.addr must be locked in safe mode: %+v", a)
	}
	if e := find(fs, "settings.edit"); e.Editable {
		t.Fatal("settings.edit must stay file-only")
	}
}

func TestApplyWritesKeysToEnvAndKeepsItsComments(t *testing.T) {
	p := setup(t, `{"server":{"addr":"127.0.0.1:8765"}}`, "# keep me\nANTHROPIC_BASE_URL=https://example.test\nBROFO_API_TOKEN="+rand.Text()+"\n")
	key := rand.Text()
	res, err := Apply(p, noEnv, map[string]json.RawMessage{"ANTHROPIC_API_KEY": raw(key), "ai.provider": raw("anthropic")}, false)
	if err != nil {
		t.Fatal(err)
	}
	env, _ := os.ReadFile(filepath.Join(p.Config, ".env"))
	if !strings.Contains(string(env), "# keep me\nANTHROPIC_BASE_URL=https://example.test\n") || !strings.Contains(string(env), "ANTHROPIC_API_KEY="+key+"\n") {
		t.Fatalf(".env:\n%s", env)
	}
	if st, _ := os.Stat(filepath.Join(p.Config, ".env")); st.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode %v", st.Mode().Perm())
	}
	cfg, _ := os.ReadFile(filepath.Join(p.Config, "config.json"))
	if !strings.Contains(string(cfg), `"provider": "anthropic"`) || !strings.Contains(string(cfg), `"addr": "127.0.0.1:8765"`) {
		t.Fatalf("config.json:\n%s", cfg)
	}
	if len(res.Restart) != 0 {
		t.Fatalf("AI settings apply at once, not after a restart: %v", res.Restart)
	}
	c, err := Load(p, noEnv)
	if err != nil || c.AI.AnthropicKey != key || c.AI.Provider != "anthropic" {
		t.Fatalf("%v %+v", err, c.AI)
	}
	// Clearing a secret empties its line.
	if _, err := Apply(p, noEnv, map[string]json.RawMessage{"ANTHROPIC_API_KEY": raw("")}, false); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(p, noEnv); c.AI.AnthropicKey != "" {
		t.Fatal("not cleared")
	}
}

func TestApplyChecksTheWholeChangeBeforeWriting(t *testing.T) {
	p := setup(t, `{}`, "")
	// openai needs a model: refused, and nothing written.
	if _, err := Apply(p, noEnv, map[string]json.RawMessage{"ai.provider": raw("openai")}, false); err == nil {
		t.Fatal("openai without a model was accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(p.Config, "config.json")); string(b) != "{}" {
		t.Fatalf("written although refused: %s", b)
	}
	for key, v := range map[string]any{"server.open": "sometimes", "webhooks.attempts": "five", "webhooks.timeout": "soon", "nope.key": 1} {
		if _, err := Apply(p, noEnv, map[string]json.RawMessage{key: raw(v)}, false); err == nil {
			t.Errorf("%s=%v accepted", key, v)
		}
	}
	if _, err := Apply(p, noEnv, map[string]json.RawMessage{"ai.provider": raw("openai"), "ai.model": raw("gpt-x")}, false); err != nil {
		t.Fatal(err)
	}
}

func TestRiskySettingsNeedAllAndAConfirmation(t *testing.T) {
	p := setup(t, `{}`, "")
	addr := map[string]json.RawMessage{"server.addr": raw("127.0.0.1:9000")}
	if _, err := Apply(p, noEnv, addr, true); !errors.Is(err, ErrLocked) {
		t.Fatalf("safe mode let server.addr through: %v", err)
	}
	p = setup(t, `{"settings":{"edit":"all"}}`, "")
	if _, err := Apply(p, noEnv, addr, false); !errors.Is(err, ErrConfirm) {
		t.Fatalf("all mode without a confirmation: %v", err)
	}
	res, err := Apply(p, noEnv, addr, true)
	if err != nil || len(res.Restart) != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	// The page can never widen its own rights.
	if _, err := Apply(p, noEnv, map[string]json.RawMessage{"settings.edit": raw("safe")}, true); !errors.Is(err, ErrLocked) {
		t.Fatalf("settings.edit changed from the page: %v", err)
	}
}

func TestTheEnvironmentWins(t *testing.T) {
	p := setup(t, `{}`, "")
	getenv := func(k string) string {
		if k == "BROFO_AI_MODEL" {
			return "from-shell"
		}
		return ""
	}
	if _, err := Apply(p, getenv, map[string]json.RawMessage{"ai.model": raw("x")}, false); !errors.Is(err, ErrLocked) {
		t.Fatalf("a value the environment sets was edited: %v", err)
	}
}

func TestEditsLandInTheFileThatSetsTheKey(t *testing.T) {
	p := setup(t, `{}`, "")
	if err := os.WriteFile(filepath.Join(p.Config, "50-ai.json"), []byte(`{"ai":{"model":"a"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(p, noEnv, map[string]json.RawMessage{"ai.model": raw("b")}, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p.Config, "50-ai.json"))
	if !strings.Contains(string(b), `"model": "b"`) {
		t.Fatalf("50-ai.json: %s", b)
	}
	if c, _ := Load(p, noEnv); c.AI.Model != "b" {
		t.Fatalf("model %q", c.AI.Model)
	}
}

// The catalogue, the loader and the shipped env.example must agree: every variable the loader reads is a tagged
// setting and is in env.example, and every tagged variable is one the loader reads.
func TestTagsLoaderAndEnvExampleAgree(t *testing.T) {
	read := map[string]bool{}
	c := Config{}
	_ = apply(&c, func(k string) string { read[k] = true; return "" })
	if len(read) < 15 {
		t.Fatalf("the loader reads %d variables; is apply's table intact?", len(read))
	}
	tagged := map[string]bool{}
	for _, f := range catalog() {
		if f.Env != "" {
			tagged[f.Env] = true
		}
	}
	example := parseEnv(EnvExample)
	for k := range read {
		if !tagged[k] {
			t.Errorf("%s is read but no setting is tagged with it", k)
		}
		if _, ok := example[k]; !ok {
			t.Errorf("%s is read but missing from env.example", k)
		}
	}
	for k := range tagged {
		if !read[k] {
			t.Errorf("%s is tagged but the loader never reads it", k)
		}
	}
	// Every listed choice is one Validate accepts.
	for _, f := range catalog() {
		if f.Type != "choice" || f.Locked != "" {
			continue
		}
		for _, ch := range f.Choices {
			p := setup(t, `{"settings":{"edit":"all"}}`, "")
			vals := map[string]json.RawMessage{f.Key: raw(ch)}
			if f.Key == "ai.provider" && (ch == "openai" || ch == "ollama") {
				vals["ai.model"] = raw("m")
			}
			if f.Key == "storage.driver" && ch == "postgres" {
				vals["BROFO_STORAGE_DSN"] = raw("postgres://brofo@127.0.0.1:55433/brofo_test")
			}
			if f.Key == "server.mode" && ch == "accounts" {
				continue // needs accounts set up; Validate accepts it, checked in config_test
			}
			if _, err := Apply(p, noEnv, vals, true); err != nil {
				t.Errorf("%s=%s listed but refused: %v", f.Key, ch, err)
			}
		}
	}
}

func TestInitMakesEnvFromTheExample(t *testing.T) {
	p := Paths{Config: t.TempDir(), State: t.TempDir()}
	tok := rand.Text()
	if _, err := Init(p, tok); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p.Config, ".env"))
	if !strings.HasPrefix(string(b), "# Browser Foreman's secrets") || !strings.Contains(string(b), "BROFO_API_TOKEN="+tok+"\n") || !strings.Contains(string(b), "OPENAI_API_KEY=\n") {
		t.Fatalf(".env:\n%s", b)
	}
	if c, err := Load(p, noEnv); err != nil || c.APIToken != tok {
		t.Fatalf("%v %q", err, c.APIToken)
	}
}

// The OpenAI key goes wherever ai.openai_url points, so pointing it elsewhere is as risky as moving the server:
// otherwise anyone who may edit safe settings could read the write-only key.
func TestTheOpenAIAddressGuardsItsKey(t *testing.T) {
	p := setup(t, `{}`, "")
	addr := map[string]json.RawMessage{"ai.openai_url": raw("https://elsewhere.example/v1")}
	if _, err := Apply(p, noEnv, addr, true); !errors.Is(err, ErrLocked) {
		t.Fatalf("safe mode let ai.openai_url through: %v", err)
	}
	p = setup(t, `{"settings":{"edit":"all"}}`, "")
	if _, err := Apply(p, noEnv, addr, false); !errors.Is(err, ErrConfirm) {
		t.Fatalf("all mode without a confirmation: %v", err)
	}
	if _, err := Apply(p, noEnv, addr, true); err != nil {
		t.Fatal(err)
	}
	if f := find(mustDescribe(t, p), "ai.ollama_url"); !f.Editable || f.Risky {
		t.Fatalf("Ollama's address carries no key and stays an ordinary setting: %+v", f)
	}
}

func mustDescribe(t *testing.T, p Paths) []Field {
	t.Helper()
	fs, _, err := Describe(p, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// A certificate needs its key, and a trusted proxy must be an address or a range.
func TestTLSAndProxySettingsAreChecked(t *testing.T) {
	c := Defaults()
	c.Server.TLSCert = "/no/such/cert.pem"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "together") {
		t.Fatalf("a certificate without a key: %v", err)
	}
	c.Server.TLSKey = "/no/such/key.pem"
	if err := c.Validate(); err == nil {
		t.Fatal("unreadable certificate files were accepted")
	}
	c = Defaults()
	c.Server.TrustedProxy = []string{"10.0.0.0/8", "192.0.2.1", "not-an-address"}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "not-an-address") {
		t.Fatalf("a bad proxy entry: %v", err)
	}
	c.Server.TrustedProxy = c.Server.TrustedProxy[:2]
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for ip, want := range map[string]bool{"10.9.8.7": true, "192.0.2.1": true, "192.0.2.2": false} {
		if c.Server.Behind(net.ParseIP(ip)) != want {
			t.Errorf("Behind(%s) != %v", ip, want)
		}
	}
}
