package shopping

import (
	"testing"
	"time"

	"leblanc.io/open-go-menu/internal/planner"
)

func slot(day string, m planner.Meal) planner.Slot {
	d, _ := planner.ParseDay(day)
	return planner.Slot{Day: d, Meal: m}
}

func names(lines []Line) []string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Article.Name)
	}
	return out
}

func TestBuild(t *testing.T) {
	veg, butcher := Aisle{ID: 1, Name: "Légumes"}, Aisle{ID: 2, Name: "Boucherie"}
	beef := Article{ID: 10, Name: "Bœuf", Aisle: 2}
	rows := []Row{
		{beef, Need{Slot: slot("2026-10-09", planner.Lunch), Quantity: "300 g"}},
		{Article{ID: 11, Name: "tomates", Aisle: 1}, Need{Slot: slot("2026-10-06", planner.Dinner), Dish: "Lasagnes"}},
		{Article{ID: 12, Name: "Bière"}, Need{Quantity: "un pack"}},
		{beef, Need{Slot: slot("2026-10-06", planner.Dinner), Dish: "Lasagnes", Quantity: "500 g"}},
		{Article{ID: 13, Name: "Courgettes", Aisle: 1}, Need{Slot: slot("2026-10-05", planner.Lunch)}},
		{Article{ID: 14, Name: "Sel", Aisle: 99}, Need{}}, // a deleted aisle
	}
	// RG-11: aisles in their order, then no aisle, names sorted within.
	list := Build(rows, []Aisle{veg, butcher}, nil)
	if len(list.Groups) != 3 || list.Groups[0].Aisle != veg || list.Groups[2].Aisle.ID != 0 {
		t.Fatalf("groups = %+v", list.Groups)
	}
	if got := names(list.Groups[0].Lines); got[0] != "Courgettes" || got[1] != "tomates" {
		t.Errorf("vegetables = %v", got)
	}
	if got := names(list.Groups[2].Lines); len(got) != 2 || got[0] != "Bière" {
		t.Errorf("no aisle = %v", got)
	}
	// One line per article, its needs in chronological order.
	b := list.Groups[1].Lines[0]
	if len(b.Needs) != 2 || b.Needs[0].Dish != "Lasagnes" || b.Key() != "2026-10-06/dinner,2026-10-09/lunch" {
		t.Errorf("beef = %+v, key %s", b, b.Key())
	}

	// RG-12: ticked for what it covered; a new need unticks it.
	list = Build(rows, []Aisle{veg, butcher}, map[int64]string{10: b.Key(), 12: "extra"})
	if got := names(list.Done); len(got) != 2 || got[0] != "Bœuf" || got[1] != "Bière" {
		t.Fatalf("done = %v", got)
	}
	more := append(rows, Row{beef, Need{Slot: slot("2026-10-10", planner.Dinner)}})
	list = Build(more, []Aisle{veg, butcher}, map[int64]string{10: b.Key()})
	if len(list.Done) != 0 {
		t.Errorf("a new need left beef ticked: %v", names(list.Done))
	}
	// A need gone keeps it ticked.
	list = Build(rows[1:], []Aisle{veg, butcher}, map[int64]string{10: b.Key()})
	if len(list.Done) != 1 {
		t.Errorf("a need gone unticked beef")
	}
}

func TestNeedOrder(t *testing.T) {
	d := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	lunch, dinner := Need{Slot: planner.Slot{Day: d, Meal: planner.Lunch}}, Need{Slot: planner.Slot{Day: d, Meal: planner.Dinner}}
	if !needBefore(lunch, dinner) || needBefore(dinner, lunch) || !needBefore(dinner, Need{}) {
		t.Error("bad order")
	}
}
