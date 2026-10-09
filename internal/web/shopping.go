package web

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/shopping"
	"leblanc.io/open-go-menu/internal/store"
)

const (
	maxShoppingDays = 31
	maxNeedsLength  = 4096
)

var anchorRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

type shoppingView struct {
	page

	// Choosing the days of a new list (EF-22).
	Creating     bool
	Replaces     bool // a list exists, which the new one replaces
	Proposal     string
	ProposalFrom string
	ProposalTo   string
	From, To     string
	DatesErr     string

	// The list.
	Range         string
	Missing       []string
	Groups        []groupView
	Done          []lineView
	Empty         bool
	Suggestions   []string
	ExtraName     string
	ExtraQuantity string
	ExtraErr      string
}

type groupView struct {
	Anchor  string
	Name    string
	NoAisle bool
	Lines   []lineView
}

type lineView struct {
	ID          int64
	Name        string
	Action      string
	Anchor      string // the group to come back to
	Key         string
	Checked     bool
	IsExtra     bool
	Aisles      []store.Aisle // set to file an article without aisle
	Needs       []needView
	TickLabel   string
	AisleLabel  string
	RemoveLabel string
}

type needView struct {
	Text     string
	Quantity string
}

// slotLabel names a meal of a date: "Mardi 6 octobre — Soir".
func slotLabel(loc *i18n.Localizer, s planner.Slot) map[string]any {
	return map[string]any{
		"Day":  loc.T(weekdayKey(s.Day.Weekday())) + " " + dayMonth(loc, s.Day),
		"Meal": loc.T("meal." + string(s.Meal)),
	}
}

func (a *App) shopping(w http.ResponseWriter, r *http.Request) {
	from, to, ok, err := storeOf(r.Context()).ShoppingDays(r.Context())
	switch {
	case err != nil:
		a.fail(w, r, err)
	case !ok:
		a.creationPage(w, r, http.StatusOK, false, nil)
	default:
		a.listPage(w, r, http.StatusOK, from, to, nil)
	}
}

func (a *App) newShopping(w http.ResponseWriter, r *http.Request) {
	_, _, ok, err := storeOf(r.Context()).ShoppingDays(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.creationPage(w, r, http.StatusOK, ok, nil)
}

// completeWeek returns the last week, from the current one on, whose planned
// meals are all filled (EF-22).
func (a *App) completeWeek(ctx context.Context) (days []time.Time, ok bool, err error) {
	start, err := storeOf(ctx).WeekStart(ctx)
	if err != nil {
		return nil, false, err
	}
	sched, err := storeOf(ctx).Schedule(ctx)
	if err != nil {
		return nil, false, err
	}
	last, planned, err := storeOf(ctx).LastPlannedDay(ctx)
	if err != nil || !planned {
		return nil, false, err
	}
	first := planner.WeekStart(planner.Day(a.Now()), start)
	entries, err := storeOf(ctx).Entries(ctx, first, last)
	if err != nil {
		return nil, false, err
	}
	for week := first; !week.After(last); week = week.AddDate(0, 0, 7) {
		wd := planner.WeekDays(week)
		slots := sched.Slots(wd)
		complete := len(slots) > 0
		for _, s := range slots {
			if _, filled := entries[s]; !filled {
				complete = false
				break
			}
		}
		if complete {
			days, ok = wd, true
		}
	}
	return days, ok, nil
}

// creationPage renders the choice of the days of a new list; typed carries
// refused dates back.
func (a *App) creationPage(w http.ResponseWriter, r *http.Request, status int, replaces bool, typed *shoppingView) {
	ctx := r.Context()
	loc := a.localizer(r)
	v := shoppingView{page: a.newPage(w, r, loc, "shopping", "shopping.title"), Creating: true, Replaces: replaces}
	days, ok, err := a.completeWeek(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if ok {
		v.ProposalFrom = days[0].Format(planner.DateLayout)
		v.ProposalTo = days[6].Format(planner.DateLayout)
		v.Proposal = loc.T("shopping.proposal", map[string]any{"From": dayMonth(loc, days[0]), "To": dayMonthYear(loc, days[6])})
	} else {
		start, err := storeOf(ctx).WeekStart(ctx)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		days = planner.WeekDays(planner.WeekStart(planner.Day(a.Now()), start))
	}
	v.From, v.To = days[0].Format(planner.DateLayout), days[6].Format(planner.DateLayout)
	if typed != nil {
		v.From, v.To, v.DatesErr = typed.From, typed.To, typed.DatesErr
	}
	a.render(w, r, loc, status, "shopping", v)
}

func (a *App) startShopping(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	typed := shoppingView{From: strings.TrimSpace(r.PostFormValue("from")), To: strings.TrimSpace(r.PostFormValue("to"))}
	from, errFrom := planner.ParseDay(typed.From)
	to, errTo := planner.ParseDay(typed.To)
	switch {
	case errFrom != nil || errTo != nil:
		typed.DatesErr = loc.T("validation.dates_invalid")
	case to.Before(from):
		typed.DatesErr = loc.T("validation.dates_order")
	case to.Sub(from) >= maxShoppingDays*24*time.Hour:
		typed.DatesErr = loc.T("validation.dates_too_long", map[string]any{"Max": maxShoppingDays})
	}
	if typed.DatesErr != "" {
		_, _, replaces, err := storeOf(r.Context()).ShoppingDays(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
		a.creationPage(w, r, http.StatusUnprocessableEntity, replaces, &typed)
		return
	}
	if err := storeOf(r.Context()).StartShopping(r.Context(), from, to); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, "/shopping", "flash.shopping_started")
}

// listPage renders the list over its days; typed carries a refused manual
// addition back.
func (a *App) listPage(w http.ResponseWriter, r *http.Request, status int, from, to time.Time, typed *shoppingView) {
	ctx := r.Context()
	loc := a.localizer(r)
	sched, err := storeOf(ctx).Schedule(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rows, err := storeOf(ctx).ShoppingRows(ctx, from, to)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	ticked, err := storeOf(ctx).Ticked(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	aisles, err := storeOf(ctx).Aisles(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	missing, err := storeOf(ctx).DishesWithoutIngredients(ctx, from, to)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	suggestions, err := storeOf(ctx).ArticleSuggestions(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	choices, err := storeOf(ctx).AislesByName(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}

	// A meal no longer planned is hidden from the week: nothing to buy for
	// it (EF-13).
	kept := rows[:0]
	for _, row := range rows {
		if row.Need.IsExtra() || sched[row.Need.Slot.Weekly()] {
			kept = append(kept, row)
		}
	}
	shopAisles := make([]shopping.Aisle, len(aisles))
	for i, x := range aisles {
		shopAisles[i] = shopping.Aisle{ID: x.ID, Name: x.Name}
	}
	list := shopping.Build(kept, shopAisles, ticked)

	v := shoppingView{
		page:        a.newPage(w, r, loc, "shopping", "shopping.title"),
		Range:       loc.T("shopping.range", map[string]any{"From": dayMonth(loc, from), "To": dayMonthYear(loc, to)}),
		Empty:       list.IsEmpty(),
		Suggestions: suggestions,
	}
	if typed != nil {
		v.ExtraName, v.ExtraQuantity, v.ExtraErr = typed.ExtraName, typed.ExtraQuantity, typed.ExtraErr
	}
	for _, m := range missing {
		if sched[m.Slot.Weekly()] {
			label := slotLabel(loc, m.Slot)
			label["Dish"] = m.Name
			v.Missing = append(v.Missing, loc.T("shopping.missing_item", label))
		}
	}
	for _, g := range list.Groups {
		gv := groupView{Anchor: "aisle-" + strconv.FormatInt(g.Aisle.ID, 10), Name: g.Aisle.Name, NoAisle: g.Aisle.ID == 0}
		if gv.NoAisle {
			gv.Anchor, gv.Name = "aisle-none", loc.T("shopping.no_aisle")
		}
		for _, l := range g.Lines {
			lv := lineOf(loc, l, gv.Anchor)
			if gv.NoAisle {
				lv.Aisles = choices
			}
			gv.Lines = append(gv.Lines, lv)
		}
		v.Groups = append(v.Groups, gv)
	}
	for _, l := range list.Done {
		v.Done = append(v.Done, lineOf(loc, l, "done"))
	}
	a.render(w, r, loc, status, "shopping", v)
}

func lineOf(loc *i18n.Localizer, l shopping.Line, anchor string) lineView {
	name := map[string]any{"Name": l.Article.Name}
	lv := lineView{
		ID:          l.Article.ID,
		Name:        l.Article.Name,
		Action:      "/shopping/articles/" + strconv.FormatInt(l.Article.ID, 10),
		Anchor:      anchor,
		Key:         l.Key(),
		Checked:     l.Checked,
		TickLabel:   loc.T("shopping.tick_label", name),
		AisleLabel:  loc.T("shopping.aisle_label", name),
		RemoveLabel: loc.T("shopping.remove_label", name),
	}
	if l.Checked {
		lv.TickLabel = loc.T("shopping.untick_label", name)
	}
	for _, n := range l.Needs {
		nv := needView{Quantity: n.Quantity}
		switch {
		case n.IsExtra():
			lv.IsExtra = true
			nv.Text = loc.T("shopping.need_extra")
		case n.Dish != "":
			label := slotLabel(loc, n.Slot)
			label["Dish"] = n.Dish
			nv.Text = loc.T("shopping.need_dish", label)
		default:
			nv.Text = loc.T("shopping.need_meal", slotLabel(loc, n.Slot))
		}
		lv.Needs = append(lv.Needs, nv)
	}
	return lv
}

// currentList returns the days of the list, redirecting to its creation when
// there is none.
func (a *App) currentList(w http.ResponseWriter, r *http.Request) (from, to time.Time, ok bool) {
	from, to, ok, err := storeOf(r.Context()).ShoppingDays(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return from, to, false
	}
	if !ok {
		redirect(w, r, "/shopping", "")
	}
	return from, to, ok
}

// addExtra adds an article to the list by hand (EF-23).
func (a *App) addExtra(w http.ResponseWriter, r *http.Request) {
	from, to, ok := a.currentList(w, r)
	if !ok {
		return
	}
	loc := a.localizer(r)
	typed := shoppingView{
		ExtraName:     strings.TrimSpace(r.PostFormValue("name")),
		ExtraQuantity: strings.TrimSpace(r.PostFormValue("quantity")),
	}
	typed.ExtraErr = validName(loc, typed.ExtraName)
	if typed.ExtraErr == "" {
		typed.ExtraErr = validQuantity(loc, typed.ExtraQuantity)
	}
	if typed.ExtraErr == "" {
		err := storeOf(r.Context()).AddExtra(r.Context(), typed.ExtraName, typed.ExtraQuantity)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			typed.ExtraErr = loc.T("validation.extra_duplicate", map[string]any{"Name": typed.ExtraName})
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/shopping#extra", "flash.extra_added")
			return
		}
	}
	a.listPage(w, r, http.StatusUnprocessableEntity, from, to, &typed)
}

// shoppingLine acts on a line: tick it for the needs shown (RG-12), untick
// it, take back a manual addition, or file the article in an aisle.
func (a *App) shoppingLine(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := a.currentList(w, r); !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	back := "/shopping"
	if anchor := r.FormValue("anchor"); anchorRe.MatchString(anchor) {
		back += "#" + anchor
	}
	ctx := r.Context()
	var flash string
	switch r.FormValue("action") {
	case "tick":
		needs := r.FormValue("needs")
		if needs == "" || len(needs) > maxNeedsLength {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		err = storeOf(ctx).Tick(ctx, id, needs)
	case "untick":
		err = storeOf(ctx).Untick(ctx, id)
	case "remove":
		err, flash = storeOf(ctx).RemoveExtra(ctx, id), "flash.extra_removed"
	case "aisle":
		aisle, ok := a.aisleField(r, "aisle")
		if !ok {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		err, flash = storeOf(ctx).SetArticleAisle(ctx, id, aisle), "flash.article_filed"
	default:
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, back, flash)
	}
}
