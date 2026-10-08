package web

import (
	"net/http"
	"strings"
	"time"

	"leblanc.io/open-go-base/i18n"
)

const (
	langCookie  = "lang"
	flashCookie = "flash"
)

// SecurityHeaders sets the response headers of SEC-02 on every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; "+
			"img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// RememberLanguage stores an explicit ?lang= choice in a cookie, so that the
// language sticks across pages and redirects (ENF-06).
func (a *App) RememberLanguage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lang := r.URL.Query().Get(i18n.QueryParam); a.supported(lang) {
			http.SetCookie(w, &http.Cookie{
				Name: langCookie, Value: lang, Path: "/",
				MaxAge: int((365 * 24 * time.Hour).Seconds()), HttpOnly: true, SameSite: http.SameSiteLaxMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) supported(lang string) bool {
	for _, tag := range a.I18n.Languages() {
		if lang != "" && tag.String() == lang {
			return true
		}
	}
	return false
}

// localizer resolves the language: ?lang=, then the cookie, then
// Accept-Language, then the configured default.
func (a *App) localizer(r *http.Request) *i18n.Localizer {
	force := r.URL.Query().Get(i18n.QueryParam)
	if !a.supported(force) {
		force = ""
		if c, err := r.Cookie(langCookie); err == nil && a.supported(c.Value) {
			force = c.Value
		}
	}
	return a.I18n.Localizer(force, r.Header.Get("Accept-Language"))
}

// setFlash queues a message for the next page. Only a translation key under
// flash. is stored, never free text.
func setFlash(w http.ResponseWriter, key string) {
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: key, Path: "/", MaxAge: 60, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// popFlash returns and clears the pending message key, if any.
func popFlash(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1})
	if !strings.HasPrefix(c.Value, "flash.") {
		return ""
	}
	return c.Value
}
