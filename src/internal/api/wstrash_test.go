package api

import (
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestWorkspaceTrashAndReset(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	// Reset: after edits, back to the first import, as an entry undo takes back.
	if w := send(h, "POST", "/api/v1/workspaces/main/trash", `{"ids":["go"]}`); w.Code != 201 {
		t.Fatalf("trash %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/reset", ""); w.Code != 201 || !strings.Contains(w.Body.String(), "Reset to how it started (change 1") {
		t.Fatalf("reset %d %s", w.Code, w.Body)
	}
	if tr, _ := tree(t, h); len(tr.Trash) != 0 {
		t.Fatal("reset did not go back to the start")
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/undo", ""); w.Code != 201 {
		t.Fatalf("undo %d", w.Code)
	}
	if tr, _ := tree(t, h); len(tr.Trash) != 1 {
		t.Fatal("undo did not bring the state before the reset back")
	}

	// Delete: the only workspace in use cannot go; with another it can, and it leaves the lists.
	if w := send(h, "POST", "/api/v1/workspaces/main/delete", `{"confirm":"main"}`); w.Code != 409 {
		t.Fatalf("only workspace %d", w.Code)
	}
	_ = send(h, "POST", "/api/v1/workspaces", `{"id":"other","name":"Other"}`)
	if w := send(h, "POST", "/api/v1/workspaces/main/delete", `{"confirm":"Main"}`); w.Code != 400 {
		t.Fatalf("wrong confirm %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/delete", `{"confirm":"main"}`); w.Code != 200 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	if b := send(h, "GET", "/api/v1/workspaces", "").Body.String(); strings.Contains(b, `"id":"main"`) || !strings.Contains(b, `"id":"other"`) {
		t.Fatalf("list %s", b)
	}
	if b := send(h, "GET", "/api/v1/workspaces?trash=1", "").Body.String(); !strings.Contains(b, `"id":"main"`) || !strings.Contains(b, `"deleted":"`) {
		t.Fatalf("trash list %s", b)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/trash", `{"ids":["go"]}`); w.Code != 410 {
		t.Fatalf("a change to a deleted workspace: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/other/restore", ""); w.Code != 409 {
		t.Fatalf("restore of one not deleted: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/main/restore", ""); w.Code != 200 {
		t.Fatalf("restore %d", w.Code)
	}
	if h2 := entries(t, h); len(h2) < 4 {
		t.Fatalf("history lost on the way: %d entries", len(h2))
	}

	// Purge: only from the Trash, and only with the id typed.
	if w := send(h, "POST", "/api/v1/workspaces/other/purge", `{"confirm":"other"}`); w.Code != 409 {
		t.Fatalf("purge outside the Trash: %d", w.Code)
	}
	_ = send(h, "POST", "/api/v1/workspaces/other/delete", `{"confirm":"other"}`)
	if w := send(h, "POST", "/api/v1/workspaces/other/purge", `{}`); w.Code != 400 {
		t.Fatalf("purge without confirm: %d", w.Code)
	}
	if w := send(h, "POST", "/api/v1/workspaces/other/purge", `{"confirm":"other"}`); w.Code != 200 {
		t.Fatalf("purge %d %s", w.Code, w.Body)
	}
	if _, err := s.Data.Workspace(t.Context(), "other"); err == nil {
		t.Fatal("purged workspace still stored")
	}

	// A locked workspace cannot be deleted; editors cannot delete at all.
	_ = send(h, "POST", "/api/v1/workspaces", `{"id":"third"}`)
	_ = send(h, "PATCH", "/api/v1/workspaces/third", `{"locked":true}`)
	if w := send(h, "POST", "/api/v1/workspaces/third/delete", `{"confirm":"third"}`); w.Code != 423 {
		t.Fatalf("locked %d", w.Code)
	}
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ed", store.RoleEditor, p)
	_, ed := signIn(t, h, "ed", p)
	if w := ed.do(h, "POST", "/api/v1/workspaces/main/delete", `{"confirm":"main"}`, true); w.Code != 403 {
		t.Fatalf("editor deleted: %d", w.Code)
	}
}
