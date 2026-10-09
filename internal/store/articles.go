package store

import (
	"context"
	"database/sql"
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"leblanc.io/open-go-menu/internal/planner"
)

// Article is anything that can be bought: an ingredient, a component, toilet
// paper (EF-21).
type Article struct {
	ID    int64
	Name  string
	Aisle int64 // 0: none
	Uses  int   // the recipes it is an ingredient of
}

// Aisle is an aisle of the shop, in the order of the walk through it (EF-20).
type Aisle struct {
	ID   int64
	Name string
}

// Ingredient is an article in a recipe, with its free-text quantity (EF-19).
type Ingredient struct {
	ArticleID int64
	Name      string
	Quantity  string
}

func sortByName[T any](list []T, name func(T) string) {
	// SQLite's NOCASE only knows ASCII: the Unicode collation sorts names
	// as a reader expects.
	col := collate.New(language.Und, collate.IgnoreCase)
	sort.SliceStable(list, func(i, j int) bool { return col.CompareString(name(list[i]), name(list[j])) < 0 })
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// findOrCreateArticle returns the article named name, whatever the case,
// creating it when there is none.
func findOrCreateArticle(ctx context.Context, tx *sql.Tx, name string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM articles WHERE name = ? COLLATE NOCASE`, name).Scan(&id)
	if err != sql.ErrNoRows {
		return id, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO articles (name) VALUES (?)`, name)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ── Articles ──────────────────────────────────────────────────────────────

// Articles returns every article, sorted by name.
func (s *Store) Articles(ctx context.Context) ([]Article, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.name, COALESCE(a.aisle_id, 0),
			(SELECT COUNT(*) FROM dish_ingredients d WHERE d.article_id = a.id)
		FROM articles a`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.Name, &a.Aisle, &a.Uses); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortByName(out, func(a Article) string { return a.Name })
	return out, nil
}

// CreateArticle adds an article to the catalogue.
func (s *Store) CreateArticle(ctx context.Context, name string, aisle int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO articles (name, aisle_id) VALUES (?, ?)`, name, nullID(aisle))
	if isUniqueViolation(err) {
		return 0, ErrDuplicateName
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateArticles saves the name and aisle of several articles at once.
func (s *Store) UpdateArticles(ctx context.Context, articles []Article) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		// Names are first moved out of the way so that two articles may
		// swap names.
		for _, a := range articles {
			if _, err := tx.ExecContext(ctx, `UPDATE articles SET name = '#' || id WHERE id = ?`, a.ID); err != nil {
				return err
			}
		}
		for _, a := range articles {
			_, err := tx.ExecContext(ctx, `UPDATE articles SET name = ?, aisle_id = ? WHERE id = ?`,
				a.Name, nullID(a.Aisle), a.ID)
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

// SetArticleAisle puts an article in an aisle; 0 takes it out of any.
func (s *Store) SetArticleAisle(ctx context.Context, article, aisle int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE articles SET aisle_id = ? WHERE id = ?`, nullID(aisle), article)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteArticle removes an article from the catalogue and the current list.
// An ingredient of a recipe is refused with ErrInUse. A component bought as
// this article gets a new one the next time it is on a list.
func (s *Store) DeleteArticle(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var uses int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM dish_ingredients WHERE article_id = ?`, id).Scan(&uses); err != nil {
			return err
		}
		if uses > 0 {
			return ErrInUse
		}
		for _, q := range []string{
			`DELETE FROM component_articles WHERE article_id = ?`,
			`DELETE FROM shopping_extras WHERE article_id = ?`,
			`DELETE FROM shopping_checks WHERE article_id = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM articles WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ArticleSuggestions returns the names to suggest when typing an article:
// the catalogue and the live starches, proteins and vegetables, which are
// bought too (EF-19). Each name comes once, sorted.
func (s *Store) ArticleSuggestions(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM articles
		UNION ALL
		SELECT name FROM items WHERE deleted_at IS NULL AND kind IN (?, ?, ?)`,
		planner.Starch.Key(), planner.Protein.Key(), planner.Vegetable.Key())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	seen := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		if key := strings.ToLower(n); !seen[key] {
			seen[key] = true
			names = append(names, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortByName(names, func(n string) string { return n })
	return names, nil
}

// ── Aisles ────────────────────────────────────────────────────────────────

// Aisles returns the aisles in their order.
func (s *Store) Aisles(ctx context.Context) ([]Aisle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM aisles ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Aisle
	for rows.Next() {
		var a Aisle
		if err := rows.Scan(&a.ID, &a.Name); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AislesByName returns the aisles sorted by name, for the choice of an
// aisle; Aisles keeps the order of the walk through the shop.
func (s *Store) AislesByName(ctx context.Context) ([]Aisle, error) {
	aisles, err := s.Aisles(ctx)
	sortByName(aisles, func(a Aisle) string { return a.Name })
	return aisles, err
}

// CreateAisle adds an aisle at the end.
func (s *Store) CreateAisle(ctx context.Context, name string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO aisles (name, position) SELECT ?, COALESCE(MAX(position), 0) + 1 FROM aisles`, name)
	if isUniqueViolation(err) {
		return 0, ErrDuplicateName
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RenameAisles saves the names of several aisles at once.
func (s *Store) RenameAisles(ctx context.Context, aisles []Aisle) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, a := range aisles {
			if _, err := tx.ExecContext(ctx, `UPDATE aisles SET name = '#' || id WHERE id = ?`, a.ID); err != nil {
				return err
			}
		}
		for _, a := range aisles {
			_, err := tx.ExecContext(ctx, `UPDATE aisles SET name = ? WHERE id = ?`, a.Name, a.ID)
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

// MoveAisle swaps an aisle with the one before it (up) or after it. At
// either end it does nothing.
func (s *Store) MoveAisle(ctx context.Context, id int64, up bool) error {
	aisles, err := s.Aisles(ctx)
	if err != nil {
		return err
	}
	i := -1
	for j, a := range aisles {
		if a.ID == id {
			i = j
		}
	}
	if i < 0 {
		return ErrNotFound
	}
	j := i + 1
	if up {
		j = i - 1
	}
	if j < 0 || j >= len(aisles) {
		return nil
	}
	aisles[i], aisles[j] = aisles[j], aisles[i]
	// Renumbering every aisle also mends equal or missing positions.
	return s.tx(ctx, func(tx *sql.Tx) error {
		for pos, a := range aisles {
			if _, err := tx.ExecContext(ctx, `UPDATE aisles SET position = ? WHERE id = ?`, pos+1, a.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetAisleOrder puts the aisles in the given order, which must name every
// aisle once; ErrNotFound otherwise.
func (s *Store) SetAisleOrder(ctx context.Context, ids []int64) error {
	aisles, err := s.Aisles(ctx)
	if err != nil {
		return err
	}
	known := map[int64]bool{}
	for _, a := range aisles {
		known[a.ID] = true
	}
	if len(ids) != len(aisles) {
		return ErrNotFound
	}
	for _, id := range ids {
		if !known[id] {
			return ErrNotFound
		}
		delete(known, id) // twice the same aisle is refused too
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		for pos, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE aisles SET position = ? WHERE id = ?`, pos+1, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteAisle removes an aisle; its articles are left without one.
func (s *Store) DeleteAisle(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE articles SET aisle_id = NULL WHERE aisle_id = ?`, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM aisles WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ── Ingredients of the dishes ─────────────────────────────────────────────

// DishIngredients returns the ingredients of a dish, in the order they were
// added.
func (s *Store) DishIngredients(ctx context.Context, dishID int64) ([]Ingredient, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.name, d.quantity
		FROM dish_ingredients d JOIN articles a ON a.id = d.article_id
		WHERE d.item_id = ? ORDER BY d.position`, dishID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ingredient
	for rows.Next() {
		var g Ingredient
		if err := rows.Scan(&g.ArticleID, &g.Name, &g.Quantity); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// IngredientCounts returns the number of ingredients of each dish that has
// some.
func (s *Store) IngredientCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, COUNT(*) FROM dish_ingredients GROUP BY item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int64]int)
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		counts[id] = n
	}
	return counts, rows.Err()
}

// AddIngredient appends an ingredient to a dish, reusing the article of the
// same name when there is one. A non-zero aisle files the article there. It
// returns ErrDuplicateName when the dish already has it.
func (s *Store) AddIngredient(ctx context.Context, dishID int64, name, quantity string, aisle int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		id, err := findOrCreateArticle(ctx, tx, name)
		if err != nil {
			return err
		}
		if aisle != 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE articles SET aisle_id = ? WHERE id = ?`, aisle, id); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO dish_ingredients (item_id, article_id, position, quantity)
			SELECT ?, ?, COALESCE(MAX(position), 0) + 1, ? FROM dish_ingredients WHERE item_id = ?`,
			dishID, id, quantity, dishID)
		if isUniqueViolation(err) {
			return ErrDuplicateName
		}
		return err
	})
}

// SetQuantities saves the quantities of the ingredients of a dish, by
// article.
func (s *Store) SetQuantities(ctx context.Context, dishID int64, quantities map[int64]string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for article, q := range quantities {
			if _, err := tx.ExecContext(ctx, `UPDATE dish_ingredients SET quantity = ? WHERE item_id = ? AND article_id = ?`,
				q, dishID, article); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveIngredient takes an article out of a recipe. The article stays in
// the catalogue, with its aisle.
func (s *Store) RemoveIngredient(ctx context.Context, dishID, articleID int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dish_ingredients WHERE item_id = ? AND article_id = ?`,
		dishID, articleID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
