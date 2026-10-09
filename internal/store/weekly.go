package store

import (
	"context"
	"database/sql"
	"time"

	"leblanc.io/open-go-menu/internal/planner"
)

// Schedule returns the meals planned on each weekday (EF-13). Default: all.
func (s *Store) Schedule(ctx context.Context) (planner.Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT weekday, meal FROM weekly_meals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sched := planner.Schedule{}
	for rows.Next() {
		var day int
		var meal string
		if err := rows.Scan(&day, &meal); err != nil {
			return nil, err
		}
		if m, ok := planner.ParseMeal(meal); ok && day >= 0 && day <= 6 {
			sched[planner.Weekly{Weekday: time.Weekday(day), Meal: m}] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(sched) == 0 {
		return planner.FullSchedule(), nil
	}
	return sched, nil
}

// SetSchedule saves the meals planned on each weekday.
func (s *Store) SetSchedule(ctx context.Context, sched planner.Schedule) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM weekly_meals`); err != nil {
			return err
		}
		for w, on := range sched {
			if !on {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO weekly_meals (weekday, meal) VALUES (?, ?)`,
				int(w.Weekday), string(w.Meal)); err != nil {
				return err
			}
		}
		return nil
	})
}

// FixedPlates returns the plates always served on a meal of the week
// (EF-18), with the names of their items. A deleted item leaves the fixed
// plate; a plate left with nothing is absent.
func (s *Store) FixedPlates(ctx context.Context) (map[planner.Weekly]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.weekday, f.meal, f.kind, i.id, i.name
		FROM fixed_items f JOIN items i ON i.id = f.item_id
		WHERE i.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fixed := make(map[planner.Weekly]Entry)
	for rows.Next() {
		var day int
		var meal, kind, name string
		var id int64
		if err := rows.Scan(&day, &meal, &kind, &id, &name); err != nil {
			return nil, err
		}
		m, okMeal := planner.ParseMeal(meal)
		k, okKind := planner.ParseKind(kind)
		if !okMeal || !okKind || day < 0 || day > 6 {
			continue
		}
		w := planner.Weekly{Weekday: time.Weekday(day), Meal: m}
		e := fixed[w]
		e.Plate[k] = id
		e.Names[k] = name
		fixed[w] = e
	}
	return fixed, rows.Err()
}

// SetFixedPlate fixes the plate of a meal of the week; an empty plate
// returns it to the draw.
func (s *Store) SetFixedPlate(ctx context.Context, w planner.Weekly, plate planner.Plate) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM fixed_items WHERE weekday = ? AND meal = ?`,
			int(w.Weekday), string(w.Meal)); err != nil {
			return err
		}
		for k, id := range plate {
			if id == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO fixed_items (weekday, meal, kind, item_id) VALUES (?, ?, ?, ?)`,
				int(w.Weekday), string(w.Meal), planner.Kind(k).Key(), id); err != nil {
				return err
			}
		}
		return nil
	})
}
