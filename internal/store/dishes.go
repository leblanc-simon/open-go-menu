package store

import (
	"context"
	"database/sql"
	"sort"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"leblanc.io/open-menu/internal/planner"
)

// DefaultWeight is the weight given to a new dish, general and seasonal.
const DefaultWeight = 5

// Dish is a dish of the list.
type Dish struct {
	ID       int64
	Name     string
	Weight   int
	Seasonal [4]int // indexed by planner.Season
}

// NewDish returns a dish named name with default weights.
func NewDish(name string) Dish {
	return Dish{
		Name:     name,
		Weight:   DefaultWeight,
		Seasonal: [4]int{DefaultWeight, DefaultWeight, DefaultWeight, DefaultWeight},
	}
}

// Candidate converts the dish for the draw.
func (d Dish) Candidate() planner.Candidate {
	return planner.Candidate{ID: d.ID, Weight: d.Weight, Seasonal: d.Seasonal}
}

// Dishes returns the live dishes, sorted by name.
func (s *Store) Dishes(ctx context.Context) ([]Dish, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, weight, weight_spring, weight_summer, weight_autumn, weight_winter
		FROM dishes WHERE deleted_at IS NULL
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dishes []Dish
	for rows.Next() {
		var d Dish
		if err := rows.Scan(&d.ID, &d.Name, &d.Weight,
			&d.Seasonal[planner.Spring], &d.Seasonal[planner.Summer],
			&d.Seasonal[planner.Autumn], &d.Seasonal[planner.Winter]); err != nil {
			return nil, err
		}
		dishes = append(dishes, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// SQLite's NOCASE only knows ASCII and would sort "Pâtes" after
	// "Poulet": the Unicode collation sorts names as a reader expects.
	col := collate.New(language.Und, collate.IgnoreCase)
	sort.SliceStable(dishes, func(i, j int) bool {
		return col.CompareString(dishes[i].Name, dishes[j].Name) < 0
	})
	return dishes, nil
}

// CreateDish inserts a dish and returns its identifier.
func (s *Store) CreateDish(ctx context.Context, d Dish) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO dishes (name, weight, weight_spring, weight_summer, weight_autumn, weight_winter, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.Weight, d.Seasonal[planner.Spring], d.Seasonal[planner.Summer],
		d.Seasonal[planner.Autumn], d.Seasonal[planner.Winter], now())
	if isUniqueViolation(err) {
		return 0, ErrDuplicateName
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateDishes saves the name and weights of several live dishes at once.
// Either every dish is saved or none is.
func (s *Store) UpdateDishes(ctx context.Context, dishes []Dish) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		// Names are first moved out of the way so that two dishes may swap
		// names without tripping the unique index halfway.
		for _, d := range dishes {
			if _, err := tx.ExecContext(ctx,
				`UPDATE dishes SET name = '#' || id WHERE id = ? AND deleted_at IS NULL`, d.ID); err != nil {
				return err
			}
		}
		for _, d := range dishes {
			_, err := tx.ExecContext(ctx, `
				UPDATE dishes SET name = ?, weight = ?, weight_spring = ?, weight_summer = ?,
					weight_autumn = ?, weight_winter = ?
				WHERE id = ? AND deleted_at IS NULL`,
				d.Name, d.Weight, d.Seasonal[planner.Spring], d.Seasonal[planner.Summer],
				d.Seasonal[planner.Autumn], d.Seasonal[planner.Winter], d.ID)
			if isUniqueViolation(err) {
				return ErrDuplicateName
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteDish removes a dish from the list and the draws. It stays referenced
// by the past plan entries, which keep displaying its name (EF-12).
func (s *Store) DeleteDish(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE dishes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
