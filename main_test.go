package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.icco.me/goals/lib/dbtest"
	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
	"go.icco.me/goals/lib/web"
	"go.uber.org/zap"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	router(zap.NewNop(), http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
}

func TestRouterWiring(t *testing.T) {
	st, err := store.New(t.Context(), dbtest.New(t))
	if err != nil {
		t.Fatal(err)
	}
	app := &web.Server{Store: st, SMS: &sms.Fake{}, BaseURL: "https://goals.example.com", Now: time.Now}
	h := router(zap.NewNop(), app.Routes())
	for path, want := range map[string]int{"/healthz": http.StatusOK, "/": http.StatusOK, "/app": http.StatusSeeOther} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestOpenDBRequiresURL(t *testing.T) {
	if _, err := openDB(""); err == nil {
		t.Fatal("openDB with empty DSN: want error")
	}
}

func TestGetenv(t *testing.T) {
	if got := getenv("NON_EXISTENT_VAR_12345", "default"); got != "default" {
		t.Errorf("getenv = %q, want %q", got, "default")
	}
	t.Setenv("EXISTENT_VAR_12345", "custom")
	if got := getenv("EXISTENT_VAR_12345", "default"); got != "custom" {
		t.Errorf("getenv = %q, want %q", got, "custom")
	}
}
