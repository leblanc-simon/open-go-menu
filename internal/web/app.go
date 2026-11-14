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

	"leblanc.io/open-go-base/authx"
	"leblanc.io/open-go-base/csrfx"
	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-go-menu/internal/accounts"
	"leblanc.io/open-go-menu/internal/families"
)

// App holds the dependencies of the handlers.
type App struct {
	Accounts *accounts.Store
	Families *families.Registry
	Auth     *authx.Manager
	I18n     *i18n.Bundle
	CSRF     *csrfx.Protector
	Logger   *slog.Logger
	// Now returns the current time; its location decides what "today" is.
	Now func() time.Time

	// Options holds the account settings.
	Options Options

	// AssetVersion tells the versions of the static files apart, in their
	// URLs: they can then be cached for good (see HashFS).
	AssetVersion string

	pages map[string]*template.Template
	login *authx.Handlers
	guard *loginGuard
	live  *hub
}

// Options are the account settings of the application.
type Options struct {
	Signup         string // config.SignupOpen, SignupInvite or SignupClosed
	InactiveMonths int
	BcryptCost     int

	// FamilyInvites lets the families make invitation codes (EF-34): each
	// valid for InviteTTL, at most InviteMax pending per family.
	FamilyInvites bool
	InviteTTL     time.Duration
	InviteMax     int
}

// pageNames lists the templates rendered inside the layout.
var pageNames = []string{"home", "settings", "shopping", "account", "error"}

// New parses the templates of templateFS and returns a ready App.
func New(templateFS fs.FS, accts *accounts.Store, fams *families.Registry, auth *authx.Manager, opts Options,
	bundle *i18n.Bundle, csrf *csrfx.Protector, logger *slog.Logger) (*App, error) {
	// Request-bound helpers are replaced on a clone at render time; these
	// stubs only let the templates parse.
	stubs := template.FuncMap{
		"T":         func(string, ...any) string { return "" },
		"Tn":        func(string, int, ...any) string { return "" },
		"csrfField": func() template.HTML { return "" },
		"csrfToken": func() string { return "" },
	}
	a := &App{Accounts: accts, Families: fams, Auth: auth, Options: opts,
		I18n: bundle, CSRF: csrf, Logger: logger, Now: time.Now,
		pages: make(map[string]*template.Template, len(pageNames)), guard: newLoginGuard(), live: newHub()}
	// asset returns the versioned URL of a static file.
	stubs["asset"] = func(path string) string {
		if a.AssetVersion == "" {
			return "/static/" + path
		}
		return "/static/" + path + "?v=" + a.AssetVersion
	}
	a.login = authx.NewHandlers(auth, loginRenderer{a})
	for _, name := range pageNames {
		t, err := template.New(name).Funcs(stubs).ParseFS(templateFS, "layout.html", name+".html")
		if err != nil {
			return nil, err
		}
		a.pages[name] = t
	}
	return a, nil
}

// Handler returns the routes of the application, behind the loading of the
// session: every page but the public ones requires a family logged in.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.Routes(mux)
	return a.Auth.LoadSession(mux)
}

// Routes registers the handlers on mux.
func (a *App) Routes(mux *http.ServeMux) {
	// Public pages.
	mux.HandleFunc("GET /login", a.login.LoginGET)
	mux.HandleFunc("POST /login", a.loginPOST)
	mux.HandleFunc("GET /signup", a.signupPage)
	mux.HandleFunc("POST /signup", a.signup)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /manifest.webmanifest", a.manifest)
	mux.HandleFunc("/", a.notFound)

	// The pages of a family.
	for pattern, h := range map[string]http.HandlerFunc{
		"GET /{$}":                                               a.home,
		"POST /week-start":                                       a.setWeekStart,
		"POST /plan/generate":                                    a.generate,
		"POST /plan/{day}/{meal}":                                a.editSlot,
		"GET /settings":                                          a.settings,
		"GET /settings/meals":                                    a.mealsSettings,
		"POST /settings/meals":                                   a.saveMeals,
		"GET /settings/meals/{weekday}/{meal}":                   a.fixedMeal,
		"POST /settings/meals/{weekday}/{meal}":                  a.saveFixedMeal,
		"GET /settings/{list}":                                   a.settingsList,
		"POST /settings/{list}":                                  a.addItem,
		"POST /settings/{list}/save":                             a.saveItems,
		"POST /settings/items/{id}/delete":                       a.deleteItem,
		"GET /settings/items/{id}":                               a.dishIngredients,
		"POST /settings/items/{id}/ingredients":                  a.addIngredient,
		"POST /settings/items/{id}/ingredients/save":             a.saveQuantities,
		"POST /settings/items/{id}/ingredients/{article}/delete": a.removeIngredient,
		"GET /settings/articles":                                 a.articlesSettings,
		"POST /settings/articles":                                a.addArticle,
		"POST /settings/articles/save":                           a.saveArticles,
		"POST /settings/articles/{id}/delete":                    a.deleteArticle,
		"GET /settings/aisles":                                   a.aislesSettings,
		"POST /settings/aisles":                                  a.addAisle,
		"POST /settings/aisles/save":                             a.saveAisles,
		"POST /settings/aisles/order":                            a.orderAisles,
		"POST /settings/aisles/{id}/move":                        a.moveAisle,
		"POST /settings/aisles/{id}/delete":                      a.deleteAisle,
		"GET /shopping":                                          a.shopping,
		"GET /shopping/new":                                      a.newShopping,
		"POST /shopping":                                         a.startShopping,
		"POST /shopping/extras":                                  a.addExtra,
		"POST /shopping/articles/{id}":                           a.shoppingLine,
		"GET /shopping/events":                                   a.shoppingEvents,
		"GET /account":                                           a.account,
		"POST /account/password":                                 a.changePassword,
		"POST /account/logout-all":                               a.logoutEverywhere,
		"POST /account/delete":                                   a.deleteFamily,
		"POST /account/invites":                                  a.createInvite,
		"POST /account/invites/{id}/revoke":                      a.revokeInvite,
	} {
		mux.Handle(pattern, a.protect(h))
	}
}

// page carries what the layout needs on every page.
type page struct {
	Lang      string
	Title     string
	Nav       string
	Family    string // the login of the family, "" on the public pages
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
	if f, ok := r.Context().Value(ctxFamily).(accounts.Family); ok {
		p.Family = f.Login
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
	// A post that ends here made its change: the other pages of the family
	// hear of it (EF-33).
	noteDone(r)
	http.Redirect(w, r, to, http.StatusSeeOther)
}
