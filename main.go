// Package main serves goals: a website and a daily text that track whether
// you did what you meant to do each day.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"go.icco.me/goals/lib/remind"
	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
	"go.icco.me/goals/lib/web"
	"go.icco.me/gutil/logging"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const service = "goals"

func main() {
	log := logging.Must(logging.NewLogger(service))
	err := run(log)
	logging.Sync(log)
	if err != nil {
		log.Fatalw("goals", zap.Error(err))
	}
}

func run(log *zap.SugaredLogger) error {
	ctx, stop := signal.NotifyContext(logging.NewContext(context.Background(), log), os.Interrupt, syscall.SIGTERM)
	defer stop()

	baseURL := strings.TrimSuffix(os.Getenv("BASE_URL"), "/")
	if baseURL == "" {
		return errors.New("BASE_URL is required")
	}
	db, err := openDB(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	st, err := store.New(ctx, db)
	if err != nil {
		return err
	}

	cfg := sms.ConfigFromEnv(os.Getenv)
	client := sms.New(cfg)
	if cfg.Enabled() {
		go (&remind.Scheduler{Store: st, Sender: client, Now: time.Now}).Run(ctx, time.Minute)
	} else {
		log.Warn("TWILIO_* is not fully set: texts and logins are disabled")
	}

	app := &web.Server{Store: st, SMS: client, BaseURL: baseURL, Now: time.Now}
	srv := &http.Server{
		Addr:              ":" + getenv("PORT", "8080"),
		Handler:           router(log.Desugar(), app.Routes()),
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
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// openDB connects to Postgres. Queries log nothing themselves; callers log the
// errors they return.
func openDB(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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
