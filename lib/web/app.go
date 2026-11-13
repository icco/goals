package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.icco.me/goals/lib/checkin"
	"go.icco.me/goals/lib/store"
)

const (
	gridDays = 30
	editDays = 7
)

// zones are offered in settings; any IANA name is accepted.
var zones = []string{
	"Pacific/Honolulu", "America/Anchorage", "America/Los_Angeles", "America/Phoenix", "America/Denver",
	"America/Chicago", "America/New_York", "America/Puerto_Rico", "UTC", "Europe/London", "Europe/Paris",
	"Europe/Berlin", "Asia/Kolkata", "Asia/Singapore", "Asia/Tokyo", "Australia/Sydney", "Pacific/Auckland",
}

type day struct {
	Date     string
	Label    string
	State    string
	Editable bool
}

type goalRow struct {
	Goal    store.Goal
	Today   string
	Current int
	Best    int
	Cells   []day
}

func state(done map[string]bool, date string) string {
	v, ok := done[date]
	switch {
	case !ok:
		return ""
	case v:
		return "done"
	default:
		return "missed"
	}
}

func (s *Server) today(u *store.User) (time.Time, error) {
	d, err := store.LocalDate(s.Now(), u.Timezone)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(store.DateLayout, d)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	today, err := s.today(u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	goals, err := s.Store.ActiveGoals(r.Context(), u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	todayStr := today.Format(store.DateLayout)
	history, err := s.Store.CheckIns(r.Context(), u.ID, "0001-01-01", todayStr)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := page{Title: "Today", Today: todayStr, TodayLong: today.Format("Monday, January 2")}
	for i := gridDays - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		p.Days = append(p.Days, day{Date: d.Format(store.DateLayout), Label: d.Format("Jan 2"), Editable: i < editDays})
	}
	for _, g := range goals {
		done := history[g.ID]
		row := goalRow{Goal: g, Today: state(done, todayStr)}
		row.Current, row.Best = checkin.Streaks(done, todayStr)
		for _, d := range p.Days {
			d.State = state(done, d.Date)
			row.Cells = append(row.Cells, d)
		}
		p.Rows = append(p.Rows, row)
	}
	s.render(w, r, http.StatusOK, "app", p)
}

func (s *Server) checkIn(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	goalID, err := parseID(r.PostFormValue("goal"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	today, err := s.today(u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	date, err := time.Parse(store.DateLayout, r.PostFormValue("date"))
	if age := int(today.Sub(date).Hours() / 24); err != nil || age < 0 || age >= editDays {
		s.render(w, r, http.StatusBadRequest, "error", page{Title: "Error", Error: fmt.Sprintf("You can only change the last %d days.", editDays)})
		return
	}
	err = s.Store.SetCheckIn(r.Context(), u.ID, goalID, date.Format(store.DateLayout), r.PostFormValue("done") == "true", store.SourceWeb)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app", http.StatusSeeOther)
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, "error", page{Title: "Not found", Error: "That goal does not exist."})
}

func (s *Server) renderGoals(w http.ResponseWriter, r *http.Request, status int, msg string) {
	goals, err := s.Store.ActiveGoals(r.Context(), currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, status, "goals", page{Title: "Your goals", Goals: goals, MaxGoals: store.MaxGoals, Error: msg})
}

func (s *Server) goalsPage(w http.ResponseWriter, r *http.Request) {
	s.renderGoals(w, r, http.StatusOK, "")
}

// goalResult turns a store error from a goal edit into a response.
func (s *Server) goalResult(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case err == nil:
		http.Redirect(w, r, "/app/goals", http.StatusSeeOther)
	case errors.Is(err, store.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, store.ErrInvalidGoal), errors.Is(err, store.ErrTooManyGoals):
		s.renderGoals(w, r, http.StatusBadRequest, err.Error())
	default:
		s.fail(w, r, err)
	}
}

func parseID(s string) (uint, error) {
	id, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, store.ErrNotFound
	}
	return uint(id), nil
}

func goalID(r *http.Request) (uint, error) {
	return parseID(chi.URLParam(r, "id"))
}

func (s *Server) addGoal(w http.ResponseWriter, r *http.Request) {
	_, err := s.Store.AddGoal(r.Context(), currentUser(r).ID, r.PostFormValue("text"))
	s.goalResult(w, r, err)
}

func (s *Server) updateGoal(w http.ResponseWriter, r *http.Request) {
	id, err := goalID(r)
	if err == nil {
		err = s.Store.UpdateGoal(r.Context(), currentUser(r).ID, id, r.PostFormValue("text"))
	}
	s.goalResult(w, r, err)
}

func (s *Server) archiveGoal(w http.ResponseWriter, r *http.Request) {
	id, err := goalID(r)
	if err == nil {
		err = s.Store.ArchiveGoal(r.Context(), currentUser(r).ID, id, s.Now())
	}
	s.goalResult(w, r, err)
}

func (s *Server) moveGoal(w http.ResponseWriter, r *http.Request) {
	delta, err := strconv.Atoi(r.PostFormValue("delta"))
	if err != nil || (delta != 1 && delta != -1) {
		s.renderGoals(w, r, http.StatusBadRequest, "Goals move one place at a time.")
		return
	}
	id, err := goalID(r)
	if err == nil {
		err = s.Store.MoveGoal(r.Context(), currentUser(r).ID, id, delta)
	}
	s.goalResult(w, r, err)
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, status int, msg string) {
	u := currentUser(r)
	p := page{Title: "Settings", Zones: zones, Error: msg, RemindTime: fmt.Sprintf("%02d:%02d", u.RemindMinute/60, u.RemindMinute%60)}
	if !slices.Contains(zones, u.Timezone) {
		p.Zones = append([]string{u.Timezone}, zones...)
	}
	s.render(w, r, status, "settings", p)
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, http.StatusOK, "")
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	at, err := time.Parse("15:04", r.PostFormValue("time"))
	if err != nil {
		s.renderSettings(w, r, http.StatusBadRequest, "Pick a reminder time like 20:00.")
		return
	}
	err = s.Store.UpdateSettings(r.Context(), u.ID, r.PostFormValue("timezone"), at.Hour()*60+at.Minute(), r.PostFormValue("paused") == "on")
	if errors.Is(err, store.ErrInvalidSettings) {
		s.renderSettings(w, r, http.StatusBadRequest, "Pick a timezone from the list.")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if r.PostFormValue("confirm") != "delete" {
		s.renderSettings(w, r, http.StatusBadRequest, `Type "delete" to delete your account.`)
		return
	}
	if err := s.Store.DeleteUser(r.Context(), currentUser(r).ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.setSession(w, "", -1)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
