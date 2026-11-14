package linkcheck

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// WaybackAPI is the Internet Archive's availability endpoint: given an address, the closest archived copy.
const WaybackAPI = "https://archive.org/wayback/available"

// Wayback finds the Internet Archive's closest copy of raw, through endpoint (WaybackAPI outside tests). It sends the
// address to archive.org, so it runs only when the owner turned the lookup on, and only for confirmed-dead links. ""
// when there is no copy or the archive could not be asked; only an https copy on web.archive.org is accepted.
func (c *Checker) Wayback(ctx context.Context, endpoint, raw string) string {
	q, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	q.RawQuery = url.Values{"url": {raw}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.String(), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.Client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Snapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Status    string `json:"status"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out) != nil {
		return ""
	}
	cl := out.Snapshots.Closest
	if !cl.Available || !strings.HasPrefix(cl.Status, "2") {
		return ""
	}
	u, err := url.Parse(cl.URL)
	if err != nil || u.Hostname() != "web.archive.org" {
		return ""
	}
	u.Scheme = "https"
	return u.String()
}
