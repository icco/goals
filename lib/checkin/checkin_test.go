package checkin

import (
	"errors"
	"slices"
	"testing"
)

func TestParseReply(t *testing.T) {
	all := []bool{true, true, true}
	none := []bool{false, false, false}
	oneThree := []bool{true, false, true}
	for in, want := range map[string][]bool{
		"Y": all, "yes!": all, "all": all, " Yep ": all,
		"n": none, "No.": none, "none": none,
		"1 3": oneThree, "1,3": oneThree, "1 and 3": oneThree, "3, 1, 1": oneThree,
	} {
		got, err := ParseReply(in, 3)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParseReply(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"4", "0", "", "maybe", "yes 1", "y n", "1 x", "!!!"} {
		if _, err := ParseReply(in, 3); !errors.Is(err, ErrUnparsed) {
			t.Errorf("ParseReply(%q) err = %v, want ErrUnparsed", in, err)
		}
	}
}

func TestStreaks(t *testing.T) {
	for _, tc := range []struct {
		name          string
		done          map[string]bool
		today         string
		current, best int
	}{
		{"today and two before", map[string]bool{"2026-11-10": true, "2026-11-11": true, "2026-11-12": true}, "2026-11-12", 3, 3},
		{"today unanswered", map[string]bool{"2026-11-10": true, "2026-11-11": true}, "2026-11-12", 2, 2},
		{"today missed", map[string]bool{"2026-11-11": true, "2026-11-12": false}, "2026-11-12", 0, 1},
		{"gap", map[string]bool{"2026-11-01": true, "2026-11-02": true, "2026-11-03": true, "2026-11-05": true, "2026-11-06": true}, "2026-11-06", 2, 3},
		{"month boundary", map[string]bool{"2026-10-31": true, "2026-11-01": true}, "2026-11-01", 2, 2},
		{"stale", map[string]bool{"2026-11-01": true}, "2026-11-12", 0, 1},
		{"empty", nil, "2026-11-12", 0, 0},
	} {
		cur, best := Streaks(tc.done, tc.today)
		if cur != tc.current || best != tc.best {
			t.Errorf("%s: Streaks = %d, %d; want %d, %d", tc.name, cur, best, tc.current, tc.best)
		}
	}
}
