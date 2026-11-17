package browsers

import (
	"reflect"
	"testing"
)

const (
	desktop  = "/Users/me/Desktop/Google Chrome.app/Contents/MacOS/Google Chrome"
	automate = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --enable-automation --no-default-browser-check --user-data-dir=/Users/me/.cache/agent profile --remote-debugging-pipe"
	helper   = "/Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/131/Helpers/Google Chrome Helper.app/Contents/MacOS/Google Chrome Helper --type=utility --utility-sub-type=network.mojom.NetworkService"
)

func TestParseMain(t *testing.T) {
	in, ok := ParseMain(7, desktop)
	if !ok || in.App != "/Users/me/Desktop/Google Chrome.app" || in.Exe != desktop || in.Profile != "" || in.Automated {
		t.Fatalf("desktop: %+v %v", in, ok)
	}
	in, ok = ParseMain(8, automate)
	if !ok || in.App != "/Applications/Google Chrome.app" || in.Profile != "/Users/me/.cache/agent profile" || !in.Automated {
		t.Fatalf("automated: %+v %v", in, ok)
	}
	if _, ok := ParseMain(9, helper); ok {
		t.Fatal("a helper is not a main process")
	}
	if _, ok := ParseMain(10, "/usr/sbin/cupsd -l"); ok {
		t.Fatal("not an app")
	}
}

func TestParsePSKeepsOnlyThatBrowser(t *testing.T) {
	out := "  7 " + desktop + "\n  8 " + automate + "\n  9 " + helper + "\n 11 /Applications/Safari.app/Contents/MacOS/Safari\nbad line\n"
	got := ParsePS(out, ExeName("/Applications/Google Chrome.app"))
	if len(got) != 2 || got[0].PID != 7 || got[1].PID != 8 {
		t.Fatalf("%+v", got)
	}
	if len(ParsePS(out, "Firefox")) != 0 {
		t.Fatal("no Firefox runs")
	}
}

func TestParseLsofAndParent(t *testing.T) {
	if got := ParseLsofPIDs("p100\nf12\np200\np42\n", 42); !reflect.DeepEqual(got, []int{100, 200}) {
		t.Fatalf("%v", got)
	}
	ppid, cmd, ok := ParseParent("  83657 " + helper + "\n")
	if !ok || ppid != 83657 || cmd != helper {
		t.Fatalf("%d %q %v", ppid, cmd, ok)
	}
	if _, _, ok := ParseParent(""); ok {
		t.Fatal("empty")
	}
}

func TestLoopbackPort(t *testing.T) {
	for addr, want := range map[string]int{"127.0.0.1:54321": 54321, "[::1]:80": 80, "192.168.1.4:5000": 0, "nonsense": 0} {
		if got := LoopbackPort(addr); got != want {
			t.Errorf("%s: %d, want %d", addr, got, want)
		}
	}
}

func TestOpenArgsKeepTheProfile(t *testing.T) {
	in, _ := ParseMain(8, automate)
	want := []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "--user-data-dir=/Users/me/.cache/agent profile", "chrome://extensions"}
	if got := OpenArgs(in, "chrome://extensions"); !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	in, _ = ParseMain(7, desktop)
	if got := OpenArgs(in, "chrome://extensions"); !reflect.DeepEqual(got, []string{desktop, "chrome://extensions"}) {
		t.Fatalf("%q", got)
	}
}
