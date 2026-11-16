package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// fakeServer answers the request list and records approvals.
func fakeServer(requests string, approved *[]string) func(method, path string, out any) (int, error) {
	return func(method, path string, out any) (int, error) {
		if method == "GET" && path == "extension/requests" {
			return 200, json.Unmarshal([]byte(requests), out)
		}
		if method == "POST" && strings.HasSuffix(path, "/approve") {
			*approved = append(*approved, strings.TrimSuffix(strings.TrimPrefix(path, "extension/requests/"), "/approve"))
			return 200, json.Unmarshal([]byte(`{"state":"approved"}`), out)
		}
		return 404, nil
	}
}

func TestApproveOnlyTheRequestWhoseLettersAreTyped(t *testing.T) {
	two := `{"requests":[{"id":"rq_a","browser":"Google Chrome","match":"KXQT"},{"id":"rq_b","browser":"Brave Browser","match":"MNPR"}]}`
	var approved []string
	var out bytes.Buffer
	if err := approveRequest(fakeServer(two, &approved), nil, &out, false, "mnpr"); err != nil || len(approved) != 1 || approved[0] != "rq_b" {
		t.Fatalf("--match: %v %v", err, approved)
	}
	approved = nil
	if err := approveRequest(fakeServer(two, &approved), strings.NewReader("KXQT\n"), &out, true, ""); err != nil || len(approved) != 1 || approved[0] != "rq_a" {
		t.Fatalf("typed: %v %v", err, approved)
	}
	approved = nil
	if err := approveRequest(fakeServer(two, &approved), strings.NewReader("ABCD\n"), &out, true, ""); err == nil || len(approved) != 0 {
		t.Fatalf("wrong letters approved something: %v %v", err, approved)
	}
	if err := approveRequest(fakeServer(two, &approved), nil, &out, false, ""); err == nil || !strings.Contains(err.Error(), "--match") || len(approved) != 0 {
		t.Fatalf("no letters and no terminal: %v %v", err, approved)
	}
	if err := approveRequest(fakeServer(`{"requests":[]}`, &approved), nil, &out, false, "KXQT"); err == nil || !strings.Contains(err.Error(), "press Connect") {
		t.Fatalf("nothing waiting: %v", err)
	}
	if !strings.Contains(out.String(), "Google Chrome's extension") {
		t.Fatalf("the waiting requests are not listed: %q", out.String())
	}
}
