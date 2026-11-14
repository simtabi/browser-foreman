package linkcheck

import (
	"context"
	"strings"
	"testing"
)

// fakeGitHub answers from a table of paths; anything else is a 404. It records what was asked.
func fakeGitHub(answers map[string]GitHubAnswer, asked *[]string) GitHubAPI {
	return func(_ context.Context, path string) GitHubAnswer {
		*asked = append(*asked, path)
		if a, ok := answers[path]; ok {
			return a
		}
		return GitHubAnswer{Code: 404}
	}
}

func TestGitHubJudgesEachKindOfLink(t *testing.T) {
	answers := map[string]GitHubAnswer{
		"gists/abc123":     {Code: 200},
		"users/alive":      {Code: 200},
		"repos/alive/kept": {Code: 200, Body: []byte(`{"full_name":"alive/kept"}`)},
		"repos/alive/old":  {Code: 200, Body: []byte(`{"full_name":"alive/new-name","archived":true}`)},
		"repos/alive/kept/contents/docs/a%20b.md?ref=main": {Code: 200},
		"orgs/acme":                      {Code: 200},
		"repos/alive/flaky":              {Code: 502},
		"repos/alive/kept/branches/main": {Code: 200},
		"repos/alive/weird":              {Code: 200, Body: []byte(`{"full_name":"x/../../y"}`)},
	}
	cases := []struct{ url, status, note, final string }{
		{"https://gist.github.com/someone/abc123", "ok", "gist exists", ""},
		{"https://gist.github.com/someone/def456", "dead", "gist deleted", ""},
		{"https://github.com/alive", "ok", "account exists", ""},
		{"https://github.com/gone", "dead", "account gone", ""},
		{"https://github.com/orgs/acme/discussions/1", "unverified", "organisation exists", ""},
		{"https://github.com/orgs/vanished/discussions", "dead", "organisation no longer exists", ""},
		{"https://github.com/alive/kept", "ok", "repository exists (GitHub API)", ""},
		{"https://github.com/alive/missing", "dead", "deleted or private; its owner still exists", ""},
		{"https://github.com/gone/repo", "dead", "repository and its owner are gone", ""},
		{"https://github.com/alive/old/issues?q=1", "moved", "archived; renamed to alive/new-name", "https://github.com/alive/new-name/issues?q=1"},
		{"https://github.com/alive/kept/blob/main/docs/a%20b.md", "ok", "repository exists", ""},
		{"https://github.com/alive/kept/blob/main/docs/gone.md", "dead", "this file or branch is gone", ""},
		{"https://github.com/alive/kept/tree/old-branch", "dead", "this file or branch is gone", ""},
		{"https://github.com/alive/flaky", "unverified", "answered 502", ""},
		{"https://github.com/alive/kept/tree/main", "ok", "repository exists", ""},
		{"https://github.com/alive/weird", "unverified", "answered 200", ""}, // a name GitHub would never give: not used
	}
	for _, c := range cases {
		var asked []string
		r, ok, limited := GitHub(context.Background(), fakeGitHub(answers, &asked), c.url)
		if !ok || limited || r.Status != c.status || !strings.Contains(r.Note, c.note) || r.Final != c.final {
			t.Errorf("%s: ok=%v limited=%v %+v (asked %v)", c.url, ok, limited, r, asked)
		}
	}
}

func TestGitHubLeavesOtherLinksAndOddNamesAlone(t *testing.T) {
	for _, u := range []string{
		"https://example.com/a/b", "https://github.com/", "https://github.com/a/..%2f..%2fuser/keys",
		"https://github.com/a%20b/c", "https://github.com/a/b%3Fx", "https://gist.github.com/x/not-hex!",
		"https://github.com/alive/kept/blob/main/../../../user/keys", "https://github.com/alive/kept/tree/./x",
	} {
		var asked []string
		if _, ok, _ := GitHub(context.Background(), fakeGitHub(nil, &asked), u); ok || len(asked) != 0 {
			t.Errorf("%s was judged (asked %v)", u, asked)
		}
	}
	if _, ok, _ := GitHub(context.Background(), nil, "https://github.com/a/b"); ok {
		t.Fatal("judged without an API")
	}
}

func TestGitHubStopsWhenTheBudgetIsSpent(t *testing.T) {
	var asked []string
	spent := fakeGitHub(map[string]GitHubAnswer{"repos/a/b": {Code: 403, Limited: true}}, &asked)
	r, ok, limited := GitHub(context.Background(), spent, "https://github.com/a/b")
	if !ok || !limited || r.Status != StatusUnverified || !strings.Contains(r.Note, "try later") {
		t.Fatalf("%v %v %+v", ok, limited, r)
	}
}

func TestParseInclude(t *testing.T) {
	a := parseInclude([]byte("HTTP/2.0 200 OK\r\nContent-Type: application/json\r\nX-Ratelimit-Remaining: 4999\r\n\r\n{\"full_name\":\"a/b\"}"))
	if a.Code != 200 || a.Limited || string(a.Body) != `{"full_name":"a/b"}` {
		t.Fatalf("%+v", a)
	}
	if a := parseInclude([]byte("HTTP/2.0 403 Forbidden\nX-Ratelimit-Remaining: 0\n\n{}")); a.Code != 403 || !a.Limited {
		t.Fatalf("spent %+v", a)
	}
	if a := parseInclude([]byte("HTTP/2.0 429 Too Many Requests\nRetry-After: 60\n\n")); !a.Limited {
		t.Fatalf("retry-after %+v", a)
	}
	if a := parseInclude([]byte("HTTP/2.0 403 Forbidden\nX-Ratelimit-Remaining: 12\n\n{}")); a.Limited {
		t.Fatal("a plain 403 is not a spent budget")
	}
	if a := parseInclude(nil); a.Code != 0 {
		t.Fatalf("nothing %+v", a)
	}
}
