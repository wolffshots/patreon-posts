package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"seconds", "120", 2 * time.Minute},
		{"zero", "0", 0},
		{"negative", "-5", 0},
		{"empty", "", 0},
		{"garbage", "soon", 0},
		{"http date", now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second},
		{"http date in the past", now.Add(-time.Hour).Format(http.TimeFormat), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.value, now); got != tt.want {
				t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// serve stands up a fake Patreon returning one canned response.
func serve(t *testing.T, status int, headers map[string]string, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := NewClient("session_id=test")
	c.baseURL = srv.URL
	return c
}

func TestDoRequestRateLimit(t *testing.T) {
	c := serve(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "45"}, "slow down")

	_, err := c.doRequest(c.baseURL)

	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("got %v, want a RateLimitError", err)
	}
	if rl.RetryAfter != 45*time.Second {
		t.Errorf("RetryAfter = %v, want 45s", rl.RetryAfter)
	}
	if rl.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d, want 429", rl.StatusCode)
	}
}

func TestDoRequestRateLimitWithoutHeader(t *testing.T) {
	c := serve(t, http.StatusServiceUnavailable, nil, "unavailable")

	_, err := c.doRequest(c.baseURL)

	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("got %v, want a RateLimitError", err)
	}
	if rl.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 so the caller picks its own backoff", rl.RetryAfter)
	}
}

// A rate limit page that happens to mention "session" or "login" used to be
// misreported as an auth failure, which aborted the run instead of backing off.
func TestRateLimitBodyIsNotMistakenForAuthFailure(t *testing.T) {
	body := "Your session was throttled. Please log in again later."
	c := serve(t, http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, body)

	_, err := c.doRequest(c.baseURL)

	if errors.Is(err, ErrAuthRequired) {
		t.Fatal("a 429 must not be reported as an auth error")
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("got %v, want a RateLimitError", err)
	}
}

// Likewise a 500 whose body mentions cookies is a server error, not a dead session.
func TestServerErrorIsNotMistakenForAuthFailure(t *testing.T) {
	c := serve(t, http.StatusInternalServerError, nil, "cookie handling failed during authentication")

	_, err := c.doRequest(c.baseURL)

	if errors.Is(err, ErrAuthRequired) {
		t.Fatal("a 500 must not be reported as an auth error")
	}
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestAuthErrorStatuses(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		c := serve(t, status, nil, "nope")
		if _, err := c.doRequest(c.baseURL); !errors.Is(err, ErrAuthRequired) {
			t.Errorf("status %d: got %v, want ErrAuthRequired", status, err)
		}
	}
}

func TestTruncateBodyKeepsErrorsReadable(t *testing.T) {
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'x'
	}
	if got := truncateBody(long); len(got) > 210 {
		t.Errorf("truncateBody returned %d chars, want it clipped", len(got))
	}
}
