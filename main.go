// Package main serves goals: a web app and daily SMS reminder that track
// whether you did what you meant to do each day.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"go.icco.me/gutil/logging"
	"go.uber.org/zap"
)

const service = "goals"

func main() {
	log := logging.Must(logging.NewLogger(service))
	defer logging.Sync(log)
	ctx, stop := signal.NotifyContext(logging.NewContext(context.Background(), log), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              ":" + getenv("PORT", "8080"),
		Handler:           router(log.Desugar(), http.NotFoundHandler()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warnw("shutdown", zap.Error(err))
		}
	}()
	log.Infow("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalw("serve", zap.Error(err))
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func router(log *zap.Logger, app http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(logging.Middleware(log))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	r.Mount("/", app)
	return r
}
