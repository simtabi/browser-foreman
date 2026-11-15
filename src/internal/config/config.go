// Package config reads Browser Foreman's configuration.
//
// Files live in the config directory ($XDG_CONFIG_HOME/browser-foreman, else ~/.config/browser-foreman, on macOS
// too): every *.json there, merged in name order (so 10-server.json overrides 00-defaults.json), then .env
// (KEY=VALUE lines), then the process environment. Environment variables win. Every key has a BROFO_ variable:
// BROFO_SERVER_ADDR, BROFO_SERVER_MODE, BROFO_STORAGE_DRIVER, BROFO_STORAGE_PATH, BROFO_WEBHOOK_TIMEOUT,
// BROFO_WEBHOOK_ATTEMPTS, BROFO_SESSION_TTL, BROFO_MCP, BROFO_MCP_WRITES, BROFO_API_TOKEN.
//
// Mutable state (the database, tokens, logs) lives in the state directory: $XDG_STATE_HOME/browser-foreman, else
// ~/.local/state/browser-foreman.
package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const App = "browser-foreman"

// Mode decides who may use the server: one implicit owner on this computer, or signed-in accounts.
type Mode string

const (
	ModeLocal    Mode = "local"
	ModeAccounts Mode = "accounts"
)

type Server struct {
	Addr string `json:"addr"` // host:port; 127.0.0.1 unless you choose otherwise
	Mode Mode   `json:"mode"`
}

type Storage struct {
	Driver string `json:"driver"` // sqlite or json
	Path   string `json:"path"`   // empty: the state directory
}

type Webhooks struct {
	Timeout  Duration `json:"timeout"`  // per delivery attempt
	Attempts int      `json:"attempts"` // tries before a delivery is marked failed
}

// Auth settings: how long a sign-in lasts.
type Auth struct {
	SessionTTL Duration `json:"session_ttl"`
}

// MCP is the Model Context Protocol server (`browser-foreman mcp`): off unless Enabled, read-only unless Writes.
type MCP struct {
	Enabled bool `json:"enabled"`
	Writes  bool `json:"writes"`
}

// Writeback is how `browser-foreman apply` writes a cleaned workspace into a browser: each choice is a value, or
// "ask" (the default) to be asked every time.
type Writeback struct {
	Read  string `json:"read"`  // where the dry run reads the profile now: file, extension or ask
	Route string `json:"route"` // how the write reaches it: file, extension or ask
	Mode  string `json:"mode"`  // merge, replace or ask
}

// Config is the whole configuration. APIToken is never read from a JSON file: secrets come from .env or the
// environment, so a config file can be shared without one.
type Config struct {
	Server    Server    `json:"server"`
	Storage   Storage   `json:"storage"`
	Webhooks  Webhooks  `json:"webhooks"`
	Auth      Auth      `json:"auth"`
	MCP       MCP       `json:"mcp"`
	Writeback Writeback `json:"writeback"`
	APIToken  string    `json:"-"`
	// Sources lists what was read, in order, for `browser-foreman config show`.
	Sources []string `json:"-"`
}

// Duration reads "10s", "1m" or a number of seconds.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("duration %q: %w", s, err)
		}
		d.Duration = v
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("duration must be a string like \"10s\" or a number of seconds")
	}
	d.Duration = time.Duration(n * float64(time.Second))
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// Defaults is a working configuration with nothing set.
func Defaults() Config {
	return Config{
		Server:    Server{Addr: "127.0.0.1:8765", Mode: ModeLocal},
		Storage:   Storage{Driver: "sqlite"},
		Webhooks:  Webhooks{Timeout: Duration{10 * time.Second}, Attempts: 5},
		Auth:      Auth{SessionTTL: Duration{24 * time.Hour}},
		Writeback: Writeback{Read: "ask", Route: "ask", Mode: "ask"},
	}
}

// Paths are the directories the program uses. Env is the environment to read them from (os.Getenv in use).
type Paths struct{ Config, State string }

func ResolvePaths(getenv func(string) string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil && (getenv("XDG_CONFIG_HOME") == "" || getenv("XDG_STATE_HOME") == "") {
		return Paths{}, fmt.Errorf("no home directory: %w", err)
	}
	pick := func(xdg, fallback string) string {
		if v := getenv(xdg); v != "" && filepath.IsAbs(v) {
			return filepath.Join(v, App)
		}
		return filepath.Join(home, fallback, App)
	}
	return Paths{Config: pick("XDG_CONFIG_HOME", ".config"), State: pick("XDG_STATE_HOME", filepath.Join(".local", "state"))}, nil
}

// Load reads the configuration: defaults, then *.json in the config directory, then .env, then the environment.
func Load(p Paths, getenv func(string) string) (Config, error) {
	c := Defaults()
	files, _ := filepath.Glob(filepath.Join(p.Config, "*.json"))
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return c, err
		}
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return c, fmt.Errorf("%s: %w", f, err)
		}
		c.Sources = append(c.Sources, f)
	}
	env := map[string]string{}
	envFile := filepath.Join(p.Config, ".env")
	if kv, err := readDotEnv(envFile); err == nil {
		env = kv
		c.Sources = append(c.Sources, envFile)
	} else if !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	get := func(k string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return env[k]
	}
	if err := apply(&c, get); err != nil {
		return c, err
	}
	if c.Storage.Path == "" {
		c.Storage.Path = p.State
	}
	return c, c.Validate()
}

func apply(c *Config, get func(string) string) error {
	set := map[string]func(string) error{
		"BROFO_SERVER_ADDR":      func(v string) error { c.Server.Addr = v; return nil },
		"BROFO_SERVER_MODE":      func(v string) error { c.Server.Mode = Mode(v); return nil },
		"BROFO_STORAGE_DRIVER":   func(v string) error { c.Storage.Driver = v; return nil },
		"BROFO_STORAGE_PATH":     func(v string) error { c.Storage.Path = v; return nil },
		"BROFO_API_TOKEN":        func(v string) error { c.APIToken = v; return nil },
		"BROFO_WEBHOOK_ATTEMPTS": func(v string) error { n, err := strconv.Atoi(v); c.Webhooks.Attempts = n; return err },
		"BROFO_MCP":              func(v string) error { b, err := strconv.ParseBool(v); c.MCP.Enabled = b; return err },
		"BROFO_MCP_WRITES":       func(v string) error { b, err := strconv.ParseBool(v); c.MCP.Writes = b; return err },
		"BROFO_SESSION_TTL": func(v string) error {
			d, err := time.ParseDuration(v)
			c.Auth.SessionTTL = Duration{d}
			return err
		},
		"BROFO_WEBHOOK_TIMEOUT": func(v string) error {
			d, err := time.ParseDuration(v)
			c.Webhooks.Timeout = Duration{d}
			return err
		},
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := get(k); v != "" {
			if err := set[k](v); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	}
	return nil
}

// Validate refuses a configuration that cannot work or would be unsafe.
func (c Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Server.Addr)
	if err != nil {
		return fmt.Errorf("server.addr %q: %w", c.Server.Addr, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("server.addr %q: port must be 1-65535", c.Server.Addr)
	}
	switch c.Server.Mode {
	case ModeLocal:
		// One implicit owner with no sign-in: only safe when nothing else can reach the server.
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("server.mode local needs a loopback address (127.0.0.1, ::1 or localhost); use mode accounts to listen on %q", host)
		}
	case ModeAccounts:
	default:
		return fmt.Errorf("server.mode %q: use local or accounts", c.Server.Mode)
	}
	if c.Storage.Driver != "sqlite" && c.Storage.Driver != "json" {
		return fmt.Errorf("storage.driver %q: use sqlite or json", c.Storage.Driver)
	}
	if c.Webhooks.Attempts < 1 || c.Webhooks.Attempts > 20 {
		return fmt.Errorf("webhooks.attempts %d: use 1-20", c.Webhooks.Attempts)
	}
	if c.Auth.SessionTTL.Duration < 5*time.Minute || c.Auth.SessionTTL.Duration > 30*24*time.Hour {
		return fmt.Errorf("auth.session_ttl %s: use 5m-720h", c.Auth.SessionTTL)
	}
	if c.Webhooks.Timeout.Duration < time.Second || c.Webhooks.Timeout.Duration > 2*time.Minute {
		return fmt.Errorf("webhooks.timeout %s: use 1s-2m", c.Webhooks.Timeout)
	}
	for _, w := range []struct{ key, v, a, b string }{
		{"writeback.read", c.Writeback.Read, "file", "extension"},
		{"writeback.route", c.Writeback.Route, "file", "extension"},
		{"writeback.mode", c.Writeback.Mode, "merge", "replace"},
	} {
		if w.v != "ask" && w.v != w.a && w.v != w.b {
			return fmt.Errorf("%s %q: use ask, %s or %s", w.key, w.v, w.a, w.b)
		}
	}
	return nil
}

// readDotEnv reads KEY=VALUE lines; blank lines and # comments are skipped, one pair of surrounding quotes is removed.
func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// .env holds the API token: refuse one that other users on this computer can read.
	if st, err := f.Stat(); err == nil && st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by other users (%v); run chmod 600 on it", path, st.Mode().Perm())
	}
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", path, n)
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, sc.Err()
}

// Init writes a starting config.json and an .env with a fresh API token, refusing to overwrite either.
func Init(p Paths, token string) ([]string, error) {
	if err := os.MkdirAll(p.Config, 0o700); err != nil {
		return nil, err
	}
	d := Defaults()
	body, _ := json.MarshalIndent(struct {
		Server   Server   `json:"server"`
		Storage  Storage  `json:"storage"`
		Webhooks Webhooks `json:"webhooks"`
		Auth     Auth     `json:"auth"`
	}{d.Server, d.Storage, d.Webhooks, d.Auth}, "", "  ")
	files := map[string][]byte{
		"config.json": append(body, '\n'),
		".env":        []byte("# Secrets for Browser Foreman. Keep this file private (it is created 0600).\nBROFO_API_TOKEN=" + token + "\n"),
	}
	var wrote []string
	for _, name := range []string{"config.json", ".env"} {
		path := filepath.Join(p.Config, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return wrote, err
		}
		_, err = f.Write(files[name])
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return wrote, err
		}
		wrote = append(wrote, path)
	}
	return wrote, nil
}
