package linkcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type seFake func(*http.Request) (*http.Response, error)

func (f seFake) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeSE answers like the API: the asked ids that exist (with a close reason for "closed" ones). It records each ask.
func fakeSE(exist map[string]string, asked *[]string, status int) http.RoundTripper {
	return seFake(func(r *http.Request) (*http.Response, error) {
		*asked = append(*asked, r.URL.Query().Get("site")+" "+strings.TrimPrefix(r.URL.Path, "/2.3/questions/"))
		var items []map[string]any
		for _, id := range strings.Split(strings.TrimPrefix(r.URL.Path, "/2.3/questions/"), ";") {
			if reason, ok := exist[r.URL.Query().Get("site")+"/"+id]; ok {
				it := map[string]any{"question_id": json.Number(id)}
				if reason != "" {
					it["closed_reason"] = reason
				}
				items = append(items, it)
			}
		}
		b, _ := json.Marshal(map[string]any{"items": items})
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}, Request: r}, nil
	})
}

func TestStackExchangeSettlesQuestions(t *testing.T) {
	c := checker()
	var asked []string
	c.Client.Transport = fakeSE(map[string]string{"stackoverflow/11": "", "stackoverflow/12": "Duplicate", "serverfault/7": "", "unix/5": ""}, &asked, 200)
	got := c.StackExchange(context.Background(), "https://api.example/2.3", []string{
		"https://stackoverflow.com/questions/11/how-to-x", "https://stackoverflow.com/q/12", "https://stackoverflow.com/questions/13/gone",
		"https://serverfault.com/questions/7/nginx", "https://unix.stackexchange.com/questions/5/sh",
		"https://stackoverflow.com/a/99", "https://stackoverflow.com/users/1/x", "https://example.com/questions/1",
	})
	want := map[string]string{
		"https://stackoverflow.com/questions/11/how-to-x": "ok", "https://stackoverflow.com/q/12": "ok",
		"https://stackoverflow.com/questions/13/gone": "dead", "https://serverfault.com/questions/7/nginx": "ok",
		"https://unix.stackexchange.com/questions/5/sh": "ok",
	}
	if len(got) != len(want) {
		t.Fatalf("%d results: %+v", len(got), got)
	}
	for u, st := range want {
		if got[u].Status != st {
			t.Errorf("%s: %+v", u, got[u])
		}
	}
	if !strings.Contains(got["https://stackoverflow.com/q/12"].Note, "closed: Duplicate") {
		t.Errorf("close reason: %+v", got["https://stackoverflow.com/q/12"])
	}
	if strings.Join(asked, " | ") != "serverfault 7 | stackoverflow 11;12;13 | unix 5" {
		t.Fatalf("asked %v", asked)
	}
}

func TestStackExchangeBatchesAHundredAndStopsOnError(t *testing.T) {
	c := checker()
	var asked []string
	c.Client.Transport = fakeSE(nil, &asked, 200)
	var urls []string
	for i := range 150 {
		urls = append(urls, fmt.Sprintf("https://stackoverflow.com/questions/%d/x", 1000+i))
	}
	if got := c.StackExchange(context.Background(), "https://api.example/2.3", urls); len(got) != 150 || len(asked) != 2 {
		t.Fatalf("%d results in %d asks", len(got), len(asked))
	}
	asked = nil
	c.Client.Transport = fakeSE(nil, &asked, 400) // refused (a spent quota, say): nothing is concluded
	if got := c.StackExchange(context.Background(), "https://api.example/2.3", urls); len(got) != 0 || len(asked) != 1 {
		t.Fatalf("%d results after a refusal, %d asks", len(got), len(asked))
	}
}

func TestSESite(t *testing.T) {
	for host, want := range map[string]string{"stackoverflow.com": "stackoverflow", "www.superuser.com": "superuser",
		"askubuntu.com": "askubuntu", "unix.stackexchange.com": "unix", "a.b.stackexchange.com": "", "example.com": ""} {
		if got := seSite(host); got != want {
			t.Errorf("%s: %q, want %q", host, got, want)
		}
	}
}
