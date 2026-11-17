package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Two copies of one browser on one computer (your Chrome, and another Chrome) both call themselves "Google Chrome".
// Only one is connected at a time: approving the second pins it and revokes the first, which is told so (401).
func TestTwoCopiesOfABrowserAreNeverBothConnected(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	connect := func(instance string) string {
		t.Helper()
		w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.7","instance":"`+instance+`"}`, "")
		var q struct{ ID, Poll string }
		_ = json.Unmarshal(w.Body.Bytes(), &q)
		if w.Code != 201 {
			t.Fatalf("ask %d %s", w.Code, w.Body)
		}
		listed := send(h, "GET", "/api/v1/extension/requests", "").Body.String()
		if !strings.Contains(listed, `"instance":"`+instance[:4]+`"`) {
			t.Fatalf("the request does not say which copy asks: %s", listed)
		}
		if w := send(h, "POST", "/api/v1/extension/requests/"+q.ID+"/approve", ""); w.Code != 200 {
			t.Fatalf("approve %d %s", w.Code, w.Body)
		}
		w = fromExtension(h, "POST", "/api/v1/extension/requests/"+q.ID+"/poll", `{"poll":"`+q.Poll+`"}`, "")
		var a struct{ Token string }
		_ = json.Unmarshal(w.Body.Bytes(), &a)
		return a.Token
	}
	first := connect("aaaa1111aaaa1111")
	if w := fromExtension(h, "GET", "/api/v1/workspaces", "", first); w.Code != 200 {
		t.Fatalf("the first copy works: %d", w.Code)
	}
	// The second copy asks on purpose (Connect pressed in it): the owner is told approving it replaces the first.
	w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.7","instance":"bbbb2222bbbb2222","replace":true}`, "")
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if listed := send(h, "GET", "/api/v1/extension/requests", "").Body.String(); !strings.Contains(listed, `"replaces":"aaaa"`) {
		t.Fatalf("the owner is not told this replaces copy aaaa: %s", listed)
	}
	var q struct{ ID, Poll string }
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if w := send(h, "POST", "/api/v1/extension/requests/"+q.ID+"/approve", ""); w.Code != 200 {
		t.Fatalf("approve the second %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "GET", "/api/v1/workspaces", "", first); w.Code != 401 {
		t.Fatalf("the first copy still works after the second was pinned: %d", w.Code)
	}
	status := send(h, "GET", "/api/v1/extension/status", "").Body.String()
	if strings.Count(status, `"name":"extension:Google Chrome"`) != 1 || !strings.Contains(status, `"instance":"bbbb"`) {
		t.Fatalf("one connected copy, the second: %s", status)
	}
	p, _ := store.LoadPrefs(context.Background(), s.Data)
	if pin := p.ExtensionPins["Google Chrome"]; pin.Instance != "bbbb2222bbbb2222" || pin.Version != "0.1.7" {
		t.Fatalf("the pin was not kept: %+v", pin)
	}
}

func TestPairingByCodePinsToo(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	code := func() string {
		var c struct{ Code string }
		_ = json.Unmarshal(send(h, "POST", "/api/v1/extension/pairings", "").Body.Bytes(), &c)
		return c.Code
	}
	pair := func(instance string) string {
		w := fromExtension(h, "POST", "/api/v1/extension/pair", `{"code":"`+code()+`","browser":"Brave Browser","instance":"`+instance+`","version":"0.1.7"}`, "")
		var a struct{ Token string }
		_ = json.Unmarshal(w.Body.Bytes(), &a)
		if w.Code != 201 {
			t.Fatalf("pair %d %s", w.Code, w.Body)
		}
		return a.Token
	}
	one := pair("1111aaaa")
	pair("2222bbbb")
	if w := fromExtension(h, "GET", "/api/v1/workspaces", "", one); w.Code != 401 {
		t.Fatalf("an earlier copy paired by code still works: %d", w.Code)
	}
}

// A second copy asking by itself while another copy is connected is refused, so it stops asking; pressing Connect in
// it (replace) may ask, and approving that moves the browser.
func TestASecondCopyMayAskOnlyOnPurpose(t *testing.T) {
	s := newServer(t, "tok")
	h := s.Handler()
	w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.8","instance":"aaaa1111"}`, "")
	var q struct{ ID string }
	_ = json.Unmarshal(w.Body.Bytes(), &q)
	if w := send(h, "POST", "/api/v1/extension/requests/"+q.ID+"/approve", ""); w.Code != 200 {
		t.Fatalf("approve %d", w.Code)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.8","instance":"bbbb2222"}`, ""); w.Code != 409 || !strings.Contains(w.Body.String(), "aaaa") {
		t.Fatalf("a second copy asking by itself: %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.8","instance":"bbbb2222","replace":true}`, ""); w.Code != 201 {
		t.Fatalf("Connect pressed in the second copy: %d %s", w.Code, w.Body)
	}
	if w := fromExtension(h, "POST", "/api/v1/extension/requests", `{"browser":"Google Chrome","version":"0.1.8","instance":"aaaa1111"}`, ""); w.Code != 201 {
		t.Fatalf("the connected copy itself may ask again: %d %s", w.Code, w.Body)
	}
}
