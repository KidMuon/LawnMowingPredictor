// Package todoist is a minimal client for the parts of Todoist's unified
// API v1 (https://developer.todoist.com/api/v1/) this application needs:
// finding the most recently completed mowing task, finding the open one,
// and creating or moving it.
//
// Note: Todoist retired its old REST API v2 and Sync API v9 in favor of
// this unified v1 API during 2026. If Todoist changes response shapes
// again in the future and requests here start failing, the fix is almost
// certainly confined to this file — check developer.todoist.com/api/v1
// against the request/response shapes below.
package todoist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/KidMuon/LawnMowingPredictor/internal/httpretry"
)

const defaultBaseURL = "https://api.todoist.com/api/v1"

// Client talks to the Todoist API.
type Client struct {
	// Token is a Todoist personal API token, sent as a Bearer token.
	Token      string
	HTTPClient *http.Client
	// BaseURL overrides the API endpoint; used by tests. Defaults to
	// Todoist's production unified API v1 endpoint.
	BaseURL string
	// RetryDelay is the wait between retries of a temporary server
	// error. Defaults to httpretry.DefaultDelay.
	RetryDelay time.Duration
}

// NewClient returns a Client ready to make requests.
func NewClient(token string) *Client {
	return &Client{
		Token:      token,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Due mirrors Todoist's "due" object on a task.
type Due struct {
	Date        string `json:"date"`
	Datetime    string `json:"datetime,omitempty"`
	String      string `json:"string,omitempty"`
	IsRecurring bool   `json:"is_recurring,omitempty"`
}

// Task mirrors the fields this package needs from a Todoist task object.
// The same shape is used for both active and completed tasks; CompletedAt
// is only populated on responses from the completed-tasks endpoints.
type Task struct {
	ID          string   `json:"id"`
	Content     string   `json:"content"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	Priority    int      `json:"priority"`
	ProjectID   string   `json:"project_id"`
	Due         *Due     `json:"due"`
	CompletedAt string   `json:"completed_at,omitempty"`
}

// pagedTasksResponse mirrors the cursor-paginated list shape used by the
// v1 API's task-listing endpoints: {"results": [...], "next_cursor": ...}.
type pagedTasksResponse struct {
	Results    []Task  `json:"results"`
	NextCursor *string `json:"next_cursor"`
}

// FindLatestCompletedByLabel searches Todoist's completed-task history,
// going back up to lookback from now, for tasks carrying label. It returns
// the Mow Date of the most recently completed matching task: the calendar
// date in loc (the lawn's timezone) it was ticked off, as UTC midnight. It
// returns nil if none was found in that window.
func (c *Client) FindLatestCompletedByLabel(ctx context.Context, label string, lookback time.Duration, loc *time.Location) (*time.Time, error) {
	until := time.Now().UTC()
	since := until.Add(-lookback)

	q := url.Values{}
	q.Set("since", since.Format("2006-01-02T15:04:05"))
	q.Set("until", until.Format("2006-01-02T15:04:05"))
	// filter_query uses Todoist's normal filter syntax; "@label" restricts
	// to tasks carrying that label. We also re-check the label client-side
	// below in case filter_query behaves differently than expected on this
	// endpoint - that check is what actually enforces correctness.
	q.Set("filter_query", "@"+label)

	tasks, err := c.listTasksPaged(ctx, "/tasks/completed/by_completion_date", q)
	if err != nil {
		return nil, err
	}

	var latest *time.Time
	for _, t := range tasks {
		if !hasLabel(t.Labels, label) {
			continue
		}
		when, ok := t.completionOrDueDate(loc)
		if !ok {
			continue
		}
		if latest == nil || when.After(*latest) {
			w := when
			latest = &w
		}
	}
	return latest, nil
}

// FindScheduledMow returns the Scheduled Mow: the open task carrying label
// with the earliest due date, whether or not it's overdue. Tasks with no
// due date come last. It returns nil if there's no open labelled task.
func (c *Client) FindScheduledMow(ctx context.Context, label string) (*Task, error) {
	q := url.Values{}
	q.Set("label", label)

	tasks, err := c.listTasksPaged(ctx, "/tasks", q)
	if err != nil {
		return nil, err
	}

	var earliest *Task
	for i := range tasks {
		t := &tasks[i]
		if !hasLabel(t.Labels, label) {
			continue
		}
		if earliest == nil || dueBefore(t, earliest) {
			earliest = t
		}
	}
	return earliest, nil
}

// DueDate returns the task's due date as UTC midnight, and false if it has
// no (parseable) due date.
func (t *Task) DueDate() (time.Time, bool) {
	if t.Due == nil || t.Due.Date == "" {
		return time.Time{}, false
	}
	due, err := time.Parse("2006-01-02", t.Due.Date)
	return due, err == nil
}

// dueBefore reports whether a is due before b, with undated tasks last.
func dueBefore(a, b *Task) bool {
	aDue, aOK := a.DueDate()
	bDue, bOK := b.DueDate()
	if !aOK || !bOK {
		return aOK && !bOK
	}
	return aDue.Before(bDue)
}

// createTaskRequest is the POST /tasks body.
type createTaskRequest struct {
	Content     string   `json:"content"`
	Description string   `json:"description,omitempty"`
	ProjectID   string   `json:"project_id,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	DueDate     string   `json:"due_date,omitempty"`
}

// CreateTask creates a new Todoist task due on dueDate, carrying label,
// optionally placed in projectID (leave empty for the Inbox).
func (c *Client) CreateTask(ctx context.Context, content, description string, dueDate time.Time, label, projectID string) (*Task, error) {
	body := createTaskRequest{
		Content:     content,
		Description: description,
		ProjectID:   projectID,
		Labels:      []string{label},
		DueDate:     dueDate.Format("2006-01-02"),
	}
	// Never retried: Todoist may have created the task before failing, and
	// a retry would make a second Scheduled Mow. The next run finds it.
	return c.postTask(ctx, "/tasks", body, false)
}

// moveTaskRequest is the POST /tasks/{id} body.
type moveTaskRequest struct {
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
}

// MoveTask changes the due date and description of the task with id.
func (c *Client) MoveTask(ctx context.Context, id string, dueDate time.Time, description string) (*Task, error) {
	body := moveTaskRequest{
		Description: description,
		DueDate:     dueDate.Format("2006-01-02"),
	}
	return c.postTask(ctx, "/tasks/"+url.PathEscape(id), body, true)
}

// postTask POSTs body as JSON to path and parses the task it returns. It
// retries temporary server errors only if retry is true.
func (c *Client) postTask(ctx context.Context, path string, body any, retry bool) (*Task, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("todoist: encoding POST %s request: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("todoist: building POST %s request: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	respBody, status, err := c.do(req, retry)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, fmt.Errorf("todoist: POST %s: unexpected status %d: %s", path, status, truncate(string(respBody), 500))
	}

	var task Task
	if err := json.Unmarshal(respBody, &task); err != nil {
		return nil, fmt.Errorf("todoist: parsing POST %s response: %w", path, err)
	}
	return &task, nil
}

// listTasksPaged GETs path with query, following cursor-based pagination
// until next_cursor is empty, and returns every task encountered.
func (c *Client) listTasksPaged(ctx context.Context, path string, query url.Values) ([]Task, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("todoist: no API token configured")
	}

	var all []Task
	cursor := ""
	for {
		q := cloneValues(query)
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		if q.Get("limit") == "" {
			q.Set("limit", "200")
		}

		reqURL := c.baseURL() + path + "?" + q.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, fmt.Errorf("todoist: building request for %s: %w", path, err)
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)

		body, status, err := c.do(req, true)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("todoist: GET %s: unexpected status %d: %s", path, status, truncate(string(body), 500))
		}

		var page pagedTasksResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("todoist: parsing response from %s: %w", path, err)
		}
		all = append(all, page.Results...)

		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
	}
	return all, nil
}

// do sends req, retrying temporary server errors if retry is true.
func (c *Client) do(req *http.Request, retry bool) ([]byte, int, error) {
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	var resp *http.Response
	var err error
	if retry {
		resp, err = httpretry.Do(httpClient, req, c.RetryDelay)
	} else {
		resp, err = httpClient.Do(req)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("todoist: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("todoist: reading response: %w", err)
	}
	return body, resp.StatusCode, nil
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

// completionOrDueDate returns the best available date representing when a
// completed task was done: the date in loc of its completed_at timestamp
// if present and parseable, otherwise its due date.
func (t Task) completionOrDueDate(loc *time.Location) (time.Time, bool) {
	if t.CompletedAt != "" {
		if ts, err := parseTodoistTimestamp(t.CompletedAt); err == nil {
			return truncateToDate(ts.In(loc)), true
		}
	}
	if t.Due != nil && t.Due.Date != "" {
		if ts, err := time.Parse("2006-01-02", t.Due.Date); err == nil {
			return truncateToDate(ts), true
		}
	}
	return time.Time{}, false
}

func parseTodoistTimestamp(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.999999Z",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	var lastErr error
	for _, layout := range layouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

func truncateToDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
