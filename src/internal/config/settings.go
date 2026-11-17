package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// The settings the web page edits. Each comes from Config's own struct tags, so the page, the files and the loader
// cannot disagree about what exists:
//
//	json     the key in a config file ("ai.provider"); "-" for a value kept only in .env
//	env      the variable that sets it (BROFO_AI_PROVIDER, OPENAI_API_KEY)
//	choices  the values it takes, when it takes a fixed few
//	ui       secret (write-only, kept in .env), risky (changes who can reach the server or where data lives:
//	         editable only with settings.edit all, after a confirmation), live (takes effect without a restart),
//	         file (edited in the file only)
//	help     one line for the page

// Field is one setting as the page shows it. A secret's value is never included: only whether it is set.
type Field struct {
	Key      string   `json:"key"` // "ai.provider", or the variable for a value kept only in .env
	Group    string   `json:"group"`
	Env      string   `json:"env,omitempty"`
	Type     string   `json:"type"` // string, choice, bool, int, duration, list, secret, map
	Choices  []string `json:"choices,omitempty"`
	Value    any      `json:"value,omitempty"`
	Set      bool     `json:"set"`    // a secret has a value; for the rest, it is set somewhere, not left to the default
	Source   string   `json:"source"` // default, a config file's name, .env, or environment
	Editable bool     `json:"editable"`
	Locked   string   `json:"locked,omitempty"` // why it cannot be changed from the page
	Risky    bool     `json:"risky,omitempty"`
	Live     bool     `json:"live,omitempty"` // applies at once; the rest after a restart
	Help     string   `json:"help,omitempty"`

	path  []string     // json path, nil for a .env-only value
	index []int        // the field in Config
	kind  reflect.Type // its Go type
}

var durationType = reflect.TypeOf(Duration{})

// catalog lists every setting, in Config's order.
func catalog() []Field {
	var out []Field
	ct := reflect.TypeOf(Config{})
	for i := 0; i < ct.NumField(); i++ {
		sf := ct.Field(i)
		if sf.Type.Kind() == reflect.Struct && sf.Type != durationType {
			section := jsonName(sf)
			for j := 0; j < sf.Type.NumField(); j++ {
				if f, ok := field(sf.Type.Field(j), section, []int{i, j}); ok {
					out = append(out, f)
				}
			}
			continue
		}
		if f, ok := field(sf, "server", []int{i}); ok { // the API token
			out = append(out, f)
		}
	}
	return out
}

func jsonName(sf reflect.StructField) string {
	n, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
	return n
}

func field(sf reflect.StructField, section string, index []int) (Field, bool) {
	name := jsonName(sf)
	env := sf.Tag.Get("env")
	if name == "-" && env == "" {
		return Field{}, false // Sources and the like
	}
	ui := "," + sf.Tag.Get("ui") + ","
	f := Field{Group: section, Env: env, Help: sf.Tag.Get("help"), index: index, kind: sf.Type,
		Risky: strings.Contains(ui, ",risky,"), Live: strings.Contains(ui, ",live,")}
	if c := sf.Tag.Get("choices"); c != "" {
		f.Choices = strings.Split(c, ",")
	}
	if name == "-" {
		f.Key = env
	} else {
		f.Key = section + "." + name
		f.path = []string{section, name}
	}
	switch {
	case strings.Contains(ui, ",secret,"):
		f.Type = "secret"
	case f.Choices != nil:
		f.Type = "choice"
	case sf.Type == durationType:
		f.Type = "duration"
	case sf.Type.Kind() == reflect.Bool:
		f.Type = "bool"
	case sf.Type.Kind() == reflect.Int:
		f.Type = "int"
	case sf.Type.Kind() == reflect.Slice:
		f.Type = "list"
	case sf.Type.Kind() == reflect.Map:
		f.Type = "map"
	default:
		f.Type = "string"
	}
	if strings.Contains(ui, ",file,") || f.Type == "map" {
		f.Locked = "Edited in the config file only."
	}
	return f, true
}

// Snapshot is the files the settings live in, as read once.
type snapshot struct {
	p      Paths
	files  []jsonFile
	env    string // .env as written, comments and all
	hasEnv bool
	getenv func(string) string
}

func readSnapshot(p Paths, getenv func(string) string) (snapshot, error) {
	files, err := readJSONFiles(p.Config)
	if err != nil {
		return snapshot{}, err
	}
	s := snapshot{p: p, files: files, getenv: getenv}
	if _, err := readDotEnv(filepath.Join(p.Config, ".env")); err == nil { // also refuses a .env others can read
		b, err := os.ReadFile(filepath.Join(p.Config, ".env"))
		if err != nil {
			return s, err
		}
		s.env, s.hasEnv = string(b), true
	} else if !errors.Is(err, os.ErrNotExist) {
		return s, err
	}
	return s, nil
}

func (s snapshot) load() (Config, error) {
	return build(s.p, s.files, parseEnv(s.env), s.hasEnv, s.getenv)
}

// source says where a setting's value comes from: the environment wins, then .env, then the last config file
// that sets it, else the default.
func (s snapshot) source(f Field) string {
	if f.Env != "" && s.getenv(f.Env) != "" {
		return "environment"
	}
	if f.Env != "" && parseEnv(s.env)[f.Env] != "" {
		return ".env"
	}
	if f.path != nil {
		for i := len(s.files) - 1; i >= 0; i-- {
			var m map[string]any
			if json.Unmarshal(s.files[i].Body, &m) == nil {
				if sec, ok := m[f.path[0]].(map[string]any); ok {
					if _, ok := sec[f.path[1]]; ok {
						return s.files[i].Name
					}
				}
			}
		}
	}
	return "default"
}

// Describe lists every setting with its value (never a secret's), where it comes from, and whether the page may
// change it under c's settings.edit.
func Describe(p Paths, getenv func(string) string) ([]Field, string, error) {
	s, err := readSnapshot(p, getenv)
	if err != nil {
		return nil, "", err
	}
	c, err := s.load()
	if err != nil {
		return nil, "", err
	}
	mode := c.Settings.Edit
	out := catalog()
	cv := reflect.ValueOf(c)
	for i := range out {
		f := &out[i]
		v := cv.FieldByIndex(f.index)
		f.Source = s.source(*f)
		if f.Type == "secret" {
			f.Set = v.String() != ""
		} else {
			f.Value = shown(v)
			f.Set = f.Source != "default"
		}
		f.Editable, f.Locked = editable(*f, mode)
	}
	return out, mode, nil
}

func shown(v reflect.Value) any {
	if v.Type() == durationType {
		return v.Interface().(Duration).String()
	}
	if v.Kind() == reflect.Slice && v.IsNil() {
		return []string{}
	}
	return v.Interface()
}

func editable(f Field, mode string) (bool, string) {
	switch {
	case f.Locked != "":
		return false, f.Locked
	case f.Source == "environment":
		return false, "Set by " + f.Env + " in the environment the server started with, which wins over the files."
	case f.Risky && mode != "all":
		return false, `Changes who can reach the server or where data lives: edit it in the file, or set settings.edit to "all" there.`
	}
	return true, ""
}

// Errors Apply returns, for the API to answer with.
var (
	ErrUnknown = errors.New("no such setting")
	ErrLocked  = errors.New("this setting cannot be changed from the page")
	ErrConfirm = errors.New("this setting changes who can reach the server or where data lives: confirm it")
)

// Result is what Apply changed.
type Result struct {
	Changed []string `json:"changed"`
	Restart []string `json:"restart"` // changed settings that take effect when the server restarts
	Files   []string `json:"files"`   // the files written
}

// Apply sets settings by key ("ai.model", "OPENAI_API_KEY") from JSON values. A secret goes to .env, and "" clears
// it. A setting already in .env stays there; any other goes to the last config file that sets it, else
// config.json. The whole change is checked by the same loader the server starts with, and nothing is written
// unless all of it passes. A risky setting needs settings.edit "all" and confirmed.
func Apply(p Paths, getenv func(string) string, values map[string]json.RawMessage, confirmed bool) (Result, error) {
	s, err := readSnapshot(p, getenv)
	if err != nil {
		return Result{}, err
	}
	c, err := s.load()
	if err != nil {
		return Result{}, err
	}
	byKey := map[string]Field{}
	for _, f := range catalog() {
		f.Source = s.source(f)
		byKey[f.Key] = f
	}
	res := Result{Changed: []string{}, Restart: []string{}, Files: []string{}} // lists, never null, in JSON
	envText := s.env
	jsonMaps := map[string]map[string]any{}
	touchedEnv := false
	for key, raw := range values {
		f, ok := byKey[key]
		if !ok {
			return Result{}, fmt.Errorf("%w: %s", ErrUnknown, key)
		}
		if ok, why := editable(f, c.Settings.Edit); !ok {
			return Result{}, fmt.Errorf("%w: %s: %s", ErrLocked, key, why)
		}
		if f.Risky && !confirmed {
			return Result{}, fmt.Errorf("%w: %s", ErrConfirm, key)
		}
		v, err := decodeValue(f, raw)
		if err != nil {
			return Result{}, fmt.Errorf("%s: %w", key, err)
		}
		if f.Type == "secret" || f.Source == ".env" {
			text, err := envValue(f, v)
			if err != nil {
				return Result{}, fmt.Errorf("%s: %w", key, err)
			}
			if envText, err = setEnvLine(envText, f.Env, text); err != nil {
				return Result{}, fmt.Errorf("%s: %w", key, err)
			}
			touchedEnv = true
		} else {
			name := f.Source
			if name == "default" {
				name = "config.json"
			}
			m, err := s.jsonMap(jsonMaps, name)
			if err != nil {
				return Result{}, err
			}
			sec, _ := m[f.path[0]].(map[string]any)
			if sec == nil {
				sec = map[string]any{}
				m[f.path[0]] = sec
			}
			if v == nil {
				delete(sec, f.path[1])
			} else {
				sec[f.path[1]] = v
			}
		}
		res.Changed = append(res.Changed, key)
		if !f.Live {
			res.Restart = append(res.Restart, key)
		}
	}
	// Check the whole change as the server will read it.
	next := s
	next.files = append([]jsonFile(nil), s.files...)
	written := map[string][]byte{}
	for name, m := range jsonMaps {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return Result{}, err
		}
		b = append(b, '\n')
		written[name] = b
		found := false
		for i := range next.files {
			if next.files[i].Name == name {
				next.files[i].Body, found = b, true
			}
		}
		if !found {
			next.files = append(next.files, jsonFile{Name: name, Body: b})
		}
	}
	if touchedEnv {
		next.env, next.hasEnv = envText, true
	}
	if _, err := next.load(); err != nil {
		return Result{}, err
	}
	if touchedEnv {
		written[".env"] = []byte(envText)
	}
	for name, b := range written {
		if err := writeAtomic(filepath.Join(p.Config, name), b); err != nil {
			return res, err
		}
		res.Files = append(res.Files, name)
	}
	return res, nil
}

func (s snapshot) jsonMap(cache map[string]map[string]any, name string) (map[string]any, error) {
	if m, ok := cache[name]; ok {
		return m, nil
	}
	m := map[string]any{}
	for _, f := range s.files {
		if f.Name == name {
			if err := json.Unmarshal(f.Body, &m); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	cache[name] = m
	return m, nil
}

// decodeValue reads a JSON value for f; nil means "back to the default" (an empty string, or an empty list).
func decodeValue(f Field, raw json.RawMessage) (any, error) {
	switch f.Type {
	case "bool":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, errors.New("expected true or false")
		}
		return b, nil
	case "int":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, errors.New("expected a whole number")
		}
		return n, nil
	case "list":
		var l []string
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, errors.New("expected a list of text")
		}
		clean := []string{}
		for _, x := range l {
			if x = strings.TrimSpace(x); x != "" {
				clean = append(clean, x)
			}
		}
		if len(clean) == 0 {
			return nil, nil
		}
		return clean, nil
	}
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return nil, errors.New("expected text")
	}
	str = strings.TrimSpace(str)
	if strings.ContainsAny(str, "\r\n") {
		return nil, errors.New("must be one line")
	}
	if str == "" {
		if f.Type == "secret" {
			return "", nil
		}
		return nil, nil
	}
	switch f.Type {
	case "duration":
		if _, err := time.ParseDuration(str); err != nil {
			return nil, fmt.Errorf("%q is not a duration like 10s, 5m or 24h", str)
		}
	case "choice":
		ok := false
		for _, c := range f.Choices {
			ok = ok || c == str
		}
		if !ok {
			return nil, fmt.Errorf("use one of %s", strings.Join(f.Choices, ", "))
		}
	}
	return str, nil
}

// envValue is v as a .env value.
func envValue(f Field, v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool, int:
		return fmt.Sprint(x), nil
	}
	return "", fmt.Errorf("%s cannot be kept in .env; set it in a config file", f.Key)
}

// setEnvLine sets key in .env text, keeping every other line (comments included) as it was: the line that sets it
// is replaced, or the key is added at the end. Values with spaces or # are quoted.
func setEnvLine(text, key, value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\"") {
		return "", errors.New("a value in .env must be one line without double quotes")
	}
	if strings.ContainsAny(value, " #'\t") {
		value = `"` + value + `"`
	}
	line := key + "=" + value
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		t := strings.TrimPrefix(strings.TrimSpace(l), "export ")
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			lines[i] = line
			return strings.Join(lines, "\n"), nil
		}
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + line + "\n", nil
}

// parseEnv reads .env text as readDotEnv does, without the file checks.
func parseEnv(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// writeAtomic replaces path with b, 0600, through a file beside it, so a crash leaves the old file or the new one.
func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // a no-op once renamed
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
