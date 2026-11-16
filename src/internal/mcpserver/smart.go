package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// smartMatch is the page's own test for a smart folder (web/src/ts/page/page.ts, matcher): the Show filter by link
// status, then the search text: "#tag" matches a tag exactly, anything else must appear, as typed, in the title,
// address, folder, description or tags.
func smartMatch(s store.SmartFolder, it item, tags []string) bool {
	switch s.Filter {
	case "", "all":
	default:
		if it.Status != s.Filter {
			return false
		}
	}
	q := strings.ToLower(strings.TrimSpace(s.Q))
	if q == "" {
		return true
	}
	if strings.HasPrefix(q, "#") && !strings.Contains(q, " ") {
		for _, t := range tags {
			if strings.ToLower(t) == q[1:] {
				return true
			}
		}
		return false
	}
	hay := strings.ToLower(strings.Join(append([]string{it.Title, it.URL, it.Folder, it.Description}, tags...), " "))
	return strings.Contains(hay, q)
}

// pageOnly are the Show filters that live in the page alone: untouched folders and removed duplicates.
var pageOnly = map[string]string{"prot": "untouched folders", "dupes": "removed duplicates"}

func (t *tools) smartFolders(ctx context.Context, workspace string) ([]store.SmartFolder, error) {
	var p struct {
		SmartFolders []store.SmartFolder `json:"smart_folders"`
	}
	if err := t.api.get(ctx, "preferences", &p); err != nil {
		return nil, err
	}
	var out []store.SmartFolder
	for _, s := range p.SmartFolders {
		if s.Workspace == "" || s.Workspace == workspace {
			out = append(out, s)
		}
	}
	return out, nil
}

type smartView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Search string `json:"search,omitempty"`
	Show   string `json:"show,omitempty"`
	Count  *int   `json:"count,omitempty" jsonschema:"bookmarks it holds now; left out for a filter only the page has"`
	Shared bool   `json:"every_workspace,omitempty"`
}

func (t *tools) listSmart(ctx context.Context, _ *mcp.CallToolRequest, in wsIn) (*mcp.CallToolResult, Untrusted, error) {
	all, err := t.smartFolders(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	l, err := t.load(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	out := []smartView{}
	for _, s := range all {
		v := smartView{ID: s.ID, Name: s.Name, Search: s.Q, Show: s.Filter, Shared: s.Workspace == ""}
		if _, ok := pageOnly[s.Filter]; !ok {
			n := 0
			walk(l.tree.Root, "", func(b *store.Node, folder string) {
				if smartMatch(s, l.view(b, folder), b.Tags) {
					n++
				}
			})
			v.Count = &n
		}
		out = append(out, v)
	}
	return nil, wrap(map[string]any{"smart_folders": out}), nil
}

type openSmartIn struct {
	Workspace string `json:"workspace"`
	ID        string `json:"id" jsonschema:"a smart folder's id, from smart_folders"`
	Limit     int    `json:"limit,omitempty" jsonschema:"at most this many bookmarks (default 50, at most 500)"`
}

func (t *tools) openSmart(ctx context.Context, _ *mcp.CallToolRequest, in openSmartIn) (*mcp.CallToolResult, Untrusted, error) {
	all, err := t.smartFolders(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	var s *store.SmartFolder
	for i := range all {
		if all[i].ID == in.ID {
			s = &all[i]
		}
	}
	if s == nil {
		return nil, Untrusted{}, fmt.Errorf("no smart folder %q in workspace %q (smart_folders lists them)", in.ID, in.Workspace)
	}
	if what, ok := pageOnly[s.Filter]; ok {
		return nil, Untrusted{}, fmt.Errorf("%q shows %s, which only the web page can list", s.Name, what)
	}
	l, err := t.load(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	limit := clamp(in.Limit, 50, 500)
	results, total := []item{}, 0
	walk(l.tree.Root, "", func(b *store.Node, folder string) {
		it := l.view(b, folder)
		if !smartMatch(*s, it, b.Tags) {
			return
		}
		total++
		if len(results) < limit {
			results = append(results, it)
		}
	})
	return nil, wrap(map[string]any{"name": s.Name, "total": total, "results": results}), nil
}
