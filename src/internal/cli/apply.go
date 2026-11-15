package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slug is a name as the extension turns it into a workspace id ("Chrome Beta" → "chrome-beta").
func slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func applyCmd() *cobra.Command {
	var ws, base, browser, profile, file, mode, route, fingerprint string
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
			if base == "" {
				return errors.New("--base names the export the cleanup started from (a workspace)")
			}
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

			if route == "extension" {
				return applyThroughExtension(ctx, s, out, j, browser, yes, list, now)
			}
			path := file
			if path == "" {
				if path, err = pickProfile(in, out, interactive, browser, profile); err != nil {
					return err
				}
			}
			running, syncs := false, false
			if p, err := writeback.PlanFile(ctx, s, j, path); err == nil {
				running, syncs = p.Running, p.SyncOn
			}
			// The safe route for this profile is offered first.
			opts := []choice{
				{"file", "file: write the profile's Bookmarks file (the browser must be quit)"},
				{"extension", "extension: write through the browser's extension (works while it runs; Sync passes it on)"},
			}
			if running || syncs {
				opts[0], opts[1] = opts[1], opts[0]
				opts[0].label += " (recommended: " + map[bool]string{true: "the browser is running", false: "the profile syncs"}[running] + ")"
			}
			r, err := choose(in, out, interactive, "Route", "route", route, opts)
			if err != nil {
				return err
			}
			if r == "extension" {
				return applyThroughExtension(ctx, s, out, j, browser, yes, list, now)
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
				Backup: slug(browser) + "-before-write"}, now)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Written. The profile as it was: workspace %s-before-write (entry %d) and %s.\n", slug(browser), got.Backup.Seq, got.Kept)
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
	f.StringVar(&browser, "browser", "Chrome", "the browser (Chrome, Brave, Edge, Vivaldi, Chromium…)")
	f.StringVar(&profile, "profile", "", "the profile: its folder (Default, Profile 1…) or the name the browser shows")
	f.StringVar(&file, "file", "", "a Bookmarks file to write instead of looking the profile up")
	f.StringVar(&mode, "mode", "", "merge or replace")
	f.StringVar(&route, "route", "", "file or extension")
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

// applyThroughExtension plans against what the extension read (<browser>-live) and, with yes, saves the target as
// <browser>-target for the extension's popup to write.
func applyThroughExtension(ctx context.Context, s store.Store, out io.Writer, j writeback.Job, browser string, yes bool, list int, now time.Time) error {
	live, target := slug(browser)+"-live", slug(browser)+"-target"
	t, rep, err := writeback.PlanWorkspace(ctx, s, j, live)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no workspace %s: in %s, open the Browser Foreman extension and read this browser's bookmarks first", live, browser)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Against %s, as the extension read it. If the profile syncs, the write reaches every device signed in to it.\n", live)
	printReport(out, rep, list)
	if j.Mode == writeback.Replace && rep.DropNew != j.DropNew {
		return fmt.Errorf("replace removes %d bookmarks the profile gained after the export: confirm with --drop-new %d", rep.DropNew, rep.DropNew)
	}
	if !yes {
		fmt.Fprintln(out, "\nDry run: nothing saved. With --yes, the result is saved as workspace "+target+" for the extension to write.")
		return nil
	}
	e, err := writeback.Save(ctx, s, target, browser+" write-back", "writeback", fmt.Sprintf("Write-back of %s (%s) planned against %s", j.Workspace, j.Mode, live), t, now)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Saved as workspace %s (entry %d). In %s, open the extension, choose %s under Write a workspace back, check the counts, and confirm.\n", target, e.Seq, browser, target)
	return nil
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
