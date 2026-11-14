package linkcheck

import (
	"bytes"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// ConfirmAfter is how long a dead link must stay dead before a check confirms it: one bad check is a suspicion.
const ConfirmAfter = 24 * time.Hour

// FrameRefused is Result.Frame for a page that forbids being shown inside another page.
const FrameRefused = "refused"

// frameRule reads whether a page may be shown in a frame on Browser Foreman's own page (another origin). Either header
// can refuse: X-Frame-Options DENY or SAMEORIGIN, or a CSP frame-ancestors that does not allow every origin.
func frameRule(h http.Header) string {
	switch strings.ToUpper(strings.TrimSpace(h.Get("X-Frame-Options"))) {
	case "DENY", "SAMEORIGIN":
		return FrameRefused
	}
	for _, csp := range h.Values("Content-Security-Policy") {
		for _, d := range strings.Split(csp, ";") {
			f := strings.Fields(strings.ToLower(d))
			if len(f) == 0 || f[0] != "frame-ancestors" {
				continue
			}
			for _, src := range f[1:] {
				if src == "*" {
					return ""
				}
			}
			return FrameRefused
		}
	}
	return ""
}

// pageMeta reads what a page says about itself from its first bytes: the OpenGraph title, description, image and
// site name, and the canonical link. Addresses are made absolute against where the page was found, and only http and
// https ones are kept. Nil when the page says nothing.
func pageMeta(b []byte, base string) *store.PageMeta {
	z := html.NewTokenizer(bytes.NewReader(b))
	var m store.PageMeta
	abs := func(ref string) string {
		u, err := url.Parse(base)
		if err != nil {
			return ""
		}
		r, err := u.Parse(strings.TrimSpace(ref))
		if err != nil || (r.Scheme != "http" && r.Scheme != "https") || len(r.String()) > 2048 {
			return ""
		}
		return r.String()
	}
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, more := z.TagName()
		if string(name) == "body" {
			break
		}
		if string(name) != "meta" && string(name) != "link" {
			continue
		}
		attrs := map[string]string{}
		for more {
			var k, v []byte
			k, v, more = z.TagAttr()
			attrs[string(k)] = string(v)
		}
		if string(name) == "link" {
			if strings.EqualFold(attrs["rel"], "canonical") && m.Canonical == "" {
				m.Canonical = abs(attrs["href"])
			}
			continue
		}
		key := strings.ToLower(attrs["property"])
		if key == "" {
			key = strings.ToLower(attrs["name"])
		}
		v := strings.TrimSpace(attrs["content"])
		switch key {
		case "og:title", "twitter:title":
			if m.Title == "" {
				m.Title = short(v, 300)
			}
		case "og:description", "twitter:description", "description":
			if m.Description == "" {
				m.Description = short(v, 600)
			}
		case "og:image", "og:image:secure_url", "twitter:image":
			if m.Image == "" {
				m.Image = abs(v)
			}
		case "og:site_name":
			if m.Site == "" {
				m.Site = short(v, 120)
			}
		}
	}
	if m == (store.PageMeta{}) {
		return nil
	}
	return &m
}

// Settle folds what a check found into what was known before (known says whether there was an earlier result):
//   - a dead link stays a suspicion until a check at least ConfirmAfter after it was first seen dead finds it dead
//     again, and a confirmed one stays confirmed while it stays dead; anything else clears both;
//   - a result that read nothing about the page (a dead or blocked one) keeps the page's description and framing,
//     and a still-dead link keeps the archive copy found for it.
//
// Results stored before DeadSince existed count from when they were checked.
func Settle(before store.LinkResult, known bool, r Result) Result {
	wasDead := known && before.Status == StatusDead
	if r.Status == StatusDead {
		r.DeadSince = r.Checked
		if wasDead {
			r.DeadSince = before.DeadSince
			if r.DeadSince.IsZero() {
				r.DeadSince = before.Checked
			}
			r.Confirmed = before.Confirmed || r.Checked.Sub(r.DeadSince) >= ConfirmAfter
			r.Archive = before.Archive
		}
	}
	if known {
		if r.Meta == nil {
			r.Meta = before.Meta
		}
		if r.Frame == "" && r.Status != StatusOK && r.Status != StatusMoved {
			r.Frame = before.Frame
		}
	}
	return r
}

// forbiddenTitle is a title that announces an error page, which Verdict lets through as "ok" when nothing else
// gives it away ("403 Forbidden" from nginx, say).
var forbiddenTitle = regexp.MustCompile(`(?i)\b(40[0-9]|50[0-9]|52[0-9]|forbidden|error|denied|blocked|unavailable)\b`)

// minRendered is how much visible text a rendered page must hold to count as a working one.
const minRendered = 200

// FromRender judges a page from what headless Chrome built of it (media.Snapper.Render): for links a plain request
// could not settle (a bot check, a 403, a timeout). Strict on purpose, since there is no status code: it is "ok" only
// for an ordinary page with real content and no error title, "dead" for a page that says it is not found or parked,
// and "unverified" otherwise. ok is false when the render says nothing new.
func FromRender(dom []byte, raw string) (r Result, ok bool) {
	title, text := pageText(dom)
	switch v, why := Verdict(title, text, raw); v {
	case "soft-404":
		return Result{Status: StatusDead, Kind: "page", Note: why + " (in Chrome)"}, true
	case "parked", "suspended":
		return Result{Status: StatusDead, Kind: "host", Note: why + " (in Chrome)"}, true
	case "ok":
		if len(text) >= minRendered && !forbiddenTitle.MatchString(title) {
			return Result{Status: StatusOK, Note: "loaded normally in Chrome", Meta: pageMeta(dom, raw)}, true
		}
	}
	return Result{}, false
}
