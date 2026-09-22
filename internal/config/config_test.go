package config

import (
	"os"
	"path/filepath"
	"testing"
)

func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func minimalSecrets(t *testing.T) {
	t.Helper()
	withEnv(t, map[string]string{
		"OPENWEATHERMAP_API_KEY": "owm-test-key",
		"TODOIST_API_TOKEN":      "todoist-test-token",
	})
}

func TestLoad_DefaultsAndFile(t *testing.T) {
	minimalSecrets(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := `
location:
  latitude: 39.7392
  longitude: -104.9903
  units: metric
schedule:
  min_days_between_mows: 10
  rain_probability_threshold_percent: 25
  completed_lookback_days: 60
todoist:
  label: mow-lawn
  task_content: "Go mow"
  project_id: "12345"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Location.Latitude != 39.7392 || cfg.Location.Longitude != -104.9903 {
		t.Errorf("unexpected location: %+v", cfg.Location)
	}
	if cfg.Location.Units != "metric" {
		t.Errorf("units = %q, want metric", cfg.Location.Units)
	}
	if cfg.Schedule.MinDaysBetweenMows != 10 {
		t.Errorf("min_days_between_mows = %d, want 10", cfg.Schedule.MinDaysBetweenMows)
	}
	if cfg.Schedule.RainProbabilityThresholdPercent != 25 {
		t.Errorf("rain threshold = %v, want 25", cfg.Schedule.RainProbabilityThresholdPercent)
	}
	if cfg.Todoist.Label != "mow-lawn" {
		t.Errorf("label = %q, want mow-lawn", cfg.Todoist.Label)
	}
	if cfg.Secrets.OpenWeatherAPIKey != "owm-test-key" {
		t.Errorf("OpenWeatherAPIKey not populated from env")
	}
	if cfg.Secrets.TodoistAPIToken != "todoist-test-token" {
		t.Errorf("TodoistAPIToken not populated from env")
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	minimalSecrets(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	contents := `
location:
  latitude: 10
  longitude: 10
schedule:
  min_days_between_mows: 7
  rain_probability_threshold_percent: 30
  completed_lookback_days: 90
todoist:
  label: lawn-mowing
  task_content: "Mow the lawn"
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}

	withEnv(t, map[string]string{
		"LAWNMOWER_MIN_DAYS_BETWEEN_MOWS":  "14",
		"LAWNMOWER_RAIN_THRESHOLD_PERCENT": "45.5",
		"LAWNMOWER_TODOIST_LABEL":          "yardwork",
	})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Schedule.MinDaysBetweenMows != 14 {
		t.Errorf("min_days_between_mows = %d, want 14 (env override)", cfg.Schedule.MinDaysBetweenMows)
	}
	if cfg.Schedule.RainProbabilityThresholdPercent != 45.5 {
		t.Errorf("rain threshold = %v, want 45.5 (env override)", cfg.Schedule.RainProbabilityThresholdPercent)
	}
	if cfg.Todoist.Label != "yardwork" {
		t.Errorf("label = %q, want yardwork (env override)", cfg.Todoist.Label)
	}
}

func TestLoad_DefaultsWithoutFile(t *testing.T) {
	minimalSecrets(t)
	withEnv(t, map[string]string{
		"LAWNMOWER_LATITUDE":  "1.23",
		"LAWNMOWER_LONGITUDE": "4.56",
	})

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Schedule.MinDaysBetweenMows != 7 {
		t.Errorf("default min_days_between_mows = %d, want 7", cfg.Schedule.MinDaysBetweenMows)
	}
	if cfg.Todoist.Label != "lawn-mowing" {
		t.Errorf("default label = %q, want lawn-mowing", cfg.Todoist.Label)
	}
	if cfg.Location.Latitude != 1.23 || cfg.Location.Longitude != 4.56 {
		t.Errorf("location not overridden from env: %+v", cfg.Location)
	}
}

func TestLoad_MissingSecretsError(t *testing.T) {
	cfg, err := Load("")
	if err == nil {
		t.Fatalf("expected error for missing secrets, got config %+v", cfg)
	}
}

func TestLoad_InvalidThresholdError(t *testing.T) {
	minimalSecrets(t)
	withEnv(t, map[string]string{"LAWNMOWER_RAIN_THRESHOLD_PERCENT": "150"})

	_, err := Load("")
	if err == nil {
		t.Fatalf("expected validation error for out-of-range threshold")
	}
}

func TestLoad_InvalidEnvNumberError(t *testing.T) {
	minimalSecrets(t)
	withEnv(t, map[string]string{"LAWNMOWER_MIN_DAYS_BETWEEN_MOWS": "not-a-number"})

	_, err := Load("")
	if err == nil {
		t.Fatalf("expected error for invalid env override value")
	}
}

func TestLoad_MissingFileError(t *testing.T) {
	minimalSecrets(t)
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatalf("expected error for missing explicit config file")
	}
}

func TestLoadEnvFile_SetsUnsetVariables(t *testing.T) {
	t.Cleanup(func() {
		os.Unsetenv("LAWNMOWER_TEST_DOTENV_A")
		os.Unsetenv("LAWNMOWER_TEST_DOTENV_B")
	})

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	contents := "LAWNMOWER_TEST_DOTENV_A=from-file\nLAWNMOWER_TEST_DOTENV_B=also-from-file\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test .env file: %v", err)
	}

	if err := LoadEnvFile(path, true); err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}

	if got := os.Getenv("LAWNMOWER_TEST_DOTENV_A"); got != "from-file" {
		t.Errorf("LAWNMOWER_TEST_DOTENV_A = %q, want from-file", got)
	}
	if got := os.Getenv("LAWNMOWER_TEST_DOTENV_B"); got != "also-from-file" {
		t.Errorf("LAWNMOWER_TEST_DOTENV_B = %q, want also-from-file", got)
	}
}

func TestLoadEnvFile_DoesNotOverrideExistingVariables(t *testing.T) {
	t.Setenv("LAWNMOWER_TEST_DOTENV_C", "real-environment-wins")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	contents := "LAWNMOWER_TEST_DOTENV_C=from-file\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test .env file: %v", err)
	}

	if err := LoadEnvFile(path, true); err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}

	if got := os.Getenv("LAWNMOWER_TEST_DOTENV_C"); got != "real-environment-wins" {
		t.Errorf("LAWNMOWER_TEST_DOTENV_C = %q, want real-environment-wins (existing env should not be overridden)", got)
	}
}

func TestLoadEnvFile_MissingDefaultIsNotAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.env")
	if err := LoadEnvFile(path, false); err != nil {
		t.Fatalf("expected no error for a missing default .env file, got %v", err)
	}
}

func TestLoadEnvFile_MissingExplicitIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.env")
	if err := LoadEnvFile(path, true); err == nil {
		t.Fatalf("expected error for a missing explicitly-requested .env file")
	}
}
