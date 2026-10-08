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

func TestDrawNoRepeatWhileCandidatesRemain(t *testing.T) {
	var dishes []Candidate
	for i := int64(1); i <= 20; i++ {
		dishes = append(dishes, flat(i, 1+int(i)%10))
	}
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	for seed := uint64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewPCG(seed, 1))
		picks := Draw(rng, dishes, slots, nil, targetsAll(slots))
		if len(picks) != 14 {
			t.Fatalf("seed %d: %d picks", seed, len(picks))
		}
		seen := map[int64]bool{}
		for _, id := range picks {
			if seen[id] {
				t.Fatalf("seed %d: dish %d repeated", seed, id)
			}
			seen[id] = true
		}
	}
}

func TestDrawRespectsSeasonZero(t *testing.T) {
	summerless := Candidate{ID: 1, Weight: 10, Seasonal: [4]int{10, 0, 10, 10}}
	other := flat(2, 1)
	slots := Slots(WeekDays(day("2026-07-06")), Meals)
	rng := rand.New(rand.NewPCG(7, 7))
	picks := Draw(rng, []Candidate{summerless, other}, slots, nil, targetsAll(slots))
	for s, id := range picks {
		if id == 1 {
			t.Fatalf("summer-excluded dish drawn on %v", s)
		}
	}
	// Repetition kicks in once the only candidate is used (RG-04).
	if len(picks) != 14 {
		t.Errorf("picks = %d, want 14", len(picks))
	}
}

func TestDrawAvoidsConsecutiveRepeatWhenExhausted(t *testing.T) {
	dishes := []Candidate{flat(1, 5), flat(2, 5)}
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	rng := rand.New(rand.NewPCG(3, 3))
	picks := Draw(rng, dishes, slots, nil, targetsAll(slots))
	for i := 1; i < len(slots); i++ {
		if picks[slots[i]] == picks[slots[i-1]] {
			t.Fatalf("consecutive repeat at %v", slots[i])
		}
	}
}

func TestDrawNothingAvailable(t *testing.T) {
	slots := Slots(WeekDays(day("2026-10-05")), Meals)
	rng := rand.New(rand.NewPCG(1, 1))
	picks := Draw(rng, []Candidate{flat(1, 0)}, slots, nil, targetsAll(slots))
	if len(picks) != 0 {
		t.Errorf("picks = %v", picks)
	}
}

func TestDrawFillKeepsExistingAndAvoidsThem(t *testing.T) {
	dishes := []Candidate{flat(1, 5), flat(2, 5), flat(3, 5)}
	slots := Slots(WeekDays(day("2026-10-05")), []Meal{Dinner})[:3]
	plan := map[Slot]int64{slots[0]: 1, slots[2]: 3}
	targets := map[Slot]bool{slots[1]: true}
	rng := rand.New(rand.NewPCG(9, 9))
	picks := Draw(rng, dishes, slots, plan, targets)
	if len(picks) != 1 || picks[slots[1]] != 2 {
		t.Errorf("picks = %v, want slot 1 -> 2", picks)
	}
}

func TestDrawAvoid(t *testing.T) {
	dishes := []Candidate{flat(1, 5), flat(2, 5)}
	slots := Slots([]time.Time{day("2026-10-05")}, []Meal{Dinner})
	for seed := uint64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewPCG(seed, 0))
		picks := Draw(rng, dishes, slots, map[Slot]int64{slots[0]: 1}, targetsAll(slots), 1)
		if picks[slots[0]] != 2 {
			t.Fatalf("reroll kept the avoided dish")
		}
	}
}

func TestWeightedDistribution(t *testing.T) {
	dishes := []Candidate{flat(1, 1), flat(2, 3)}
	rng := rand.New(rand.NewPCG(42, 42))
	counts := map[int64]int{}
	for range 10000 {
		id, _ := weighted(rng, dishes, Autumn, func(Candidate) bool { return true })
		counts[id]++
	}
	ratio := float64(counts[2]) / float64(counts[1])
	if ratio < 2.7 || ratio > 3.3 {
		t.Errorf("ratio = %.2f, want ~3", ratio)
	}
}
