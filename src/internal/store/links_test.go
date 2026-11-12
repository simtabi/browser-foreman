package store

import (
	"context"
	"fmt"
	"testing"
)

func TestLinkResults(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if got, _ := s.Links(ctx, []string{"https://a.example/"}); len(got) != 0 {
			t.Fatal("results before any check")
		}
		_ = s.SaveLinks(ctx, []LinkResult{{URL: "https://a.example/", Status: "ok", Code: 200, Checked: t0}, {URL: "https://b.example/", Status: "dead", Kind: "host", Checked: t0}})
		_ = s.SaveLinks(ctx, []LinkResult{{URL: "https://a.example/", Status: "moved", Final: "https://a.example/new", Checked: t0}})
		got, err := s.Links(ctx, []string{"https://a.example/", "https://nope.example/"})
		if err != nil || len(got) != 1 || got["https://a.example/"].Status != "moved" || got["https://a.example/"].Final != "https://a.example/new" || !got["https://a.example/"].Checked.Equal(t0) {
			t.Fatalf("%+v %v", got, err)
		}
		all, _ := s.Links(ctx, nil)
		if len(all) != 2 || all["https://b.example/"].Kind != "host" {
			t.Fatalf("%+v", all)
		}
		// More addresses than one query may bind.
		many := make([]LinkResult, 1200)
		urls := make([]string, 1200)
		for i := range many {
			urls[i] = fmt.Sprintf("https://x.example/%d", i)
			many[i] = LinkResult{URL: urls[i], Status: "ok", Checked: t0}
		}
		_ = s.SaveLinks(ctx, many)
		if got, err := s.Links(ctx, urls); err != nil || len(got) != 1200 {
			t.Fatalf("%d %v", len(got), err)
		}
	})
}
