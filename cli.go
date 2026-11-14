package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"leblanc.io/open-go-base/authx"

	"leblanc.io/open-go-menu/internal/accounts"
	"leblanc.io/open-go-menu/internal/config"
)

// commandArgs returns what follows the flags appconf reads (-c, -version):
// the command and its arguments.
func commandArgs(args []string) []string {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("c", "", "")
	fs.Bool("version", false, "")
	if err := fs.Parse(args); err != nil {
		return nil
	}
	return fs.Args()
}

const usage = `Commands:
  serve                                 run the server (default)
  create-family [-from file.db] <login> create a family and print its password;
                                        -from imports the database of a single-family install
  reset-password <login>                print a new password; every device is logged out
  invite [-days 30]                     print a single-use invitation code
  families                              list the families, their last visit and who invited them
  purge [-dry-run]                      delete the families without any visit for
                                        families.inactive_months, the expired sessions
                                        and the invitation codes expired unused
`

// command runs an administration command (EF-30, EF-31).
func command(cfg config.Config, args []string, out io.Writer) error {
	name, args := args[0], args[1:]
	if name == "help" || name == "-h" {
		fmt.Fprint(out, usage)
		return nil
	}
	accts, fams, err := openData(cfg)
	if err != nil {
		return err
	}
	defer accts.Close()
	defer fams.Close()
	ctx := context.Background()
	now := time.Now()
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)

	switch name {
	case "create-family":
		from := fs.String("from", "", "database of a single-family install to import")
		if err := fs.Parse(args); err != nil {
			return err
		}
		login, err := loginArg(fs)
		if err != nil {
			return err
		}
		password, hash, err := newPassword(cfg)
		if err != nil {
			return err
		}
		fam, err := accts.CreateFamily(ctx, login, hash, "", now)
		if err != nil {
			return err
		}
		if *from != "" {
			if err := importDatabase(ctx, *from, fams.Path(fam.File)); err != nil {
				_ = accts.DeleteFamily(ctx, fam.UserID)
				_ = fams.Remove(fam.File)
				return err
			}
		}
		// Opening the database creates it, or brings an import up to date.
		_, release, err := fams.Acquire(ctx, fam.File)
		if err != nil {
			return err
		}
		release()
		fmt.Fprintf(out, "family %s created\npassword: %s\n", login, password)

	case "reset-password":
		if err := fs.Parse(args); err != nil {
			return err
		}
		login, err := loginArg(fs)
		if err != nil {
			return err
		}
		fam, err := accts.FamilyByLogin(ctx, login)
		if err != nil {
			return fmt.Errorf("%s: %w", login, err)
		}
		password, hash, err := newPassword(cfg)
		if err != nil {
			return err
		}
		if err := accts.SetPassword(ctx, fam.UserID, hash); err != nil {
			return err
		}
		fmt.Fprintf(out, "new password of %s: %s\nevery device is logged out\n", login, password)

	case "invite":
		days := fs.Int("days", 30, "validity of the code, in days")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *days < 1 {
			return errors.New("invite: -days must be positive")
		}
		code, err := accts.CreateInvite(ctx, time.Duration(*days)*24*time.Hour, now)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "invitation code: %s\nvalid once, until %s\n", code, now.AddDate(0, 0, *days).Format("2006-01-02 15:04"))

	case "families":
		list, err := accts.Families(ctx)
		if err != nil {
			return err
		}
		inviters, err := accts.Inviters(ctx)
		if err != nil {
			return err
		}
		for _, f := range list {
			fmt.Fprintf(out, "%-40s created %s, last visit %s", f.Login, f.CreatedAt.Local().Format("2006-01-02"), f.LastActive.Local().Format("2006-01-02"))
			if by, ok := inviters[f.UserID]; ok {
				fmt.Fprintf(out, ", invited by %s", by)
			}
			fmt.Fprintln(out)
		}

	case "purge":
		dry := fs.Bool("dry-run", false, "list the families to delete, delete nothing")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if cfg.Families.InactiveMonths < 1 {
			return errors.New("purge: families.inactive_months must be positive")
		}
		before := now.AddDate(0, -cfg.Families.InactiveMonths, 0)
		list, err := accts.Inactive(ctx, before)
		if err != nil {
			return err
		}
		deleted := 0
		for _, f := range list {
			if *dry {
				fmt.Fprintf(out, "would delete %s (last visit %s)\n", f.Login, f.LastActive.Local().Format("2006-01-02"))
				continue
			}
			// A family seen since the listing is kept.
			err := accts.DeleteInactiveFamily(ctx, f.UserID, before)
			if errors.Is(err, accounts.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if err := fams.Remove(f.File); err != nil {
				return err
			}
			deleted++
			fmt.Fprintf(out, "deleted %s (last visit %s)\n", f.Login, f.LastActive.Local().Format("2006-01-02"))
		}
		if !*dry {
			n, err := accts.PurgeSessions()
			if err != nil {
				return err
			}
			codes, err := accts.PurgeInvites(ctx, now)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "%d families deleted, %d expired sessions deleted, %d expired invitation codes deleted\n", deleted, n, codes)
		}

	default:
		fmt.Fprint(out, usage)
		return fmt.Errorf("unknown command %q", name)
	}
	return nil
}

func loginArg(fs *flag.FlagSet) (string, error) {
	if fs.NArg() != 1 {
		return "", errors.New("expected one login")
	}
	login := accounts.NormalizeLogin(fs.Arg(0))
	if !accounts.ValidLogin(login) {
		return "", fmt.Errorf("login %q: 3 to 40 characters among a-z 0-9 . _ -", login)
	}
	return login, nil
}

// newPassword returns a random password and its hash: 26 characters of
// base 32, 130 bits.
func newPassword(cfg config.Config) (string, string, error) {
	password := rand.Text()
	hash, err := authx.HashPassword(password, cfg.Auth.BcryptCost)
	return password, hash, err
}

// importDatabase copies a single-family database to dest; VACUUM INTO also
// folds in what its WAL file holds. The source is opened read-only and left
// untouched.
func importDatabase(ctx context.Context, src, dest string) error {
	db, err := sql.Open("sqlite", "file:"+src+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	// The first migration of the menu, not any schema_migrations table: the
	// accounts database has one too.
	var done int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = 'migrations/001_init.sql'`).Scan(&done)
	if err == nil && done == 0 {
		err = errors.New("no menu migration")
	}
	if err != nil {
		return fmt.Errorf("%s: not an OpenGoMenu database: %w", src, err)
	}
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("import %s: %w", src, err)
	}
	return nil
}
