package api

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// maxBackup is the largest backup the server reads back.
const maxBackup = 256 << 20

// backup downloads every workspace with its whole history, and the link results, as gzipped JSON. Accounts, tokens
// and webhook secrets are not in it (see store.Backup).
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	b, err := store.MakeBackup(r.Context(), s.Data, s.Now())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(b); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := zw.Close(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	name := "browser-foreman-backup-" + s.Now().UTC().Format("2006-01-02_150405") + ".json.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	_, _ = s.Bus.Publish("backup.created", map[string]any{"workspaces": len(b.Workspaces), "trees": len(b.Trees), "links": len(b.Links)})
	_, _ = w.Write(buf.Bytes())
}

// ReadBackup reads a backup, gzipped or plain JSON.
func ReadBackup(r io.Reader) (store.Backup, error) {
	var b store.Backup
	br := bufio.NewReader(io.LimitReader(r, maxBackup+1))
	var src io.Reader = br
	if head, _ := br.Peek(2); len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return b, err
		}
		defer zr.Close()
		src = io.LimitReader(zr, 4*maxBackup)
	}
	err := json.NewDecoder(src).Decode(&b)
	return b, err
}

// restore adds what a backup holds and replaces nothing (store.Restore).
func (s *Server) restore(w http.ResponseWriter, r *http.Request) {
	b, err := ReadBackup(http.MaxBytesReader(w, r.Body, maxBackup))
	if err != nil {
		writeErr(w, 400, "not a readable backup: "+err.Error())
		return
	}
	// Not cut short when the browser disconnects: a restore runs to its end, or rolls itself back.
	res, err := store.Restore(context.WithoutCancel(r.Context()), s.Data, b, s.Now())
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	_, _ = s.Bus.Publish("backup.restored", map[string]any{"workspaces": res.Workspaces, "entries": res.Entries, "links": res.Links})
	writeJSON(w, 200, res)
}

// settings shows the configuration in force, without secrets. It is read-only on purpose: a token that could change
// the server's mode or address could open it to the network. Change settings in the config files and restart.
func (s *Server) settings(w http.ResponseWriter, _ *http.Request) {
	c := s.Cfg
	writeJSON(w, 200, map[string]any{
		"server":        map[string]any{"addr": c.Server.Addr, "mode": c.Server.Mode},
		"storage":       map[string]any{"driver": c.Storage.Driver, "path": c.Storage.Path},
		"webhooks":      map[string]any{"timeout": c.Webhooks.Timeout.String(), "attempts": c.Webhooks.Attempts},
		"auth":          map[string]any{"session_ttl": c.Auth.SessionTTL.String()},
		"api_token_set": c.APIToken != "",
		"note":          "Read-only. Change these in ~/.config/browser-foreman (config.json, .env) or BROFO_* variables, then restart the server.",
	})
}
