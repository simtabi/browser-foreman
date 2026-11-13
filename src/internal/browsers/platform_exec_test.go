package browsers

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecPlatformCommandsAndAllowList(t *testing.T) {
	chrome := Browser{Name: "Google Chrome", Path: "/usr/bin/google-chrome", Engine: "chromium", PrivateFlag: "--incognito"}
	safari := Browser{Name: "Orion", Path: "/usr/bin/orion", Engine: "webkit"}
	p := &execPlatform{find: func() []Browser { return []Browser{chrome, safari} }, dflt: func() string { return "" },
		opener: func(u string) []string { return []string{"xdg-open", u} }, LaunchWait: 10 * time.Millisecond}
	s := &Service{Platform: p}
	for how, want := range map[string]string{
		AsTabs: "/usr/bin/google-chrome https://a.example/", NewWindow: "/usr/bin/google-chrome --new-window https://a.example/",
		Private: "/usr/bin/google-chrome --incognito https://a.example/",
	} {
		argv, err := p.Command(chrome, how, []string{"https://a.example/"})
		if err != nil || strings.Join(argv, " ") != want {
			t.Errorf("%s: %v %v", how, argv, err)
		}
	}
	if _, err := p.Command(safari, Private, []string{"https://a.example/"}); err == nil {
		t.Error("a browser with no private option was asked for a private window")
	}
	if got := p.DefaultCommands([]string{"https://a.example/", "https://b.example/"}); len(got) != 2 || got[1][1] != "https://b.example/" {
		t.Errorf("default commands %v", got)
	}
	for _, argv := range [][]string{{"/bin/sh", "-c", "echo hi"}, {"/usr/bin/curl", "https://x"}, {}} {
		if err := p.Launch(context.Background(), argv); err == nil {
			t.Errorf("%v was run", argv)
		}
	}
	// The service picks the platform's commands over macOS's open(1).
	var ran []string
	s.Platform = launchRec{p, &ran}
	if res, _ := s.Open(context.Background(), []string{"https://a.example/"}, AsTabs, []string{"Google Chrome"}); !res[0].OK || ran[0] != "/usr/bin/google-chrome" {
		t.Fatalf("%+v %v", res, ran)
	}
}

// launchRec records what would run, through an execPlatform's commands.
type launchRec struct {
	*execPlatform
	ran *[]string
}

func (l launchRec) Launch(_ context.Context, argv []string) error {
	*l.ran = append(*l.ran, argv[0])
	return nil
}
