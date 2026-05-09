// Command sms-mock-server is the entrypoint for the Twilio-compatible mock
// SMS/voice API server. It loads config, opens the SQLite store, builds the
// provider/template/dispatcher stack, registers HTTP handlers, and runs an
// http.Server with graceful shutdown on SIGINT/SIGTERM.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/notfoundsam/sms-mock-server/app/callback"
	"github.com/notfoundsam/sms-mock-server/app/clock"
	"github.com/notfoundsam/sms-mock-server/app/config"
	"github.com/notfoundsam/sms-mock-server/app/httpapi"
	"github.com/notfoundsam/sms-mock-server/app/provider"
	"github.com/notfoundsam/sms-mock-server/app/provider/twilio"
	"github.com/notfoundsam/sms-mock-server/app/storage"
	tmpl "github.com/notfoundsam/sms-mock-server/app/template"
	"github.com/notfoundsam/sms-mock-server/app/ui"
)

func main() {
	logger := newLogger()

	if err := run(logger); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// stack bundles the constructed dependencies so callers can drive shutdown.
type stack struct {
	store      storage.Store
	dispatcher *callback.Dispatcher
	handler    http.Handler
}

// buildStack constructs the full server stack from a loaded config. Used
// by run() (for the production entrypoint) and by main_test.go (for the
// smoke test, which substitutes its own config + an in-memory DB path).
func buildStack(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*stack, error) {
	store, err := storage.New(ctx, cfg.Database.Path)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}

	providers := map[string]provider.Provider{
		"twilio": twilio.New(&cfg.Twilio),
	}
	prov, ok := providers[cfg.Provider]
	if !ok {
		_ = store.Close()
		return nil, fmt.Errorf("unsupported provider: %q", cfg.Provider)
	}

	tmplFS, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("template sub-fs: %w", err)
	}
	tz, err := time.LoadLocation(cfg.Server.Timezone)
	if err != nil {
		logger.Warn("invalid timezone, falling back to UTC", "tz", cfg.Server.Timezone)
		tz = time.UTC
	}
	manifest := loadAssetManifest(staticFS, logger)
	engine, err := tmpl.New(tmplFS, tmpl.Options{
		Provider:      cfg.Provider,
		AssetManifest: manifest,
		Timezone:      tz,
	})
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("template engine: %w", err)
	}

	// Bounded HTTP client for outbound callbacks. Without a timeout, a slow
	// or hanging receiver pins a worker goroutine indefinitely; with only
	// `Workers` slots available, a few stuck callbacks would deadlock the
	// dispatcher.
	callbackHTTP := &http.Client{Timeout: 30 * time.Second}
	dispatcher := callback.New(logger, store, clock.Real{}, callbackHTTP.Do, callback.Config{
		Delay:         time.Duration(cfg.Twilio.Callbacks.DelaySeconds) * time.Second,
		RetryAttempts: cfg.Twilio.Callbacks.RetryAttempts,
		RetryDelay:    time.Duration(cfg.Twilio.Callbacks.RetryDelaySeconds) * time.Second,
		Workers:       4,
	}, cfg.Twilio.AccountSid)

	apiServer := httpapi.NewServer(httpapi.Deps{
		Logger:     logger,
		Provider:   prov,
		Store:      store,
		Templates:  engine,
		Dispatcher: dispatcher,
		AccountSid: cfg.Twilio.AccountSid,
	})
	uiHandler := ui.New(logger, store, engine, prov.Name(), cfg.Server.Timezone)

	mux := http.NewServeMux()
	apiServer.RegisterRoutes(mux)
	uiHandler.Register(mux)

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("static sub-fs: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	handler := httpapi.WrapWithMiddleware(mux, logger)

	return &stack{store: store, dispatcher: dispatcher, handler: handler}, nil
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Info("config loaded", "provider", cfg.Provider,
		"host", cfg.Server.Host, "port", cfg.Server.Port,
		"timezone", cfg.Server.Timezone, "db_path", cfg.Database.Path)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	st, err := buildStack(rootCtx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.store.Close(); err != nil {
			logger.Warn("store close failed", "error", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           st.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 7. Run + graceful shutdown
	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case err := <-serverErrCh:
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	}

	// Shutdown sequence: HTTP server (stop accepting new connections, drain
	// in-flight requests) → dispatcher (drain workers, no new HTTP callbacks)
	// → store close (deferred above). 30s deadline for the whole sequence.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown failed", "error", err)
	}
	if err := st.dispatcher.Close(shutdownCtx); err != nil {
		logger.Warn("dispatcher close failed", "error", err)
	}
	logger.Info("shutdown complete")
	return nil
}

// loadAssetManifest reads static/manifest.json (cache-bust map) from the
// embedded FS. Returns an empty map and logs a warning if the file is
// missing or malformed — assetUrl falls back to the original path in that case.
func loadAssetManifest(fsys fs.FS, logger *slog.Logger) map[string]string {
	data, err := fs.ReadFile(fsys, "static/manifest.json")
	if err != nil {
		logger.Warn("asset manifest not found, using original URLs", "error", err)
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		logger.Warn("asset manifest malformed, using original URLs", "error", err)
		return map[string]string{}
	}
	logger.Info("asset manifest loaded", "entries", len(m))
	return m
}
