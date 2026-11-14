package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	codeRe   = regexp.MustCompile(`<p class="invites__code">([A-Z2-9-]{14})</p>`)
	revokeRe = regexp.MustCompile(`action="(/account/invites/\d+/revoke)"`)
)

func TestFamilyInvites(t *testing.T) {
	c := newClient(t)
	app := c.app

	// Off by default: no section, no route.
	if body, _ := c.get("/account"); strings.Contains(body, `id="invites"`) {
		t.Fatal("invitations shown while off")
	}
	if _, res := c.post("/account/invites", url.Values{}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("invite while off: status %d", res.StatusCode)
	}

	app.Options.FamilyInvites, app.Options.InviteTTL, app.Options.InviteMax = true, 7*24*time.Hour, 2
	body, _ := c.get("/account")
	if !strings.Contains(body, "Inviter une famille") || !strings.Contains(body, "Il reste valable 7 jours.") ||
		!strings.Contains(body, `action="/account/invites"`) {
		t.Fatal("EF-34: no invitation section")
	}

	// A code made is shown once, with its link, and never kept.
	if _, res := c.post("/account/invites", url.Values{}); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/account#invites" {
		t.Fatalf("make a code: status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
	body, res := c.get("/account")
	m := codeRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("EF-34: the new code is not shown")
	}
	code := m[1]
	if !strings.Contains(body, c.srv.URL+"/signup?invite="+code) || res.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("EF-34: link missing or page cacheable (%q)", res.Header.Get("Cache-Control"))
	}
	if body, _ = c.get("/account"); codeRe.MatchString(body) {
		t.Error("EF-34: the code is shown twice")
	}
	if !strings.Contains(body, "Créé le 8 octobre 2026, valable jusqu&#39;au 15 octobre 2026") {
		t.Error("EF-34: the pending code is not listed")
	}

	// At most InviteMax pending.
	c.post("/account/invites", url.Values{})
	c.get("/account")
	if body, _ = c.get("/account"); strings.Contains(body, `action="/account/invites"`) || !strings.Contains(body, "Vos 2 codes sont encore en attente") {
		t.Error("EF-34: the limit is not shown")
	}
	if _, res := c.post("/account/invites", url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("over the limit: status %d", res.StatusCode)
	}
	if body, _ = c.get("/account"); codeRe.MatchString(body) || !strings.Contains(body, "Trop de codes en attente") {
		t.Error("EF-34: a code made over the limit")
	}

	// The link fills the code in; the family signs up with it and is listed.
	guest := anonymous(t, app, c.srv)
	if body, _ := guest.get("/signup?invite=" + code); !strings.Contains(body, `name="invite" value="`+code+`"`) {
		t.Error("EF-34: the link does not fill the code in")
	}
	if _, res := guest.post("/signup", url.Values{"login": {"voisins"}, "password": {testPassword}, "password_confirm": {testPassword}, "invite": {code}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("signup with the code of a family: status %d", res.StatusCode)
	}
	body, _ = c.get("/account")
	if !strings.Contains(body, "voisins, inscrite le 8 octobre 2026") || !strings.Contains(body, `action="/account/invites"`) {
		t.Error("EF-34: the invited family is not listed, or no place freed")
	}

	// The invited family invites too, and cannot revoke the codes of another.
	rm := revokeRe.FindStringSubmatch(body)
	if rm == nil {
		t.Fatal("no pending code to revoke")
	}
	if gb, _ := guest.get("/account"); !strings.Contains(gb, `action="/account/invites"`) {
		t.Error("EF-34: an invited family cannot invite")
	} else if m := csrfRe.FindStringSubmatch(gb); m != nil {
		guest.token = m[1]
	}
	if _, res := guest.post(rm[1], url.Values{}); res.StatusCode != http.StatusNotFound {
		t.Errorf("revoke the code of another family: status %d", res.StatusCode)
	}
	if _, res := c.post(rm[1], url.Values{}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("revoke: status %d", res.StatusCode)
	}
	if body, _ = c.get("/account"); revokeRe.MatchString(body) {
		t.Error("EF-34: the revoked code is still pending")
	}
}
