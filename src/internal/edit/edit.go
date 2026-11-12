// Package edit applies changes to a bookmark tree: create, update, move, trash, restore and purge. It is pure: it takes
// a tree and returns a new one with the events that describe what happened, and never touches storage. The API, the CLI
// and the web interface all change bookmarks through it, so one set of rules holds everywhere.
package edit

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Op is one change. Which fields count depends on Op:
//
//	create  Parent, Index (end when nil), Kind, Title, URL (bookmarks), Note
//	update  ID, and any of Title, URL, Note
//	move    IDs, Parent, Index (end when nil), in the order given
//	trash   IDs, Reason
//	restore IDs (back where each came from; to the first top folder if that is gone)
//	purge   IDs (gone for good; every trashed node when empty)
type Op struct {
	Op     string   `json:"op"`
	ID     string   `json:"id,omitempty"`
	IDs    []string `json:"ids,omitempty"`
	Parent string   `json:"parent,omitempty"`
	Index  *int     `json:"index,omitempty"`
	Kind   string   `json:"kind,omitempty"`
	Title  *string  `json:"title,omitempty"`
	URL    *string  `json:"url,omitempty"`
	Note   *string  `json:"note,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// Event says what one op did, in the words of the event stream: bookmark.created, folder.moved, trash.emptied…
type Event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Error is a change the rules refuse; the API answers it with 400.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func refuse(format string, a ...any) error { return &Error{fmt.Sprintf(format, a...)} }

// IsRefusal reports whether err is a refused change rather than a failure.
func IsRefusal(err error) bool { var e *Error; return errors.As(err, &e) }

const maxText = 2000

// Apply runs ops in order on a copy of t. Either every op applies or none does. newID makes ids for created nodes.
func Apply(t store.Tree, ops []Op, now time.Time, newID func() string) (store.Tree, []Event, error) {
	if len(ops) == 0 {
		return t, nil, refuse("no changes given")
	}
	if len(ops) > 1000 {
		return t, nil, refuse("at most 1000 changes at once")
	}
	c, err := clone(t)
	if err != nil {
		return t, nil, err
	}
	e := &editor{t: &c, now: now, newID: newID}
	for i, op := range ops {
		if err := e.apply(op); err != nil {
			if len(ops) > 1 && IsRefusal(err) {
				err = refuse("change %d (%s): %s", i+1, op.Op, err)
			}
			return t, nil, err
		}
	}
	if err := c.Validate(); err != nil {
		return t, nil, err
	}
	return c, e.events, nil
}

// Summary describes events in a line for the history: "Moved 3 bookmarks", "Edited 2 items".
func Summary(events []Event) string {
	if len(events) == 1 {
		return describe(events[0])
	}
	counts := map[string]int{}
	order := []string{}
	for _, e := range events {
		verb := e.Type[strings.Index(e.Type, ".")+1:]
		if counts[verb] == 0 {
			order = append(order, verb)
		}
		counts[verb]++
	}
	parts := []string{}
	for _, v := range order {
		parts = append(parts, fmt.Sprintf("%s %d", verbs[v], counts[v]))
	}
	return strings.Join(parts, ", ")
}

var verbs = map[string]string{"created": "Added", "updated": "Edited", "moved": "Moved", "trashed": "Trashed",
	"restored": "Restored", "deleted": "Deleted", "emptied": "Emptied the Trash of"}

func describe(e Event) string {
	what, _ := e.Data["title"].(string)
	if what == "" {
		what, _ = e.Data["id"].(string)
	}
	verb := verbs[e.Type[strings.Index(e.Type, ".")+1:]]
	if e.Type == "trash.emptied" {
		return fmt.Sprintf("Emptied the Trash (%v items)", e.Data["count"])
	}
	return fmt.Sprintf("%s %q", verb, what)
}

func clone(t store.Tree) (store.Tree, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return t, err
	}
	var c store.Tree
	return c, json.Unmarshal(b, &c)
}

type editor struct {
	t      *store.Tree
	now    time.Time
	newID  func() string
	events []Event
}

func (e *editor) emit(kind, verb string, data map[string]any) {
	e.events = append(e.events, Event{Type: kind + "." + verb, Data: data})
}

// find returns the node with id and its parent folder (nil for the root itself).
func (e *editor) find(id string) (node, parent *store.Node) {
	var walk func(p *store.Node) bool
	walk = func(p *store.Node) bool {
		for _, c := range p.Children {
			if c.ID == id {
				node, parent = c, p
				return true
			}
			if c.Kind == store.KindFolder && walk(c) {
				return true
			}
		}
		return false
	}
	if e.t.Root.ID != "" && e.t.Root.ID == id {
		return e.t.Root, nil
	}
	walk(e.t.Root)
	return
}

// folder returns the folder named by id; "" means the root.
func (e *editor) folder(id string) (*store.Node, error) {
	if id == "" {
		return e.t.Root, nil
	}
	n, _ := e.find(id)
	if n == nil {
		return nil, refuse("no folder %q", id)
	}
	if n.Kind != store.KindFolder {
		return nil, refuse("%q is a bookmark, not a folder", id)
	}
	return n, nil
}

func (e *editor) exists(id string) bool {
	if n, _ := e.find(id); n != nil {
		return true
	}
	for _, x := range e.t.Trash {
		if x.Node.ID == id || contains(x.Node, id) {
			return true
		}
	}
	return false
}

func contains(n *store.Node, id string) bool {
	for _, c := range n.Children {
		if c.ID == id || contains(c, id) {
			return true
		}
	}
	return false
}

func checkText(field, v string) error {
	if len(v) > maxText {
		return refuse("%s is longer than %d characters", field, maxText)
	}
	if strings.ContainsAny(v, "\x00") {
		return refuse("%s contains a NUL character", field)
	}
	return nil
}

// CheckURL accepts the addresses a bookmark can hold and refuses ones that run code when opened.
func CheckURL(raw string) error {
	if raw == "" {
		return refuse("a bookmark needs a URL")
	}
	if len(raw) > 8192 {
		return refuse("the URL is longer than 8192 characters")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return refuse("%q is not a URL with a scheme (https://…)", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "javascript", "data", "vbscript", "blob":
		return refuse("%s: URLs are refused: they run code when opened", u.Scheme)
	}
	return nil
}

func at(list []*store.Node, idx *int) int {
	if idx == nil || *idx > len(list) {
		return len(list)
	}
	if *idx < 0 {
		return 0
	}
	return *idx
}

func insert(list []*store.Node, i int, n *store.Node) []*store.Node {
	list = append(list, nil)
	copy(list[i+1:], list[i:])
	list[i] = n
	return list
}

func remove(parent *store.Node, n *store.Node) int {
	for i, c := range parent.Children {
		if c == n {
			parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
			return i
		}
	}
	return -1
}

func kindOf(n *store.Node) string {
	if n.Kind == store.KindFolder {
		return "folder"
	}
	return "bookmark"
}

func (e *editor) apply(op Op) error {
	switch op.Op {
	case "create":
		return e.create(op)
	case "update":
		return e.update(op)
	case "move":
		return e.move(op)
	case "trash":
		return e.trash(op)
	case "restore":
		return e.restore(op)
	case "purge":
		return e.purge(op)
	}
	return refuse("unknown change %q (create, update, move, trash, restore or purge)", op.Op)
}

func (e *editor) create(op Op) error {
	parent, err := e.folder(op.Parent)
	if err != nil {
		return err
	}
	n := &store.Node{Kind: op.Kind, Added: e.now.Unix()}
	if op.Title != nil {
		n.Title = strings.TrimSpace(*op.Title)
	}
	if op.Note != nil {
		n.Note = *op.Note
	}
	if err := checkText("title", n.Title); err != nil {
		return err
	}
	if err := checkText("note", n.Note); err != nil {
		return err
	}
	switch op.Kind {
	case store.KindBookmark:
		if op.URL == nil {
			return refuse("a bookmark needs a URL")
		}
		n.URL = strings.TrimSpace(*op.URL)
		if err := CheckURL(n.URL); err != nil {
			return err
		}
		if n.Title == "" {
			n.Title = n.URL
		}
	case store.KindFolder:
		if op.URL != nil && *op.URL != "" {
			return refuse("a folder has no URL")
		}
		if n.Title == "" {
			return refuse("a folder needs a title")
		}
	default:
		return refuse("kind must be bookmark or folder")
	}
	for i := 0; ; i++ {
		n.ID = e.newID()
		if !e.exists(n.ID) {
			break
		}
		if i > 10 {
			return errors.New("could not make a unique id")
		}
	}
	parent.Children = insert(parent.Children, at(parent.Children, op.Index), n)
	e.emit(kindOf(n), "created", map[string]any{"id": n.ID, "parent": parent.ID, "title": n.Title, "url": n.URL})
	return nil
}

func (e *editor) update(op Op) error {
	n, parent := e.find(op.ID)
	if n == nil || parent == nil {
		return refuse("no bookmark or folder %q", op.ID)
	}
	changed := map[string]any{"id": n.ID}
	if op.Title != nil {
		v := strings.TrimSpace(*op.Title)
		if err := checkText("title", v); err != nil {
			return err
		}
		if v == "" && n.Kind == store.KindFolder {
			return refuse("a folder needs a title")
		}
		n.Title, changed["title"] = v, v
	}
	if op.Note != nil {
		if err := checkText("note", *op.Note); err != nil {
			return err
		}
		n.Note, changed["note"] = *op.Note, *op.Note
	}
	if op.URL != nil {
		if n.Kind != store.KindBookmark {
			return refuse("a folder has no URL")
		}
		v := strings.TrimSpace(*op.URL)
		if err := CheckURL(v); err != nil {
			return err
		}
		n.URL, changed["url"] = v, v
	}
	if len(changed) == 1 {
		return refuse("update %q: give a title, url or note", op.ID)
	}
	if _, ok := changed["title"]; !ok {
		changed["title"] = n.Title
	}
	e.emit(kindOf(n), "updated", changed)
	return nil
}

func (e *editor) move(op Op) error {
	if len(op.IDs) == 0 {
		return refuse("move: give ids")
	}
	target, err := e.folder(op.Parent)
	if err != nil {
		return err
	}
	idx := at(target.Children, op.Index)
	for _, id := range op.IDs {
		n, parent := e.find(id)
		if n == nil || parent == nil {
			return refuse("no bookmark or folder %q", id)
		}
		if n == target || contains(n, target.ID) {
			return refuse("cannot move %q into itself", n.Title)
		}
		from := remove(parent, n)
		if parent == target && from < idx {
			idx--
		}
		if idx > len(target.Children) {
			idx = len(target.Children)
		}
		target.Children = insert(target.Children, idx, n)
		idx++
		e.emit(kindOf(n), "moved", map[string]any{"id": n.ID, "title": n.Title, "from": parent.ID, "to": target.ID, "index": idx - 1})
	}
	return nil
}

func (e *editor) trash(op Op) error {
	if len(op.IDs) == 0 {
		return refuse("trash: give ids")
	}
	for _, id := range op.IDs {
		n, parent := e.find(id)
		if n == nil || parent == nil {
			return refuse("no bookmark or folder %q", id)
		}
		if parent == e.t.Root && n.Kind == store.KindFolder {
			return refuse("%q is a top folder of the browser; it cannot go to the Trash", n.Title)
		}
		i := remove(parent, n)
		e.t.Trash = append(e.t.Trash, &store.Trashed{Node: n, From: parent.ID, Index: i, At: e.now.Unix(), Reason: op.Reason})
		e.emit(kindOf(n), "trashed", map[string]any{"id": n.ID, "title": n.Title, "from": parent.ID, "reason": op.Reason})
	}
	return nil
}

func (e *editor) takeTrashed(id string) *store.Trashed {
	for i, x := range e.t.Trash {
		if x.Node.ID == id {
			e.t.Trash = append(e.t.Trash[:i], e.t.Trash[i+1:]...)
			return x
		}
	}
	return nil
}

func (e *editor) restore(op Op) error {
	if len(op.IDs) == 0 {
		return refuse("restore: give ids")
	}
	for _, id := range op.IDs {
		x := e.takeTrashed(id)
		if x == nil {
			return refuse("%q is not in the Trash", id)
		}
		i := x.Index
		parent, _ := e.find(x.From)
		if parent == nil || parent.Kind != store.KindFolder {
			i = -1 // its old position means nothing in another folder: it goes at the end
			parent = e.t.Root
			for _, c := range e.t.Root.Children {
				if c.Kind == store.KindFolder {
					parent = c
					break
				}
			}
		}
		pos := at(parent.Children, &i)
		if i < 0 {
			pos = len(parent.Children)
		}
		parent.Children = insert(parent.Children, pos, x.Node)
		e.emit(kindOf(x.Node), "restored", map[string]any{"id": x.Node.ID, "title": x.Node.Title, "to": parent.ID})
	}
	return nil
}

func (e *editor) purge(op Op) error {
	if len(op.IDs) == 0 {
		n := len(e.t.Trash)
		if n == 0 {
			return refuse("the Trash is empty")
		}
		e.t.Trash = nil
		e.emit("trash", "emptied", map[string]any{"count": n})
		return nil
	}
	for _, id := range op.IDs {
		x := e.takeTrashed(id)
		if x == nil {
			return refuse("%q is not in the Trash", id)
		}
		e.emit(kindOf(x.Node), "deleted", map[string]any{"id": x.Node.ID, "title": x.Node.Title})
	}
	if len(e.t.Trash) == 0 {
		e.t.Trash = nil
	}
	return nil
}
