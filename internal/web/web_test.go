package web

import (
	"context"
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

	"leblanc.io/open-go-base/csrfx"
	"leblanc.io/open-go-base/i18n"

	"leblanc.io/open-go-menu/internal/planner"
	"leblanc.io/open-go-menu/internal/store"
)

// client drives the real handler chain (language, CSRF, routes) through a
// cookie-keeping HTTP client that does not follow redirects.
type client struct {
	t     *testing.T
	srv   *httptest.Server
	http  *http.Client
	store *store.Store
	token string
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([0-9a-f]+)"`)

func newClient(t *testing.T) *client {
	t.Helper()
	st, err := store.Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	bundle, err := i18n.NewFS(os.DirFS("../../locales"), ".", "fr")
	if err != nil {
		t.Fatal(err)
	}
	csrf := csrfx.New(csrfx.WithSecure("off"))
	app, err := New(os.DirFS("../../templates"), st, bundle, csrf, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// A fixed "today": Thursday 8 October 2026.
	app.Now = func() time.Time { return time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC) }

	mux := http.NewServeMux()
	app.Routes(mux)
	srv := httptest.NewServer(SecurityHeaders(app.RememberLanguage(csrf.Middleware(mux))))
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	c := &client{t: t, srv: srv, store: st, http: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	body, _ := c.get("/settings")
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no CSRF token in page")
	}
	c.token = m[1]
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

func (c *client) setShare(share string) {
	c.t.Helper()
	form := url.Values{"meal": {"lunch", "dinner"}, "composed_share": {share}}
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
	if _, res := c.post("/settings/meals", url.Values{"meal": {"dinner"}, "composed_share": {"101"}}); res.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("share 101: status %d", res.StatusCode)
	}
	c.post("/settings/meals", url.Values{"meal": {"dinner"}, "composed_share": {"30"}})
	if n, _ := c.store.ComposedShare(context.Background()); n != 30 {
		t.Errorf("share = %d", n)
	}
	body, _ := c.get("/")
	if strings.Contains(body, ">Midi<") || !strings.Contains(body, ">Soir<") {
		t.Error("EF-13: lunch still shown")
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
