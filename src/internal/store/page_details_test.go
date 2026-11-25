package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPageDetailsFillOnlyWhatIsMissing(t *testing.T) {
	ctx := context.Background()
	s, err := Open("json", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	checked := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	kept := &PageMeta{Title: "Kept"}
	_ = s.SaveLinks(ctx, []LinkResult{
		{URL: "https://a.example/", Status: "ok", Checked: checked},
		{URL: "https://b.example/", Status: "dead", Checked: checked, Meta: kept, Final: "https://b.example/old"},
	})
	file := `{"u":"https://a.example/","code":200,"final":"https://a.example/home","title":"A","description":"About A","text":"…"}
{"u":"https://b.example/","code":200,"final":"https://b.example/new","title":"B","description":"","text":""}

{"u":"https://never.example/","code":200,"final":"","title":"N","description":"","text":""}
`
	res, err := AddPageDetails(ctx, s, strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if res != (PageDetails{Rows: 3, Added: 1, Unchecked: 1}) {
		t.Fatalf("%+v", res)
	}
	got, _ := s.Links(ctx, nil)
	if a := got["https://a.example/"]; a.Status != "ok" || !a.Checked.Equal(checked) || a.Final != "https://a.example/home" || a.Meta == nil || a.Meta.Title != "A" || a.Meta.Description != "About A" {
		t.Fatalf("a: %+v %+v", a, a.Meta)
	}
	if b := got["https://b.example/"]; b.Status != "dead" || b.Final != "https://b.example/old" || b.Meta.Title != "Kept" {
		t.Fatalf("b changed: %+v %+v", b, b.Meta)
	}
	if _, ok := got["https://never.example/"]; ok {
		t.Fatal("a result was made for a URL never checked")
	}
	if _, err := AddPageDetails(ctx, s, strings.NewReader("{not json\n")); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("%v", err)
	}
}
