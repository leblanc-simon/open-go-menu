package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"leblanc.io/open-go-base/appconf"
	"leblanc.io/open-go-base/authx"
	"leblanc.io/open-go-base/csrfx"
	"leblanc.io/open-go-base/i18n"
	"leblanc.io/open-go-base/logx"
	"leblanc.io/open-go-base/ratelimit"

	"leblanc.io/open-go-menu/internal/accounts"
	"leblanc.io/open-go-menu/internal/config"
	"leblanc.io/open-go-menu/internal/families"
	"leblanc.io/open-go-menu/internal/web"
)

// version is injected at build time via -ldflags "-X 'main.version=...'".
var version = "develop"

const appName = "open-go-menu"

//go:embed static
var staticFiles embed.FS

//go:embed templates
var templateFiles embed.FS

//go:embed locales
var localeFiles embed.FS

func main() {
	var cfg config.Config
	appconf.MustLoad(&cfg, appconf.Options{Name: appName, Version: version})
	logger := logx.New(cfg.Log)

	switch cfg.Families.Signup {
	case config.SignupOpen, config.SignupInvite, config.SignupClosed:
	default:
		fmt.Fprintf(os.Stderr, "%s: families.signup %q: open, invite or closed\n", appName, cfg.Families.Signup)
		os.Exit(2)
	}

	args := commandArgs(os.Args[1:])
	var err error
	if len(args) == 0 || args[0] == "serve" {
		err = run(cfg)
	} else {
		err = command(cfg, args, os.Stdout)
	}
	if err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// openData opens the accounts database and the registry of the families,
// under the data directory.
func openData(cfg config.Config) (*accounts.Store, *families.Registry, error) {
	dir := cfg.Families.DataDir
	fams, err := families.New(filepath.Join(dir, "families"))
	if err != nil {
		return nil, nil, err
	}
	accts, err := accounts.Open(context.Background(), filepath.Join(dir, "accounts.db"))
	if err != nil {
		fams.Close()
		return nil, nil, err
	}
	return accts, fams, nil
}

func run(cfg config.Config) error {
	logger := logx.New(cfg.Log)

	accts, fams, err := openData(cfg)
	if err != nil {
		return err
	}
	defer accts.Close()
	defer fams.Close()
	// The session slides at every visit and lasts as long as browsers keep a
	// cookie: a family stays logged in until it logs out (EF-27).
	cfg.Auth.SessionTTL = web.SessionTTL
	auth := authx.New(cfg.Auth, accts.Users, accts.Sessions)

	// Locales are embedded so that the binary is self-contained.
	bundle, err := i18n.NewFS(localeFiles, "locales", cfg.I18n.DefaultLanguage)
	if err != nil {
		return err
	}

	tmplFS, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		return err
	}
	csrf := csrfx.New(csrfx.WithSecure(cfg.Auth.CookieSecure))
	opts := web.Options{Signup: cfg.Families.Signup, InactiveMonths: cfg.Families.InactiveMonths, BcryptCost: cfg.Auth.BcryptCost}
	app, err := web.New(tmplFS, accts, fams, auth, opts, bundle, csrf, logger)
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}

	limiter, err := ratelimit.New(cfg.Web.RateLimit, cfg.Web.TrustedProxies,
		ratelimit.WithLimitHandler(rateLimitHandler(bundle)))
	if err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	// Static assets bypass the limiter: one page load fetches several.
	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
	// The service worker is served from the root, so that its scope is the
	// whole application; it is checked for updates at every visit.
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, staticSub, "sw.js")
	})
	mux.Handle("/", limiter.Middleware(csrf.Middleware(app.Handler())))

	addr := cfg.Web.Host + ":" + strconv.Itoa(cfg.Web.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           web.SecurityHeaders(app.RememberLanguage(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The databases of the families nobody uses are closed after a while.
	go func() {
		tick := time.NewTicker(5 * time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				fams.CloseIdle(15 * time.Minute)
			}
		}
	}()

	errc := make(chan error, 1)
	go func() {
		logger.Info("starting", "app", appName, "version", version, "addr", addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// rateLimitHandler answers 429 with a localized message. The Retry-After and
// X-RateLimit-* headers are already set by the limiter.
func rateLimitHandler(bundle *i18n.Bundle) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, bundle.FromRequest(r).T("error.rate_limit"), http.StatusTooManyRequests)
	})
}
