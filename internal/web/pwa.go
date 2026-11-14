package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
)

// HashFS returns a short digest of every file of fsys, as the version of the
// static files: it changes whenever one of them does.
func HashFS(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := fsys.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		io.WriteString(h, path+"\x00")
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// StaticHandler serves the static files; a versioned URL (?v=) never
// changes, so it is cached for a year.
func StaticHandler(fsys fs.FS) http.Handler {
	files := http.StripPrefix("/static/", http.FileServer(http.FS(fsys)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

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
