// Package checkin parses text replies and computes streaks.
package checkin

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ErrUnparsed means a reply was not yes, no, or a list of goal numbers.
var ErrUnparsed = errors.New("checkin: reply not understood")

var (
	yes = map[string]bool{"y": true, "yes": true, "yep": true, "yeah": true, "all": true, "done": true}
	no  = map[string]bool{"n": true, "no": true, "nope": true, "none": true}
)

const dateLayout = "2006-01-02"

// ParseReply reads a reply to a reminder listing n goals and returns whether
// each was done. Yes marks all, no marks none, and 1-based numbers mark those
// goals and leave the rest not done.
func ParseReply(body string, n int) ([]bool, error) {
	tokens := strings.FieldsFunc(strings.ToLower(body), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	tokens = removeAnd(tokens)
	if len(tokens) == 0 {
		return nil, ErrUnparsed
	}
	out := make([]bool, n)
	if len(tokens) == 1 && (yes[tokens[0]] || no[tokens[0]]) {
		for i := range out {
			out[i] = yes[tokens[0]]
		}
		return out, nil
	}
	for _, tok := range tokens {
		i, err := strconv.Atoi(tok)
		if err != nil || i < 1 || i > n {
			return nil, ErrUnparsed
		}
		out[i-1] = true
	}
	return out, nil
}

func removeAnd(tokens []string) []string {
	out := tokens[:0]
	for _, t := range tokens {
		if t != "and" {
			out = append(out, t)
		}
	}
	return out
}

// Streaks returns the current run of done days ending today (or yesterday,
// when today is not answered yet) and the longest run ever. done maps
// YYYY-MM-DD dates to whether the goal was done.
func Streaks(done map[string]bool, today string) (current, best int) {
	var days []time.Time
	for d, ok := range done {
		if t, err := time.Parse(dateLayout, d); ok && err == nil {
			days = append(days, t)
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	run := 0
	for i, d := range days {
		if i > 0 && d.Equal(days[i-1].AddDate(0, 0, 1)) {
			run++
		} else {
			run = 1
		}
		best = max(best, run)
	}

	day, err := time.Parse(dateLayout, today)
	if err != nil {
		return 0, best
	}
	if _, answered := done[today]; !answered {
		day = day.AddDate(0, 0, -1)
	}
	for done[day.Format(dateLayout)] {
		current++
		day = day.AddDate(0, 0, -1)
	}
	return current, best
}
