//go:build darwin

package tabs

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/browsers"
)

// readJXA reads one browser through JavaScript for Automation. It checks running() first, so it never starts a
// browser, and reads every window's tabs in bulk.
const readJXA = `
function run(argv) {
  const name = argv[0], safari = name === "Safari";
  let app;
  try { app = Application(name); } catch (e) { return JSON.stringify({installed: false}); }
  if (!app.running()) return JSON.stringify({running: false});
  const out = [], wins = app.windows();
  for (let i = 0; i < wins.length; i++) {
    const w = wins[i];
    let urls = [], titles = [], ids = [], mode = "normal", active = 0, wid = 0;
    try { urls = w.tabs.url(); titles = safari ? w.tabs.name() : w.tabs.title(); } catch (e) { continue; }
    try { wid = w.id(); } catch (e) {}
    if (!safari) { try { ids = w.tabs.id(); } catch (e) {} try { mode = w.mode(); } catch (e) {} try { active = w.activeTabIndex(); } catch (e) {} }
    else { try { active = w.currentTab().index(); } catch (e) {} }
    out.push({index: i + 1, id: wid, mode: mode || "normal", active: active,
      tabs: urls.map((u, k) => ({url: u || "", title: titles[k] || "", id: ids[k] == null ? 0 : ids[k]}))});
  }
  return JSON.stringify({running: true, windows: out});
}`

// closeJXA closes tabs by id (Chromium) or position (Safari), highest first, and only when the tab still shows the
// address that was read.
const closeJXA = `
function run(argv) {
  const name = argv[0], items = JSON.parse(argv[1]), safari = name === "Safari";
  const app = Application(name);
  if (!app.running()) return JSON.stringify({closed: 0, skipped: ["not running"]});
  let closed = 0; const skipped = [];
  items.sort((a, b) => (b.pos || 0) - (a.pos || 0));
  for (const it of items) {
    try {
      let tab;
      if (!safari && it.tab_id) tab = app.windows.byId(it.window_id).tabs.byId(it.tab_id);
      else tab = app.windows.byId(it.window_id).tabs[it.pos];
      if (tab.url() !== it.url) { skipped.push(it.url + ": changed since it was read"); continue; }
      tab.close(); closed++;
    } catch (e) { skipped.push(it.url + ": already closed or window gone"); }
  }
  return JSON.stringify({closed: closed, skipped: skipped});
}`

// Mac reads and closes tabs through osascript.
type Mac struct{ Timeout time.Duration }

func NewMac() *Mac { return &Mac{Timeout: 20 * time.Second} }

func (m *Mac) Read(ctx context.Context, b browsers.Browser) Browser {
	out := Browser{Browser: b.Name}
	if !b.Scriptable {
		out.Error = ErrNotScriptable.Error()
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, m.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", readJXA, b.Name)
	stdout, err := cmd.Output()
	if ctx.Err() != nil {
		// macOS holds the request while its permission dialog is open, so a first read often times out.
		out.Error, out.Hint = "needs-permission", "macOS may be asking whether Browser Foreman can control "+b.Name+": choose Allow, or turn it on in System Settings › Privacy & Security › Automation."
		return out
	}
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		switch {
		case strings.Contains(msg, "-1743") || strings.Contains(strings.ToLower(msg), "not allowed") || strings.Contains(msg, "Not authorized"):
			out.Error, out.Hint = "needs-permission", "Allow it in System Settings › Privacy & Security › Automation (browser-foreman → "+b.Name+")."
		case strings.Contains(msg, "-600") || strings.Contains(msg, "isn't running"):
		default:
			if i := strings.LastIndex(msg, "\n"); i >= 0 {
				msg = msg[i+1:]
			}
			out.Error = msg
		}
		return out
	}
	var doc struct {
		Running bool     `json:"running"`
		Windows []Window `json:"windows"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		out.Error = "unreadable answer"
		return out
	}
	out.Running, out.Windows = doc.Running, doc.Windows
	if !b.PrivateKnown {
		for i := range out.Windows {
			out.Windows[i].Mode = "normal"
		}
	}
	return out
}

func (m *Mac) Close(ctx context.Context, browser string, items []CloseItem) (int, []string, error) {
	b, _ := json.Marshal(items)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	stdout, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", closeJXA, browser, string(b)).Output()
	if err != nil {
		return 0, nil, err
	}
	var doc struct {
		Closed  int      `json:"closed"`
		Skipped []string `json:"skipped"`
	}
	if err := json.Unmarshal(stdout, &doc); err != nil {
		return 0, nil, err
	}
	return doc.Closed, doc.Skipped, nil
}
