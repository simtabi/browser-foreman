// Package config reads Browser Foreman's configuration.
//
// Files live in the config directory ($XDG_CONFIG_HOME/browser-foreman, else ~/.config/browser-foreman, on macOS
// too): every *.json there, merged in name order (so 10-server.json overrides 00-defaults.json), then .env
// (KEY=VALUE lines), then the process environment. Environment variables win. Every key has a BROFO_ variable:
// BROFO_SERVER_ADDR, BROFO_SERVER_MODE, BROFO_SERVER_OPEN, BROFO_STORAGE_DRIVER, BROFO_STORAGE_PATH, BROFO_WEBHOOK_TIMEOUT,
// BROFO_WEBHOOK_ATTEMPTS, BROFO_SESSION_TTL, BROFO_MCP, BROFO_MCP_WRITES, BROFO_API_TOKEN.
//
// Mutable state (the database, tokens, logs) lives in the state directory: $XDG_STATE_HOME/browser-foreman, else
// ~/.local/state/browser-foreman.
package config

import (
	"bufio"
	"crypto/tls"
	_ "embed"
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
	Addr string `json:"addr" env:"BROFO_SERVER_ADDR" ui:"risky" help:"Address and port the server listens on."` // host:port; 127.0.0.1 unless you choose otherwise
	Mode Mode   `json:"mode" env:"BROFO_SERVER_MODE" ui:"risky" choices:"local,accounts" help:"local: one owner on this computer, no sign-in. accounts: people sign in."`
	// Open says whether serve opens its page in the default browser when it starts: auto (when started from a
	// terminal), always or never.
	Open string `json:"open" env:"BROFO_SERVER_OPEN" choices:"auto,always,never" help:"Open the page in your browser when the server starts."`
	// TLSCert and TLSKey serve https directly: for accounts mode reached from other computers, so passwords,
	// session cookies and tokens are not sent in clear text. Both or neither.
	TLSCert string `json:"tls_cert,omitempty" env:"BROFO_SERVER_TLS_CERT" ui:"risky" help:"A PEM certificate file to serve https with. Needs tls_key."`
	TLSKey  string `json:"tls_key,omitempty" env:"BROFO_SERVER_TLS_KEY" ui:"risky" help:"The certificate's PEM private key file."`
	// TrustedProxy lists the addresses or ranges (CIDR) of a TLS proxy in front of the server. Only from those are
	// X-Forwarded-Proto (is the visit https?) and X-Forwarded-For (who is the client?) believed.
	TrustedProxy []string `json:"trusted_proxy,omitempty" ui:"risky" help:"Addresses or CIDR ranges of a TLS proxy in front of the server; only its X-Forwarded headers are believed."`
}

// Behind says whether ip is one of the trusted proxies.
func (s Server) Behind(ip net.IP) bool {
	for _, p := range s.TrustedProxy {
		if _, n, err := net.ParseCIDR(p); err == nil && n.Contains(ip) {
			return true
		}
		if q := net.ParseIP(p); q != nil && q.Equal(ip) {
			return true
		}
	}
	return false
}

type Storage struct {
	Driver string `json:"driver" env:"BROFO_STORAGE_DRIVER" ui:"risky" choices:"sqlite,json" help:"Where data is kept. Move it first with storage migrate."` // sqlite or json
	Path   string `json:"path" env:"BROFO_STORAGE_PATH" ui:"risky" help:"The data folder; empty uses the state folder."`                                     // empty: the state directory
}

type Webhooks struct {
	Timeout  Duration `json:"timeout" env:"BROFO_WEBHOOK_TIMEOUT" help:"How long one delivery attempt may take (1s to 2m)."`    // per delivery attempt
	Attempts int      `json:"attempts" env:"BROFO_WEBHOOK_ATTEMPTS" help:"Tries before a delivery counts as failed (1 to 20)."` // tries before a delivery is marked failed
}

// Auth settings: how long a sign-in lasts.
type Auth struct {
	SessionTTL Duration `json:"session_ttl" env:"BROFO_SESSION_TTL" help:"How long a sign-in lasts (5m to 720h)."`
}

// MCP is the Model Context Protocol server (`browser-foreman mcp`): off unless Enabled, read-only unless Writes.
type MCP struct {
	Enabled bool `json:"enabled" env:"BROFO_MCP" help:"Let an assistant reach Browser Foreman over MCP."`
	Writes  bool `json:"writes" env:"BROFO_MCP_WRITES" help:"Let it change bookmarks too, not only read them."`
	// Folders limits writes to what is inside these folders (paths like "Bookmarks Bar/Inbox"); empty: anywhere.
	Folders []string `json:"folders,omitempty" help:"Limit its changes to these folders, one per line, like Bookmarks Bar/Inbox."`
	// Clients gives one MCP client, by the name it reports (clientInfo.name, such as "claude-code"), its own writes
	// and folders in place of the two above.
	Clients map[string]MCPScope `json:"clients,omitempty" ui:"file" help:"Per-client rights; edit this in the file."`
}

// MCPScope is what one MCP client may change.
type MCPScope struct {
	Writes  bool     `json:"writes"`
	Folders []string `json:"folders,omitempty"`
}

// Writeback is how `browser-foreman apply` writes a cleaned workspace into a browser: each choice is a value, or
// "ask" (the default) to be asked every time.
type Writeback struct {
	Read  string `json:"read" choices:"ask,file,extension" help:"Where a write-back reads the browser's bookmarks first."` // where the dry run reads the profile now: file, extension or ask
	Route string `json:"route" choices:"ask,file,extension" help:"How a write-back reaches the browser."`                  // how the write reaches it: file, extension or ask
	Mode  string `json:"mode" choices:"ask,merge,replace" help:"Merge into the browser's bookmarks, or replace them."`     // merge, replace or ask
}

// AI is who answers AI jobs (suggested tags, duplicate verdicts) as soon as they are made: nobody (none, the
// default: an MCP client takes them), Anthropic's API (anthropic), OpenAI's API (openai) or a local Ollama (ollama). Jobs are made only when
// the owner asks, and only then does anything about the bookmarks leave this computer.
type AI struct {
	Provider  string `json:"provider" env:"BROFO_AI_PROVIDER" ui:"live" choices:"none,anthropic,openai,ollama" help:"Who answers AI jobs. none leaves them for your assistant."` // none, anthropic, openai or ollama
	Model     string `json:"model,omitempty" env:"BROFO_AI_MODEL" ui:"live" help:"The model. Needed for OpenAI and Ollama; Anthropic uses claude-opus-5-5 when empty."`          // anthropic: claude-opus-5-5 when empty; openai and ollama: required
	Effort    string `json:"effort,omitempty" ui:"live" choices:"low,medium,high" help:"How hard Anthropic's model thinks."`                                                     // anthropic: low, medium or high; empty: low
	OllamaURL string `json:"ollama_url,omitempty" ui:"live" help:"Ollama's address."`                                                                                            // http://127.0.0.1:11434 when empty
	OpenAIURL string `json:"openai_url,omitempty" ui:"live,risky" help:"OpenAI's address, or any server with the same API. Your OpenAI key is sent there."`                      // https://api.openai.com/v1 when empty; any server speaking the same API
	// OpenAIKey is OPENAI_API_KEY, from .env or the environment only.
	OpenAIKey string `json:"-" env:"OPENAI_API_KEY" ui:"secret,live" help:"Your OpenAI API key. Kept in .env, never shown again."`
	// AnthropicKey is ANTHROPIC_API_KEY, from .env or the environment only; empty lets the SDK use `ant auth login`.
	AnthropicKey string `json:"-" env:"ANTHROPIC_API_KEY" ui:"secret,live" help:"Your Anthropic API key. Kept in .env, never shown again."`
}

// Notify is desktop notifications (through go-notifykit): one banner when a scheduled link re-check finds links
// that stopped working or work again. BROFO_NOTIFY_DESKTOP=0 turns them off for one run; go-notifykit reads it.
type Notify struct {
	Desktop bool `json:"desktop" help:"A notification when a scheduled re-check finds links newly dead or fixed."`
}

// Settings is how much the web page's Settings may change: safe (the default) leaves the server's address, mode,
// storage and API token to the files; all lets the page change those too, each after a confirmation. Only the
// files and the environment can set this one.
type Settings struct {
	Edit string `json:"edit" env:"BROFO_SETTINGS_EDIT" ui:"file" choices:"safe,all" help:"safe: the page cannot change the address, mode, storage or token. all: it can, after a confirmation."`
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
	Notify    Notify    `json:"notify"`
	AI        AI        `json:"ai"`
	Settings  Settings  `json:"settings"`
	APIToken  string    `json:"-" env:"BROFO_API_TOKEN" ui:"secret,risky" help:"The token the page and the CLI use. Changing it signs out every page."`
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
		Server:    Server{Addr: "127.0.0.1:8765", Mode: ModeLocal, Open: "auto"},
		Storage:   Storage{Driver: "sqlite"},
		Webhooks:  Webhooks{Timeout: Duration{10 * time.Second}, Attempts: 5},
		Auth:      Auth{SessionTTL: Duration{24 * time.Hour}},
		Writeback: Writeback{Read: "ask", Route: "ask", Mode: "ask"},
		Notify:    Notify{Desktop: true},
		AI:        AI{Provider: "none", Effort: "low", OllamaURL: "http://127.0.0.1:11434", OpenAIURL: "https://api.openai.com/v1"},
		Settings:  Settings{Edit: "safe"},
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
	files, err := readJSONFiles(p.Config)
	if err != nil {
		return Defaults(), err
	}
	envFile := filepath.Join(p.Config, ".env")
	env, err := readDotEnv(envFile)
	hasEnv := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Defaults(), err
	}
	return build(p, files, env, hasEnv, getenv)
}

// jsonFile is one *.json file of the config directory: its name and contents.
type jsonFile struct {
	Name string
	Body []byte
}

// readJSONFiles reads every *.json in dir, in name order.
func readJSONFiles(dir string) ([]jsonFile, error) {
	names, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(names)
	out := make([]jsonFile, 0, len(names))
	for _, f := range names {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		out = append(out, jsonFile{Name: filepath.Base(f), Body: b})
	}
	return out, nil
}

// build is Load on contents already read: so a change can be checked before it is written.
func build(p Paths, files []jsonFile, env map[string]string, hasEnv bool, getenv func(string) string) (Config, error) {
	c := Defaults()
	for _, f := range files {
		dec := json.NewDecoder(strings.NewReader(string(f.Body)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return c, fmt.Errorf("%s: %w", filepath.Join(p.Config, f.Name), err)
		}
		c.Sources = append(c.Sources, filepath.Join(p.Config, f.Name))
	}
	if hasEnv {
		c.Sources = append(c.Sources, filepath.Join(p.Config, ".env"))
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
		"BROFO_SERVER_OPEN":      func(v string) error { c.Server.Open = v; return nil },
		"BROFO_SERVER_TLS_CERT":  func(v string) error { c.Server.TLSCert = v; return nil },
		"BROFO_SERVER_TLS_KEY":   func(v string) error { c.Server.TLSKey = v; return nil },
		"BROFO_STORAGE_DRIVER":   func(v string) error { c.Storage.Driver = v; return nil },
		"BROFO_STORAGE_PATH":     func(v string) error { c.Storage.Path = v; return nil },
		"BROFO_API_TOKEN":        func(v string) error { c.APIToken = v; return nil },
		"BROFO_AI_PROVIDER":      func(v string) error { c.AI.Provider = v; return nil },
		"BROFO_AI_MODEL":         func(v string) error { c.AI.Model = v; return nil },
		"ANTHROPIC_API_KEY":      func(v string) error { c.AI.AnthropicKey = v; return nil },
		"OPENAI_API_KEY":         func(v string) error { c.AI.OpenAIKey = v; return nil },
		"BROFO_SETTINGS_EDIT":    func(v string) error { c.Settings.Edit = v; return nil },
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
	if (c.Server.TLSCert == "") != (c.Server.TLSKey == "") {
		return fmt.Errorf("server.tls_cert and server.tls_key go together: set both, or neither")
	}
	if c.Server.TLSCert != "" {
		if _, err := tls.LoadX509KeyPair(c.Server.TLSCert, c.Server.TLSKey); err != nil {
			return fmt.Errorf("server.tls_cert/tls_key: %w", err)
		}
	}
	for _, p := range c.Server.TrustedProxy {
		if _, _, err := net.ParseCIDR(p); err != nil && net.ParseIP(p) == nil {
			return fmt.Errorf("server.trusted_proxy %q: use an address or a CIDR range", p)
		}
	}
	if o := c.Server.Open; o != "auto" && o != "always" && o != "never" {
		return fmt.Errorf("server.open %q: use auto, always or never", o)
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
	switch c.AI.Provider {
	case "none", "anthropic":
	case "ollama":
		if c.AI.Model == "" {
			return errors.New("ai.provider ollama needs ai.model: a model you have pulled (`ollama list` shows them)")
		}
	case "openai":
		if c.AI.Model == "" {
			return errors.New("ai.provider openai needs ai.model: an OpenAI model your key can use")
		}
	default:
		return fmt.Errorf("ai.provider %q: use none, anthropic, openai or ollama", c.AI.Provider)
	}
	if c.Settings.Edit != "safe" && c.Settings.Edit != "all" {
		return fmt.Errorf("settings.edit %q: use safe or all", c.Settings.Edit)
	}
	if e := c.AI.Effort; e != "" && e != "low" && e != "medium" && e != "high" {
		return fmt.Errorf("ai.effort %q: use low, medium or high", e)
	}
	if err := checkFolders("mcp.folders", c.MCP.Folders); err != nil {
		return err
	}
	for name, sc := range c.MCP.Clients {
		if strings.TrimSpace(name) == "" {
			return errors.New("mcp.clients: a client needs its name")
		}
		if err := checkFolders("mcp.clients."+name+".folders", sc.Folders); err != nil {
			return err
		}
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

// checkFolders refuses a folder path that is empty or starts or ends with "/": paths are written as the page shows
// them, "Bookmarks Bar/Inbox".
func checkFolders(key string, folders []string) error {
	for _, f := range folders {
		if strings.TrimSpace(f) == "" || strings.HasPrefix(f, "/") || strings.HasSuffix(f, "/") {
			return fmt.Errorf("%s: %q is not a folder path like \"Bookmarks Bar/Inbox\"", key, f)
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

// EnvExample is the .env the app ships: every variable it reads, empty, with what each is for. Init creates the
// real .env from it, with a fresh API token.
//
//go:embed env.example
var EnvExample string

// Init writes a starting config.json, and an .env made from EnvExample with a fresh API token, refusing to
// overwrite either.
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
	env, err := setEnvLine(EnvExample, "BROFO_API_TOKEN", token)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{
		"config.json": append(body, '\n'),
		".env":        []byte(env),
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
