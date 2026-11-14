// Package sorting files a workspace's bookmarks into folders by rules the owner writes: the step that turns one
// browser's export into a tidy tree. The rules are data (a JSON file in the data folder), never code, because they
// name the owner's own folders, sites and schools.
//
// It plans; it does not edit. Plan says where each bookmark would go and why; Apply returns the tree with those moves
// made. In order, a bookmark:
//
//  1. stays put when it is in a protected folder;
//  2. goes to the review folder for dead links when it is dead (confirmed, unless DeadUnconfirmed);
//  3. goes to a protected folder it matches (by pattern, or by a host already filed there);
//  4. goes where Manual names it, when it is outside every folder;
//  5. on the bar itself, moves only when its topic has a bar folder (BarTopicFolders), else stays;
//  6. stays where it is when the owner filed it;
//  7. otherwise (loose, outside every folder) goes to a review folder (not a web page, local, a saved search, a
//     sign-out page) or to its topic's folder (Topics, then Archive, through TopicFolders and PrefixFolders), and to
//     Review.Unsorted when nothing fits.
//
// Then big folders are split by type (Splits), big piles get a second level (Piles), and named folders get subgroups
// (Groups). A subfolder is made only for a type that would hold at least its TypeMin links, and a folder a sort made is
// not split again.
package sorting

import (
	"cmp"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Rules are the owner's filing rules. Patterns are Go regular expressions, matched without regard to case.
type Rules struct {
	Bar          string      `json:"bar"`           // the bar folder's path, e.g. "Bookmarks Bar"
	Protected    []Protected `json:"protected"`     // folders left as they are, which pull matching links in
	GenericHosts []string    `json:"generic_hosts"` // hosts too common to tie a link to a protected folder
	Review       Review      `json:"review"`
	// DeadUnconfirmed also files links found dead only once (by default only confirmed-dead links move).
	DeadUnconfirmed bool              `json:"dead_unconfirmed"`
	Manual          map[string]string `json:"manual"` // address → folder, decided by hand
	Topics          []Topic           `json:"topics"` // first match wins
	Archive         *Archive          `json:"archive"`
	// TopicFolders maps a topic to the folder that holds it; PrefixFolders catches the topics it does not name.
	TopicFolders  map[string]string `json:"topic_folders"`
	PrefixFolders []PrefixFolder    `json:"prefix_folders"`
	// A loose link on the bar moves only when its topic starts with BarTopicPrefix and BarTopicFolders names it.
	BarTopicPrefix  string            `json:"bar_topic_prefix"`
	BarTopicFolders map[string]string `json:"bar_topic_folders"`
	Splits          []Split           `json:"splits"`
	Piles           []Pile            `json:"piles"`
	Groups          map[string][]Kind `json:"groups"` // folder → subgroups (first match wins; the rest stay)
	TypeMin         int               `json:"type_min"`
}

// Protected is a folder left as it is. Match sees "host title".
type Protected struct {
	Folder string `json:"folder"`
	Match  string `json:"match"`
}

// Review names the folders for links that need a person's eye. Empty ones are not used.
type Review struct {
	Dead     string `json:"dead"`
	NotWeb   string `json:"not_web"`
	Local    string `json:"local"`
	Searches string `json:"searches"`
	Session  string `json:"session"`
	Unsorted string `json:"unsorted"`
}

// Topic is one filing rule. Host matches the host (no "www."); Text matches "title host path" (file extensions
// dropped); Repo matches "<repository name> <title>" for a github.com repository; Profile matches a github.com
// address with fewer than two path parts. Every pattern given must match.
type Topic struct {
	Host    string `json:"host,omitempty"`
	Text    string `json:"text,omitempty"`
	Repo    string `json:"repo,omitempty"`
	Profile bool   `json:"profile,omitempty"`
	Topic   string `json:"topic"`
}

// Archive files a link nothing else placed, added in or before Year, under Topic.
type Archive struct {
	Year  int    `json:"year"`
	Topic string `json:"topic"`
}

// PrefixFolder maps every topic starting with Topic to Folder plus the rest of the topic.
type PrefixFolder struct {
	Topic  string `json:"topic"`
	Folder string `json:"folder"`
}

// Kind is a named group with its pattern; Text is "host title path" (Splits) or "host path title" (Piles, Groups).
type Kind struct {
	Name  string `json:"name"`
	Match string `json:"match"`
}

// Split divides each big folder under one of Under (prefix) or In (exact) by type: a type with TypeMin links gets
// <base>/<type> (or an alias the owner already has), the rest stay in base. Base is the folder, or Rename[folder].
type Split struct {
	Under   []string            `json:"under"`
	In      []string            `json:"in"`
	Min     int                 `json:"min"`
	Types   []Kind              `json:"types"`
	Default string              `json:"default"`
	Aliases map[string][]string `json:"aliases"`
	Rename  map[string]string   `json:"rename"`
}

// Pile gives a big folder a second level when its last name contains LeafContains, starts with LeafPrefix, or is
// one of LeafIn. Links matching no group go to Fallback.
type Pile struct {
	LeafContains string   `json:"leaf_contains,omitempty"`
	LeafPrefix   string   `json:"leaf_prefix,omitempty"`
	LeafIn       []string `json:"leaf_in,omitempty"`
	Min          int      `json:"min"`
	Groups       []Kind   `json:"groups"`
	Fallback     string   `json:"fallback"`
}

// Move is one bookmark's place before and after, and why.
type Move struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// Plan is where every bookmark would go. Moves lists only those whose folder changes.
type Plan struct {
	Moves   []Move         `json:"moves"`
	Folders map[string]int `json:"folders"` // bookmarks per folder afterwards, every folder that holds one
	New     []string       `json:"new"`     // folders that do not exist yet
}

type compiled struct {
	Rules
	protected []*regexp.Regexp
	topics    []struct{ host, text, repo *regexp.Regexp }
	split     [][]*regexp.Regexp
	piles     [][]*regexp.Regexp
	groups    map[string][]*regexp.Regexp
	generic   map[string]bool
}

func re(p string) (*regexp.Regexp, error) {
	if p == "" {
		return nil, nil
	}
	return regexp.Compile("(?i)" + p)
}

func compile(r Rules) (*compiled, error) {
	c := &compiled{Rules: r, groups: map[string][]*regexp.Regexp{}, generic: map[string]bool{}}
	if c.TypeMin <= 0 {
		c.TypeMin = 4
	}
	for _, h := range r.GenericHosts {
		c.generic[strings.ToLower(h)] = true
	}
	bad := func(where string, err error) error { return fmt.Errorf("rules: %s: %w", where, err) }
	for _, p := range r.Protected {
		x, err := re(p.Match)
		if err != nil {
			return nil, bad("protected "+p.Folder, err)
		}
		c.protected = append(c.protected, x)
	}
	for i, t := range r.Topics {
		var e struct{ host, text, repo *regexp.Regexp }
		var err error
		if e.host, err = re(t.Host); err != nil {
			return nil, bad(fmt.Sprintf("topic %d host", i+1), err)
		}
		if e.text, err = re(t.Text); err != nil {
			return nil, bad(fmt.Sprintf("topic %d text", i+1), err)
		}
		if e.repo, err = re(t.Repo); err != nil {
			return nil, bad(fmt.Sprintf("topic %d repo", i+1), err)
		}
		c.topics = append(c.topics, e)
	}
	kinds := func(where string, ks []Kind) ([]*regexp.Regexp, error) {
		var out []*regexp.Regexp
		for _, k := range ks {
			x, err := re(k.Match)
			if err != nil || x == nil {
				return nil, bad(where+" "+k.Name, cmp.Or(err, fmt.Errorf("no pattern")))
			}
			out = append(out, x)
		}
		return out, nil
	}
	for _, s := range r.Splits {
		ks, err := kinds("split type", s.Types)
		if err != nil {
			return nil, err
		}
		c.split = append(c.split, ks)
	}
	for _, p := range r.Piles {
		ks, err := kinds("pile group", p.Groups)
		if err != nil {
			return nil, err
		}
		c.piles = append(c.piles, ks)
	}
	for f, g := range r.Groups {
		ks, err := kinds("group in "+f, g)
		if err != nil {
			return nil, err
		}
		c.groups[f] = ks
	}
	return c, nil
}

// link is one bookmark as the rules see it.
type link struct {
	n         *store.Node
	folder    string // its folder's path ("" outside every folder)
	host      string
	u         *url.URL
	to        string
	why       string
	protected bool // in a protected folder: never moved, never split
}

var (
	localHost = regexp.MustCompile(`(\.test$|\.local$|^localhost$|^127\.|^192\.168\.|^stg-|^staging\.)`)
	googleRe  = regexp.MustCompile(`(^|\.)google\.`)
	sessionRe = regexp.MustCompile(`(?i)(logoff|logout|timeout\.aspx|signout)`)
	extRe     = regexp.MustCompile(`\.(html?|php|aspx?|jsp)\b`)
)

func under(path, folder string) bool { return path == folder || strings.HasPrefix(path, folder+"/") }

// Plan works out where each bookmark of t goes. links are the latest checks by address (dead links need them).
func (r Rules) Plan(t store.Tree, links map[string]store.LinkResult) (Plan, error) {
	c, err := compile(r)
	if err != nil {
		return Plan{}, err
	}
	var ls []*link
	existing := map[string]bool{}
	var walk func(n *store.Node, path string)
	walk = func(n *store.Node, path string) {
		for _, ch := range n.Children {
			if ch.Kind == store.KindFolder {
				p := strings.TrimPrefix(path+"/"+ch.Title, "/")
				existing[p] = true
				walk(ch, p)
				continue
			}
			l := &link{n: ch, folder: path}
			if u, err := url.Parse(ch.URL); err == nil {
				l.u, l.host = u, strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
			}
			ls = append(ls, l)
		}
	}
	if t.Root != nil {
		walk(t.Root, "")
	}
	// The hosts each protected folder already holds tie other links to it.
	held := make([]map[string]bool, len(c.Protected))
	for i, p := range c.Protected {
		held[i] = map[string]bool{}
		for _, l := range ls {
			if under(l.folder, p.Folder) && l.host != "" && !c.generic[l.host] {
				held[i][l.host] = true
			}
		}
	}
	for _, l := range ls {
		l.to, l.why = c.place(l, held, links)
	}
	c.splitAll(ls, existing)
	c.pileAll(ls)
	c.groupAll(ls)

	var p Plan
	p.Folders = map[string]int{}
	for _, l := range ls {
		p.Folders[l.to]++
		if l.to != l.folder {
			p.Moves = append(p.Moves, Move{ID: l.n.ID, Title: l.n.Title, URL: l.n.URL, From: l.folder, To: l.to, Reason: l.why})
		}
	}
	for f := range p.Folders {
		if f != "" && !existing[f] {
			p.New = append(p.New, f)
		}
	}
	slices.Sort(p.New)
	return p, nil
}

func (c *compiled) place(l *link, held []map[string]bool, links map[string]store.LinkResult) (string, string) {
	for _, p := range c.Protected {
		if under(l.folder, p.Folder) {
			l.protected = true
			return l.folder, "in a protected folder"
		}
	}
	// A link the review folders claim (not a web page, local, a saved search, a sign-out page) is never pulled into a
	// protected folder: those are housekeeping, whatever site they are on.
	match := ""
	reviewed, _ := c.review(l)
	for i, p := range c.Protected {
		if reviewed != "" {
			break
		}
		if held[i][l.host] || c.protected[i] != nil && c.protected[i].MatchString(l.host+" "+l.n.Title) {
			match = p.Folder
			break
		}
	}
	if r, ok := links[l.n.URL]; ok && r.Status == "dead" && (r.Confirmed || c.DeadUnconfirmed) && match == "" && c.Review.Dead != "" {
		return c.Review.Dead, "dead link"
	}
	if match != "" {
		return match, "matches " + match
	}
	if f, ok := c.Manual[l.n.URL]; ok && l.folder == "" {
		return f, "filed by hand"
	}
	if c.Bar != "" && l.folder == c.Bar {
		if t := c.topic(l); c.BarTopicPrefix != "" && strings.HasPrefix(t, c.BarTopicPrefix) {
			if f, ok := c.BarTopicFolders[t]; ok {
				return f, "on the bar: topic " + t
			}
		}
		return l.folder, "on the bar"
	}
	if l.folder != "" {
		return l.folder, "where you filed it"
	}
	if f, why := c.review(l); f != "" {
		return f, why
	}
	t := c.topic(l)
	if t == "" && c.Archive != nil && l.n.Added != 0 && time.Unix(l.n.Added, 0).UTC().Year() <= c.Archive.Year {
		t = c.Archive.Topic
	}
	if f, ok := c.TopicFolders[t]; ok && t != "" {
		return f, "topic " + t
	}
	for _, pf := range c.PrefixFolders {
		if rest, ok := strings.CutPrefix(t, pf.Topic); ok && t != "" {
			return pf.Folder + rest, "topic " + t
		}
	}
	return c.Review.Unsorted, "no rule matched"
}

// review is the review folder for a link that is not a page to keep as it is ("" when it is one).
func (c *compiled) review(l *link) (string, string) {
	switch {
	case l.u == nil || l.u.Scheme != "http" && l.u.Scheme != "https":
		return c.Review.NotWeb, "not a web link"
	case localHost.MatchString(l.host):
		return c.Review.Local, "local or staging site"
	case googleRe.MatchString(l.host) && strings.HasPrefix(l.u.Path, "/search"):
		return c.Review.Searches, "a saved search"
	case sessionRe.MatchString(l.n.URL):
		return c.Review.Session, "a sign-out or timeout page"
	}
	return "", ""
}

// topic is the first Topics rule a web link matches ("" for none).
func (c *compiled) topic(l *link) string {
	if l.u == nil || l.u.Scheme != "http" && l.u.Scheme != "https" {
		return ""
	}
	if f, _ := c.review(l); f != "" {
		return ""
	}
	text := strings.ToLower(l.n.Title + " " + l.host + " " + extRe.ReplaceAllString(l.u.Path, " "))
	parts := strings.FieldsFunc(l.u.Path, func(r rune) bool { return r == '/' })
	for i, t := range c.Topics {
		x := c.topics[i]
		if x.host != nil && !x.host.MatchString(l.host) || x.text != nil && !x.text.MatchString(text) {
			continue
		}
		if t.Profile && (l.host != "github.com" || len(parts) >= 2) {
			continue
		}
		if x.repo != nil && (l.host != "github.com" || len(parts) < 2 || !x.repo.MatchString(strings.ToLower(parts[1]+" "+l.n.Title))) {
			continue
		}
		return t.Topic
	}
	return ""
}

// texts as the type rules read them: "host title path", and "host path title" for piles and groups.
func typeText(l *link) string {
	if l.u == nil {
		return strings.ToLower(l.n.Title)
	}
	return strings.ToLower(l.host + " " + l.n.Title + " " + l.u.Path)
}
func pileText(l *link) string {
	if l.u == nil {
		return strings.ToLower(l.n.Title)
	}
	return strings.ToLower(l.host + " " + l.u.Path + " " + l.n.Title)
}

func byFolder(ls []*link, keep func(string) bool) (map[string][]*link, []string) {
	m := map[string][]*link{}
	var order []string
	for _, l := range ls {
		if l.protected || !keep(l.to) {
			continue
		}
		if _, ok := m[l.to]; !ok {
			order = append(order, l.to)
		}
		m[l.to] = append(m[l.to], l)
	}
	return m, order
}

func (c *compiled) splitAll(ls []*link, existing map[string]bool) {
	for si, s := range c.Splits {
		in := func(f string) bool {
			return slices.Contains(s.In, f) || slices.ContainsFunc(s.Under, func(u string) bool { return under(f, u) })
		}
		m, order := byFolder(ls, in)
		for _, f := range order {
			group := m[f]
			if len(group) < s.Min || slices.Contains(s.made(), leafOf(f)) {
				continue // too small, or a folder an earlier sort made: sorting again changes nothing
			}
			kind := map[*link]string{}
			count := map[string]int{}
			for _, l := range group {
				k := s.Default
				for i, x := range c.split[si] {
					if x.MatchString(typeText(l)) {
						k = s.Types[i].Name
						break
					}
				}
				kind[l] = k
				count[k]++
			}
			big := 0
			for _, n := range count {
				if n >= c.TypeMin {
					big++
				}
			}
			if big < 2 {
				continue
			}
			base := cmp.Or(s.Rename[f], f)
			for _, l := range group {
				k := kind[l]
				if count[k] < c.TypeMin {
					l.to = base
					continue
				}
				l.to = base + "/" + k
				for _, a := range s.Aliases[k] {
					if existing[base+"/"+a] {
						l.to = base + "/" + a
						break
					}
				}
				l.why += "; " + k
			}
		}
	}
}

func (c *compiled) pileAll(ls []*link) {
	m, order := byFolder(ls, func(string) bool { return true })
	for _, f := range order {
		leaf := leafOf(f)
		for pi, p := range c.Piles {
			if !(p.LeafContains != "" && strings.Contains(leaf, p.LeafContains) || p.LeafPrefix != "" && strings.HasPrefix(leaf, p.LeafPrefix) || slices.Contains(p.LeafIn, leaf)) {
				continue
			}
			group := m[f]
			if len(group) < p.Min {
				break
			}
			name := map[*link]string{}
			count := map[string]int{}
			for _, l := range group {
				n := p.Fallback
				for i, x := range c.piles[pi] {
					if x.MatchString(pileText(l)) {
						n = p.Groups[i].Name
						break
					}
				}
				name[l] = n
				count[n]++
			}
			big := 0
			for _, n := range count {
				if n >= c.TypeMin {
					big++
				}
			}
			if big < 2 {
				break
			}
			for _, l := range group {
				if count[name[l]] >= c.TypeMin {
					l.to = f + "/" + name[l]
					l.why += "; " + name[l]
				}
			}
			break
		}
	}
}

func (c *compiled) groupAll(ls []*link) {
	m, order := byFolder(ls, func(f string) bool { _, ok := c.Groups[f]; return ok }) // only the named folders: their
	// subgroups are never keys themselves, so sorting again finds nothing new
	for _, f := range order {
		group, rules := m[f], c.groups[f]
		name := map[*link]string{}
		count := map[string]int{}
		for _, l := range group {
			for i, x := range rules {
				if x.MatchString(pileText(l)) {
					name[l] = c.Groups[f][i].Name
					count[name[l]]++
					break
				}
			}
		}
		for _, l := range group {
			if n := name[l]; n != "" && count[n] >= c.TypeMin {
				l.to = f + "/" + n
				l.why += "; " + n
			}
		}
	}
}

func leafOf(f string) string { return f[strings.LastIndex(f, "/")+1:] }

// made is every folder name the split can create: its types, its default and their aliases.
func (s Split) made() []string {
	out := []string{s.Default}
	for _, k := range s.Types {
		out = append(out, k.Name)
	}
	for _, as := range s.Aliases {
		out = append(out, as...)
	}
	return out
}

// Only keeps the moves into the given folders, for an owner who ticked some of a plan's folders and not others.
// Folders lists every destination afterwards as before; New drops the folders nothing now goes to.
func (p Plan) Only(folders []string) Plan {
	keep := map[string]bool{}
	for _, f := range folders {
		keep[f] = true
	}
	out := Plan{Folders: p.Folders}
	going := map[string]bool{}
	for _, m := range p.Moves {
		if keep[m.To] {
			out.Moves = append(out.Moves, m)
			going[m.To] = true
		}
	}
	for _, f := range p.New {
		if going[f] {
			out.New = append(out.New, f)
		}
	}
	return out
}
