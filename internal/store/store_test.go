package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"leblanc.io/open-menu/internal/planner"
)

func open(t *testing.T) *Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Re-running the migrations must be a no-op.
	if err := s.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestItemLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	a, err := s.CreateItem(ctx, NewItem(planner.Dish, "Lasagnes"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateItem(ctx, NewItem(planner.Dish, "Gratin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateItem(ctx, NewItem(planner.Dish, "lasagnes")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate: err = %v", err)
	}

	// Swapping names in one save must work.
	err = s.UpdateItems(ctx, planner.Dish, []Item{
		{ID: a, Name: "Gratin", Weight: 2, Seasonal: [4]int{1, 0, 3, 4}},
		{ID: b, Name: "Lasagnes", Weight: 5, Seasonal: [4]int{5, 5, 5, 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := s.Items(ctx, planner.Dish)
	if len(items) != 2 || items[0].ID != a || items[0].Seasonal[planner.Summer] != 0 {
		t.Fatalf("items = %+v", items)
	}

	if err := s.UpdateItems(ctx, planner.Dish, []Item{{ID: a, Name: "lasagnes", Weight: 1}}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("update duplicate: err = %v", err)
	}

	// A planned item, once deleted, leaves the list but not the plan.
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	slot := planner.Slot{Day: day, Meal: planner.Dinner}
	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{slot: {planner.Dish: a}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v", err)
	}
	if _, err := s.Item(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted item still found: %v", err)
	}
	items, _ = s.Items(ctx, planner.Dish)
	if len(items) != 1 {
		t.Fatalf("after delete: %+v", items)
	}
	entries, err := s.Entries(ctx, day, day)
	if err != nil {
		t.Fatal(err)
	}
	if entries[slot].Names[planner.Dish] != "Gratin" {
		t.Fatalf("entries = %+v", entries)
	}
	// The name of a deleted item is free again.
	if _, err := s.CreateItem(ctx, NewItem(planner.Dish, "Gratin")); err != nil {
		t.Fatal(err)
	}
}

func TestNamesUniquePerKind(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	rice, err := s.CreateItem(ctx, NewItem(planner.Starch, "Riz"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateItem(ctx, NewItem(planner.Dish, "Riz")); err != nil {
		t.Fatalf("TR-12: same name in another list refused: %v", err)
	}
	if _, err := s.CreateItem(ctx, NewItem(planner.Starch, "riz")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("TR-12: duplicate starch accepted: %v", err)
	}
	// Saving the dish list never touches a starch.
	if err := s.UpdateItems(ctx, planner.Dish, []Item{{ID: rice, Name: "Pâtes", Weight: 1}}); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item(ctx, rice); it.Name != "Riz" || it.Kind != planner.Starch {
		t.Fatalf("starch changed: %+v", it)
	}
	all, _ := s.AllItems(ctx)
	if len(all[planner.Starch]) != 1 || len(all[planner.Dish]) != 1 {
		t.Fatalf("all = %+v", all)
	}
}

func TestPlates(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	soup, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Soupe"))
	rice, _ := s.CreateItem(ctx, NewItem(planner.Starch, "Riz"))
	fish, _ := s.CreateItem(ctx, NewItem(planner.Protein, "Saumon"))
	beans, _ := s.CreateItem(ctx, NewItem(planner.Vegetable, "Haricots verts"))
	d1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	lunch := planner.Slot{Day: d1, Meal: planner.Lunch}

	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{lunch: {planner.Dish: soup}}); err != nil {
		t.Fatal(err)
	}
	composed := planner.Plate{planner.Starch: rice, planner.Protein: fish, planner.Vegetable: beans}
	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{lunch: composed}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Entries(ctx, d1, d1.AddDate(0, 0, 6))
	if e := entries[lunch]; len(entries) != 1 || e.Plate != composed || e.Names[planner.Vegetable] != "Haricots verts" {
		t.Fatalf("a composed plate must replace the dish: %+v", entries)
	}
	if err := s.ClearEntries(ctx, lunch); err != nil {
		t.Fatal(err)
	}
	if entries, _ = s.Entries(ctx, d1, d1.AddDate(0, 0, 6)); len(entries) != 0 {
		t.Fatalf("after clear: %+v", entries)
	}
}

// TestMigrationKeepsV1Data plays 001, writes data the v1 way, then lets Open
// apply 002.
func TestMigrationKeepsV1Data(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := migrations.ReadFile("migrations/001_init.sql")
	for _, q := range []string{
		string(v1),
		`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations VALUES ('migrations/001_init.sql', '2026-10-08T00:00:00Z')`,
		`INSERT INTO dishes (id, name, weight_summer, created_at) VALUES (7, 'Raclette', 0, '2026-10-08T00:00:00Z')`,
		`INSERT INTO plan_entries VALUES ('2026-10-08', 'dinner', 7)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(ctx, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	it, err := s.Item(ctx, 7)
	if err != nil || it.Kind != planner.Dish || it.Name != "Raclette" || it.Seasonal[planner.Summer] != 0 {
		t.Fatalf("item = %+v, %v", it, err)
	}
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	entries, _ := s.Entries(ctx, day, day)
	if e := entries[planner.Slot{Day: day, Meal: planner.Dinner}]; e.Plate[planner.Dish] != 7 {
		t.Fatalf("plan lost: %+v", entries)
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if d, _ := s.WeekStart(ctx); d != time.Monday {
		t.Errorf("default week start = %v", d)
	}
	if m, _ := s.Meals(ctx); len(m) != 2 {
		t.Errorf("default meals = %v", m)
	}
	if n, _ := s.ComposedShare(ctx); n != DefaultComposedShare {
		t.Errorf("default share = %d", n)
	}
	_ = s.SetWeekStart(ctx, time.Saturday)
	_ = s.SetMeals(ctx, []planner.Meal{planner.Dinner})
	_ = s.SetComposedShare(ctx, 80)
	if d, _ := s.WeekStart(ctx); d != time.Saturday {
		t.Errorf("week start = %v", d)
	}
	if m, _ := s.Meals(ctx); len(m) != 1 || m[0] != planner.Dinner {
		t.Errorf("meals = %v", m)
	}
	if n, _ := s.ComposedShare(ctx); n != 80 {
		t.Errorf("share = %d", n)
	}
}
