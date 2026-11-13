// Package web serves the goals website and the Twilio reply webhook.
package web

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
	"go.icco.me/gutil/logging"
	"go.uber.org/zap"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const cookieName = "goals_session"

// Server holds what the handlers need.
type Server struct {
	Store   *store.Store
	SMS     sms.Client
	BaseURL string
	Now     func() time.Time
	pages   map[string]*template.Template
}

type userKey struct{}

// page is the data every template receives; each page uses the fields it needs.
type page struct {
	Title   string
	User    *store.User
	Error   string
	BaseURL string

	Phone   string
	Consent bool

	Today     string
	TodayLong string
	Rows      []goalRow
	Days      []day

	Goals    []store.Goal
	MaxGoals int

	Zones      []string
	RemindTime string
}

var funcs = template.FuncMap{
	"inc": func(i int) int { return i + 1 },
}

func (s *Server) parse() error {
	s.pages = map[string]*template.Template{}
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == "templates/layout.html" {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", name)
		if err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
		s.pages[name[len("templates/"):len(name)-len(".html")]] = t
	}
	return nil
}

// Routes returns the site's handler. It panics if the embedded templates do
// not parse, which a test catches.
func (s *Server) Routes() http.Handler {
	if err := s.parse(); err != nil {
		panic(err)
	}
	r := chi.NewRouter()
	r.Use(securityHeaders, s.loadUser)
	static, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServerFS(static)))

	r.Get("/", s.index)
	r.Get("/terms", s.static("terms", "Terms"))
	r.Get("/privacy", s.static("privacy", "Privacy"))
	r.Post("/signup", s.signup)
	r.Get("/login", s.static("login", "Log in"))
	r.Post("/login", s.loginPost)
	r.Get("/verify", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/login", http.StatusSeeOther) })
	r.Post("/verify", s.verifyPost)
	r.Post("/logout", s.logout)
	r.Post("/sms", s.webhook)

	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/app", s.dashboard)
		r.Post("/app/checkin", s.checkIn)
		r.Get("/app/goals", s.goalsPage)
		r.Post("/app/goals", s.addGoal)
		r.Post("/app/goals/{id}", s.updateGoal)
		r.Post("/app/goals/{id}/archive", s.archiveGoal)
		r.Post("/app/goals/{id}/move", s.moveGoal)
		r.Get("/settings", s.settingsPage)
		r.Post("/settings", s.saveSettings)
		r.Post("/settings/delete", s.deleteAccount)
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusNotFound, "error", page{Title: "Not found", Error: "That page does not exist."})
	})
	return http.NewCrossOriginProtection().Handler(r)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.Store.SessionUser(r.Context(), c.Value, s.Now())
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			s.fail(w, r, err)
			return
		default:
			r = r.WithContext(context.WithValue(r.Context(), userKey{}, u))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey{}).(*store.User)
	return u
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	p.User = currentUser(r)
	p.BaseURL = s.BaseURL
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[name].ExecuteTemplate(w, "layout.html", p); err != nil {
		logging.FromContext(r.Context()).Errorw("render", "page", name, zap.Error(err))
	}
}

func (s *Server) static(name, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusOK, name, page{Title: title})
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	logging.FromContext(r.Context()).Errorw("request failed", "path", r.URL.Path, zap.Error(err))
	s.render(w, r, http.StatusInternalServerError, "error", page{Title: "Error", Error: "Something went wrong. Please try again."})
}

func (s *Server) setSession(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
