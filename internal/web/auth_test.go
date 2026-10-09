package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAnonymous(t *testing.T) {
	app, srv := newApp(t, "invite")
	c := anonymous(t, app, srv)
	for path, want := range map[string]string{
		"/":         "/login",
		"/settings": "/login?next=%2Fsettings",
		"/shopping": "/login?next=%2Fshopping",
		"/account":  "/login?next=%2Faccount",
	} {
		if _, res := c.get(path); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != want {
			t.Errorf("%s: status %d, location %q", path, res.StatusCode, res.Header.Get("Location"))
		}
	}
	if _, res := c.post("/plan/generate", url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous post: status %d", res.StatusCode)
	}
	body, res := c.get("/login")
	if res.StatusCode != http.StatusOK || strings.Contains(body, "masthead__nav") {
		t.Errorf("login page: status %d, nav shown", res.StatusCode)
	}
	if _, res := c.get("/manifest.webmanifest"); res.StatusCode != http.StatusOK {
		t.Errorf("manifest: status %d", res.StatusCode)
	}
}

func TestLogin(t *testing.T) {
	app, srv := newApp(t, "invite")
	createFamily(t, app, testLogin, testPassword)
	c := anonymous(t, app, srv)

	body, res := c.post("/login", url.Values{"email": {testLogin}, "password": {"nope"}})
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Identifiant ou mot de passe incorrect.") {
		t.Fatalf("wrong password: status %d", res.StatusCode)
	}
	// The login is case-insensitive; the session outlives the browser (EF-27).
	_, res = c.post("/login", url.Values{"email": {"FAMILLE"}, "password": {testPassword}, "next": {"/shopping"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/shopping" {
		t.Fatalf("login: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	var session *http.Cookie
	for _, ck := range res.Cookies() {
		if ck.Name == "session" {
			session = ck
		}
	}
	if session == nil || !session.HttpOnly || time.Until(session.Expires) < 300*24*time.Hour {
		t.Fatalf("session cookie = %+v", session)
	}
	if body, res := c.get("/"); res.StatusCode != http.StatusOK || !strings.Contains(body, `href="/account"`) {
		t.Fatalf("home once logged in: status %d", res.StatusCode)
	}

	// Log out: the offline copies go with the session.
	_, res = c.post("/logout", url.Values{})
	if res.Header.Get("Clear-Site-Data") == "" || res.Header.Get("Location") != "/login" {
		t.Errorf("logout: %v", res.Header)
	}
	if _, res := c.get("/"); res.StatusCode != http.StatusSeeOther {
		t.Errorf("still logged in: status %d", res.StatusCode)
	}
}

func TestLoginBruteForce(t *testing.T) {
	app, srv := newApp(t, "invite")
	createFamily(t, app, testLogin, testPassword)
	c := anonymous(t, app, srv)
	for range loginMaxFails {
		c.post("/login", url.Values{"email": {testLogin}, "password": {"nope"}})
	}
	// SEC-07: even the right password is refused for a while.
	if _, res := c.post("/login", url.Values{"email": {testLogin}, "password": {testPassword}}); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("blocked login: status %d", res.StatusCode)
	}
	app.Now = func() time.Time { return time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC) }
	if _, res := c.post("/login", url.Values{"email": {testLogin}, "password": {testPassword}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("after the window: status %d", res.StatusCode)
	}
}

func TestSignup(t *testing.T) {
	app, srv := newApp(t, "invite")
	c := anonymous(t, app, srv)
	createFamily(t, app, "prise", testPassword)
	code, _ := app.Accounts.CreateInvite(context.Background(), time.Hour, app.Now())

	form := func(login, pw, confirm, invite string) url.Values {
		return url.Values{"login": {login}, "password": {pw}, "password_confirm": {confirm}, "invite": {invite}}
	}
	for name, f := range map[string]url.Values{
		"no invite":   form("dupont", testPassword, testPassword, ""),
		"bad invite":  form("dupont", testPassword, testPassword, "AAAA-BBBB-CCCC"),
		"bad login":   form("du pont", testPassword, testPassword, code),
		"short login": form("du", testPassword, testPassword, code),
		"short pw":    form("dupont", "court", "court", code),
		"mismatch":    form("dupont", testPassword, testPassword+"!", code),
		"login taken": form("Prise", testPassword, testPassword, code),
	} {
		if _, res := c.post("/signup", f); res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d", name, res.StatusCode)
		}
	}
	_, res := c.post("/signup", form("Dupont", testPassword, testPassword, strings.ToLower(code)))
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings" {
		t.Fatalf("signup: status %d", res.StatusCode)
	}
	// Logged in at once, its database created.
	if _, res := c.get("/settings"); res.StatusCode != http.StatusOK {
		t.Fatalf("after signup: status %d", res.StatusCode)
	}
	fam, err := app.Accounts.FamilyByLogin(context.Background(), "dupont")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app.Families.Path(fam.File)); err != nil {
		t.Fatalf("no database: %v", err)
	}
	// The code is used up.
	other := anonymous(t, app, srv)
	if _, res := other.post("/signup", form("martin", testPassword, testPassword, code)); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("code reused: status %d", res.StatusCode)
	}
}

func TestSignupModes(t *testing.T) {
	app, srv := newApp(t, "open")
	c := anonymous(t, app, srv)
	if body, _ := c.get("/signup"); strings.Contains(body, `name="invite"`) {
		t.Error("open signup asks for a code")
	}
	if _, res := c.post("/signup", url.Values{"login": {"libre"}, "password": {testPassword}, "password_confirm": {testPassword}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("open signup: status %d", res.StatusCode)
	}

	app, srv = newApp(t, "closed")
	c = anonymous(t, app, srv)
	if body, res := c.get("/signup"); res.StatusCode != http.StatusNotFound || !strings.Contains(body, "Les inscriptions sont fermées") {
		t.Errorf("closed signup page: status %d", res.StatusCode)
	}
	if body, _ := c.get("/login"); strings.Contains(body, `href="/signup"`) {
		t.Error("closed signup linked from the login page")
	}
	if _, res := c.post("/signup", url.Values{"login": {"libre"}, "password": {testPassword}, "password_confirm": {testPassword}}); res.StatusCode != http.StatusNotFound {
		t.Errorf("closed signup post: status %d", res.StatusCode)
	}
}

// TestIsolation guards SEC-06: a family never sees another one's data.
func TestIsolation(t *testing.T) {
	a := newClient(t)
	createFamily(t, a.app, "voisins", testPassword)
	b := anonymous(t, a.app, a.srv)
	b.logIn("voisins", testPassword)

	a.addDishes("Lasagnes secrètes")
	if body, _ := b.get("/settings"); strings.Contains(body, "Lasagnes secrètes") {
		t.Fatal("family b sees the dishes of family a")
	}
	items, _ := a.store.Items(context.Background(), 0)
	if _, res := b.post("/settings/items/"+itoa(items[0].ID)+"/delete", url.Values{}); res.StatusCode != http.StatusNotFound {
		t.Errorf("family b deletes an item of a: status %d", res.StatusCode)
	}
	if items, _ = a.store.Items(context.Background(), 0); len(items) != 1 {
		t.Error("the item of family a is gone")
	}
}

func TestAccount(t *testing.T) {
	c := newClient(t)
	other := anonymous(t, c.app, c.srv)
	other.logIn(testLogin, testPassword)

	if body, res := c.get("/account"); res.StatusCode != http.StatusOK || !strings.Contains(body, "Famille créée le 8 octobre 2026.") {
		t.Fatalf("account page: status %d", res.StatusCode)
	}
	newPassword := "un nouveau mot de passe"
	if _, res := c.post("/account/password", url.Values{"current": {"faux"}, "password": {newPassword}, "password_confirm": {newPassword}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("wrong current password: status %d", res.StatusCode)
	}
	if _, res := c.post("/account/password", url.Values{"current": {testPassword}, "password": {newPassword}, "password_confirm": {newPassword}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("change password: status %d", res.StatusCode)
	}
	// EF-28: this device stays, the other one is logged out.
	if _, res := c.get("/"); res.StatusCode != http.StatusOK {
		t.Errorf("this device logged out: status %d", res.StatusCode)
	}
	if _, res := other.get("/"); res.StatusCode != http.StatusSeeOther {
		t.Errorf("other device still logged in: status %d", res.StatusCode)
	}
	other.logIn(testLogin, newPassword)

	// Every device at once.
	if _, res := c.post("/account/logout-all", url.Values{}); res.Header.Get("Location") != "/login" {
		t.Fatalf("logout everywhere: %d", res.StatusCode)
	}
	if _, res := other.get("/"); res.StatusCode != http.StatusSeeOther {
		t.Error("the other device survived logout everywhere")
	}
}

func TestDeleteFamily(t *testing.T) {
	c := newClient(t)
	c.addDishes("Lasagnes")
	fam, _ := c.app.Accounts.FamilyByLogin(context.Background(), testLogin)

	if _, res := c.post("/account/delete", url.Values{"password": {"faux"}, "confirm": {"yes"}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("wrong password: status %d", res.StatusCode)
	}
	if _, res := c.post("/account/delete", url.Values{"password": {testPassword}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("not confirmed: status %d", res.StatusCode)
	}
	// The test client holds the database too: let it go.
	c.release()
	_, res := c.post("/account/delete", url.Values{"password": {testPassword}, "confirm": {"yes"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Clear-Site-Data") == "" {
		t.Fatalf("delete: status %d", res.StatusCode)
	}
	if _, err := os.Stat(c.app.Families.Path(fam.File)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("database left: %v", err)
	}
	if _, res := c.post("/login", url.Values{"email": {testLogin}, "password": {testPassword}}); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleted family logs in: status %d", res.StatusCode)
	}
}
