// Package shopping builds the shopping list from what the planned meals and
// the household need. It is pure: the store gathers the needs.
package shopping

import (
	"sort"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"leblanc.io/open-go-menu/internal/planner"
)

// extraKey is the need key of an article added by hand.
const extraKey = "extra"

// Article is something to buy, in an aisle of the shop (0: none).
type Article struct {
	ID    int64
	Name  string
	Aisle int64
}

// Need is a reason to buy an article: a planned meal, or a manual addition
// when Slot is zero.
type Need struct {
	Slot     planner.Slot
	Dish     string // the full dish the article is an ingredient of
	Quantity string
}

// IsExtra reports whether the article was added by hand.
func (n Need) IsExtra() bool {
	return n.Slot.Day.IsZero()
}

func (n Need) key() string {
	if n.IsExtra() {
		return extraKey
	}
	return n.Slot.Day.Format(planner.DateLayout) + "/" + string(n.Slot.Meal)
}

// Row is one need of one article, as the store reads it.
type Row struct {
	Article Article
	Need    Need
}

// Line is an article of the list with everything it is needed for, meals in
// chronological order and the manual addition last.
type Line struct {
	Article Article
	Needs   []Need
	Checked bool
	// Aisle is the aisle the line is listed under while to buy: the zero
	// Aisle for an article without aisle, or whose aisle is gone.
	Aisle Aisle
	// Rank is the position of the line in the whole list, ticked or not:
	// a ticked line goes back to its place when unticked.
	Rank int
}

// Key sums up the needs of the line. Ticking the line stores it; a later
// need missing from it unticks the line (RG-12).
func (l Line) Key() string {
	seen := map[string]bool{}
	var keys []string
	for _, n := range l.Needs {
		if k := n.key(); !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// covered reports whether every need of the line was there when it was
// ticked with the given key.
func (l Line) covered(ticked string) bool {
	had := map[string]bool{}
	for _, k := range strings.Split(ticked, ",") {
		had[k] = true
	}
	for _, n := range l.Needs {
		if !had[n.key()] {
			return false
		}
	}
	return true
}

// Aisle is an aisle of the shop.
type Aisle struct {
	ID   int64
	Name string
}

// Group is the lines of one aisle; the group of Aisle.ID 0 holds the
// articles without aisle.
type Group struct {
	Aisle Aisle
	Lines []Line
}

// List is the shopping list: the lines to buy by aisle, then the ticked ones.
type List struct {
	Groups []Group
	Done   []Line
}

// IsEmpty reports whether the list has nothing at all.
func (l List) IsEmpty() bool {
	return len(l.Groups) == 0 && len(l.Done) == 0
}

// Build groups rows into lines, by aisle in the given order, articles
// without aisle last, by name within an aisle (RG-11). A line ticked with a
// key covering all its needs goes to Done, in the same order.
func Build(rows []Row, aisles []Aisle, ticked map[int64]string) List {
	byID := map[int64]*Line{}
	var lines []*Line
	for _, r := range rows {
		l, ok := byID[r.Article.ID]
		if !ok {
			l = &Line{Article: r.Article}
			byID[r.Article.ID] = l
			lines = append(lines, l)
		}
		l.Needs = append(l.Needs, r.Need)
	}

	rank := map[int64]int{}
	for i, a := range aisles {
		rank[a.ID] = i + 1
	}
	last := len(aisles) + 1
	order := func(l *Line) int {
		if r, ok := rank[l.Article.Aisle]; ok {
			return r
		}
		return last
	}
	col := collate.New(language.Und, collate.IgnoreCase)
	sort.SliceStable(lines, func(i, j int) bool {
		if oi, oj := order(lines[i]), order(lines[j]); oi != oj {
			return oi < oj
		}
		return col.CompareString(lines[i].Article.Name, lines[j].Article.Name) < 0
	})

	var list List
	for i, l := range lines {
		sort.SliceStable(l.Needs, func(i, j int) bool { return needBefore(l.Needs[i], l.Needs[j]) })
		l.Rank = i
		if r := order(l); r != last {
			l.Aisle = aisles[r-1]
		}
		if k, ok := ticked[l.Article.ID]; ok && l.covered(k) {
			l.Checked = true
			list.Done = append(list.Done, *l)
			continue
		}
		aisle := l.Aisle
		if n := len(list.Groups); n == 0 || list.Groups[n-1].Aisle != aisle {
			list.Groups = append(list.Groups, Group{Aisle: aisle})
		}
		g := &list.Groups[len(list.Groups)-1]
		g.Lines = append(g.Lines, *l)
	}
	return list
}

// needBefore orders needs chronologically, lunch before dinner, the manual
// addition last.
func needBefore(a, b Need) bool {
	if a.IsExtra() != b.IsExtra() {
		return b.IsExtra()
	}
	if !a.Slot.Day.Equal(b.Slot.Day) {
		return a.Slot.Day.Before(b.Slot.Day)
	}
	return a.Slot.Meal == planner.Lunch && b.Slot.Meal == planner.Dinner
}
