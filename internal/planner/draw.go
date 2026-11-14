package planner

import (
	"math/rand/v2"
	"time"
)

// Kind is the list an item belongs to.
type Kind int

const (
	Dish   Kind = iota // a full dish, a meal on its own
	Starch             // the components of a composed meal…
	Protein
	Vegetable
)

// Kinds lists every kind in display order.
var Kinds = []Kind{Dish, Starch, Protein, Vegetable}

// Components lists the kinds a composed meal is made of, in display order.
var Components = []Kind{Starch, Protein, Vegetable}

var kindKeys = [...]string{"dish", "starch", "protein", "vegetable"}

// KindCount is the number of kinds, for arrays indexed by Kind.
const KindCount = len(kindKeys)

// Key returns the stable identifier of the kind, used for storage, form
// fields and translation keys (kind.<key>).
func (k Kind) Key() string {
	return kindKeys[k]
}

// ParseKind validates a kind identifier.
func ParseKind(s string) (Kind, bool) {
	for _, k := range Kinds {
		if k.Key() == s {
			return k, true
		}
	}
	return 0, false
}

// Plate is what a planned meal holds: the item chosen for each kind, 0 for
// none. A full dish only sets Dish; a composed meal sets components only.
type Plate [KindCount]int64

// IsZero reports whether the plate holds nothing.
func (p Plate) IsZero() bool {
	return p == Plate{}
}

// Seasonal bonuses range from -MaxSeasonal to +MaxSeasonal: at -MaxSeasonal,
// an item is never drawn during the season, whatever its general weight.
const MaxSeasonal = 10

// Candidate is an item as seen by the draw: an identifier, its general
// weight and its seasonal bonuses.
type Candidate struct {
	ID       int64
	Weight   int
	Seasonal [4]int // bonus (or malus when negative), indexed by Season
}

// EffectiveWeight is the draw weight of the candidate during a season: the
// general weight plus the bonus of the season, never below zero (RG-01). The
// general weight comes first: at 0, no bonus brings the item back.
func (c Candidate) EffectiveWeight(s Season) int {
	if c.Weight <= 0 {
		return 0
	}
	return max(0, c.Weight+c.Seasonal[s])
}

// Pools holds the candidates of each kind.
type Pools [KindCount][]Candidate

// available reports whether kind k has a candidate during season s.
func (p *Pools) available(k Kind, s Season) bool {
	for _, c := range p[k] {
		if c.EffectiveWeight(s) > 0 {
			return true
		}
	}
	return false
}

// Slot identifies one meal of one day.
type Slot struct {
	Day  time.Time
	Meal Meal
}

// Drawer draws plates for a week.
type Drawer struct {
	Rng   *rand.Rand
	Pools Pools
	// ComposedShare is the probability, in percent, that a meal is composed
	// rather than a full dish (RG-08).
	ComposedShare int
	// Fixed holds the plates always served on a meal of the week (EF-18):
	// Draw serves them on its targets instead of drawing.
	Fixed map[Weekly]Plate
}

// Draw picks a plate for every target slot.
//
// slots is the whole week in chronological order; plan holds the plates
// already assigned to it. Items of non-target slots count as already served
// this week (RG-03), as do the items of avoid — typically the current plate
// of a slot being re-rolled. Target slots with a fixed plate get it first, so
// that the draw of the others avoids its items (RG-10); the other target
// slots are drawn in chronological order. A slot nothing can fill is absent
// from the result (RG-05).
func (d Drawer) Draw(slots []Slot, plan map[Slot]Plate, targets map[Slot]bool, avoid Plate) map[Slot]Plate {
	used := usedItems(slots, plan, targets, avoid)
	picks := make(map[Slot]Plate, len(targets))
	serve := func(s Slot, p Plate) {
		picks[s] = p
		for k, id := range p {
			if id != 0 {
				used[k][id] = true
			}
		}
	}
	for _, s := range slots {
		if f := d.Fixed[s.Weekly()]; targets[s] && !f.IsZero() {
			serve(s, f)
		}
	}
	var prev Plate
	for _, s := range slots {
		if !targets[s] {
			prev = plan[s]
			continue
		}
		if p, fixed := picks[s]; fixed {
			prev = p
			continue
		}
		p := d.plate(SeasonOf(s.Day), used, prev, avoid)
		prev = p
		if !p.IsZero() {
			serve(s, p)
		}
	}
	return picks
}

// DrawComponent re-draws one component of the plate of slot, leaving the
// others untouched (EF-09). It reports false when the kind has no candidate.
func (d Drawer) DrawComponent(slots []Slot, plan map[Slot]Plate, slot Slot, kind Kind) (int64, bool) {
	used := usedItems(slots, plan, map[Slot]bool{slot: true}, Plate{})
	var prev int64
	for _, s := range slots {
		if s == slot {
			break
		}
		prev = plan[s][kind]
	}
	return pick(d.Rng, d.Pools[kind], SeasonOf(slot.Day), used[kind], prev, plan[slot][kind])
}

func usedItems(slots []Slot, plan map[Slot]Plate, targets map[Slot]bool, avoid Plate) [KindCount]map[int64]bool {
	var used [KindCount]map[int64]bool
	for k := range used {
		used[k] = make(map[int64]bool)
		if avoid[k] != 0 {
			used[k][avoid[k]] = true
		}
	}
	for _, s := range slots {
		if p, ok := plan[s]; ok && !targets[s] {
			for k, id := range p {
				if id != 0 {
					used[k][id] = true
				}
			}
		}
	}
	return used
}

// plate draws one meal: its form first (RG-08), then its items (RG-09).
func (d Drawer) plate(season Season, used [KindCount]map[int64]bool, prev, avoid Plate) Plate {
	dishOK := d.Pools.available(Dish, season)
	composedOK := false
	for _, k := range Components {
		composedOK = composedOK || d.Pools.available(k, season)
	}

	var p Plate
	switch {
	case composedOK && (!dishOK || d.Rng.IntN(100) < d.ComposedShare):
		for _, k := range Components {
			if id, ok := pick(d.Rng, d.Pools[k], season, used[k], prev[k], avoid[k]); ok {
				p[k] = id
			}
		}
	case dishOK:
		p[Dish], _ = pick(d.Rng, d.Pools[Dish], season, used[Dish], prev[Dish], avoid[Dish])
	}
	return p
}

// pick draws one item proportionally to its effective weight. It prefers an
// item not served yet this week; once they are exhausted, any item but the
// previous meal's; as a last resort, any item (RG-04). The avoid item — the
// one being re-rolled — is only drawn again when it is the sole candidate.
func pick(rng *rand.Rand, pool []Candidate, season Season, used map[int64]bool, prev, avoid int64) (int64, bool) {
	filters := []func(Candidate) bool{
		func(c Candidate) bool { return !used[c.ID] && c.ID != avoid },
		func(c Candidate) bool { return c.ID != prev && c.ID != avoid },
		func(c Candidate) bool { return c.ID != avoid },
		func(Candidate) bool { return true },
	}
	for _, keep := range filters {
		if id, ok := weighted(rng, pool, season, keep); ok {
			return id, true
		}
	}
	return 0, false
}

func weighted(rng *rand.Rand, pool []Candidate, season Season, keep func(Candidate) bool) (int64, bool) {
	total := 0
	for _, c := range pool {
		if w := c.EffectiveWeight(season); w > 0 && keep(c) {
			total += w
		}
	}
	if total == 0 {
		return 0, false
	}
	n := rng.IntN(total)
	for _, c := range pool {
		if w := c.EffectiveWeight(season); w > 0 && keep(c) {
			if n < w {
				return c.ID, true
			}
			n -= w
		}
	}
	return 0, false
}
