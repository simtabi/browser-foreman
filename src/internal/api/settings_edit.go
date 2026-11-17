package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/simtabi/browser-foreman/src/internal/config"
)

// Settings that the page edits: the config files and .env in the config folder. Owner only, from this computer
// only, secrets write-only. What the page may change at all is settings.edit, which only the files set: "safe"
// keeps the address, mode, storage and token in the files; "all" lets the page change them after a confirmation.

// settingsFields lists every setting, its value (never a secret's), where it comes from and whether it can be
// changed here.
func (s *Server) settingsFields(w http.ResponseWriter, r *http.Request) {
	if s.Getenv == nil {
		writeErr(w, 501, "this server was not started with editable settings")
		return
	}
	fields, mode, err := config.Describe(s.ConfigPaths, s.Getenv)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"edit": mode, "fields": fields, "folder": s.ConfigPaths.Config, "local": fromThisComputer(r)})
}

// saveSettings applies {values: {key: value}, confirm}: all of it or none. AI settings take effect at once; the
// answer names the ones that need a restart.
func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	if s.Getenv == nil {
		writeErr(w, 501, "this server was not started with editable settings")
		return
	}
	if !fromThisComputer(r) {
		writeErr(w, http.StatusForbidden, "settings can be changed only from this computer")
		return
	}
	var in struct {
		Values  map[string]json.RawMessage `json:"values"`
		Confirm bool                       `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Values) == 0 {
		writeErr(w, 400, "values: nothing to change")
		return
	}
	res, err := config.Apply(s.ConfigPaths, s.Getenv, in.Values, in.Confirm)
	switch {
	case errors.Is(err, config.ErrConfirm):
		writeErr(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, config.ErrLocked):
		writeErr(w, http.StatusForbidden, err.Error())
		return
	case err != nil:
		writeErr(w, 400, err.Error())
		return
	}
	// AI settings apply now: rebuild the provider from what was just written.
	if s.NewAI != nil {
		if c, err := config.Load(s.ConfigPaths, s.Getenv); err == nil {
			s.setProvider(s.NewAI(c.AI))
		}
	}
	_, _ = s.Bus.Publish("settings.updated", map[string]any{"changed": res.Changed, "restart": res.Restart, "files": res.Files})
	fields, mode, _ := config.Describe(s.ConfigPaths, s.Getenv)
	writeJSON(w, 200, map[string]any{"result": res, "edit": mode, "fields": fields, "folder": s.ConfigPaths.Config, "local": true})
}

// fromThisComputer is a request whose connection comes from a loopback address.
func fromThisComputer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
