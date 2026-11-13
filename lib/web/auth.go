package web

import (
	"errors"
	"net/http"

	"go.icco.me/goals/lib/sms"
	"go.icco.me/goals/lib/store"
	"go.icco.me/gutil/logging"
	"go.uber.org/zap"
)

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "index", page{Title: "Text yourself your goals"})
}

// sendCode texts a login code and shows the code form, or re-renders page
// name with an error when the text could not be sent. The phone stays out of
// URLs so request logs never record it.
func (s *Server) sendCode(w http.ResponseWriter, r *http.Request, name string, p page) {
	err := s.SMS.StartVerify(r.Context(), p.Phone)
	switch {
	case errors.Is(err, sms.ErrDisabled):
		p.Error = "Texts are not set up yet. Please check back soon."
		s.render(w, r, http.StatusServiceUnavailable, name, p)
		return
	case err != nil:
		logging.FromContext(r.Context()).Warnw("start verification", zap.Error(err))
		p.Error = "We couldn't text a code to that number. Please try again."
		s.render(w, r, http.StatusBadGateway, name, p)
		return
	}
	p.Title = "Enter your code"
	s.render(w, r, http.StatusOK, "verify", p)
}

func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	p := page{Title: "Text yourself your goals", Phone: r.PostFormValue("phone"), Consent: r.PostFormValue("consent") == "on"}
	phone, err := store.NormalizePhone(p.Phone)
	if err != nil {
		p.Error = err.Error()
		s.render(w, r, http.StatusBadRequest, "index", p)
		return
	}
	p.Phone = phone
	if !p.Consent {
		p.Error = "Please agree to receive texts to sign up."
		s.render(w, r, http.StatusBadRequest, "index", p)
		return
	}
	s.sendCode(w, r, "index", p)
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	p := page{Title: "Log in", Phone: r.PostFormValue("phone")}
	phone, err := store.NormalizePhone(p.Phone)
	if err != nil {
		p.Error = err.Error()
		s.render(w, r, http.StatusBadRequest, "login", p)
		return
	}
	p.Phone = phone
	_, err = s.Store.UserByPhone(r.Context(), phone)
	if errors.Is(err, store.ErrNotFound) {
		p.Error = "We don't know that number. Please sign up first."
		s.render(w, r, http.StatusBadRequest, "login", p)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.sendCode(w, r, "login", p)
}

func (s *Server) verifyPost(w http.ResponseWriter, r *http.Request) {
	p := page{Title: "Enter your code", Phone: r.PostFormValue("phone"), Consent: r.PostFormValue("consent") == "on"}
	ok, err := s.SMS.CheckVerify(r.Context(), p.Phone, r.PostFormValue("code"))
	switch {
	case errors.Is(err, sms.ErrDisabled):
		p.Error = "Texts are not set up yet. Please check back soon."
		s.render(w, r, http.StatusServiceUnavailable, "verify", p)
		return
	case err != nil:
		logging.FromContext(r.Context()).Warnw("check verification", zap.Error(err))
		p.Error = "We couldn't check that code. Please request a new one."
		s.render(w, r, http.StatusBadGateway, "verify", p)
		return
	case !ok:
		p.Error = "That code didn't match. Check the text and try again."
		s.render(w, r, http.StatusBadRequest, "verify", p)
		return
	}
	u, err := s.Store.EnsureUser(r.Context(), p.Phone, p.Consent, s.Now())
	if errors.Is(err, store.ErrNoConsent) {
		p.Error = "We don't know that number. Please sign up first."
		s.render(w, r, http.StatusBadRequest, "verify", p)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	token, err := s.Store.CreateSession(r.Context(), u.ID, s.Now())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.setSession(w, token, int(store.SessionTTL.Seconds()))
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := s.Store.DeleteSession(r.Context(), c.Value); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.setSession(w, "", -1)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
