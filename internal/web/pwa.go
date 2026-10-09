package web

import (
	"encoding/json"
	"net/http"
)

// The colours of the installed app, mirroring the light tokens of main.css.
const (
	themeColor      = "#276749"
	backgroundColor = "#f4f7f3"
)

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type manifestShortcut struct {
	Name  string         `json:"name"`
	URL   string         `json:"url"`
	Icons []manifestIcon `json:"icons,omitempty"`
}

type webManifest struct {
	ID              string             `json:"id"`
	Name            string             `json:"name"`
	ShortName       string             `json:"short_name"`
	Description     string             `json:"description"`
	Lang            string             `json:"lang"`
	Dir             string             `json:"dir"`
	StartURL        string             `json:"start_url"`
	Scope           string             `json:"scope"`
	Display         string             `json:"display"`
	ThemeColor      string             `json:"theme_color"`
	BackgroundColor string             `json:"background_color"`
	Icons           []manifestIcon     `json:"icons"`
	Shortcuts       []manifestShortcut `json:"shortcuts"`
}

// manifest serves the web app manifest that makes the application
// installable (EI-06), in the language of the request.
func (a *App) manifest(w http.ResponseWriter, r *http.Request) {
	loc := a.localizer(r)
	icon := func(size, purpose string) manifestIcon {
		name := "icon-"
		if purpose == "maskable" {
			name = "icon-maskable-"
		}
		return manifestIcon{Src: "/static/icons/" + name + size + ".png", Sizes: size + "x" + size, Type: "image/png", Purpose: purpose}
	}
	m := webManifest{
		ID:              "/",
		Name:            loc.T("app.name"),
		ShortName:       loc.T("app.name"),
		Description:     loc.T("app.tagline"),
		Lang:            loc.Lang().String(),
		Dir:             "ltr",
		StartURL:        "/",
		Scope:           "/",
		Display:         "standalone",
		ThemeColor:      themeColor,
		BackgroundColor: backgroundColor,
		Icons: []manifestIcon{
			icon("192", ""), icon("512", ""), icon("192", "maskable"), icon("512", "maskable"),
			{Src: "/static/opengomenu-logo.svg", Sizes: "any", Type: "image/svg+xml"},
		},
		Shortcuts: []manifestShortcut{
			{Name: loc.T("nav.week"), URL: "/", Icons: []manifestIcon{icon("192", "")}},
			{Name: loc.T("nav.shopping"), URL: "/shopping", Icons: []manifestIcon{icon("192", "")}},
		},
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	// The language comes from the cookie or Accept-Language.
	w.Header().Set("Vary", "Accept-Language, Cookie")
	if err := json.NewEncoder(w).Encode(m); err != nil {
		a.Logger.Error("manifest", "err", err)
	}
}
