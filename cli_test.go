package main

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"leblanc.io/open-go-base/appconf"
	"leblanc.io/open-go-base/authx"

	"leblanc.io/open-go-menu/internal/config"
	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/store"
)

func TestCommandArgs(t *testing.T) {
	if got := commandArgs([]string{"-c", "x.yaml", "purge", "-dry-run"}); strings.Join(got, " ") != "purge -dry-run" {
		t.Errorf("got %v", got)
	}
	if got := commandArgs(nil); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestCommands(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{
		Auth:     appconf.Auth{BcryptCost: 4},
		Families: config.Families{DataDir: filepath.Join(dir, "data"), Signup: config.SignupInvite, InactiveMonths: 12},
	}
	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := command(cfg, args, &out); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}
	password := regexp.MustCompile(`password[^:]*: (\S+)`)

	// A single-family database, imported as a family (EF-31).
	old := filepath.Join(dir, "open-go-menu.db")
	st, err := store.Open(ctx, "file:"+old)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.CreateItem(ctx, store.NewItem(planner.Dish, "Lasagnes"))
	st.Close()
	out := run("create-family", "-from", old, "Dupont")
	m := password.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no password in %q", out)
	}

	accts, fams, err := openData(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fam, err := accts.FamilyByLogin(ctx, "dupont")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := accts.Users.GetByID(fam.UserID)
	if authx.CheckPassword(u.PasswordHash, m[1]) != nil {
		t.Error("printed password refused")
	}
	imported, release, err := fams.Acquire(ctx, fam.File)
	if err != nil {
		t.Fatal(err)
	}
	if items, _ := imported.Items(ctx, planner.Dish); len(items) != 1 || items[0].Name != "Lasagnes" {
		t.Fatalf("imported items = %+v", items)
	}
	release()
	accts.Close()
	fams.Close()

	if err := command(cfg, []string{"create-family", "du pont"}, &bytes.Buffer{}); err == nil {
		t.Error("bad login accepted")
	}
	if err := command(cfg, []string{"create-family", "dupont"}, &bytes.Buffer{}); err == nil {
		t.Error("login taken accepted")
	}

	out = run("reset-password", "dupont")
	if m2 := password.FindStringSubmatch(out); m2 == nil || m2[1] == m[1] {
		t.Fatalf("reset: %q", out)
	}
	if out = run("invite", "-days", "7"); !regexp.MustCompile(`[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}`).MatchString(out) {
		t.Errorf("invite: %q", out)
	}
	// A family last seen 13 months ago.
	accts, fams, _ = openData(cfg)
	if _, err := accts.CreateFamily(ctx, "martin", "h", "", time.Now().AddDate(-1, -1, 0)); err != nil {
		t.Fatal(err)
	}
	accts.Close()
	fams.Close()
	if out = run("families"); !strings.Contains(out, "dupont") || !strings.Contains(out, "martin") {
		t.Errorf("families: %q", out)
	}

	// Purge: only the family without a visit for a year goes (EF-30).
	if out = run("purge", "-dry-run"); !strings.Contains(out, "would delete martin") {
		t.Errorf("dry run: %q", out)
	}
	if out = run("purge"); !strings.Contains(out, "deleted martin") || strings.Contains(out, "dupont") {
		t.Errorf("purge: %q", out)
	}
	if out = run("families"); strings.Contains(out, "martin") || !strings.Contains(out, "dupont") {
		t.Errorf("after purge: %q", out)
	}
}
