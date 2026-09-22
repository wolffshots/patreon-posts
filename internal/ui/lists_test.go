package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"patreon-posts/internal/config"
	"patreon-posts/internal/db"
)

// TestLiveRunFlow walks a run's messages through the Lists tab: the group
// appears on start, fills as links arrive, and hands over to its database copy
// once a reload after the finish brings it back.
func TestLiveRunFlow(t *testing.T) {
	m := NewModel(&config.Config{}, nil, "")
	m.tab = tabLists
	m.lists.running = true
	m.lists.events = make(chan tea.Msg, 1)

	step := func(msg tea.Msg) {
		next, _ := m.Update(msg)
		m = next.(Model)
	}

	run := db.LinkRun{ID: 7, StartedAt: time.Now(), After: "2026-09-22 08:38:25"}
	step(runStartedMsg(run))
	step(runLinkMsg{runID: 7, link: db.RunLink{URL: "https://youtu.be/a", PostTitle: "First"}})
	step(runLogMsg("[campaign 1/1] Test\n\n"))

	rows := m.lists.rows()
	// Header, the link, and the one non-blank log line.
	if len(rows) != 3 || rows[1].kind != rowLink || rows[2].text != "[campaign 1/1] Test" {
		t.Fatalf("live group rows wrong: %+v", rows)
	}
	if view := m.View(); !strings.Contains(view, "https://youtu.be/a") {
		t.Errorf("view does not show the live link:\n%s", view)
	}

	step(runDoneMsg{})
	if m.lists.running || m.lists.live == nil {
		t.Fatal("the finished run should stay on show until the reload replaces it")
	}

	saved := run
	saved.FinishedAt = time.Now()
	saved.Links = []db.RunLink{{URL: "https://youtu.be/a"}}
	step(runsLoadedMsg{runs: []db.LinkRun{saved}})
	if m.lists.live != nil || len(m.lists.groups()) != 1 {
		t.Fatalf("reload should replace the live run, got %d groups", len(m.lists.groups()))
	}

	// a on the group header adds every link in it.
	m.lists.cursor = 0
	step(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if len(m.clipboardLinks) != 1 || m.clipboardLinks[0] != "https://youtu.be/a" {
		t.Errorf("clipboard = %v, want the group's link", m.clipboardLinks)
	}
}
