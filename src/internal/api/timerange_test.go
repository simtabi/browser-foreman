package api

import (
	"net/url"
	"testing"
	"time"
)

func TestARangeToTheMinuteKeepsThatWholeMinute(t *testing.T) {
	from, to, set, err := timeRange(url.Values{"from": {"2026-11-13T09:00"}, "to": {"2026-11-13T09:30"}})
	if err != nil || !set {
		t.Fatal(set, err)
	}
	visit := time.Date(2026, 11, 13, 9, 30, 40, 0, time.Local)
	if visit.After(to) || from.After(time.Date(2026, 11, 13, 9, 0, 0, 0, time.Local)) {
		t.Fatalf("from %v to %v leave out a visit at 09:30:40", from, to)
	}
	if !time.Date(2026, 11, 13, 9, 31, 0, 0, time.Local).After(to) {
		t.Fatal("the next minute is inside the range")
	}
	_, exact, _, _ := timeRange(url.Values{"to": {"2026-11-13T09:30:00Z"}})
	if !exact.Equal(time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("an exact time was widened: %v", exact)
	}
}
