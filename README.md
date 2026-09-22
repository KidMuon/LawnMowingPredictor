# LawnMowingPredictor

A small Go CLI that checks the weather forecast for your location and, if
there's a good rain-free day coming up, schedules a "Mow the lawn" task in
Todoist for it.

It's meant to be run periodically by an external scheduler (cron, a
systemd timer, a scheduled CI job, ...) rather than left running as a
daemon. It's safe to run more often than your mowing interval - each run
checks your Todoist history and any already-scheduled task before creating
a new one, so it won't pile up duplicates.

## How the decision works

1. Fetch the daily forecast (today + next 7 days) from [OpenWeatherMap's
   One Call API 3.0](https://openweathermap.org/api/one-call-3) for your
   configured coordinates.
2. Look up the most recently **completed** Todoist task carrying your
   configured label, searching back up to `completed_lookback_days`. If
   none is found, today counts as eligible immediately.
3. Compute the earliest eligible date: `last mow date + min_days_between_mows`
   (or today, if that's later, or if there's no mowing history).
4. Starting from the earliest eligible date, walk the forecast and pick the
   **first** day whose chance of rain is at or under
   `rain_probability_threshold_percent`. This favors mowing sooner rather
   than holding out for the single driest day in the window.
5. If a day is found, check whether an **open** (incomplete) task with the
   label is already due today or later. If so, do nothing - it's already
   scheduled. Otherwise, create a new Todoist task due on the chosen date.
6. If no day in the forecast qualifies, nothing is created; just re-run it
   again later (e.g. on the next scheduled run) once the forecast has
   moved forward.

All of this logic lives in `internal/planner`, which has no knowledge of
HTTP/Todoist/weather APIs and is fully unit tested.

## Setup

### 1. Prerequisites

- Go 1.22+
- An OpenWeatherMap account with a **One Call API 3.0** subscription and
  API key (the free tier includes 1,000 calls/day, which is far more than
  a daily cron run needs).
- A Todoist account and a [personal API
  token](https://todoist.com/app/settings/integrations/developer).
- A Todoist label (e.g. `lawn-mowing`) created once ahead of time in your
  account - this app doesn't create the label itself, only tasks tagged
  with it.

### 2. Configure

```bash
cp config.example.yaml config.yaml
$EDITOR config.yaml   # set your latitude/longitude and adjust to taste
```

`config.yaml` is gitignored - it holds your location and preferences, not
secrets, but there's no reason to commit it either.

### 3. Set the two required secrets

These are **never** read from `config.yaml`, so they can't accidentally end
up committed. The easiest way to supply them is a `.env` file:

```bash
cp .env.example .env
$EDITOR .env   # fill in OPENWEATHERMAP_API_KEY and TODOIST_API_TOKEN
```

`.env` is gitignored and is loaded automatically from the current
directory on every run - no need to `source` it or export anything
yourself. If a variable is already set in your real environment (e.g.
exported in your shell, injected by systemd's `EnvironmentFile`, or set as
a CI secret), that value wins and the `.env` file is ignored for it, so
it's safe to keep a `.env` around even in environments that also set these
some other way.

If you'd rather not use a file at all, exporting them directly works the
same way:

```bash
export OPENWEATHERMAP_API_KEY="..."
export TODOIST_API_TOKEN="..."
```

To use a `.env` file somewhere other than the default `./.env`, pass
`-env-file /path/to/file` or set `LAWNMOWER_ENV_FILE`. Unlike the default
path, an explicitly-specified one must exist or the app exits with an
error.

### 4. Build and run

```bash
go build -o lawnmower ./cmd/lawnmower

# See what it would do without touching Todoist:
./lawnmower -dry-run

# Do it for real:
./lawnmower
```

Every setting in `config.yaml` can also be overridden with an environment
variable (handy for cron/CI), e.g. `LAWNMOWER_MIN_DAYS_BETWEEN_MOWS=10`,
`LAWNMOWER_RAIN_THRESHOLD_PERCENT=25`, `LAWNMOWER_TODOIST_LABEL=yardwork`.
See `internal/config/config.go` for the full list.

## Scheduling

Run it once a day; it's a no-op most days (nothing new to schedule, or
something's already scheduled) and only creates a task when it finds a
good day within a currently-eligible window.

### cron

The default `.env` lookup is relative to the current working directory,
which cron doesn't set to your project directory for you - pass
`-env-file` with an absolute path (and lock the file down, e.g.
`chmod 600 /path/to/.env`) rather than relying on the default:

```
# Every day at 7am
0 7 * * * /path/to/lawnmower -config /path/to/config.yaml -env-file /path/to/.env >> /var/log/lawnmower.log 2>&1
```

### systemd timer

systemd's own `EnvironmentFile=` (shown below) works just as well as
`-env-file` here and needs no extra flag, since it sets real environment
variables before the process starts.

`/etc/systemd/system/lawnmower.service`:

```ini
[Unit]
Description=Check weather and schedule lawn mowing

[Service]
Type=oneshot
EnvironmentFile=/etc/lawnmower.env
ExecStart=/usr/local/bin/lawnmower -config /etc/lawnmower/config.yaml
```

`/etc/systemd/system/lawnmower.timer`:

```ini
[Unit]
Description=Run lawnmower daily

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

```bash
sudo systemctl enable --now lawnmower.timer
```

## Testing

```bash
go test ./...
```

`internal/planner` is pure logic with table-driven tests. `internal/weather`
and `internal/todoist` are tested against `httptest` mock servers that
assert the request shape and exercise response parsing, pagination, and
error handling, without hitting the real APIs.

## A note on the Todoist API

This app targets Todoist's current [unified API
v1](https://developer.todoist.com/api/v1/) (`api.todoist.com/api/v1/...`),
which replaced the older REST API v2 and Sync API v9 during 2026. If
Todoist changes response shapes again and requests start failing, the fix
is almost certainly confined to `internal/todoist/client.go` - run with
`-dry-run` first, and compare the error output (the client logs the raw
response body on unexpected status codes) against the current docs.

## Project layout

```
cmd/lawnmower/          CLI entry point and orchestration
internal/config/        YAML + env config loading and validation
internal/planner/       Pure date-selection logic (no I/O)
internal/weather/       OpenWeatherMap One Call 3.0 client
internal/todoist/       Todoist API v1 client
config.example.yaml     Copy to config.yaml and edit
```
