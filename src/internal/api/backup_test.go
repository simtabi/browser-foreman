package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func TestBackupDownloadAndRestore(t *testing.T) {
	s := seeded(t)
	h := s.Handler()
	send(h, "PATCH", "/api/v1/workspaces/main/nodes/go", `{"title":"Golang"}`)
	w := send(h, "GET", "/api/v1/backup", "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/gzip" || !strings.Contains(w.Header().Get("Content-Disposition"), "browser-foreman-backup-") {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := io.ReadAll(zr)
	if strings.Contains(string(plain), apiTok) && strings.Contains(string(plain), "api_token") {
		t.Fatal("the API token reached a backup")
	}
	// Restore the gzipped file into the same server: the backup's workspace comes back beside the live one.
	w = send(h, "POST", "/api/v1/restore", w.Body.String())
	var res store.Restored
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Workspaces["main"] != "main-restored-1" || res.Entries != 2 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// Plain JSON works too.
	if w := send(h, "POST", "/api/v1/restore", string(plain)); w.Code != 200 || !strings.Contains(w.Body.String(), "main-restored-2") {
		t.Fatalf("plain %d %s", w.Code, w.Body)
	}
	if w := send(h, "POST", "/api/v1/restore", `{"format":"nope","version":1}`); w.Code != 400 {
		t.Fatalf("not a backup %d", w.Code)
	}
	if w := send(h, "GET", "/api/v1/settings", ""); w.Code != 200 || strings.Contains(w.Body.String(), `"`+apiTok+`"`) || !strings.Contains(w.Body.String(), `"api_token_set":true`) {
		t.Fatalf("settings %d %s", w.Code, w.Body)
	}
}

func TestBackupNeedsTheOwner(t *testing.T) {
	s := seeded(t)
	s.Cfg.Server.Mode = "accounts"
	p := pw(t)
	addUser(t, s, "ed", store.RoleEditor, p)
	h := s.Handler()
	_, ed := signIn(t, h, "ed", p)
	for _, c := range []struct{ m, p string }{{"GET", "/api/v1/backup"}, {"POST", "/api/v1/restore"}, {"GET", "/api/v1/settings"}} {
		if w := ed.do(h, c.m, c.p, "{}", true); w.Code != 403 {
			t.Errorf("%s %s as an editor: %d", c.m, c.p, w.Code)
		}
	}
}
