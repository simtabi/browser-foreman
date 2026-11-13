package cli

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/simtabi/browser-foreman/src/internal/api"
	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

func openStore() (store.Store, error) {
	_, c, err := load()
	if err != nil {
		return nil, err
	}
	return store.Open(c.Storage.Driver, c.Storage.Path)
}

func importCmd() *cobra.Command {
	var wsID, name string
	cmd := &cobra.Command{
		Use:   "import FILE",
		Short: "Read a browser's bookmarks file into a new workspace (Chrome's Bookmarks JSON, or any exported bookmarks HTML)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			tree, format, err := importer.Read(f)
			if err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			ctx, now := context.Background(), time.Now()
			if name == "" {
				name = filepath.Base(args[0])
			}
			if _, err := s.CreateWorkspace(ctx, wsID, name, now); err != nil {
				return fmt.Errorf("%w (choose another with --workspace)", err)
			}
			bm, fo := tree.Count()
			e, err := s.Commit(ctx, wsID, 0, store.Change{Op: "import",
				Summary: fmt.Sprintf("Imported %d bookmarks in %d folders from %s (%s)", bm, fo, filepath.Base(args[0]), format), Tree: tree}, now)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "workspace %s: %s (entry %d)\n", wsID, e.Summary, e.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&wsID, "workspace", "main", "id of the new workspace (letters, digits, - and _)")
	cmd.Flags().StringVar(&name, "name", "", "its display name (default: the file name)")
	return cmd
}

func workspaceCmd() *cobra.Command {
	c := &cobra.Command{Use: "workspace", Short: "List workspaces, read their history, undo and redo"}
	c.AddCommand(&cobra.Command{
		Use: "list", Short: "Every workspace, its size and last change",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			all, err := s.Workspaces(context.Background())
			if err != nil {
				return err
			}
			if len(all) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No workspaces yet: `browser-foreman import <file>` creates one.")
			}
			for _, w := range all {
				tree, e, err := s.State(context.Background(), w.ID, 0)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
				}
				bm, fo := tree.Count()
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-32s %6d bookmarks %5d folders  change #%d %s\n", w.ID, w.Name, bm, fo, e.Seq, e.Summary)
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{
		Use: "history ID", Short: "Every change, oldest first", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			h, err := s.History(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, e := range h {
				fmt.Fprintf(cmd.OutOrStdout(), "#%-4d %s  %-7s %s\n", e.Seq, e.At.Local().Format("2006-01-02 15:04"), e.Op, e.Summary)
			}
			return nil
		},
	})
	for _, op := range []struct {
		name, short string
		run         func(context.Context, store.Store, string, time.Time) (store.Entry, error)
	}{{"undo", "Take back the last change (as a new entry, so it can be redone)", store.Undo}, {"redo", "Put back the last change undo took back", store.Redo}} {
		c.AddCommand(&cobra.Command{
			Use: op.name + " ID", Short: op.short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				s, err := openStore()
				if err != nil {
					return err
				}
				defer s.Close()
				e, err := op.run(context.Background(), s, args[0], time.Now())
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "#%d %s\n", e.Seq, e.Summary)
				return nil
			},
		})
	}
	return c
}

func storageCmd() *cobra.Command {
	c := &cobra.Command{Use: "storage", Short: "Move data between storage drivers"}
	var to string
	m := &cobra.Command{
		Use:   "migrate",
		Short: "Copy every workspace (with its whole history) and tab snapshot to the other driver; the source is left as it was",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, cfg, err := load()
			if err != nil {
				return err
			}
			if to == cfg.Storage.Driver {
				return fmt.Errorf("storage is already %s", to)
			}
			src, err := store.Open(cfg.Storage.Driver, cfg.Storage.Path)
			if err != nil {
				return err
			}
			defer src.Close()
			dst, err := store.Open(to, cfg.Storage.Path)
			if err != nil {
				return err
			}
			defer dst.Close()
			ws, n, err := store.Migrate(context.Background(), src, dst)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "copied %d workspaces (%d history entries) from %s to %s.\nSet storage.driver to %s (or BROFO_STORAGE_DRIVER=%s) to use it.\n",
				ws, n, cfg.Storage.Driver, to, strconv.Quote(to), to)
			return nil
		},
	}
	m.Flags().StringVar(&to, "to", "", "json or sqlite")
	_ = m.MarkFlagRequired("to")
	c.AddCommand(m)
	return c
}

func backupCmd() *cobra.Command {
	return &cobra.Command{
		Use: "backup FILE", Short: "Write every workspace with its history, and the link results, to FILE (gzipped JSON); no accounts or secrets",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			b, err := store.MakeBackup(context.Background(), s, time.Now())
			if err != nil {
				return err
			}
			f, err := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err // never overwrites: pick a new name
			}
			zw := gzip.NewWriter(f)
			if err := json.NewEncoder(zw).Encode(b); err != nil {
				f.Close()
				return err
			}
			if err := zw.Close(); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "backed up %d workspaces (%d distinct trees) and %d link results to %s\n", len(b.Workspaces), len(b.Trees), len(b.Links), args[0])
			return nil
		},
	}
}

func restoreCmd() *cobra.Command {
	return &cobra.Command{
		Use: "restore FILE", Short: "Add what a backup holds; replaces nothing (a taken workspace id comes back under a new one)",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			b, err := api.ReadBackup(f)
			if err != nil {
				return fmt.Errorf("%s is not a readable backup: %w", args[0], err)
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			defer s.Close()
			res, err := store.Restore(context.Background(), s, b, time.Now())
			if err != nil {
				return err
			}
			for from, to := range res.Workspaces {
				if from == to {
					fmt.Fprintf(cmd.OutOrStdout(), "restored %s\n", to)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "restored %s as %s (that id was taken)\n", from, to)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%d entries, %d newer link results, %d tab snapshots", res.Entries, res.Links, res.Tabs)
			if res.Prefs {
				fmt.Fprint(cmd.OutOrStdout(), ", preferences")
			}
			fmt.Fprintln(cmd.OutOrStdout())
			return nil
		},
	}
}
