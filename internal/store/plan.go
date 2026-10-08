package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"leblanc.io/open-menu/internal/planner"
)

// Entry is a planned meal.
type Entry struct {
	DishID   int64
	DishName string
}

// Entries returns the plan entries between from and to, both included.
func (s *Store) Entries(ctx context.Context, from, to time.Time) (map[planner.Slot]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.day, p.meal, d.id, d.name
		FROM plan_entries p JOIN dishes d ON d.id = p.dish_id
		WHERE p.day BETWEEN ? AND ?`,
		from.Format(planner.DateLayout), to.Format(planner.DateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make(map[planner.Slot]Entry)
	for rows.Next() {
		var day, meal string
		var e Entry
		if err := rows.Scan(&day, &meal, &e.DishID, &e.DishName); err != nil {
			return nil, err
		}
		d, err := planner.ParseDay(day)
		if err != nil {
			return nil, err
		}
		entries[planner.Slot{Day: d, Meal: planner.Meal(meal)}] = e
	}
	return entries, rows.Err()
}

// SetEntries assigns dishes to slots, replacing what they held.
func (s *Store) SetEntries(ctx context.Context, picks map[planner.Slot]int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for slot, id := range picks {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO plan_entries (day, meal, dish_id) VALUES (?, ?, ?)
				ON CONFLICT (day, meal) DO UPDATE SET dish_id = excluded.dish_id`,
				slot.Day.Format(planner.DateLayout), string(slot.Meal), id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClearEntries empties slots.
func (s *Store) ClearEntries(ctx context.Context, slots ...planner.Slot) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, slot := range slots {
			if _, err := tx.ExecContext(ctx, `DELETE FROM plan_entries WHERE day = ? AND meal = ?`,
				slot.Day.Format(planner.DateLayout), string(slot.Meal)); err != nil {
				return err
			}
		}
		return nil
	})
}

const (
	keyWeekStart = "week_start"
	keyMeals     = "meals"
)

func (s *Store) setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) setSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// WeekStart returns the first day of the week. Default: Monday.
func (s *Store) WeekStart(ctx context.Context) (time.Weekday, error) {
	v, ok, err := s.setting(ctx, keyWeekStart)
	if err != nil || !ok {
		return time.Monday, err
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 6 {
		return time.Monday, nil
	}
	return time.Weekday(n), nil
}

// SetWeekStart saves the first day of the week.
func (s *Store) SetWeekStart(ctx context.Context, d time.Weekday) error {
	return s.setSetting(ctx, keyWeekStart, strconv.Itoa(int(d)))
}

// Meals returns the planned meals, in chronological order. Default: all.
func (s *Store) Meals(ctx context.Context) ([]planner.Meal, error) {
	v, ok, err := s.setting(ctx, keyMeals)
	if err != nil || !ok {
		return planner.Meals, err
	}
	var meals []planner.Meal
	for _, m := range planner.Meals {
		for _, part := range strings.Split(v, ",") {
			if part == string(m) {
				meals = append(meals, m)
			}
		}
	}
	if len(meals) == 0 {
		return planner.Meals, nil
	}
	return meals, nil
}

// SetMeals saves the planned meals.
func (s *Store) SetMeals(ctx context.Context, meals []planner.Meal) error {
	parts := make([]string, len(meals))
	for i, m := range meals {
		parts[i] = string(m)
	}
	return s.setSetting(ctx, keyMeals, strings.Join(parts, ","))
}
