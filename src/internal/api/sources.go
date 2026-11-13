package api

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/merge"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// maxSources is how many files one request may merge.
const maxSources = 20

// addSources merges bookmarks files, and optionally another workspace's bookmarks (?from=), into the workspace as one
// entry. Copies of the same page are folded into one; each copy that loses goes to the Trash as "duplicate of" the one
// kept. The body is one file (?name= labels it) or multipart/form-data with up to 20 "file" parts.
// ?separate=1 puts each source's folders under a folder named after it.
func (s *Server) addSources(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	saw, given, err := base(r, nil)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	q := r.URL.Query()
	var sources []merge.Source
	if from := q.Get("from"); from != "" {
		other, err := s.Data.Workspace(r.Context(), from)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 404, "no workspace "+from+" to add from")
			return
		} else if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if other.ID == x.ID {
			writeErr(w, 400, "a workspace cannot be added to itself")
			return
		}
		t, err := s.head(r, other)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		sources = append(sources, merge.Source{Name: "workspace " + other.Name, Tree: store.Tree{Root: t.Root}})
	}
	files, code, err := readFiles(w, r)
	if err != nil {
		writeErr(w, code, err.Error())
		return
	}
	for _, f := range files {
		t, _, err := importer.Read(bytes.NewReader(f.body))
		if err != nil {
			writeErr(w, 400, f.name+": "+err.Error())
			return
		}
		sources = append(sources, merge.Source{Name: f.name, Tree: t})
	}
	if len(sources) == 0 {
		writeErr(w, 400, "send at least one bookmarks file, or ?from= another workspace")
		return
	}
	prev, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	res, err := merge.Merge(prev, sources, q.Get("separate") == "1", s.Now().Unix())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	names := make([]string, len(sources))
	for i, src := range sources {
		names[i] = src.Name
	}
	summary := fmt.Sprintf("Added %d bookmarks from %s", res.Added, strings.Join(names, ", "))
	if len(res.Dups) > 0 {
		summary += fmt.Sprintf("; %d duplicate copies went to the Trash", len(res.Dups))
	}
	s.commitWith(w, r, x, saw, given, given, store.Change{Op: "merge", Summary: summary, Tree: res.Tree}, nil, "imported",
		map[string]any{"added": res.Added, "duplicates": len(res.Dups), "folders": res.Folder})
}

type upload struct {
	name string
	body []byte
}

// readFiles reads the files of a request: multipart "file" parts, or the whole body as one.
func readFiles(w http.ResponseWriter, r *http.Request) ([]upload, int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, importer.MaxSize)
	mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mt != "multipart/form-data" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, 413, errors.New("the files are larger than 64 MiB in all")
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return nil, 0, nil
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			name = "a file"
		}
		return []upload{{name, body}}, 0, nil
	}
	mr, err := r.MultipartReader()
	if err != nil || params["boundary"] == "" {
		return nil, 400, errors.New("bad multipart body")
	}
	var out []upload
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return out, 0, nil
		}
		if err != nil {
			return nil, 413, errors.New("the files are larger than 64 MiB in all, or the body is cut short")
		}
		if p.FormName() != "file" {
			continue
		}
		if len(out) == maxSources {
			return nil, 400, fmt.Errorf("at most %d files at once", maxSources)
		}
		b, err := io.ReadAll(p)
		if err != nil {
			return nil, 413, errors.New("the files are larger than 64 MiB in all")
		}
		name := p.FileName()
		if name == "" {
			name = fmt.Sprintf("file %d", len(out)+1)
		}
		out = append(out, upload{name, b})
	}
}

// duplicatesCSV lists the copies merged away: every top-level Trash entry whose reason is "duplicate of …", with the
// bookmark kept for it as it is now.
func (s *Server) duplicatesCSV(w http.ResponseWriter, r *http.Request) {
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	nodes, paths, folderPaths := map[string]*store.Node{}, map[string]string{}, map[string]string{"": ""}
	var walk func(n *store.Node, path string)
	walk = func(n *store.Node, path string) {
		folderPaths[n.ID] = path
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				p := c.Title
				if path != "" {
					p = path + "/" + c.Title
				}
				walk(c, p)
			} else {
				nodes[c.ID], paths[c.ID] = c, path
			}
		}
	}
	walk(t.Root, "")
	for _, x := range t.Trash {
		var bin func(n *store.Node)
		bin = func(n *store.Node) {
			if n.Kind == store.KindBookmark {
				nodes[n.ID], paths[n.ID] = n, "(in the Trash)"
			}
			for _, c := range n.Children {
				bin(c)
			}
		}
		bin(x.Node)
	}
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"kept_title", "kept_url", "kept_in", "removed_title", "removed_url", "removed_was_in", "removed_added", "trashed_at", "removed_id", "kept_id"})
	for _, d := range t.Trash {
		keptID, ok := strings.CutPrefix(d.Reason, merge.DuplicateOf)
		if !ok {
			continue
		}
		keptID = strings.TrimSpace(keptID)
		kept := nodes[keptID]
		row := []string{"(no longer in the workspace)", "", ""}
		if kept != nil {
			row = []string{safeCell(kept.Title), safeCell(kept.URL), safeCell(orOther(paths[keptID]))}
		}
		was, known := folderPaths[d.From]
		if !known {
			was = "(a folder no longer in the workspace)"
		}
		added := ""
		if d.Node.Added > 0 {
			added = time.Unix(d.Node.Added, 0).UTC().Format("2006-01-02")
		}
		row = append(row, safeCell(d.Node.Title), safeCell(d.Node.URL), safeCell(orOther(was)), added,
			time.Unix(d.At, 0).UTC().Format(time.RFC3339), d.Node.ID, keptID)
		_ = cw.Write(row)
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-duplicates.csv"`, x.ID))
	_, _ = w.Write(buf.Bytes())
}

func orOther(p string) string {
	if p == "" {
		return "(top level)"
	}
	return p
}

// safeCell stops a spreadsheet from running a cell as a formula: titles and addresses come from web pages.
func safeCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}
