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
	// Embeds timezone data so the lawn's timezone resolves even on hosts
	// without a system tz database.
	_ "time/tzdata"

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
	forecast, err := wc.GetDailyForecast(ctx, cfg.Location.Latitude, cfg.Location.Longitude)
	if err != nil {
		return fmt.Errorf("fetching forecast: %w", err)
	}
	if len(forecast.Days) == 0 {
		return fmt.Errorf("forecast returned no daily data")
	}

	// One Call 3.0's daily[0] is always the forecast location's "today".
	today := forecast.Days[0].Date

	tc := todoist.NewClient(cfg.Secrets.TodoistAPIToken)

	lookback := time.Duration(cfg.Schedule.CompletedLookbackDays) * 24 * time.Hour
	lastMow, err := tc.FindLatestCompletedByLabel(ctx, cfg.Todoist.Label, lookback, forecast.TimeZone)
	if err != nil {
		return fmt.Errorf("looking up last completed mow: %w", err)
	}
	if lastMow != nil {
		log.Printf("last mow: %s", lastMow.Format("2006-01-02"))
	} else {
		log.Printf("no completed mow found in the last %d days", cfg.Schedule.CompletedLookbackDays)
	}

	existing, err := tc.FindScheduledMow(ctx, cfg.Todoist.Label)
	if err != nil {
		return fmt.Errorf("looking up the scheduled mow: %w", err)
	}

	in := planner.Input{
		Today:                today,
		LastMow:              lastMow,
		MinIntervalDays:      cfg.Schedule.MinDaysBetweenMows,
		IdealIntervalDays:    cfg.Schedule.IdealDaysBetweenMows,
		RainThresholdPercent: cfg.Schedule.RainProbabilityThresholdPercent,
	}
	for _, d := range forecast.Days {
		in.Forecast = append(in.Forecast, planner.ForecastDay{Date: d.Date, RainProbabilityPercent: d.RainProbabilityPercent})
	}
	if existing != nil {
		// A missing due date leaves Due zero, which never matches a
		// marker, so the task is treated as Pinned.
		due, _ := existing.DueDate()
		in.Scheduled = &planner.ScheduledMow{Due: due, Description: existing.Description}
	}

	decision := planner.Plan(in)
	day := decision.Date.Format("2006-01-02")
	prefix := ""
	if dryRun {
		prefix = "[dry-run] would have "
	}

	switch decision.Action {
	case planner.ActionNone:
		if existing != nil {
			log.Printf("mow task already scheduled (id=%s, due=%s); leaving it as is", existing.ID, dueString(existing))
		}
		return nil

	case planner.ActionCreate:
		if !dryRun {
			task, err := tc.CreateTask(ctx, cfg.Todoist.TaskContent, decision.Description, decision.Date, cfg.Todoist.Label, cfg.Todoist.ProjectID)
			if err != nil {
				return fmt.Errorf("creating Todoist task: %w", err)
			}
			log.Printf("created mow task id=%s due=%s", task.ID, day)
			return nil
		}
		log.Printf("%screated mow task %q due %s with label %q", prefix, cfg.Todoist.TaskContent, day, cfg.Todoist.Label)
		return nil

	case planner.ActionMove:
		if !dryRun {
			if _, err := tc.MoveTask(ctx, existing.ID, decision.Date, decision.Description); err != nil {
				return fmt.Errorf("moving Todoist task %s: %w", existing.ID, err)
			}
		}
		log.Printf("%smoved mow task id=%s from %s to %s", prefix, existing.ID, dueString(existing), day)
		return nil
	}
	return fmt.Errorf("unknown planner action %v", decision.Action)
}

func dueString(t *todoist.Task) string {
	if due, ok := t.DueDate(); ok {
		return due.Format("2006-01-02")
	}
	return "no date"
}
