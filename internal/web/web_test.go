package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"leblanc.io/open-go-base/appconf"
	"leblanc.io/open-go-base/authx"
	"leblanc.io/open-go-base/csrfx"
	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-go-menu/internal/accounts"
	"leblanc.io/open-go-menu/internal/families"
	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/store"
)

// client drives the real handler chain (language, CSRF, session, routes)
// through a cookie-keeping HTTP client that does not follow redirects. It is
// logged in as a family whose database is store.
type client struct {
	t       *testing.T
	app     *App
	srv     *httptest.Server
	http    *http.Client
	store   *store.Store
	release func() // lets store go
	token   string
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([0-9a-f]+)"`)

const (
	testLogin    = "famille"
	testPassword = "un mot de passe"
)

// newApp serves an application with its accounts and families in a
// temporary directory.
func newApp(t *testing.T, signup string) (*App, *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	accts, err := accounts.Open(context.Background(), filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { accts.Close() })
	fams, err := families.New(filepath.Join(dir, "families"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fams.Close)
	auth := authx.New(appconf.Auth{SessionTTL: SessionTTL, CookieSecure: "off", BcryptCost: 4}, accts.Users, accts.Sessions)

	bundle, err := i18n.NewFS(os.DirFS("../../locales"), ".", "fr")
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfx.New(csrfx.WithSecure("off"))
	app, err := New(os.DirFS("../../templates"), accts, fams, auth,
		Options{Signup: signup, InactiveMonths: 12, BcryptCost: 4},
		bundle, csrf, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// A fixed "today": Thursday 8 October 2026.
	app.Now = func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }

	srv := httptest.NewServer(SecurityHeaders(app.RememberLanguage(csrf.Middleware(app.Handler()))))
	t.Cleanup(srv.Close)
	return app, srv
}

// anonymous returns a client of srv without any session yet.
func anonymous(t *testing.T, app *App, srv *httptest.Server) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, app: app, srv: srv, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	body, _ := c.get("/login")
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no CSRF token in page")
	}
	c.token = m[1]
	return c
}

// createFamily creates a family the way the purge and import commands do.
func createFamily(t *testing.T, app *App, login, password string) accounts.Family {
	t.Helper()
	hash, _ := authx.HashPassword(password, 4)
	fam, err := app.Accounts.CreateFamily(context.Background(), login, hash, "", app.Now())
	if err != nil {
		t.Fatal(err)
	}
	return fam
}

// logIn logs the client in and gives it the database of the family.
func (c *client) logIn(login, password string) {
	c.t.Helper()
	_, res := c.post("/login", url.Values{"email": {login}, "password": {password}})
	if res.StatusCode != http.StatusSeeOther {
		c.t.Fatalf("login %s: status %d", login, res.StatusCode)
	}
	fam, err := c.app.Accounts.FamilyByLogin(context.Background(), login)
	if err != nil {
		c.t.Fatal(err)
	}
	st, release, err := c.app.Families.Acquire(context.Background(), fam.File)
	if err != nil {
		c.t.Fatal(err)
	}
	c.t.Cleanup(release)
	c.store, c.release = st, release
}

func newClient(t *testing.T) *client {
	t.Helper()
	app, srv := newApp(t, "invite")
	createFamily(t, app, testLogin, testPassword)
	c := anonymous(t, app, srv)
	c.logIn(testLogin, testPassword)
	return c
}

func (c *client) get(path string) (string, *http.Response) {
	c.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.srv.URL+path, nil)
	req.Header.Set("Accept-Language", "fr")
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), res
}

func (c *client) post(path string, form url.Values) (string, *http.Response) {
	c.t.Helper()
	if form.Get("csrf_token") == "" {
		form.Set("csrf_token", c.token)
	}
	res, err := c.http.PostForm(c.srv.URL+path, form)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), res
}

func dishValues(name string, weights ...string) url.Values {
	w := []string{"5", "5", "5", "5", "5"}
	copy(w, weights)
	return url.Values{"name": {name}, "weight": {w[0]}, "spring": {w[1]}, "summer": {w[2]}, "autumn": {w[3]}, "winter": {w[4]}}
}

func (c *client) addTo(list string, names ...string) {
	c.t.Helper()
	for _, n := range names {
		if _, res := c.post("/settings/"+list, dishValues(n)); res.StatusCode != http.StatusSeeOther {
			c.t.Fatalf("add %q to %s: status %d", n, list, res.StatusCode)
		}
	}
}

func (c *client) addDishes(names ...string) {
	c.t.Helper()
	c.addTo("dishes", names...)
}

// allSlots ticks every meal of every weekday in the meals tab.
func allSlots() []string {
	var slots []string
	for _, d := range weekdayNames {
		slots = append(slots, d+"/lunch", d+"/dinner")
	}
	return slots
}

func (c *client) setShare(share string) {
	c.t.Helper()
	form := url.Values{"slot": allSlots(), "composed_share": {share}}
	if _, res := c.post("/settings/meals", form); res.StatusCode != http.StatusSeeOther {
		c.t.Fatalf("share: status %d", res.StatusCode)
	}
}

var plateRe = regexp.MustCompile(`<span class="plate__name">([^<]+)</span>`)

var dishRe = regexp.MustCompile(`<p class="meal__dish">([^<]+)</p>`)

func dishesShown(body string) []string {
	var out []string
	for _, m := range dishRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestEmptyWeek(t *testing.T) {
	c := newClient(t)
	body, res := c.get("/")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if !strings.Contains(body, "Du 5 octobre au 11 octobre 2026") {
		t.Error("week range missing")
	}
	if !strings.Contains(body, `href="/settings#add"`) {
		t.Error("TR-01: no invitation to add dishes")
	}
	if res.Header.Get("Content-Security-Policy") == "" {
		t.Error("no CSP header")
	}
}

func TestGenerateWeek(t *testing.T) {
	c := newClient(t)
	c.addDishes("A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P")

	body, _ := c.get("/")
	if !strings.Contains(body, "Générer") {
		t.Fatal("EF-03: no generate button")
	}
	_, res := c.post("/plan/generate", url.Values{"week": {"2026-10-05"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("generate: status %d", res.StatusCode)
	}
	body, _ = c.get(res.Header.Get("Location"))
	if !strings.Contains(body, "Semaine générée.") {
		t.Error("no flash message")
	}
	shown := dishesShown(body)
	if len(shown) != 14 {
		t.Fatalf("TR-02: %d meals shown", len(shown))
	}
	seen := map[string]bool{}
	for _, d := range shown {
		if seen[d] {
			t.Errorf("TR-02: %q twice", d)
		}
		seen[d] = true
	}
	if strings.Contains(body, "plate__name") {
		t.Error("composed meal drawn without any component")
	}
	// The flash is consumed.
	if body, _ = c.get("/?week=2026-10-05"); strings.Contains(body, "Semaine générée.") {
		t.Error("flash shown twice")
	}
}

func TestWeekStartKeepsPlan(t *testing.T) {
	c := newClient(t)
	c.addDishes("A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S", "T")
	c.post("/plan/generate", url.Values{"week": {"2026-10-05"}})

	if _, res := c.post("/week-start", url.Values{"week_start": {"6"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("week start: %d", res.StatusCode)
	}
	body, _ := c.get("/")
	if !strings.Contains(body, "Du 3 octobre au 9 octobre 2026") {
		t.Fatal("week did not move to Saturday")
	}
	if !strings.Contains(body, "Compléter les repas vides") {
		t.Fatal("TR-04: no fill button on a partial week")
	}
	before := dishesShown(body)

	c.post("/plan/generate", url.Values{"week": {"2026-10-03"}, "mode": {"fill"}})
	body, _ = c.get("/")
	after := dishesShown(body)
	// 3–4 October were empty, 5–9 October kept their dishes.
	for i := 4; i < 14; i++ {
		if before[i] != after[i] {
			t.Errorf("TR-04: slot %d changed from %q to %q", i, before[i], after[i])
		}
	}
	if strings.Contains(body, "Rien de prévu") {
		t.Error("TR-04: empty meals left")
	}
}

func TestSlotActions(t *testing.T) {
	c := newClient(t)
	c.addDishes("Soupe", "Salade", "Gratin")
	ctx := context.Background()
	dishes, _ := c.store.Items(ctx, planner.Dish)

	if _, res := c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}, "dish_id": {itoa(dishes[0].ID)}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("set: %d", res.StatusCode)
	}
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	entries, _ := c.store.Entries(ctx, day, day)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
	for _, e := range entries {
		if e.Plate[planner.Dish] != dishes[0].ID {
			t.Fatal("set: wrong dish")
		}
	}

	c.post("/plan/2026-10-08/dinner", url.Values{"action": {"reroll"}})
	entries, _ = c.store.Entries(ctx, day, day)
	for _, e := range entries {
		if e.Plate[planner.Dish] == dishes[0].ID {
			t.Error("TR-05: reroll kept the same dish")
		}
	}

	c.post("/plan/2026-10-08/dinner", url.Values{"action": {"clear"}})
	if entries, _ = c.store.Entries(ctx, day, day); len(entries) != 0 {
		t.Error("clear left an entry")
	}

	for _, path := range []string{"/plan/2026-13-01/dinner", "/plan/2026-10-08/brunch"} {
		if _, res := c.post(path, url.Values{"action": {"clear"}}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", path, res.StatusCode)
		}
	}
	if _, res := c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}, "dish_id": {"999"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown dish: status %d", res.StatusCode)
	}
}

func TestMoveMeal(t *testing.T) {
	c := newClient(t)
	c.addDishes("Soupe", "Salade")
	ctx := context.Background()
	dishes, _ := c.store.Items(ctx, planner.Dish)
	c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}, "dish_id": {itoa(dishes[0].ID)}})
	c.post("/plan/2026-10-09/lunch", url.Values{"action": {"set"}, "dish_id": {itoa(dishes[1].ID)}})

	body, _ := c.get("/?week=2026-10-05")
	if !strings.Contains(body, `<option value="2026-10-09/lunch">Vendredi 9 octobre — Midi (échange)</option>`) {
		t.Error("the move form does not offer the filled slot as a swap")
	}

	_, res := c.post("/plan/2026-10-08/dinner", url.Values{"action": {"move"}, "to": {"2026-10-09/lunch"}, "week": {"2026-10-05"}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/?week=2026-10-05#day-2026-10-09" {
		t.Fatalf("move: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	from, to := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	entries, _ := c.store.Entries(ctx, from, to)
	if entries[planner.Slot{Day: from, Meal: planner.Dinner}].Plate[planner.Dish] != dishes[1].ID ||
		entries[planner.Slot{Day: to, Meal: planner.Lunch}].Plate[planner.Dish] != dishes[0].ID {
		t.Fatalf("the two meals were not swapped: %+v", entries)
	}

	for _, target := range []string{"", "2026-10-08/dinner", "2026-10-08/brunch", "nope"} {
		if _, res := c.post("/plan/2026-10-08/dinner", url.Values{"action": {"move"}, "to": {target}}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("move to %q: status %d", target, res.StatusCode)
		}
	}
}

func TestSettingsValidation(t *testing.T) {
	c := newClient(t)
	c.addDishes("Lasagnes")

	body, res := c.post("/settings/dishes", dishValues("lasagnes"))
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, "déjà dans cette liste") {
		t.Errorf("duplicate: status %d", res.StatusCode)
	}
	body, res = c.post("/settings/dishes", dishValues("Pizza", "5", "5", "11"))
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, `value="Pizza"`) {
		t.Errorf("EI-05: invalid weight not redisplayed (status %d)", res.StatusCode)
	}
	if _, res = c.post("/settings/dishes", dishValues(" ")); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("blank name: status %d", res.StatusCode)
	}

	// Bulk save: summer weight to 0.
	dishes, _ := c.store.Items(context.Background(), planner.Dish)
	id := itoa(dishes[0].ID)
	form := url.Values{"id": {id}, "name_" + id: {"Lasagnes"}, "weight_" + id: {"8"},
		"spring_" + id: {"5"}, "summer_" + id: {"0"}, "autumn_" + id: {"7"}, "winter_" + id: {"10"}}
	if _, res = c.post("/settings/dishes/save", form); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: status %d", res.StatusCode)
	}
	dishes, _ = c.store.Items(context.Background(), planner.Dish)
	if d := dishes[0]; d.Weight != 8 || d.Seasonal != [4]int{5, 0, 7, 10} {
		t.Errorf("saved = %+v", d)
	}

	// TR-03: never drawn in July.
	c.post("/plan/generate", url.Values{"week": {"2026-07-06"}})
	body, _ = c.get("/?week=2026-07-06")
	if strings.Contains(body, `class="meal__dish">Lasagnes`) {
		t.Error("TR-03: summer-excluded dish drawn in July")
	}
}

func TestDeleteKeepsPlan(t *testing.T) {
	c := newClient(t)
	c.addDishes("Pot-au-feu")
	dishes, _ := c.store.Items(context.Background(), planner.Dish)
	c.post("/plan/generate", url.Values{"week": {"2026-10-05"}})

	_, res := c.post("/settings/items/"+itoa(dishes[0].ID)+"/delete", url.Values{})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings/dishes" {
		t.Fatalf("delete: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	body, _ := c.get("/settings")
	if strings.Contains(body, `value="Pot-au-feu"`) {
		t.Error("TR-06: deleted dish still listed")
	}
	body, _ = c.get("/?week=2026-10-05")
	if !strings.Contains(body, "Pot-au-feu") {
		t.Error("TR-06: deleted dish gone from the plan")
	}
}

func TestMeals(t *testing.T) {
	c := newClient(t)
	if _, res := c.post("/settings/meals", url.Values{"composed_share": {"50"}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("no meal: status %d", res.StatusCode)
	}
	if _, res := c.post("/settings/meals", url.Values{"slot": {"monday/dinner"}, "composed_share": {"101"}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("share 101: status %d", res.StatusCode)
	}
	if _, res := c.post("/settings/meals", url.Values{"slot": {"someday/dinner"}, "composed_share": {"50"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown weekday: status %d", res.StatusCode)
	}
	// EF-13: dinner every day, lunch on Wednesday only.
	slots := []string{"wednesday/lunch"}
	for _, d := range weekdayNames {
		slots = append(slots, d+"/dinner")
	}
	c.post("/settings/meals", url.Values{"slot": slots, "composed_share": {"30"}})
	if n, _ := c.store.ComposedShare(context.Background()); n != 30 {
		t.Errorf("share = %d", n)
	}
	c.addDishes("A", "B", "C", "D", "E", "F", "G", "H", "I", "J")
	c.post("/plan/generate", url.Values{"week": {"2026-10-05"}})
	body, _ := c.get("/?week=2026-10-05")
	if n := strings.Count(body, `<h3 class="meal__slot">Midi</h3>`); n != 1 {
		t.Errorf("EF-13: %d lunches shown", n)
	}
	if n := len(dishesShown(body)); n != 8 {
		t.Errorf("EF-13: %d meals drawn, want 8", n)
	}
	if !strings.Contains(body, "Régénérer") || strings.Contains(body, "Compléter") {
		t.Error("the week should be full")
	}
}

func TestDayWithoutMeal(t *testing.T) {
	c := newClient(t)
	c.post("/settings/meals", url.Values{"slot": {"monday/lunch"}, "composed_share": {"50"}})
	body, _ := c.get("/?week=2026-10-05")
	if n := strings.Count(body, "Aucun repas à prévoir ce jour-là."); n != 6 {
		t.Errorf("%d days without meal, want 6", n)
	}
}

func TestFixedMeal(t *testing.T) {
	c := newClient(t)
	c.addDishes("Pizza", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "M", "N")
	ctx := context.Background()
	dishes, _ := c.store.Items(ctx, planner.Dish)
	var pizza int64
	for _, d := range dishes {
		if d.Name == "Pizza" {
			pizza = d.ID
		}
	}

	if _, res := c.get("/settings/meals/someday/lunch"); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown weekday: status %d", res.StatusCode)
	}
	body, res := c.get("/settings/meals/wednesday/lunch")
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Repas fixe : Mercredi — Midi") {
		t.Fatalf("fixed page: status %d", res.StatusCode)
	}
	_, res = c.post("/settings/meals/wednesday/lunch", url.Values{"action": {"set"}, "dish_id": {itoa(pizza)}})
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/settings/meals#weekday-wednesday" {
		t.Fatalf("set: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	if body, _ := c.get("/settings/meals"); !strings.Contains(body, `<span class="schedule__names">Pizza</span>`) {
		t.Error("fixed meal not shown in the meals tab")
	}

	// EF-18: every generation serves it, and only there.
	for range 5 {
		c.post("/plan/generate", url.Values{"week": {"2026-10-05"}, "mode": {"all"}})
		entries, _ := c.store.Entries(ctx, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC))
		for s, e := range entries {
			wednesdayLunch := s.Day.Weekday() == time.Wednesday && s.Meal == planner.Lunch
			if wednesdayLunch != (e.Plate[planner.Dish] == pizza) {
				t.Fatalf("RG-10: %v holds %v", s, e.Names)
			}
		}
	}
	// A re-roll of the fixed meal draws at random.
	c.post("/plan/2026-10-07/lunch", url.Values{"action": {"reroll"}, "week": {"2026-10-05"}})
	wed := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	if e, _ := c.store.Entries(ctx, wed, wed); e[planner.Slot{Day: wed, Meal: planner.Lunch}].Plate[planner.Dish] == pizza {
		t.Error("re-roll kept the fixed meal")
	}

	// Nothing chosen, then back to the draw.
	if _, res := c.post("/settings/meals/wednesday/lunch", url.Values{"action": {"set"}}); res.Header.Get("Location") != "/settings/meals/wednesday/lunch" {
		t.Errorf("nothing chosen: location %q", res.Header.Get("Location"))
	}
	c.post("/settings/meals/wednesday/lunch", url.Values{"action": {"clear"}})
	if fixed, _ := c.store.FixedPlates(ctx); len(fixed) != 0 {
		t.Errorf("fixed = %v", fixed)
	}
}

func TestIngredients(t *testing.T) {
	c := newClient(t)
	c.addDishes("Lasagnes")
	c.addTo("proteins", "Bœuf")
	ctx := context.Background()
	dishes, _ := c.store.Items(ctx, planner.Dish)
	proteins, _ := c.store.Items(ctx, planner.Protein)
	page := "/settings/items/" + itoa(dishes[0].ID)

	if body, _ := c.get("/settings"); !strings.Contains(body, `href="`+page+`">Ingrédients</a>`) {
		t.Error("no link to the ingredients")
	}
	if _, res := c.get("/settings/items/" + itoa(proteins[0].ID)); res.StatusCode != http.StatusNotFound {
		t.Errorf("ingredients of a protein: status %d", res.StatusCode)
	}
	body, res := c.get(page)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Ingrédients : Lasagnes") {
		t.Fatalf("dish page: status %d", res.StatusCode)
	}
	// EF-19: the components are suggested.
	if !strings.Contains(body, `<option value="Bœuf">`) {
		t.Error("the protein is not suggested")
	}

	for _, n := range []string{"Bœuf", "Pâtes", "Tomate"} {
		if _, res := c.post(page+"/ingredients", url.Values{"name": {n}}); res.StatusCode != http.StatusSeeOther {
			t.Fatalf("add %q: status %d", n, res.StatusCode)
		}
	}
	if body, res := c.post(page+"/ingredients", url.Values{"name": {"tomate"}}); res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, `value="tomate"`) {
		t.Errorf("duplicate: status %d", res.StatusCode)
	}
	if _, res := c.post(page+"/ingredients", url.Values{"name": {" "}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("empty: status %d", res.StatusCode)
	}
	if body, _ := c.get("/settings"); !strings.Contains(body, ">3 ingrédients</a>") {
		t.Error("ingredient count missing")
	}
	// EF-19: an ingredient never joins a component list.
	if starches, _ := c.store.Items(ctx, planner.Starch); len(starches) != 0 {
		t.Errorf("starches = %+v", starches)
	}

	got, _ := c.store.DishIngredients(ctx, dishes[0].ID)
	if _, res := c.post(page+"/ingredients/"+itoa(got[1].ArticleID)+"/delete", url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("remove: status %d", res.StatusCode)
	}
	if _, res := c.post(page+"/ingredients/"+itoa(got[1].ArticleID)+"/delete", url.Values{}); res.StatusCode != http.StatusNotFound {
		t.Errorf("second remove: status %d", res.StatusCode)
	}
	if body, _ := c.get("/settings"); !strings.Contains(body, ">2 ingrédients</a>") {
		t.Error("count not updated")
	}
}

func TestQuantities(t *testing.T) {
	c := newClient(t)
	c.addDishes("Lasagnes")
	c.post("/settings/aisles", url.Values{"name": {"Boucherie"}})
	ctx := context.Background()
	dishes, _ := c.store.Items(ctx, planner.Dish)
	aisles, _ := c.store.Aisles(ctx)
	page := "/settings/items/" + itoa(dishes[0].ID)

	if _, res := c.post(page+"/ingredients", url.Values{"name": {"Bœuf"}, "quantity": {"500 g"}, "aisle": {itoa(aisles[0].ID)}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("add: status %d", res.StatusCode)
	}
	if _, res := c.post(page+"/ingredients", url.Values{"name": {"Sel"}, "aisle": {"999"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown aisle: status %d", res.StatusCode)
	}
	got, _ := c.store.DishIngredients(ctx, dishes[0].ID)
	articles, _ := c.store.Articles(ctx)
	if len(got) != 1 || got[0].Quantity != "500 g" || articles[0].Aisle != aisles[0].ID {
		t.Fatalf("ingredients = %+v, articles = %+v", got, articles)
	}
	id := itoa(got[0].ArticleID)
	if _, res := c.post(page+"/ingredients/save", url.Values{"id": {id}, "quantity_" + id: {strings.Repeat("x", 51)}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("long quantity: status %d", res.StatusCode)
	}
	c.post(page+"/ingredients/save", url.Values{"id": {id}, "quantity_" + id: {"1 kg"}})
	if got, _ = c.store.DishIngredients(ctx, dishes[0].ID); got[0].Quantity != "1 kg" {
		t.Errorf("quantity = %q", got[0].Quantity)
	}
}

func TestAislesAndArticlesTabs(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()
	for _, n := range []string{"Légumes", "Boucherie", "Boissons"} {
		if _, res := c.post("/settings/aisles", url.Values{"name": {n}}); res.StatusCode != http.StatusSeeOther {
			t.Fatalf("add aisle: status %d", res.StatusCode)
		}
	}
	if _, res := c.post("/settings/aisles", url.Values{"name": {"boissons"}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("duplicate aisle: status %d", res.StatusCode)
	}
	aisles, _ := c.store.Aisles(ctx)
	_, res := c.post("/settings/aisles/"+itoa(aisles[2].ID)+"/move", url.Values{"dir": {"up"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("move: status %d", res.StatusCode)
	}
	if aisles, _ = c.store.Aisles(ctx); aisles[1].Name != "Boissons" {
		t.Errorf("order = %+v", aisles)
	}
	body, _ := c.get("/settings/aisles")
	if !strings.Contains(body, `aria-label="Monter « Légumes »" title="Monter « Légumes »" disabled`) {
		t.Error("the first aisle can go up")
	}
	// The order dropped by the script, every aisle once.
	order := url.Values{"id": {itoa(aisles[2].ID), itoa(aisles[0].ID), itoa(aisles[1].ID)}}
	if _, res := c.post("/settings/aisles/order", order); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("order: status %d", res.StatusCode)
	}
	if got, _ := c.store.Aisles(ctx); got[0].ID != aisles[2].ID {
		t.Errorf("order = %+v", got)
	}
	if _, res := c.post("/settings/aisles/order", url.Values{"id": {itoa(aisles[0].ID)}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("partial order: status %d", res.StatusCode)
	}
	c.post("/settings/aisles/order", url.Values{"id": {itoa(aisles[0].ID), itoa(aisles[1].ID), itoa(aisles[2].ID)}})
	aisles, _ = c.store.Aisles(ctx)
	form := url.Values{"id": {itoa(aisles[0].ID), itoa(aisles[1].ID)}}
	form.Set("name_"+itoa(aisles[0].ID), "Primeur")
	form.Set("name_"+itoa(aisles[1].ID), "primeur")
	if _, res := c.post("/settings/aisles/save", form); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("two aisles named alike: status %d", res.StatusCode)
	}

	c.post("/settings/articles", url.Values{"name": {"Bière"}, "aisle": {itoa(aisles[1].ID)}})
	c.post("/settings/articles", url.Values{"name": {"Savon"}})
	articles, _ := c.store.Articles(ctx)
	if len(articles) != 2 || articles[0].Aisle != aisles[1].ID {
		t.Fatalf("articles = %+v", articles)
	}
	form = url.Values{"id": {itoa(articles[1].ID)}, "name_" + itoa(articles[1].ID): {"Shampoing"}, "aisle_" + itoa(articles[1].ID): {itoa(aisles[0].ID)}}
	if _, res := c.post("/settings/articles/save", form); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save articles: status %d", res.StatusCode)
	}
	// The aisle choices are sorted by name, not in the walk order (Légumes
	// first).
	body, _ = c.get("/settings/articles")
	if i, j := strings.Index(body, ">Boissons</option>"), strings.Index(body, ">Légumes</option>"); i < 0 || j < 0 || i > j {
		t.Errorf("aisle choices not sorted by name: %d %d", i, j)
	}
	// An ingredient may not be deleted; an aisle gone frees its articles.
	c.addDishes("Lasagnes")
	dishes, _ := c.store.Items(ctx, planner.Dish)
	c.post("/settings/items/"+itoa(dishes[0].ID)+"/ingredients", url.Values{"name": {"bière"}})
	if _, res := c.post("/settings/articles/"+itoa(articles[0].ID)+"/delete", url.Values{}); res.Header.Get("Location") != "/settings/articles" {
		t.Errorf("delete in use: %d", res.StatusCode)
	}
	c.post("/settings/aisles/"+itoa(aisles[0].ID)+"/delete", url.Values{})
	articles, _ = c.store.Articles(ctx)
	if len(articles) != 2 || articles[1].Name != "Shampoing" || articles[1].Aisle != 0 {
		t.Fatalf("after deletes: %+v", articles)
	}
	body, _ = c.get("/settings/articles")
	if !strings.Contains(body, "Dans 1 recette") {
		t.Error("uses not shown")
	}

}

func TestShoppingList(t *testing.T) {
	c := newClient(t)
	ctx := context.Background()

	// No complete week: the days are to be chosen.
	if body, _ := c.get("/shopping"); !strings.Contains(body, "Aucune semaine n&#39;est entièrement prévue") || !strings.Contains(body, `value="2026-10-05"`) {
		t.Fatal("no date form")
	}
	c.addDishes("Lasagnes", "Raclette", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L")
	c.addTo("proteins", "Bœuf")
	c.post("/settings/aisles", url.Values{"name": {"Boucherie"}})
	c.post("/settings/aisles", url.Values{"name": {"Épicerie"}})
	dishes, _ := c.store.Items(ctx, planner.Dish)
	proteins, _ := c.store.Items(ctx, planner.Protein)
	aisles, _ := c.store.Aisles(ctx)
	var lasagna, raclette int64
	for _, d := range dishes {
		switch d.Name {
		case "Lasagnes":
			lasagna = d.ID
		case "Raclette":
			raclette = d.ID
		}
	}
	c.post("/settings/items/"+itoa(lasagna)+"/ingredients", url.Values{"name": {"bœuf"}, "quantity": {"500 g"}, "aisle": {itoa(aisles[0].ID)}})
	c.post("/settings/items/"+itoa(lasagna)+"/ingredients", url.Values{"name": {"Pâtes à lasagne"}, "aisle": {itoa(aisles[1].ID)}})

	// The next week fully planned is proposed.
	c.post("/plan/generate", url.Values{"week": {"2026-10-12"}})
	body, _ := c.get("/shopping")
	if !strings.Contains(body, "Semaine du 12 octobre au 18 octobre 2026") {
		t.Fatal("EF-22: the complete week is not proposed")
	}
	for _, bad := range []url.Values{
		{"from": {"2026-10-12"}, "to": {"nope"}},
		{"from": {"2026-10-12"}, "to": {"2026-10-11"}},
		{"from": {"2026-10-01"}, "to": {"2026-11-01"}},
	} {
		if _, res := c.post("/shopping", bad); res.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%v: status %d", bad, res.StatusCode)
		}
	}

	d := func(s string) time.Time { day, _ := planner.ParseDay(s); return day }
	var week []planner.Slot
	for _, day := range planner.WeekDays(d("2026-10-12")) {
		week = append(week, planner.Slot{Day: day, Meal: planner.Lunch}, planner.Slot{Day: day, Meal: planner.Dinner})
	}
	_ = c.store.ClearEntries(ctx, week...)
	_ = c.store.SetPlates(ctx, map[planner.Slot]planner.Plate{
		{Day: d("2026-10-12"), Meal: planner.Lunch}:  {planner.Protein: proteins[0].ID},
		{Day: d("2026-10-13"), Meal: planner.Dinner}: {planner.Dish: lasagna},
		{Day: d("2026-10-14"), Meal: planner.Dinner}: {planner.Dish: raclette},
	})
	if _, res := c.post("/shopping", url.Values{"from": {"2026-10-12"}, "to": {"2026-10-18"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("start: status %d", res.StatusCode)
	}
	c.post("/shopping/extras", url.Values{"name": {"Papier toilette"}, "quantity": {"6 rouleaux"}})
	if _, res := c.post("/shopping/extras", url.Values{"name": {"papier toilette"}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("extra twice: status %d", res.StatusCode)
	}

	body, _ = c.get("/shopping")
	// RG-11: Boucherie, then Épicerie, then no aisle.
	iMeat, iShelf, iNone := strings.Index(body, ">Boucherie</h2>"), strings.Index(body, ">Épicerie</h2>"), strings.Index(body, ">Sans rayon</h2>")
	if iMeat < 0 || iMeat > iShelf || iShelf > iNone {
		t.Fatalf("aisles out of order: %d %d %d", iMeat, iShelf, iNone)
	}
	// One line for the protein and the ingredient, with both meals.
	if strings.Count(body, `<p class="line__name">bœuf</p>`) != 1 ||
		!strings.Contains(body, "Lundi 12 octobre, Midi") ||
		!strings.Contains(body, `<span class="line__quantity">500 g</span> <span class="line__why">Lasagnes — Mardi 13 octobre, Soir</span>`) {
		t.Error("EF-24: the beef line is wrong")
	}
	if !strings.Contains(body, "Raclette — Mercredi 14 octobre, Soir") {
		t.Error("the dish without ingredients is not reported")
	}

	// RG-12: tick, then a new meal needing beef unticks it.
	articles, _ := c.store.Articles(ctx)
	var beef int64
	for _, a := range articles {
		if a.Name == "bœuf" {
			beef = a.ID
		}
	}
	key := "2026-10-12/lunch,2026-10-13/dinner"
	if _, res := c.post("/shopping/articles/"+itoa(beef), url.Values{"action": {"tick"}, "needs": {key}, "anchor": {"aisle-1"}}); res.Header.Get("Location") != "/shopping#aisle-1" {
		t.Fatalf("tick: location %q", res.Header.Get("Location"))
	}
	body, _ = c.get("/shopping")
	if i := strings.Index(body, ">Dans le panier</h2>"); i < 0 || !strings.Contains(body[i:], `<p class="line__name">bœuf</p>`) {
		t.Fatal("the ticked line is not in the basket")
	}
	_ = c.store.SetPlates(ctx, map[planner.Slot]planner.Plate{{Day: d("2026-10-16"), Meal: planner.Lunch}: {planner.Protein: proteins[0].ID}})
	if body, _ = c.get("/shopping"); strings.Contains(body, ">Dans le panier</h2>") {
		t.Error("RG-12: a new need left the line ticked")
	}

	// File the article without aisle, take back the extra.
	var paper int64
	for _, a := range articles {
		if a.Name == "Papier toilette" {
			paper = a.ID
		}
	}
	c.post("/shopping/articles/"+itoa(paper), url.Values{"action": {"aisle"}, "aisle": {itoa(aisles[1].ID)}})
	c.post("/shopping/articles/"+itoa(paper), url.Values{"action": {"remove"}})
	if body, _ = c.get("/shopping"); strings.Contains(body, "Papier toilette</p>") {
		t.Error("the extra is still listed")
	}
	if _, res := c.post("/shopping/articles/"+itoa(paper), url.Values{"action": {"tick"}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("tick without needs: status %d", res.StatusCode)
	}

	// A new list starts afresh.
	c.post("/shopping/extras", url.Values{"name": {"Bière"}})
	if body, _ := c.get("/shopping/new"); !strings.Contains(body, "remplace la liste en cours") {
		t.Error("no warning about replacing the list")
	}
	c.post("/shopping", url.Values{"from": {"2026-10-12"}, "to": {"2026-10-12"}})
	if body, _ = c.get("/shopping"); strings.Contains(body, "Bière</p>") || !strings.Contains(body, "bœuf</p>") {
		t.Error("EF-22: the new list keeps the previous additions")
	}
}

func TestLanguage(t *testing.T) {
	c := newClient(t)
	body, _ := c.get("/?lang=en")
	if !strings.Contains(body, `<html lang="en">`) || !strings.Contains(body, "October 5 to October 11, 2026") {
		t.Fatal("TR-07: not in English")
	}
	// The choice sticks through the cookie.
	body, _ = c.get("/settings")
	if !strings.Contains(body, "General weight") {
		t.Error("TR-07: language not remembered")
	}
}

func TestCSRF(t *testing.T) {
	c := newClient(t)
	if _, res := c.post("/settings/dishes", url.Values{"csrf_token": {"forged"}, "name": {"X"}}); res.StatusCode != http.StatusForbidden {
		t.Errorf("TR-08: status %d", res.StatusCode)
	}
}

func TestNotFound(t *testing.T) {
	c := newClient(t)
	if _, res := c.get("/nope"); res.StatusCode != http.StatusNotFound {
		t.Errorf("status %d", res.StatusCode)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func TestComposedMeals(t *testing.T) {
	c := newClient(t)
	c.addTo("starches", "Riz", "Pâtes", "Pommes de terre")
	c.addTo("proteins", "Saumon", "Poulet", "Bœuf")
	c.addTo("vegetables", "Haricots verts", "Carottes", "Courgettes")
	c.addDishes("Lasagnes")
	c.setShare("100")

	c.post("/plan/generate", url.Values{"week": {"2026-10-05"}})
	body, _ := c.get("/?week=2026-10-05")
	if n := len(plateRe.FindAllString(body, -1)); n != 42 {
		t.Fatalf("TR-09: %d components shown, want 14 × 3", n)
	}
	if strings.Contains(body, "Lasagnes</p>") {
		t.Error("TR-09: full dish drawn at 100 %")
	}

	// TR-11: re-rolling the vegetable keeps the rest.
	ctx := context.Background()
	day := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	slot := planner.Slot{Day: day, Meal: planner.Dinner}
	before, _ := c.store.Entries(ctx, day, day)
	c.post("/plan/2026-10-08/dinner", url.Values{"action": {"reroll"}, "kind": {"vegetable"}, "week": {"2026-10-05"}})
	after, _ := c.store.Entries(ctx, day, day)
	b, a := before[slot].Plate, after[slot].Plate
	if a[planner.Vegetable] == b[planner.Vegetable] || a[planner.Starch] != b[planner.Starch] || a[planner.Protein] != b[planner.Protein] {
		t.Errorf("TR-11: before %v, after %v", b, a)
	}

	// Manual choice: a full dish replaces the components…
	starches, _ := c.store.Items(ctx, planner.Starch)
	dishes, _ := c.store.Items(ctx, planner.Dish)
	c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}, "dish_id": {itoa(dishes[0].ID)}, "starch_id": {itoa(starches[0].ID)}})
	got, _ := c.store.Entries(ctx, day, day)
	if p := got[slot].Plate; p != (planner.Plate{planner.Dish: dishes[0].ID}) {
		t.Errorf("set dish: %v", p)
	}
	// …and components alone make a composed meal.
	c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}, "dish_id": {""}, "starch_id": {itoa(starches[0].ID)}})
	got, _ = c.store.Entries(ctx, day, day)
	if p := got[slot].Plate; p != (planner.Plate{planner.Starch: starches[0].ID}) {
		t.Errorf("set components: %v", p)
	}
	// An item chosen in the wrong list is refused.
	if _, res := c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}, "protein_id": {itoa(starches[0].ID)}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong list: status %d", res.StatusCode)
	}
	// Nothing chosen.
	if _, res := c.post("/plan/2026-10-08/dinner", url.Values{"action": {"set"}}); res.StatusCode != http.StatusSeeOther {
		t.Errorf("nothing chosen: status %d", res.StatusCode)
	}
}

func TestComposedFallback(t *testing.T) {
	c := newClient(t)
	c.addTo("proteins", "Saumon", "Poulet")
	c.addTo("vegetables", "Carottes", "Courgettes")
	c.setShare("0")
	c.post("/plan/generate", url.Values{"week": {"2026-10-05"}})
	body, _ := c.get("/?week=2026-10-05")
	// TR-10 and RG-09: no full dish, no starch → meat/fish + vegetable.
	if n := len(plateRe.FindAllString(body, -1)); n != 28 {
		t.Errorf("TR-10: %d components shown, want 14 × 2", n)
	}
}

func TestSettingsTabs(t *testing.T) {
	c := newClient(t)
	for path, want := range map[string]string{
		"/settings":            "Ajouter un plat complet",
		"/settings/starches":   "Ajouter un féculent",
		"/settings/proteins":   "Ajouter une viande ou un poisson",
		"/settings/vegetables": "Ajouter un légume",
		"/settings/meals":      "Part de repas composés",
	} {
		body, res := c.get(path)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, want) {
			t.Errorf("%s: status %d, %q missing", path, res.StatusCode, want)
		}
	}
	if _, res := c.get("/settings/desserts"); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown list: status %d", res.StatusCode)
	}
	// TR-12: same name in two lists, not twice in one.
	c.addTo("starches", "Riz")
	c.addDishes("Riz")
	if _, res := c.post("/settings/starches", dishValues("riz")); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("TR-12: duplicate starch: status %d", res.StatusCode)
	}
}

func TestManifest(t *testing.T) {
	c := newClient(t)
	body, _ := c.get("/")
	if !strings.Contains(body, `<link rel="manifest" href="/manifest.webmanifest" crossorigin="use-credentials">`) {
		t.Error("EI-06: no manifest link")
	}
	body, res := c.get("/manifest.webmanifest?lang=en")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("status %d, type %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	var m webManifest
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	if m.Lang != "en" || m.Display != "standalone" || m.StartURL != "/" || len(m.Shortcuts) != 2 || m.Shortcuts[1].Name != "Shopping" {
		t.Errorf("manifest = %+v", m)
	}
	// Every icon is served.
	for _, icon := range m.Icons {
		if !strings.HasPrefix(icon.Src, "/static/") {
			t.Errorf("icon %s", icon.Src)
			continue
		}
		if _, err := os.Stat("../.." + icon.Src); err != nil {
			t.Errorf("icon %s: %v", icon.Src, err)
		}
	}
}
