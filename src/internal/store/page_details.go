package store

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// PageDetails is what adding the Python tool's page details did: Added results gained a title, description or final
// address; Unchecked rows named a URL with no link result, which is left alone (the file says nothing about when).
type PageDetails struct{ Rows, Added, Unchecked int }

// AddPageDetails reads the Python tool's thumbs/_meta.jsonl (one JSON object per line: u, final, title, description)
// and fills in what each URL's link result is missing. It never changes a status or replaces a value already there.
func AddPageDetails(ctx context.Context, links Links, r io.Reader) (PageDetails, error) {
	type row struct{ U, Final, Title, Description string }
	var rows []row
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // a row carries the page's text, which can be long
	for line := 1; sc.Scan(); line++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var x row
		if err := json.Unmarshal(sc.Bytes(), &x); err != nil {
			return PageDetails{}, fmt.Errorf("line %d: %w", line, err)
		}
		if x.U != "" {
			rows = append(rows, x)
		}
	}
	if err := sc.Err(); err != nil {
		return PageDetails{}, err
	}
	urls := make([]string, len(rows))
	for i, x := range rows {
		urls[i] = x.U
	}
	have, err := links.Links(ctx, urls)
	if err != nil {
		return PageDetails{}, err
	}
	res := PageDetails{Rows: len(rows)}
	var changed []LinkResult
	for _, x := range rows {
		l, ok := have[x.U]
		if !ok {
			res.Unchecked++
			continue
		}
		before := l
		if l.Meta == nil && (x.Title != "" || x.Description != "") {
			l.Meta = &PageMeta{Title: x.Title, Description: x.Description}
		}
		if l.Final == "" && x.Final != "" && x.Final != x.U {
			l.Final = x.Final
		}
		if l.Meta != before.Meta || l.Final != before.Final {
			have[x.U] = l
			changed = append(changed, l)
			res.Added++
		}
	}
	return res, links.SaveLinks(ctx, changed)
}
