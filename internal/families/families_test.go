package families

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/store"
)

func TestRegistry(t *testing.T) {
	ctx := context.Background()
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, _, err := r.Acquire(ctx, "../accounts"); !errors.Is(err, ErrBadFile) {
		t.Fatalf("bad name: %v", err)
	}
	a, b := "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"

	// Each family has its own data.
	st, release, err := r.Acquire(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateItem(ctx, store.NewItem(planner.Dish, "Lasagnes")); err != nil {
		t.Fatal(err)
	}
	other, releaseOther, _ := r.Acquire(ctx, b)
	if items, _ := other.Items(ctx, planner.Dish); len(items) != 0 {
		t.Fatalf("family b sees %+v", items)
	}
	releaseOther()

	// In use: neither closed nor removed.
	r.CloseIdle(0)
	if items, err := st.Items(ctx, planner.Dish); err != nil || len(items) != 1 {
		t.Fatalf("store closed while used: %v", err)
	}
	release()
	release() // twice is harmless

	// Closed when idle, reopened with its data.
	r.CloseIdle(0)
	st, release, _ = r.Acquire(ctx, a)
	if items, _ := st.Items(ctx, planner.Dish); len(items) != 1 {
		t.Fatalf("data lost on reopening: %+v", items)
	}
	release()
	r.CloseIdle(time.Hour) // recently used: kept

	if err := r.Remove(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Path(a)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file left: %v", err)
	}
	// Removed: never created again.
	if _, _, err := r.Acquire(ctx, a); !errors.Is(err, ErrGone) {
		t.Fatalf("reopened after removal: %v", err)
	}

	// In use: removed once the last request lets it go.
	st, release, _ = r.Acquire(ctx, b)
	if err := r.Remove(b); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Items(ctx, planner.Dish); err != nil {
		t.Fatalf("store closed while used: %v", err)
	}
	if _, err := os.Stat(r.Path(b)); err != nil {
		t.Fatalf("file removed while used: %v", err)
	}
	release()
	if _, err := os.Stat(r.Path(b)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file left after release: %v", err)
	}
}
