package planner

import (
	"math/rand/v2"
	"time"
)

// Candidate is a dish as seen by the draw: an identifier and its weights.
type Candidate struct {
	ID       int64
	Weight   int
	Seasonal [4]int // indexed by Season
}

// EffectiveWeight is the draw weight of the candidate during a season: the
// general weight times the seasonal one (RG-01).
func (c Candidate) EffectiveWeight(s Season) int {
	return c.Weight * c.Seasonal[s]
}

// Slot identifies one meal of one day.
type Slot struct {
	Day  time.Time
	Meal Meal
}

// Slots returns every slot of the given days for the given meals, in
// chronological order.
func Slots(days []time.Time, meals []Meal) []Slot {
	slots := make([]Slot, 0, len(days)*len(meals))
	for _, d := range days {
		for _, m := range Meals {
			for _, enabled := range meals {
				if m == enabled {
					slots = append(slots, Slot{Day: Day(d), Meal: m})
				}
			}
		}
	}
	return slots
}

// Draw picks a dish for every target slot.
//
// slots is the whole week in chronological order; plan holds the dishes
// already assigned to it. Dishes of non-target slots count as already served
// this week (RG-03), as do the dishes listed in avoid — typically the current
// dish of a slot being re-rolled. Target slots are drawn in chronological
// order. A slot no dish can fill is absent from the result (RG-05).
func Draw(rng *rand.Rand, dishes []Candidate, slots []Slot, plan map[Slot]int64, targets map[Slot]bool, avoid ...int64) map[Slot]int64 {
	used := make(map[int64]bool, len(slots)+len(avoid))
	for _, s := range slots {
		if id, ok := plan[s]; ok && !targets[s] {
			used[id] = true
		}
	}
	for _, id := range avoid {
		used[id] = true
	}

	picks := make(map[Slot]int64, len(targets))
	var prev int64
	for _, s := range slots {
		if !targets[s] {
			prev = plan[s]
			continue
		}
		id, ok := pick(rng, dishes, SeasonOf(s.Day), used, prev)
		if !ok {
			prev = 0
			continue
		}
		picks[s] = id
		used[id] = true
		prev = id
	}
	return picks
}

// pick draws one dish proportionally to its effective weight. It prefers a
// dish not served yet this week; once they are exhausted, any dish but the
// previous meal's; as a last resort, any dish (RG-04).
func pick(rng *rand.Rand, dishes []Candidate, season Season, used map[int64]bool, prev int64) (int64, bool) {
	filters := []func(Candidate) bool{
		func(c Candidate) bool { return !used[c.ID] },
		func(c Candidate) bool { return c.ID != prev },
		func(Candidate) bool { return true },
	}
	for _, keep := range filters {
		if id, ok := weighted(rng, dishes, season, keep); ok {
			return id, true
		}
	}
	return 0, false
}

func weighted(rng *rand.Rand, dishes []Candidate, season Season, keep func(Candidate) bool) (int64, bool) {
	total := 0
	for _, c := range dishes {
		if w := c.EffectiveWeight(season); w > 0 && keep(c) {
			total += w
		}
	}
	if total == 0 {
		return 0, false
	}
	n := rng.IntN(total)
	for _, c := range dishes {
		if w := c.EffectiveWeight(season); w > 0 && keep(c) {
			if n < w {
				return c.ID, true
			}
			n -= w
		}
	}
	return 0, false
}
