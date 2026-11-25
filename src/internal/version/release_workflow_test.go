package version

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// block returns the lines indented under the top-level key `name:` in a YAML file, or nil when it is absent.
func block(src, name string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		top := !strings.HasPrefix(line, " ")
		if top {
			if in {
				break
			}
			in = line == name+":"
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return out
}

// keysAt returns the keys at exactly `indent` spaces among lines.
func keysAt(lines []string, indent int) []string {
	re := regexp.MustCompile(`^` + strings.Repeat(" ", indent) + `([A-Za-z_][A-Za-z0-9_-]*):`)
	var keys []string
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			keys = append(keys, m[1])
		}
	}
	sort.Strings(keys)
	return keys
}

// A release builds on a pushed version tag, or on request naming one, and nowhere but the GitHub release
// (owner, 2026-11-13). release.yml is a thin caller of simtabi/.github's shared go-release.yml: any other
// trigger (a branch push, pull_request, schedule, release) or a pipeline of its own fails here.
func TestReleaseRunsOnAVersionTagOrOnRequest(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(b)
	on := block(wf, "on")
	if got := strings.Join(keysAt(on, 2), ","); got != "push,workflow_dispatch" {
		t.Fatalf("release.yml triggers [%s], want exactly push (version tags) and workflow_dispatch", got)
	}

	var push, dispatch []string
	cur := ""
	for _, l := range on {
		if m := regexp.MustCompile(`^  ([a-z_]+):`).FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		switch cur {
		case "push":
			push = append(push, l)
		case "workflow_dispatch":
			dispatch = append(dispatch, l)
		}
	}
	if got := strings.Join(keysAt(push, 4), ","); got != "tags" {
		t.Fatalf("release.yml push filters [%s]; it must push-trigger on version tags only, never a branch", got)
	}
	tags := 0
	for _, l := range push {
		if m := regexp.MustCompile(`^\s+-\s+'?"?([^'"]+)`).FindStringSubmatch(l); m != nil {
			tags++
			if !strings.HasPrefix(m[1], "v[0-9]+.") {
				t.Errorf("tag filter %q is not a version tag", m[1])
			}
		}
	}
	if tags == 0 {
		t.Fatal("release.yml has no version tag filter")
	}
	d := strings.Join(dispatch, "\n")
	if !regexp.MustCompile(`(?m)^      tag:\n(        .*\n)*        required: true`).MatchString(d + "\n") {
		t.Fatal("release.yml has no required tag input on workflow_dispatch")
	}

	jobs := block(wf, "jobs")
	if got := strings.Join(keysAt(jobs, 2), ","); got != "release" {
		t.Fatalf("release.yml jobs [%s], want one: release", got)
	}
	j := strings.Join(jobs, "\n")
	if !regexp.MustCompile(`(?m)^    uses: simtabi/\.github/\.github/workflows/go-release\.yml@[0-9a-f]{40}\b`).MatchString(j) {
		t.Fatal("the release job must call simtabi/.github's shared go-release.yml, pinned to a commit")
	}
	if !strings.Contains(j, "      tag: ${{ inputs.tag || github.ref_name }}") {
		t.Error("the release job must pass tag: ${{ inputs.tag || github.ref_name }}")
	}
	if regexp.MustCompile(`(?m)^    (steps|runs-on):`).MatchString(j) {
		t.Error("the release job runs steps of its own; the pipeline belongs in the shared workflow")
	}

	// The release builds the web interface (src/.goreleaser.yaml's `make web` hook) on the same Node
	// as CI's web job, not on whatever the shared workflow defaults to.
	ci, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	ciNode := regexp.MustCompile(`(?m)^\s+node-version:\s*'?"?([0-9][0-9.x]*)`).FindSubmatch(ci)
	if ciNode == nil {
		t.Fatal("ci.yml sets no node-version for the web job")
	}
	relNode := regexp.MustCompile(`(?m)^      node-version:\s*'?"?([0-9][0-9.x]*)`).FindStringSubmatch(j)
	if relNode == nil || relNode[1] != string(ciNode[1]) {
		t.Errorf("the release job's node-version is %v; want %q, the Node ci.yml's web job uses", relNode, ciNode[1])
	}
}

// The GitHub release is the only channel. go.md's list of GoReleaser blocks that publish anywhere else.
var otherChannels = []string{
	"brews", "homebrew_casks", "scoops", "winget", "chocolateys", "aurs", "aur_sources", "snapcrafts", "flatpaks",
	"nix", "dockers", "dockers_v2", "docker_manifests", "docker_signs", "kos", "krews", "npms", "cloudsmiths",
	"furies", "publishers", "blobs", "uploads", "artifactories",
}

func TestGoReleaserPublishesNowhereElse(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if !regexp.MustCompile(`(?m)^release:`).MatchString(cfg) {
		t.Fatal("src/.goreleaser.yaml has no release block; the GitHub release is the channel")
	}
	for _, k := range otherChannels {
		if regexp.MustCompile(`(?m)^` + k + `:`).MatchString(cfg) {
			t.Errorf("src/.goreleaser.yaml publishes through %q; binaries go to the GitHub release only", k)
		}
	}
}

// The release carries the tag's CHANGELOG section, which the shared workflow passes with --release-notes, and a
// pre-release tag (v0.1.0-rc.1) is published as a pre-release. `changelog: disable: true` drops those notes, and
// without `prerelease: auto` an rc tag becomes the latest release: v0.1.0-rc.1 shipped with both faults.
func TestGoReleaserKeepsTheNotesAndMarksPreReleases(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(b)
	if regexp.MustCompile(`(?m)^changelog:\s*\n\s+disable:\s*true`).MatchString(cfg) {
		t.Error("src/.goreleaser.yaml disables the changelog, which also drops the release notes the shared workflow passes")
	}
	if !regexp.MustCompile(`(?m)^release:\s*\n(?:\s+.*\n)*?\s+prerelease:\s*auto\b`).MatchString(cfg) {
		t.Error("src/.goreleaser.yaml's release block needs `prerelease: auto`, so an rc tag is not published as the latest release")
	}
}

// make lint runs the golangci-lint version CI runs, so a local pass means a CI pass.
func TestLocalLintIsCIsLint(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	local := regexp.MustCompile(`(?m)^GOLANGCI_VERSION := (v\S+)$`).FindSubmatch(mk)
	remote := regexp.MustCompile(`golangci-lint-action@\S+[^\n]*\n(?:[^\n]*\n){0,4}?\s+version: (v\S+)`).FindSubmatch(ci)
	if local == nil || remote == nil {
		t.Fatalf("version not found: Makefile %q, ci.yml %q", local, remote)
	}
	if string(local[1]) != string(remote[1]) {
		t.Fatalf("make lint runs %s, CI runs %s", local[1], remote[1])
	}
	if !strings.Contains(string(mk), "golangci-lint@$(GOLANGCI_VERSION) run") {
		t.Fatal("make lint does not run the pinned version")
	}
}

// make test runs the race detector, and CI runs make test, so a data race fails a pull request.
func TestTheTestsRunUnderTheRaceDetector(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	target := regexp.MustCompile(`(?m)^test:.*\n\t(.+)$`).FindSubmatch(mk)
	if target == nil || !strings.Contains(string(target[1]), "go -C src test -race") {
		t.Fatalf("make test does not run go test -race: %q", target)
	}
	if !regexp.MustCompile(`(?m)run: make .*\btest\b`).Match(ci) {
		t.Fatal("CI does not run make test")
	}
}

// CI and make check vet every system the release builds for, keep go.mod tidy and run govulncheck at a pinned version:
// a build-tagged file for another system is otherwise first compiled when a release tag is built.
func TestChecksCoverEverySystemAndTheModuleFile(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	oses := regexp.MustCompile(`(?m)^VET_OSES := (.+)$`).FindSubmatch(mk)
	if oses == nil {
		t.Fatal("no VET_OSES in the Makefile")
	}
	for _, want := range []string{"darwin", "linux", "windows", "freebsd"} {
		if !strings.Contains(" "+string(oses[1])+" ", " "+want+" ") {
			t.Errorf("make vet does not vet %s", want)
		}
	}
	if !regexp.MustCompile(`(?m)^GOVULNCHECK_VERSION := v\d+\.\d+\.\d+$`).Match(mk) {
		t.Error("govulncheck is not pinned to a release")
	}
	check := regexp.MustCompile(`(?m)^check: (.*?)  ##`).FindSubmatch(mk)
	for _, step := range []string{"fmt", "vet", "tidy", "lint", "vuln", "test"} {
		if check == nil || !strings.Contains(" "+string(check[1])+" ", " "+step+" ") {
			t.Errorf("make check leaves out %s", step)
		}
	}
	run := regexp.MustCompile(`(?m)run: make (.*)$`).FindAllSubmatch(ci, -1)
	got := ""
	for _, r := range run {
		got += " " + string(r[1]) + " "
	}
	for _, step := range []string{"vet", "tidy", "vuln", "test"} {
		if !strings.Contains(got, " "+step+" ") {
			t.Errorf("CI does not run make %s", step)
		}
	}
}
