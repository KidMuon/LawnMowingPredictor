// Package planner contains the pure decision logic for choosing a mowing
// date: given a weather forecast and mowing history, it decides whether
// there is a good day to mow, and if so, which one. It has no knowledge of
// HTTP, Todoist, or OpenWeatherMap and is deliberately easy to unit test.
package planner

import (
	"sort"
	"time"
)

// ForecastDay is one day's weather forecast, normalized to a calendar date
// (time-of-day and location are not meaningful and should be zeroed by the
// caller, e.g. via a UTC midnight timestamp).
type ForecastDay struct {
	Date                   time.Time
	RainProbabilityPercent float64
}

// EarliestEligibleDate returns the first calendar date on or after which a
// new mow may be scheduled, given when the lawn was last mowed (if known)
// and the configured minimum number of days between mows.
//
// If lastMow is nil (no mowing history was found), today is eligible
// immediately. Otherwise the earliest eligible date is lastMow +
// minDaysBetweenMows, but never earlier than today.
func EarliestEligibleDate(today time.Time, lastMow *time.Time, minDaysBetweenMows int) time.Time {
	today = truncateToDate(today)
	if lastMow == nil {
		return today
	}

	earliest := truncateToDate(*lastMow).AddDate(0, 0, minDaysBetweenMows)
	if earliest.Before(today) {
		return today
	}
	return earliest
}

// ChooseMowDate scans forecast for the earliest date that is on or after
// earliestEligible and whose rain probability is at or under
// thresholdPercent. forecast need not be pre-sorted. It returns the chosen
// day and true, or a zero ForecastDay and false if no day qualifies (e.g.
// every eligible day in the forecast is too likely to rain, or the forecast
// doesn't extend far enough to reach earliestEligible).
func ChooseMowDate(forecast []ForecastDay, earliestEligible time.Time, thresholdPercent float64) (ForecastDay, bool) {
	earliestEligible = truncateToDate(earliestEligible)

	sorted := make([]ForecastDay, len(forecast))
	copy(sorted, forecast)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

	for _, day := range sorted {
		d := truncateToDate(day.Date)
		if d.Before(earliestEligible) {
			continue
		}
		if day.RainProbabilityPercent <= thresholdPercent {
			return ForecastDay{Date: d, RainProbabilityPercent: day.RainProbabilityPercent}, true
		}
	}
	return ForecastDay{}, false
}

func truncateToDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
