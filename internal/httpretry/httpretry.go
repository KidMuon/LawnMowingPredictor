// Package httpretry retries HTTP requests that fail with a temporary
// server error, so a blip at Todoist or OpenWeatherMap doesn't cost a
// whole run.
package httpretry

import (
	"fmt"
	"net/http"
	"time"
)

// Attempts is how many times a request is tried in total.
const Attempts = 3

// DefaultDelay is the wait between attempts when a client doesn't set one.
const DefaultDelay = 2 * time.Second

// Do sends req, retrying up to Attempts times in total when the request
// fails outright or the server answers with a 5xx status. It waits delay
// between attempts. The last response or error is returned as is.
func Do(client *http.Client, req *http.Request, delay time.Duration) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		resp, err := client.Do(req)
		if attempt == Attempts || !temporary(resp, err) || req.Context().Err() != nil {
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}

		select {
		case <-time.After(delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}

		if req.Body != nil {
			if req.GetBody == nil {
				return nil, fmt.Errorf("httpretry: cannot resend a %s %s request body", req.Method, req.URL.Path)
			}
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("httpretry: rewinding request body: %w", err)
			}
			req.Body = body
		}
	}
}

func temporary(resp *http.Response, err error) bool {
	return err != nil || resp.StatusCode >= 500
}
