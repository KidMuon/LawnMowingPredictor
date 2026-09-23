// Package config loads LawnMowingPredictor's settings from a YAML file,
// applies optional environment-variable overrides, and reads the two
// required secrets (API credentials) from the environment. Secrets are
// intentionally never read from the YAML file so they can't accidentally
// be committed to version control alongside the rest of the config.
//
// Secrets are commonly supplied via a local .env file (see LoadEnvFile);
// that file is just a convenient way to populate the process environment
// and is treated no differently than variables exported some other way -
// real environment variables always take precedence over the .env file.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// LoadEnvFile loads KEY=VALUE pairs from a .env-style file at path into the
// process environment. It never overrides a variable that's already set,
// so real environment variables (exported manually, injected by a systemd
// EnvironmentFile, set as CI secrets, etc.) always take precedence over the
// file.
//
// If explicit is false, path is treated as a default (e.g. ".env") rather
// than something the caller specifically asked for, so a missing file is
// not an error - it's normal to run without one when secrets are already
// present in the environment. If explicit is true, a missing file is
// reported as an error.
func LoadEnvFile(path string, explicit bool) error {
	if err := godotenv.Load(path); err != nil {
		if os.IsNotExist(err) && !explicit {
			return nil
		}
		return fmt.Errorf("loading env file %q: %w", path, err)
	}
	return nil
}

// Config holds all application settings.
type Config struct {
	Location LocationConfig `yaml:"location"`
	Schedule ScheduleConfig `yaml:"schedule"`
	Todoist  TodoistConfig  `yaml:"todoist"`

	// Secrets is never populated from YAML; see Load.
	Secrets Secrets `yaml:"-"`
}

// LocationConfig identifies where to fetch the forecast for.
type LocationConfig struct {
	Latitude  float64 `yaml:"latitude"`
	Longitude float64 `yaml:"longitude"`
	// Units is one of "standard", "metric", or "imperial" (OpenWeatherMap's
	// own unit names). It only affects the temperature units mentioned in
	// the created task's description; it has no effect on the rain-chance
	// decision, which is always a percentage.
	Units string `yaml:"units"`
}

// ScheduleConfig controls the mowing-date decision.
type ScheduleConfig struct {
	// MinDaysBetweenMows is the Minimum Interval: the fewest days after
	// the Last Mow that a new mow may be scheduled.
	MinDaysBetweenMows int `yaml:"min_days_between_mows"`
	// IdealDaysBetweenMows is the Ideal Interval: the Target Date is the
	// Last Mow plus this many days.
	IdealDaysBetweenMows int `yaml:"ideal_days_between_mows"`
	// RainProbabilityThresholdPercent is the maximum acceptable chance of
	// rain (0-100) for a day to be considered safe to mow.
	RainProbabilityThresholdPercent float64 `yaml:"rain_probability_threshold_percent"`
	// CompletedLookbackDays bounds how far back Todoist's completed-task
	// history is searched to find the last mow. If nothing is found in
	// this window, the app assumes there's no relevant history and treats
	// today as eligible. OpenWeatherMap's forecast horizon is separate and
	// fixed by the API at 8 days (today + 7).
	CompletedLookbackDays int `yaml:"completed_lookback_days"`
}

// TodoistConfig controls how mowing tasks are found and created in Todoist.
type TodoistConfig struct {
	// Label identifies lawn-mowing tasks, both when searching for past ones
	// and when creating a new one. Do not include the leading "@". The
	// label must already exist in your Todoist account.
	Label string `yaml:"label"`
	// TaskContent is the task text used when creating a new task.
	TaskContent string `yaml:"task_content"`
	// ProjectID optionally places created tasks in a specific project.
	// Leave blank to use your Todoist Inbox.
	ProjectID string `yaml:"project_id"`
}

// Secrets holds credential values that must come from the environment.
type Secrets struct {
	// OpenWeatherAPIKey is read from OPENWEATHERMAP_API_KEY.
	OpenWeatherAPIKey string
	// TodoistAPIToken is read from TODOIST_API_TOKEN.
	TodoistAPIToken string
}

// Default returns a Config populated with sane defaults. Load starts from
// this before applying the YAML file and environment overrides.
func Default() *Config {
	return &Config{
		Location: LocationConfig{
			Units: "imperial",
		},
		Schedule: ScheduleConfig{
			MinDaysBetweenMows:              7,
			RainProbabilityThresholdPercent: 30,
			CompletedLookbackDays:           90,
		},
		Todoist: TodoistConfig{
			Label:       "lawn-mowing",
			TaskContent: "Mow the lawn",
		},
	}
}

// Load reads settings from the YAML file at path (if it exists), applies
// any LAWNMOWER_* environment overrides, reads the required secrets from
// the environment, and validates the result.
//
// If path is empty, no file is read and defaults + environment overrides
// apply. It is not an error for path to point to a nonexistent file only
// when path was defaulted by the caller to "config.yaml"; callers that
// pass an explicit path expect it to exist.
func Load(path string) (*Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config file %q: %w", path, err)
		}
	}

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}
	// Left unset, the Ideal Interval is the Minimum, so a config written
	// before ideal_days_between_mows existed keeps working.
	if cfg.Schedule.IdealDaysBetweenMows == 0 {
		cfg.Schedule.IdealDaysBetweenMows = cfg.Schedule.MinDaysBetweenMows
	}

	cfg.Secrets.OpenWeatherAPIKey = os.Getenv("OPENWEATHERMAP_API_KEY")
	cfg.Secrets.TodoistAPIToken = os.Getenv("TODOIST_API_TOKEN")

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// applyEnvOverrides lets any non-secret setting be overridden without
// editing the YAML file, which is convenient for CI/cron environments.
func applyEnvOverrides(cfg *Config) error {
	var errs []string

	setFloat := func(env string, dst *float64) {
		v, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%q is not a valid number", env, v))
			return
		}
		*dst = f
	}
	setInt := func(env string, dst *int) {
		v, ok := os.LookupEnv(env)
		if !ok {
			return
		}
		i, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s=%q is not a valid integer", env, v))
			return
		}
		*dst = i
	}
	setString := func(env string, dst *string) {
		if v, ok := os.LookupEnv(env); ok {
			*dst = v
		}
	}

	setFloat("LAWNMOWER_LATITUDE", &cfg.Location.Latitude)
	setFloat("LAWNMOWER_LONGITUDE", &cfg.Location.Longitude)
	setString("LAWNMOWER_UNITS", &cfg.Location.Units)

	setInt("LAWNMOWER_MIN_DAYS_BETWEEN_MOWS", &cfg.Schedule.MinDaysBetweenMows)
	setInt("LAWNMOWER_IDEAL_DAYS_BETWEEN_MOWS", &cfg.Schedule.IdealDaysBetweenMows)
	setFloat("LAWNMOWER_RAIN_THRESHOLD_PERCENT", &cfg.Schedule.RainProbabilityThresholdPercent)
	setInt("LAWNMOWER_COMPLETED_LOOKBACK_DAYS", &cfg.Schedule.CompletedLookbackDays)

	setString("LAWNMOWER_TODOIST_LABEL", &cfg.Todoist.Label)
	setString("LAWNMOWER_TODOIST_TASK_CONTENT", &cfg.Todoist.TaskContent)
	setString("LAWNMOWER_TODOIST_PROJECT_ID", &cfg.Todoist.ProjectID)

	if len(errs) > 0 {
		return fmt.Errorf("invalid environment overrides: %s", strings.Join(errs, "; "))
	}
	return nil
}

// Validate checks that the config is complete and internally consistent.
func (c *Config) Validate() error {
	var errs []string

	if c.Location.Latitude < -90 || c.Location.Latitude > 90 {
		errs = append(errs, fmt.Sprintf("location.latitude must be between -90 and 90, got %v", c.Location.Latitude))
	}
	if c.Location.Longitude < -180 || c.Location.Longitude > 180 {
		errs = append(errs, fmt.Sprintf("location.longitude must be between -180 and 180, got %v", c.Location.Longitude))
	}
	switch c.Location.Units {
	case "standard", "metric", "imperial":
	default:
		errs = append(errs, fmt.Sprintf("location.units must be one of standard, metric, imperial, got %q", c.Location.Units))
	}

	if c.Schedule.MinDaysBetweenMows < 0 {
		errs = append(errs, "schedule.min_days_between_mows must be >= 0")
	}
	if c.Schedule.MinDaysBetweenMows > c.Schedule.IdealDaysBetweenMows {
		errs = append(errs, fmt.Sprintf("schedule.min_days_between_mows (%d) must not be larger than schedule.ideal_days_between_mows (%d)", c.Schedule.MinDaysBetweenMows, c.Schedule.IdealDaysBetweenMows))
	}
	if c.Schedule.RainProbabilityThresholdPercent < 0 || c.Schedule.RainProbabilityThresholdPercent > 100 {
		errs = append(errs, fmt.Sprintf("schedule.rain_probability_threshold_percent must be between 0 and 100, got %v", c.Schedule.RainProbabilityThresholdPercent))
	}
	if c.Schedule.CompletedLookbackDays <= 0 {
		errs = append(errs, "schedule.completed_lookback_days must be > 0")
	}

	if strings.TrimSpace(c.Todoist.Label) == "" {
		errs = append(errs, "todoist.label must not be empty")
	}
	if strings.TrimSpace(c.Todoist.TaskContent) == "" {
		errs = append(errs, "todoist.task_content must not be empty")
	}

	if c.Secrets.OpenWeatherAPIKey == "" {
		errs = append(errs, "OPENWEATHERMAP_API_KEY environment variable is not set")
	}
	if c.Secrets.TodoistAPIToken == "" {
		errs = append(errs, "TODOIST_API_TOKEN environment variable is not set")
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
