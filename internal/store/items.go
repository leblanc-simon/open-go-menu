package store

import (
	"context"
	"database/sql"
	"sort"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"leblanc.io/open-go-menu/internal/planner"
)

// DefaultWeight is the general weight given to a new item; its seasonal
// bonuses start at 0, neutral.
const DefaultWeight = 5

// Item is an entry of one of the lists: a full dish or a component.
type Item struct {
	ID       int64
	Kind     planner.Kind
	Name     string
	Weight   int
	Seasonal [4]int // indexed by planner.Season
}

// NewItem returns an item of the given kind named name, with the default
// weight and no seasonal bonus.
func NewItem(kind planner.Kind, name string) Item {
	return Item{
		Kind:   kind,
		Name:   name,
		Weight: DefaultWeight,
	}
}

// Candidate converts the item for the draw.
func (it Item) Candidate() planner.Candidate {
	return planner.Candidate{ID: it.ID, Weight: it.Weight, Seasonal: it.Seasonal}
}

// Items returns the live items of a kind, sorted by name.
func (s *Store) Items(ctx context.Context, kind planner.Kind) ([]Item, error) {
	lists, err := s.query(ctx, `WHERE deleted_at IS NULL AND kind = ?`, kind.Key())
	if err != nil {
		return nil, err
	}
	return lists[kind], nil
}

// AllItems returns the live items of every kind, each list sorted by name.
func (s *Store) AllItems(ctx context.Context) ([planner.KindCount][]Item, error) {
	return s.query(ctx, `WHERE deleted_at IS NULL`)
}

// Item returns a live item.
func (s *Store) Item(ctx context.Context, id int64) (Item, error) {
	lists, err := s.query(ctx, `WHERE deleted_at IS NULL AND id = ?`, id)
	if err != nil {
		return Item{}, err
	}
	for _, l := range lists {
		if len(l) > 0 {
			return l[0], nil
		}
	}
	return Item{}, ErrNotFound
}

func (s *Store) query(ctx context.Context, where string, args ...any) ([planner.KindCount][]Item, error) {
	var lists [planner.KindCount][]Item
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, name, weight, weight_spring, weight_summer, weight_autumn, weight_winter
		FROM items `+where+` ORDER BY id`, args...)
	if err != nil {
		return lists, err
	}
	defer rows.Close()

	for rows.Next() {
		var it Item
		var kind string
		if err := rows.Scan(&it.ID, &kind, &it.Name, &it.Weight,
			&it.Seasonal[planner.Spring], &it.Seasonal[planner.Summer],
			&it.Seasonal[planner.Autumn], &it.Seasonal[planner.Winter]); err != nil {
			return lists, err
		}
		k, ok := planner.ParseKind(kind)
		if !ok {
			continue
		}
		it.Kind = k
		lists[k] = append(lists[k], it)
	}
	if err := rows.Err(); err != nil {
		return lists, err
	}
	// SQLite's NOCASE only knows ASCII and would sort "Pâtes" after
	// "Poulet": the Unicode collation sorts names as a reader expects.
	col := collate.New(language.Und, collate.IgnoreCase)
	for _, l := range lists {
		sort.SliceStable(l, func(i, j int) bool {
			return col.CompareString(l[i].Name, l[j].Name) < 0
		})
	}
	return lists, nil
}

// CreateItem inserts an item and returns its identifier.
func (s *Store) CreateItem(ctx context.Context, it Item) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO items (kind, name, weight, weight_spring, weight_summer, weight_autumn, weight_winter, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		it.Kind.Key(), it.Name, it.Weight, it.Seasonal[planner.Spring], it.Seasonal[planner.Summer],
		it.Seasonal[planner.Autumn], it.Seasonal[planner.Winter], now())
	if isUniqueViolation(err) {
		return 0, ErrDuplicateName
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateItems saves the name and weights of several live items of one kind
// at once. Either every item is saved or none is; an item of another kind is
// left untouched.
func (s *Store) UpdateItems(ctx context.Context, kind planner.Kind, items []Item) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		// Names are first moved out of the way so that two items may swap
		// names without tripping the unique index halfway.
		for _, it := range items {
			if _, err := tx.ExecContext(ctx,
				`UPDATE items SET name = '#' || id WHERE id = ? AND kind = ? AND deleted_at IS NULL`,
				it.ID, kind.Key()); err != nil {
				return err
			}
		}
		for _, it := range items {
			_, err := tx.ExecContext(ctx, `
				UPDATE items SET name = ?, weight = ?, weight_spring = ?, weight_summer = ?,
					weight_autumn = ?, weight_winter = ?
				WHERE id = ? AND kind = ? AND deleted_at IS NULL`,
				it.Name, it.Weight, it.Seasonal[planner.Spring], it.Seasonal[planner.Summer],
				it.Seasonal[planner.Autumn], it.Seasonal[planner.Winter], it.ID, kind.Key())
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

// DeleteItem removes an item from its list and the draws. It stays referenced
// by the past plans, which keep displaying its name (EF-12).
func (s *Store) DeleteItem(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE items SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
