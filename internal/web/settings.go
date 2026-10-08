package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
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

	// The tab of a list.
	Kind    string
	ListURL string
	Seasons []seasonColumn
	Rows    []itemForm
	New     itemForm
	SaveErr string
	Weight  string // default weight, shown in the explanation

	// The meals tab.
	Meals    []option
	Share    string
	MealsErr string
	ShareErr string
}

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
	v.Tabs = append(v.Tabs, tab{URL: "/settings/meals", Label: loc.T("settings.tab.meals"), Current: current == "meals"})
	return v
}

// listView builds the tab of a list; rows and newItem override the stored
// values when a form is redisplayed with errors.
func (a *App) listView(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer, kind planner.Kind, rows []itemForm, newItem *itemForm) (settingsView, error) {
	v := a.basePage(w, r, loc, listSlugs[kind])
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

// mealsView builds the meals tab from the stored settings.
func (a *App) mealsView(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer) (settingsView, error) {
	ctx := r.Context()
	v := a.basePage(w, r, loc, "meals")
	meals, err := a.Store.Meals(ctx)
	if err != nil {
		return v, err
	}
	for _, m := range planner.Meals {
		o := option{Value: string(m), Label: loc.T("meal." + string(m))}
		for _, on := range meals {
			o.Selected = o.Selected || on == m
		}
		v.Meals = append(v.Meals, o)
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
	var meals []planner.Meal
	for _, raw := range r.PostForm["meal"] {
		m, ok := planner.ParseMeal(raw)
		if !ok {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		meals = append(meals, m)
	}
	rawShare := strings.TrimSpace(r.PostFormValue("composed_share"))
	share, err := strconv.Atoi(rawShare)
	shareOK := err == nil && share >= 0 && share <= 100

	if len(meals) == 0 || !shareOK {
		v, err := a.mealsView(w, r, loc)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		// Show the boxes as they were ticked (EI-05).
		for i := range v.Meals {
			v.Meals[i].Selected = false
			for _, m := range meals {
				v.Meals[i].Selected = v.Meals[i].Selected || string(m) == v.Meals[i].Value
			}
		}
		if len(meals) == 0 {
			v.MealsErr = loc.T("validation.meals_required")
		}
		if !shareOK {
			v.ShareErr = loc.T("validation.share_range")
			v.Share = rawShare
		}
		a.render(w, r, loc, http.StatusUnprocessableEntity, "settings", v)
		return
	}
	if err := a.Store.SetMeals(r.Context(), meals); err != nil {
		a.fail(w, r, err)
		return
	}
	if err := a.Store.SetComposedShare(r.Context(), share); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, "/settings/meals", "flash.meals_saved")
}
