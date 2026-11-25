package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/simtabi/browser-foreman/src/internal/config"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/importer"
	"github.com/simtabi/browser-foreman/src/internal/media"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// servePicture answers /favicons/<name> and /thumbs/<name> for the page's <img> tags, which send no token: in local
// mode the localhost and same-site checks guard them, in accounts mode a signed-in session is needed, since the
// pictures show which sites are bookmarked.
// mayViewPictures is for the page's own <img> loads, which carry cookies but no token: a session in accounts mode,
// the local cookie in local mode. Icon names are a hash of the site's host, so without this another site on this
// computer (a dev server on localhost:3000 is same-site) could test which sites are bookmarked.
func (s *Server) mayViewPictures(r *http.Request) bool {
	if _, ok := s.who(r); ok { // a token, or a session in accounts mode
		return true
	}
	return s.Cfg.Server.Mode == config.ModeLocal && s.localSignedIn(r)
}

func (s *Server) servePicture(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.mayViewPictures(r) {
			writeErr(w, 401, "sign in first")
			return
		}
		path, ct, err := s.media(r).Path(kind, r.PathValue("name"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox") // an SVG never runs
		_, _ = w.Write(b)
	}
}

// keepIcons saves the site icons an imported file carries, for sites that have none yet.
func (s *Server) keepIcons(b []byte) int {
	n, m := 0, s.mediaNow()
	for href, uri := range importer.IconURIs(b) {
		if ok, _ := m.SaveIconURI(media.Host(href), uri); ok {
			n++
		}
	}
	return n
}

// mediaFor names the icons and snapshots there are for a tree's bookmarks: {"icons": {host: file}, "thumbs":
// {url: key}}, for the page to show.
func (s *Server) mediaFor(t store.Tree) map[string]any {
	icons, thumbs := map[string]string{}, map[string]string{}
	seen, m := map[string]bool{}, s.mediaNow().Index() // one read of each folder, not one per bookmark
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				walk(c)
				continue
			}
			if h := media.Host(c.URL); h != "" && !seen[h] {
				seen[h] = true
				if f := m.Icon(h); f != "" {
					icons[h] = f
				}
			}
			if m.HasThumb(c.URL) {
				thumbs[c.URL] = media.ThumbKey(c.URL)
			}
		}
	}
	walk(t.Root)
	for _, x := range t.Trash {
		walk(&store.Node{Kind: store.KindFolder, Children: []*store.Node{x.Node}})
	}
	return map[string]any{"icons": icons, "thumbs": thumbs}
}

// snapJob takes page snapshots in the background.
// forgetOldSnapJobs keeps the newest keptChecks snapshot jobs and the running one; s.snaps.mu is held.
func (s *Server) forgetOldSnapJobs() {
	for len(s.snaps.jobs) > keptChecks {
		var oldest *snapJob
		for _, j := range s.snaps.jobs {
			if j != s.snaps.live && (oldest == nil || j.Started.Before(oldest.Started)) {
				oldest = j
			}
		}
		if oldest == nil {
			return
		}
		delete(s.snaps.jobs, oldest.ID)
	}
}

type snapJob struct {
	mu        sync.Mutex
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Total     int       `json:"total"`
	Done      int       `json:"done"`
	Failed    int       `json:"failed"`
	Skipped   int       `json:"skipped"` // not public pages
	State     string    `json:"state"`   // running, done, cancelled
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitzero"`
	LastError string    `json:"last_error,omitempty"`
	cancel    func()
}

func (j *snapJob) view() snapJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	return snapJob{ID: j.ID, Workspace: j.Workspace, Total: j.Total, Done: j.Done, Failed: j.Failed, Skipped: j.Skipped, State: j.State,
		Started: j.Started, Ended: j.Ended, LastError: j.LastError}
}

type snapRunner struct {
	mu   sync.Mutex
	live *snapJob
	jobs map[string]*snapJob
}

func (s *Server) snapper() *media.Snapper {
	if s.Snapper != nil {
		return s.Snapper
	}
	var paths []string
	if s.Browsers != nil && s.Browsers.Platform != nil {
		for _, b := range s.Browsers.Installed() {
			if b.Engine == "chromium" && b.Path != "" {
				paths = append(paths, b.Path)
			}
		}
	}
	p, _ := store.LoadPrefs(context.Background(), s.Data)
	return &media.Snapper{Store: s.mediaWith(p), Chrome: media.FindChrome(paths), Width: p.SnapshotWidth, Height: p.SnapshotHeight,
		Wait: time.Duration(p.SnapshotWaitSeconds) * time.Second}
}

// media is where site icons and page snapshots are kept: the folder the owner chose (Preferences › snapshot_dir), or
// the data folder's media/.
func (s *Server) media(r *http.Request) media.Store {
	p, _ := store.LoadPrefs(r.Context(), s.Data)
	return s.mediaWith(p)
}

// mediaNow is media for work that has no request (an import, the page's boot data).
func (s *Server) mediaNow() media.Store {
	p, _ := store.LoadPrefs(context.Background(), s.Data)
	return s.mediaWith(p)
}

func (s *Server) mediaWith(p store.Prefs) media.Store {
	if p.SnapshotDir != "" {
		return media.Store{Dir: p.SnapshotDir}
	}
	return s.Media
}

// startSnapshots takes snapshots of a workspace's pages (or those under nodes) that have none, or all with force.
func (s *Server) startSnapshots(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Workspace string   `json:"workspace"`
		Nodes     []string `json:"nodes"`
		Force     bool     `json:"force"`
	}
	if !decode(w, r, &in) {
		return
	}
	x, err := s.Data.Workspace(r.Context(), in.Workspace)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "no workspace with that id")
		return
	} else if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	t, err := s.head(r, x)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var urls []string
	have := s.media(r).Index()
	for _, u := range webURLs(t, in.Nodes) {
		if in.Force || !have.HasThumb(u) {
			urls = append(urls, u)
		}
	}
	sn := s.snapper()
	if sn.Chrome == "" && len(urls) > 0 {
		writeErr(w, 409, "no Chrome or Chromium browser is installed to take snapshots with")
		return
	}
	s.snaps.mu.Lock()
	defer s.snaps.mu.Unlock()
	if s.snaps.jobs == nil {
		s.snaps.jobs = map[string]*snapJob{}
	}
	if s.snaps.live != nil {
		writeErr(w, 409, "snapshots are already being taken: "+s.snaps.live.ID)
		return
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	ctx, cancel := context.WithCancel(context.Background())
	j := &snapJob{ID: "sn_" + hex.EncodeToString(b), Workspace: x.ID, Total: len(urls), State: "running", Started: s.Now(), cancel: cancel}
	s.snaps.jobs[j.ID], s.snaps.live = j, j
	s.forgetOldSnapJobs()
	go s.runSnapshots(ctx, j, sn, urls)
	writeJSON(w, 202, map[string]any{"job": j.view()})
}

func (s *Server) runSnapshots(ctx context.Context, j *snapJob, sn *media.Snapper, urls []string) {
	work := make(chan string)
	var wg sync.WaitGroup
	for range 2 { // two pages at a time: a browser per page is heavy
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range work {
				err := sn.Snap(ctx, u)
				j.mu.Lock()
				switch {
				case err == nil:
					j.Done++
				case errors.Is(err, media.ErrNotPublic):
					j.Skipped++
				default:
					j.Failed++
					j.LastError = err.Error()
				}
				j.mu.Unlock()
			}
		}()
	}
	for _, u := range urls {
		select {
		case work <- u:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(work)
	wg.Wait()
	j.mu.Lock()
	j.State, j.Ended = "done", s.Now()
	if ctx.Err() != nil {
		j.State = "cancelled"
	}
	j.mu.Unlock()
	s.snaps.mu.Lock()
	s.snaps.live = nil
	s.snaps.mu.Unlock()
	v := j.view()
	_, _ = s.Bus.Publish("snapshots.finished", map[string]any{"job": v.ID, "workspace": v.Workspace, "done": v.Done, "failed": v.Failed, "state": v.State})
}

func (s *Server) snapJobOf(w http.ResponseWriter, r *http.Request) (*snapJob, bool) {
	s.snaps.mu.Lock()
	j := s.snaps.jobs[r.PathValue("id")]
	s.snaps.mu.Unlock()
	if j == nil {
		writeErr(w, 404, "no snapshot job with that id")
		return nil, false
	}
	return j, true
}

func (s *Server) getSnapshots(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.snapJobOf(w, r); ok {
		writeJSON(w, 200, map[string]any{"job": j.view()})
	}
}

func (s *Server) cancelSnapshots(w http.ResponseWriter, r *http.Request) {
	if j, ok := s.snapJobOf(w, r); ok {
		j.cancel()
		writeJSON(w, 200, map[string]any{"job": j.view()})
	}
}
