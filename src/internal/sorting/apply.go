package sorting

import (
	"encoding/json"
	"strings"

	"github.com/simtabi/browser-foreman/src/internal/store"
)

// Apply returns a copy of t with the plan's moves made: each bookmark goes to the end of its new folder, folders
// that do not exist yet are made (newID names them), and folders left empty stay, as the owner made them.
func Apply(t store.Tree, p Plan, newID func() string) (store.Tree, error) {
	var out store.Tree
	b, err := json.Marshal(t)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, err
	}
	if out.Root == nil {
		out.Root = &store.Node{ID: newID(), Kind: store.KindFolder}
	}
	folders := map[string]*store.Node{"": out.Root}
	parent := map[string]*store.Node{}
	var walk func(n *store.Node, path string)
	walk = func(n *store.Node, path string) {
		for _, c := range n.Children {
			parent[c.ID] = n
			if c.Kind == store.KindFolder {
				p := strings.TrimPrefix(path+"/"+c.Title, "/")
				if _, dup := folders[p]; !dup { // two folders of one name: the first holds what moves in
					folders[p] = c
				}
				walk(c, p)
			}
		}
	}
	walk(out.Root, "")
	var ensure func(path string) *store.Node
	ensure = func(path string) *store.Node {
		if f, ok := folders[path]; ok {
			return f
		}
		up, name := "", path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			up, name = path[:i], path[i+1:]
		}
		p := ensure(up)
		f := &store.Node{ID: newID(), Kind: store.KindFolder, Title: name}
		p.Children = append(p.Children, f)
		folders[path] = f
		return f
	}
	for _, m := range p.Moves {
		from := parent[m.ID]
		if from == nil {
			continue // gone since the plan was made
		}
		var n *store.Node
		for i, c := range from.Children {
			if c.ID == m.ID {
				n = c
				from.Children = append(from.Children[:i:i], from.Children[i+1:]...)
				break
			}
		}
		to := ensure(m.To)
		to.Children = append(to.Children, n)
		parent[m.ID] = to
	}
	return out, out.Validate()
}
