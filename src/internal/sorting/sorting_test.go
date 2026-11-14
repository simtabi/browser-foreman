package sorting

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

func bm(id, title, u string) *store.Node {
	return &store.Node{ID: id, Kind: store.KindBookmark, Title: title, URL: u}
}
func dir(id, title string, c ...*store.Node) *store.Node {
	return &store.Node{ID: id, Kind: store.KindFolder, Title: title, Children: c}
}

// rules is a small, made-up rule set touching every step.
func rules() Rules {
	return Rules{
		Bar:          "Bar",
		Protected:    []Protected{{Folder: "Bar/School", Match: `(^|\.)school\.example$|\bcampus\b`}},
		GenericHosts: []string{"github.com"},
		Review: Review{Dead: "Bar/_Review/Dead", NotWeb: "Bar/_Review/Not web", Local: "Bar/_Review/Local",
			Searches: "Bar/_Review/Searches", Session: "Bar/_Review/Session", Unsorted: "Bar/_Review/Unsorted"},
		Manual: map[string]string{"https://hand.example/": "Bar/Picked"},
		Topics: []Topic{
			{Host: `^github\.com$`, Profile: true, Topic: "Dev/People"},
			{Host: `^github\.com$`, Repo: `laravel`, Topic: "Dev/PHP"},
			{Host: `^github\.com$`, Topic: "Dev/Other repos"},
			{Host: `php\.net$`, Topic: "Dev/PHP"},
			{Text: `\bdocker\b`, Topic: "Dev/Ops"},
			{Text: `recipe`, Topic: "Home/Cooking"},
		},
		Archive:         &Archive{Year: 2019, Topic: "Old"},
		TopicFolders:    map[string]string{"Home/Cooking": "Bar/Kitchen", "Old": "Bar/_Old"},
		PrefixFolders:   []PrefixFolder{{Topic: "Dev/", Folder: "Bar/Dev/"}},
		BarTopicPrefix:  "Dev/",
		BarTopicFolders: map[string]string{"Dev/PHP": "Bar/Dev/PHP"},
		TypeMin:         2,
	}
}

func where(t *testing.T, p Plan) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, mv := range p.Moves {
		m[mv.ID] = mv.To
	}
	return m
}

func TestPlanFollowsTheStepsInOrder(t *testing.T) {
	old := time.Date(2018, 3, 1, 0, 0, 0, 0, time.UTC).Unix()
	tr := store.Tree{Root: dir("root", "",
		dir("bar", "Bar",
			dir("school", "School", bm("s1", "Timetable", "https://school.example/t"), // protected: stays
				dir("notes", "Notes", bm("s2", "Notes", "https://school.example/n"))), // a protected subfolder: stays
			dir("mine", "Mine", bm("m1", "My docker notes", "https://notes.example/docker")), // filed: stays
			bm("b1", "laravel/framework", "https://github.com/laravel/framework"),            // on the bar, a bar topic: moves
			bm("b2", "Recipe of the day", "https://food.example/r"),                          // on the bar, not a bar topic: stays
		),
		bm("l1", "Campus map", "https://maps.example/campus"),              // matches the protected folder's pattern
		bm("l2", "Timetable copy", "https://school.example/t2"),            // a host the protected folder holds
		bm("l3", "Gone", "https://gone.example/"),                          // confirmed dead
		bm("l4", "Hand", "https://hand.example/"),                          // filed by hand
		bm("l5", "ftp", "ftp://files.example/"),                            // not a web link
		bm("l6", "dev", "http://site.test/"),                               // local
		bm("l7", "q", "https://www.google.com/search?q=x"),                 // saved search
		bm("l8", "Sign out", "https://school.example/logout"),              // sign-out: review wins over protected
		bm("l9", "taylorotwell", "https://github.com/taylorotwell"),        // a GitHub profile
		bm("l10", "symfony/symfony", "https://github.com/symfony/symfony"), // another repo
		bm("l11", "Docker docs", "https://docs.example/docker-intro"),      // by text
		bm("l12", "Pie recipe", "https://food.example/pie"),                // topic with its own folder
		bm("l13", "Nothing", "https://nothing.example/"),                   // no rule: unsorted
		&store.Node{ID: "l14", Kind: store.KindBookmark, Title: "Old page", URL: "https://old.example/", Added: old},
		bm("l15", "Unsure", "https://flaky.example/"), // dead once only: not moved
	)}
	links := map[string]store.LinkResult{
		"https://gone.example/":  {Status: "dead", Confirmed: true},
		"https://flaky.example/": {Status: "dead"},
	}
	p, err := rules().Plan(tr, links)
	if err != nil {
		t.Fatal(err)
	}
	got := where(t, p)
	want := map[string]string{
		"b1": "Bar/Dev/PHP", "l1": "Bar/School", "l2": "Bar/School", "l3": "Bar/_Review/Dead", "l4": "Bar/Picked",
		"l5": "Bar/_Review/Not web", "l6": "Bar/_Review/Local", "l7": "Bar/_Review/Searches", "l8": "Bar/_Review/Session",
		"l9": "Bar/Dev/People", "l10": "Bar/Dev/Other repos", "l11": "Bar/Dev/Ops", "l12": "Bar/Kitchen",
		"l13": "Bar/_Review/Unsorted", "l14": "Bar/_Old", "l15": "Bar/_Review/Unsorted",
	}
	for id, to := range want {
		if got[id] != to {
			t.Errorf("%s went to %q, want %q", id, got[id], to)
		}
	}
	for _, id := range []string{"s1", "s2", "m1", "b2"} {
		if to, moved := got[id]; moved {
			t.Errorf("%s moved to %q; it should stay", id, to)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d moves, want %d: %v", len(got), len(want), got)
	}
	if !strings.Contains(strings.Join(p.New, ","), "Bar/_Review/Dead") || strings.Contains(strings.Join(p.New, ","), "Bar/School") {
		t.Fatalf("new folders %v", p.New)
	}
	// Dead links found once move too when the owner says so.
	r := rules()
	r.DeadUnconfirmed = true
	if p, _ := r.Plan(tr, links); where(t, p)["l15"] != "Bar/_Review/Dead" {
		t.Fatal("dead_unconfirmed")
	}
}

func TestSplitsPilesAndGroups(t *testing.T) {
	var kids []*store.Node
	add := func(prefix, title, u string, n int) {
		for i := range n {
			kids = append(kids, bm(fmt.Sprintf("%s%d", prefix, i), fmt.Sprintf("%s %d", title, i), fmt.Sprintf("%s/%d", u, i)))
		}
	}
	add("t", "How to deploy", "https://blog.example/posts", 3)
	add("k", "Starter kit", "https://kits.example/k", 3)
	add("o", "Thing", "https://misc.example/x", 1) // a type below TypeMin stays in the base
	r := rules()
	r.Splits = []Split{{Under: []string{"Bar/Dev"}, Min: 5, Default: "Tools",
		Types:   []Kind{{Name: "Tutorials", Match: `how to`}, {Name: "Kits", Match: `\bkit\b`}},
		Aliases: map[string][]string{"Tutorials": {"Reads"}}, Rename: map[string]string{"Bar/Dev": "Bar/Dev/General"}}}
	tr := store.Tree{Root: dir("root", "", dir("bar", "Bar", dir("dev", "Dev", dir("reads", "Reads")), dir("q", "Q&A")))}
	// Everything starts loose with the topic "Dev/" so it lands in Bar/Dev, then the split runs.
	r.Topics = []Topic{{Text: `.`, Topic: "Dev/"}}
	r.PrefixFolders = []PrefixFolder{{Topic: "Dev/", Folder: "Bar/Dev"}}
	tr.Root.Children = append(tr.Root.Children, kids...)
	p, err := r.Plan(tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := where(t, p)
	if got["t0"] != "Bar/Dev/General/Tutorials" || got["k1"] != "Bar/Dev/General/Kits" || got["o0"] != "Bar/Dev/General" {
		t.Fatalf("split %v", got)
	}
	// The alias is used when the owner already has that folder under the base.
	r.Splits[0].Rename = nil
	if p, _ := r.Plan(tr, nil); where(t, p)["t0"] != "Bar/Dev/Reads" {
		t.Fatalf("alias %v", where(t, p))
	}
	// A pile and a group: second levels by name.
	r = rules()
	r.Topics = []Topic{{Text: `.`, Topic: "Q"}}
	r.TopicFolders = map[string]string{"Q": "Bar/Q&A"}
	r.Piles = []Pile{{LeafContains: "Q&A", Min: 4, Fallback: "Other", Groups: []Kind{{Name: "PHP", Match: `php`}, {Name: "JS", Match: `javascript`}}}}
	tr = store.Tree{Root: dir("root", "", dir("bar", "Bar"),
		bm("p1", "php arrays", "https://qa.example/1"), bm("p2", "php strings", "https://qa.example/2"),
		bm("j1", "javascript maps", "https://qa.example/3"), bm("j2", "javascript sets", "https://qa.example/4"),
		bm("x1", "rust", "https://qa.example/5"))}
	p, _ = r.Plan(tr, nil)
	got = where(t, p)
	if got["p1"] != "Bar/Q&A/PHP" || got["j2"] != "Bar/Q&A/JS" || got["x1"] != "Bar/Q&A" {
		t.Fatalf("pile %v", got)
	}
	r.Piles = nil
	r.Groups = map[string][]Kind{"Bar/Q&A": {{Name: "PHP", Match: `php`}, {Name: "Rust", Match: `rust`}}}
	p, _ = r.Plan(tr, nil)
	if got := where(t, p); got["p2"] != "Bar/Q&A/PHP" || got["j1"] != "Bar/Q&A" || got["x1"] != "Bar/Q&A" {
		t.Fatalf("group (a group of one stays in the folder) %v", got)
	}
}

func TestApplyMakesFoldersAndKeepsEveryBookmark(t *testing.T) {
	// Two folders called Old: what moves to "Bar/Old" goes into the first.
	tr := store.Tree{Root: dir("root", "", dir("bar", "Bar", dir("old", "Old", bm("a", "A", "https://a.example/")), dir("old2", "Old")),
		bm("b", "B", "https://b.example/"))}
	n := 0
	out, err := Apply(tr, Plan{Moves: []Move{{ID: "a", To: "Bar/New/Deep"}, {ID: "b", To: "Bar/Old"}, {ID: "gone", To: "Bar/X"}}},
		func() string { n++; return fmt.Sprint("f", n) })
	if err != nil {
		t.Fatal(err)
	}
	bar := out.Root.Children[0]
	if len(out.Root.Children) != 1 || bar.Children[0].Title != "Old" || bar.Children[0].Children[0].ID != "b" ||
		len(bar.Children[1].Children) != 0 || bar.Children[2].Title != "New" || bar.Children[2].Children[0].Children[0].ID != "a" {
		b, _ := fmt.Println(out.Root)
		t.Fatalf("tree %d", b)
	}
	if tr.Root.Children[1].ID != "b" {
		t.Fatal("the input tree was changed")
	}
}

func TestBadRulesAreRefusedByName(t *testing.T) {
	r := rules()
	r.Topics = append(r.Topics, Topic{Text: `(unclosed`, Topic: "x"})
	if _, err := r.Plan(store.Tree{Root: dir("root", "")}, nil); err == nil || !strings.Contains(err.Error(), "topic 7 text") {
		t.Fatalf("%v", err)
	}
	r = rules()
	r.Piles = []Pile{{LeafContains: "x", Groups: []Kind{{Name: "empty"}}}}
	if _, err := r.Plan(store.Tree{Root: dir("root", "")}, nil); err == nil || !strings.Contains(err.Error(), "pile group empty") {
		t.Fatalf("%v", err)
	}
}

// Sorting a sorted tree changes nothing: a folder a sort made (a type, a pile group) is not split again, even when it
// holds a mix, as a folder sorted by hand or by an older rule set does.
func TestSortingTwiceMovesNothing(t *testing.T) {
	r := rules()
	r.Splits = []Split{{Under: []string{"Bar/Dev"}, Min: 3, Default: "Tools",
		Types: []Kind{{Name: "Tutorials", Match: `how to`}, {Name: "Kits", Match: `\bkit\b`}}}}
	r.Piles = []Pile{{LeafContains: "Q&A", Min: 3, Fallback: "Other", Groups: []Kind{{Name: "PHP", Match: `php`}, {Name: "JS", Match: `javascript`}}}}
	tr := store.Tree{Root: dir("root", "", dir("bar", "Bar",
		dir("dev", "Dev", dir("tut", "Tutorials",
			bm("t1", "how to a", "https://x.example/1"), bm("t2", "how to b", "https://x.example/2"),
			bm("k1", "a kit", "https://x.example/3"), bm("k2", "b kit", "https://x.example/4"))),
		dir("qa", "Q&A", dir("php", "PHP",
			bm("p1", "php a", "https://q.example/1"), bm("p2", "php b", "https://q.example/2"),
			bm("j1", "javascript a", "https://q.example/3"), bm("j2", "javascript b", "https://q.example/4"))))),
	}
	p, err := r.Plan(tr, nil)
	if err != nil || len(p.Moves) != 0 {
		t.Fatalf("%v; moved %+v", err, p.Moves)
	}
}

// The hand-filed list places loose bookmarks only: once the owner has filed one, a later sort leaves it there.
func TestManualLeavesFiledBookmarksAlone(t *testing.T) {
	tr := store.Tree{Root: dir("root", "", dir("bar", "Bar", dir("mine", "Mine", bm("f", "Hand", "https://hand.example/"))),
		bm("l", "Hand", "https://hand.example/"))}
	p, _ := rules().Plan(tr, nil)
	if got := where(t, p); got["l"] != "Bar/Picked" || got["f"] != "" {
		t.Fatalf("%v", got)
	}
}

func TestOnlyKeepsTheTickedFolders(t *testing.T) {
	p := Plan{Moves: []Move{{ID: "a", To: "X"}, {ID: "b", To: "Y"}, {ID: "c", To: "Y"}}, New: []string{"X", "Y"}, Folders: map[string]int{"X": 1, "Y": 2}}
	o := p.Only([]string{"Y"})
	if len(o.Moves) != 2 || o.Moves[0].ID != "b" || len(o.New) != 1 || o.New[0] != "Y" {
		t.Fatalf("%+v", o)
	}
	if o := p.Only([]string{}); len(o.Moves) != 0 || len(o.New) != 0 {
		t.Fatalf("none ticked %+v", o)
	}
}
