package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The json driver keeps accounts in three files under <dir>/json/accounts: users.json, tokens.json, sessions.json,
// each an object keyed by name, id or hash. They are 0600 like everything else here.

func (s *JSONStore) accountsFile(name string) string {
	return filepath.Join(s.root, "accounts", name+".json")
}

func readMap[T any](path string) (map[string]T, error) {
	out := map[string]T{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(b, &out)
}

func writeMap[T any](path string, m map[string]T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

func (s *JSONStore) CreateUser(_ context.Context, u User) error {
	if err := ValidID(u.Name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[User](s.accountsFile("users"))
	if err != nil {
		return err
	}
	if _, ok := m[u.Name]; ok {
		return fmt.Errorf("user %q already exists", u.Name)
	}
	m[u.Name] = u
	return writeMap(s.accountsFile("users"), m)
}

func (s *JSONStore) User(_ context.Context, name string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[User](s.accountsFile("users"))
	if err != nil {
		return User{}, err
	}
	u, ok := m[name]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (s *JSONStore) Users(_ context.Context) ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[User](s.accountsFile("users"))
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(m))
	for _, u := range m {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *JSONStore) UpdateUser(_ context.Context, u User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[User](s.accountsFile("users"))
	if err != nil {
		return err
	}
	if _, ok := m[u.Name]; !ok {
		return ErrNotFound
	}
	m[u.Name] = u
	return writeMap(s.accountsFile("users"), m)
}

func (s *JSONStore) DeleteUser(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	users, err := readMap[User](s.accountsFile("users"))
	if err != nil {
		return err
	}
	if _, ok := users[name]; !ok {
		return ErrNotFound
	}
	delete(users, name)
	tokens, err := readMap[Token](s.accountsFile("tokens"))
	if err != nil {
		return err
	}
	for k, t := range tokens {
		if t.User == name {
			delete(tokens, k)
		}
	}
	sessions, err := readMap[Session](s.accountsFile("sessions"))
	if err != nil {
		return err
	}
	for k, x := range sessions {
		if x.User == name {
			delete(sessions, k)
		}
	}
	if err := writeMap(s.accountsFile("tokens"), tokens); err != nil {
		return err
	}
	if err := writeMap(s.accountsFile("sessions"), sessions); err != nil {
		return err
	}
	return writeMap(s.accountsFile("users"), users)
}

func (s *JSONStore) CreateToken(_ context.Context, t Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Token](s.accountsFile("tokens"))
	if err != nil {
		return err
	}
	if _, ok := m[t.ID]; ok {
		return fmt.Errorf("token %q already exists", t.ID)
	}
	m[t.ID] = t
	return writeMap(s.accountsFile("tokens"), m)
}

func (s *JSONStore) TokenByHash(_ context.Context, hash string) (Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Token](s.accountsFile("tokens"))
	if err != nil {
		return Token{}, err
	}
	for _, t := range m {
		if t.Hash == hash {
			return t, nil
		}
	}
	return Token{}, ErrNotFound
}

func (s *JSONStore) Tokens(_ context.Context, user string) ([]Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Token](s.accountsFile("tokens"))
	if err != nil {
		return nil, err
	}
	out := []Token{}
	for _, t := range m {
		if user == "" || t.User == user {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}

func (s *JSONStore) TouchToken(_ context.Context, id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Token](s.accountsFile("tokens"))
	if err != nil {
		return err
	}
	t, ok := m[id]
	if !ok {
		return ErrNotFound
	}
	t.LastUsed = at.UTC()
	m[id] = t
	return writeMap(s.accountsFile("tokens"), m)
}

func (s *JSONStore) RevokeToken(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Token](s.accountsFile("tokens"))
	if err != nil {
		return err
	}
	if _, ok := m[id]; !ok {
		return ErrNotFound
	}
	delete(m, id)
	return writeMap(s.accountsFile("tokens"), m)
}

func (s *JSONStore) CreateSession(_ context.Context, x Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Session](s.accountsFile("sessions"))
	if err != nil {
		return err
	}
	now := time.Now()
	for k, old := range m { // expired sessions go when a new one is made
		if old.Expires.Before(now) {
			delete(m, k)
		}
	}
	m[x.Hash] = x
	return writeMap(s.accountsFile("sessions"), m)
}

func (s *JSONStore) SessionByHash(_ context.Context, hash string, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Session](s.accountsFile("sessions"))
	if err != nil {
		return Session{}, err
	}
	x, ok := m[hash]
	if !ok || !x.Expires.After(now) {
		return Session{}, ErrNotFound
	}
	return x, nil
}

func (s *JSONStore) DeleteSession(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := readMap[Session](s.accountsFile("sessions"))
	if err != nil {
		return err
	}
	delete(m, hash)
	return writeMap(s.accountsFile("sessions"), m)
}
