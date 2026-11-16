package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Scope is what an MCP client may change: nothing unless Writes, and with Folders only what is inside them.
type Scope struct {
	Writes  bool
	Folders []string // folder paths, "Bookmarks Bar/Inbox"; empty: anywhere
}

// anyWrites says whether any client may write, so the write tools are offered at all.
func (o Options) anyWrites() bool {
	if o.Writes {
		return true
	}
	for _, c := range o.Clients {
		if c.Writes {
			return true
		}
	}
	return false
}

// scopeFor is the scope of the client that sent req: its own entry in Clients, else the server-wide one.
func (t *tools) scopeFor(req *mcp.CallToolRequest) (Scope, string) {
	name := ""
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil {
			name = p.ClientInfo.Name
		}
	}
	if c, ok := t.opts.Clients[name]; ok {
		return c, name
	}
	return Scope{Writes: t.opts.Writes, Folders: t.opts.Folders}, name
}

// errOutOfScope is a write the client's scope does not allow; the tool answers it as an error the assistant reads.
var errOutOfScope = errors.New("not allowed")

func outOfScope(who, what string) error {
	if who == "" {
		who = "this client"
	}
	return fmt.Errorf("%w: %s may not %s (mcp.folders and mcp.clients in the configuration say what each client may change)", errOutOfScope, who, what)
}

// inside says whether path lies inside one of folders: the folder itself or anything under it.
func inside(path string, folders []string) bool {
	for _, f := range folders {
		if path == f || strings.HasPrefix(path, f+"/") {
			return true
		}
	}
	return false
}

// paths maps every node of t to its path: a folder's own path, a bookmark's folder path plus its title.
func paths(t store.Tree) map[string]string {
	out := map[string]string{}
	var visit func(n *store.Node, path string)
	visit = func(n *store.Node, path string) {
		for _, c := range n.Children {
			p := c.Title
			if path != "" {
				p = path + "/" + c.Title
			}
			out[c.ID] = p
			if c.Kind == store.KindFolder {
				visit(c, p)
			}
		}
	}
	if t.Root != nil {
		visit(t.Root, "")
	}
	return out
}

// allowWrite checks a write against the caller's scope: writes at all, then each node changed (ids) lying strictly
// inside an allowed folder, and the folder written into (to, a path; "" for none) being one or inside one.
// wholeWorkspace marks a write that can touch anything (undo, rollback, put back), which a folder scope refuses.
func (t *tools) allowWrite(ctx context.Context, req *mcp.CallToolRequest, workspace string, ids []string, to string, wholeWorkspace bool) error {
	sc, who := t.scopeFor(req)
	if !sc.Writes {
		return outOfScope(who, "change bookmarks")
	}
	if len(sc.Folders) == 0 {
		return nil
	}
	if wholeWorkspace {
		return outOfScope(who, "undo, roll back or put back: those can change anything, and it may change only "+strings.Join(sc.Folders, ", "))
	}
	if to != "" && !inside(to, sc.Folders) {
		return outOfScope(who, "write into "+to)
	}
	if len(ids) == 0 {
		return nil
	}
	l, err := t.load(ctx, workspace)
	if err != nil {
		return err
	}
	at := paths(l.tree)
	for _, id := range ids {
		p, ok := at[id]
		if !ok {
			continue // the API answers an unknown id
		}
		in := false
		for _, f := range sc.Folders {
			if strings.HasPrefix(p, f+"/") { // strictly inside: an allowed folder itself cannot be moved, renamed or trashed
				in = true
			}
		}
		if !in {
			return outOfScope(who, "change "+p)
		}
	}
	return nil
}
