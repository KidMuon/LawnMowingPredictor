package todoist

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFindLatestCompletedByLabel(t *testing.T) {
	// Two pages, exercising cursor pagination. Only tasks with the
	// requested label should count; the most recent completed_at wins.
	page1 := pagedTasksResponse{
		Results: []Task{
			{ID: "1", Content: "Mow the lawn", Labels: []string{"lawn-mowing"}, CompletedAt: "2026-09-01T14:00:00Z"},
			{ID: "2", Content: "Unrelated task", Labels: []string{"other"}, CompletedAt: "2026-09-10T14:00:00Z"},
		},
	}
	cursor := "abc123"
	page1.NextCursor = &cursor
	page2 := pagedTasksResponse{
		Results: []Task{
			{ID: "3", Content: "Mow the lawn", Labels: []string{"lawn-mowing"}, CompletedAt: "2026-09-08T09:00:00Z"},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/completed/by_completion_date" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q", got)
		}
		if r.URL.Query().Get("filter_query") != "@lawn-mowing" {
			t.Errorf("filter_query = %q", r.URL.Query().Get("filter_query"))
		}

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "abc123" {
			json.NewEncoder(w).Encode(page2)
			return
		}
		json.NewEncoder(w).Encode(page1)
	}))
	defer server.Close()

	c := NewClient("test-token")
	c.BaseURL = server.URL

	got, err := c.FindLatestCompletedByLabel(context.Background(), "lawn-mowing", 90*24*time.Hour, time.UTC)
	if err != nil {
		t.Fatalf("FindLatestCompletedByLabel() error = %v", err)
	}
	if got == nil {
		t.Fatalf("expected a date, got nil")
	}
	// Task 2 (2026-09-10) is unlabeled and must be ignored. Among the
	// labeled tasks, task 3 (2026-09-08) is more recent than task 1
	// (2026-09-01).
	want := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindLatestCompletedByLabel_NoMatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(pagedTasksResponse{})
	}))
	defer server.Close()

	c := NewClient("test-token")
	c.BaseURL = server.URL

	got, err := c.FindLatestCompletedByLabel(context.Background(), "lawn-mowing", 90*24*time.Hour, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestFindScheduledMow(t *testing.T) {
	tests := []struct {
		name   string
		tasks  []Task
		wantID string // "" means none
	}{
		{
			name: "finds the open task with the label",
			tasks: []Task{
				{ID: "10", Labels: []string{"other"}, Due: &Due{Date: "2026-09-20"}},
				{ID: "11", Labels: []string{"lawn-mowing"}, Due: &Due{Date: "2026-09-28"}},
			},
			wantID: "11",
		},
		{
			name: "an overdue task still counts",
			tasks: []Task{
				{ID: "12", Labels: []string{"lawn-mowing"}, Due: &Due{Date: "2026-09-01"}},
			},
			wantID: "12",
		},
		{
			name: "with several, the earliest due wins",
			tasks: []Task{
				{ID: "13", Labels: []string{"lawn-mowing"}},
				{ID: "14", Labels: []string{"lawn-mowing"}, Due: &Due{Date: "2026-09-28"}},
				{ID: "15", Labels: []string{"lawn-mowing"}, Due: &Due{Date: "2026-09-25"}},
			},
			wantID: "15",
		},
		{
			name:  "no tasks at all",
			tasks: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("label") != "lawn-mowing" {
					t.Errorf("label query param = %q", r.URL.Query().Get("label"))
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(pagedTasksResponse{Results: tc.tasks})
			}))
			defer server.Close()

			c := NewClient("test-token")
			c.BaseURL = server.URL

			got, err := c.FindScheduledMow(context.Background(), "lawn-mowing")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			switch {
			case tc.wantID == "" && got != nil:
				t.Fatalf("expected no task, got %+v", got)
			case tc.wantID != "" && got == nil:
				t.Fatalf("expected task %s, got nil", tc.wantID)
			case tc.wantID != "" && got.ID != tc.wantID:
				t.Errorf("got ID %q, want %q", got.ID, tc.wantID)
			}
		})
	}
}

func TestCreateTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/tasks" {
			t.Errorf("path = %q, want /tasks", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}

		var body createTaskRequest
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("bad request body: %v", err)
		}
		if body.Content != "Mow the lawn" {
			t.Errorf("content = %q", body.Content)
		}
		if body.DueDate != "2026-09-25" {
			t.Errorf("due_date = %q", body.DueDate)
		}
		if len(body.Labels) != 1 || body.Labels[0] != "lawn-mowing" {
			t.Errorf("labels = %v", body.Labels)
		}
		if body.ProjectID != "999" {
			t.Errorf("project_id = %q", body.ProjectID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Task{
			ID:      "42",
			Content: body.Content,
			Labels:  body.Labels,
			Due:     &Due{Date: body.DueDate},
		})
	}))
	defer server.Close()

	c := NewClient("test-token")
	c.BaseURL = server.URL

	due := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	task, err := c.CreateTask(context.Background(), "Mow the lawn", "some description", due, "lawn-mowing", "999")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ID != "42" {
		t.Errorf("task.ID = %q, want 42", task.ID)
	}
}

func TestCreateTask_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"invalid token"}`))
	}))
	defer server.Close()

	c := NewClient("bad-token")
	c.BaseURL = server.URL

	_, err := c.CreateTask(context.Background(), "Mow the lawn", "", time.Now(), "lawn-mowing", "")
	if err == nil {
		t.Fatalf("expected error for 403 response")
	}
}

func TestNoTokenConfigured(t *testing.T) {
	c := NewClient("")
	_, err := c.FindScheduledMow(context.Background(), "lawn-mowing")
	if err == nil {
		t.Fatalf("expected error for missing token")
	}
}

func TestFindLatestCompletedByLabel_UsesTheLawnsLocalDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(pagedTasksResponse{Results: []Task{
			// 21:30 on 09-22 in New York is already 09-23 in UTC.
			{ID: "1", Labels: []string{"lawn-mowing"}, CompletedAt: "2026-09-23T01:30:00Z"},
		}})
	}))
	defer server.Close()
	c := NewClient("test-token")
	c.BaseURL = server.URL
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("loading timezone: %v", err)
	}

	got, err := c.FindLatestCompletedByLabel(context.Background(), "lawn-mowing", 90*24*time.Hour, newYork)
	if err != nil {
		t.Fatalf("FindLatestCompletedByLabel() error = %v", err)
	}
	if want := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC); got == nil || !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestMoveTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/tasks/42" {
			t.Errorf("request = %s %s, want POST /tasks/42", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if body["due_date"] != "2026-09-25" {
			t.Errorf("due_date = %q, want 2026-09-25", body["due_date"])
		}
		if body["description"] != "new description" {
			t.Errorf("description = %q, want %q", body["description"], "new description")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": "42", "due": {"date": "2026-09-25"}}`))
	}))
	defer server.Close()
	c := NewClient("test-token")
	c.BaseURL = server.URL

	task, err := c.MoveTask(context.Background(), "42", time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), "new description")
	if err != nil {
		t.Fatalf("MoveTask() error = %v", err)
	}
	if task.Due == nil || task.Due.Date != "2026-09-25" {
		t.Errorf("returned task due = %+v, want 2026-09-25", task.Due)
	}
}

func TestRetriesATemporaryServerError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(pagedTasksResponse{Results: []Task{
			{ID: "7", Labels: []string{"lawn-mowing"}, Due: &Due{Date: "2026-09-25"}},
		}})
	}))
	defer server.Close()
	c := NewClient("test-token")
	c.BaseURL = server.URL
	c.RetryDelay = time.Millisecond

	got, err := c.FindScheduledMow(context.Background(), "lawn-mowing")
	if err != nil {
		t.Fatalf("FindScheduledMow() error = %v", err)
	}
	if got == nil || got.ID != "7" {
		t.Errorf("got %+v, want task 7", got)
	}
}

func TestRetriesResendTheRequestBody(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if calls == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if len(body) == 0 {
			t.Errorf("retried request had an empty body")
		}
		w.Write([]byte(`{"id": "9"}`))
	}))
	defer server.Close()
	c := NewClient("test-token")
	c.BaseURL = server.URL
	c.RetryDelay = time.Millisecond

	task, err := c.CreateTask(context.Background(), "Mow the lawn", "", time.Now(), "lawn-mowing", "")
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ID != "9" {
		t.Errorf("task ID = %q, want 9", task.ID)
	}
}
