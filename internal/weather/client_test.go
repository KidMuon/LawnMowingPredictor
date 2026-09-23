package weather

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetDailyForecast(t *testing.T) {
	// dt values are UTC unix timestamps at roughly noon local time for
	// each day; timezone_offset is -6 hours (America/Denver, standard
	// time), chosen so the UTC calendar date and local calendar date
	// differ, exercising the timezone-shift logic.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("appid") != "test-key" {
			t.Errorf("appid = %q, want test-key", q.Get("appid"))
		}
		if q.Get("lat") != "39.7392" {
			t.Errorf("lat = %q, want 39.7392", q.Get("lat"))
		}
		if q.Get("units") != "imperial" {
			t.Errorf("units = %q, want imperial", q.Get("units"))
		}
		if q.Get("exclude") != "current,minutely,hourly,alerts" {
			t.Errorf("exclude = %q", q.Get("exclude"))
		}

		resp := map[string]interface{}{
			"timezone_offset": -21600, // -6h
			"daily": []map[string]interface{}{
				{
					"dt":  time.Date(2026, 9, 23, 3, 0, 0, 0, time.UTC).Unix(), // 2026-09-22 21:00 local
					"pop": 0.05,
					"temp": map[string]float64{
						"max": 75.0,
						"min": 55.0,
					},
					"weather": []map[string]string{{"description": "clear sky"}},
				},
				{
					"dt":  time.Date(2026, 9, 24, 18, 0, 0, 0, time.UTC).Unix(), // 2026-09-24 12:00 local
					"pop": 0.8,
					"temp": map[string]float64{
						"max": 68.0,
						"min": 50.0,
					},
					"weather": []map[string]string{{"description": "heavy rain"}},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encoding test response: %v", err)
		}
	}))
	defer server.Close()

	c := NewClient("test-key", "imperial")
	c.BaseURL = server.URL

	forecast, err := c.GetDailyForecast(context.Background(), 39.7392, -104.9903)
	if err != nil {
		t.Fatalf("GetDailyForecast() error = %v", err)
	}
	days := forecast.Days
	if len(days) != 2 {
		t.Fatalf("got %d days, want 2", len(days))
	}

	if want := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC); !days[0].Date.Equal(want) {
		t.Errorf("day[0].Date = %v, want %v (timezone-shifted)", days[0].Date, want)
	}
	if days[0].RainProbabilityPercent != 5 {
		t.Errorf("day[0].RainProbabilityPercent = %v, want 5", days[0].RainProbabilityPercent)
	}
	if days[0].Summary != "clear sky" {
		t.Errorf("day[0].Summary = %q, want clear sky", days[0].Summary)
	}

	if want := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC); !days[1].Date.Equal(want) {
		t.Errorf("day[1].Date = %v, want %v", days[1].Date, want)
	}
	if days[1].RainProbabilityPercent != 80 {
		t.Errorf("day[1].RainProbabilityPercent = %v, want 80", days[1].RainProbabilityPercent)
	}
}

func TestGetDailyForecast_MissingAPIKey(t *testing.T) {
	c := NewClient("", "imperial")
	_, err := c.GetDailyForecast(context.Background(), 0, 0)
	if err == nil {
		t.Fatalf("expected error for missing API key")
	}
}

func TestGetDailyForecast_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"cod":401,"message":"Invalid API key"}`))
	}))
	defer server.Close()

	c := NewClient("bad-key", "imperial")
	c.BaseURL = server.URL

	_, err := c.GetDailyForecast(context.Background(), 39.7392, -104.9903)
	if err == nil {
		t.Fatalf("expected error for 401 response")
	}
}

func serveJSON(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGetDailyForecast_ReportsTheLawnsTimezone(t *testing.T) {
	server := serveJSON(t, `{"timezone": "America/New_York", "timezone_offset": -14400, "daily": []}`)
	c := NewClient("test-key", "imperial")
	c.BaseURL = server.URL

	forecast, err := c.GetDailyForecast(context.Background(), 40.7, -74)
	if err != nil {
		t.Fatalf("GetDailyForecast() error = %v", err)
	}
	if got := forecast.TimeZone.String(); got != "America/New_York" {
		t.Errorf("TimeZone = %q, want America/New_York", got)
	}
}

func TestGetDailyForecast_RetriesATemporaryServerError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"timezone": "America/New_York", "daily": [{"dt": 1790000000, "pop": 0.1}]}`))
	}))
	defer server.Close()
	c := NewClient("test-key", "imperial")
	c.BaseURL = server.URL
	c.RetryDelay = time.Millisecond

	forecast, err := c.GetDailyForecast(context.Background(), 40.7, -74)
	if err != nil {
		t.Fatalf("GetDailyForecast() error = %v", err)
	}
	if len(forecast.Days) != 1 {
		t.Errorf("got %d days, want 1", len(forecast.Days))
	}
}
