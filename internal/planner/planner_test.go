package planner

import (
	"testing"
	"time"
)

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEarliestEligibleDate(t *testing.T) {
	today := date("2026-09-22")

	tests := []struct {
		name    string
		lastMow *time.Time
		minDays int
		want    time.Time
	}{
		{
			name:    "no history means today is eligible",
			lastMow: nil,
			minDays: 7,
			want:    date("2026-09-22"),
		},
		{
			name:    "recent mow pushes eligibility into the future",
			lastMow: ptr(date("2026-09-20")),
			minDays: 7,
			want:    date("2026-09-27"),
		},
		{
			name:    "old mow clamps to today, never returns a past date",
			lastMow: ptr(date("2026-06-01")),
			minDays: 7,
			want:    date("2026-09-22"),
		},
		{
			name:    "mow exactly minDays ago is eligible today",
			lastMow: ptr(date("2026-09-15")),
			minDays: 7,
			want:    date("2026-09-22"),
		},
		{
			name:    "zero-day interval with a mow today is still eligible today",
			lastMow: ptr(date("2026-09-22")),
			minDays: 0,
			want:    date("2026-09-22"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EarliestEligibleDate(today, tc.lastMow, tc.minDays)
			if !got.Equal(tc.want) {
				t.Errorf("EarliestEligibleDate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChooseMowDate(t *testing.T) {
	forecast := []ForecastDay{
		{Date: date("2026-09-22"), RainProbabilityPercent: 10}, // eligible? depends on test
		{Date: date("2026-09-23"), RainProbabilityPercent: 80},
		{Date: date("2026-09-24"), RainProbabilityPercent: 90},
		{Date: date("2026-09-25"), RainProbabilityPercent: 20},
		{Date: date("2026-09-26"), RainProbabilityPercent: 5},
	}

	t.Run("skips days before the eligible window even if drier", func(t *testing.T) {
		// 2026-09-22 is the driest day overall, but it's before the
		// eligible window, so it must never be chosen.
		got, ok := ChooseMowDate(forecast, date("2026-09-23"), 30)
		if !ok {
			t.Fatalf("expected a day to be chosen")
		}
		if !got.Date.Equal(date("2026-09-25")) {
			t.Errorf("got date %v, want 2026-09-25", got.Date)
		}
	})

	t.Run("picks the first day at/under threshold, not the driest", func(t *testing.T) {
		// 2026-09-26 is drier than 2026-09-25, but 09-25 should win
		// because it's the *first* day under threshold.
		got, ok := ChooseMowDate(forecast, date("2026-09-22"), 25)
		if !ok {
			t.Fatalf("expected a day to be chosen")
		}
		if !got.Date.Equal(date("2026-09-22")) {
			t.Errorf("got date %v, want 2026-09-22", got.Date)
		}
	})

	t.Run("threshold is inclusive", func(t *testing.T) {
		got, ok := ChooseMowDate(forecast, date("2026-09-25"), 20)
		if !ok {
			t.Fatalf("expected a day to be chosen")
		}
		if !got.Date.Equal(date("2026-09-25")) {
			t.Errorf("got date %v, want 2026-09-25", got.Date)
		}
	})

	t.Run("returns false when nothing qualifies", func(t *testing.T) {
		_, ok := ChooseMowDate(forecast, date("2026-09-23"), 1)
		if ok {
			t.Fatalf("expected no day to qualify")
		}
	})

	t.Run("works with unsorted input", func(t *testing.T) {
		unsorted := []ForecastDay{forecast[4], forecast[0], forecast[3], forecast[1], forecast[2]}
		got, ok := ChooseMowDate(unsorted, date("2026-09-22"), 25)
		if !ok {
			t.Fatalf("expected a day to be chosen")
		}
		if !got.Date.Equal(date("2026-09-22")) {
			t.Errorf("got date %v, want 2026-09-22", got.Date)
		}
	})

	t.Run("empty forecast returns false", func(t *testing.T) {
		_, ok := ChooseMowDate(nil, date("2026-09-22"), 100)
		if ok {
			t.Fatalf("expected no day to qualify for an empty forecast")
		}
	})
}

func ptr(t time.Time) *time.Time { return &t }
