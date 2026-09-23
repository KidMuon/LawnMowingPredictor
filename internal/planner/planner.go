// Package planner contains the pure decision logic for scheduling a mow:
// given the Last Mow, the forecast, and the current Scheduled Mow, it picks
// the Mow Day and decides whether to create, move, or leave the task. It
// has no knowledge of HTTP, Todoist, or OpenWeatherMap and is deliberately
// easy to unit test. Terms follow CONTEXT.md.
package planner

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ForecastDay is one day's weather forecast, normalized to a calendar date
// (time-of-day and location are not meaningful and should be zeroed by the
// caller, e.g. via a UTC midnight timestamp).
type ForecastDay struct {
	Date                   time.Time
	RainProbabilityPercent float64
}

// Action is what Plan decides to do about the Scheduled Mow.
type Action int

const (
	// ActionNone leaves Todoist as it is.
	ActionNone Action = iota
	// ActionCreate creates a new Planned Mow.
	ActionCreate
	// ActionMove moves the existing Planned Mow to a new date.
	ActionMove
)

// Input is everything Plan needs to decide on the next mow.
type Input struct {
	Today                time.Time
	LastMow              *time.Time
	Forecast             []ForecastDay
	MinIntervalDays      int
	IdealIntervalDays    int
	RainThresholdPercent float64
	// Scheduled is the current Scheduled Mow, if there is one.
	Scheduled *ScheduledMow
}

// ScheduledMow is an open mowing task, as far as planning cares.
type ScheduledMow struct {
	Due         time.Time
	Description string
}

// Decision is the outcome of Plan.
type Decision struct {
	Action Action
	Date   time.Time
	// Description is the task description to write when creating or
	// moving a Planned Mow.
	Description string
}

// Plan decides whether to create, move, or leave the Scheduled Mow.
func Plan(in Input) Decision {
	day, rain, good := mowDay(in)
	d := Decision{Action: ActionCreate, Date: day, Description: plannedDescription(day, rain, good)}
	if in.Scheduled != nil {
		if !isPlanned(*in.Scheduled) || day.Equal(truncateToDate(in.Scheduled.Due)) {
			return Decision{Action: ActionNone}
		}
		d.Action = ActionMove
		if notes := ownerNotes(in.Scheduled.Description); notes != "" {
			d.Description = notes + "\n\n" + d.Description
		}
	}
	return d
}

// appLine matches the lines the app writes into a Planned Mow's
// description: the summary and the marker.
var appLine = regexp.MustCompile(`^(Planned automatically[ :].*|lawnmower-planned: .*)$`)

// ownerNotes returns description without the app's own lines, so moving
// a Planned Mow keeps anything the owner added.
func ownerNotes(description string) string {
	var kept []string
	for _, line := range strings.Split(description, "\n") {
		if !appLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// isPlanned reports whether s is a Planned Mow: its marker is present and
// still matches its due date. Anything else is a Pinned Mow.
func isPlanned(s ScheduledMow) bool {
	m := plannedMarker.FindStringSubmatch(s.Description)
	return m != nil && m[1] == truncateToDate(s.Due).Format("2006-01-02")
}

// plannedMarker is written into every Planned Mow's description, recording
// the date the app chose (see docs/adr/0001).
var plannedMarker = regexp.MustCompile(`(?m)^lawnmower-planned: (\d{4}-\d{2}-\d{2})$`)

func plannedDescription(day time.Time, rain float64, good bool) string {
	ymd := day.Format("2006-01-02")
	summary := fmt.Sprintf("Planned automatically for %s; the forecast doesn't show a good day yet.", ymd)
	if good {
		summary = fmt.Sprintf("Planned automatically: %.0f%% chance of rain forecast for %s.", rain, ymd)
	}
	return fmt.Sprintf("%s\n\nlawnmower-planned: %s", summary, ymd)
}

// mowDay picks the Mow Day: the Target Date if it's a Good Mowing Day,
// otherwise the nearest good day before it, going back no further than the
// Minimum Interval; otherwise the first good day after it. If none of those
// is a Good Mowing Day it falls back to the Target Date, and good is false.
func mowDay(in Input) (day time.Time, rainPercent float64, good bool) {
	rain := make(map[time.Time]float64, len(in.Forecast))
	for _, d := range in.Forecast {
		rain[truncateToDate(d.Date)] = d.RainProbabilityPercent
	}
	dry := func(d time.Time) bool {
		r, ok := rain[d]
		return ok && r <= in.RainThresholdPercent
	}
	today := truncateToDate(in.Today)
	// A Good Mowing Day needs the day before to be dry too, so the grass
	// has dried out. Yesterday isn't in the forecast, so it counts as dry.
	isGood := func(d time.Time) bool {
		return dry(d) && (d.Equal(today) || dry(d.AddDate(0, 0, -1)))
	}

	// With no Last Mow, mowing can happen as soon as there's a good day.
	target, earliest := today, today
	if in.LastMow != nil {
		lastMow := truncateToDate(*in.LastMow)
		target = lastMow.AddDate(0, 0, in.IdealIntervalDays)
		earliest = lastMow.AddDate(0, 0, in.MinIntervalDays)
	}
	// Never plan a mow in the past.
	target = latest(target, today)
	earliest = latest(earliest, today)
	if _, inForecast := rain[target]; !inForecast {
		// Too far ahead to judge; a later run re-plans once it's in range.
		return target, 0, false
	}
	if isGood(target) {
		return target, rain[target], true
	}
	for d := target.AddDate(0, 0, -1); !d.Before(earliest); d = d.AddDate(0, 0, -1) {
		if isGood(d) {
			return d, rain[d], true
		}
	}
	for d := target.AddDate(0, 0, 1); ; d = d.AddDate(0, 0, 1) {
		if _, inForecast := rain[d]; !inForecast {
			break
		}
		if isGood(d) {
			return d, rain[d], true
		}
	}
	return target, 0, false
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func truncateToDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
