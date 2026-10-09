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
	"strconv"
	"syscall"
	"time"

	"leblanc.io/open-go-base/appconf"
	"leblanc.io/open-go-base/csrfx"
	"leblanc.io/open-go-base/i18n"
	"leblanc.io/open-go-base/logx"
	"leblanc.io/open-go-base/ratelimit"

	"leblanc.io/open-go-menu/internal/config"
	"leblanc.io/open-go-menu/internal/store"
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
	if err := run(cfg); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config) error {
	logger := logx.New(cfg.Log)

	if cfg.DB.Dialect() != appconf.DialectSQLite {
		return fmt.Errorf("database driver %q: only sqlite is supported", cfg.DB.Driver)
	}
	dsn := cfg.DB.DSN
	if dsn == "" {
		dsn = config.DefaultDSN
	}
	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		return err
	}
	defer st.Close()

	// Locales are embedded so that the binary is self-contained.
	bundle, err := i18n.NewFS(localeFiles, "locales", cfg.I18n.DefaultLanguage)
	if err != nil {
		return err
	}

	tmplFS, err := fs.Sub(templateFiles, "templates")
	if err != nil {
		return err
	}
	csrf := csrfx.New()
	app, err := web.New(tmplFS, st, bundle, csrf, logger)
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}

	limiter, err := ratelimit.New(cfg.Web.RateLimit, cfg.Web.TrustedProxies,
		ratelimit.WithLimitHandler(rateLimitHandler(bundle)))
	if err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	appMux := http.NewServeMux()
	app.Routes(appMux)

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
	mux.Handle("/", limiter.Middleware(csrf.Middleware(appMux)))

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
