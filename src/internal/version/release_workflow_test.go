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
