package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simtabi/browser-foreman/src/internal/dupes"
	"github.com/simtabi/browser-foreman/src/internal/store"
)

// find returns the node with id and its folder path, or an error naming the id.
func find(t store.Tree, id string) (*store.Node, string, error) {
	var hit *store.Node
	var at string
	var visit func(n *store.Node, path string)
	visit = func(n *store.Node, path string) {
		for _, c := range n.Children {
			if hit != nil {
				return
			}
			if c.ID == id {
				hit, at = c, path
				return
			}
			if c.Kind == store.KindFolder {
				p := c.Title
				if path != "" {
					p = path + "/" + c.Title
				}
				visit(c, p)
			}
		}
	}
	if t.Root != nil {
		visit(t.Root, "")
	}
	if hit == nil {
		return nil, "", fmt.Errorf("no bookmark or folder %q in this workspace (ids come from search_bookmarks or read_folder)", id)
	}
	return hit, at, nil
}

// ---- get_preview ----

type previewIn struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id" jsonschema:"a bookmark's id, from search_bookmarks or read_folder"`
}

type preview struct {
	item
	Rating    int      `json:"rating,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Added     string   `json:"added,omitempty"`
	Checked   string   `json:"checked,omitempty"`
	Code      int      `json:"code,omitempty"`
	Final     string   `json:"final_url,omitempty"`
	Confirmed bool     `json:"confirmed_dead,omitempty"`
	PageTitle string   `json:"page_title,omitempty"`
	PageText  string   `json:"page_description,omitempty"`
	Image     string   `json:"image,omitempty"`
	Canonical string   `json:"canonical,omitempty"`
	Framing   string   `json:"framing,omitempty"`
	Archive   string   `json:"archive_copy,omitempty"`
}

func (t *tools) getPreview(ctx context.Context, _ *mcp.CallToolRequest, in previewIn) (*mcp.CallToolResult, Untrusted, error) {
	l, err := t.load(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	n, folder, err := find(l.tree, in.ID)
	if err != nil {
		return nil, Untrusted{}, err
	}
	if n.Kind != store.KindBookmark {
		return nil, Untrusted{}, fmt.Errorf("%q is a folder: read_folder shows what is in it", n.Title)
	}
	p := preview{item: item{ID: n.ID, Title: n.Title, URL: n.URL, Folder: folder, Note: n.Note, Description: n.Description}, Rating: n.Rating, Tags: n.Tags}
	if n.Added > 0 {
		p.Added = time.Unix(n.Added, 0).UTC().Format(time.DateOnly)
	}
	if r, ok := l.links[n.URL]; ok {
		p.Status, p.Code, p.Final, p.Confirmed, p.Framing, p.Archive = r.Status, r.Code, r.Final, r.Confirmed, r.Frame, r.Archive
		if !r.Checked.IsZero() {
			p.Checked = r.Checked.UTC().Format(time.DateOnly)
		}
		if r.Meta != nil {
			p.PageTitle, p.PageText, p.Image, p.Canonical = r.Meta.Title, r.Meta.Description, r.Meta.Image, r.Meta.Canonical
		}
	}
	return nil, wrap(p), nil
}

// ---- health_report ----

type healthIn struct {
	Workspace string `json:"workspace"`
	Folder    string `json:"folder,omitempty" jsonschema:"only this folder and what is inside it (a path); the whole workspace when left out"`
	Limit     int    `json:"limit,omitempty" jsonschema:"bookmarks listed per kind of problem, 1-200; 25 when left out"`
}

type healthReport struct {
	Bookmarks   int            `json:"bookmarks"`
	ByStatus    map[string]int `json:"by_status" jsonschema:"ok, moved, needs-login, unverified, dead, local, skipped, and unchecked"`
	Dead        []item         `json:"dead,omitempty" jsonschema:"confirmed dead: dead on two checks at least a day apart"`
	Suspect     []item         `json:"suspected_dead,omitempty" jsonschema:"dead once so far: a later check confirms or clears it"`
	Moved       []moved        `json:"moved,omitempty"`
	Unverified  []item         `json:"unverified,omitempty"`
	NeedsLogin  []item         `json:"needs_login,omitempty"`
	OldestCheck string         `json:"oldest_check,omitempty"`
}

type moved struct {
	item
	Final string `json:"final_url"`
}

func (t *tools) healthReport(ctx context.Context, _ *mcp.CallToolRequest, in healthIn) (*mcp.CallToolResult, Untrusted, error) {
	l, err := t.load(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	top, err := folderAt(l.tree.Root, in.Folder)
	if err != nil {
		return nil, Untrusted{}, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 25
	}
	r := healthReport{ByStatus: map[string]int{}}
	var oldest time.Time
	add := func(list *[]item, it item) {
		if len(*list) < limit {
			*list = append(*list, it)
		}
	}
	walk(top, strings.Trim(in.Folder, "/"), func(b *store.Node, folder string) {
		r.Bookmarks++
		res, ok := l.links[b.URL]
		if !ok || res.Checked.IsZero() {
			r.ByStatus["unchecked"]++
			return
		}
		r.ByStatus[res.Status]++
		if oldest.IsZero() || res.Checked.Before(oldest) {
			oldest = res.Checked
		}
		it := item{ID: b.ID, Title: b.Title, URL: b.URL, Folder: folder, Status: res.Status}
		switch {
		case res.Status == "dead" && res.Confirmed:
			add(&r.Dead, it)
		case res.Status == "dead":
			add(&r.Suspect, it)
		case res.Status == "moved" && len(r.Moved) < limit:
			r.Moved = append(r.Moved, moved{it, res.Final})
		case res.Status == "unverified":
			add(&r.Unverified, it)
		case res.Status == "needs-login":
			add(&r.NeedsLogin, it)
		}
	})
	if !oldest.IsZero() {
		r.OldestCheck = oldest.UTC().Format(time.DateOnly)
	}
	return nil, wrap(r), nil
}

// ---- find_duplicates ----

type dupesIn struct {
	Workspace string `json:"workspace"`
	Limit     int    `json:"limit,omitempty" jsonschema:"groups returned, 1-200; 50 when left out"`
}

type dupesOut struct {
	Groups []dupes.Group `json:"groups"`
	Copies int           `json:"copies" jsonschema:"bookmarks that could go, across every group"`
	Shown  int           `json:"shown"`
	Total  int           `json:"total_groups"`
}

func (t *tools) findDuplicates(ctx context.Context, _ *mcp.CallToolRequest, in dupesIn) (*mcp.CallToolResult, Untrusted, error) {
	var out dupesOut
	if err := t.api.get(ctx, ws(in.Workspace)+"/duplicates", &out); err != nil {
		return nil, Untrusted{}, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out.Total = len(out.Groups)
	if len(out.Groups) > limit {
		out.Groups = out.Groups[:limit]
	}
	out.Shown = len(out.Groups)
	return nil, wrap(out), nil
}

// ---- stats ----

type statsOut struct {
	Bookmarks  int            `json:"bookmarks"`
	Folders    int            `json:"folders"`
	InTrash    int            `json:"in_trash"`
	Rated      int            `json:"rated"`
	Tagged     int            `json:"tagged"`
	WithNotes  int            `json:"with_notes"`
	TopFolders []countedName  `json:"top_folders" jsonschema:"bookmarks under each top-level folder"`
	TopTags    []countedName  `json:"top_tags,omitempty"`
	TopSites   []countedName  `json:"top_sites" jsonschema:"hosts with the most bookmarks, up to 15"`
	ByStatus   map[string]int `json:"by_status"`
	Oldest     string         `json:"oldest_added,omitempty"`
	Newest     string         `json:"newest_added,omitempty"`
}

type countedName struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func top(m map[string]int, n int) []countedName {
	out := make([]countedName, 0, len(m))
	for k, v := range m {
		out = append(out, countedName{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func (t *tools) stats(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, Untrusted, error) {
	l, err := t.load(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	s := statsOut{InTrash: len(l.tree.Trash), ByStatus: map[string]int{}}
	folders, tags, sites := map[string]int{}, map[string]int{}, map[string]int{}
	var oldest, newest int64
	var countFolders func(n *store.Node)
	countFolders = func(n *store.Node) {
		for _, c := range n.Children {
			if c.Kind == store.KindFolder {
				s.Folders++
				countFolders(c)
			}
		}
	}
	countFolders(l.tree.Root)
	walk(l.tree.Root, "", func(b *store.Node, folder string) {
		s.Bookmarks++
		first, _, _ := strings.Cut(folder, "/")
		if first == "" {
			first = "(top)"
		}
		folders[first]++
		if b.Rating > 0 {
			s.Rated++
		}
		if len(b.Tags) > 0 {
			s.Tagged++
		}
		for _, tg := range b.Tags {
			tags[tg]++
		}
		if b.Note != "" {
			s.WithNotes++
		}
		if h := host(b.URL); h != "" {
			sites[h]++
		}
		if res, ok := l.links[b.URL]; ok && !res.Checked.IsZero() {
			s.ByStatus[res.Status]++
		} else {
			s.ByStatus["unchecked"]++
		}
		if b.Added > 0 {
			if oldest == 0 || b.Added < oldest {
				oldest = b.Added
			}
			if b.Added > newest {
				newest = b.Added
			}
		}
	})
	s.TopFolders, s.TopTags, s.TopSites = top(folders, 0), top(tags, 15), top(sites, 15)
	if oldest > 0 {
		s.Oldest, s.Newest = time.Unix(oldest, 0).UTC().Format(time.DateOnly), time.Unix(newest, 0).UTC().Format(time.DateOnly)
	}
	return nil, wrap(s), nil
}
