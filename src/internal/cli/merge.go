package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// mergeCmd merges bookmarks files into a workspace (made when missing), folding copies of the same page into one, as
// one history entry. Copies that lose go to the workspace's Trash.
func mergeCmd() *cobra.Command {
	var wsID, name string
	var separate bool
	cmd := &cobra.Command{
		Use:   "merge FILE...",
		Short: "Merge bookmarks files into a workspace, folding duplicates (each copy goes to the Trash)",
		Args:  cobra.RangeArgs(1, 20),
		RunE: func(cmd *cobra.Command, args []string) error {
			var sources []merge.Source
			for _, path := range args {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				t, _, err := importer.Read(f)
				f.Close()
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				sources = append(sources, merge.Source{Name: filepath.Base(path), Tree: t})
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			ctx, now := context.Background(), time.Now()
			x, err := s.Workspace(ctx, wsID)
			if errors.Is(err, store.ErrNotFound) {
				if name == "" {
					name = wsID
				}
				x, err = s.CreateWorkspace(ctx, wsID, name, now)
			}
			if err != nil {
				return err
			}
			prev := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder}}
			if x.Head > 0 {
				if prev, _, err = s.State(ctx, x.ID, 0); err != nil {
					return err
				}
			}
			res, err := merge.Merge(prev, sources, separate, now.Unix())
			if err != nil {
				return err
			}
			names := make([]string, len(sources))
			for i, src := range sources {
				names[i] = src.Name
			}
			summary := fmt.Sprintf("Added %d bookmarks from %s", res.Added, strings.Join(names, ", "))
			if len(res.Dups) > 0 {
				summary += fmt.Sprintf("; %d duplicate copies went to the Trash", len(res.Dups))
			}
			e, err := s.Commit(ctx, x.ID, x.Head, store.Change{Op: "merge", Summary: summary, Tree: res.Tree}, now)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "workspace %s: %s (entry %d)\n", x.ID, e.Summary, e.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&wsID, "workspace", "main", "the workspace to merge into; made when missing")
	cmd.Flags().StringVar(&name, "name", "", "the new workspace's display name (default: its id)")
	cmd.Flags().BoolVar(&separate, "separate", false, "put each file's folders under a folder named after the file")
	return cmd
}
