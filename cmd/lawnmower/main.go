// Command lawnmower checks the weather forecast for a configured
// location, decides whether there's a good rain-free day to mow the lawn,
// and if so, schedules a Todoist task for it.
//
// It's meant to be run periodically (cron, a systemd timer, a scheduled
// GitHub Actions workflow, etc.) rather than left running continuously;
// see the README for scheduling examples. It's safe to run more often
// than your mowing interval: it looks up your mowing history and any
// already-scheduled task in Todoist before creating a new one.
//
// API credentials (OPENWEATHERMAP_API_KEY and TODOIST_API_TOKEN) are read
// from the process environment. A .env file (default: .env in the working
// directory) is loaded first as a convenience, but never overrides a
// variable that's already set in the real environment.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/KidMuon/LawnMowingPredictor/internal/config"
	"github.com/KidMuon/LawnMowingPredictor/internal/planner"
	"github.com/KidMuon/LawnMowingPredictor/internal/todoist"
	"github.com/KidMuon/LawnMowingPredictor/internal/weather"
)

func main() {
	configPath := flag.String("config", "", "path to YAML config file (default: config.yaml, or $LAWNMOWER_CONFIG)")
	envFilePath := flag.String("env-file", "", "path to a .env file holding OPENWEATHERMAP_API_KEY and TODOIST_API_TOKEN (default: .env, or $LAWNMOWER_ENV_FILE)")
	dryRun := flag.Bool("dry-run", false, "compute and log the decision, but never create a Todoist task")
	flag.Parse()

	envPath, envPathExplicit := *envFilePath, *envFilePath != ""
	if envPath == "" {
		envPath, envPathExplicit = os.Getenv("LAWNMOWER_ENV_FILE"), os.Getenv("LAWNMOWER_ENV_FILE") != ""
	}
	if envPath == "" {
		envPath = ".env"
	}
	if err := config.LoadEnvFile(envPath, envPathExplicit); err != nil {
		log.Fatalf("loading .env file: %v", err)
	}

	path := *configPath
	if path == "" {
		path = os.Getenv("LAWNMOWER_CONFIG")
	}
	if path == "" {
		path = "config.yaml"
	}

	cfg, err := config.Load(path)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := run(ctx, cfg, *dryRun); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(ctx context.Context, cfg *config.Config, dryRun bool) error {
	wc := weather.NewClient(cfg.Secrets.OpenWeatherAPIKey, cfg.Location.Units)
	days, err := wc.GetDailyForecast(ctx, cfg.Location.Latitude, cfg.Location.Longitude)
	if err != nil {
		return fmt.Errorf("fetching forecast: %w", err)
	}
	if len(days) == 0 {
		return fmt.Errorf("forecast returned no daily data")
	}

	// One Call 3.0's daily[0] is always the forecast location's "today".
	today := days[0].Date

	tc := todoist.NewClient(cfg.Secrets.TodoistAPIToken)

	lookback := time.Duration(cfg.Schedule.CompletedLookbackDays) * 24 * time.Hour
	lastMow, err := tc.FindLatestCompletedByLabel(ctx, cfg.Todoist.Label, lookback)
	if err != nil {
		return fmt.Errorf("looking up last completed mow: %w", err)
	}

	earliest := planner.EarliestEligibleDate(today, lastMow, cfg.Schedule.MinDaysBetweenMows)
	if lastMow != nil {
		log.Printf("last completed mow: %s (next eligible on/after %s)", lastMow.Format("2006-01-02"), earliest.Format("2006-01-02"))
	} else {
		log.Printf("no completed mow found in the last %d days (next eligible on/after %s)", cfg.Schedule.CompletedLookbackDays, earliest.Format("2006-01-02"))
	}

	forecastDays := make([]planner.ForecastDay, 0, len(days))
	for _, d := range days {
		forecastDays = append(forecastDays, planner.ForecastDay{
			Date:                   d.Date,
			RainProbabilityPercent: d.RainProbabilityPercent,
		})
	}

	chosen, ok := planner.ChooseMowDate(forecastDays, earliest, cfg.Schedule.RainProbabilityThresholdPercent)
	if !ok {
		log.Printf("no day in the %d-day forecast (from %s) is both on/after %s and at/under %.0f%% chance of rain; nothing scheduled this run",
			len(days), today.Format("2006-01-02"), earliest.Format("2006-01-02"), cfg.Schedule.RainProbabilityThresholdPercent)
		return nil
	}
	log.Printf("chosen mow date: %s (%.0f%% chance of rain)", chosen.Date.Format("2006-01-02"), chosen.RainProbabilityPercent)

	existing, err := tc.FindOpenFutureTaskByLabel(ctx, cfg.Todoist.Label, today)
	if err != nil {
		return fmt.Errorf("checking for an already-scheduled mow: %w", err)
	}
	if existing != nil {
		due := "unknown date"
		if existing.Due != nil {
			due = existing.Due.Date
		}
		log.Printf("an open mow task already exists (id=%s, due=%s); not creating another", existing.ID, due)
		return nil
	}

	if dryRun {
		log.Printf("[dry-run] would create Todoist task %q due %s with label %q", cfg.Todoist.TaskContent, chosen.Date.Format("2006-01-02"), cfg.Todoist.Label)
		return nil
	}

	description := fmt.Sprintf("Scheduled automatically: %.0f%% chance of rain forecast for %s.", chosen.RainProbabilityPercent, chosen.Date.Format("2006-01-02"))
	task, err := tc.CreateTask(ctx, cfg.Todoist.TaskContent, description, chosen.Date, cfg.Todoist.Label, cfg.Todoist.ProjectID)
	if err != nil {
		return fmt.Errorf("creating Todoist task: %w", err)
	}

	log.Printf("created Todoist task id=%s due=%s", task.ID, chosen.Date.Format("2006-01-02"))
	return nil
}
