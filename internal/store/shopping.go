package store

import (
	"context"
	"database/sql"
	"time"

	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/shopping"
)

const (
	keyShoppingFrom = "shopping_from"
	keyShoppingTo   = "shopping_to"
)

// PlannedDish is a full dish planned on a slot.
type PlannedDish struct {
	Slot planner.Slot
	Name string
}

// ShoppingDays returns the days of the current list, both included. ok is
// false when there is no list yet.
func (s *Store) ShoppingDays(ctx context.Context) (from, to time.Time, ok bool, err error) {
	f, okFrom, err := s.setting(ctx, keyShoppingFrom)
	if err != nil {
		return
	}
	t, okTo, err := s.setting(ctx, keyShoppingTo)
	if err != nil || !okFrom || !okTo {
		return
	}
	from, errFrom := planner.ParseDay(f)
	to, errTo := planner.ParseDay(t)
	return from, to, errFrom == nil && errTo == nil, nil
}

// StartShopping begins a new list over the given days. The articles added
// by hand and the ticks of the previous list go with it (EF-22).
func (s *Store) StartShopping(ctx context.Context, from, to time.Time) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{`DELETE FROM shopping_extras`, `DELETE FROM shopping_checks`} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		for key, day := range map[string]time.Time{keyShoppingFrom: from, keyShoppingTo: to} {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO settings (key, value) VALUES (?, ?)
				ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, day.Format(planner.DateLayout)); err != nil {
				return err
			}
		}
		return nil
	})
}

// LastPlannedDay returns the last day holding a planned meal; ok is false
// when nothing is planned.
func (s *Store) LastPlannedDay(ctx context.Context) (day time.Time, ok bool, err error) {
	var v sql.NullString
	if err = s.db.QueryRowContext(ctx, `SELECT MAX(day) FROM plan_items`).Scan(&v); err != nil || !v.Valid {
		return
	}
	day, err = planner.ParseDay(v.String)
	return day, err == nil, err
}

// ShoppingRows returns what the list needs: the ingredients of the full
// dishes and the components planned between from and to, then the articles
// added by hand. A component bought for the first time gets the article of
// its name, created if need be, and keeps it afterwards.
func (s *Store) ShoppingRows(ctx context.Context, from, to time.Time) ([]shopping.Row, error) {
	f, t := from.Format(planner.DateLayout), to.Format(planner.DateLayout)
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT DISTINCT i.id, i.name FROM plan_items p JOIN items i ON i.id = p.item_id
			WHERE p.day BETWEEN ? AND ? AND p.kind <> ?
			AND NOT EXISTS (SELECT 1 FROM component_articles c WHERE c.item_id = i.id)`,
			f, t, planner.Dish.Key())
		if err != nil {
			return err
		}
		type item struct {
			id   int64
			name string
		}
		var missing []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.id, &it.name); err != nil {
				rows.Close()
				return err
			}
			missing = append(missing, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, it := range missing {
			article, err := findOrCreateArticle(ctx, tx, it.name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO component_articles (item_id, article_id) VALUES (?, ?)`,
				it.id, article); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT p.day, p.meal, i.name, a.id, a.name, COALESCE(a.aisle_id, 0), d.quantity
		FROM plan_items p
		JOIN items i ON i.id = p.item_id
		JOIN dish_ingredients d ON d.item_id = p.item_id
		JOIN articles a ON a.id = d.article_id
		WHERE p.day BETWEEN ? AND ? AND p.kind = ?
		UNION ALL
		SELECT p.day, p.meal, '', a.id, a.name, COALESCE(a.aisle_id, 0), ''
		FROM plan_items p
		JOIN component_articles c ON c.item_id = p.item_id
		JOIN articles a ON a.id = c.article_id
		WHERE p.day BETWEEN ? AND ? AND p.kind <> ?
		UNION ALL
		SELECT '', '', '', a.id, a.name, COALESCE(a.aisle_id, 0), e.quantity
		FROM shopping_extras e JOIN articles a ON a.id = e.article_id`,
		f, t, planner.Dish.Key(), f, t, planner.Dish.Key())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []shopping.Row
	for rows.Next() {
		var day, meal string
		var r shopping.Row
		if err := rows.Scan(&day, &meal, &r.Need.Dish, &r.Article.ID, &r.Article.Name, &r.Article.Aisle, &r.Need.Quantity); err != nil {
			return nil, err
		}
		if day != "" {
			d, err := planner.ParseDay(day)
			if err != nil {
				return nil, err
			}
			r.Need.Slot = planner.Slot{Day: d, Meal: planner.Meal(meal)}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DishesWithoutIngredients returns the full dishes planned between from and
// to that have no ingredient, which the list therefore misses.
func (s *Store) DishesWithoutIngredients(ctx context.Context, from, to time.Time) ([]PlannedDish, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.day, p.meal, i.name FROM plan_items p JOIN items i ON i.id = p.item_id
		WHERE p.day BETWEEN ? AND ? AND p.kind = ?
		AND NOT EXISTS (SELECT 1 FROM dish_ingredients d WHERE d.item_id = i.id)
		ORDER BY p.day, p.meal DESC`,
		from.Format(planner.DateLayout), to.Format(planner.DateLayout), planner.Dish.Key())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlannedDish
	for rows.Next() {
		var day, meal string
		var p PlannedDish
		if err := rows.Scan(&day, &meal, &p.Name); err != nil {
			return nil, err
		}
		d, err := planner.ParseDay(day)
		if err != nil {
			return nil, err
		}
		p.Slot = planner.Slot{Day: d, Meal: planner.Meal(meal)}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AddExtra adds an article to the current list by hand, creating it in the
// catalogue if need be (EF-23). It returns ErrDuplicateName when it already
// was added by hand.
func (s *Store) AddExtra(ctx context.Context, name, quantity string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		id, err := findOrCreateArticle(ctx, tx, name)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO shopping_extras (article_id, quantity) VALUES (?, ?)`, id, quantity)
		if isUniqueViolation(err) {
			return ErrDuplicateName
		}
		return err
	})
}

// RemoveExtra takes back an article added by hand.
func (s *Store) RemoveExtra(ctx context.Context, article int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shopping_extras WHERE article_id = ?`, article)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Ticked returns the ticked articles of the list and the needs each one
// covered when ticked.
func (s *Store) Ticked(ctx context.Context) (map[int64]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT article_id, needs FROM shopping_checks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var needs string
		if err := rows.Scan(&id, &needs); err != nil {
			return nil, err
		}
		out[id] = needs
	}
	return out, rows.Err()
}

// Tick marks an article as in the basket for the given needs (RG-12).
func (s *Store) Tick(ctx context.Context, article int64, needs string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO shopping_checks (article_id, needs) VALUES (?, ?)
		ON CONFLICT (article_id) DO UPDATE SET needs = excluded.needs`, article, needs)
	if isForeignKeyViolation(err) {
		return ErrNotFound
	}
	return err
}

// Untick puts an article back on the list.
func (s *Store) Untick(ctx context.Context, article int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM shopping_checks WHERE article_id = ?`, article)
	return err
}
