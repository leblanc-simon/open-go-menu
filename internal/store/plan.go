package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"leblanc.io/open-go-menu/internal/planner"
)

// Entry is a planned meal: its plate and the names of its items, which may
// have been deleted since.
type Entry struct {
	Plate planner.Plate
	Names [planner.KindCount]string
}

// Entries returns the plan between from and to, both included.
func (s *Store) Entries(ctx context.Context, from, to time.Time) (map[planner.Slot]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.day, p.meal, p.kind, i.id, i.name
		FROM plan_items p JOIN items i ON i.id = p.item_id
		WHERE p.day BETWEEN ? AND ?`,
		from.Format(planner.DateLayout), to.Format(planner.DateLayout))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make(map[planner.Slot]Entry)
	for rows.Next() {
		var day, meal, kind, name string
		var id int64
		if err := rows.Scan(&day, &meal, &kind, &id, &name); err != nil {
			return nil, err
		}
		d, err := planner.ParseDay(day)
		if err != nil {
			return nil, err
		}
		k, ok := planner.ParseKind(kind)
		if !ok {
			continue
		}
		slot := planner.Slot{Day: d, Meal: planner.Meal(meal)}
		e := entries[slot]
		e.Plate[k] = id
		e.Names[k] = name
		entries[slot] = e
	}
	return entries, rows.Err()
}

// SetPlates assigns plates to slots, replacing what they held.
func (s *Store) SetPlates(ctx context.Context, plates map[planner.Slot]planner.Plate) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for slot, plate := range plates {
			day := slot.Day.Format(planner.DateLayout)
			if _, err := tx.ExecContext(ctx, `DELETE FROM plan_items WHERE day = ? AND meal = ?`,
				day, string(slot.Meal)); err != nil {
				return err
			}
			for k, id := range plate {
				if id == 0 {
					continue
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO plan_items (day, meal, kind, item_id) VALUES (?, ?, ?, ?)`,
					day, string(slot.Meal), planner.Kind(k).Key(), id); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// SwapPlates exchanges the plates of two slots; an empty slot makes it a
// move. The meal column briefly holds a placeholder, which the primary key
// needs while both rows are in flight.
func (s *Store) SwapPlates(ctx context.Context, a, b planner.Slot) error {
	const parked = "~swap"
	move := func(tx *sql.Tx, fromDay, fromMeal, toDay, toMeal string) error {
		_, err := tx.ExecContext(ctx, `UPDATE plan_items SET day = ?, meal = ? WHERE day = ? AND meal = ?`,
			toDay, toMeal, fromDay, fromMeal)
		return err
	}
	aDay, bDay := a.Day.Format(planner.DateLayout), b.Day.Format(planner.DateLayout)
	return s.tx(ctx, func(tx *sql.Tx) error {
		if err := move(tx, aDay, string(a.Meal), aDay, parked); err != nil {
			return err
		}
		if err := move(tx, bDay, string(b.Meal), aDay, string(a.Meal)); err != nil {
			return err
		}
		return move(tx, aDay, parked, bDay, string(b.Meal))
	})
}

// ClearEntries empties slots.
func (s *Store) ClearEntries(ctx context.Context, slots ...planner.Slot) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, slot := range slots {
			if _, err := tx.ExecContext(ctx, `DELETE FROM plan_items WHERE day = ? AND meal = ?`,
				slot.Day.Format(planner.DateLayout), string(slot.Meal)); err != nil {
				return err
			}
		}
		return nil
	})
}

const (
	keyWeekStart     = "week_start"
	keyMeals         = "meals"
	keyComposedShare = "composed_share"
)

// DefaultComposedShare is the default share of composed meals, in percent.
const DefaultComposedShare = 50

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

// ComposedShare returns the share of composed meals, in percent (EF-15).
func (s *Store) ComposedShare(ctx context.Context) (int, error) {
	v, ok, err := s.setting(ctx, keyComposedShare)
	if err != nil || !ok {
		return DefaultComposedShare, err
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 100 {
		return DefaultComposedShare, nil
	}
	return n, nil
}

// SetComposedShare saves the share of composed meals, in percent.
func (s *Store) SetComposedShare(ctx context.Context, share int) error {
	return s.setSetting(ctx, keyComposedShare, strconv.Itoa(share))
}
