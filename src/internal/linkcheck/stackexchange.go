package linkcheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// StackExchangeAPI is the public Stack Exchange API. Stack Overflow and its sister sites answer scripts with 403 and a
// browser with a bot check, so their question links are asked about here instead, a hundred to a request.
const StackExchangeAPI = "https://api.stackexchange.com/2.3"

var seQuestion = regexp.MustCompile(`^/(?:questions|q)/(\d{1,12})(?:/|$)`)

// seSite is the API's name for a Stack Exchange site ("" for any other host).
func seSite(host string) string {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	switch host {
	case "stackoverflow.com", "serverfault.com", "superuser.com", "askubuntu.com", "stackapps.com", "mathoverflow.net":
		return strings.TrimSuffix(strings.TrimSuffix(host, ".com"), ".net")
	}
	if name, ok := strings.CutSuffix(host, ".stackexchange.com"); ok && name != "" && !strings.Contains(name, ".") {
		return name
	}
	return ""
}

// StackExchange settles question links on Stack Exchange sites through the API at endpoint (StackExchangeAPI
// outside tests): a question the API returns exists (closed ones say so); one it does not was deleted. Other links,
// and links whose batch could not be asked, are left out of the answer.
func (c *Checker) StackExchange(ctx context.Context, endpoint string, urls []string) map[string]Result {
	type q struct{ url, id string }
	bySite := map[string][]q{}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		site := seSite(u.Hostname())
		m := seQuestion.FindStringSubmatch(u.Path)
		if site == "" || m == nil {
			continue
		}
		bySite[site] = append(bySite[site], q{raw, m[1]})
	}
	out := map[string]Result{}
	sites := make([]string, 0, len(bySite))
	for s := range bySite {
		sites = append(sites, s)
	}
	sort.Strings(sites)
	for _, site := range sites {
		qs := bySite[site]
		for i := 0; i < len(qs); i += 100 {
			part := qs[i:min(i+100, len(qs))]
			seen := map[string]bool{}
			var ids []string
			for _, x := range part {
				if !seen[x.id] {
					seen[x.id] = true
					ids = append(ids, x.id)
				}
			}
			found, ok := c.seQuestions(ctx, endpoint, site, ids)
			if !ok {
				return out // the API refused or could not be reached: the rest keep what the run found
			}
			for _, x := range part {
				if closed, exists := found[x.id]; exists {
					note := "question exists (Stack Exchange API)"
					if closed != "" {
						note = "question exists but is closed: " + closed + " (Stack Exchange API)"
					}
					out[x.url] = Result{Status: StatusOK, Code: 200, Note: note}
				} else {
					out[x.url] = Result{Status: StatusDead, Kind: "page", Code: 404, Note: "question deleted (Stack Exchange API)"}
				}
			}
		}
	}
	return out
}

// seQuestions asks for up to 100 questions of one site: the ids it returns, each with its close reason ("" when open).
func (c *Checker) seQuestions(ctx context.Context, endpoint, site string, ids []string) (map[string]string, bool) {
	q := url.Values{"site": {site}, "pagesize": {"100"}, "filter": {"default"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/questions/"+strings.Join(ids, ";")+"?"+q.Encode(), nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	var out struct {
		Items []struct {
			ID           int64  `json:"question_id"`
			ClosedReason string `json:"closed_reason"`
		} `json:"items"`
		ErrorID int `json:"error_id"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out) != nil || out.ErrorID != 0 {
		return nil, false
	}
	found := map[string]string{}
	for _, it := range out.Items {
		found[strconv.FormatInt(it.ID, 10)] = it.ClosedReason
	}
	return found, true
}
