package browsers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fake records what would run; it never starts anything.
type fake struct {
	mu        sync.Mutex
	installed []Browser
	def       string
	ran       [][]string
	fail      map[string]error // by app path
}

func (f *fake) Installed() []Browser              { return f.installed }
func (f *fake) DefaultBundle() string             { return f.def }
func (f *fake) Icon(Browser, int) ([]byte, error) { return []byte("png"), nil }
func (f *fake) Launch(_ context.Context, argv []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ran = append(f.ran, argv)
	for path, err := range f.fail {
		if strings.Contains(strings.Join(argv, " "), path) {
			return err
		}
	}
	return nil
}

func app(name string) Browser {
	for _, b := range Known {
		if b.Name == name {
			b.Path = "/Applications/" + name + ".app"
			return b
		}
	}
	panic(name)
}

func TestCommand(t *testing.T) {
	u := []string{"https://go.dev/"}
	cases := []struct {
		b    Browser
		how  string
		want string
	}{
		{app("Google Chrome"), AsTabs, "open -a /Applications/Google Chrome.app https://go.dev/"},
		{app("Google Chrome"), NewWindow, "open -na /Applications/Google Chrome.app --args --new-window https://go.dev/"},
		{app("Microsoft Edge"), Private, "open -na /Applications/Microsoft Edge.app --args --inprivate https://go.dev/"},
		{app("Firefox"), Private, "open -na /Applications/Firefox.app --args --private-window https://go.dev/"},
		{app("Safari"), NewWindow, "open -a /Applications/Safari.app https://go.dev/"},
	}
	for _, c := range cases {
		argv, err := Command(c.b, c.how, u)
		if err != nil || strings.Join(argv, " ") != c.want {
			t.Errorf("%s %s: %q %v", c.b.Name, c.how, argv, err)
		}
	}
	if _, err := Command(app("Safari"), Private, u); err == nil {
		t.Fatal("Safari has no private-window option; opening normally instead would leak a private link into history")
	}
}

func TestOnlyWebLinks(t *testing.T) {
	for _, bad := range [][]string{nil, {"file:///etc/passwd"}, {"javascript:alert(1)"}, {"-a"}, {"x-apple.systempreferences:"}, {"https://"}, {"ftp://example.com/x"}, {"vscode://file/etc/hosts"}} {
		if CheckLinks(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	many := make([]string, MaxLinks+1)
	for i := range many {
		many[i] = "https://a.example/"
	}
	if CheckLinks(many) == nil {
		t.Fatal("too many links accepted")
	}
}

func TestOpenResolvesTheDefaultAndReportsEachBrowser(t *testing.T) {
	f := &fake{installed: []Browser{app("Google Chrome"), app("Safari")}, def: "com.apple.safari",
		fail: map[string]error{"Google Chrome": errors.New("refused")}}
	s := &Service{Platform: f}
	res, err := s.Open(context.Background(), []string{"https://go.dev/"}, AsTabs, []string{SystemDefault, "Google Chrome", "Arc"})
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].OK || res[0].Browser != "Safari" || res[1].OK || res[1].Error != "refused" || res[2].OK || !strings.Contains(res[2].Error, "not installed") {
		t.Fatalf("%+v", res)
	}
	saw := false
	for _, argv := range f.ran {
		if strings.Contains(strings.Join(argv, " "), "Safari.app") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("the system default (Safari) was not used: %q", f.ran)
	}
	// An unknown default: links go to open(1) as they are, never to a private window.
	f2 := &fake{def: "com.example.unknown"}
	s2 := &Service{Platform: f2}
	res, _ = s2.Open(context.Background(), []string{"https://go.dev/"}, Private, nil)
	if res[0].OK || len(f2.ran) != 0 {
		t.Fatalf("a private window was asked of an unknown default: %+v %q", res, f2.ran)
	}
	if s.Default() != "Safari" {
		t.Fatal(s.Default())
	}
}
