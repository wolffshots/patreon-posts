package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"patreon-posts/internal/cli"
	"patreon-posts/internal/datetime"
	"patreon-posts/internal/db"
)

// liveLogLines is how much of a running list's log shows inside its group.
const liveLogLines = 6

// cursorStyle is selectedStyle without the padding, for rows that pad
// themselves. It is its own style because lipgloss 0.9 setters write through
// to the style they are called on, so selectedStyle.Padding(0) would strip
// the padding from every other user of selectedStyle.
var cursorStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#1a1a2e")).
	Background(accent).
	Bold(true)

// listsState is the Lists tab: every link extraction run, newest first, each a
// group holding the links it found.
type listsState struct {
	runs []db.LinkRun // from the database
	// live is the run this session started. It shows ahead of runs, and
	// replaces its own database copy, until a reload after it finishes.
	live    *db.LinkRun
	running bool
	log     []string // the tail of the live run's log
	status  string   // the live run's countdown line
	events  chan tea.Msg
	open    map[int64]bool // expanded groups, by run id
	cursor  int
	err     string
}

type runsLoadedMsg struct {
	runs []db.LinkRun
	err  error
}
type runStartedMsg db.LinkRun
type runLinkMsg struct {
	runID int64
	link  db.RunLink
}
type runLogMsg string
type runStatusMsg string
type runDoneMsg struct{ err error }

type rowKind int

const (
	rowHeader rowKind = iota
	rowLink
	rowNote // a log line, the countdown, or "no links"
)

// listRow is one visible line of the tab.
type listRow struct {
	kind rowKind
	run  *db.LinkRun
	link int    // index into run.Links, for rowLink
	text string // for rowNote
}

func (m Model) loadRuns() tea.Cmd {
	database := m.database
	return func() tea.Msg {
		if database == nil {
			return runsLoadedMsg{}
		}
		runs, err := database.ListLinkRuns()
		return runsLoadedMsg{runs: runs, err: err}
	}
}

func waitRun(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// startRun does what --extract-links --after last does, in the background. Its
// progress comes back one message per waitRun.
func (m Model) startRun() (Model, tea.Cmd) {
	if m.lists.running {
		m.statusMessage = "A list is already being made"
		return m, nil
	}
	ch := make(chan tea.Msg, 64)
	m.lists.events = ch
	m.lists.running = true
	m.lists.err = ""
	m.lists.log = nil
	m.lists.status = ""

	cfg, database := m.cfg, m.database
	go func() {
		rep := cli.Reporter{
			Log:    func(text string) { ch <- runLogMsg(text) },
			Status: func(line string) { ch <- runStatusMsg(line) },
			Start:  func(run db.LinkRun) { ch <- runStartedMsg(run) },
			Link:   func(id int64, l db.RunLink) { ch <- runLinkMsg{runID: id, link: l} },
		}
		err := cli.ExtractYouTubeLinks(cfg, database, "last", false, rep)
		if err == nil {
			// Record the run where "last" looks, so the next list and the CLI
			// both start from here.
			err = database.SaveLastRun(time.Now(), []string{"--after=last", "--extract-links", "--tui"})
		}
		ch <- runDoneMsg{err: err}
	}()
	return m, tea.Batch(waitRun(ch), m.spinner.Tick)
}

func (m Model) updateLists(msg tea.Msg) (tea.Model, tea.Cmd) {
	l := &m.lists
	switch msg := msg.(type) {
	case runsLoadedMsg:
		if msg.err != nil {
			l.err = msg.err.Error()
			return m, nil
		}
		l.runs = msg.runs
		if l.live != nil && !l.running {
			for _, r := range l.runs {
				if r.ID == l.live.ID {
					l.live = nil
					break
				}
			}
		}
		// Open the newest list the first time, since it is the one you came for.
		if len(l.open) == 0 && len(l.runs) > 0 {
			l.open[l.runs[0].ID] = true
		}
		return m, nil

	case runStartedMsg:
		run := db.LinkRun(msg)
		l.live = &run
		l.open[run.ID] = true
		l.cursor = 0 // the new group heads the list

	case runLinkMsg:
		if l.live != nil && l.live.ID == msg.runID {
			l.live.Links = append(l.live.Links, msg.link)
		}

	case runLogMsg:
		for _, line := range strings.Split(string(msg), "\n") {
			if strings.TrimSpace(line) != "" {
				l.log = append(l.log, line)
			}
		}
		if len(l.log) > liveLogLines {
			l.log = l.log[len(l.log)-liveLogLines:]
		}
		l.status = ""

	case runStatusMsg:
		l.status = string(msg)

	case runDoneMsg:
		l.running = false
		l.status = ""
		if l.live != nil {
			l.live.FinishedAt = time.Now()
		}
		if msg.err != nil {
			l.err = msg.err.Error()
			m.statusMessage = "✗ List failed"
		} else if l.live != nil {
			m.statusMessage = fmt.Sprintf("✓ List finished with %d links", len(l.live.Links))
		}
		return m, m.loadRuns()
	}
	return m, waitRun(l.events)
}

// groups is every run in display order, the live one first.
func (l listsState) groups() []*db.LinkRun {
	var out []*db.LinkRun
	if l.live != nil {
		out = append(out, l.live)
	}
	for i := range l.runs {
		if l.live == nil || l.runs[i].ID != l.live.ID {
			out = append(out, &l.runs[i])
		}
	}
	return out
}

func (l listsState) rows() []listRow {
	var rows []listRow
	for _, run := range l.groups() {
		rows = append(rows, listRow{kind: rowHeader, run: run})
		if !l.open[run.ID] {
			continue
		}
		for i := range run.Links {
			rows = append(rows, listRow{kind: rowLink, run: run, link: i})
		}
		if run == l.live && l.running {
			for _, line := range l.log {
				rows = append(rows, listRow{kind: rowNote, run: run, text: line})
			}
			if l.status != "" {
				rows = append(rows, listRow{kind: rowNote, run: run, text: l.status})
			}
		} else if len(run.Links) == 0 {
			rows = append(rows, listRow{kind: rowNote, run: run, text: "no links"})
		}
	}
	return rows
}

func (m Model) handleListsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	l := &m.lists
	rows := l.rows()
	l.cursor = min(max(l.cursor, 0), max(len(rows)-1, 0))

	switch {
	case key.Matches(msg, m.keys.Up):
		if l.cursor > 0 {
			l.cursor--
		}
	case key.Matches(msg, m.keys.Down):
		if l.cursor < len(rows)-1 {
			l.cursor++
		}
	case key.Matches(msg, m.keys.Toggle):
		if len(rows) == 0 {
			return m, nil
		}
		run := rows[l.cursor].run
		l.open[run.ID] = !l.open[run.ID]
		// Put the cursor on the group, since the row it was on may be gone.
		for i, r := range l.rows() {
			if r.kind == rowHeader && r.run == run {
				l.cursor = i
				break
			}
		}
	case key.Matches(msg, m.keys.ListAdd):
		if len(rows) == 0 {
			return m, nil
		}
		row := rows[l.cursor]
		var links []string
		if row.kind == rowLink {
			links = []string{row.run.Links[row.link].URL}
		} else {
			for _, link := range row.run.Links {
				links = append(links, link.URL)
			}
		}
		if len(links) == 0 {
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("✓ Added %d of %d links to clipboard", m.addToClipboard(links...), len(links))
	case key.Matches(msg, m.keys.NewList):
		return m.startRun()
	}
	return m, nil
}

func (m Model) viewLists(w, h int) string {
	l := m.lists
	title := titleStyle.Render("Lists") + dimStyle.Render(fmt.Sprintf(" %d", len(l.groups())))
	inner := w - frameCols

	var lines []string
	if l.err != "" {
		lines = append(lines, errorStyle.Render("✗ "+l.err), "")
	}
	rows := l.rows()
	if len(rows) == 0 {
		lines = append(lines, dimStyle.Render("No lists yet. Press n to extract links posted since the last run."))
		return box(title, strings.Join(lines, "\n"), w, h)
	}

	// Window the rows around the cursor, keeping a line for the scroll marker.
	visible := max(h-frameRows-len(lines)-1, 1)
	cursor := min(l.cursor, len(rows)-1)
	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}
	end := min(start+visible, len(rows))
	for i := start; i < end; i++ {
		lines = append(lines, m.renderListRow(rows[i], i == cursor, inner))
	}
	if len(rows) > visible {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  %d-%d of %d", start+1, end, len(rows))))
	}
	return box(title, strings.Join(lines, "\n"), w, h)
}

func (m Model) renderListRow(row listRow, selected bool, width int) string {
	var plain, styled string
	switch row.kind {
	case rowHeader:
		run := row.run
		arrow := "▸"
		if m.lists.open[run.ID] {
			arrow = "▾"
		}
		started := datetime.FormatLocal(run.StartedAt)
		after := "all posts"
		if run.After != "" {
			after = "after " + run.After
		}
		count := fmt.Sprintf("%d links", len(run.Links))
		if len(run.Links) == 1 {
			count = "1 link"
		}
		var state, stateStyled string
		switch {
		case run == m.lists.live && m.lists.running:
			state = "running"
			stateStyled = m.spinner.View() + " " + state
		case run.Error != "":
			state = "⚠ " + run.Error
			stateStyled = warnStyle.Render(state)
		case run.FinishedAt.IsZero():
			// The process died mid-run. The links it saved are all there is.
			state = "interrupted"
			stateStyled = warnStyle.Render(state)
		}
		plain = fmt.Sprintf("%s %s  %s · %s  %s", arrow, started, after, count, state)
		styled = arrow + " " + titleStyle.Render(started) + dimStyle.Render("  "+after+" · "+count) + "  " + stateStyled

	case rowLink:
		link := row.run.Links[row.link]
		mark := " "
		for _, c := range m.clipboardLinks {
			if c == link.URL {
				mark = "✓"
				break
			}
		}
		plain = fmt.Sprintf("  %s %s  %s", mark, link.URL, link.PostTitle)
		styled = "  " + cachedStyle.Render(mark) + " " + urlStyle.Render(link.URL) + "  " + dimStyle.Render(link.PostTitle)

	case rowNote:
		plain = "    " + row.text
		styled = dimStyle.Render(plain)
	}

	if selected {
		// Restyle the whole row rather than wrapping the styled one, so the
		// cursor background is not broken up by the cell colours underneath.
		return cursorStyle.Render(pad(plain, width))
	}
	return styled
}
