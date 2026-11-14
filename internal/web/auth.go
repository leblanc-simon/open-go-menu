package web

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"leblanc.io/open-go-base/authx"

	"leblanc.io/open-go-menu/internal/accounts"
	"leblanc.io/open-go-menu/internal/config"
	"leblanc.io/open-go-menu/internal/families"
	"leblanc.io/open-go-menu/internal/store"
)

type ctxKey int

const (
	ctxStore ctxKey = iota
	ctxFamily
	ctxRelease
	ctxLive
)

// SessionTTL is the lifetime of a session, slid at every visit: the longest
// browsers keep a cookie (400 days). A family stays logged in until it logs
// out (EF-27).
const SessionTTL = 400 * 24 * time.Hour

const (
	minPasswordLength = 10
	maxPasswordLength = 200
	// After loginMaxFails failed logins within loginWindow, a login is refused
	// until the window has passed (SEC-07).
	loginMaxFails = 5
	loginWindow   = 15 * time.Minute
)

// storeOf returns the database of the family of the request, opened by
// protect.
func storeOf(ctx context.Context) *store.Store {
	return ctx.Value(ctxStore).(*store.Store)
}

func familyOf(ctx context.Context) accounts.Family {
	return ctx.Value(ctxFamily).(accounts.Family)
}

// protect requires a family logged in, records its visit and opens its
// database for the handler (SEC-06): the family comes from the session only,
// never from the request.
func (a *App) protect(h http.HandlerFunc) http.Handler {
	return a.Auth.RequireAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := authx.UserFrom(r)
		fam, err := a.Accounts.FamilyOf(ctx, user.ID)
		var st *store.Store
		release := func() {}
		if err == nil {
			if err := a.Accounts.Touch(ctx, user.ID, a.Now()); err != nil {
				a.Logger.Warn("activity not recorded", "err", err)
			}
			st, release, err = a.Families.Acquire(ctx, fam.File)
		}
		if errors.Is(err, accounts.ErrNotFound) || errors.Is(err, families.ErrGone) {
			// A login without family, or one deleted meanwhile: nothing to
			// show it.
			a.Auth.Logout(w, r)
			redirect(w, r, "/login", "")
			return
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
		defer release()
		ctx = context.WithValue(ctx, ctxStore, st)
		ctx = context.WithValue(ctx, ctxFamily, fam)
		ctx = context.WithValue(ctx, ctxRelease, release)
		note := &liveNote{}
		ctx = context.WithValue(ctx, ctxLive, note)
		h(w, r.WithContext(ctx))
		a.publishNote(r, fam.File, note)
	}))
}

// ── Brute force ───────────────────────────────────────────────────────────

// loginGuard counts the failed logins of each login name, on top of the
// limit per IP.
type loginGuard struct {
	mu    sync.Mutex
	fails map[string][]time.Time
	swept time.Time // last sweep of the stale names
}

func newLoginGuard() *loginGuard {
	return &loginGuard{fails: map[string][]time.Time{}}
}

func (g *loginGuard) recent(login string, now time.Time) []time.Time {
	var kept []time.Time
	for _, t := range g.fails[login] {
		if now.Sub(t) < loginWindow {
			kept = append(kept, t)
		}
	}
	if kept == nil {
		delete(g.fails, login)
	} else {
		g.fails[login] = kept
	}
	return kept
}

func (g *loginGuard) blocked(login string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.recent(login, now)) >= loginMaxFails
}

func (g *loginGuard) fail(login string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	// Names tried at random must not grow the map forever; the sweep walks
	// the whole map, hence at most once a window.
	if len(g.fails) > 10000 && now.Sub(g.swept) >= loginWindow {
		g.swept = now
		for l := range g.fails {
			g.recent(l, now)
		}
	}
	g.fails[login] = append(g.recent(login, now), now)
}

func (g *loginGuard) reset(login string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.fails, login)
}

// ── Login, logout ─────────────────────────────────────────────────────────

// authView is the login and signup pages, and the account page.
type authView struct {
	page
	View   string // login, signup or account
	Next   string
	Login  string
	Error  string
	Signup string // the signup mode
	Months int    // inactivity before deletion

	// The signup page: the code of the link followed, or the one typed.
	Invite string

	// The account page.
	Since       string
	PasswordErr string
	DeleteErr   string
	Invites     *invitesView // nil when the families may not invite
}

// invitesView is the invitation section of the account page (EF-34).
type invitesView struct {
	Validity int // days
	Max      int
	Full     bool // as many codes pending as allowed
	// The code just made, shown this once.
	Code string
	Link string
	// The codes still to be used, and the families who used one.
	Pending []pendingInvite
	Invited []string
}

type pendingInvite struct {
	ID          int64
	Text        string
	RevokeLabel string
}

// loginRenderer renders the login form for authx. The message of a failed
// login is ours, translated; a failure counts against the login.
type loginRenderer struct{ a *App }

func (lr loginRenderer) RenderLogin(w http.ResponseWriter, r *http.Request, v authx.LoginView) {
	a := lr.a
	status := http.StatusOK
	msg := ""
	if v.Error != "" {
		status = http.StatusUnauthorized
		msg = a.localizer(r).T("login.invalid")
		a.guard.fail(v.Email, a.Now())
	}
	a.renderAuth(w, r, status, "login", v.Next, v.Email, msg)
}

func (a *App) renderAuth(w http.ResponseWriter, r *http.Request, status int, view, next, login, msg string) {
	loc := a.localizer(r)
	title := map[string]string{"login": "login.title", "signup": "signup.title"}[view]
	v := authView{
		page: a.newPage(w, r, loc, view, title), View: view, Next: next, Login: login, Error: msg,
		Signup: a.Options.Signup, Months: a.Options.InactiveMonths,
	}
	if view == "signup" && a.Options.Signup == config.SignupInvite {
		// The link of an invitation fills the code in (EF-34).
		v.Invite = r.FormValue("invite")
		if len(v.Invite) > maxInviteLength {
			v.Invite = ""
		}
	}
	a.render(w, r, loc, status, "account", v)
}

// statusRecorder notes the status of a response.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// loginPOST guards authx's login against brute force (SEC-07).
func (a *App) loginPOST(w http.ResponseWriter, r *http.Request) {
	login := accounts.NormalizeLogin(r.PostFormValue("email"))
	if a.guard.blocked(login, a.Now()) {
		msg := a.localizer(r).T("login.too_many", map[string]any{"Minutes": int(loginWindow.Minutes())})
		a.renderAuth(w, r, http.StatusTooManyRequests, "login", r.PostFormValue("next"), login, msg)
		return
	}
	rec := &statusRecorder{ResponseWriter: w}
	a.login.LoginPOST(rec, r)
	if rec.status == http.StatusSeeOther {
		a.guard.reset(login)
	}
}

// clearSite asks the browser to drop the pages the service worker kept
// offline, so that the next family on the device does not see them.
func clearSite(w http.ResponseWriter) {
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
}

// logout closes the session of this device only (EF-27).
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	clearSite(w)
	a.Auth.Logout(w, r)
	redirect(w, r, "/login", "flash.logged_out")
}

// ── Signup ────────────────────────────────────────────────────────────────

func (a *App) signupPage(w http.ResponseWriter, r *http.Request) {
	if authx.UserFrom(r) != nil {
		redirect(w, r, "/", "")
		return
	}
	status := http.StatusOK
	if a.Options.Signup == config.SignupClosed {
		status = http.StatusNotFound
	}
	a.renderAuth(w, r, status, "signup", "", "", "")
}

// checkPassword validates a new password and its confirmation; it returns
// the message to show, or "".
func (a *App) checkPassword(r *http.Request, password, confirm string) string {
	loc := a.localizer(r)
	switch n := utf8.RuneCountInString(password); {
	case n < minPasswordLength:
		return loc.T("validation.password_short", map[string]any{"Min": minPasswordLength})
	case n > maxPasswordLength:
		return loc.T("validation.password_long", map[string]any{"Max": maxPasswordLength})
	case password != confirm:
		return loc.T("validation.password_mismatch")
	}
	return ""
}

// signup creates a family, its database, and logs it in (EF-25, EF-26).
func (a *App) signup(w http.ResponseWriter, r *http.Request) {
	if a.Options.Signup == config.SignupClosed {
		a.notFound(w, r)
		return
	}
	ctx := r.Context()
	loc := a.localizer(r)
	login := accounts.NormalizeLogin(r.PostFormValue("login"))
	invite := ""
	if a.Options.Signup == config.SignupInvite {
		invite = accounts.NormalizeInvite(r.PostFormValue("invite"))
	}

	msg := ""
	switch {
	case !accounts.ValidLogin(login):
		msg = loc.T("validation.login_format")
	case a.Options.Signup == config.SignupInvite && invite == "":
		msg = loc.T("validation.invite_invalid")
	default:
		msg = a.checkPassword(r, r.PostFormValue("password"), r.PostFormValue("password_confirm"))
	}
	// A wrong code is refused before bcrypt, whose cost it would not deserve.
	if msg == "" && invite != "" {
		switch err := a.Accounts.CheckInvite(ctx, invite, a.Now()); {
		case errors.Is(err, accounts.ErrInvite):
			msg = loc.T("validation.invite_invalid")
		case err != nil:
			a.fail(w, r, err)
			return
		}
	}
	if msg == "" {
		hash, err := authx.HashPassword(r.PostFormValue("password"), a.Options.BcryptCost)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		fam, err := a.Accounts.CreateFamily(ctx, login, hash, invite, a.Now())
		switch {
		case errors.Is(err, accounts.ErrLoginTaken):
			msg = loc.T("validation.login_taken")
		case errors.Is(err, accounts.ErrInvite):
			msg = loc.T("validation.invite_invalid")
		case err != nil:
			a.fail(w, r, err)
			return
		default:
			// The database of the family is created now, not at its first page.
			_, release, err := a.Families.Acquire(ctx, fam.File)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			release()
			user, err := a.Accounts.Users.GetByID(fam.UserID)
			if err == nil {
				_, err = a.Auth.OpenSession(w, r, user, authx.PurposeAuth)
			}
			if err != nil {
				a.fail(w, r, err)
				return
			}
			redirect(w, r, "/settings", "flash.family_created")
			return
		}
	}
	a.renderAuth(w, r, http.StatusUnprocessableEntity, "signup", "", login, msg)
}

// ── Account ───────────────────────────────────────────────────────────────

func (a *App) accountPage(w http.ResponseWriter, r *http.Request, status int, passwordErr, deleteErr string) {
	loc := a.localizer(r)
	fam := familyOf(r.Context())
	v := authView{
		page: a.newPage(w, r, loc, "account", "account.title"), View: "account",
		Login: fam.Login, Months: a.Options.InactiveMonths,
		Since:       dayMonthYear(loc, fam.CreatedAt.In(a.Now().Location())),
		PasswordErr: passwordErr, DeleteErr: deleteErr,
	}
	if a.Options.FamilyInvites {
		inv, err := a.invitesOf(w, r, fam)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		v.Invites = inv
	}
	a.render(w, r, loc, status, "account", v)
}

// ── Invitations by the families (EF-34) ───────────────────────────────────

const (
	// inviteCookie carries a code just made to the account page, which shows
	// it once: only its hash is kept.
	inviteCookie    = "invite"
	maxInviteLength = 20
)

var inviteCodeRe = regexp.MustCompile(`^[A-Z2-9]{4}-[A-Z2-9]{4}-[A-Z2-9]{4}$`)

func (a *App) invitesOf(w http.ResponseWriter, r *http.Request, fam accounts.Family) (*invitesView, error) {
	ctx := r.Context()
	loc := a.localizer(r)
	tz := a.Now().Location()
	v := &invitesView{Validity: int(a.Options.InviteTTL / (24 * time.Hour)), Max: a.Options.InviteMax}

	if c, err := r.Cookie(inviteCookie); err == nil {
		http.SetCookie(w, &http.Cookie{Name: inviteCookie, Path: "/account", MaxAge: -1})
		if inviteCodeRe.MatchString(c.Value) {
			v.Code = c.Value
			v.Link = origin(r) + "/signup?invite=" + c.Value
			// The page shows the code: no copy of it anywhere.
			w.Header().Set("Cache-Control", "no-store")
		}
	}

	pending, err := a.Accounts.PendingInvites(ctx, fam.UserID, a.Now())
	if err != nil {
		return nil, err
	}
	v.Full = len(pending) >= a.Options.InviteMax
	for _, p := range pending {
		dates := map[string]any{"Created": dayMonthYear(loc, p.CreatedAt.In(tz)), "Expires": dayMonthYear(loc, p.ExpiresAt.In(tz))}
		v.Pending = append(v.Pending, pendingInvite{
			ID: p.ID, Text: loc.T("account.invite_pending_item", dates), RevokeLabel: loc.T("account.invite_revoke_label", dates),
		})
	}
	invited, err := a.Accounts.InvitedBy(ctx, fam.UserID)
	if err != nil {
		return nil, err
	}
	for _, f := range invited {
		v.Invited = append(v.Invited, loc.T("account.invite_invited_item",
			map[string]any{"Login": f.Login, "Since": dayMonthYear(loc, f.CreatedAt.In(tz))}))
	}
	return v, nil
}

// origin is the address of the server as the browser sees it, for the link
// of an invitation.
func origin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// createInvite makes an invitation code of the family, shown once on the
// account page.
func (a *App) createInvite(w http.ResponseWriter, r *http.Request) {
	if !a.Options.FamilyInvites {
		a.notFound(w, r)
		return
	}
	fam := familyOf(r.Context())
	code, err := a.Accounts.CreateFamilyInvite(r.Context(), fam.UserID, a.Options.InviteTTL, a.Options.InviteMax, a.Now())
	switch {
	case errors.Is(err, accounts.ErrInviteLimit):
		redirect(w, r, "/account#invites", "flash.invite_limit")
	case err != nil:
		a.fail(w, r, err)
	default:
		http.SetCookie(w, &http.Cookie{
			Name: inviteCookie, Value: code, Path: "/account", MaxAge: 300,
			HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteStrictMode,
		})
		redirect(w, r, "/account#invites", "")
	}
}

// revokeInvite deletes a pending code of the family.
func (a *App) revokeInvite(w http.ResponseWriter, r *http.Request) {
	if !a.Options.FamilyInvites {
		a.notFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	switch err := a.Accounts.RevokeInvite(r.Context(), familyOf(r.Context()).UserID, id); {
	case errors.Is(err, accounts.ErrNotFound):
		a.notFound(w, r)
	case err != nil:
		a.fail(w, r, err)
	default:
		redirect(w, r, "/account#invites", "flash.invite_revoked")
	}
}

func (a *App) account(w http.ResponseWriter, r *http.Request) {
	a.accountPage(w, r, http.StatusOK, "", "")
}

// changePassword replaces the password; every other device is logged out,
// this one stays logged in (EF-28).
func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	fam := familyOf(r.Context())
	ok, err := a.Accounts.CheckPassword(fam.UserID, r.PostFormValue("current"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	msg := ""
	if !ok {
		msg = loc.T("validation.password_wrong")
	} else {
		msg = a.checkPassword(r, r.PostFormValue("password"), r.PostFormValue("password_confirm"))
	}
	if msg != "" {
		a.accountPage(w, r, http.StatusUnprocessableEntity, msg, "")
		return
	}
	hash, err := authx.HashPassword(r.PostFormValue("password"), a.Options.BcryptCost)
	if err == nil {
		err = a.Accounts.SetPassword(r.Context(), fam.UserID, hash)
	}
	if err == nil {
		_, err = a.Auth.OpenSession(w, r, authx.UserFrom(r), authx.PurposeAuth)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// The other devices may have lost their session.
	a.endStreams(r.Context())
	redirect(w, r, "/account", "flash.password_changed")
}

// logoutEverywhere closes every session of the family, this one included.
func (a *App) logoutEverywhere(w http.ResponseWriter, r *http.Request) {
	if err := a.Accounts.Sessions.DeleteByUser(familyOf(r.Context()).UserID); err != nil {
		a.fail(w, r, err)
		return
	}
	clearSite(w)
	a.Auth.Logout(w, r)
	// The other devices may have lost their session.
	a.endStreams(r.Context())
	redirect(w, r, "/login", "flash.logged_out_everywhere")
}

// deleteFamily deletes the family, its login and its data, once confirmed
// with the password (EF-29).
func (a *App) deleteFamily(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := a.localizer(r)
	fam := familyOf(ctx)
	ok, err := a.Accounts.CheckPassword(fam.UserID, r.PostFormValue("password"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	switch {
	case !ok:
		a.accountPage(w, r, http.StatusUnprocessableEntity, "", loc.T("validation.password_wrong"))
		return
	case r.PostFormValue("confirm") != "yes":
		a.accountPage(w, r, http.StatusUnprocessableEntity, "", loc.T("validation.delete_confirm"))
		return
	}
	if err := a.Accounts.DeleteFamily(ctx, fam.UserID); err != nil {
		a.fail(w, r, err)
		return
	}
	// This request holds the database: let it go before removing the file.
	ctx.Value(ctxRelease).(func())()
	if err := a.Families.Remove(fam.File); err != nil {
		a.Logger.Error("family file left", "file", fam.File, "err", err)
	}
	clearSite(w)
	a.Auth.Logout(w, r)
	// The other devices may have lost their session.
	a.endStreams(r.Context())
	redirect(w, r, "/login", "flash.family_deleted")
}
