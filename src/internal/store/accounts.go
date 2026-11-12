package store

import (
	"context"
	"time"
)

// Roles, from least to most: a viewer reads, an editor changes bookmarks and tabs, an owner also manages people,
// tokens and webhooks.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleOwner  = "owner"
)

// RoleRank orders roles so a check can ask "at least editor"; an unknown role ranks below viewer.
func RoleRank(role string) int {
	switch role {
	case RoleViewer:
		return 1
	case RoleEditor:
		return 2
	case RoleOwner:
		return 3
	}
	return 0
}

// User is one person who can sign in. PasswordHash is an argon2id PHC string (internal/auth); it never leaves the
// server.
type User struct {
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"password_hash"`
	Disabled     bool      `json:"disabled"`
	Created      time.Time `json:"created"`
}

// Token is an API token. Only its SHA-256 is kept: the token itself is shown once, when it is made.
type Token struct {
	ID       string    `json:"id"`
	User     string    `json:"user"`
	Name     string    `json:"name"`
	Hash     string    `json:"hash"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used,omitempty"`
}

// Session is a signed-in browser. Only the SHA-256 of its cookie value is kept, so a copy of the data cannot be replayed.
type Session struct {
	Hash    string    `json:"hash"`
	User    string    `json:"user"`
	CSRF    string    `json:"csrf"` // sent back in X-CSRF-Token on every change made with this session
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
}

// Accounts is the part of a Store that keeps people, tokens and sessions.
type Accounts interface {
	CreateUser(ctx context.Context, u User) error
	User(ctx context.Context, name string) (User, error)
	Users(ctx context.Context) ([]User, error)
	UpdateUser(ctx context.Context, u User) error
	DeleteUser(ctx context.Context, name string) error // also ends its sessions and revokes its tokens

	CreateToken(ctx context.Context, t Token) error
	TokenByHash(ctx context.Context, hash string) (Token, error)
	Tokens(ctx context.Context, user string) ([]Token, error) // "" for everyone's
	TouchToken(ctx context.Context, id string, at time.Time) error
	RevokeToken(ctx context.Context, id string) error

	CreateSession(ctx context.Context, s Session) error
	SessionByHash(ctx context.Context, hash string, now time.Time) (Session, error) // expired: ErrNotFound
	DeleteSession(ctx context.Context, hash string) error
}
