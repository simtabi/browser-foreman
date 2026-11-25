package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg" // the extension sends the visible tab as JPEG or PNG
	_ "image/png"
	"net/http"
	"net/url"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/edit"
	"github.com/simtabi/browser-foreman/src/internal/media"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// maxPicture caps the picture an extension sends with a saved page (a data URL of the visible tab).
const maxPicture = 8 << 20

// savedFolder is the top-level folder a browser's saved pages go into.
func savedFolder(browser string) string { return "Saved from " + browser }

// savePage keeps the page an extension's popup saved: one bookmark in the workspace's "Saved from <browser>" folder
// (made when missing), and its picture as the page's snapshot. It only adds: an address the workspace already holds
// is left where it is (200, already), and nothing else in the workspace is changed.
func (s *Server) savePage(w http.ResponseWriter, r *http.Request, browser string) {
	if store.RoleRank(principal(r).Role) < store.RoleRank(store.RoleEditor) {
		writeErr(w, 403, "this needs the editor role")
		return
	}
	var in struct {
		Workspace string `json:"workspace"`
		URL       string `json:"url"`
		Title     string `json:"title"`
		Picture   string `json:"picture"` // data:image/jpeg;base64,… or data:image/png;base64,…; optional
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPicture*4/3+64<<10)) // the picture, as base64, and the rest
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeErr(w, 400, "body must be {workspace, url, title, picture}: "+err.Error())
		return
	}
	u, err := url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeErr(w, 400, "only http and https pages are saved")
		return
	}
	if err := edit.CheckURL(in.URL); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var pic image.Image
	if in.Picture != "" {
		if pic, err = picture(in.Picture); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
	}
	r.SetPathValue("id", in.Workspace)
	x, ok := s.workspace(w, r)
	if !ok {
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if found := findURL(t.Root, in.URL); found != nil {
		s.keepPicture(r, in.URL, pic) // only where the page has none yet
		writeJSON(w, 200, map[string]any{"already": true, "node": found.ID})
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = u.Host
	}
	var ops []edit.Op
	folder := ""
	for _, c := range t.Root.Children {
		if c.Kind == store.KindFolder && c.Title == savedFolder(browser) {
			folder = c.ID
		}
	}
	next := t
	var events []edit.Event
	if folder == "" {
		name := savedFolder(browser)
		next, events, err = edit.Apply(t, []edit.Op{{Op: "create", Parent: "root", Kind: store.KindFolder, Title: &name}}, s.Now(), NodeID)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		folder = next.Root.Children[len(next.Root.Children)-1].ID
	}
	ops = append(ops, edit.Op{Op: "create", Parent: folder, Kind: store.KindBookmark, Title: &title, URL: &in.URL})
	next, more, err := edit.Apply(next, ops, s.Now(), NodeID)
	if edit.IsRefusal(err) {
		writeErr(w, 400, err.Error())
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	events = append(events, more...)
	// The picture is kept once the bookmark is: a save the workspace refuses (locked, changed meanwhile) keeps none.
	s.commitWith(w, r, x, 0, false, false, store.Change{Op: "edit", Summary: "Saved " + title + " from " + browser, Tree: next}, events, "committed",
		nil, func(store.Entry) error { s.keepPicture(r, in.URL, pic); return nil })
}

// keepPicture keeps a saved page's picture as its snapshot, unless it is blank or the page has one already (snapshots
// are kept by address, for every workspace; a Refresh takes a new one). A failure to keep it does not fail the save.
func (s *Server) keepPicture(r *http.Request, raw string, pic image.Image) {
	m := s.media(r)
	if pic == nil || media.Blank(pic) || m.HasThumb(raw) {
		return
	}
	if err := m.SaveThumb(raw, pic); err != nil {
		s.say("A saved page's picture was not kept: %v", err)
	}
}

// picture decodes a data URL of a JPEG or PNG, refusing anything else or anything larger than maxPicture.
func picture(data string) (image.Image, error) {
	head, b64, ok := strings.Cut(data, ",")
	if !ok || (head != "data:image/jpeg;base64" && head != "data:image/png;base64") {
		return nil, errors.New("picture must be a JPEG or PNG data URL")
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) > maxPicture {
		return nil, errors.New("picture is not readable base64, or is larger than 8 MB")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width*cfg.Height > 40_000_000 {
		return nil, errors.New("picture is not a readable image, or is too large")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("picture is not a readable image")
	}
	return img, nil
}

// findURL is the first live bookmark (not in the Trash) with this address.
func findURL(n *store.Node, raw string) *store.Node {
	for _, c := range n.Children {
		if c.Kind == store.KindBookmark && c.URL == raw {
			return c
		}
		if found := findURL(c, raw); found != nil {
			return found
		}
	}
	return nil
}

// pageLink tells an extension what Browser Foreman knows about one address, for its toolbar badge: its last status,
// and whether it is confirmed dead. Only the status, never where it is bookmarked.
func (s *Server) pageLink(w http.ResponseWriter, r *http.Request, _ string) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		writeErr(w, 400, "url is required")
		return
	}
	got, err := s.Data.Links(r.Context(), []string{raw})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	l, ok := got[raw]
	if !ok {
		writeJSON(w, 200, map[string]any{"status": "unchecked"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": l.Status, "confirmed": l.Confirmed, "archive": l.Archive, "checked": l.Checked})
}
