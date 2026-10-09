package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-go-menu/internal/store"
)

// nameRow is an article or an aisle as typed in a form, with its errors.
type nameRow struct {
	ID      int64
	Name    string
	Aisle   int64
	Uses    string // the recipes an article is in
	Errors  []string
	Invalid bool

	// The aisles tab.
	IsFirst, IsLast    bool
	UpLabel, DownLabel string
}

func (n *nameRow) fail(msg string) {
	n.Invalid = true
	n.Errors = append(n.Errors, msg)
}

// readNames reads the rows of a grouped form: the id fields, then
// name_<id> and, for articles, aisle_<id>. Two rows may not share a name.
// It reports false on a malformed form.
func (a *App) readNames(r *http.Request, loc *i18n.Localizer, withAisle bool) ([]nameRow, bool, bool) {
	if err := r.ParseForm(); err != nil {
		return nil, false, false
	}
	var rows []nameRow
	valid := true
	seen := map[string]bool{}
	for _, raw := range r.PostForm["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			return nil, false, false
		}
		row := nameRow{ID: id, Name: strings.TrimSpace(r.PostFormValue(fieldName("name", id)))}
		if withAisle {
			aisle, ok := a.aisleField(r, fieldName("aisle", id))
			if !ok {
				return nil, false, false
			}
			row.Aisle = aisle
		}
		if msg := validName(loc, row.Name); msg != "" {
			row.fail(msg)
		} else if key := strings.ToLower(row.Name); seen[key] {
			row.fail(loc.T("validation.name_duplicate", map[string]any{"Name": row.Name}))
		} else {
			seen[key] = true
		}
		valid = valid && !row.Invalid
		rows = append(rows, row)
	}
	return rows, valid, true
}

// ── Articles (EF-21) ──────────────────────────────────────────────────────

// articlesPage renders the articles tab; rows and newRow override the stored
// values when a form is redisplayed with errors.
func (a *App) articlesPage(w http.ResponseWriter, r *http.Request, status int, rows []nameRow, newRow *nameRow, saveErr string) {
	ctx := r.Context()
	loc := a.localizer(r)
	articles, err := a.Store.Articles(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	aisles, err := a.Store.AislesByName(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.basePage(w, r, loc, "articles")
	v.View = "articles"
	v.Aisles = aisles
	v.SaveErr = saveErr
	typed := map[int64]nameRow{}
	for _, row := range rows {
		typed[row.ID] = row
	}
	for _, art := range articles {
		row, ok := typed[art.ID]
		if !ok {
			row = nameRow{ID: art.ID, Name: art.Name, Aisle: art.Aisle}
		}
		if art.Uses > 0 {
			row.Uses = loc.Tn("settings.articles.uses", art.Uses)
		}
		v.Names = append(v.Names, row)
	}
	if newRow != nil {
		v.NewName = *newRow
	}
	a.render(w, r, loc, status, "settings", v)
}

func (a *App) articlesSettings(w http.ResponseWriter, r *http.Request) {
	a.articlesPage(w, r, http.StatusOK, nil, nil, "")
}

func (a *App) addArticle(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	row := nameRow{Name: strings.TrimSpace(r.PostFormValue("name"))}
	aisle, ok := a.aisleField(r, "aisle")
	if !ok {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	row.Aisle = aisle
	if msg := validName(loc, row.Name); msg != "" {
		row.fail(msg)
	} else {
		_, err := a.Store.CreateArticle(r.Context(), row.Name, aisle)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			row.fail(loc.T("validation.article_duplicate", map[string]any{"Name": row.Name}))
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/settings/articles#add", "flash.article_added")
			return
		}
	}
	a.articlesPage(w, r, http.StatusUnprocessableEntity, nil, &row, "")
}

func (a *App) saveArticles(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	rows, valid, ok := a.readNames(r, loc, true)
	if !ok {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	saveErr := loc.T("validation.form_invalid")
	if valid {
		articles := make([]store.Article, len(rows))
		for i, row := range rows {
			articles[i] = store.Article{ID: row.ID, Name: row.Name, Aisle: row.Aisle}
		}
		err := a.Store.UpdateArticles(r.Context(), articles)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			saveErr = loc.T("validation.name_taken")
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/settings/articles", "flash.articles_saved")
			return
		}
	}
	a.articlesPage(w, r, http.StatusUnprocessableEntity, rows, nil, saveErr)
}

func (a *App) deleteArticle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = a.Store.DeleteArticle(r.Context(), id)
	} else {
		err = store.ErrNotFound
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
	case errors.Is(err, store.ErrInUse):
		redirect(w, r, "/settings/articles", "flash.article_in_use")
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, "/settings/articles", "flash.article_deleted")
	}
}

// ── Aisles (EF-20) ────────────────────────────────────────────────────────

func (a *App) aislesPage(w http.ResponseWriter, r *http.Request, status int, rows []nameRow, newRow *nameRow, saveErr string) {
	loc := a.localizer(r)
	aisles, err := a.Store.Aisles(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	v := a.basePage(w, r, loc, "aisles")
	v.View = "aisles"
	v.SaveErr = saveErr
	typed := map[int64]nameRow{}
	for _, row := range rows {
		typed[row.ID] = row
	}
	for i, x := range aisles {
		row, ok := typed[x.ID]
		if !ok {
			row = nameRow{ID: x.ID, Name: x.Name}
		}
		row.IsFirst, row.IsLast = i == 0, i == len(aisles)-1
		row.UpLabel = loc.T("settings.aisles.up_label", map[string]any{"Name": x.Name})
		row.DownLabel = loc.T("settings.aisles.down_label", map[string]any{"Name": x.Name})
		v.Names = append(v.Names, row)
	}
	if newRow != nil {
		v.NewName = *newRow
	}
	a.render(w, r, loc, status, "settings", v)
}

func (a *App) aislesSettings(w http.ResponseWriter, r *http.Request) {
	a.aislesPage(w, r, http.StatusOK, nil, nil, "")
}

func (a *App) addAisle(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	row := nameRow{Name: strings.TrimSpace(r.PostFormValue("name"))}
	if msg := validName(loc, row.Name); msg != "" {
		row.fail(msg)
	} else {
		_, err := a.Store.CreateAisle(r.Context(), row.Name)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			row.fail(loc.T("validation.aisle_duplicate", map[string]any{"Name": row.Name}))
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/settings/aisles#add", "flash.aisle_added")
			return
		}
	}
	a.aislesPage(w, r, http.StatusUnprocessableEntity, nil, &row, "")
}

func (a *App) saveAisles(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	rows, valid, ok := a.readNames(r, loc, false)
	if !ok {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	saveErr := loc.T("validation.form_invalid")
	if valid {
		aisles := make([]store.Aisle, len(rows))
		for i, row := range rows {
			aisles[i] = store.Aisle{ID: row.ID, Name: row.Name}
		}
		err := a.Store.RenameAisles(r.Context(), aisles)
		switch {
		case errors.Is(err, store.ErrDuplicateName):
			saveErr = loc.T("validation.name_taken")
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			redirect(w, r, "/settings/aisles", "flash.aisles_saved")
			return
		}
	}
	a.aislesPage(w, r, http.StatusUnprocessableEntity, rows, nil, saveErr)
}

func (a *App) moveAisle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	dir := r.FormValue("dir")
	if err != nil || (dir != "up" && dir != "down") {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	err = a.Store.MoveAisle(r.Context(), id, dir == "up")
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, "/settings/aisles#aisle-"+strconv.FormatInt(id, 10), "flash.aisle_moved")
	}
}

// orderAisles saves the order of the aisles dropped by the script (EF-20).
func (a *App) orderAisles(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
		return
	}
	var ids []int64
	for _, raw := range r.PostForm["id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
			return
		}
		ids = append(ids, id)
	}
	err := a.Store.SetAisleOrder(r.Context(), ids)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// A list changed meanwhile, in another tab.
		a.renderError(w, r, http.StatusBadRequest, "error.bad_request")
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, "/settings/aisles", "flash.aisle_moved")
	}
}

func (a *App) deleteAisle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err == nil {
		err = a.Store.DeleteAisle(r.Context(), id)
	} else {
		err = store.ErrNotFound
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.notFound(w, r)
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, "/settings/aisles", "flash.aisle_deleted")
	}
}
