// Package web serves the HTML interface: the week page and the settings page.
package web

import (
	"bytes"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"leblanc.io/open-go-base/csrfx"
	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-menu/internal/store"
)

// App holds the dependencies of the handlers.
type App struct {
	Store  *store.Store
	I18n   *i18n.Bundle
	CSRF   *csrfx.Protector
	Logger *slog.Logger
	// Now returns the current time; its location decides what "today" is.
	Now func() time.Time

	pages map[string]*template.Template
}

// pageNames lists the templates rendered inside the layout.
var pageNames = []string{"home", "settings", "error"}

// New parses the templates of templateFS and returns a ready App.
func New(templateFS fs.FS, st *store.Store, bundle *i18n.Bundle, csrf *csrfx.Protector, logger *slog.Logger) (*App, error) {
	// Request-bound helpers are replaced on a clone at render time; these
	// stubs only let the templates parse.
	stubs := template.FuncMap{
		"T":         func(string, ...any) string { return "" },
		"Tn":        func(string, int, ...any) string { return "" },
		"csrfField": func() template.HTML { return "" },
		"csrfToken": func() string { return "" },
	}
	a := &App{Store: st, I18n: bundle, CSRF: csrf, Logger: logger, Now: time.Now,
		pages: make(map[string]*template.Template, len(pageNames))}
	for _, name := range pageNames {
		t, err := template.New(name).Funcs(stubs).ParseFS(templateFS, "layout.html", name+".html")
		if err != nil {
			return nil, err
		}
		a.pages[name] = t
	}
	return a, nil
}

// Routes registers the handlers on mux.
func (a *App) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("POST /week-start", a.setWeekStart)
	mux.HandleFunc("POST /plan/generate", a.generate)
	mux.HandleFunc("POST /plan/{day}/{meal}", a.editSlot)
	mux.HandleFunc("GET /settings", a.settings)
	mux.HandleFunc("GET /settings/meals", a.mealsSettings)
	mux.HandleFunc("POST /settings/meals", a.saveMeals)
	mux.HandleFunc("GET /settings/{list}", a.settingsList)
	mux.HandleFunc("POST /settings/{list}", a.addItem)
	mux.HandleFunc("POST /settings/{list}/save", a.saveItems)
	mux.HandleFunc("POST /settings/items/{id}/delete", a.deleteItem)
	mux.HandleFunc("/", a.notFound)
}

// page carries what the layout needs on every page.
type page struct {
	Lang      string
	Title     string
	Nav       string
	Flash     string
	LangLinks []langLink
}

type langLink struct {
	Code    string
	Label   string
	URL     string
	Current bool
}

// newPage fills the layout data. It consumes the pending flash message.
func (a *App) newPage(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer, nav, titleKey string) page {
	p := page{
		Lang:  loc.Lang().String(),
		Title: loc.T(titleKey),
		Nav:   nav,
	}
	if key := popFlash(w, r); key != "" {
		p.Flash = loc.T(key)
	}
	for _, tag := range a.I18n.Languages() {
		q := url.Values{}
		for k, v := range r.URL.Query() {
			q[k] = v
		}
		q.Set(i18n.QueryParam, tag.String())
		p.LangLinks = append(p.LangLinks, langLink{
			Code:    tag.String(),
			Label:   loc.T("language." + tag.String()),
			URL:     r.URL.Path + "?" + q.Encode(),
			Current: tag == loc.Lang(),
		})
	}
	return p
}

// render executes a page with the request-bound helpers: translations and
// CSRF field. The output is buffered so that a template error never leaves a
// half-written page behind.
func (a *App) render(w http.ResponseWriter, r *http.Request, loc *i18n.Localizer, status int, name string, data any) {
	t, err := a.pages[name].Clone()
	if err != nil {
		a.fail(w, r, err)
		return
	}
	t.Funcs(loc.FuncMap()).Funcs(a.CSRF.FuncMap(r))

	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

type errorView struct {
	page
	Message string
}

// renderError shows a localized error page.
func (a *App) renderError(w http.ResponseWriter, r *http.Request, status int, key string) {
	loc := a.localizer(r)
	v := errorView{page: a.newPage(w, r, loc, "", "error.title"), Message: loc.T(key)}
	a.render(w, r, loc, status, "error", v)
}

// fail logs an unexpected error and answers with a bare 500: it may come from
// the rendering itself, so it must not render a template.
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	http.Error(w, a.localizer(r).T("error.internal"), http.StatusInternalServerError)
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.renderError(w, r, http.StatusNotFound, "error.not_found")
}

// redirect ends a POST with a flash message (PRG, EI-04).
func redirect(w http.ResponseWriter, r *http.Request, to, flashKey string) {
	if flashKey != "" {
		setFlash(w, flashKey)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}
