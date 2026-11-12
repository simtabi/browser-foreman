package webhooks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(dir)
	l.Keep = 5
	for i := 1; i <= 12; i++ {
		sub := "wh_a"
		if i%2 == 0 {
			sub = "wh_b"
		}
		if err := l.Add(Delivery{Subscription: sub, Event: uint64(i), OK: i%3 != 0}); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := l.Recent("", 100)
	if len(all) < 5 || len(all) >= 10 || all[0].Event != 12 {
		t.Fatalf("kept %d, newest %d", len(all), all[0].Event)
	}
	b, _ := l.Recent("wh_b", 2)
	if len(b) != 2 || b[0].Event != 12 || b[1].Event != 10 {
		t.Fatalf("%+v", b)
	}
	fi, _ := os.Stat(filepath.Join(dir, "deliveries.jsonl"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}
