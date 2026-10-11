package accounts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"leblanc.io/open-go-base/authx"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Re-running the migrations must be a no-op.
	if err := migrate(context.Background(), s.db); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFamilyLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	hash, _ := authx.HashPassword("un mot de passe", 4)

	f, err := s.CreateFamily(ctx, "dupont", hash, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.File) != 32 {
		t.Fatalf("file = %q", f.File)
	}
	if _, err := s.CreateFamily(ctx, "dupont", hash, "", now); !errors.Is(err, ErrLoginTaken) {
		t.Fatalf("login twice: err = %v", err)
	}
	got, err := s.FamilyByLogin(ctx, "dupont")
	if err != nil || got.UserID != f.UserID || got.File != f.File || !got.LastActive.Equal(now) {
		t.Fatalf("by login = %+v, %v", got, err)
	}
	// authx finds the login as its "email".
	if u, err := s.Users.GetByEmail("dupont"); err != nil || u.ID != f.UserID {
		t.Fatalf("authx user = %+v, %v", u, err)
	}
	if ok, _ := s.CheckPassword(f.UserID, "un mot de passe"); !ok {
		t.Error("password refused")
	}

	// A visit is recorded at most once an hour.
	_ = s.Touch(ctx, f.UserID, now.Add(30*time.Minute))
	if got, _ = s.FamilyOf(ctx, f.UserID); !got.LastActive.Equal(now) {
		t.Errorf("touched within the hour: %v", got.LastActive)
	}
	_ = s.Touch(ctx, f.UserID, now.Add(2*time.Hour))
	if got, _ = s.FamilyOf(ctx, f.UserID); !got.LastActive.Equal(now.Add(2 * time.Hour)) {
		t.Errorf("not touched: %v", got.LastActive)
	}
	other, _ := s.CreateFamily(ctx, "martin", hash, "", now.AddDate(-2, 0, 0))
	inactive, _ := s.Inactive(ctx, now.AddDate(-1, 0, 0))
	if len(inactive) != 1 || inactive[0].UserID != other.UserID {
		t.Fatalf("inactive = %+v", inactive)
	}

	// A new password closes the sessions.
	_ = s.Sessions.Create(&authx.Session{Token: "t", UserID: f.UserID, Purpose: authx.PurposeAuth, ExpiresAt: now.Add(time.Hour)})
	newHash, _ := authx.HashPassword("un autre mot de passe", 4)
	if err := s.SetPassword(ctx, f.UserID, newHash); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sessions.Get("t"); !errors.Is(err, authx.ErrSessionNotFound) {
		t.Errorf("session kept: %v", err)
	}
	if ok, _ := s.CheckPassword(f.UserID, "un autre mot de passe"); !ok {
		t.Error("new password refused")
	}

	if err := s.DeleteFamily(ctx, f.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FamilyOf(ctx, f.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted family: %v", err)
	}
	// The login is free again.
	if _, err := s.CreateFamily(ctx, "dupont", hash, "", now); err != nil {
		t.Fatal(err)
	}
}

func TestInvites(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	code, err := s.CreateInvite(ctx, 24*time.Hour, now)
	if err != nil || len(code) != 14 {
		t.Fatalf("code = %q, %v", code, err)
	}
	if _, err := s.CreateFamily(ctx, "a", "h", "WRONG-CODE", now); !errors.Is(err, ErrInvite) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := s.CheckInvite(ctx, "WRONG-CODE", now); !errors.Is(err, ErrInvite) {
		t.Fatalf("check wrong code: %v", err)
	}
	// Checking does not consume.
	for range 2 {
		if err := s.CheckInvite(ctx, code, now); err != nil {
			t.Fatalf("check valid code: %v", err)
		}
	}
	// Typed loosely: lower case, without dashes.
	if _, err := s.CreateFamily(ctx, "a", "h", " "+NormalizeInvite(code)[:4]+" "+lower(NormalizeInvite(code)[4:]), now); err != nil {
		t.Fatalf("valid code: %v", err)
	}
	if _, err := s.CreateFamily(ctx, "b", "h", code, now); !errors.Is(err, ErrInvite) {
		t.Fatalf("code used twice: %v", err)
	}
	if err := s.CheckInvite(ctx, code, now); !errors.Is(err, ErrInvite) {
		t.Fatalf("check used code: %v", err)
	}
	late, _ := s.CreateInvite(ctx, time.Hour, now)
	if _, err := s.CreateFamily(ctx, "c", "h", late, now.Add(2*time.Hour)); !errors.Is(err, ErrInvite) {
		t.Fatalf("expired code: %v", err)
	}
	if err := s.CheckInvite(ctx, late, now.Add(2*time.Hour)); !errors.Is(err, ErrInvite) {
		t.Fatalf("check expired code: %v", err)
	}
	// A refused invitation creates nothing.
	if _, err := s.FamilyByLogin(ctx, "c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("family created with a bad code: %v", err)
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
