package webhooks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Log keeps the latest deliveries in deliveries.jsonl in the state folder, one JSON line each, so the owner can see
// what each receiver answered. It holds at most Keep records: when the file reaches twice that, it is cut back.
type Log struct {
	mu   sync.Mutex
	path string
	Keep int
}

func NewLog(dir string) *Log { return &Log{path: filepath.Join(dir, "deliveries.jsonl"), Keep: 1000} }

// Add records one delivery. Errors are returned for the caller to report; a failed write loses only that record.
func (l *Log) Add(d Delivery) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(d)
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	all, err := l.read()
	if err == nil && len(all) >= 2*l.Keep {
		err = l.rewrite(all[len(all)-l.Keep:])
	}
	return err
}

func (l *Log) read() ([]Delivery, error) {
	b, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Delivery
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var d Delivery
		if json.Unmarshal(sc.Bytes(), &d) == nil {
			out = append(out, d)
		}
	}
	return out, sc.Err()
}

func (l *Log) rewrite(keep []Delivery) error {
	var buf bytes.Buffer
	for _, d := range keep {
		b, _ := json.Marshal(d)
		buf.Write(append(b, '\n'))
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// Recent returns up to limit deliveries, newest first, for one subscription or ("") all.
func (l *Log) Recent(subscription string, limit int) ([]Delivery, error) {
	l.mu.Lock()
	all, err := l.read()
	l.mu.Unlock()
	out := []Delivery{}
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		if subscription == "" || all[i].Subscription == subscription {
			out = append(out, all[i])
		}
	}
	return out, err
}
