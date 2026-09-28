package cli

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"patreon-posts/internal/api"
	"patreon-posts/internal/config"
	"patreon-posts/internal/db"
)

func TestWithRateLimitRetryPassesThroughOtherErrors(t *testing.T) {
	boom := errors.New("network is down")
	calls := 0

	_, err := withRateLimitRetry(Reporter{}, "t", func() (int, error) {
		calls++
		return 0, boom
	})

	if !errors.Is(err, boom) {
		t.Errorf("got %v, want the original error", err)
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1: only rate limits should be retried", calls)
	}
}

func TestWithRateLimitRetryAbortsOverCap(t *testing.T) {
	calls := 0

	// Waiting an hour is worse than failing, so this must not sleep at all.
	start := time.Now()
	_, err := withRateLimitRetry(Reporter{}, "t", func() (int, error) {
		calls++
		return 0, &api.RateLimitError{StatusCode: 429, RetryAfter: time.Hour}
	})

	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("called %d times, want 1", calls)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("waited %s; an over-cap Retry-After must abort immediately", elapsed)
	}
}

func TestWithRateLimitRetryHonoursRetryAfter(t *testing.T) {
	calls := 0

	start := time.Now()
	got, err := withRateLimitRetry(Reporter{}, "t", func() (int, error) {
		calls++
		if calls == 1 {
			return 0, &api.RateLimitError{StatusCode: 429, RetryAfter: time.Second}
		}
		return 42, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	if calls != 2 {
		t.Errorf("called %d times, want 2", calls)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("returned after %s; Retry-After was not respected", elapsed)
	}
}

func TestExtractFailsWhenACampaignFails(t *testing.T) {
	// Nothing listens on port 1, so the first page fetch fails at once.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	database, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfg := &config.Config{Campaigns: []config.Campaign{{ID: "1"}}}
	if err := ExtractYouTubeLinks(cfg, database, "", false, Reporter{}); err == nil {
		t.Fatal("got nil, want an error so the run is not recorded for --after last")
	}
}

func TestDedupe(t *testing.T) {
	seen := map[string]bool{}

	first := dedupe([]string{"a", "b", "a"}, seen)
	if len(first) != 2 {
		t.Errorf("got %v, want the duplicate dropped", first)
	}

	// The map carries across campaigns, so a repeat later is still a duplicate.
	if second := dedupe([]string{"b", "c"}, seen); len(second) != 1 || second[0] != "c" {
		t.Errorf("got %v, want only [c]", second)
	}
}
