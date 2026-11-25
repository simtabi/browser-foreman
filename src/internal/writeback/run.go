package writeback

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/chromefile"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Job is one write-back: the cleaned workspace, the export it started from, and how to treat what the profile
// gained since (DropNew confirms how many bookmarks a replace removes).
type Job struct {
	Workspace string
	Base      string
	Mode      Mode
	DropNew   int
}

// load is a workspace's newest tree.
func load(ctx context.Context, s store.Store, id string) (store.Tree, error) {
	if _, err := s.Workspace(ctx, id); err != nil {
		return store.Tree{}, fmt.Errorf("workspace %s: %w", id, err)
	}
	t, _, err := s.State(ctx, id, 0)
	return t, err
}

// Save commits t as the newest state of workspace id, made (named name) when missing: one history entry.
func Save(ctx context.Context, s store.Store, id, name, op, summary string, t store.Tree, now time.Time) (store.Entry, error) {
	x, err := s.Workspace(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		x, err = s.CreateWorkspace(ctx, id, name, now)
	}
	if err != nil {
		return store.Entry{}, err
	}
	return s.Commit(ctx, x.ID, x.Head, store.Change{Op: op, Summary: summary, Tree: t}, now)
}

// planAgainst plans the job against theirs.
func planAgainst(ctx context.Context, s store.Store, j Job, theirs store.Tree) (store.Tree, Report, error) {
	base, err := load(ctx, s, j.Base)
	if err != nil {
		return store.Tree{}, Report{}, err
	}
	ours, err := load(ctx, s, j.Workspace)
	if err != nil {
		return store.Tree{}, Report{}, err
	}
	return Plan(base, ours, theirs, j.Mode)
}

// FilePlan is a plan against a profile's Bookmarks file, with what decides whether the file may be written.
type FilePlan struct {
	Path    string
	Target  store.Tree
	Report  Report
	SyncOn  bool // the profile syncs its bookmarks: a file written now can be overwritten or spread by Sync
	Running bool // the browser is running: it would overwrite the file
	doc     *chromefile.Doc
	theirs  store.Tree
}

// PlanFile reads the profile's Bookmarks file at path and plans the job against it. It writes nothing.
func PlanFile(ctx context.Context, s store.Store, j Job, path string) (FilePlan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FilePlan{}, err
	}
	doc, theirs, err := chromefile.Read(b)
	if err != nil {
		return FilePlan{}, err
	}
	target, rep, err := planAgainst(ctx, s, j, theirs)
	if err != nil {
		return FilePlan{}, err
	}
	return FilePlan{Path: path, Target: target, Report: rep, SyncOn: doc.SyncOn(), Running: chromefile.Running(filepath.Dir(path)),
		doc: doc, theirs: theirs}, nil
}

// FileOptions are the owner's answers a file write needs.
type FileOptions struct {
	Fingerprint string // the dry run's: the write refuses a profile that changed since
	SyncRiskOK  bool   // write although the profile syncs
	Backup      string // the workspace the profile's bookmarks as they are go to first
}

// Applied is what a write did.
type Applied struct {
	Report Report
	Backup store.Entry
	Kept   string // the file as it was, beside the profile's
	Drift  Report // planning again against the written file: empty when the write landed as planned
}

// ApplyFile writes the job into the profile's Bookmarks file at path, after the same checks as the dry run plus:
// the browser must be quit, a syncing profile needs SyncRiskOK, the file must still match the dry run's fingerprint,
// and a replace needs DropNew to equal what it drops. The profile's bookmarks go to a workspace first, and the file
// as it was is kept beside it. Afterwards the file is read back and planned against again.
func ApplyFile(ctx context.Context, s store.Store, j Job, path string, o FileOptions, now time.Time) (Applied, error) {
	p, err := PlanFile(ctx, s, j, path)
	if err != nil {
		return Applied{}, err
	}
	switch {
	case p.Running:
		return Applied{}, errors.New("the browser is running and would overwrite the file: quit it, or write through its extension")
	case p.SyncOn && !o.SyncRiskOK:
		return Applied{}, errors.New("this profile syncs its bookmarks: a file written now can be overwritten by the synced copy or spread to every device; write through the extension, or confirm the risk")
	case o.Fingerprint == "" || o.Fingerprint != p.Report.Fingerprint:
		return Applied{}, errors.New("the profile's bookmarks changed since the dry run: run it again")
	case j.Mode == Replace && j.DropNew != p.Report.DropNew:
		return Applied{}, fmt.Errorf("replace removes %d bookmarks the profile gained after the export: confirm that number", p.Report.DropNew)
	}
	if o.Backup == "" {
		return Applied{}, errors.New("no backup workspace named")
	}
	backup, err := Save(ctx, s, o.Backup, o.Backup, "backup", "The profile's bookmarks before a write-back", p.theirs, now)
	if err != nil {
		return Applied{}, fmt.Errorf("backup: %w", err)
	}
	data, err := p.doc.Encode(p.Target, now, newGUID)
	if err != nil {
		return Applied{}, err
	}
	kept, err := chromefile.Replace(path, data, path+".before-browser-foreman-"+now.UTC().Format("20060102-150405"))
	if err != nil {
		return Applied{}, err
	}
	again, err := PlanFile(ctx, s, Job{Workspace: j.Workspace, Base: j.Base, Mode: j.Mode}, path)
	if err != nil {
		return Applied{}, fmt.Errorf("written, but reading it back failed: %w", err)
	}
	return Applied{Report: p.Report, Backup: backup, Kept: kept, Drift: again.Report}, nil
}

// PlanTree plans the job against theirs as given: a profile read some other way (its file, for the extension to
// write: see WithPrefix).
func PlanTree(ctx context.Context, s store.Store, j Job, theirs store.Tree) (store.Tree, Report, error) {
	return planAgainst(ctx, s, j, theirs)
}

// WithPrefix is t with every id but the root's prefixed: a profile read from its file, whose ids are the ones its
// extension sees, given the extension's "ext-" so the extension can write a plan made from it.
func WithPrefix(t store.Tree, prefix string) store.Tree {
	var conv func(n *store.Node) *store.Node
	conv = func(n *store.Node) *store.Node {
		c := *n
		c.ID = prefix + n.ID
		c.Children = nil
		for _, k := range n.Children {
			c.Children = append(c.Children, conv(k))
		}
		return &c
	}
	root := *t.Root
	root.Children = nil
	for _, k := range t.Root.Children {
		root.Children = append(root.Children, conv(k))
	}
	return store.Tree{Root: &root}
}

// ReadFile reads a profile's Bookmarks file as a tree with the profile's own ids.
func ReadFile(path string) (store.Tree, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return store.Tree{}, err
	}
	_, t, err := chromefile.Read(b)
	return t, err
}

// PlanWorkspace plans the job against a workspace the browser's extension read (its ids are "ext-" and the
// browser's own), for the extension to write.
func PlanWorkspace(ctx context.Context, s store.Store, j Job, theirsWS string) (store.Tree, Report, error) {
	theirs, err := load(ctx, s, theirsWS)
	if err != nil {
		return store.Tree{}, Report{}, err
	}
	return planAgainst(ctx, s, j, theirs)
}

// newGUID is a random (version 4) UUID, as Chromium gives each bookmark.
func newGUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
