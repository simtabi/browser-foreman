package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUsersTokensSessions(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		if err := s.CreateUser(ctx, User{Name: "ada", Role: RoleOwner, PasswordHash: "h1", Created: t0}); err != nil {
			t.Fatal(err)
		}
		if err := s.CreateUser(ctx, User{Name: "ada", Role: RoleViewer, PasswordHash: "h2", Created: t0}); err == nil {
			t.Fatal("a second user with the same name")
		}
		if err := s.CreateUser(ctx, User{Name: "../x", Role: RoleViewer}); err == nil {
			t.Fatal("a path in a name was accepted")
		}
		_ = s.CreateUser(ctx, User{Name: "bob", Role: RoleViewer, PasswordHash: "h3", Created: t0})
		u, _ := s.User(ctx, "ada")
		u.Role, u.Disabled = RoleEditor, true
		if err := s.UpdateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.User(ctx, "ada"); got.Role != RoleEditor || !got.Disabled || got.PasswordHash != "h1" {
			t.Fatalf("%+v", got)
		}
		if all, _ := s.Users(ctx); len(all) != 2 || all[0].Name != "ada" {
			t.Fatalf("%+v", all)
		}
		if err := s.CreateToken(ctx, Token{ID: "tk1", User: "ada", Name: "cli", Hash: "hash-a", Created: t0}); err != nil {
			t.Fatal(err)
		}
		_ = s.CreateToken(ctx, Token{ID: "tk2", User: "bob", Name: "x", Hash: "hash-b", Created: t0.Add(time.Second)})
		if tk, err := s.TokenByHash(ctx, "hash-a"); err != nil || tk.ID != "tk1" {
			t.Fatalf("%+v %v", tk, err)
		}
		_ = s.TouchToken(ctx, "tk1", t0.Add(time.Hour))
		if tk, _ := s.TokenByHash(ctx, "hash-a"); !tk.LastUsed.Equal(t0.Add(time.Hour)) {
			t.Fatalf("last used %v", tk.LastUsed)
		}
		if mine, _ := s.Tokens(ctx, "ada"); len(mine) != 1 {
			t.Fatalf("ada's tokens %+v", mine)
		}
		if all, _ := s.Tokens(ctx, ""); len(all) != 2 {
			t.Fatalf("all tokens %+v", all)
		}
		if err := s.CreateSession(ctx, Session{Hash: "sess-a", User: "ada", CSRF: "c", Created: t0, Expires: t0.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SessionByHash(ctx, "sess-a", t0.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SessionByHash(ctx, "sess-a", t0.Add(2*time.Hour)); !errors.Is(err, ErrNotFound) {
			t.Fatal("an expired session was accepted")
		}
		if err := s.RevokeToken(ctx, "tk2"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.TokenByHash(ctx, "hash-b"); !errors.Is(err, ErrNotFound) {
			t.Fatal("a revoked token still works")
		}
		if err := s.DeleteUser(ctx, "ada"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.TokenByHash(ctx, "hash-a"); !errors.Is(err, ErrNotFound) {
			t.Fatal("a deleted user's token still works")
		}
		if _, err := s.SessionByHash(ctx, "sess-a", t0.Add(time.Minute)); !errors.Is(err, ErrNotFound) {
			t.Fatal("a deleted user's session still works")
		}
		if err := s.DeleteUser(ctx, "ada"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	})
}

func TestMigrateCarriesPeopleNotSessions(t *testing.T) {
	ctx := context.Background()
	src, dst := open(t, "json"), open(t, "sqlite")
	_ = src.CreateUser(ctx, User{Name: "ada", Role: RoleOwner, PasswordHash: "h", Created: t0})
	_ = src.CreateToken(ctx, Token{ID: "tk1", User: "ada", Name: "cli", Hash: "hash-a", Created: t0})
	_ = src.CreateSession(ctx, Session{Hash: "s", User: "ada", CSRF: "c", Created: t0, Expires: time.Now().Add(time.Hour)})
	if _, _, err := Migrate(ctx, src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := dst.TokenByHash(ctx, "hash-a"); err != nil {
		t.Fatal("token not carried")
	}
	if _, err := dst.SessionByHash(ctx, "s", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatal("sessions must not move")
	}
}

func TestDeleteSessionsKeepsOne(t *testing.T) {
	each(t, func(t *testing.T, s Store) {
		ctx := context.Background()
		for _, n := range []string{"ada", "bob"} {
			if err := s.CreateUser(ctx, User{Name: n, Role: RoleOwner, PasswordHash: "x", Created: t0}); err != nil {
				t.Fatal(err)
			}
		}
		later := time.Now().Add(time.Hour)
		for _, x := range []Session{{Hash: "a1", User: "ada"}, {Hash: "a2", User: "ada"}, {Hash: "b1", User: "bob"}} {
			x.Created, x.Expires = time.Now(), later
			if err := s.CreateSession(ctx, x); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.DeleteSessions(ctx, "ada", "a2"); err != nil {
			t.Fatal(err)
		}
		_, e1 := s.SessionByHash(ctx, "a1", time.Now())
		_, e2 := s.SessionByHash(ctx, "a2", time.Now())
		_, e3 := s.SessionByHash(ctx, "b1", time.Now())
		if !errors.Is(e1, ErrNotFound) || e2 != nil || e3 != nil {
			t.Fatalf("%v %v %v", e1, e2, e3)
		}
	})
}
