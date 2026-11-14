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

func TestFamilyInvites(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour
	host, _ := s.CreateFamily(ctx, "hote", "h", "", now)
	other, _ := s.CreateFamily(ctx, "autre", "h", "", now)

	// At most max codes pending; another family has its own.
	first, err := s.CreateFamilyInvite(ctx, host.UserID, week, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFamilyInvite(ctx, host.UserID, week, 2, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFamilyInvite(ctx, host.UserID, week, 2, now); !errors.Is(err, ErrInviteLimit) {
		t.Fatalf("third code: %v", err)
	}
	if _, err := s.CreateFamilyInvite(ctx, other.UserID, week, 2, now); err != nil {
		t.Fatalf("other family: %v", err)
	}
	pending, _ := s.PendingInvites(ctx, host.UserID, now)
	if len(pending) != 2 || !pending[0].ExpiresAt.Equal(now.Add(week)) {
		t.Fatalf("pending = %+v", pending)
	}

	// A code used is no longer pending, and frees a place; the family it
	// made is listed, and known to the administrator.
	guest, err := s.CreateFamily(ctx, "invitee", "h", first, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.PendingInvites(ctx, host.UserID, now); len(pending) != 1 {
		t.Fatalf("pending after use = %+v", pending)
	}
	if invited, _ := s.InvitedBy(ctx, host.UserID); len(invited) != 1 || invited[0].Login != "invitee" {
		t.Fatalf("invited = %+v", invited)
	}
	if inviters, _ := s.Inviters(ctx); inviters[guest.UserID] != "hote" || len(inviters) != 1 {
		t.Fatalf("inviters = %v", inviters)
	}

	// A family revokes only its own pending codes.
	if err := s.RevokeInvite(ctx, other.UserID, pending[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke by another family: %v", err)
	}
	if err := s.RevokeInvite(ctx, host.UserID, pending[0].ID); err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.PendingInvites(ctx, host.UserID, now); len(pending) != 0 {
		t.Fatalf("pending after revoke = %+v", pending)
	}

	// Expired codes are not pending, and the purge deletes them.
	if _, err := s.CreateFamilyInvite(ctx, host.UserID, time.Hour, 2, now); err != nil {
		t.Fatal(err)
	}
	if pending, _ = s.PendingInvites(ctx, host.UserID, now.Add(2*time.Hour)); len(pending) != 0 {
		t.Fatalf("expired code pending: %+v", pending)
	}
	if n, err := s.PurgeInvites(ctx, now.Add(2*time.Hour)); err != nil || n != 1 {
		t.Fatalf("purge: %d, %v", n, err)
	}

	// The host deleted, its codes go and the families it invited stay.
	if _, err := s.CreateFamilyInvite(ctx, host.UserID, week, 2, now); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFamily(ctx, host.UserID); err != nil {
		t.Fatal(err)
	}
	var left int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invites WHERE created_by = ?`, host.UserID).Scan(&left)
	if left != 0 {
		t.Errorf("%d codes of the deleted family left", left)
	}
	if _, err := s.FamilyByLogin(ctx, "invitee"); err != nil {
		t.Errorf("invited family: %v", err)
	}
	if inviters, _ := s.Inviters(ctx); len(inviters) != 0 {
		t.Errorf("inviters = %v", inviters)
	}
}
