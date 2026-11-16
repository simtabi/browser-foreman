package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/simtabi/browser-foreman/src/internal/config"
	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
	"github.com/simtabi/browser-foreman/src/internal/writeback"
)

// choice is one answer to a question, with why it would be picked.
type choice struct{ value, label string }

// choose returns the flag's value when given (it must be one of the options), else asks on a terminal, the
// recommended option first and taken on an empty answer. With no terminal it says which flag to pass.
func choose(in io.Reader, out io.Writer, interactive bool, question, flag, given string, options []choice) (string, error) {
	if given != "" {
		for _, o := range options {
			if strings.EqualFold(o.value, given) {
				return o.value, nil
			}
		}
		return "", fmt.Errorf("--%s %q is not one of: %s", flag, given, values(options))
	}
	if len(options) == 1 {
		return options[0].value, nil
	}
	if !interactive {
		return "", fmt.Errorf("%s: pass --%s (%s)", question, flag, values(options))
	}
	fmt.Fprintln(out, question)
	for i, o := range options {
		fmt.Fprintf(out, "  %d. %s\n", i+1, o.label)
	}
	fmt.Fprintf(out, "Choose 1-%d [1]: ", len(options))
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return options[0].value, nil
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(options) {
		return "", fmt.Errorf("%q is not a number from 1 to %d", line, len(options))
	}
	return options[n-1].value, nil
}

func values(options []choice) string {
	v := make([]string, len(options))
	for i, o := range options {
		v[i] = o.value
	}
	return strings.Join(v, ", ")
}

// browserNames pairs the name profile detection gives a browser with the one its extension reports (core.ts
// browserName), which names the workspaces the extension reads into and writes from.
var browserNames = [][2]string{
	{"Chrome", "Google Chrome"}, {"Chrome Beta", "Google Chrome"}, {"Brave", "Brave Browser"}, {"Edge", "Microsoft Edge"},
	{"Vivaldi", "Vivaldi"}, {"Chromium", "Chromium"}, {"Chromium (snap)", "Chromium"}, {"Arc", "Arc"}, {"Opera", "Opera"},
}

// namesOf returns a browser's profile-detection name and its extension's name, from either.
func namesOf(browser string) (profileName, extensionName string) {
	for _, n := range browserNames {
		if strings.EqualFold(browser, n[0]) || strings.EqualFold(browser, n[1]) {
			return n[0], n[1]
		}
	}
	return browser, browser
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug is a name as the extension turns it into a workspace id ("Chrome Beta" → "chrome-beta").
func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func applyCmd() *cobra.Command {
	var ws, base, browser, profile, file, mode, route, read, fingerprint string
	var yes, syncRiskOK bool
	var dropNew, list int
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Write a cleaned workspace back into a browser profile (a dry run unless --yes)",
		Long: "Plans writing a cleaned workspace into a Chromium profile as a three-way merge: the export the cleanup started\n" +
			"from (--base), the cleaned workspace (--workspace) and the profile now. It prints what would change and writes\n" +
			"nothing unless --yes.\n\n" +
			"Mode: merge keeps what the profile changed after the export; replace drops what it gained (confirm the count\n" +
			"with --drop-new). Route: file writes the profile's Bookmarks file (the browser must be quit; refused for a\n" +
			"syncing profile unless --sync-risk-ok); extension plans against the workspace the browser's extension read\n" +
			"(<browser>-live) and saves the result as <browser>-target, which the extension's popup writes. Missing choices\n" +
			"are asked on a terminal.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in, out := cmd.InOrStdin(), cmd.OutOrStdout()
			interactive := in == os.Stdin && term.IsTerminal(int(os.Stdin.Fd()))
			profileName, extName := namesOf(browser)
			if base == "" {
				return errors.New("--base names the export the cleanup started from (a workspace)")
			}
			_, c, err := load()
			if err != nil {
				return err
			}
			// A flag wins; else the configuration, unless it says "ask".
			setting := func(flag, cfg string) string {
				if flag != "" || cfg == "ask" {
					return flag
				}
				return cfg
			}
			mode, route, read = setting(mode, c.Writeback.Mode), setting(route, c.Writeback.Route), setting(read, c.Writeback.Read)
			m, err := choose(in, out, interactive, "Mode", "mode", mode, []choice{
				{"merge", "merge: apply the cleanup, keep what the profile changed after the export (recommended)"},
				{"replace", "replace: the profile becomes the cleanup; what it gained after the export is dropped (asks for the count)"},
			})
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			ctx, now := context.Background(), time.Now()
			j := writeback.Job{Workspace: ws, Base: base, Mode: writeback.Mode(m), DropNew: dropNew}

			// The profile's file, unless the extension route was chosen and the extension reads it: then it is
			// looked for only if the file is to be read.
			path, pathErr := file, error(nil)
			findPath := func() error {
				if path == "" && pathErr == nil {
					path, pathErr = pickProfile(in, out, interactive, profileName, profile)
				}
				return pathErr
			}
			r := route
			if r == "" {
				running, syncs := false, false
				if findPath() == nil {
					if p, err := writeback.PlanFile(ctx, s, j, path); err == nil {
						running, syncs = p.Running, p.SyncOn
					}
				}
				// The safe route for this profile is offered first.
				opts := []choice{
					{"file", "file: write the profile's Bookmarks file (the browser must be quit)"},
					{"extension", "extension: write through the browser's extension (works while it runs; Sync passes it on)"},
				}
				switch {
				case pathErr != nil:
					opts = opts[1:] // the file cannot be read, so it cannot be written
				case running || syncs:
					opts[0], opts[1] = opts[1], opts[0]
					opts[0].label += " (recommended: " + map[bool]string{true: "the browser is running", false: "the profile syncs"}[running] + ")"
				}
				if r, err = choose(in, out, interactive, "Route", "route", "", opts); err != nil {
					return err
				}
			}
			if r == "extension" {
				rd, err := choose(in, out, interactive, "Read the profile now", "read", read, []choice{
					{"extension", "extension: the extension reads the browser's bookmarks now (Browser Foreman must be running)"},
					{"file", "file: read the profile's Bookmarks file (macOS may need Full Disk Access for this)"},
				})
				if err != nil {
					return err
				}
				var theirs store.Tree
				var from string
				if rd == "file" {
					if err := findPath(); err != nil {
						return err
					}
					t, err := writeback.ReadFile(path)
					if err != nil {
						return err
					}
					theirs, from = writeback.WithPrefix(t, importer.ExtPrefix), path
				} else {
					if theirs, from, err = readThroughExtension(ctx, s, c, cmd.ErrOrStderr(), extName); err != nil {
						return err
					}
				}
				return applyThroughExtension(ctx, s, c, out, j, extName, theirs, from, yes, list, now)
			}
			if r != "file" {
				return fmt.Errorf("--route %q: use file or extension", r)
			}
			if err := findPath(); err != nil {
				return err
			}
			p, err := writeback.PlanFile(ctx, s, j, path)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Profile file %s (sync %s, browser %s)\n", path, onOff(p.SyncOn), map[bool]string{true: "running", false: "quit"}[p.Running])
			printReport(out, p.Report, list)
			if !yes {
				if !interactive {
					fmt.Fprintf(out, "\nDry run: nothing written. To write it: --yes --fingerprint %s%s\n", p.Report.Fingerprint, confirmFlags(j, p))
					return nil
				}
				fmt.Fprint(out, "\nWrite this into the profile? Type yes: ")
				line, _ := bufio.NewReader(in).ReadString('\n')
				if strings.TrimSpace(line) != "yes" {
					fmt.Fprintln(out, "Nothing written.")
					return nil
				}
				fingerprint = p.Report.Fingerprint
			}
			got, err := writeback.ApplyFile(ctx, s, j, path, writeback.FileOptions{Fingerprint: fingerprint, SyncRiskOK: syncRiskOK,
				Backup: slug(extName) + "-before-write"}, now)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Written. The profile as it was: workspace %s-before-write (entry %d) and %s.\n", slug(extName), got.Backup.Seq, got.Kept)
			if got.Drift.Empty() {
				fmt.Fprintln(out, "Verified: read back, it holds the plan.")
				return nil
			}
			fmt.Fprintln(out, "Read back, it differs from the plan:")
			printReport(out, got.Drift, list)
			return errors.New("the written profile differs from the plan")
		},
	}
	f := cmd.Flags()
	f.StringVar(&ws, "workspace", "main", "the cleaned workspace to write")
	f.StringVar(&base, "base", "", "the workspace holding the export the cleanup started from")
	f.StringVar(&browser, "browser", "Chrome", "the browser (Chrome, Brave, Edge, Vivaldi, Chromium…; the extension's names, such as Google Chrome, work too)")
	f.StringVar(&profile, "profile", "", "the profile: its folder (Default, Profile 1…) or the name the browser shows")
	f.StringVar(&file, "file", "", "a Bookmarks file to write instead of looking the profile up")
	f.StringVar(&mode, "mode", "", "merge or replace (or writeback.mode)")
	f.StringVar(&route, "route", "", "file or extension (or writeback.route)")
	f.StringVar(&read, "read", "", "extension route: read the profile through the extension or its file (or writeback.read)")
	f.BoolVar(&yes, "yes", false, "write (without it, a dry run)")
	f.StringVar(&fingerprint, "fingerprint", "", "the dry run's fingerprint: the write refuses a profile that changed since")
	f.IntVar(&dropNew, "drop-new", 0, "replace: how many bookmarks the profile gained after the export you agree to drop")
	f.BoolVar(&syncRiskOK, "sync-risk-ok", false, "file route: write although the profile syncs its bookmarks")
	f.IntVar(&list, "list", 10, "how many items of each kind the report lists")
	return cmd
}

// pickProfile finds the browser's profiles and picks one: by --profile, the only one, or by asking (Default first).
func pickProfile(in io.Reader, out io.Writer, interactive bool, browser, profile string) (string, error) {
	var opts []choice
	var refused []string
	for _, p := range importer.FindProfiles() {
		if !strings.EqualFold(p.Browser, browser) {
			continue
		}
		if !p.Readable {
			refused = append(refused, p.Why)
			continue
		}
		label := p.Profile
		if p.Name != "" && p.Name != p.Profile {
			label += " (" + p.Name + ")"
		}
		c := choice{p.Path, label}
		if p.Profile == "Default" {
			opts = append([]choice{c}, opts...)
		} else {
			opts = append(opts, c)
		}
		if profile != "" && (strings.EqualFold(profile, p.Profile) || strings.EqualFold(profile, p.Name)) {
			return p.Path, nil
		}
	}
	if len(opts) == 0 {
		why := "none found"
		if len(refused) > 0 {
			why = strings.Join(refused, "; ")
		}
		return "", fmt.Errorf("no readable %s profile (%s); point --file at its Bookmarks file, or use --route extension", browser, why)
	}
	if profile != "" {
		return "", fmt.Errorf("no %s profile %q (found: %s)", browser, profile, labels(opts))
	}
	return choose(in, out, interactive, browser+" profile", "profile", "", opts)
}

func labels(options []choice) string {
	v := make([]string, len(options))
	for i, o := range options {
		v[i] = o.label
	}
	return strings.Join(v, ", ")
}

func onOff(b bool) string { return map[bool]string{true: "on", false: "off"}[b] }

func confirmFlags(j writeback.Job, p writeback.FilePlan) string {
	s := ""
	if j.Mode == writeback.Replace && p.Report.DropNew > 0 {
		s += fmt.Sprintf(" --drop-new %d", p.Report.DropNew)
	}
	if p.SyncOn {
		s += " --sync-risk-ok (the profile syncs: prefer --route extension)"
	}
	return s
}

// readThroughExtension has the browser's extension read its bookmarks into <browser>-live now, through the running
// server, and returns them. With no server running it falls back to the copy the extension read last, and says when.
func readThroughExtension(ctx context.Context, s store.Store, c config.Config, warn io.Writer, browser string) (store.Tree, string, error) {
	live := slug(browser) + "-live"
	token := os.Getenv("BROFO_TOKEN")
	if token == "" {
		token = c.APIToken
	}
	if addr := serverAddr(ctx, c); answers(ctx, addr) {
		if err := askExtensionToRead(ctx, "http://"+addr, token, browser, 2*time.Minute); err != nil { // the extension takes commands at least once a minute
			return store.Tree{}, "", err
		}
	} else {
		if _, err := s.Workspace(ctx, live); err != nil {
			return store.Tree{}, "", fmt.Errorf("Browser Foreman is not running, so the extension cannot be asked to read %s, and there is no earlier read (%s): start `browser-foreman serve`, or read the profile's file with --read file", browser, live)
		}
		t, e, err := s.State(ctx, live, 0)
		if err == nil {
			fmt.Fprintf(warn, "Browser Foreman is not running: using the bookmarks the extension read last (%s, %s).\n", live, e.At.Local().Format("2 Jan 15:04"))
		}
		return t, live, err
	}
	t, _, err := s.State(ctx, live, 0)
	return t, live, err
}

// askExtensionToRead queues a read-bookmarks command for the browser's extension and waits until it is done.
func askExtensionToRead(ctx context.Context, base, token, browser string, wait time.Duration) error {
	_, err := runExtensionCommand(ctx, base, token, browser, map[string]any{"kind": "read-bookmarks"}, "read its bookmarks", wait)
	return err
}

// runExtensionCommand queues a command for the browser's extension, waits until it is done, and returns what the
// extension reported. what says what it was asked, for the errors.
func runExtensionCommand(ctx context.Context, base, token, browser string, cmd map[string]any, what string, wait time.Duration) (json.RawMessage, error) {
	call := func(method, path string, body any, out any) error {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, base+"/api/v1/"+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var e struct{ Error string }
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if resp.StatusCode >= 300 {
			_ = json.Unmarshal(raw, &e)
			return fmt.Errorf("%s (HTTP %d)", e.Error, resp.StatusCode)
		}
		return json.Unmarshal(raw, out)
	}
	var q struct {
		Command struct {
			ID, State, Error string
			Result           json.RawMessage
		}
	}
	body := map[string]any{"browser": browser}
	for k, v := range cmd {
		body[k] = v
	}
	if err := call("POST", "extension/commands", body, &q); err != nil {
		return nil, fmt.Errorf("asking %s's extension to %s: %w", browser, what, err)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if err := call("GET", "extension/commands/"+url.PathEscape(q.Command.ID), nil, &q); err != nil {
			return nil, err
		}
		switch q.Command.State {
		case "done":
			return q.Command.Result, nil
		case "failed":
			return nil, fmt.Errorf("%s's extension could not %s: %s", browser, what, q.Command.Error)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("%s's extension did not answer within %s: is it installed, paired and is %s running?", browser, wait, browser)
}

// applyThroughExtension plans against theirs (read from where from says) and, with yes, saves the target as
// <browser>-target for the extension's popup to write.
func applyThroughExtension(ctx context.Context, s store.Store, c config.Config, out io.Writer, j writeback.Job, browser string, theirs store.Tree, from string, yes bool, list int, now time.Time) error {
	target := slug(browser) + "-target"
	t, rep, err := writeback.PlanTree(ctx, s, j, theirs)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Against %s. If the profile syncs, the write reaches every device signed in to it.\n", from)
	printReport(out, rep, list)
	if j.Mode == writeback.Replace && rep.DropNew != j.DropNew {
		return fmt.Errorf("replace removes %d bookmarks the profile gained after the export: confirm with --drop-new %d", rep.DropNew, rep.DropNew)
	}
	if !yes {
		fmt.Fprintln(out, "\nDry run: nothing saved. With --yes, the result is saved as workspace "+target+" for the extension to write.")
		return nil
	}
	e, err := writeback.Save(ctx, s, target, browser+" write-back", "writeback", fmt.Sprintf("Write-back of %s (%s) planned against %s", j.Workspace, j.Mode, from), t, now)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved as workspace %s (entry %d).\n", target, e.Seq)
	addr := serverAddr(ctx, c)
	if !answers(ctx, addr) {
		fmt.Fprintf(out, "Browser Foreman is not running, so the extension cannot be asked to write it: start `browser-foreman serve` and run this again, or in %s open the extension, choose %s under Write a workspace back, and confirm.\n", browser, target)
		return nil
	}
	token := os.Getenv("BROFO_TOKEN")
	if token == "" {
		token = c.APIToken
	}
	base := "http://" + addr
	fmt.Fprintf(out, "Writing it into %s through its extension (its bookmarks as they are go to %s-before-write first)…\n", browser, slug(browser))
	res, err := runExtensionCommand(ctx, base, token, browser, map[string]any{"kind": "write-bookmarks", "workspace": target, "expect": writeback.ExtFingerprint(theirs)}, "write the plan", 20*time.Minute)
	if err != nil {
		return err
	}
	var counts struct {
		Created, Updated, Moved, RemovedBookmarks, RemovedFolders, Steps int
		Backup                                                           string
	}
	_ = json.Unmarshal(res, &counts)
	fmt.Fprintf(out, "Written: %d created, %d renamed or readdressed, %d moved, %d bookmarks and %d folders removed (%d steps). Saved first: %s\n",
		counts.Created, counts.Updated, counts.Moved, counts.RemovedBookmarks, counts.RemovedFolders, counts.Steps, counts.Backup)
	// Verify: read the browser again and plan against it; a write that landed as planned leaves nothing to do.
	if err := askExtensionToRead(ctx, base, token, browser, 2*time.Minute); err != nil {
		return fmt.Errorf("written, but reading it back failed: %w", err)
	}
	after, _, err := s.State(ctx, slug(browser)+"-live", 0)
	if err != nil {
		return err
	}
	_, drift, err := writeback.PlanTree(ctx, s, j, after)
	if err != nil {
		return err
	}
	if drift.Empty() {
		fmt.Fprintf(out, "Verified: %s now holds the plan; planning again changes nothing.\n", browser)
		return nil
	}
	fmt.Fprintln(out, "Read back, it differs from the plan:")
	printReport(out, drift, list)
	return errors.New("the browser's bookmarks differ from the plan after writing")
}

// printReport prints the report's counts and the first list items of each kind.
func printReport(out io.Writer, r writeback.Report, list int) {
	section := func(name string, items []writeback.Item, show func(writeback.Item) string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(out, "%s: %d\n", name, len(items))
		for i, it := range items {
			if i == list {
				fmt.Fprintf(out, "    … and %d more\n", len(items)-list)
				break
			}
			fmt.Fprintf(out, "    %s\n", show(it))
		}
	}
	where := func(it writeback.Item) string {
		s := it.Title
		if it.IsFolder {
			s += "/"
		}
		return s + "  (" + it.Folder + ")"
	}
	fmt.Fprintf(out, "\nWhat the write changes (%s):\n", r.Mode)
	if r.Empty() {
		fmt.Fprintln(out, "  nothing: the profile already holds this")
	}
	section("  added", r.Added, where)
	section("  removed", r.Removed, where)
	section("  moved", r.Moved, func(it writeback.Item) string { return it.Title + "  (" + it.Folder + " → " + it.To + ")" })
	section("  renamed or readdressed", r.Renamed, func(it writeback.Item) string {
		if it.WasURL != "" {
			return it.WasURL + " → " + it.URL
		}
		return it.Was + " → " + it.Title
	})
	fmt.Fprintln(out, "What the profile changed after the export:")
	if len(r.AddedSince)+len(r.RemovedSince)+len(r.MovedSince)+len(r.RenamedSince) == 0 {
		fmt.Fprintln(out, "  nothing")
	}
	section("  added", r.AddedSince, where)
	section("  removed (stays removed)", r.RemovedSince, where)
	section("  moved", r.MovedSince, func(it writeback.Item) string { return it.Title + "  (" + it.Folder + " → " + it.To + ")" })
	section("  renamed", r.RenamedSince, func(it writeback.Item) string { return it.Was + " → " + it.Title })
	section("Conflicts (the cleanup wins)", r.Conflicts, where)
	if r.DropNew > 0 {
		fmt.Fprintf(out, "Replace drops %d bookmarks the profile gained after the export.\n", r.DropNew)
	}
	if r.NotWritten > 0 {
		fmt.Fprintf(out, "%d bookmarks carry notes, descriptions, ratings or tags: the browser has no place for them, so they stay in Browser Foreman.\n", r.NotWritten)
	}
}
