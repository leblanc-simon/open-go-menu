// Package accounts keeps the families and their logins in the accounts
// database, next to the users and sessions of authx. A family has one login,
// shared by its members, and no personal data: no e-mail, no recovery.
package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"leblanc.io/open-go-base/appconf"
	"leblanc.io/open-go-base/authx"
	"leblanc.io/open-go-base/authx/sqlstore"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	// ErrLoginTaken is returned when a family already has the login.
	ErrLoginTaken = errors.New("accounts: login taken")
	// ErrInvite is returned for an unknown, used or expired invitation.
	ErrInvite = errors.New("accounts: invalid invitation")
	// ErrNotFound is returned for an unknown family.
	ErrNotFound = errors.New("accounts: not found")
)

// loginRe is the shape of a login: lower-case letters, digits and . _ -,
// 3 to 40 characters, starting with a letter or a digit.
var loginRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,39}$`)

// NormalizeLogin returns the login as stored: logins ignore case.
func NormalizeLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

// ValidLogin reports whether a normalized login has the expected shape.
func ValidLogin(login string) bool {
	return loginRe.MatchString(login)
}

// timeLayout stores instants in UTC, so that they compare as strings.
const timeLayout = "2006-01-02T15:04:05Z"

// activityStep is how stale the last activity of a family may get before a
// visit records it again: one write an hour at most.
const activityStep = time.Hour

// Family is an account: a login and the database file of its data.
type Family struct {
	UserID     int64
	Login      string
	File       string
	CreatedAt  time.Time
	LastActive time.Time
}

// Store is the accounts database.
type Store struct {
	db       *sql.DB
	Users    *sqlstore.UserStore
	Sessions *sqlstore.SessionStore
}

// Open opens the accounts database at path and applies the migrations of
// authx and its own.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("accounts: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := sqlstore.Migrate(db, appconf.DialectSQLite); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{
		db:       db,
		Users:    sqlstore.NewUserStore(db, appconf.DialectSQLite),
		Sessions: sqlstore.NewSessionStore(db, appconf.DialectSQLite),
	}, nil
}

// migrate applies the migrations of this package, tracked in the
// schema_migrations table of authx under the "accounts/" prefix.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		key := "accounts/" + strings.TrimPrefix(name, "migrations/")
		var done int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, key).Scan(&done); err != nil {
			return fmt.Errorf("accounts: migration %s: %w", key, err)
		}
		if done > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("accounts: migration %s: %w", key, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (name) VALUES (?)`, key); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the database.
func (s *Store) Close() error {
	return s.db.Close()
}

func format(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(NormalizeInvite(code)))
	return hex.EncodeToString(sum[:])
}

// CheckInvite tells whether an invitation can still be used, without
// consuming it: ErrInvite if not. It lets a signup refuse a wrong code before
// hashing the password; CreateFamily still consumes it atomically.
func (s *Store) CheckInvite(ctx context.Context, invite string, now time.Time) error {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM invites WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
		hashCode(invite), format(now)).Scan(&n)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvite
	}
	return nil
}

// CreateFamily creates a family: its login, with an already hashed password,
// and the name of its database file. A non-empty invite is consumed in the
// same transaction; ErrInvite if it cannot be.
func (s *Store) CreateFamily(ctx context.Context, login, passwordHash, invite string, now time.Time) (Family, error) {
	file, err := randomHex(16)
	if err != nil {
		return Family{}, err
	}
	f := Family{Login: login, File: file, CreatedAt: now.UTC().Truncate(time.Second), LastActive: now.UTC().Truncate(time.Second)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return f, err
	}
	defer tx.Rollback()

	if invite != "" {
		res, err := tx.ExecContext(ctx, `
			UPDATE invites SET used_at = ? WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
			format(now), hashCode(invite), format(now))
		if err != nil {
			return f, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return f, ErrInvite
		}
	}
	// The users table is authx's; its sqlstore cannot insert within a
	// transaction, hence the statement here.
	res, err := tx.ExecContext(ctx, `INSERT INTO users (email, password_hash) VALUES (?, ?)`, login, passwordHash)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return f, ErrLoginTaken
		}
		return f, err
	}
	if f.UserID, err = res.LastInsertId(); err != nil {
		return f, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO families (user_id, file, created_at, last_active_at) VALUES (?, ?, ?, ?)`,
		f.UserID, f.File, format(now), format(now)); err != nil {
		return f, err
	}
	return f, tx.Commit()
}

const familyColumns = `f.user_id, u.email, f.file, f.created_at, f.last_active_at`

func scanFamilies(rows *sql.Rows) ([]Family, error) {
	defer rows.Close()
	var out []Family
	for rows.Next() {
		var f Family
		var created, active string
		if err := rows.Scan(&f.UserID, &f.Login, &f.File, &created, &active); err != nil {
			return nil, err
		}
		f.CreatedAt, _ = time.Parse(timeLayout, created)
		f.LastActive, _ = time.Parse(timeLayout, active)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) family(ctx context.Context, where string, arg any) (Family, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+familyColumns+` FROM families f JOIN users u ON u.id = f.user_id WHERE `+where, arg)
	if err != nil {
		return Family{}, err
	}
	list, err := scanFamilies(rows)
	if err != nil {
		return Family{}, err
	}
	if len(list) == 0 {
		return Family{}, ErrNotFound
	}
	return list[0], nil
}

// FamilyOf returns the family of an authx user.
func (s *Store) FamilyOf(ctx context.Context, userID int64) (Family, error) {
	return s.family(ctx, `f.user_id = ?`, userID)
}

// FamilyByLogin returns the family of a login.
func (s *Store) FamilyByLogin(ctx context.Context, login string) (Family, error) {
	return s.family(ctx, `u.email = ?`, login)
}

// Families returns every family, by login.
func (s *Store) Families(ctx context.Context) ([]Family, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+familyColumns+` FROM families f JOIN users u ON u.id = f.user_id ORDER BY u.email`)
	if err != nil {
		return nil, err
	}
	return scanFamilies(rows)
}

// Touch records a visit of the family, at most once per activityStep.
func (s *Store) Touch(ctx context.Context, userID int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE families SET last_active_at = ? WHERE user_id = ? AND last_active_at < ?`,
		format(now), userID, format(now.Add(-activityStep)))
	return err
}

// Inactive returns the families without any visit since before.
func (s *Store) Inactive(ctx context.Context, before time.Time) ([]Family, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+familyColumns+` FROM families f JOIN users u ON u.id = f.user_id
		WHERE f.last_active_at < ? ORDER BY f.last_active_at`, format(before))
	if err != nil {
		return nil, err
	}
	return scanFamilies(rows)
}

// SetPassword replaces the password hash of a family and closes all its
// sessions.
func (s *Store) SetPassword(ctx context.Context, userID int64, passwordHash string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteFamily removes the account of a family: its login, its sessions. The
// caller removes its database file.
func (s *Store) DeleteFamily(ctx context.Context, userID int64) error {
	return s.deleteFamily(ctx, userID, nil)
}

// DeleteInactiveFamily removes the account of a family, as DeleteFamily, only
// if it has had no visit since before: a visit during a purge saves it.
// ErrNotFound if it has been seen since.
func (s *Store) DeleteInactiveFamily(ctx context.Context, userID int64, before time.Time) error {
	return s.deleteFamily(ctx, userID, &before)
}

func (s *Store) deleteFamily(ctx context.Context, userID int64, before *time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if before != nil {
		res, err := tx.ExecContext(ctx, `DELETE FROM families WHERE user_id = ? AND last_active_at < ?`, userID, format(*before))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
	}
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM families WHERE user_id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, userID); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// inviteAlphabet leaves out the letters and digits read alike (0/O, 1/I/L).
const inviteAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// CreateInvite returns a new single-use invitation code, valid for ttl. Only
// its hash is kept.
func (s *Store) CreateInvite(ctx context.Context, ttl time.Duration, now time.Time) (string, error) {
	// Bytes beyond the last whole multiple of the alphabet are drawn again,
	// so that every character is as likely.
	const limit = 256 / len(inviteAlphabet) * len(inviteAlphabet)
	var code strings.Builder
	b := make([]byte, 1)
	for n := 0; n < 12; {
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		if int(b[0]) >= limit {
			continue
		}
		if n > 0 && n%4 == 0 {
			code.WriteByte('-')
		}
		code.WriteByte(inviteAlphabet[int(b[0])%len(inviteAlphabet)])
		n++
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO invites (code_hash, created_at, expires_at) VALUES (?, ?, ?)`,
		hashCode(code.String()), format(now), format(now.Add(ttl)))
	return code.String(), err
}

// NormalizeInvite makes a typed code comparable: upper case, without
// spaces nor dashes.
func NormalizeInvite(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
}

// PurgeSessions deletes the expired sessions.
func (s *Store) PurgeSessions() (int64, error) {
	return s.Sessions.PurgeExpired()
}

// CheckPassword reports whether plain is the password of the family.
func (s *Store) CheckPassword(userID int64, plain string) (bool, error) {
	u, err := s.Users.GetByID(userID)
	if err != nil {
		return false, err
	}
	err = authx.CheckPassword(u.PasswordHash, plain)
	if errors.Is(err, authx.ErrInvalidCredentials) {
		return false, nil
	}
	return err == nil, err
}
