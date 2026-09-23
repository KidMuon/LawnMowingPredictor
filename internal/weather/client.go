// Package weather fetches daily forecasts from OpenWeatherMap's One Call
// API 3.0 (https://openweathermap.org/api/one-call-3) and converts them
// into calendar-date, rain-probability-percent terms the planner package
// can use.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/KidMuon/LawnMowingPredictor/internal/httpretry"
)

const defaultBaseURL = "https://api.openweathermap.org/data/3.0/onecall"

// Client talks to OpenWeatherMap's One Call API 3.0.
type Client struct {
	APIKey string
	// Units is one of "standard", "metric", or "imperial". Defaults to
	// "imperial" if empty.
	Units      string
	HTTPClient *http.Client
	// BaseURL overrides the API endpoint; used by tests. Defaults to
	// OpenWeatherMap's production One Call 3.0 endpoint.
	BaseURL string
	// RetryDelay is the wait between retries of a temporary server
	// error. Defaults to httpretry.DefaultDelay.
	RetryDelay time.Duration
}

// NewClient returns a Client ready to make requests.
func NewClient(apiKey, units string) *Client {
	return &Client{
		APIKey:     apiKey,
		Units:      units,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Day is one day of the One Call daily forecast, converted to the forecast
// location's local calendar date (using the API's timezone_offset, since
// the raw "dt" timestamp is UTC).
type Day struct {
	Date                   time.Time
	RainProbabilityPercent float64
	Summary                string
	TempMax                float64
	TempMin                float64
}

// Forecast is the daily forecast for a location, along with that
// location's timezone.
type Forecast struct {
	// Location is the forecast location's timezone.
	Location *time.Location
	Days     []Day
}

// oneCallResponse mirrors the subset of the One Call 3.0 JSON response this
// package cares about. See https://openweathermap.org/api/one-call-3 for
// the full schema.
type oneCallResponse struct {
	Timezone       string     `json:"timezone"`
	TimezoneOffset int        `json:"timezone_offset"`
	Daily          []dailyRaw `json:"daily"`
}

type dailyRaw struct {
	Dt   int64   `json:"dt"`
	Pop  float64 `json:"pop"` // probability of precipitation, 0.0-1.0
	Temp struct {
		Max float64 `json:"max"`
		Min float64 `json:"min"`
	} `json:"temp"`
	Weather []struct {
		Description string `json:"description"`
	} `json:"weather"`
}

// GetDailyForecast fetches the daily forecast (today + up to 7 more days)
// for the given coordinates. The current, minutely, hourly, and alerts
// sections are excluded from the request since only the daily forecast is
// needed.
func (c *Client) GetDailyForecast(ctx context.Context, lat, lon float64) (*Forecast, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("weather: no API key configured")
	}

	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(lat, 'f', -1, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', -1, 64))
	q.Set("exclude", "current,minutely,hourly,alerts")
	units := c.Units
	if units == "" {
		units = "imperial"
	}
	q.Set("units", units)
	q.Set("appid", c.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("weather: building request: %w", err)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	delay := c.RetryDelay
	if delay == 0 {
		delay = httpretry.DefaultDelay
	}
	resp, err := httpretry.Do(httpClient, req, delay)
	if err != nil {
		return nil, fmt.Errorf("weather: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("weather: reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weather: unexpected status %d from One Call API: %s", resp.StatusCode, truncate(string(body), 500))
	}

	var parsed oneCallResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("weather: parsing response: %w", err)
	}

	offset := time.Duration(parsed.TimezoneOffset) * time.Second
	days := make([]Day, 0, len(parsed.Daily))
	for _, d := range parsed.Daily {
		// dt is a UTC unix timestamp; shifting by the location's
		// timezone_offset gives the correct local calendar date even
		// when the runner and the forecast location are in different
		// timezones.
		local := time.Unix(d.Dt, 0).UTC().Add(offset)
		y, m, dd := local.Date()
		date := time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)

		summary := ""
		if len(d.Weather) > 0 {
			summary = d.Weather[0].Description
		}

		days = append(days, Day{
			Date:                   date,
			RainProbabilityPercent: d.Pop * 100,
			Summary:                summary,
			TempMax:                d.Temp.Max,
			TempMin:                d.Temp.Min,
		})
	}
	loc, err := time.LoadLocation(parsed.Timezone)
	if err != nil || parsed.Timezone == "" {
		// Unknown zone name: today's offset is close enough.
		loc = time.FixedZone(parsed.Timezone, parsed.TimezoneOffset)
	}
	return &Forecast{Location: loc, Days: days}, nil
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
