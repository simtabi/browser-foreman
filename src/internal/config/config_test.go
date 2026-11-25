package config

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestPathsFollowXDGAndFallBackToDotConfig(t *testing.T) {
	p, err := ResolvePaths(env(map[string]string{"XDG_CONFIG_HOME": "/x/conf", "XDG_STATE_HOME": "/x/state"}))
	if err != nil || p.Config != "/x/conf/browser-foreman" || p.State != "/x/state/browser-foreman" {
		t.Fatalf("got %+v %v", p, err)
	}
	home, _ := os.UserHomeDir()
	p, _ = ResolvePaths(env(map[string]string{"XDG_CONFIG_HOME": "relative/is/ignored"}))
	if p.Config != filepath.Join(home, ".config", "browser-foreman") || p.State != filepath.Join(home, ".local", "state", "browser-foreman") {
		t.Fatalf("fallback %+v", p)
	}
}

func TestLayersMergeInOrderAndEnvironmentWins(t *testing.T) {
	dir := t.TempDir()
	p := Paths{Config: dir, State: filepath.Join(dir, "state")}
	must := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("00-base.json", `{"server": {"addr": "127.0.0.1:9000"}, "webhooks": {"attempts": 3, "timeout": "5s"}}`)
	must("10-override.json", `{"webhooks": {"attempts": 4}}`)
	dotenvTok := rand.Text()
	must(".env", "# secrets\nBROFO_API_TOKEN=\""+dotenvTok+"\"\nexport BROFO_STORAGE_DRIVER=json\n")
	c, err := Load(p, env(map[string]string{"BROFO_SERVER_ADDR": "127.0.0.1:9100"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Addr != "127.0.0.1:9100" || c.Webhooks.Attempts != 4 || c.Webhooks.Timeout.Duration != 5*time.Second {
		t.Fatalf("merge: %+v", c)
	}
	if c.APIToken != dotenvTok || c.Storage.Driver != "json" || c.Storage.Path != p.State {
		t.Fatalf("dotenv/defaults: %+v", c)
	}
	if len(c.Sources) != 3 {
		t.Fatalf("sources %v", c.Sources)
	}
}

func TestAnEnvFileOthersCanReadIsRefused(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, ".env")
	_ = os.WriteFile(f, []byte("BROFO_API_TOKEN="+rand.Text()+"\n"), 0o644)
	if err := os.Chmod(f, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Paths{Config: dir, State: dir}, env(nil)); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("want a refusal, got %v", err)
	}
}

func TestUnknownKeysAndTokensInJSONAreRefused(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"apiToken": "`+rand.Text()+`"}`), 0o600)
	if _, err := Load(Paths{Config: dir, State: dir}, env(nil)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("want unknown field, got %v", err)
	}
}

func TestValidateRefusesUnsafeOrBrokenSettings(t *testing.T) {
	bad := map[string]func(*Config){
		"local mode on a public address": func(c *Config) { c.Server.Addr = "0.0.0.0:8765" },
		"no port":                        func(c *Config) { c.Server.Addr = "127.0.0.1" },
		"unknown mode":                   func(c *Config) { c.Server.Mode = "open" },
		"unknown driver":                 func(c *Config) { c.Storage.Driver = "mongo" },
		"zero attempts":                  func(c *Config) { c.Webhooks.Attempts = 0 },
		"tiny timeout":                   func(c *Config) { c.Webhooks.Timeout = Duration{time.Millisecond} },
		"unknown write-back read":        func(c *Config) { c.Writeback.Read = "api" },
		"unknown open setting":           func(c *Config) { c.Server.Open = "sometimes" },
		"unknown write-back route":       func(c *Config) { c.Writeback.Route = "sync" },
		"unknown write-back mode":        func(c *Config) { c.Writeback.Mode = "overwrite" },
	}
	for name, f := range bad {
		c := Defaults()
		f(&c)
		if c.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	c := Defaults()
	c.Server.Addr, c.Server.Mode = "0.0.0.0:8765", ModeAccounts
	if err := c.Validate(); err != nil {
		t.Errorf("accounts mode may listen widely: %v", err)
	}
	c = Defaults()
	if c.Writeback != (Writeback{Read: "ask", Route: "ask", Mode: "ask"}) {
		t.Errorf("write-back choices are asked unless set: %+v", c.Writeback)
	}
	c.Writeback = Writeback{Read: "extension", Route: "file", Mode: "replace"}
	if err := c.Validate(); err != nil {
		t.Errorf("set write-back choices: %v", err)
	}
}

func TestInitWritesPrivateFilesOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	p := Paths{Config: dir, State: dir}
	tok := rand.Text()
	wrote, err := Init(p, tok)
	if err != nil || len(wrote) != 2 {
		t.Fatalf("%v %v", wrote, err)
	}
	for _, f := range wrote {
		if st, _ := os.Stat(f); st.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v, want 0600", f, st.Mode().Perm())
		}
	}
	if again, _ := Init(p, rand.Text()); len(again) != 0 {
		t.Fatalf("overwrote %v", again)
	}
	c, err := Load(p, env(nil))
	if err != nil || c.APIToken != tok {
		t.Fatalf("round trip: %+v %v", c, err)
	}
}
