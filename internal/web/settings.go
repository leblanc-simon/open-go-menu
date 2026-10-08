package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-menu/internal/planner"
	"leblanc.io/open-menu/internal/store"
)

const (
	maxNameLength = 100
	maxWeight     = 10
)

type settingsView struct {
	page
	Seasons  []seasonColumn
	Rows     []dishForm
	New      dishForm
	Meals    []option
	SaveErr  string
	MealsErr string
	Weight   string // default weight, shown in the explanation
}

type seasonColumn struct {
	Key   string
	Label string
}

// dishForm is a dish as typed in a form: raw values, so that an invalid input
// is displayed back as is, with its translated errors (EI-05).
type dishForm struct {
	ID       int64
	Name     string
	Weight   string
	Seasonal [4]string
	Errors   []string
	Invalid  map[string]bool // field name → in error
}

func formOf(d store.Dish) dishForm {
	f := dishForm{ID: d.ID, Name: d.Name, Weight: strconv.Itoa(d.Weight)}
	for _, s := range planner.Seasons {
		f.Seasonal[s] = strconv.Itoa(d.Seasonal[s])
	}
	return f
}

// fieldName returns the form field of a dish row; suffix is "" for the
// creation form.
func fieldName(base string, id int64) string {
	if id == 0 {
		return base
	}
	return base + "_" + strconv.FormatInt(id, 10)
}

// parseDishForm reads and validates a dish from the posted form.
func parseDishForm(r *http.Request, id int64, loc *i18n.Localizer) (dishForm, store.Dish, bool) {
	f := dishForm{
		ID:      id,
		Name:    strings.TrimSpace(r.PostFormValue(fieldName("name", id))),
		Weight:  strings.TrimSpace(r.PostFormValue(fieldName("weight", id))),
		Invalid: map[string]bool{},
	}
	d := store.Dish{ID: id, Name: f.Name}

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
	d.Weight = weight("weight", f.Weight, loc.T("settings.weight"))
	for _, s := range planner.Seasons {
		f.Seasonal[s] = strings.TrimSpace(r.PostFormValue(fieldName(s.Key(), id)))
		d.Seasonal[s] = weight(s.Key(), f.Seasonal[s], loc.T("season."+s.Key()))
	}
	return f, d, len(f.Errors) == 0
}

func (f *dishForm) fail(field, msg string) {
	f.Invalid[field] = true
	f.Errors = append(f.Errors, msg)
}

// settingsView builds the page; rows and newDish override the stored values
// when a form is redisplayed with errors.
func (a *App) settingsView(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer, rows []dishForm, newDish *dishForm) (settingsView, error) {
	ctx := r.Context()
	v := settingsView{
		page:   a.newPage(w, r, loc, "settings", "settings.title"),
		Rows:   rows,
		Weight: strconv.Itoa(store.DefaultWeight),
	}
	for _, s := range planner.Seasons {
		v.Seasons = append(v.Seasons, seasonColumn{Key: s.Key(), Label: loc.T("season." + s.Key())})
	}
	if rows == nil {
		dishes, err := a.Store.Dishes(ctx)
		if err != nil {
			return v, err
		}
		for _, d := range dishes {
			v.Rows = append(v.Rows, formOf(d))
		}
	}
	if newDish != nil {
		v.New = *newDish
	} else {
		v.New = formOf(store.NewDish(""))
	}

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
	return v, nil
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	v, err := a.settingsView(w, r, loc, nil, nil)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, loc, http.StatusOK, "settings", v)
}

func (a *App) addDish(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	f, d, ok := parseDishForm(r, 0, loc)
	if ok {
		_, err := a.Store.CreateDish(r.Context(), d)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			f.fail("name", loc.T("validation.name_duplicate", map[string]any{"Name": d.Name}))
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/settings#add", "flash.dish_added")
			return
		}
	}
	v, err := a.settingsView(w, r, loc, nil, &f)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, loc, http.StatusUnprocessableEntity, "settings", v)
}

func (a *App) saveDishes(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	if err := r.ParseForm(); err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}

	var (
		rows   []dishForm
		dishes []store.Dish
		valid  = true
		names  = map[string]int{} // lower-cased name → row index
	)
	for _, raw := range r.PostForm["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		f, d, ok := parseDishForm(r, id, loc)
		if ok {
			key := strings.ToLower(d.Name)
			if _, dup := names[key]; dup {
				f.fail("name", loc.T("validation.name_duplicate", map[string]any{"Name": d.Name}))
				ok = false
			}
			names[key] = len(rows)
		}
		valid = valid && ok
		rows = append(rows, f)
		dishes = append(dishes, d)
	}

	var saveErr string
	if valid {
		err := a.Store.UpdateDishes(r.Context(), dishes)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			// Clashes with a dish added meanwhile, absent from this form.
			saveErr = loc.T("validation.name_taken")
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/settings", "flash.dishes_saved")
			return
		}
	}

	v, err := a.settingsView(w, r, loc, rows, nil)
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

func (a *App) deleteDish(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	err = a.Store.DeleteDish(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.renderError(w, r, http.StatusNotFound, "error.not_found")
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, "/settings", "flash.dish_deleted")
	}
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
	if len(meals) == 0 {
		v, err := a.settingsView(w, r, loc, nil, nil)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		v.MealsErr = loc.T("validation.meals_required")
		a.render(w, r, loc, http.StatusUnprocessableEntity, "settings", v)
		return
	}
	if err := a.Store.SetMeals(r.Context(), meals); err != nil {
		a.fail(w, r, err)
		return
	}
	redirect(w, r, "/settings#meals", "flash.meals_saved")
}
