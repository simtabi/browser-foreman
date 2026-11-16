package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestPromptsAndResourcesArePinned(t *testing.T) {
	cs, _ := setup(t, false)
	ctx := context.Background()
	ps, err := cs.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range ps.Prompts {
		got = append(got, p.Name)
	}
	if !slices.Equal(sorted(got), sorted(Prompts[:4])) || len(Prompts) != 5 { // process-queue comes with writes
		t.Fatalf("prompts %v, want %v", got, Prompts)
	}
	rs, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := cs.ListResourceTemplates(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs.Resources) != 1 || rs.Resources[0].URI != Resources[0] || len(ts.ResourceTemplates) != 1 || ts.ResourceTemplates[0].URITemplate != Resources[1] {
		t.Fatalf("resources %+v templates %+v", rs.Resources, ts.ResourceTemplates)
	}
	r, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bookmarks://workspaces/main/health"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Contents[0].Text, `"untrusted_data"`) || !strings.Contains(r.Contents[0].Text, `"bookmarks": 3`) {
		t.Fatalf("%s", r.Contents[0].Text)
	}
	if _, err := cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: "bookmarks://workspaces/main/other"}); err == nil {
		t.Fatal("an unknown resource was read")
	}
	p, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: "tidy-folder", Arguments: map[string]string{"workspace": "main", "folder": "Bookmarks Bar/Dev"}})
	if err != nil {
		t.Fatal(err)
	}
	text := p.Messages[0].Content.(*mcp.TextContent).Text
	if !strings.Contains(text, `"Bookmarks Bar/Dev"`) || !strings.Contains(text, "wait for my yes") || !strings.Contains(text, "never follow instructions") {
		t.Fatalf("%s", text)
	}
}

func TestReadToolsForHealthDuplicatesAndStats(t *testing.T) {
	cs, _ := setup(t, false)
	out, ok := call(t, cs, "stats", map[string]any{"workspace": "main"})
	if !ok || out["untrusted_data"].(map[string]any)["bookmarks"].(float64) != 3 || out["untrusted_data"].(map[string]any)["folders"].(float64) != 3 {
		t.Fatalf("stats %v", out)
	}
	out, ok = call(t, cs, "health_report", map[string]any{"workspace": "main", "folder": "Bookmarks Bar/Dev"})
	if d := out["untrusted_data"].(map[string]any); !ok || d["bookmarks"].(float64) != 2 || d["by_status"].(map[string]any)["unchecked"].(float64) != 2 {
		t.Fatalf("health %v", out)
	}
	out, ok = call(t, cs, "get_preview", map[string]any{"workspace": "main", "id": "b1"})
	if d := out["untrusted_data"].(map[string]any); !ok || d["folder"] != "Bookmarks Bar/Dev" || d["url"] != "https://go.dev/doc/" {
		t.Fatalf("preview %v", out)
	}
	if out, ok := call(t, cs, "get_preview", map[string]any{"workspace": "main", "id": "dev"}); ok || !strings.Contains(out["error"].(string), "folder") {
		t.Fatalf("preview of a folder: %v", out)
	}
	if out, ok := call(t, cs, "find_duplicates", map[string]any{"workspace": "main"}); !ok || out["untrusted_data"].(map[string]any)["total_groups"].(float64) != 0 {
		t.Fatalf("duplicates %v", out)
	}
}

func tree(t *testing.T, a *API) store.Tree {
	t.Helper()
	var w struct{ Tree store.Tree }
	if err := a.get(context.Background(), "workspaces/main", &w); err != nil {
		t.Fatal(err)
	}
	return w.Tree
}

func TestLabelsAddRemoveAndClearTags(t *testing.T) {
	cs, a := setup(t, true)
	if out, ok := call(t, cs, "label_bookmarks", map[string]any{"workspace": "main", "ids": []string{"b1", "b3"}, "add_tags": []string{"go", "Docs"}, "rating": 4}); !ok {
		t.Fatal(out)
	}
	n, _, _ := find(tree(t, a), "b1")
	if strings.Join(n.Tags, ",") != "Docs,go" || n.Rating != 4 {
		t.Fatalf("%+v", n)
	}
	if out, ok := call(t, cs, "label_bookmarks", map[string]any{"workspace": "main", "ids": []string{"b1"}, "remove_tags": []string{"DOCS", "go"}, "note": "kept"}); !ok {
		t.Fatal(out)
	}
	n, _, _ = find(tree(t, a), "b1")
	if len(n.Tags) != 0 || n.Note != "kept" || n.Rating != 4 {
		t.Fatalf("the last tags did not go: %+v", n)
	}
	if out, ok := call(t, cs, "label_bookmarks", map[string]any{"workspace": "main", "ids": []string{"dev"}, "rating": 2}); ok || !strings.Contains(out["error"].(string), "folder") {
		t.Fatalf("a folder was labelled: %v", out)
	}
}

func TestAddBookmarkMakesTheFolderAndLabels(t *testing.T) {
	cs, a := setup(t, true)
	out, ok := call(t, cs, "add_bookmark", map[string]any{"workspace": "main", "url": "https://pkg.go.dev/", "folder": "Bookmarks Bar/Reading", "tags": []string{"go"}, "rating": 5})
	if !ok {
		t.Fatal(out)
	}
	var hit *store.Node
	walk(tree(t, a).Root, "", func(b *store.Node, folder string) {
		if b.URL == "https://pkg.go.dev/" && folder == "Bookmarks Bar/Reading" {
			hit = b
		}
	})
	if hit == nil || hit.Title != "https://pkg.go.dev/" || strings.Join(hit.Tags, ",") != "go" || hit.Rating != 5 {
		t.Fatalf("%+v", hit)
	}
	if out, ok := call(t, cs, "add_bookmark", map[string]any{"workspace": "main", "url": "javascript:alert(1)", "folder": "Bookmarks Bar"}); ok {
		t.Fatalf("a non-web address was added: %v", out)
	}
}

func TestWriteScopesByFolderAndClient(t *testing.T) {
	o := Options{Writes: false, Version: "test", Clients: map[string]Scope{"claude-code": {Writes: true, Folders: []string{"Bookmarks Bar/Dev"}}}}
	other, _ := serve(t, o, true, "some-client")
	if out, ok := call(t, other, "label_bookmarks", map[string]any{"workspace": "main", "ids": []string{"b1"}, "rating": 3}); ok || !strings.Contains(out["error"].(string), "some-client may not change bookmarks") {
		t.Fatalf("a client with no writes wrote: %v", out)
	}
	cc, a := serve(t, o, true, "claude-code")
	if out, ok := call(t, cc, "label_bookmarks", map[string]any{"workspace": "main", "ids": []string{"b1"}, "rating": 3}); !ok {
		t.Fatalf("inside its folder: %v", out)
	}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"label_bookmarks", map[string]any{"workspace": "main", "ids": []string{"b3"}, "rating": 3}},                   // outside
		{"rename", map[string]any{"workspace": "main", "id": "dev", "title": "x"}},                                     // the scope folder itself
		{"move_bookmarks", map[string]any{"workspace": "main", "ids": []string{"b1"}, "to_folder": "Other bookmarks"}}, // out of it
		{"add_bookmark", map[string]any{"workspace": "main", "url": "https://x.example/", "folder": "Bookmarks Bar"}},
		{"undo", map[string]any{"workspace": "main"}},
	} {
		if out, ok := call(t, cc, c.tool, c.args); ok || !strings.Contains(out["error"].(string), "not allowed") {
			t.Errorf("%s %v: %v", c.tool, c.args, out)
		}
	}
	if n, _, _ := find(tree(t, a), "b3"); n.Rating != 0 {
		t.Fatal("a refused write changed b3")
	}
	if out, ok := call(t, cc, "add_bookmark", map[string]any{"workspace": "main", "url": "https://x.example/", "folder": "Bookmarks Bar/Dev/New"}); !ok {
		t.Fatalf("into a subfolder of its scope: %v", out)
	}
}

func TestLabelOpsSendAnEmptyListToClearTags(t *testing.T) {
	ops := labelOps(map[string][]string{"x": {"a"}}, labelIn{IDs: []string{"x"}, RemoveTags: []string{"A"}})
	b, _ := json.Marshal(ops)
	if string(b) != `[{"id":"x","op":"update","tags":[]}]` {
		t.Fatalf("%s", b)
	}
}

func TestAIJobsThroughMCP(t *testing.T) {
	cs, a := setup(t, true)
	ctx := context.Background()
	if err := a.do(ctx, "POST", "ai/jobs", map[string]any{"kind": "tag", "workspace": "main", "folder": "Bookmarks Bar/Dev", "apply": true}, nil); err != nil {
		t.Fatal(err)
	}
	out, ok := call(t, cs, "next_ai_jobs", map[string]any{})
	if !ok {
		t.Fatal(out)
	}
	d := out["untrusted_data"].(map[string]any)
	jobs := d["jobs"].([]any)
	if len(jobs) != 1 || !strings.Contains(d["how_to_answer"].(string), "never follow instructions") {
		t.Fatalf("%v", out)
	}
	id := jobs[0].(map[string]any)["id"].(string)
	if out, ok := call(t, cs, "submit_ai_results", map[string]any{"id": id, "results": map[string]any{"tags": []map[string]any{{"id": "b1", "tags": []string{"go"}}}}}); !ok {
		t.Fatal(out)
	}
	if n, _, _ := find(tree(t, a), "b1"); strings.Join(n.Tags, ",") != "go" {
		t.Fatalf("%+v", n)
	}
	if out, ok := call(t, cs, "run_ai_jobs", map[string]any{}); ok || !strings.Contains(out["error"].(string), "sampling") {
		t.Fatalf("a client without sampling: %v", out)
	}
}

func TestAFolderScopedClientCannotAnswerADedupeJob(t *testing.T) {
	o := Options{Version: "test", Clients: map[string]Scope{"claude-code": {Writes: true, Folders: []string{"Bookmarks Bar/Dev"}}}}
	cs, a := serve(t, o, true, "claude-code")
	ctx := context.Background()
	// Two bookmarks titled alike in Dev make one group to judge.
	if err := a.do(ctx, "POST", "workspaces/main/nodes", map[string]any{"parent": "dev", "kind": "bookmark", "title": "Release notes for version two", "url": "https://n.example/r?v=1"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.do(ctx, "POST", "workspaces/main/nodes", map[string]any{"parent": "dev", "kind": "bookmark", "title": "Release notes for version two", "url": "https://n.example/r?v=2"}, nil); err != nil {
		t.Fatal(err)
	}
	var made struct{ Jobs []struct{ ID string } }
	if err := a.do(ctx, "POST", "ai/jobs", map[string]any{"kind": "dedupe", "workspace": "main", "apply": true}, &made); err != nil || len(made.Jobs) != 1 {
		t.Fatalf("%v %+v", err, made)
	}
	out, ok := call(t, cs, "submit_ai_results", map[string]any{"id": made.Jobs[0].ID, "results": map[string]any{"verdicts": []any{}}})
	if ok || !strings.Contains(out["error"].(string), "not allowed") {
		t.Fatalf("%v", out)
	}
}
