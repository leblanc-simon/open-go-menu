package store

import (
	"context"
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

func TestDishLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	a, err := s.CreateDish(ctx, NewDish("Lasagnes"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateDish(ctx, NewDish("Gratin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDish(ctx, NewDish("lasagnes")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate: err = %v", err)
	}

	// Swapping names in one save must work.
	err = s.UpdateDishes(ctx, []Dish{
		{ID: a, Name: "Gratin", Weight: 2, Seasonal: [4]int{1, 0, 3, 4}},
		{ID: b, Name: "Lasagnes", Weight: 5, Seasonal: [4]int{5, 5, 5, 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	dishes, _ := s.Dishes(ctx)
	if len(dishes) != 2 || dishes[0].ID != a || dishes[0].Seasonal[planner.Summer] != 0 {
		t.Fatalf("dishes = %+v", dishes)
	}

	if err := s.UpdateDishes(ctx, []Dish{{ID: a, Name: "lasagnes", Weight: 1}}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("update duplicate: err = %v", err)
	}

	// A planned dish, once deleted, leaves the list but not the plan.
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	slot := planner.Slot{Day: day, Meal: planner.Dinner}
	if err := s.SetEntries(ctx, map[planner.Slot]int64{slot: a}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDish(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDish(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v", err)
	}
	dishes, _ = s.Dishes(ctx)
	if len(dishes) != 1 {
		t.Fatalf("after delete: %+v", dishes)
	}
	entries, err := s.Entries(ctx, day, day)
	if err != nil {
		t.Fatal(err)
	}
	if entries[slot].DishName != "Gratin" {
		t.Fatalf("entries = %+v", entries)
	}
	// The name of a deleted dish is free again.
	if _, err := s.CreateDish(ctx, NewDish("Gratin")); err != nil {
		t.Fatal(err)
	}
}

func TestPlanEntries(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	id, _ := s.CreateDish(ctx, NewDish("Soupe"))
	id2, _ := s.CreateDish(ctx, NewDish("Salade"))
	d1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(0, 0, 7)
	lunch := planner.Slot{Day: d1, Meal: planner.Lunch}
	if err := s.SetEntries(ctx, map[planner.Slot]int64{lunch: id, {Day: d2, Meal: planner.Lunch}: id}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEntries(ctx, map[planner.Slot]int64{lunch: id2}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Entries(ctx, d1, d1.AddDate(0, 0, 6))
	if len(entries) != 1 || entries[lunch].DishID != id2 {
		t.Fatalf("entries = %+v", entries)
	}
	if err := s.ClearEntries(ctx, lunch); err != nil {
		t.Fatal(err)
	}
	entries, _ = s.Entries(ctx, d1, d1.AddDate(0, 0, 6))
	if len(entries) != 0 {
		t.Fatalf("after clear: %+v", entries)
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
	_ = s.SetWeekStart(ctx, time.Saturday)
	_ = s.SetMeals(ctx, []planner.Meal{planner.Dinner})
	if d, _ := s.WeekStart(ctx); d != time.Saturday {
		t.Errorf("week start = %v", d)
	}
	if m, _ := s.Meals(ctx); len(m) != 1 || m[0] != planner.Dinner {
		t.Errorf("meals = %v", m)
	}
}
