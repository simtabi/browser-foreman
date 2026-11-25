package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/api"
	"github.com/simtabi/browser-foreman/src/internal/edit"
	"github.com/simtabi/browser-foreman/src/internal/sorting"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// sortCmd files a workspace's bookmarks into folders by the owner's rules. It only reports what it would do unless
// --apply is given; applied, the moves are one history entry.
func sortCmd() *cobra.Command {
	var wsID, rulesPath string
	var apply bool
	cmd := &cobra.Command{
		Use:   "sort",
		Short: "File a workspace's bookmarks into folders by your rules (a dry run unless --apply)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, c, err := load()
			if err != nil {
				return err
			}
			var rules sorting.Rules
			if rulesPath != "" {
				b, err := os.ReadFile(rulesPath)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(b, &rules); err != nil {
					return fmt.Errorf("%s: %w", rulesPath, err)
				}
			} else if rules, err = api.ReadSortRules(c.Storage.Path); errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("no filing rules: write them to %s or pass --rules (see docs/tools/sorting.md)", filepath.Join(c.Storage.Path, api.SortRulesFile))
			} else if err != nil {
				return err
			}
			s, err := store.Open(c.Storage.Driver, c.Storage.Path, c.Storage.DSN)
			if err != nil {
				return err
			}
			defer s.Close()
			ctx := context.Background()
			x, err := s.Workspace(ctx, wsID)
			if err != nil {
				return err
			}
			t, _, err := s.State(ctx, x.ID, 0)
			if err != nil {
				return err
			}
			links, err := s.Links(ctx, nil)
			if err != nil {
				return err
			}
			p, err := rules.Plan(t, links)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			type row struct {
				f string
				n int
			}
			var rows []row
			for f, n := range p.Folders {
				rows = append(rows, row{f, n})
			}
			slices.SortFunc(rows, func(a, b row) int { return cmp.Or(cmp.Compare(b.n, a.n), cmp.Compare(a.f, b.f)) })
			fmt.Fprintf(out, "%d bookmarks would move, %d folders would be made\n", len(p.Moves), len(p.New))
			for i, r := range rows {
				if i == 25 {
					fmt.Fprintf(out, "  … and %d more folders\n", len(rows)-i)
					break
				}
				fmt.Fprintf(out, "  %5d  %s\n", r.n, cmp.Or(r.f, "(top level)"))
			}
			if !apply || len(p.Moves) == 0 {
				if !apply {
					fmt.Fprintln(out, "dry run: nothing changed (add --apply to make these moves)")
				}
				return nil
			}
			next, err := sorting.Apply(t, p, api.NodeID)
			if err != nil {
				return err
			}
			if _, err := edit.Diff(t, next); err != nil {
				return err
			}
			e, err := s.Commit(ctx, x.ID, x.Head, store.Change{Op: "sort", Summary: api.SortSummary(p), Tree: next}, time.Now())
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "workspace %s: %s (entry %d)\n", x.ID, e.Summary, e.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&wsID, "workspace", "main", "the workspace to sort")
	cmd.Flags().StringVar(&rulesPath, "rules", "", "a rules file (default: "+api.SortRulesFile+" in the data folder)")
	cmd.Flags().BoolVar(&apply, "apply", false, "make the moves (one history entry); without it, only report them")
	return cmd
}
