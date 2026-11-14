package store

import (
	"context"
	"database/sql"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Names are told apart loosely, to keep duplicates out of the lists and the
// catalogue: SQLite's NOCASE only folds ASCII, so "Pâtes" and "pates" would
// both get in. The Unicode collation at primary strength ignores case,
// accents and width.

// queryer is a database or a transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// looseKey returns the key two names share when they only differ by case,
// accents or width. A collator is not safe for concurrent use: each call has
// its own.
func looseKey(col *collate.Collator, buf *collate.Buffer, name string) string {
	defer buf.Reset()
	return string(col.KeyFromString(buf, name))
}

func newLoose() (*collate.Collator, *collate.Buffer) {
	return collate.New(language.Und, collate.Loose), &collate.Buffer{}
}

// SameName reports whether two names are the same but for case, accents or
// width.
func SameName(a, b string) bool {
	col, buf := newLoose()
	return looseKey(col, buf, a) == looseKey(col, buf, b)
}

// findLoose returns the identifier of the first row of query — selecting
// id, name — whose name loosely equals name; ok is false when there is none.
func findLoose(ctx context.Context, q queryer, name, query string, args ...any) (id int64, ok bool, err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	col, buf := newLoose()
	want := looseKey(col, buf, name)
	for rows.Next() {
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return 0, false, err
		}
		if looseKey(col, buf, n) == want {
			return id, true, rows.Close()
		}
	}
	return 0, false, rows.Err()
}

// names returns the names of the rows of query — selecting id, name.
func names(ctx context.Context, q queryer, query string, args ...any) (map[int64]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// renamedClash reports whether a row renamed since before — the names
// before a save, from names — now loosely shares its name with another row
// of query. Duplicates already there are left alone, so that a bulk save
// still goes through.
func renamedClash(ctx context.Context, q queryer, before map[int64]string, query string, args ...any) (bool, error) {
	after, err := names(ctx, q, query, args...)
	if err != nil {
		return false, err
	}
	col, buf := newLoose()
	byKey := map[string][]int64{}
	for id, n := range after {
		k := looseKey(col, buf, n)
		byKey[k] = append(byKey[k], id)
	}
	for _, ids := range byKey {
		if len(ids) < 2 {
			continue
		}
		for _, id := range ids {
			if after[id] != before[id] {
				return true, nil
			}
		}
	}
	return false, nil
}
