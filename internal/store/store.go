// Package store persists dishes, the meal plan and the settings in SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrDuplicateName is returned when the name is already taken: a live item
// of the same list, an article, an aisle, an ingredient of the recipe.
var ErrDuplicateName = errors.New("store: duplicate name")

// ErrNotFound is returned when what is asked for does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrInUse is returned when an article still is an ingredient of a recipe.
var ErrInUse = errors.New("store: in use")

// Store is the SQLite-backed persistence of the application.
type Store struct {
	db *sql.DB
}

// Open opens the SQLite database designated by dsn and applies the pending
// migrations. The connection-level pragmas are added to the DSN so that every
// pooled connection gets them.
func Open(ctx context.Context, dsn string) (*Store, error) {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	dsn += sep + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// SQLite serialises writers anyway; one connection avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate applies, in name order, every embedded migration not yet recorded.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("store: migrations table: %w", err)
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		var done int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&done); err != nil {
			return fmt.Errorf("store: migration %s: %w", name, err)
		}
		if done > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
				name, now())
			return err
		})
		if err != nil {
			return fmt.Errorf("store: migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
