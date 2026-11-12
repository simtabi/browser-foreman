package version

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The release workflow must only run when dispatched by hand with confirm=release-go (Go releases need the owner's
// go each time). Any push, schedule, pull_request or release trigger would publish without it.
func TestReleaseRunsOnlyOnAConfirmedDispatch(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	wf := string(b)
	on := wf[strings.Index(wf, "\non:")+1:]
	on = on[:strings.Index(on, "\nconcurrency:")]
	for _, trig := range []string{"push", "schedule", "pull_request", "release", "workflow_run"} {
		if regexp.MustCompile(`(?m)^  ` + trig + `:`).MatchString(on) {
			t.Errorf("release.yml has a %s trigger", trig)
		}
	}
	if !strings.Contains(on, "workflow_dispatch:") || !strings.Contains(wf, `"${{ inputs.confirm }}" = "release-go"`) {
		t.Error("release.yml must be workflow_dispatch only and refuse without confirm=release-go")
	}
}
