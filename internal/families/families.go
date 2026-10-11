// Package families opens the database of each family on demand: one SQLite
// file per family, so that no query ever needs a family filter and a family
// is deleted with its file.
package families

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"leblanc.io/open-go-menu/internal/store"
)

// fileRe is the shape of a family file name: random, never derived from the
// login, so that it neither leaks it nor lets a path escape the directory.
var fileRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

var (
	// ErrBadFile is returned for a file name that is not a family's.
	ErrBadFile = errors.New("families: bad file name")
	// ErrGone is returned for a family removed while this process runs: its
	// file is never created again.
	ErrGone = errors.New("families: removed")
)

// Registry keeps the family databases open while they are used.
type Registry struct {
	dir string

	mu   sync.Mutex
	open map[string]*entry
	gone map[string]bool // removed files, never reopened
}

type entry struct {
	st      *store.Store
	users   int // requests holding the store
	used    time.Time
	removed bool // to remove once the last request lets it go
}

// New returns a registry of the families of dir, creating it if need be.
func New(dir string) (*Registry, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("families: %w", err)
	}
	return &Registry{dir: dir, open: map[string]*entry{}, gone: map[string]bool{}}, nil
}

// Path returns the database file of a family.
func (r *Registry) Path(file string) string {
	return filepath.Join(r.dir, file+".db")
}

// Acquire returns the store of a family, opening (and creating) it if need
// be. The caller calls release once done; a store is never closed while
// acquired.
func (r *Registry) Acquire(ctx context.Context, file string) (st *store.Store, release func(), err error) {
	if !fileRe.MatchString(file) {
		return nil, nil, ErrBadFile
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gone[file] {
		return nil, nil, ErrGone
	}
	e, ok := r.open[file]
	if !ok {
		st, err := store.Open(ctx, "file:"+r.Path(file))
		if err != nil {
			return nil, nil, err
		}
		e = &entry{st: st}
		r.open[file] = e
	}
	e.users++
	e.used = time.Now()
	var once sync.Once
	return e.st, func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			e.users--
			e.used = time.Now()
			if e.users == 0 && e.removed {
				e.st.Close()
				delete(r.open, file)
				_ = r.removeFiles(file)
			}
		})
	}, nil
}

// CloseIdle closes the stores nobody has used for maxIdle.
func (r *Registry) CloseIdle(maxIdle time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for file, e := range r.open {
		if e.users == 0 && time.Since(e.used) >= maxIdle {
			e.st.Close()
			delete(r.open, file)
		}
	}
}

// Remove deletes the database of a family, with its WAL files, and keeps it
// from being opened again. While requests use it, the files are deleted when
// the last one releases it.
func (r *Registry) Remove(file string) error {
	if !fileRe.MatchString(file) {
		return ErrBadFile
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gone[file] = true
	if e, ok := r.open[file]; ok {
		if e.users > 0 {
			e.removed = true
			return nil
		}
		e.st.Close()
		delete(r.open, file)
	}
	return r.removeFiles(file)
}

// removeFiles deletes the files of a closed database; r.mu is held.
func (r *Registry) removeFiles(file string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(r.Path(file) + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Close closes every open store.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for file, e := range r.open {
		e.st.Close()
		delete(r.open, file)
	}
}
