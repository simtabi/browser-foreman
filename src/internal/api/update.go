package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// A file or path source keeps the tree it held last time as JSON (a path source's file can be a large database); a
// file sent in keeps its bytes. read is one source as it was last read and as it is now, with the bytes to keep for next time.
type read struct {
	src  store.Source
	up   merge.Upstream
	body []byte
}

// readSource parses a source's kept bytes: a tree for a workspace, a bookmarks file otherwise.
func readSource(kind string, b []byte) (store.Tree, error) {
	if kind == store.SourceWorkspace || kind == store.SourcePath {
		var t store.Tree
		if err := json.Unmarshal(b, &t); err != nil || t.Root == nil {
			return store.Tree{}, errors.New("the kept copy is unreadable")
		}
		return t, nil
	}
	t, _, err := importer.Read(bytes.NewReader(b))
	return t, err
}

// upstreams reads every source of a workspace again. Sources that cannot be read are named in missing.
func (s *Server) upstreams(ctx context.Context, x store.Workspace) (out []read, missing []string, err error) {
	list, err := s.Data.SourceList(ctx, x.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, src := range list {
		before, err := s.Data.SourceBody(ctx, x.ID, src.ID)
		if err != nil {
			missing = append(missing, src.Name+": no copy of it was kept")
			continue
		}
		old, err := readSource(src.Kind, before)
		if err != nil {
			missing = append(missing, src.Name+": "+err.Error())
			continue
		}
		nowBody := before
		switch src.Kind {
		case store.SourcePath:
			t, _, err := importer.ReadFile(src.Path)
			if err != nil {
				missing = append(missing, src.Name+": "+err.Error())
				continue
			}
			nowBody, _ = json.Marshal(store.Tree{Root: t.Root})
		case store.SourceWorkspace:
			other, err := s.Data.Workspace(ctx, src.From)
			if err != nil {
				missing = append(missing, src.Name+": that workspace is gone")
				continue
			}
			t := store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder}}
			if other.Head > 0 {
				if t, _, err = s.Data.State(ctx, other.ID, 0); err != nil {
					return nil, nil, err
				}
			}
			nowBody, _ = json.Marshal(store.Tree{Root: t.Root})
		}
		now, err := readSource(src.Kind, nowBody)
		if err != nil {
			missing = append(missing, src.Name+": "+err.Error())
			continue
		}
		out = append(out, read{src, merge.Upstream{Name: src.Name, Before: old, Now: now, Separate: src.Separate}, nowBody})
	}
	return out, missing, nil
}

func ups(rs []read) []merge.Upstream {
	out := make([]merge.Upstream, len(rs))
	for i, r := range rs {
		out[i] = r.up
	}
	return out
}

// updatePreview says what Update would do: new upstream, retitled upstream, gone upstream (kept), unreadable sources.
func (s *Server) updatePreview(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rs, missing, err := s.upstreams(r.Context(), x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if missing == nil {
		missing = []string{}
	}
	writeJSON(w, 200, map[string]any{"plan": merge.PlanUpdate(t, ups(rs)), "sources": len(rs), "missing": missing})
}

// updateApply brings the upstream changes in as one entry: new bookmarks added (copies among them to the Trash), titles
// changed upstream taken where they were not renamed here. Nothing is removed. Each source then remembers what it held.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rs, missing, err := s.upstreams(r.Context(), x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(rs) == 0 {
		writeErr(w, 409, "this workspace has no source to read again (add files to it, or merge another workspace in)")
		return
	}
	plan := merge.PlanUpdate(t, ups(rs))
	if len(plan.New)+len(plan.Retitle) == 0 {
		writeJSON(w, 200, map[string]any{"entry": nil, "plan": plan, "missing": missing})
		return
	}
	res, err := merge.ApplyUpdate(t, ups(rs), plan, s.Now().Unix())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	summary := fmt.Sprintf("Updated from source: %d new, %d retitled upstream, %d gone upstream (kept)", res.Added, len(plan.Retitle), len(plan.Gone))
	if len(res.Dups) > 0 {
		summary += fmt.Sprintf("; %d duplicate copies went to the Trash", len(res.Dups))
	}
	s.commitWith(w, r, x, saw, given, given, store.Change{Op: "update", Summary: summary, Tree: res.Tree}, nil, "updated",
		map[string]any{"plan": plan, "missing": missing}, func(store.Entry) error {
			for _, rd := range rs {
				rd.src.Count = count(rd.up.Now)
				rd.src.Added = s.Now().UTC()
				if err := s.Data.AddSource(r.Context(), x.ID, rd.src, rd.body); err != nil {
					return err
				}
			}
			return nil
		})
}

// listSources is a workspace's sources, without their kept bytes.
func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	list, err := s.Data.SourceList(r.Context(), x.ID)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"sources": list})
}
