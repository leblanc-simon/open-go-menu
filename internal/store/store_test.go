package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"leblanc.io/open-go-menu/internal/planner"
)

func open(t *testing.T) *Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	s, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Re-running the migrations must be a no-op.
	if err := s.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestItemLifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	a, err := s.CreateItem(ctx, NewItem(planner.Dish, "Lasagnes"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateItem(ctx, NewItem(planner.Dish, "Gratin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateItem(ctx, NewItem(planner.Dish, "lasagnes")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate: err = %v", err)
	}

	// Swapping names in one save must work.
	err = s.UpdateItems(ctx, planner.Dish, []Item{
		{ID: a, Name: "Gratin", Weight: 2, Seasonal: [4]int{1, 0, 3, 4}},
		{ID: b, Name: "Lasagnes", Weight: 5, Seasonal: [4]int{5, 5, 5, 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := s.Items(ctx, planner.Dish)
	if len(items) != 2 || items[0].ID != a || items[0].Seasonal[planner.Summer] != 0 {
		t.Fatalf("items = %+v", items)
	}

	if err := s.UpdateItems(ctx, planner.Dish, []Item{{ID: a, Name: "lasagnes", Weight: 1}}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("update duplicate: err = %v", err)
	}

	// A planned item, once deleted, leaves the list but not the plan.
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	slot := planner.Slot{Day: day, Meal: planner.Dinner}
	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{slot: {planner.Dish: a}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteItem(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: err = %v", err)
	}
	if _, err := s.Item(ctx, a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted item still found: %v", err)
	}
	items, _ = s.Items(ctx, planner.Dish)
	if len(items) != 1 {
		t.Fatalf("after delete: %+v", items)
	}
	entries, err := s.Entries(ctx, day, day)
	if err != nil {
		t.Fatal(err)
	}
	if entries[slot].Names[planner.Dish] != "Gratin" {
		t.Fatalf("entries = %+v", entries)
	}
	// The name of a deleted item is free again.
	if _, err := s.CreateItem(ctx, NewItem(planner.Dish, "Gratin")); err != nil {
		t.Fatal(err)
	}
}

func TestNamesUniquePerKind(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	rice, err := s.CreateItem(ctx, NewItem(planner.Starch, "Riz"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateItem(ctx, NewItem(planner.Dish, "Riz")); err != nil {
		t.Fatalf("TR-12: same name in another list refused: %v", err)
	}
	if _, err := s.CreateItem(ctx, NewItem(planner.Starch, "riz")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("TR-12: duplicate starch accepted: %v", err)
	}
	// Saving the dish list never touches a starch.
	if err := s.UpdateItems(ctx, planner.Dish, []Item{{ID: rice, Name: "Pâtes", Weight: 1}}); err != nil {
		t.Fatal(err)
	}
	if it, _ := s.Item(ctx, rice); it.Name != "Riz" || it.Kind != planner.Starch {
		t.Fatalf("starch changed: %+v", it)
	}
	all, _ := s.AllItems(ctx)
	if len(all[planner.Starch]) != 1 || len(all[planner.Dish]) != 1 {
		t.Fatalf("all = %+v", all)
	}
}

func TestPlates(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	soup, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Soupe"))
	rice, _ := s.CreateItem(ctx, NewItem(planner.Starch, "Riz"))
	fish, _ := s.CreateItem(ctx, NewItem(planner.Protein, "Saumon"))
	beans, _ := s.CreateItem(ctx, NewItem(planner.Vegetable, "Haricots verts"))
	d1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	lunch := planner.Slot{Day: d1, Meal: planner.Lunch}

	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{lunch: {planner.Dish: soup}}); err != nil {
		t.Fatal(err)
	}
	composed := planner.Plate{planner.Starch: rice, planner.Protein: fish, planner.Vegetable: beans}
	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{lunch: composed}); err != nil {
		t.Fatal(err)
	}
	entries, _ := s.Entries(ctx, d1, d1.AddDate(0, 0, 6))
	if e := entries[lunch]; len(entries) != 1 || e.Plate != composed || e.Names[planner.Vegetable] != "Haricots verts" {
		t.Fatalf("a composed plate must replace the dish: %+v", entries)
	}
	// Swapping with the dinner of another day, then back to an empty slot.
	dinner := planner.Slot{Day: d1.AddDate(0, 0, 2), Meal: planner.Dinner}
	if err := s.SetPlates(ctx, map[planner.Slot]planner.Plate{dinner: {planner.Dish: soup}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SwapPlates(ctx, lunch, dinner); err != nil {
		t.Fatal(err)
	}
	entries, _ = s.Entries(ctx, d1, d1.AddDate(0, 0, 6))
	if entries[lunch].Plate != (planner.Plate{planner.Dish: soup}) || entries[dinner].Plate != composed {
		t.Fatalf("after swap: %+v", entries)
	}
	empty := planner.Slot{Day: d1.AddDate(0, 0, 3), Meal: planner.Lunch}
	if err := s.SwapPlates(ctx, dinner, empty); err != nil {
		t.Fatal(err)
	}
	entries, _ = s.Entries(ctx, d1, d1.AddDate(0, 0, 6))
	if _, ok := entries[dinner]; ok || entries[empty].Plate != composed || len(entries) != 2 {
		t.Fatalf("after move to an empty slot: %+v", entries)
	}
	if err := s.ClearEntries(ctx, lunch, empty); err != nil {
		t.Fatal(err)
	}
	if entries, _ = s.Entries(ctx, d1, d1.AddDate(0, 0, 6)); len(entries) != 0 {
		t.Fatalf("after clear: %+v", entries)
	}
}

// TestMigrationKeepsV1Data plays 001, writes data the v1 way, then lets Open
// apply 002.
func TestMigrationKeepsV1Data(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := migrations.ReadFile("migrations/001_init.sql")
	for _, q := range []string{
		string(v1),
		`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations VALUES ('migrations/001_init.sql', '2026-10-08T00:00:00Z')`,
		`INSERT INTO dishes (id, name, weight_summer, created_at) VALUES (7, 'Raclette', 0, '2026-10-08T00:00:00Z')`,
		`INSERT INTO plan_entries VALUES ('2026-10-08', 'dinner', 7)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(ctx, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	it, err := s.Item(ctx, 7)
	if err != nil || it.Kind != planner.Dish || it.Name != "Raclette" || it.Seasonal[planner.Summer] != 0 {
		t.Fatalf("item = %+v, %v", it, err)
	}
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	entries, _ := s.Entries(ctx, day, day)
	if e := entries[planner.Slot{Day: day, Meal: planner.Dinner}]; e.Plate[planner.Dish] != 7 {
		t.Fatalf("plan lost: %+v", entries)
	}
}

func TestSettings(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if d, _ := s.WeekStart(ctx); d != time.Monday {
		t.Errorf("default week start = %v", d)
	}
	if sched, _ := s.Schedule(ctx); len(sched) != 14 {
		t.Errorf("default schedule = %v", sched)
	}
	if n, _ := s.ComposedShare(ctx); n != DefaultComposedShare {
		t.Errorf("default share = %d", n)
	}
	_ = s.SetWeekStart(ctx, time.Saturday)
	wednesday := planner.Weekly{Weekday: time.Wednesday, Meal: planner.Lunch}
	_ = s.SetSchedule(ctx, planner.Schedule{wednesday: true})
	_ = s.SetComposedShare(ctx, 80)
	if d, _ := s.WeekStart(ctx); d != time.Saturday {
		t.Errorf("week start = %v", d)
	}
	if sched, _ := s.Schedule(ctx); len(sched) != 1 || !sched[wednesday] {
		t.Errorf("schedule = %v", sched)
	}
	if n, _ := s.ComposedShare(ctx); n != 80 {
		t.Errorf("share = %d", n)
	}
}

// TestMigrationKeepsMeals checks that 004 turns the former "meals" setting
// into a schedule.
func TestMigrationKeepsMeals(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"migrations/001_init.sql", "migrations/002_items.sql", "migrations/003_ingredients.sql"} {
		body, _ := migrations.ReadFile(name)
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (?, '2026-10-08T00:00:00Z')`, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO settings VALUES ('meals', 'dinner')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(ctx, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sched, _ := s.Schedule(ctx)
	if len(sched) != 7 || !sched[planner.Weekly{Weekday: time.Sunday, Meal: planner.Dinner}] {
		t.Fatalf("schedule = %v", sched)
	}
}

func TestFixedPlates(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	pasta, _ := s.CreateItem(ctx, NewItem(planner.Starch, "Pâtes"))
	ham, _ := s.CreateItem(ctx, NewItem(planner.Protein, "Jambon"))
	pizza, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Pizza"))
	wednesday := planner.Weekly{Weekday: time.Wednesday, Meal: planner.Lunch}
	friday := planner.Weekly{Weekday: time.Friday, Meal: planner.Dinner}

	_ = s.SetFixedPlate(ctx, wednesday, planner.Plate{planner.Dish: pizza})
	// Replacing a plate drops what it held.
	_ = s.SetFixedPlate(ctx, wednesday, planner.Plate{planner.Starch: pasta, planner.Protein: ham})
	_ = s.SetFixedPlate(ctx, friday, planner.Plate{planner.Dish: pizza})
	fixed, err := s.FixedPlates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if e := fixed[wednesday]; len(fixed) != 2 || e.Plate != (planner.Plate{planner.Starch: pasta, planner.Protein: ham}) || e.Names[planner.Protein] != "Jambon" {
		t.Fatalf("fixed = %+v", fixed)
	}
	// A deleted item leaves the fixed plate.
	_ = s.DeleteItem(ctx, ham)
	_ = s.SetFixedPlate(ctx, friday, planner.Plate{})
	fixed, _ = s.FixedPlates(ctx)
	if len(fixed) != 1 || fixed[wednesday].Plate != (planner.Plate{planner.Starch: pasta}) {
		t.Fatalf("after delete and clear: %+v", fixed)
	}
}

func TestIngredients(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	lasagna, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Lasagnes"))
	gratin, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Gratin"))
	if _, err := s.CreateItem(ctx, NewItem(planner.Protein, "Bœuf")); err != nil {
		t.Fatal(err)
	}
	dairy, _ := s.CreateAisle(ctx, "Crèmerie")

	for _, n := range []string{"Pâtes", "Bœuf", "Tomate"} {
		if err := s.AddIngredient(ctx, lasagna, n, "", 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddIngredient(ctx, lasagna, "tomate", "", 0); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate: err = %v", err)
	}
	// Two dishes share the article of the same name; an aisle files it.
	if err := s.AddIngredient(ctx, gratin, "Crème", "20 cl", dairy); err != nil {
		t.Fatal(err)
	}
	if err := s.AddIngredient(ctx, gratin, "tomate", "2", 0); err != nil {
		t.Fatal(err)
	}
	got, _ := s.DishIngredients(ctx, lasagna)
	shared, _ := s.DishIngredients(ctx, gratin)
	if len(got) != 3 || got[0].Name != "Pâtes" || got[2].Name != "Tomate" || shared[1].ArticleID != got[2].ArticleID || shared[0].Quantity != "20 cl" {
		t.Fatalf("ingredients = %+v, %+v", got, shared)
	}
	if err := s.SetQuantities(ctx, lasagna, map[int64]string{got[0].ArticleID: "500 g"}); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.DishIngredients(ctx, lasagna); got[0].Quantity != "500 g" {
		t.Fatalf("quantity = %q", got[0].Quantity)
	}
	if counts, _ := s.IngredientCounts(ctx); counts[lasagna] != 3 || counts[gratin] != 2 {
		t.Fatalf("counts = %v", counts)
	}

	// EF-19: ingredients never join a component list, components are
	// suggested, each name once.
	if starches, _ := s.Items(ctx, planner.Starch); len(starches) != 0 {
		t.Fatalf("starches = %+v", starches)
	}
	_, _ = s.CreateItem(ctx, NewItem(planner.Vegetable, "Courgette"))
	sugg, _ := s.ArticleSuggestions(ctx)
	if want := "Bœuf Courgette Crème Pâtes Tomate"; strings.Join(sugg, " ") != want {
		t.Fatalf("suggestions = %v, want %s", sugg, want)
	}

	// Out of every recipe, an article stays in the catalogue (EF-21).
	tomato := got[2].ArticleID
	_ = s.RemoveIngredient(ctx, lasagna, tomato)
	if err := s.RemoveIngredient(ctx, lasagna, tomato); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: err = %v", err)
	}
	if err := s.DeleteArticle(ctx, tomato); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete an ingredient of the gratin: err = %v", err)
	}
	_ = s.RemoveIngredient(ctx, gratin, tomato)
	if err := s.DeleteArticle(ctx, tomato); err != nil {
		t.Fatal(err)
	}
	articles, _ := s.Articles(ctx)
	if len(articles) != 3 || articles[1].Name != "Crème" || articles[1].Aisle != dairy || articles[1].Uses != 1 {
		t.Fatalf("articles = %+v", articles)
	}
}

func TestAislesAndArticles(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	veg, _ := s.CreateAisle(ctx, "Légumes")
	meat, _ := s.CreateAisle(ctx, "Boucherie")
	drinks, _ := s.CreateAisle(ctx, "Boissons")
	if _, err := s.CreateAisle(ctx, "boissons"); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate: err = %v", err)
	}
	order := func() string {
		aisles, _ := s.Aisles(ctx)
		var n []string
		for _, a := range aisles {
			n = append(n, a.Name)
		}
		return strings.Join(n, ",")
	}
	_ = s.MoveAisle(ctx, drinks, true)
	_ = s.MoveAisle(ctx, veg, true) // already first
	if got := order(); got != "Légumes,Boissons,Boucherie" {
		t.Fatalf("order = %s", got)
	}
	if err := s.RenameAisles(ctx, []Aisle{{ID: veg, Name: "Boucherie"}, {ID: meat, Name: "Fruits et légumes"}}); err != nil {
		t.Fatal(err)
	}
	if got := order(); got != "Boucherie,Boissons,Fruits et légumes" {
		t.Fatalf("renamed = %s", got)
	}

	beer, _ := s.CreateArticle(ctx, "Bière", drinks)
	if _, err := s.CreateArticle(ctx, "bière", 0); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("duplicate article: err = %v", err)
	}
	soap, _ := s.CreateArticle(ctx, "Savon", 0)
	if err := s.UpdateArticles(ctx, []Article{{ID: beer, Name: "Savon", Aisle: drinks}, {ID: soap, Name: "Bière", Aisle: veg}}); err != nil {
		t.Fatal(err)
	}
	_ = s.SetArticleAisle(ctx, soap, meat)
	// An aisle gone leaves its articles without one.
	_ = s.DeleteAisle(ctx, drinks)
	articles, _ := s.Articles(ctx)
	if articles[0].ID != soap || articles[0].Aisle != meat || articles[1].Aisle != 0 {
		t.Fatalf("articles = %+v", articles)
	}
}

func TestShopping(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	lasagna, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Lasagnes"))
	raclette, _ := s.CreateItem(ctx, NewItem(planner.Dish, "Raclette"))
	rice, _ := s.CreateItem(ctx, NewItem(planner.Starch, "Riz"))
	beef, _ := s.CreateItem(ctx, NewItem(planner.Protein, "Bœuf"))
	_ = s.AddIngredient(ctx, lasagna, "bœuf", "500 g", 0)
	d1 := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	_ = s.SetPlates(ctx, map[planner.Slot]planner.Plate{
		{Day: d1, Meal: planner.Lunch}:                   {planner.Starch: rice, planner.Protein: beef},
		{Day: d1.AddDate(0, 0, 1), Meal: planner.Dinner}: {planner.Dish: lasagna},
		{Day: d1.AddDate(0, 0, 2), Meal: planner.Dinner}: {planner.Dish: raclette},
		{Day: d1.AddDate(0, 0, 9), Meal: planner.Dinner}: {planner.Dish: lasagna}, // out of the list
	})

	if _, _, ok, _ := s.ShoppingDays(ctx); ok {
		t.Fatal("a list before any")
	}
	if last, ok, _ := s.LastPlannedDay(ctx); !ok || !last.Equal(d1.AddDate(0, 0, 9)) {
		t.Fatalf("last planned day = %v", last)
	}
	to := d1.AddDate(0, 0, 6)
	_ = s.AddExtra(ctx, "Bière", "")
	_ = s.Tick(ctx, 1, "x")
	if err := s.StartShopping(ctx, d1, to); err != nil {
		t.Fatal(err)
	}
	if from, end, ok, _ := s.ShoppingDays(ctx); !ok || !from.Equal(d1) || !end.Equal(to) {
		t.Fatalf("days = %v %v", from, end)
	}
	if ticked, _ := s.Ticked(ctx); len(ticked) != 0 {
		t.Fatalf("a new list keeps the ticks: %v", ticked)
	}
	_ = s.AddExtra(ctx, "Papier toilette", "6 rouleaux")
	if err := s.AddExtra(ctx, "papier toilette", ""); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("extra twice: err = %v", err)
	}

	rows, err := s.ShoppingRows(ctx, d1, to)
	if err != nil {
		t.Fatal(err)
	}
	// The protein Bœuf and the ingredient bœuf are one article.
	byName := map[string][]string{}
	for _, r := range rows {
		byName[r.Article.Name] = append(byName[r.Article.Name], r.Need.Dish+"|"+r.Need.Quantity)
	}
	if len(rows) != 4 || len(byName["bœuf"]) != 2 || byName["Riz"] == nil || byName["Papier toilette"][0] != "|6 rouleaux" {
		t.Fatalf("rows = %v", byName)
	}
	// Renaming the component keeps its article.
	_ = s.UpdateItems(ctx, planner.Starch, []Item{{ID: rice, Name: "Riz basmati", Weight: 5}})
	rows, _ = s.ShoppingRows(ctx, d1, to)
	for _, r := range rows {
		if r.Article.Name == "Riz basmati" {
			t.Fatal("a renamed component got a new article")
		}
	}
	missing, _ := s.DishesWithoutIngredients(ctx, d1, to)
	if len(missing) != 1 || missing[0].Name != "Raclette" {
		t.Fatalf("without ingredients = %+v", missing)
	}

	id := rows[0].Article.ID
	_ = s.Tick(ctx, id, "2026-10-05/lunch")
	if err := s.Tick(ctx, 9999, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tick unknown: err = %v", err)
	}
	if ticked, _ := s.Ticked(ctx); ticked[id] != "2026-10-05/lunch" {
		t.Fatalf("ticked = %v", ticked)
	}
	_ = s.Untick(ctx, id)
	if ticked, _ := s.Ticked(ctx); len(ticked) != 0 {
		t.Fatalf("after untick: %v", ticked)
	}
}

// TestMigrationKeepsIngredients plays 001 to 004, adds an ingredient, then
// lets Open apply 005.
func TestMigrationKeepsIngredients(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v4.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"migrations/001_init.sql", "migrations/002_items.sql", "migrations/003_ingredients.sql", "migrations/004_weekly_meals.sql"} {
		body, _ := migrations.ReadFile(name)
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations VALUES (?, '2026-10-08T00:00:00Z')`, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO items (id, kind, name, created_at) VALUES (1, 'dish', 'Lasagnes', '2026-10-08T00:00:00Z')`,
		`INSERT INTO ingredients (id, name) VALUES (3, 'Béchamel')`,
		`INSERT INTO dish_ingredients VALUES (1, 3, 1)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	s, err := Open(ctx, "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.DishIngredients(ctx, 1)
	if err != nil || len(got) != 1 || got[0].ArticleID != 3 || got[0].Name != "Béchamel" {
		t.Fatalf("ingredients = %+v, %v", got, err)
	}
	// The foreign key follows the renamed table.
	if err := s.DeleteArticle(ctx, 3); !errors.Is(err, ErrInUse) {
		t.Fatalf("delete: err = %v", err)
	}
}
