package planner

import "time"

// DateLayout is the storage and URL format of a day.
const DateLayout = "2006-01-02"

// Season is a meteorological season of the northern hemisphere.
type Season int

const (
	Spring Season = iota
	Summer
	Autumn
	Winter
)

// Seasons lists every season in display order.
var Seasons = []Season{Spring, Summer, Autumn, Winter}

// Key returns the stable identifier of the season, used for form fields and
// translation keys (season.<key>).
func (s Season) Key() string {
	switch s {
	case Spring:
		return "spring"
	case Summer:
		return "summer"
	case Autumn:
		return "autumn"
	default:
		return "winter"
	}
}

// SeasonOf returns the meteorological season of a day: winter is
// December–February, spring March–May, summer June–August, autumn
// September–November (RG-06).
func SeasonOf(day time.Time) Season {
	switch day.Month() {
	case time.March, time.April, time.May:
		return Spring
	case time.June, time.July, time.August:
		return Summer
	case time.September, time.October, time.November:
		return Autumn
	default:
		return Winter
	}
}

// Meal is a slot of a day.
type Meal string

const (
	Lunch  Meal = "lunch"
	Dinner Meal = "dinner"
)

// Meals lists every meal in chronological order.
var Meals = []Meal{Lunch, Dinner}

// ParseMeal validates a meal identifier coming from a form or a URL.
func ParseMeal(s string) (Meal, bool) {
	for _, m := range Meals {
		if string(m) == s {
			return m, true
		}
	}
	return "", false
}

// Weekly is a meal of the week whatever its date: "Wednesday lunch".
type Weekly struct {
	Weekday time.Weekday
	Meal    Meal
}

// Weekly returns the meal of the week the slot falls on.
func (s Slot) Weekly() Weekly {
	return Weekly{Weekday: s.Day.Weekday(), Meal: s.Meal}
}

// Schedule tells which meals are planned on each weekday (EF-13).
type Schedule map[Weekly]bool

// FullSchedule plans every meal of every day.
func FullSchedule() Schedule {
	s := make(Schedule, 7*len(Meals))
	for d := time.Sunday; d <= time.Saturday; d++ {
		for _, m := range Meals {
			s[Weekly{Weekday: d, Meal: m}] = true
		}
	}
	return s
}

// Meals returns the meals planned on a weekday, in chronological order.
func (s Schedule) Meals(d time.Weekday) []Meal {
	var meals []Meal
	for _, m := range Meals {
		if s[Weekly{Weekday: d, Meal: m}] {
			meals = append(meals, m)
		}
	}
	return meals
}

// Slots returns the planned slots of the given days, in chronological order.
func (s Schedule) Slots(days []time.Time) []Slot {
	var slots []Slot
	for _, d := range days {
		for _, m := range s.Meals(d.Weekday()) {
			slots = append(slots, Slot{Day: Day(d), Meal: m})
		}
	}
	return slots
}

// Day truncates t to its calendar day, expressed at midnight UTC so that day
// arithmetic never trips over daylight saving changes.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseDay parses a YYYY-MM-DD day.
func ParseDay(s string) (time.Time, error) {
	return time.Parse(DateLayout, s)
}

// WeekStart returns the first day of the week containing day, weeks starting
// on the given weekday.
func WeekStart(day time.Time, start time.Weekday) time.Time {
	day = Day(day)
	offset := (int(day.Weekday()) - int(start) + 7) % 7
	return day.AddDate(0, 0, -offset)
}

// WeekDays returns the 7 days of the week beginning at first.
func WeekDays(first time.Time) []time.Time {
	days := make([]time.Time, 7)
	for i := range days {
		days[i] = first.AddDate(0, 0, i)
	}
	return days
}
