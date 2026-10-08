package web

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-menu/internal/planner"
	"leblanc.io/open-menu/internal/store"
)

// Week states, deciding which draw button the page offers.
const (
	weekEmpty   = "empty"
	weekPartial = "partial"
	weekFull    = "full"
)

type homeView struct {
	page
	Range      string
	Week       string // first day, YYYY-MM-DD
	Ref        string // ?week= as received, echoed back by the forms
	IsCurrent  bool
	PrevURL    string
	NextURL    string
	WeekStarts []option
	State      string
	DishCount  int
	Dishes     []store.Dish
	Days       []dayView
	Wide       bool // two meals a day: slots side by side
}

type option struct {
	Value    string
	Label    string
	Selected bool
}

type dayView struct {
	Anchor  string
	Weekday string
	Date    string
	Season  string
	Label   string // season label
	IsToday bool
	Slots   []slotView
}

type slotView struct {
	Meal      string
	MealLabel string
	Action    string
	Entry     *store.Entry
	ChooseFor string // accessible label of the dish picker
}

// week resolves the displayed week from ?week= (any day of it), defaulting
// to the current week.
func (a *App) week(r *http.Request, start time.Weekday) (first time.Time, ref string) {
	day, ref := a.refDay(r)
	return planner.WeekStart(day, start), ref
}

func weekURL(day time.Time) string {
	return "/?week=" + day.Format(planner.DateLayout)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := a.localizer(r)

	start, err := a.Store.WeekStart(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	meals, err := a.Store.Meals(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	dishes, err := a.Store.Dishes(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	first, ref := a.week(r, start)
	days := planner.WeekDays(first)
	entries, err := a.Store.Entries(ctx, days[0], days[6])
	if err != nil {
		a.fail(w, r, err)
		return
	}

	today := planner.Day(a.Now())
	v := homeView{
		page:      a.newPage(w, r, loc, "home", "home.title"),
		Range:     loc.T("home.week_range", map[string]any{"From": dayMonth(loc, days[0]), "To": dayMonthYear(loc, days[6])}),
		Week:      first.Format(planner.DateLayout),
		Ref:       ref,
		IsCurrent: first.Equal(planner.WeekStart(today, start)),
		PrevURL:   weekURL(first.AddDate(0, 0, -7)),
		NextURL:   weekURL(first.AddDate(0, 0, 7)),
		DishCount: len(dishes),
		Dishes:    dishes,
		Wide:      len(meals) > 1,
	}
	// Weekday options, starting on Monday as calendars do.
	for i := 1; i <= 7; i++ {
		d := time.Weekday(i % 7)
		v.WeekStarts = append(v.WeekStarts, option{
			Value: strconv.Itoa(int(d)), Label: loc.T(weekdayKey(d)), Selected: d == start,
		})
	}

	filled := 0
	for _, d := range days {
		season := planner.SeasonOf(d)
		dv := dayView{
			Anchor:  "day-" + d.Format(planner.DateLayout),
			Weekday: loc.T(weekdayKey(d.Weekday())),
			Date:    dayMonth(loc, d),
			Season:  season.Key(),
			Label:   loc.T("season." + season.Key()),
			IsToday: d.Equal(today),
		}
		for _, m := range meals {
			sv := slotView{
				Meal:      string(m),
				MealLabel: loc.T("meal." + string(m)),
				Action:    "/plan/" + d.Format(planner.DateLayout) + "/" + string(m),
			}
			sv.ChooseFor = loc.T("home.choose_for", map[string]any{"Meal": sv.MealLabel, "Day": dv.Weekday + " " + dv.Date})
			if e, ok := entries[planner.Slot{Day: d, Meal: m}]; ok {
				sv.Entry = &e
				filled++
			}
			dv.Slots = append(dv.Slots, sv)
		}
		v.Days = append(v.Days, dv)
	}
	switch {
	case filled == 0:
		v.State = weekEmpty
	case filled < len(days)*len(meals):
		v.State = weekPartial
	default:
		v.State = weekFull
	}

	a.render(w, r, loc, http.StatusOK, "home", v)
}

// backToWeek redirects to the week the form was posted from.
func backToWeek(w http.ResponseWriter, r *http.Request, ref, anchor, flashKey string) {
	to := "/"
	if ref != "" {
		to = "/?week=" + ref
	}
	if anchor != "" {
		to += "#" + anchor
	}
	redirect(w, r, to, flashKey)
}

func (a *App) setWeekStart(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.FormValue("week_start"))
	if err != nil || n < 0 || n > 6 {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	if err := a.Store.SetWeekStart(r.Context(), time.Weekday(n)); err != nil {
		a.fail(w, r, err)
		return
	}
	_, ref := a.week(r, time.Weekday(n))
	backToWeek(w, r, ref, "", "flash.week_start_saved")
}

// newRand returns a generator for one draw. math/rand/v2's top-level source
// is safe for concurrent use and randomly seeded.
func newRand() *rand.Rand {
	return rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
}

// candidates loads the live dishes for a draw.
func (a *App) candidates(r *http.Request) ([]planner.Candidate, error) {
	dishes, err := a.Store.Dishes(r.Context())
	if err != nil {
		return nil, err
	}
	cs := make([]planner.Candidate, len(dishes))
	for i, d := range dishes {
		cs[i] = d.Candidate()
	}
	return cs, nil
}

// refDay reads the ?week= day a form was posted from. It returns the day to
// work on (today when absent) and the value to echo back on redirect.
func (a *App) refDay(r *http.Request) (time.Time, string) {
	ref := r.FormValue("week")
	day, err := planner.ParseDay(ref)
	if err != nil {
		return planner.Day(a.Now()), ""
	}
	return day, ref
}

// weekSlots returns the slots of the week containing day and their current
// dishes.
func (a *App) weekSlots(ctx context.Context, day time.Time) ([]planner.Slot, map[planner.Slot]int64, error) {
	start, err := a.Store.WeekStart(ctx)
	if err != nil {
		return nil, nil, err
	}
	meals, err := a.Store.Meals(ctx)
	if err != nil {
		return nil, nil, err
	}
	days := planner.WeekDays(planner.WeekStart(day, start))
	entries, err := a.Store.Entries(ctx, days[0], days[6])
	if err != nil {
		return nil, nil, err
	}
	plan := make(map[planner.Slot]int64, len(entries))
	for s, e := range entries {
		plan[s] = e.DishID
	}
	return planner.Slots(days, meals), plan, nil
}

// generate draws the week: every meal (mode=all) or the empty ones only.
func (a *App) generate(w http.ResponseWriter, r *http.Request) {
	day, ref := a.refDay(r)
	slots, plan, err := a.weekSlots(r.Context(), day)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	all := r.FormValue("mode") == "all"
	targets := make(map[planner.Slot]bool, len(slots))
	for _, s := range slots {
		if _, filled := plan[s]; all || !filled {
			targets[s] = true
		}
	}
	cs, err := a.candidates(r)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	picks := planner.Draw(newRand(), cs, slots, plan, targets)
	if err := a.Store.SetEntries(r.Context(), picks); err != nil {
		a.fail(w, r, err)
		return
	}
	flash := "flash.week_generated"
	switch {
	case len(picks) == 0 && len(targets) > 0:
		flash = "flash.no_dish_available"
	case len(picks) < len(targets):
		flash = "flash.week_incomplete"
	}
	backToWeek(w, r, ref, "", flash)
}

// editSlot re-rolls, sets or clears one meal.
func (a *App) editSlot(w http.ResponseWriter, r *http.Request) {
	day, err := planner.ParseDay(r.PathValue("day"))
	meal, ok := planner.ParseMeal(r.PathValue("meal"))
	if err != nil || !ok {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	slot := planner.Slot{Day: day, Meal: meal}
	anchor := "day-" + day.Format(planner.DateLayout)
	_, ref := a.refDay(r)
	ctx := r.Context()

	switch r.FormValue("action") {
	case "clear":
		if err := a.Store.ClearEntries(ctx, slot); err != nil {
			a.fail(w, r, err)
			return
		}
		backToWeek(w, r, ref, anchor, "flash.meal_cleared")

	case "set":
		id, err := strconv.ParseInt(r.FormValue("dish_id"), 10, 64)
		if err != nil || !a.isLiveDish(r, id) {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		if err := a.Store.SetEntries(ctx, map[planner.Slot]int64{slot: id}); err != nil {
			a.fail(w, r, err)
			return
		}
		backToWeek(w, r, ref, anchor, "flash.meal_set")

	case "reroll":
		// The draw sees the whole week of the slot, so that it avoids the
		// dishes already planned around it.
		slots, plan, err := a.weekSlots(ctx, day)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		cs, err := a.candidates(r)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		var avoid []int64
		if id, ok := plan[slot]; ok {
			avoid = append(avoid, id)
		}
		if !containsSlot(slots, slot) {
			// A meal no longer planned (EF-13) is still re-rollable.
			slots = append(slots, slot)
		}
		picks := planner.Draw(newRand(), cs, slots, plan, map[planner.Slot]bool{slot: true}, avoid...)
		if len(picks) == 0 {
			backToWeek(w, r, ref, anchor, "flash.no_dish_available")
			return
		}
		if err := a.Store.SetEntries(ctx, picks); err != nil {
			a.fail(w, r, err)
			return
		}
		backToWeek(w, r, ref, anchor, "flash.meal_rerolled")

	default:
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
	}
}

func containsSlot(slots []planner.Slot, s planner.Slot) bool {
	for _, x := range slots {
		if x == s {
			return true
		}
	}
	return false
}

func (a *App) isLiveDish(r *http.Request, id int64) bool {
	dishes, err := a.Store.Dishes(r.Context())
	if err != nil {
		return false
	}
	for _, d := range dishes {
		if d.ID == id {
			return true
		}
	}
	return false
}

func weekdayKey(d time.Weekday) string {
	return "weekday." + [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}[d]
}

func monthKey(m time.Month) string {
	return "month." + [...]string{"", "january", "february", "march", "april", "may", "june",
		"july", "august", "september", "october", "november", "december"}[m]
}

// dayMonth formats "6 octobre" / "October 6": the order is the translation's.
func dayMonth(loc *i18n.Localizer, d time.Time) string {
	return loc.T("date.day_month", map[string]any{"Day": d.Day(), "Month": loc.T(monthKey(d.Month()))})
}

func dayMonthYear(loc *i18n.Localizer, d time.Time) string {
	return loc.T("date.day_month_year", map[string]any{"Day": d.Day(), "Month": loc.T(monthKey(d.Month())), "Year": d.Year()})
}
