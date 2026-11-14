package linkcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHubAnswer is one GitHub REST reply. Code 0 means GitHub could not be asked (no network, no gh); Limited means
// the API budget is spent, so nothing more should be asked this run.
type GitHubAnswer struct {
	Code    int
	Body    []byte
	Limited bool
}

// GitHubAPI makes one GET to GitHub's REST API, for a path such as "repos/owner/name".
type GitHubAPI func(ctx context.Context, path string) GitHubAnswer

// GhCLI asks through the gh command, signed in as its user: the program never sees a token. A signed-in API tells a
// deleted repository from a private one, which a plain web request cannot (both answer 404). nil when gh is not
// installed.
func GhCLI() GitHubAPI {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return nil
	}
	return func(ctx context.Context, path string) GitHubAnswer {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		// One argument per word and no shell, so nothing in a bookmark's address can become an option or a command;
		// paths are built only from checked names (see githubPath).
		out, _ := exec.CommandContext(ctx, gh, "api", "--method", "GET", "--include", path).Output()
		return parseInclude(out)
	}
}

// parseInclude reads `gh api --include` output: the status line, the headers, a blank line, the body.
func parseInclude(out []byte) GitHubAnswer {
	head, body, ok := bytes.Cut(out, []byte("\r\n\r\n"))
	if !ok {
		head, body, _ = bytes.Cut(out, []byte("\n\n"))
	}
	lines := strings.Split(strings.ReplaceAll(string(head), "\r\n", "\n"), "\n")
	f := strings.Fields(lines[0])
	if len(f) < 2 {
		return GitHubAnswer{}
	}
	code, err := strconv.Atoi(f[1])
	if err != nil {
		return GitHubAnswer{}
	}
	a := GitHubAnswer{Code: code, Body: body}
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, ":")
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.TrimSpace(v)
		if (code == 403 || code == 429) && (k == "retry-after" || k == "x-ratelimit-remaining" && v == "0") {
			a.Limited = true
		}
	}
	return a
}

var (
	ghName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`) // an account or repository name ("." and ".." are refused earlier)
	ghGist = regexp.MustCompile(`^[0-9a-fA-F]{1,64}$`)
)

// GitHub judges a github.com or gist.github.com link through the API: a gist, an account, an organisation, a
// repository (renamed ones become "moved"), or a file or branch inside one. ok is false for any other link, and for
// one whose parts are not plain GitHub names. limited says the API budget is spent, or GitHub could not be asked at
// all: stop asking, and keep what the run found.
func GitHub(ctx context.Context, api GitHubAPI, raw string) (r Result, ok, limited bool) {
	u, err := url.Parse(raw)
	if err != nil || api == nil {
		return Result{}, false, false
	}
	host := strings.ToLower(u.Hostname())
	var parts []string
	for _, p := range strings.Split(u.Path, "/") {
		if p == "." || p == ".." {
			// Would climb to another API path, as the signed-in user ("%2F" decodes to "/", so this also covers
			// /a/..%2F..%2Fuser/keys).
			return Result{}, false, false
		}
		if p != "" {
			parts = append(parts, p)
		}
	}
	ask := func(path string) GitHubAnswer {
		a := api(ctx, path)
		// No answer at all (gh signed out, no network) says nothing about the link, and the next will fare no better.
		limited = limited || a.Limited || a.Code == 0
		return a
	}
	unreachable := Result{Status: StatusUnverified, Note: "GitHub could not be asked; try later"}
	switch {
	case host == "gist.github.com" && len(parts) >= 1 && ghGist.MatchString(parts[len(parts)-1]):
		a := ask("gists/" + parts[len(parts)-1])
		switch a.Code {
		case 200:
			return Result{Status: StatusOK, Code: 200, Note: "gist exists (GitHub API)"}, true, limited
		case 404:
			return Result{Status: StatusDead, Kind: "page", Code: 404, Note: "gist deleted or made secret (GitHub API)"}, true, limited
		}
		return apiTrouble(a, unreachable), true, limited
	case host != "github.com" && host != "www.github.com" || len(parts) == 0:
		return Result{}, false, false
	case parts[0] == "orgs" && len(parts) >= 2 && ghName.MatchString(parts[1]):
		a := ask("orgs/" + parts[1])
		if a.Code == 404 {
			return Result{Status: StatusDead, Kind: "host", Code: 404, Note: "organisation no longer exists (GitHub API)"}, true, limited
		}
		// Org pages such as discussions have no REST lookup: the organisation existing is as far as the API goes.
		return Result{Status: StatusUnverified, Code: a.Code, Note: "organisation exists; this page needs a browser to confirm"}, true, limited
	case len(parts) == 1 && ghName.MatchString(parts[0]):
		a := ask("users/" + parts[0])
		switch a.Code {
		case 200:
			return Result{Status: StatusOK, Code: 200, Note: "account exists (GitHub API)"}, true, limited
		case 404:
			return Result{Status: StatusDead, Kind: "host", Code: 404, Note: "account gone (GitHub API)"}, true, limited
		}
		return apiTrouble(a, unreachable), true, limited
	case len(parts) < 2 || !ghName.MatchString(parts[0]) || !ghName.MatchString(parts[1]):
		return Result{}, false, false
	}
	owner, repo := parts[0], parts[1]
	a := ask("repos/" + owner + "/" + repo)
	if a.Code == 404 {
		why := "repository and its owner are gone"
		if ask("users/"+owner).Code == 200 {
			why = "repository deleted or private; its owner still exists"
		}
		return Result{Status: StatusDead, Kind: "page", Code: 404, Note: why + " (GitHub API)"}, true, limited
	}
	var d struct {
		FullName string `json:"full_name"`
		Archived bool   `json:"archived"`
	}
	if a.Code != 200 || json.Unmarshal(a.Body, &d) != nil || !ghFull.MatchString(d.FullName) {
		return apiTrouble(a, unreachable), true, limited
	}
	if len(parts) >= 4 && (parts[2] == "blob" || parts[2] == "tree") {
		ref, path := parts[3], strings.Join(parts[4:], "/")
		var p string
		if path != "" {
			p = "repos/" + d.FullName + "/contents/" + escapePath(path) + "?ref=" + url.QueryEscape(ref)
		} else {
			p = "repos/" + d.FullName + "/branches/" + url.PathEscape(ref)
		}
		if ask(p).Code == 404 {
			return Result{Status: StatusDead, Kind: "page", Code: 404, Note: "repository exists, but this file or branch is gone (" + d.FullName + ", GitHub API)"}, true, limited
		}
	}
	note := "repository exists"
	if d.Archived {
		note = "repository exists but is archived"
	}
	if !strings.EqualFold(d.FullName, owner+"/"+repo) {
		final := *u
		final.Path = "/" + d.FullName + strings.TrimPrefix(u.Path, "/"+owner+"/"+repo)
		return Result{Status: StatusMoved, Code: 301, Final: final.String(), Note: note + "; renamed to " + d.FullName + " (GitHub API)"}, true, limited
	}
	return Result{Status: StatusOK, Code: 200, Note: note + " (GitHub API)"}, true, limited
}

var ghFull = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

func apiTrouble(a GitHubAnswer, unreachable Result) Result {
	if a.Code == 0 || a.Limited {
		return unreachable
	}
	return Result{Status: StatusUnverified, Code: a.Code, Note: "GitHub API answered " + strconv.Itoa(a.Code)}
}

func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}
