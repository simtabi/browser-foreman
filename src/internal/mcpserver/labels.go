package mcpserver

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// host is an address's host without "www.", or "" for one that is not a web address.
func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// ---- label_bookmarks ----

type labelIn struct {
	Workspace  string   `json:"workspace"`
	IDs        []string `json:"ids" jsonschema:"the bookmarks, 1-500 ids"`
	AddTags    []string `json:"add_tags,omitempty" jsonschema:"tags to add; each up to 40 characters, no commas, at most 20 per bookmark"`
	RemoveTags []string `json:"remove_tags,omitempty" jsonschema:"tags to take off, ignoring case"`
	Rating     *int     `json:"rating,omitempty" jsonschema:"1 to 5 stars; 0 clears"`
	Note       *string  `json:"note,omitempty" jsonschema:"replaces the note; empty clears it"`
}

// labelOps builds one update op per bookmark from its current tags: added ones join, removed ones go (ignoring case),
// and an op says only what changes. Tags are sent as a list even when empty, which is how a bookmark's last tag goes.
func labelOps(current map[string][]string, in labelIn) []map[string]any {
	var ops []map[string]any
	for _, id := range in.IDs {
		op := map[string]any{"op": "update", "id": id}
		if len(in.AddTags) > 0 || len(in.RemoveTags) > 0 {
			drop := map[string]bool{}
			for _, t := range in.RemoveTags {
				drop[strings.ToLower(strings.TrimSpace(t))] = true
			}
			tags := []string{}
			for _, t := range append(append([]string{}, current[id]...), in.AddTags...) {
				if !drop[strings.ToLower(strings.TrimSpace(t))] {
					tags = append(tags, t)
				}
			}
			op["tags"] = tags // the server trims, removes repeats ignoring case, and sorts
		}
		if in.Rating != nil {
			op["rating"] = *in.Rating
		}
		if in.Note != nil {
			op["note"] = *in.Note
		}
		ops = append(ops, op)
	}
	return ops
}

func (t *tools) label(ctx context.Context, req *mcp.CallToolRequest, in labelIn) (*mcp.CallToolResult, Untrusted, error) {
	if len(in.IDs) == 0 || len(in.IDs) > 500 {
		return nil, Untrusted{}, errors.New("give 1 to 500 bookmark ids")
	}
	if len(in.AddTags) == 0 && len(in.RemoveTags) == 0 && in.Rating == nil && in.Note == nil {
		return nil, Untrusted{}, errors.New("say what to change: add_tags, remove_tags, rating or note")
	}
	if err := t.allowWrite(ctx, req, in.Workspace, in.IDs, "", false); err != nil {
		return nil, Untrusted{}, err
	}
	l, err := t.load(ctx, in.Workspace)
	if err != nil {
		return nil, Untrusted{}, err
	}
	current := map[string][]string{}
	for _, id := range in.IDs {
		n, _, err := find(l.tree, id)
		if err != nil {
			return nil, Untrusted{}, err
		}
		if n.Kind != "bookmark" {
			return nil, Untrusted{}, errors.New(n.Title + " is a folder: only bookmarks carry labels")
		}
		current[id] = n.Tags
	}
	var out changed
	err = t.api.do(ctx, "POST", ws(in.Workspace)+"/changes", map[string]any{"ops": labelOps(current, in)}, &out)
	return nil, wrap(out), err
}

// ---- add_bookmark ----

type addIn struct {
	Workspace string   `json:"workspace"`
	URL       string   `json:"url" jsonschema:"a web address (http or https)"`
	Title     string   `json:"title,omitempty" jsonschema:"the address when left out"`
	Folder    string   `json:"folder" jsonschema:"where it goes, a folder path like \"Bookmarks Bar/Reading\"; made when missing"`
	Note      string   `json:"note,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Rating    int      `json:"rating,omitempty" jsonschema:"1 to 5 stars"`
}

func (t *tools) addBookmark(ctx context.Context, req *mcp.CallToolRequest, in addIn) (*mcp.CallToolResult, Untrusted, error) {
	if host(in.URL) == "" {
		return nil, Untrusted{}, errors.New("url must be a web address (http or https)")
	}
	if strings.Trim(in.Folder, "/") == "" {
		return nil, Untrusted{}, errors.New("name the folder it goes in, such as \"Bookmarks Bar/Reading\"")
	}
	folder := strings.Trim(in.Folder, "/")
	if err := t.allowWrite(ctx, req, in.Workspace, nil, folder, false); err != nil {
		return nil, Untrusted{}, err
	}
	f, err := t.ensureFolder(ctx, in.Workspace, folder, true)
	if err != nil {
		return nil, Untrusted{}, err
	}
	title := in.Title
	if strings.TrimSpace(title) == "" {
		title = in.URL
	}
	var made changed
	body := map[string]any{"parent": f.ID, "kind": "bookmark", "title": title, "url": in.URL}
	if in.Note != "" {
		body["note"] = in.Note
	}
	if err := t.api.do(ctx, "POST", ws(in.Workspace)+"/nodes", body, &made); err != nil {
		return nil, Untrusted{}, err
	}
	if len(in.Tags) == 0 && in.Rating == 0 {
		return nil, wrap(made), nil
	}
	// Labels are a second entry: creating takes no tags or rating. The create event names the new bookmark's id.
	id := ""
	if len(made.Events) > 0 {
		if e, ok := made.Events[0].(map[string]any); ok {
			if d, ok := e["data"].(map[string]any); ok {
				id, _ = d["id"].(string)
			}
		}
	}
	if id == "" {
		return nil, wrap(made), errors.New("the bookmark was added, but its id did not come back, so its tags and rating were not set")
	}
	op := map[string]any{"op": "update", "id": id}
	if len(in.Tags) > 0 {
		op["tags"] = in.Tags
	}
	if in.Rating > 0 {
		op["rating"] = in.Rating
	}
	var out changed
	err = t.api.do(ctx, "POST", ws(in.Workspace)+"/changes", map[string]any{"ops": []map[string]any{op}}, &out)
	return nil, wrap(out), err
}
