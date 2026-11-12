package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/edit"
	"github.com/simtabi/browser-foreman/src/internal/exporter"
	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Every change to a workspace is one history entry made on the head the caller last saw. A caller says which head
// that was with If-Match: "<head>" (the ETag a workspace read answers with) or "base" in a /changes body; a stale one
// is refused (412 or 409) instead of overwriting a change it never saw. Without either, the change applies to the
// current head.

func nodeID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "b" + hex.EncodeToString(b)
}

// base reads the head the caller saw: from If-Match, else from body (nil: none given).
func base(r *http.Request, body *int) (int, bool, error) {
	if v := strings.Trim(r.Header.Get("If-Match"), `" `); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return 0, false, errors.New(`If-Match must be a head number, as the workspace's ETag gives it ("12")`)
		}
		return n, true, nil
	}
	if body != nil {
		return *body, true, nil
	}
	return 0, false, nil
}

// workspace answers 404 for a missing workspace and returns it otherwise.
func (s *Server) workspace(w http.ResponseWriter, r *http.Request) (store.Workspace, bool) {
	x, err := s.Data.Workspace(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return x, false
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return x, false
	}
	return x, true
}

// head returns the workspace's current tree, an empty one before its first entry.
func (s *Server) head(r *http.Request, x store.Workspace) (store.Tree, error) {
	if x.Head == 0 {
		return store.Tree{Root: &store.Node{ID: "root", Kind: store.KindFolder}}, nil
	}
	t, _, err := s.Data.State(r.Context(), x.ID, 0)
	return t, err
}

// commit records a change and publishes it: the edit events first, then workspace.<kind> with the entry.
func (s *Server) commit(w http.ResponseWriter, r *http.Request, x store.Workspace, saw int, given bool, viaHeader bool, c store.Change, events []edit.Event, kind string) {
	b := x.Head
	if given {
		b = saw
	}
	e, err := s.Data.Commit(r.Context(), x.ID, b, c, s.Now())
	if errors.Is(err, store.ErrConflict) {
		code := http.StatusConflict
		if viaHeader {
			code = http.StatusPreconditionFailed
		}
		writeErr(w, code, fmt.Sprintf("the workspace is at %d now, not %d: reload and try again", x.Head, b))
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, ev := range events {
		data := map[string]any{"workspace": x.ID, "seq": e.Seq}
		for k, v := range ev.Data {
			data[k] = v
		}
		_, _ = s.Bus.Publish(ev.Type, data)
	}
	_, _ = s.Bus.Publish("workspace."+kind, map[string]any{"workspace": x.ID, "seq": e.Seq, "op": e.Op, "summary": e.Summary})
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(e.Seq)))
	if events == nil {
		events = []edit.Event{}
	}
	writeJSON(w, 201, map[string]any{"entry": e, "events": events})
}

// change applies edit ops to the head and commits them as one entry.
func (s *Server) change(w http.ResponseWriter, r *http.Request, ops []edit.Op, bodyBase *int) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	saw, given, err := base(r, bodyBase)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	viaHeader := r.Header.Get("If-Match") != ""
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	next, events, err := edit.Apply(t, ops, s.Now(), nodeID)
	if edit.IsRefusal(err) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.commit(w, r, x, saw, given, viaHeader, store.Change{Op: "edit", Summary: edit.Summary(events), Tree: next}, events, "committed")
}

func (s *Server) postChanges(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Base *int      `json:"base"`
		Ops  []edit.Op `json:"ops"`
	}
	if decode(w, r, &in) {
		s.change(w, r, in.Ops, in.Base)
	}
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Parent string  `json:"parent"`
		Index  *int    `json:"index"`
		Kind   string  `json:"kind"`
		Title  *string `json:"title"`
		URL    *string `json:"url"`
		Note   *string `json:"note"`
	}
	if decode(w, r, &in) {
		s.change(w, r, []edit.Op{{Op: "create", Parent: in.Parent, Index: in.Index, Kind: in.Kind, Title: in.Title, URL: in.URL, Note: in.Note}}, nil)
	}
}

func (s *Server) updateNode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title *string `json:"title"`
		URL   *string `json:"url"`
		Note  *string `json:"note"`
	}
	if decode(w, r, &in) {
		s.change(w, r, []edit.Op{{Op: "update", ID: r.PathValue("node"), Title: in.Title, URL: in.URL, Note: in.Note}}, nil)
	}
}

func (s *Server) moveNodes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs    []string `json:"ids"`
		Parent string   `json:"parent"`
		Index  *int     `json:"index"`
	}
	if decode(w, r, &in) {
		s.change(w, r, []edit.Op{{Op: "move", IDs: in.IDs, Parent: in.Parent, Index: in.Index}}, nil)
	}
}

func (s *Server) trashNodes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs    []string `json:"ids"`
		Reason string   `json:"reason"`
	}
	if decode(w, r, &in) {
		s.change(w, r, []edit.Op{{Op: "trash", IDs: in.IDs, Reason: in.Reason}}, nil)
	}
}

func (s *Server) restoreNodes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if decode(w, r, &in) {
		s.change(w, r, []edit.Op{{Op: "restore", IDs: in.IDs}}, nil)
	}
}

// emptyTrash deletes what is in the Trash for good: ?ids=a,b for some, nothing for all. History still holds them, so
// undo brings them back.
func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	var ids []string
	if v := r.URL.Query().Get("ids"); v != "" {
		ids = strings.Split(v, ",")
	}
	s.change(w, r, []edit.Op{{Op: "purge", IDs: ids}}, nil)
}

func (s *Server) getTrash(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	items := t.Trash
	if items == nil {
		items = []*store.Trashed{}
	}
	writeJSON(w, 200, map[string]any{"trash": items})
}

func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := store.ValidID(in.ID); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = in.ID
	}
	if _, err := s.Data.Workspace(r.Context(), in.ID); err == nil {
		writeErr(w, 409, "a workspace with that id exists")
		return
	}
	x, err := s.Data.CreateWorkspace(r.Context(), in.ID, strings.TrimSpace(in.Name), s.Now())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("workspace.created", map[string]any{"workspace": x.ID, "name": x.Name})
	writeJSON(w, 201, map[string]any{"workspace": wsOut{x, 0, 0}})
}

// importInto replaces the workspace's tree with a bookmarks file (Chrome JSON or any exported HTML), as one entry:
// the previous tree stays in history, so undo brings it back.
func (s *Server) importInto(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, importer.MaxSize))
	if err != nil {
		writeErr(w, 413, "the file is larger than 64 MiB")
		return
	}
	t, format, err := importer.Read(bytes.NewReader(body))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	bm, fo := t.Count()
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "a file"
	}
	summary := fmt.Sprintf("Imported %d bookmarks in %d folders from %s (%s)", bm, fo, name, format)
	s.commit(w, r, x, saw, given, given, store.Change{Op: "import", Summary: summary, Tree: t}, nil, "imported")
}

func (s *Server) undoRedo(op string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		x, ok := s.workspace(w, r)
		if !ok {
			return
		}
		run := store.Undo
		kind := "undone"
		if op == "redo" {
			run, kind = store.Redo, "redone"
		}
		e, err := run(r.Context(), s.Data, x.ID, s.Now())
		if errors.Is(err, store.ErrNothing) {
			writeErr(w, 409, err.Error())
			return
		}
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		_, _ = s.Bus.Publish("workspace."+kind, map[string]any{"workspace": x.ID, "seq": e.Seq, "op": e.Op, "summary": e.Summary})
		w.Header().Set("ETag", strconv.Quote(strconv.Itoa(e.Seq)))
		writeJSON(w, 201, map[string]any{"entry": e, "events": []edit.Event{}})
	}
}

// rollback makes the tree as it was after entry "to" the newest state, as a new entry; it can be undone like any change.
func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To int `json:"to"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	if in.To < 1 || in.To >= x.Head {
		writeErr(w, 400, fmt.Sprintf("to must be an earlier entry, 1 to %d", x.Head-1))
		return
	}
	t, e, err := s.Data.State(r.Context(), x.ID, in.To)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	summary := fmt.Sprintf("Went back to %d: %s", in.To, e.Summary)
	s.commit(w, r, x, saw, given, given, store.Change{Op: "rollback", Summary: summary, Tree: t}, nil, "rolled_back")
}

// exportWorkspace downloads the tree as an HTML bookmarks file (?format=html, the default) or Chrome's JSON
// (?format=chrome); ?at=N exports an earlier state.
func (s *Server) exportWorkspace(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	at := 0
	if v := r.URL.Query().Get("at"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > x.Head {
			writeErr(w, 400, fmt.Sprintf("at must be an entry number, 1 to %d", x.Head))
			return
		}
		at = n
	}
	t, err := s.head(r, x)
	if at > 0 {
		t, _, err = s.Data.State(r.Context(), x.ID, at)
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var buf bytes.Buffer
	name := x.ID
	switch r.URL.Query().Get("format") {
	case "", "html":
		err = exporter.Netscape(&buf, t)
		name += ".html"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case "chrome":
		err = exporter.Chrome(&buf, t)
		name += ".json"
		w.Header().Set("Content-Type", "application/json")
	default:
		writeErr(w, 400, "format must be html or chrome")
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// A download, never a page: nothing in it runs, whatever the titles hold.
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	_, _ = s.Bus.Publish("workspace.exported", map[string]any{"workspace": x.ID, "format": r.URL.Query().Get("format"), "at": at})
	_, _ = w.Write(buf.Bytes())
}

// putTree saves a whole tree at once, as the web interface does. The server works out what changed (edit.Diff) and
// publishes those events, so webhooks see the same moves and edits as from the single-change routes. A save that
// changes nothing records no entry and answers 200.
func (s *Server) putTree(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	var next store.Tree
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, importer.MaxSize))
	if err := dec.Decode(&next); err != nil {
		writeErr(w, 400, "bad JSON body: "+err.Error())
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	prev, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	events, err := edit.Diff(prev, next)
	if edit.IsRefusal(err) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if len(events) == 0 && (!given || saw == x.Head) {
		w.Header().Set("ETag", strconv.Quote(strconv.Itoa(x.Head)))
		writeJSON(w, 200, map[string]any{"entry": nil, "events": []edit.Event{}})
		return
	}
	s.commit(w, r, x, saw, given, given, store.Change{Op: "edit", Summary: edit.Summary(events), Tree: next}, events, "committed")
}
