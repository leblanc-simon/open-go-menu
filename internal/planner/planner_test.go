package planner

import (
	"math/rand/v2"
	"testing"
	"time"
)

func day(s string) time.Time {
	d, err := ParseDay(s)
	if err != nil {
		panic(err)
	}
	return d
}

func flat(id int64, w int) Candidate {
	return Candidate{ID: id, Weight: w, Seasonal: [4]int{5, 5, 5, 5}}
}

func TestSeasonOf(t *testing.T) {
	cases := map[string]Season{
		"2026-02-28": Winter, "2026-03-01": Spring, "2026-05-31": Spring,
		"2026-06-01": Summer, "2026-08-31": Summer, "2026-09-01": Autumn,
		"2026-11-30": Autumn, "2026-12-01": Winter, "2026-01-15": Winter,
	}
	for d, want := range cases {
		if got := SeasonOf(day(d)); got != want {
			t.Errorf("SeasonOf(%s) = %v, want %v", d, got, want)
		}
	}
}

func TestWeekStart(t *testing.T) {
	// 2026-10-08 is a Thursday.
	cases := []struct {
		start time.Weekday
		want  string
	}{
		{time.Monday, "2026-10-05"},
		{time.Thursday, "2026-10-08"},
		{time.Friday, "2026-10-02"},
		{time.Sunday, "2026-10-04"},
		{time.Saturday, "2026-10-03"},
	}
	for _, c := range cases {
		got := WeekStart(day("2026-10-08"), c.start).Format(DateLayout)
		if got != c.want {
			t.Errorf("WeekStart(%v) = %s, want %s", c.start, got, c.want)
		}
	}
}

func TestWeekStartAcrossDST(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skip("no tzdata")
	}
	// Sunday 2026-10-25 is the end of summer time in Paris.
	now := time.Date(2026, 10, 27, 0, 30, 0, 0, paris)
	if got := WeekStart(now, time.Monday).Format(DateLayout); got != "2026-10-26" {
		t.Errorf("got %s", got)
	}
	days := WeekDays(WeekStart(now, time.Saturday))
	if days[0].Format(DateLayout) != "2026-10-24" || days[6].Format(DateLayout) != "2026-10-30" {
		t.Errorf("days = %v", days)
	}
}

func TestSlotsOrder(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), []Meal{Dinner, Lunch})
	if len(slots) != 14 {
		t.Fatalf("len = %d", len(slots))
	}
	if slots[0].Meal != Lunch || slots[1].Meal != Dinner {
		t.Errorf("meals out of order: %v", slots[:2])
	}
	if got := Slots(WeekDays(day("2026-10-05")), []Meal{Dinner}); len(got) != 7 {
		t.Errorf("dinner only: len = %d", len(got))
	}
}

func targetsAll(slots []Slot) map[Slot]bool {
	t := make(map[Slot]bool, len(slots))
	for _, s := range slots {
		t[s] = true
	}
	return t
}

func dishes(cs ...Candidate) Pools {
	var p Pools
	p[Dish] = cs
	return p
}

func drawer(seed uint64, pools Pools, share int) Drawer {
	return Drawer{Rng: rand.New(rand.NewPCG(seed, 1)), Pools: pools, ComposedShare: share}
}

func TestDrawNoRepeatWhileCandidatesRemain(t *testing.T) {
	var cs []Candidate
	for i := int64(1); i <= 20; i++ {
		cs = append(cs, flat(i, 1+int(i)%10))
	}
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	for seed := uint64(0); seed < 50; seed++ {
		picks := drawer(seed, dishes(cs...), 50).Draw(slots, nil, targetsAll(slots), Plate{})
		if len(picks) != 14 {
			t.Fatalf("seed %d: %d picks", seed, len(picks))
		}
		seen := map[int64]bool{}
		for _, p := range picks {
			if seen[p[Dish]] {
				t.Fatalf("seed %d: dish %d repeated", seed, p[Dish])
			}
			seen[p[Dish]] = true
		}
	}
}

func TestDrawRespectsSeasonZero(t *testing.T) {
	summerless := Candidate{ID: 1, Weight: 10, Seasonal: [4]int{10, 0, 10, 10}}
	slots := Slots(WeekDays(day("2026-07-06")), Meals)
	picks := drawer(7, dishes(summerless, flat(2, 1)), 0).Draw(slots, nil, targetsAll(slots), Plate{})
	for s, p := range picks {
		if p[Dish] == 1 {
			t.Fatalf("summer-excluded dish drawn on %v", s)
		}
	}
	// Repetition kicks in once the only candidate is used (RG-04).
	if len(picks) != 14 {
		t.Errorf("picks = %d, want 14", len(picks))
	}
}

func TestDrawAvoidsConsecutiveRepeatWhenExhausted(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	picks := drawer(3, dishes(flat(1, 5), flat(2, 5)), 0).Draw(slots, nil, targetsAll(slots), Plate{})
	for i := 1; i < len(slots); i++ {
		if picks[slots[i]] == picks[slots[i-1]] {
			t.Fatalf("consecutive repeat at %v", slots[i])
		}
	}
}

func TestDrawNothingAvailable(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	if picks := drawer(1, dishes(flat(1, 0)), 50).Draw(slots, nil, targetsAll(slots), Plate{}); len(picks) != 0 {
		t.Errorf("picks = %v", picks)
	}
}

func TestDrawFillKeepsExistingAndAvoidsThem(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), []Meal{Dinner})[:3]
	plan := map[Slot]Plate{slots[0]: {Dish: 1}, slots[2]: {Dish: 3}}
	targets := map[Slot]bool{slots[1]: true}
	picks := drawer(9, dishes(flat(1, 5), flat(2, 5), flat(3, 5)), 0).Draw(slots, plan, targets, Plate{})
	if len(picks) != 1 || picks[slots[1]][Dish] != 2 {
		t.Errorf("picks = %v, want slot 1 -> 2", picks)
	}
}

func TestDrawAvoid(t *testing.T) {
	slots := Slots([]time.Time{day("2026-10-05")}, []Meal{Dinner})
	for seed := uint64(0); seed < 20; seed++ {
		picks := drawer(seed, dishes(flat(1, 5), flat(2, 5)), 0).
			Draw(slots, map[Slot]Plate{slots[0]: {Dish: 1}}, targetsAll(slots), Plate{Dish: 1})
		if picks[slots[0]][Dish] != 2 {
			t.Fatalf("reroll kept the avoided dish")
		}
	}
}

// components returns pools of n items for each component kind, with
// distinct identifiers: starches 10+, proteins 20+, vegetables 30+.
func components(n int) Pools {
	var p Pools
	for i, k := range Components {
		for j := range n {
			p[k] = append(p[k], flat(int64(10*(i+1)+j), 5))
		}
	}
	return p
}

func TestDrawComposedOnly(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	pools := components(3)
	pools[Dish] = []Candidate{flat(1, 5)}
	picks := drawer(5, pools, 100).Draw(slots, nil, targetsAll(slots), Plate{})
	if len(picks) != 14 {
		t.Fatalf("picks = %d", len(picks))
	}
	for s, p := range picks {
		if p[Dish] != 0 || p[Starch] == 0 || p[Protein] == 0 || p[Vegetable] == 0 {
			t.Fatalf("TR-09: %v -> %v", s, p)
		}
	}
	// Each list is tracked on its own: with 3 of each, no consecutive repeat.
	for i := 1; i < len(slots); i++ {
		for _, k := range Components {
			if picks[slots[i]][k] == picks[slots[i-1]][k] {
				t.Fatalf("consecutive %v repeat at %v", k, slots[i])
			}
		}
	}
}

func TestDrawFallsBackToAvailableForm(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	// TR-10: no full dish, share 0 → composed anyway.
	picks := drawer(1, components(5), 0).Draw(slots, nil, targetsAll(slots), Plate{})
	if len(picks) != 14 {
		t.Fatalf("composed fallback: %d picks", len(picks))
	}
	// No component, share 100 → full dishes anyway.
	picks = drawer(1, dishes(flat(1, 5), flat(2, 5)), 100).Draw(slots, nil, targetsAll(slots), Plate{})
	for _, p := range picks {
		if p[Dish] == 0 {
			t.Fatal("dish fallback: composed plate drawn")
		}
	}
}

func TestDrawMissingComponentKindIsOmitted(t *testing.T) {
	pools := components(5)
	pools[Starch] = nil
	slots := Slots([]time.Time{day("2026-10-05")}, []Meal{Dinner})
	p := drawer(2, pools, 100).Draw(slots, nil, targetsAll(slots), Plate{})[slots[0]]
	if p[Starch] != 0 || p[Protein] == 0 || p[Vegetable] == 0 {
		t.Errorf("RG-09: plate = %v", p)
	}
}

func TestDrawShare(t *testing.T) {
	pools := components(20)
	for i := range 20 {
		pools[Dish] = append(pools[Dish], flat(int64(100+i), 5))
	}
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	composed := 0
	for seed := uint64(0); seed < 100; seed++ {
		for _, p := range drawer(seed, pools, 25).Draw(slots, nil, targetsAll(slots), Plate{}) {
			if p[Dish] == 0 {
				composed++
			}
		}
	}
	if ratio := float64(composed) / 1400; ratio < 0.2 || ratio > 0.3 {
		t.Errorf("composed ratio = %.2f, want ~0.25", ratio)
	}
}

func TestDrawComponent(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), []Meal{Dinner})
	plan := map[Slot]Plate{slots[2]: {Starch: 10, Protein: 20, Vegetable: 30}}
	for seed := uint64(0); seed < 20; seed++ {
		id, ok := drawer(seed, components(2), 100).DrawComponent(slots, plan, slots[2], Vegetable)
		if !ok || id != 31 {
			t.Fatalf("got %d, %v; want the other vegetable", id, ok)
		}
	}
	if _, ok := drawer(1, dishes(flat(1, 5)), 100).DrawComponent(slots, plan, slots[2], Starch); ok {
		t.Error("drew from an empty list")
	}
}

// A re-roll changes the item even when its list is exhausted for the week.
func TestRerollWhenListExhausted(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	pools := components(3)
	for seed := uint64(0); seed < 50; seed++ {
		d := drawer(seed, pools, 100)
		plan := d.Draw(slots, nil, targetsAll(slots), Plate{})
		slot := slots[7]
		id, ok := d.DrawComponent(slots, plan, slot, Vegetable)
		if !ok || id == plan[slot][Vegetable] {
			t.Fatalf("seed %d: component re-roll kept %d", seed, id)
		}
		p := d.Draw(slots, plan, map[Slot]bool{slot: true}, plan[slot])[slot]
		for _, k := range Components {
			if p[k] == plan[slot][k] {
				t.Fatalf("seed %d: meal re-roll kept %v %d", seed, k, p[k])
			}
		}
	}
	// With a single candidate, re-rolling gives it back rather than nothing.
	single := dishes(flat(1, 5))
	one := Slots([]time.Time{day("2026-10-05")}, []Meal{Dinner})
	if p := drawer(1, single, 0).Draw(one, map[Slot]Plate{one[0]: {Dish: 1}}, targetsAll(one), Plate{Dish: 1}); p[one[0]][Dish] != 1 {
		t.Errorf("single candidate: %v", p)
	}
}

func TestParseKind(t *testing.T) {
	for _, k := range Kinds {
		if got, ok := ParseKind(k.Key()); !ok || got != k {
			t.Errorf("ParseKind(%q) = %v, %v", k.Key(), got, ok)
		}
	}
	if _, ok := ParseKind("dessert"); ok {
		t.Error("unknown kind accepted")
	}
}

func TestWeightedDistribution(t *testing.T) {
	pool := []Candidate{flat(1, 1), flat(2, 3)}
	rng := rand.New(rand.NewPCG(42, 42))
	counts := map[int64]int{}
	for range 10000 {
		id, _ := weighted(rng, pool, Autumn, func(Candidate) bool { return true })
		counts[id]++
	}
	ratio := float64(counts[2]) / float64(counts[1])
	if ratio < 2.7 || ratio > 3.3 {
		t.Errorf("ratio = %.2f, want ~3", ratio)
	}
}
