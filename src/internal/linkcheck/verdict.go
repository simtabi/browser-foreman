package linkcheck

import (
	"regexp"
	"strings"
)

// What a fetched page really is, from its title and opening text. Strong signals give a verdict; a weak "not found"
// near the top only adds a note.
var (
	soft404Title = regexp.MustCompile(`(?i)^\s*(error\s*)?(404|410)\b|^\s*(page|file|post|article|resource|project|repository)?\s*not\s+found\s*([-|:–—].{0,40})?$|` +
		`^\s*(oops|sorry|whoops)[!.,]?\s.*(not\s+found|can.?t\s+(be\s+)?find|doesn.?t\s+exist|no\s+longer)|` +
		`page\s+(cannot|can.?t|could\s+not)\s+be\s+found|page\s+(does\s+not|doesn.?t)\s+exist|` +
		`^\s*(this\s+)?page\s+(is\s+)?(no\s+longer\s+available|has\s+been\s+removed|unavailable)`)
	parked = regexp.MustCompile(`(?i)(this\s+)?domain\s+(name\s+)?(is\s+)?(for\s+sale|may\s+be\s+for\s+sale|is\s+parked|has\s+expired)|buy\s+this\s+domain|` +
		`domain\s+parking|parked\s+(free|domain)|hugedomains|dan\.com|\bsedo\b|afternic|godaddy\s+(domain\s+)?auction|` +
		`the\s+domain\s+.{0,40}\s+is\s+for\s+sale|inquire\s+about\s+this\s+domain|domain\s+expired|` +
		`^\s*[\w-]+(\.[\w-]+)+\s+is\s+for\s+sale|make\s+(an\s+)?offer\s+on\s+this\s+domain`)
	suspended = regexp.MustCompile(`(?i)account\s+(has\s+been\s+)?suspended|this\s+account\s+has\s+been\s+suspended|` +
		`site\s+(has\s+been\s+)?(suspended|deactivated|disabled)|website\s+is\s+(no\s+longer\s+)?(suspended|offline)|` +
		`no\s+longer\s+in\s+service|this\s+site\s+can.?t\s+be\s+reached|welcome\s+to\s+nginx|` +
		`apache2?\s+(ubuntu\s+)?default\s+page|it\s+works!|index\s+of\s+/\s*$|future\s+home\s+of`)
	challenge = regexp.MustCompile(`(?i)just\s+a\s+moment|attention\s+required|checking\s+(if\s+the\s+site|your\s+browser)|` +
		`verify(ing)?\s+you\s+are\s+(a\s+)?human|are\s+you\s+a\s+robot|access\s+denied|cf-chl|captcha|` +
		`ddos[- ]guard|please\s+enable\s+(javascript\s+and\s+)?cookies|request\s+blocked`)
	weak404 = regexp.MustCompile(`(?i)\b404\b.{0,60}(not\s+found|error)|page\s+not\s+found|we\s+couldn.?t\s+find\s+(that|this|the)\s+page|` +
		`nothing\s+(was\s+)?found\s+(here|at\s+this)|no\s+longer\s+available|has\s+been\s+(deleted|removed)`)
	space = regexp.MustCompile(`\s+`)
)

// Verdict is ok, soft-404, parked, suspended, challenge or suspect-404, with what gave it away.
func Verdict(title, text, rawURL string) (string, string) {
	title = strings.TrimSpace(title)
	head := text
	if len(head) > 1500 {
		head = head[:1500]
	}
	short := func(s string, n int) string {
		if len(s) > n {
			return s[:n]
		}
		return s
	}
	what := title
	if what == "" {
		what = short(head, 60)
	}
	switch {
	case challenge.MatchString(title) || (len(head) < 400 && challenge.MatchString(head)):
		return "challenge", "bot check page: " + what
	case parked.MatchString(title) || (len(head) < 700 && parked.MatchString(head)):
		return "parked", "parked or for-sale domain: " + what
	case suspended.MatchString(title) || (len(head) < 600 && suspended.MatchString(head)):
		return "suspended", "site switched off: " + what
	case soft404Title.MatchString(title) && !strings.Contains(rawURL, "404"):
		return "soft-404", "page says it is not found: " + short(title, 80)
	case weak404.MatchString(short(head, 600)) && !strings.Contains(rawURL, "404"):
		return "suspect-404", "may be a 'not found' page: " + space.ReplaceAllString(short(head, 90), " ")
	}
	return "ok", ""
}
