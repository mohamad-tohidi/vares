package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"vares/echo/internal/api"
	"vares/echo/internal/config"
	"vares/echo/internal/proxy"
	"vares/echo/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid config", "err", err)
		os.Exit(1)
	}
	slog.Info("starting echo", "listen", cfg.Listen, "upstream", cfg.UpstreamURL, "data_dir", cfg.DataDir)

	rec, err := store.Open(cfg.DataDir, store.Options{
		SegmentBytes: cfg.SegmentBytes,
		Retention:    cfg.Retention,
		FlushEvery:   cfg.FlushEvery,
		Logger:       logger,
	})
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}

	proxyHandler, err := proxy.New(proxy.Options{
		Upstream: cfg.UpstreamURL,
		MaxBody:  cfg.MaxBody,
		Recorder: rec,
		Logger:   logger,
	})
	if err != nil {
		slog.Error("build proxy", "err", err)
		os.Exit(1)
	}

	apiHandler, err := api.New(api.Options{
		Store:  rec,
		Token:  cfg.Token,
		Logger: logger,
	})
	if err != nil {
		slog.Error("build api", "err", err)
		os.Exit(1)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if api.Match(r) {
			apiHandler.ServeHTTP(w, r)
			return
		}
		proxyHandler.ServeHTTP(w, r)
	})

	server := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	slog.Info("echo up", "listen", cfg.Listen)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Warn("shutdown incomplete", "err", err)
	}
	if err := rec.Close(); err != nil {
		slog.Error("close store", "err", err)
		os.Exit(1)
	}
	slog.Info("echo stopped")
}
