package web

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.icco.me/goals/lib/checkin"
	"go.icco.me/goals/lib/remind"
	"go.icco.me/goals/lib/store"
	"go.icco.me/gutil/logging"
	"go.uber.org/zap"
)

type twiml struct {
	XMLName xml.Name `xml:"Response"`
	Message string   `xml:"Message,omitempty"`
}

func writeTwiML(w http.ResponseWriter, r *http.Request, msg string) {
	out, err := xml.Marshal(twiml{Message: msg})
	if err != nil {
		logging.FromContext(r.Context()).Errorw("marshal twiml", zap.Error(err))
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	_, _ = w.Write(append([]byte(xml.Header), out...))
}

// webhook handles replies to the Twilio number.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	params := make(map[string]string, len(r.PostForm))
	for k := range r.PostForm {
		params[k] = r.PostForm.Get(k)
	}
	if !s.SMS.ValidRequest(s.BaseURL+"/sms", params, r.Header.Get("X-Twilio-Signature")) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}
	msg, err := s.handleReply(r, params["From"], params["Body"], params["OptOutType"])
	if err != nil {
		logging.FromContext(r.Context()).Errorw("handle sms", zap.Error(err))
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeTwiML(w, r, msg)
}

func (s *Server) handleReply(r *http.Request, from, body, optOut string) (string, error) {
	ctx := r.Context()
	switch strings.ToUpper(optOut) {
	case "STOP", "START":
		err := s.Store.SetOptedOut(ctx, from, strings.EqualFold(optOut, "STOP"))
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", err
	case "HELP":
		return "", nil
	}

	u, err := s.Store.UserByPhone(ctx, from)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	rem, err := s.Store.LatestReminder(ctx, u.ID)
	if errors.Is(err, store.ErrNotFound) {
		return "No check-in to answer yet.", nil
	}
	if err != nil {
		return "", err
	}
	goals, err := s.Store.GoalsByID(ctx, u.ID, rem.GoalIDs)
	if err != nil {
		return "", err
	}
	done, err := checkin.ParseReply(body, len(goals))
	if errors.Is(err, checkin.ErrUnparsed) {
		return remind.ReplyHint, nil
	}
	if err != nil {
		return "", err
	}
	count := 0
	for i, g := range goals {
		if err := s.Store.SetCheckIn(ctx, u.ID, g.ID, rem.Date, done[i], store.SourceSMS); err != nil {
			return "", err
		}
		if done[i] {
			count++
		}
	}
	history, err := s.Store.CheckIns(ctx, u.ID, "0001-01-01", rem.Date)
	if err != nil {
		return "", err
	}
	streaks := make([]string, len(goals))
	for i, g := range goals {
		cur, _ := checkin.Streaks(history[g.ID], rem.Date)
		streaks[i] = fmt.Sprintf("%s %d", g.Text, cur)
	}
	label := rem.Date
	if d, err := time.Parse(store.DateLayout, rem.Date); err == nil {
		label = d.Format("Jan 2")
	}
	return fmt.Sprintf("Saved: %d of %d done for %s.\nStreaks: %s", count, len(goals), label, strings.Join(streaks, ", ")), nil
}
