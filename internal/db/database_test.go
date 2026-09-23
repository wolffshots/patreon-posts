package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLinkRunsRoundTrip(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	start := time.Date(2026, 9, 22, 18, 41, 54, 0, time.Local)
	// Insert the newer run first, so the order must come from the start
	// time rather than the id.
	newer, err := d.StartLinkRun(start, "2026-09-22 08:38:25")
	older, _ := d.StartLinkRun(start.Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	for i, url := range []string{"https://a", "https://b"} {
		if err := d.AddRunLink(newer, i, RunLink{URL: url, PostID: "p", PostTitle: "t", PublishedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	d.FinishLinkRun(newer, start.Add(time.Minute), "")

	runs, err := d.ListLinkRuns()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].ID != newer || runs[1].ID != older {
		t.Fatalf("got %+v, want the newer run first", runs)
	}
	got := runs[0]
	if !got.StartedAt.Equal(start) || got.After != "2026-09-22 08:38:25" || got.FinishedAt.IsZero() {
		t.Errorf("run fields did not survive: %+v", got)
	}
	if len(got.Links) != 2 || got.Links[0].URL != "https://a" || got.Links[1].URL != "https://b" {
		t.Errorf("links out of order or missing: %+v", got.Links)
	}
	// A run with no finish time is one that died; the list must say so.
	if !runs[1].FinishedAt.IsZero() || len(runs[1].Links) != 0 {
		t.Errorf("unfinished run came back wrong: %+v", runs[1])
	}
}
