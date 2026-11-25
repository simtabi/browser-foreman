package version

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var updateNotices = flag.Bool("update-notices", false, "rewrite THIRD_PARTY_NOTICES from the dependencies (make notices)")

// The release ships THIRD_PARTY_NOTICES beside the binaries: the licence and notice texts of every Go module linked
// into browser-foreman and every npm package bundled into its web page, as their authors wrote them. This fails when
// the file no longer matches the dependencies; `make notices` rewrites it.
func TestThirdPartyNoticesAreCurrent(t *testing.T) {
	path := filepath.Join("..", "..", "..", "THIRD_PARTY_NOTICES")
	goPart, modules := goNotices(t)
	npmPart, packages, haveNode := npmNotices(t)
	// Measured 2026-11-25: 45 Go modules linked on one release system or another, 3 npm packages bundled.
	if modules < 30 || (haveNode && packages < 3) {
		t.Fatalf("found %d Go modules and %d npm packages; the dependency walk has lost its way", modules, packages)
	}
	if *updateNotices {
		if !haveNode {
			t.Fatal("web/node_modules is missing: run npm ci in web first, so the bundled packages are included")
		}
		if err := os.WriteFile(path, []byte(noticesHeader+goPart+npmPart), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v: run make notices", err)
	}
	have := string(b)
	// Without web/node_modules (the Go-only CI job) the npm half cannot be rebuilt, so only the Go half is compared.
	want := noticesHeader + goPart
	if haveNode {
		want += npmPart
	} else if i := strings.Index(have, npmHeading); i >= 0 {
		have = have[:i]
	}
	if have != want {
		t.Error("THIRD_PARTY_NOTICES is out of date with the dependencies: run make notices")
	}
}

const (
	noticesHeader = "Browser Foreman includes the third-party software below. Each entry carries the licence and notice\n" +
		"files its authors ship with it. Written by `make notices` from the dependencies; do not edit by hand.\n"
	npmHeading = "\n\n#### Bundled into the web page (npm)\n"
	rule       = "\n\n================================================================================\n"
)

// licenceFile is a file a package ships its terms in: LICENSE, LICENCE, COPYING, NOTICE, with any extension.
var licenceFile = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice)([-._].*)?$`)

// entry renders one package: its name and version, then each licence file in dir, in name order. declared is the
// licence an npm package names in its package.json, used only when it ships no licence file.
func entry(t *testing.T, name, version, dir string, declared *declaration) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(rule + name + " " + version + "\n")
	found := 0
	for _, e := range ents {
		if e.IsDir() || !licenceFile.MatchString(e.Name()) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		found++
		fmt.Fprintf(&b, "\n--- %s\n\n%s\n", e.Name(), strings.TrimRight(strings.ReplaceAll(string(text), "\r\n", "\n"), "\n"))
	}
	switch {
	case found > 0:
	case declared != nil && declared.License == "MIT" && declared.Author != "":
		// The MIT text is fixed but for the holder, which the package names as its author.
		fmt.Fprintf(&b, "\n--- MIT, as declared in its package.json (the package ships no licence file; source: %s)\n\n%s\n",
			declared.Repository, strings.Replace(mitText, "{holder}", declared.Author, 1))
	default:
		t.Errorf("%s %s ships no licence file in %s", name, version, dir)
	}
	return b.String()
}

// declaration is what an npm package says about its licence in package.json.
type declaration struct{ License, Author, Repository string }

const mitText = `Copyright (c) {holder}

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.`

// goNotices lists the modules `go list -deps` links into the command on any system the release builds for (a module
// can be linked on one system only), from the module cache.
func goNotices(t *testing.T) (string, int) {
	t.Helper()
	cfg, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	systems := map[string]bool{}
	for target := range releaseTargets(string(cfg)) {
		systems[strings.Split(target, "/")[0]] = true
	}
	if len(systems) < 4 {
		t.Fatalf("read %d systems from src/.goreleaser.yaml", len(systems))
	}
	seen := map[string]bool{}
	var lines []string
	for goos := range systems {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}", "./cmd/browser-foreman")
		cmd.Dir = filepath.Join("..", "..")
		cmd.Env = append(os.Environ(), "GOOS="+goos)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list for %s: %v", goos, err)
		}
		for _, l := range strings.Split(string(out), "\n") {
			if l != "" && !seen[l] {
				seen[l] = true
				lines = append(lines, l)
			}
		}
	}
	sort.Strings(lines)
	b := strings.Builder{}
	b.WriteString("\n\n#### Linked into the program (Go modules)\n")
	for _, l := range lines {
		f := strings.SplitN(l, " ", 3)
		if len(f) != 3 || f[2] == "" {
			t.Fatalf("no module folder for %q: run go mod download", l)
		}
		b.WriteString(entry(t, f[0], f[1], f[2], nil))
	}
	return b.String(), len(lines)
}

// npmNotices lists the packages web/package.json depends on (not its devDependencies), and theirs, from
// web/node_modules: what esbuild bundles into the page. haveNode is false when node_modules is absent.
func npmNotices(t *testing.T) (part string, n int, haveNode bool) {
	t.Helper()
	web := filepath.Join("..", "..", "..", "web")
	if _, err := os.Stat(filepath.Join(web, "node_modules")); err != nil {
		return "", 0, false
	}
	type manifest struct {
		Version      string            `json:"version"`
		License      string            `json:"license"`
		Author       any               `json:"author"` // "Name <mail>" or {"name": ...}
		Repository   any               `json:"repository"`
		Dependencies map[string]string `json:"dependencies"`
	}
	field := func(v any, key string) string {
		if m, ok := v.(map[string]any); ok {
			v = m[key]
		}
		s, _ := v.(string)
		return s
	}
	read := func(dir string) manifest {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		var m manifest
		if err := json.NewDecoder(bytes.NewReader(b)).Decode(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	found := map[string]manifest{}
	var walk func(deps map[string]string)
	walk = func(deps map[string]string) {
		for name := range deps {
			if _, ok := found[name]; ok {
				continue
			}
			m := read(filepath.Join(web, "node_modules", name))
			found[name] = m
			walk(m.Dependencies)
		}
	}
	walk(read(web).Dependencies)
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(npmHeading)
	for _, name := range names {
		m := found[name]
		d := &declaration{License: m.License, Author: field(m.Author, "name"), Repository: field(m.Repository, "url")}
		b.WriteString(entry(t, name, m.Version, filepath.Join(web, "node_modules", name), d))
	}
	return b.String(), len(names), true
}

// The release must carry the notices and the licence: its assets are bare binaries, with nothing bundled beside them.
func TestTheReleaseShipsTheNotices(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	extra := strings.Join(block(string(b), "release"), "\n")
	for _, f := range []string{"./LICENSE", "./THIRD_PARTY_NOTICES"} {
		if !strings.Contains(extra, "glob: "+f) {
			t.Errorf("src/.goreleaser.yaml's release block does not ship %s as an extra file", f)
		}
	}
}
