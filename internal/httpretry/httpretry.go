// Package httpretry retries HTTP requests that fail with a temporary
// server error, so a blip at Todoist or OpenWeatherMap doesn't cost a
// whole run.
package httpretry

import (
	"fmt"
	"net/http"
	"time"
)

// attempts is how many times a request is tried in total.
const attempts = 3

// DefaultDelay is the wait between attempts when Do is given zero.
const DefaultDelay = 2 * time.Second

// Do sends req, retrying up to attempts times in total when the request
// fails outright or the server answers with a 5xx status. It waits delay
// between attempts, or DefaultDelay if delay is zero. The last response or
// error is returned as is.
func Do(client *http.Client, req *http.Request, delay time.Duration) (*http.Response, error) {
	if delay == 0 {
		delay = DefaultDelay
	}
	for attempt := 1; ; attempt++ {
		resp, err := client.Do(req)
		if attempt == attempts || !temporary(resp, err) || req.Context().Err() != nil {
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
