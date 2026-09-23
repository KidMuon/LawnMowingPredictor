package planner

import (
	"strings"
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

func ptr(t time.Time) *time.Time { return &t }

// week returns an 8-day forecast starting on start, with the given chances
// of rain in order.
func week(start string, rain ...float64) []ForecastDay {
	days := make([]ForecastDay, len(rain))
	for i, r := range rain {
		days[i] = ForecastDay{Date: date(start).AddDate(0, 0, i), RainProbabilityPercent: r}
	}
	return days
}

func baseInput() Input {
	return Input{
		Today:                date("2026-09-22"),
		LastMow:              ptr(date("2026-09-17")),
		MinIntervalDays:      5,
		IdealIntervalDays:    7,
		RainThresholdPercent: 30,
	}
}

func TestPlan_CreatesOnTargetDateWhenItIsAGoodMowingDay(t *testing.T) {
	in := baseInput()
	// Target Date = 09-17 + 7 = 09-24.
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)

	got := Plan(in)

	if got.Action != ActionCreate {
		t.Fatalf("Action = %v, want create", got.Action)
	}
	if !got.Date.Equal(date("2026-09-24")) {
		t.Errorf("Date = %s, want 2026-09-24", got.Date.Format("2006-01-02"))
	}
	if want := "Planned automatically: 0% chance of rain forecast for 2026-09-24."; !strings.Contains(got.Description, want) {
		t.Errorf("Description = %q, want it to contain %q", got.Description, want)
	}
}

func TestPlan_DescriptionSaysWhenTheForecastHasNoAnswerYet(t *testing.T) {
	in := baseInput()
	in.IdealIntervalDays = 14 // Target Date 10-01, beyond the forecast
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)

	got := Plan(in)

	if want := "Planned automatically for 2026-10-01; the forecast doesn't show a good day yet."; !strings.Contains(got.Description, want) {
		t.Errorf("Description = %q, want it to contain %q", got.Description, want)
	}
}

// Last Mow 09-17, Minimum 5, Ideal 7: the Minimum Interval ends on 09-22
// (today) and the Target Date is 09-24.
func TestPlan_MowDay(t *testing.T) {
	tests := []struct {
		name string
		// Chance of rain for 09-22 .. 09-29.
		rain []float64
		want string
		// Optional overrides of baseInput.
		lastMow   string
		noLastMow bool
		idealDays int
	}{
		{
			name: "rainy Target Date moves to the day before",
			rain: []float64{0, 0, 90, 0, 0, 0, 0, 0},
			want: "2026-09-23",
		},
		{
			name: "keeps going back as far as the Minimum Interval",
			rain: []float64{0, 90, 90, 0, 0, 0, 0, 0},
			want: "2026-09-22",
		},
		{
			name: "nothing good back to the Minimum Interval, so the first good day after the Target Date",
			rain: []float64{90, 90, 90, 0, 0, 0, 0, 0},
			want: "2026-09-26",
		},
		{
			name: "a day under the threshold straight after a rainy one is not a Good Mowing Day",
			rain: []float64{0, 90, 0, 0, 0, 0, 0, 0},
			want: "2026-09-22",
		},
		{
			name: "no good day anywhere falls back to the Target Date",
			rain: []float64{90, 90, 90, 90, 90, 90, 90, 90},
			want: "2026-09-24",
		},
		{
			// Last Mow 09-10: the Target Date (09-17) has already passed.
			name:    "a passed Target Date means the first good day from today",
			lastMow: "2026-09-10",
			rain:    []float64{90, 0, 0, 0, 0, 0, 0, 0},
			want:    "2026-09-24",
		},
		{
			// Ideal 14: the Target Date (10-01) is past the end of the
			// forecast (09-29), so the dry days in the forecast aren't
			// known to be the nearest good ones.
			name:      "a Target Date beyond the forecast is used as is",
			idealDays: 14,
			rain:      []float64{0, 0, 0, 0, 0, 0, 0, 0},
			want:      "2026-10-01",
		},
		{
			// Last Mow 09-18: the Minimum Interval ends on 09-23 and the
			// Target Date is 09-25. Today (09-22) is good but too soon.
			name:    "never goes back before the Minimum Interval",
			lastMow: "2026-09-18",
			rain:    []float64{0, 90, 90, 90, 0, 0, 0, 0},
			want:    "2026-09-27",
		},
		{
			// Last Mow 09-15, Ideal 9: the Minimum Interval ended on 09-20
			// (already passed) and the Target Date is 09-24.
			name:      "going back stops at today, even when the Minimum Interval has passed",
			lastMow:   "2026-09-15",
			idealDays: 9,
			rain:      []float64{0, 90, 90, 0, 0, 0, 0, 0},
			want:      "2026-09-22",
		},
		{
			// Last Mow 09-10: the Target Date (09-17) has already passed.
			name:    "a passed Target Date with no good day falls back to today",
			lastMow: "2026-09-10",
			rain:    []float64{90, 90, 90, 90, 90, 90, 90, 90},
			want:    "2026-09-22",
		},
		{
			// Last Mow 09-15: the Target Date is today. Yesterday isn't in
			// the forecast.
			name:    "the day before today counts as dry",
			lastMow: "2026-09-15",
			rain:    []float64{0, 90, 90, 90, 90, 90, 90, 90},
			want:    "2026-09-22",
		},
		{
			name:      "no Last Mow means the first good day from today",
			noLastMow: true,
			rain:      []float64{90, 90, 0, 0, 0, 0, 0, 0},
			want:      "2026-09-25",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := baseInput()
			in.Forecast = week("2026-09-22", tc.rain...)
			if tc.lastMow != "" {
				in.LastMow = ptr(date(tc.lastMow))
			}
			if tc.noLastMow {
				in.LastMow = nil
			}
			if tc.idealDays != 0 {
				in.IdealIntervalDays = tc.idealDays
			}

			got := Plan(in)

			if got.Action != ActionCreate {
				t.Fatalf("Action = %v, want create", got.Action)
			}
			if gotDate := got.Date.Format("2006-01-02"); gotDate != tc.want {
				t.Errorf("Date = %s, want %s", gotDate, tc.want)
			}
		})
	}
}

func TestPlan_LeavesItsOwnPlannedMowAloneWhenNothingChanged(t *testing.T) {
	in := baseInput()
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)
	created := Plan(in)

	in.Scheduled = &ScheduledMow{Due: created.Date, Description: created.Description}
	got := Plan(in)

	if got.Action != ActionNone {
		t.Errorf("Action = %v, want none", got.Action)
	}
}

func TestPlan_MovesItsOwnPlannedMowWhenTheForecastChanges(t *testing.T) {
	in := baseInput()
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)
	created := Plan(in) // 09-24

	in.Scheduled = &ScheduledMow{Due: created.Date, Description: created.Description}
	in.Forecast = week("2026-09-22", 0, 0, 90, 0, 0, 0, 0, 0)
	got := Plan(in)

	if got.Action != ActionMove {
		t.Fatalf("Action = %v, want move", got.Action)
	}
	if gotDate := got.Date.Format("2006-01-02"); gotDate != "2026-09-23" {
		t.Errorf("Date = %s, want 2026-09-23", gotDate)
	}

	// The moved task is still the app's to re-plan.
	in.Scheduled = &ScheduledMow{Due: got.Date, Description: got.Description}
	if again := Plan(in); again.Action != ActionNone {
		t.Errorf("after the move, Action = %v, want none", again.Action)
	}
}

func TestPlan_MovesAnOverduePlannedMow(t *testing.T) {
	in := baseInput()
	in.LastMow = ptr(date("2026-09-13")) // Target Date 09-20
	in.Today = date("2026-09-20")
	in.Forecast = week("2026-09-20", 0, 0, 0, 0, 0, 0, 0, 0)
	created := Plan(in)

	// Two days later it rained on the 20th, and the task is still open.
	in.Today = date("2026-09-22")
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)
	in.Scheduled = &ScheduledMow{Due: created.Date, Description: created.Description}
	got := Plan(in)

	if got.Action != ActionMove {
		t.Fatalf("Action = %v, want move", got.Action)
	}
	if gotDate := got.Date.Format("2006-01-02"); gotDate != "2026-09-22" {
		t.Errorf("Date = %s, want 2026-09-22", gotDate)
	}
}

func TestPlan_NeverTouchesAPinnedMow(t *testing.T) {
	in := baseInput()
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)
	planned := Plan(in) // 09-24, marker says 09-24

	tests := []struct {
		name      string
		scheduled ScheduledMow
	}{
		{
			name:      "the owner created it",
			scheduled: ScheduledMow{Due: date("2026-09-27"), Description: "remember the edges"},
		},
		{
			name:      "the owner moved a Planned Mow",
			scheduled: ScheduledMow{Due: date("2026-09-27"), Description: planned.Description},
		},
		{
			name:      "the owner's task is overdue",
			scheduled: ScheduledMow{Due: date("2026-09-20")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in.Scheduled = &tc.scheduled

			got := Plan(in)

			if got.Action != ActionNone {
				t.Errorf("Action = %v, want none", got.Action)
			}
		})
	}
}

func TestPlan_MovingAPlannedMowKeepsTheOwnersNotes(t *testing.T) {
	in := baseInput()
	in.Forecast = week("2026-09-22", 0, 0, 0, 0, 0, 0, 0, 0)
	created := Plan(in) // 09-24

	notes := "Borrow the neighbour's trimmer.\nDo the back verge too."
	in.Scheduled = &ScheduledMow{Due: created.Date, Description: notes + "\n\n" + created.Description}
	in.Forecast = week("2026-09-22", 0, 0, 90, 0, 0, 0, 0, 0)
	got := Plan(in)

	if got.Action != ActionMove {
		t.Fatalf("Action = %v, want move", got.Action)
	}
	want := notes + "\n\n" +
		"Planned automatically: 0% chance of rain forecast for 2026-09-23.\n\n" +
		"lawnmower-planned: 2026-09-23"
	if got.Description != want {
		t.Errorf("Description =\n%s\nwant\n%s", got.Description, want)
	}
}
