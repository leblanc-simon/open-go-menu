package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/store"
)

const (
	maxNameLength = 100
	maxWeight     = 10
)

// listSlugs maps each kind to the URL segment of its settings tab.
var listSlugs = [planner.KindCount]string{"dishes", "starches", "proteins", "vegetables"}

func listURL(k planner.Kind) string {
	return "/settings/" + listSlugs[k]
}

func parseList(slug string) (planner.Kind, bool) {
	for k, s := range listSlugs {
		if s == slug {
			return planner.Kind(k), true
		}
	}
	return 0, false
}

type settingsView struct {
	page
	Tabs []tab
	View string // list, meals, fixed, dish, articles or aisles

	// The tab of a list.
	Kind    string
	ListURL string
	Seasons []seasonColumn
	Rows    []itemForm
	New     itemForm
	SaveErr string
	Weight  string // default weight, shown in the explanation

	// The meals tab.
	Schedule []scheduleDay
	Share    string
	MealsErr string
	ShareErr string

	Fixed fixedView
	Dish  dishView

	// The tabs of the articles and the aisles.
	Names   []nameRow
	NewName nameRow
	Aisles  []store.Aisle // the choices of an aisle select
}

// scheduleDay is a row of the meals tab: a weekday and its meals.
type scheduleDay struct {
	Anchor string
	Label  string
	Meals  []scheduleMeal
}

type scheduleMeal struct {
	Key        string // wednesday/lunch
	Label      string
	CheckLabel string // accessible label of the box: "Wednesday lunch"
	Planned    bool
	FixedNames []string
	EditURL    string
	EditLabel  string
}

// fixedView is the page fixing the meal of a weekday (EF-18).
type fixedView struct {
	Title   string
	Action  string
	Plate   planner.Plate
	IsSet   bool
	Choices []choiceList
}

// dishView is the page of the ingredients of a dish (EF-19).
type dishView struct {
	Title       string
	Action      string
	Ingredients []ingredientRow
	Suggestions []string
	New         string
	NewQuantity string
	NewAisle    int64
	NewErr      string
	SaveErr     string
}

type ingredientRow struct {
	ID          int64
	Name        string
	Quantity    string
	Invalid     bool
	RemoveLabel string
}

const maxQuantityLength = 50

type tab struct {
	URL     string
	Label   string
	Current bool
}

type seasonColumn struct {
	Key   string
	Label string
}

// itemForm is an item as typed in a form: raw values, so that an invalid
// input is displayed back as is, with its translated errors (EI-05).
type itemForm struct {
	ID       int64
	Name     string
	Weight   string
	Seasonal [4]string
	Errors   []string
	Invalid  map[string]bool // field name → in error

	Ingredients string // link to the ingredients of a dish
}

func formOf(it store.Item) itemForm {
	f := itemForm{ID: it.ID, Name: it.Name, Weight: strconv.Itoa(it.Weight)}
	for _, s := range planner.Seasons {
		f.Seasonal[s] = strconv.Itoa(it.Seasonal[s])
	}
	return f
}

// fieldName returns the form field of an item row; it is the bare base for
// the creation form (id 0).
func fieldName(base string, id int64) string {
	if id == 0 {
		return base
	}
	return base + "_" + strconv.FormatInt(id, 10)
}

// parseItemForm reads and validates an item from the posted form.
func parseItemForm(r *http.Request, kind planner.Kind, id int64, loc *i18n.Localizer) (itemForm, store.Item, bool) {
	f := itemForm{
		ID:      id,
		Name:    strings.TrimSpace(r.PostFormValue(fieldName("name", id))),
		Weight:  strings.TrimSpace(r.PostFormValue(fieldName("weight", id))),
		Invalid: map[string]bool{},
	}
	it := store.Item{ID: id, Kind: kind, Name: f.Name}

	switch {
	case f.Name == "":
		f.fail("name", loc.T("validation.name_required"))
	case utf8.RuneCountInString(f.Name) > maxNameLength:
		f.fail("name", loc.T("validation.name_too_long", map[string]any{"Max": maxNameLength}))
	}

	weight := func(field, raw, label string) int {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > maxWeight {
			f.fail(field, loc.T("validation.weight_range", map[string]any{"Field": label, "Max": maxWeight}))
		}
		return n
	}
	it.Weight = weight("weight", f.Weight, loc.T("settings.weight"))
	for _, s := range planner.Seasons {
		f.Seasonal[s] = strings.TrimSpace(r.PostFormValue(fieldName(s.Key(), id)))
		it.Seasonal[s] = weight(s.Key(), f.Seasonal[s], loc.T("season."+s.Key()))
	}
	return f, it, len(f.Errors) == 0
}

func (f *itemForm) fail(field, msg string) {
	f.Invalid[field] = true
	f.Errors = append(f.Errors, msg)
}

// basePage fills the layout and the tabs; current is a list slug or "meals".
func (a *App) basePage(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer, current string) settingsView {
	v := settingsView{page: a.newPage(w, r, loc, "settings", "settings.title")}
	for _, k := range planner.Kinds {
		v.Tabs = append(v.Tabs, tab{URL: listURL(k), Label: loc.T("settings.tab." + k.Key()), Current: listSlugs[k] == current})
	}
	for _, t := range []string{"articles", "aisles", "meals"} {
		v.Tabs = append(v.Tabs, tab{URL: "/settings/" + t, Label: loc.T("settings.tab." + t), Current: current == t})
	}
	return v
}

// listView builds the tab of a list; rows and newItem override the stored
// values when a form is redisplayed with errors.
func (a *App) listView(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer, kind planner.Kind, rows []itemForm, newItem *itemForm) (settingsView, error) {
	v := a.basePage(w, r, loc, listSlugs[kind])
	v.View = "list"
	v.Kind = kind.Key()
	v.ListURL = listURL(kind)
	v.Rows = rows
	v.Weight = strconv.Itoa(store.DefaultWeight)
	for _, s := range planner.Seasons {
		v.Seasons = append(v.Seasons, seasonColumn{Key: s.Key(), Label: loc.T("season." + s.Key())})
	}
	if rows == nil {
		items, err := a.Store.Items(r.Context(), kind)
		if err != nil {
			return v, err
		}
		for _, it := range items {
			v.Rows = append(v.Rows, formOf(it))
		}
	}
	if newItem != nil {
		v.New = *newItem
	} else {
		v.New = formOf(store.NewItem(kind, ""))
	}
	if kind == planner.Dish {
		counts, err := a.Store.IngredientCounts(r.Context())
		if err != nil {
			return v, err
		}
		for i := range v.Rows {
			v.Rows[i].Ingredients = loc.T("settings.ingredients.none")
			if n := counts[v.Rows[i].ID]; n > 0 {
				v.Rows[i].Ingredients = loc.Tn("settings.ingredients.count", n)
			}
		}
	}
	return v, nil
}

func (a *App) renderList(w http.ResponseWriter, r *http.Request, kind planner.Kind) {
	loc := a.localizer(r)
	v, err := a.listView(w, r, loc, kind, nil, nil)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, loc, http.StatusOK, "settings", v)
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	a.renderList(w, r, planner.Dish)
}

func (a *App) settingsList(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseList(r.PathValue("list"))
	if !ok {
		a.notFound(w, r)
		return
	}
	a.renderList(w, r, kind)
}

func (a *App) addItem(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseList(r.PathValue("list"))
	if !ok {
		a.notFound(w, r)
		return
	}
	loc := a.localizer(r)
	f, it, ok := parseItemForm(r, kind, 0, loc)
	if ok {
		_, err := a.Store.CreateItem(r.Context(), it)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			f.fail("name", loc.T("validation.name_duplicate", map[string]any{"Name": it.Name}))
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, listURL(kind)+"#add", "flash.item_added")
			return
		}
	}
	v, err := a.listView(w, r, loc, kind, nil, &f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, loc, http.StatusUnprocessableEntity, "settings", v)
}

func (a *App) saveItems(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseList(r.PathValue("list"))
	if !ok {
		a.notFound(w, r)
		return
	}
	loc := a.localizer(r)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}

	var (
		rows  []itemForm
		items []store.Item
		valid = true
		names = map[string]bool{} // lower-cased names seen so far
	)
	for _, raw := range r.PostForm["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		f, it, ok := parseItemForm(r, kind, id, loc)
		if ok {
			key := strings.ToLower(it.Name)
			if names[key] {
				f.fail("name", loc.T("validation.name_duplicate", map[string]any{"Name": it.Name}))
				ok = false
			}
			names[key] = true
		}
		valid = valid && ok
		rows = append(rows, f)
		items = append(items, it)
	}

	var saveErr string
	if valid {
		err := a.Store.UpdateItems(r.Context(), kind, items)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			// Clashes with an item added meanwhile, absent from this form.
			saveErr = loc.T("validation.name_taken")
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, listURL(kind), "flash.items_saved")
			return
		}
	}

	v, err := a.listView(w, r, loc, kind, rows, nil)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v.SaveErr = saveErr
	if saveErr == "" {
		v.SaveErr = loc.T("validation.form_invalid")
	}
	a.render(w, r, loc, http.StatusUnprocessableEntity, "settings", v)
}

func (a *App) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	it, err := a.Store.Item(r.Context(), id)
	if err == nil {
		err = a.Store.DeleteItem(r.Context(), id)
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.renderError(w, r, http.StatusNotFound, "error.not_found")
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, listURL(it.Kind), "flash.item_deleted")
	}
}

// mealsView builds the meals tab from the stored settings: one row per
// weekday, from the first day of the week.
func (a *App) mealsView(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer) (settingsView, error) {
	ctx := r.Context()
	v := a.basePage(w, r, loc, "meals")
	v.View = "meals"
	start, err := a.Store.WeekStart(ctx)
	if err != nil {
		return v, err
	}
	sched, err := a.Store.Schedule(ctx)
	if err != nil {
		return v, err
	}
	fixed, err := a.Store.FixedPlates(ctx)
	if err != nil {
		return v, err
	}
	for i := range 7 {
		d := time.Weekday((int(start) + i) % 7)
		day := scheduleDay{Anchor: "weekday-" + weekdayNames[d], Label: loc.T(weekdayKey(d))}
		for _, m := range planner.Meals {
			wk := planner.Weekly{Weekday: d, Meal: m}
			labels := map[string]any{"Day": day.Label, "Meal": loc.T("meal." + string(m))}
			sm := scheduleMeal{
				Key:        weeklyKey(wk),
				Label:      loc.T("meal." + string(m)),
				CheckLabel: loc.T("settings.schedule.planned", labels),
				Planned:    sched[wk],
				EditURL:    "/settings/meals/" + weeklyKey(wk),
				EditLabel:  loc.T("settings.schedule.fix_label", labels),
			}
			if e, ok := fixed[wk]; ok {
				sm.EditLabel = loc.T("settings.schedule.change_label", labels)
				for _, k := range planner.Kinds {
					if e.Plate[k] != 0 {
						sm.FixedNames = append(sm.FixedNames, e.Names[k])
					}
				}
			}
			day.Meals = append(day.Meals, sm)
		}
		v.Schedule = append(v.Schedule, day)
	}
	share, err := a.Store.ComposedShare(ctx)
	v.Share = strconv.Itoa(share)
	return v, err
}

func (a *App) mealsSettings(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	v, err := a.mealsView(w, r, loc)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, loc, http.StatusOK, "settings", v)
}

func (a *App) saveMeals(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	sched := planner.Schedule{}
	for _, raw := range r.PostForm["slot"] {
		day, meal, _ := strings.Cut(raw, "/")
		wk, ok := parseWeekly(day, meal)
		if !ok {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		sched[wk] = true
	}
	rawShare := strings.TrimSpace(r.PostFormValue("composed_share"))
	share, err := strconv.Atoi(rawShare)
	shareOK := err == nil && share >= 0 && share <= 100

	if len(sched) == 0 || !shareOK {
		v, err := a.mealsView(w, r, loc)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		// Show the boxes as they were ticked (EI-05).
		for i := range v.Schedule {
			for j := range v.Schedule[i].Meals {
				m := &v.Schedule[i].Meals[j]
				day, meal, _ := strings.Cut(m.Key, "/")
				wk, _ := parseWeekly(day, meal)
				m.Planned = sched[wk]
			}
		}
		if len(sched) == 0 {
			v.MealsErr = loc.T("validation.meals_required")
		}
		if !shareOK {
			v.ShareErr = loc.T("validation.share_range")
			v.Share = rawShare
		}
		a.render(w, r, loc, http.StatusUnprocessableEntity, "settings", v)
		return
	}
	if err := a.Store.SetSchedule(r.Context(), sched); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Store.SetComposedShare(r.Context(), share); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, "/settings/meals", "flash.meals_saved")
}

// fixedMeal shows the page fixing the meal of a weekday (EF-18).
func (a *App) fixedMeal(w http.ResponseWriter, r *http.Request) {
	wk, ok := parseWeekly(r.PathValue("weekday"), r.PathValue("meal"))
	if !ok {
		a.notFound(w, r)
		return
	}
	ctx := r.Context()
	loc := a.localizer(r)
	items, err := a.Store.AllItems(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	fixed, err := a.Store.FixedPlates(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.basePage(w, r, loc, "meals")
	v.View = "fixed"
	plate := fixed[wk].Plate
	v.Fixed = fixedView{
		Title:   loc.T("settings.fixed.title", map[string]any{"Day": loc.T(weekdayKey(wk.Weekday)), "Meal": loc.T("meal." + string(wk.Meal))}),
		Action:  "/settings/meals/" + weeklyKey(wk),
		Plate:   plate,
		IsSet:   !plate.IsZero(),
		Choices: choices(loc, items),
	}
	a.render(w, r, loc, http.StatusOK, "settings", v)
}

// saveFixedMeal fixes the meal of a weekday (action=set) or returns it to
// the draw (action=clear).
func (a *App) saveFixedMeal(w http.ResponseWriter, r *http.Request) {
	wk, ok := parseWeekly(r.PathValue("weekday"), r.PathValue("meal"))
	if !ok {
		a.notFound(w, r)
		return
	}
	var plate planner.Plate
	flash := "flash.fixed_cleared"
	switch r.FormValue("action") {
	case "clear":
	case "set":
		if plate, ok = a.chosenPlate(r); !ok {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		if plate.IsZero() {
			redirect(w, r, "/settings/meals/"+weeklyKey(wk), "flash.nothing_chosen")
			return
		}
		flash = "flash.fixed_saved"
	default:
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	if err := a.Store.SetFixedPlate(r.Context(), wk, plate); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, "/settings/meals#weekday-"+weekdayNames[wk.Weekday], flash)
}

// liveDish returns the live full dish named by the {id} path segment.
func (a *App) liveDish(r *http.Request) (store.Item, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return store.Item{}, store.ErrNotFound
	}
	it, err := a.Store.Item(r.Context(), id)
	if err == nil && it.Kind != planner.Dish {
		err = store.ErrNotFound
	}
	return it, err
}

// dishPage renders the ingredients of a dish. form carries a refused input
// back (EI-05): the addition, or the quantities typed in the list.
func (a *App) dishPage(w http.ResponseWriter, r *http.Request, dish store.Item, status int, form *dishView) {
	ctx := r.Context()
	loc := a.localizer(r)
	ingredients, err := a.Store.DishIngredients(ctx, dish.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	suggestions, err := a.Store.ArticleSuggestions(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	aisles, err := a.Store.AislesByName(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.basePage(w, r, loc, listSlugs[planner.Dish])
	v.View = "dish"
	v.Aisles = aisles
	if form != nil {
		v.Dish = *form
	}
	v.Dish.Title = loc.T("settings.dish.title", map[string]any{"Name": dish.Name})
	v.Dish.Action = dishURL(dish.ID) + "/ingredients"
	v.Dish.Suggestions = suggestions
	typed := map[int64]ingredientRow{}
	for _, row := range v.Dish.Ingredients {
		typed[row.ID] = row
	}
	v.Dish.Ingredients = nil
	for _, g := range ingredients {
		row := ingredientRow{ID: g.ArticleID, Name: g.Name, Quantity: g.Quantity,
			RemoveLabel: loc.T("settings.dish.remove_label", map[string]any{"Name": g.Name})}
		if t, ok := typed[g.ArticleID]; ok {
			row.Quantity, row.Invalid = t.Quantity, t.Invalid
		}
		v.Dish.Ingredients = append(v.Dish.Ingredients, row)
	}
	a.render(w, r, loc, status, "settings", v)
}

func dishURL(id int64) string {
	return "/settings/items/" + strconv.FormatInt(id, 10)
}

// validName checks a name typed in a form; it returns the error to show, or
// "" when the name is fine.
func validName(loc *i18n.Localizer, name string) string {
	switch {
	case name == "":
		return loc.T("validation.name_required")
	case utf8.RuneCountInString(name) > maxNameLength:
		return loc.T("validation.name_too_long", map[string]any{"Max": maxNameLength})
	}
	return ""
}

func validQuantity(loc *i18n.Localizer, q string) string {
	if utf8.RuneCountInString(q) > maxQuantityLength {
		return loc.T("validation.quantity_too_long", map[string]any{"Max": maxQuantityLength})
	}
	return ""
}

// aisleField reads an aisle select: 0 for none, else an existing aisle.
func (a *App) aisleField(r *http.Request, field string) (int64, bool) {
	raw := r.PostFormValue(field)
	if raw == "" || raw == "0" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	aisles, err := a.Store.Aisles(r.Context())
	if err != nil {
		return 0, false
	}
	for _, x := range aisles {
		if x.ID == id {
			return id, true
		}
	}
	return 0, false
}

func (a *App) dishIngredients(w http.ResponseWriter, r *http.Request) {
	dish, err := a.liveDish(r)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
	case err != nil:
		a.fail(w, r, err)
	default:
		a.dishPage(w, r, dish, http.StatusOK, nil)
	}
}

func (a *App) addIngredient(w http.ResponseWriter, r *http.Request) {
	dish, err := a.liveDish(r)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	loc := a.localizer(r)
	f := dishView{
		New:         strings.TrimSpace(r.PostFormValue("name")),
		NewQuantity: strings.TrimSpace(r.PostFormValue("quantity")),
	}
	aisle, ok := a.aisleField(r, "aisle")
	if !ok {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	f.NewAisle = aisle
	f.NewErr = validName(loc, f.New)
	if f.NewErr == "" {
		f.NewErr = validQuantity(loc, f.NewQuantity)
	}
	if f.NewErr == "" {
		err := a.Store.AddIngredient(r.Context(), dish.ID, f.New, f.NewQuantity, aisle)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			f.NewErr = loc.T("validation.ingredient_duplicate", map[string]any{"Name": f.New})
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, dishURL(dish.ID)+"#add-ingredient", "flash.ingredient_added")
			return
		}
	}
	a.dishPage(w, r, dish, http.StatusUnprocessableEntity, &f)
}

// saveQuantities saves the quantities of the ingredients of a dish.
func (a *App) saveQuantities(w http.ResponseWriter, r *http.Request) {
	dish, err := a.liveDish(r)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
		return
	case err != nil:
		a.fail(w, r, err)
		return
	}
	loc := a.localizer(r)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	var f dishView
	quantities := map[int64]string{}
	for _, raw := range r.PostForm["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		q := strings.TrimSpace(r.PostFormValue(fieldName("quantity", id)))
		row := ingredientRow{ID: id, Quantity: q}
		if msg := validQuantity(loc, q); msg != "" {
			row.Invalid = true
			f.SaveErr = msg
		}
		f.Ingredients = append(f.Ingredients, row)
		quantities[id] = q
	}
	if f.SaveErr != "" {
		a.dishPage(w, r, dish, http.StatusUnprocessableEntity, &f)
		return
	}
	if err := a.Store.SetQuantities(r.Context(), dish.ID, quantities); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, dishURL(dish.ID), "flash.quantities_saved")
}

func (a *App) removeIngredient(w http.ResponseWriter, r *http.Request) {
	dish, err := a.liveDish(r)
	if err == nil {
		var ingredient int64
		if ingredient, err = strconv.ParseInt(r.PathValue("article"), 10, 64); err != nil {
			err = store.ErrNotFound
		} else {
			err = a.Store.RemoveIngredient(r.Context(), dish.ID, ingredient)
		}
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, dishURL(dish.ID), "flash.ingredient_removed")
	}
}
