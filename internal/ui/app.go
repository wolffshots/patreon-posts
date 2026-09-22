package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"patreon-posts/internal/api"
	"patreon-posts/internal/config"
	"patreon-posts/internal/datetime"
	"patreon-posts/internal/db"
	"patreon-posts/internal/models"
)

// Styles
var (
	accent = lipgloss.Color("#FF424D")
	dim    = lipgloss.Color("#666680")

	titleStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimStyle   = lipgloss.NewStyle().Foreground(dim)
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))

	tabActiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1a2e")).
			Background(accent).
			Bold(true).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().Foreground(dim).Padding(0, 2)
	tabBarStyle      = lipgloss.NewStyle().Padding(0, 0, 1, 0)

	footerStyle = lipgloss.NewStyle().
			Foreground(dim).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#3d3d5c"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00D4AA")).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#3d3d5c"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1a2e")).
			Background(accent).
			Bold(true).
			Padding(0, 1)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e0e0e0")).
			Padding(0, 1)

	canViewStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D4AA")).
			Bold(true)

	cannotViewStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6b6b")).
			Bold(true)

	typeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9d8cff")).
			Italic(true)

	urlStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5c9eff")).
			Underline(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6b6b")).
			Bold(true)

	cachedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D4AA")).
			Bold(true)

	notCachedStyle = lipgloss.NewStyle().
			Foreground(dim)

	youtubeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	descriptionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#b0b0b0")).
				PaddingLeft(2)

	clipboardLinkStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5c9eff"))

	clipboardSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1a2e")).
				Background(lipgloss.Color("#5c9eff")).
				Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D4AA")).
			Bold(true)

	linkSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1a2e")).
				Background(lipgloss.Color("#FF0000")).
				Bold(true)
)

// Tabs
type tab int

const (
	tabPosts tab = iota
	tabLists
)

var tabNames = []string{"Posts", "Lists"}

// View states of the Posts tab
type viewState int

const (
	stateInput viewState = iota
	stateLoading
	stateList
	stateDetails
	stateError
	stateRateLimited
)

// Rate limit policy for the TUI. It retries fewer times than the CLI because
// somebody is sitting there watching it wait.
const (
	uiRateLimitMaxAttempts = 3
	uiRateLimitMaxWait     = 5 * time.Minute
	uiRateLimitBaseWait    = 60 * time.Second
)

const (
	clipboardPanelWidth = 45
	// wideLayout is the narrowest terminal that still fits the clipboard frame
	// beside a usable main frame.
	wideLayout = 100
)

// ---- Keys ---------------------------------------------------------------

type keyMap struct {
	PostsTab, ListsTab, Help, Quit key.Binding

	Up, Down key.Binding

	Copy, Remove, Clear, ClipUp, ClipDown key.Binding

	// Campaign selection
	Select, NewCampaign, Filter, Delete, Exit key.Binding
	// Text entry
	Confirm, Cancel key.Binding
	// Posts list
	Open, NextPage, PrevPage, Refresh, ForceRefresh, Back key.Binding
	// Post details
	AddLink, AddAll, PageUp, PageDown key.Binding
	// Error and rate limit
	Retry, CancelWait key.Binding
	// Lists
	Toggle, ListAdd, NewList key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		PostsTab: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "posts")),
		ListsTab: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "lists")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),

		Up:   key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down: key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),

		Copy:     key.NewBinding(key.WithKeys("c", "y"), key.WithHelp("c/y", "copy clipboard")),
		Remove:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove link")),
		Clear:    key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "clear clipboard")),
		ClipUp:   key.NewBinding(key.WithKeys("["), key.WithHelp("[", "clipboard up")),
		ClipDown: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "clipboard down")),

		Select:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "load posts")),
		NewCampaign: key.NewBinding(key.WithKeys("n", "a"), key.WithHelp("n/a", "new campaign")),
		Filter:      key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "date filter")),
		Delete:      key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete")),
		Exit:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit")),

		Confirm: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),

		Open:         key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view post")),
		NextPage:     key.NewBinding(key.WithKeys("n", "l", "right"), key.WithHelp("n/→", "next page")),
		PrevPage:     key.NewBinding(key.WithKeys("p", "h", "left"), key.WithHelp("p/←", "prev page")),
		Refresh:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		ForceRefresh: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "force refresh")),
		Back:         key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", "back")),

		AddLink:  key.NewBinding(key.WithKeys("a", "enter"), key.WithHelp("a", "add link")),
		AddAll:   key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "add all")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "scroll down")),

		Retry:      key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry")),
		CancelWait: key.NewBinding(key.WithKeys("c", "esc"), key.WithHelp("c", "cancel")),

		Toggle:  key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "expand")),
		ListAdd: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add to clipboard")),
		NewList: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new list")),
	}
}

// helpKeys is the footer for one screen: a column per group of keys.
type helpKeys [][]key.Binding

func (h helpKeys) ShortHelp() []key.Binding {
	var out []key.Binding
	for _, col := range h {
		out = append(out, col...)
	}
	return out
}

func (h helpKeys) FullHelp() [][]key.Binding { return h }

// currentKeys names the keys the screen on show answers to.
func (m Model) currentKeys() helpKeys {
	k := m.keys
	clip := []key.Binding{k.Copy, k.Remove, k.Clear, k.ClipUp, k.ClipDown}
	global := []key.Binding{k.PostsTab, k.ListsTab, k.Help, k.Quit}

	if m.tab == tabLists {
		return helpKeys{{k.Up, k.Down, k.Toggle, k.ListAdd, k.NewList}, clip, global}
	}
	switch m.state {
	case stateInput:
		if m.typing() {
			return helpKeys{{k.Confirm, k.Cancel}}
		}
		return helpKeys{{k.Up, k.Down, k.Select, k.NewCampaign, k.Filter, k.Delete, k.Exit}, clip, global}
	case stateList:
		return helpKeys{{k.Up, k.Down, k.Open, k.NextPage, k.PrevPage, k.Refresh, k.ForceRefresh, k.Back}, clip, global}
	case stateDetails:
		return helpKeys{{k.Up, k.Down, k.AddLink, k.AddAll, k.PageUp, k.PageDown, k.ForceRefresh, k.Back}, clip, global}
	case stateError:
		return helpKeys{{k.Retry, k.Back}, global}
	case stateRateLimited:
		return helpKeys{{k.Retry, k.CancelWait}, global}
	}
	return helpKeys{global}
}

// ---- Model --------------------------------------------------------------

// Model represents the TUI state
type Model struct {
	tab             tab
	keys            keyMap
	help            help.Model
	cfg             *config.Config
	state           viewState
	posts           []models.Post
	cursor          int
	client          *api.Client
	database        *db.Database
	input           textinput.Model
	spinner         spinner.Model
	viewport        viewport.Model
	err             error
	width           int
	height          int
	campaignID      string
	campaignName    string
	loadingMsg      string
	postDetails     *models.PostDetails
	cachedDetails   *db.CachedPost
	clipboardLinks  []string // Links collected in clipboard
	clipboardCursor int      // Cursor position in clipboard
	linkCursor      int      // Cursor for YouTube links in details view
	statusMessage   string   // Temporary status message
	// Pagination
	currentPage   int      // Current page number (1-indexed for display)
	nextCursor    string   // Cursor for next page
	cursorHistory []string // History of cursors for going back
	totalPosts    int      // Total posts available
	hasMorePages  bool     // Whether there are more pages
	// Campaign selection
	savedCampaigns  []db.SavedCampaign
	campaignCursor  int             // Cursor for campaign selection
	inputStep       int             // 0 = selection, 1 = entering ID, 2 = entering name, 3 = entering date
	nameInput       textinput.Model // Input for campaign name
	dateInput       textinput.Model // Input for date filter
	pendingID       string          // ID entered in step 1, waiting for name
	publishedAfter  string          // Date filter (YYYY-MM-DD format)
	editingDateOnly bool            // True when editing date from selection screen
	// Rate limiting. pending* records what to re-issue once the wait is over.
	rateLimitUntil   time.Time
	rateLimitAttempt int
	rateLimitStatus  int
	pendingKind      string // "posts" or "details"
	pendingCursor    string
	pendingPostID    string
	// The Lists tab
	lists listsState
}

// PostsFetchedMsg is sent when posts are fetched
type PostsFetchedMsg struct {
	Posts      []models.Post
	NextCursor string
	HasMore    bool
	Total      int
	Err        error
	FromCache  bool
	Cursor     string // cursor this fetch used, so a retry can repeat it
}

// PostDetailsFetchedMsg is sent when post details are fetched
type PostDetailsFetchedMsg struct {
	Details *models.PostDetails
	Err     error
	PostID  string // so a retry knows what to re-request
}

// rateLimitTickMsg drives the countdown while waiting out a rate limit.
type rateLimitTickMsg time.Time

// CampaignsLoadedMsg is sent when saved campaigns are loaded
type CampaignsLoadedMsg struct {
	Campaigns []db.SavedCampaign
}

// NewModel creates a new TUI model
func NewModel(cfg *config.Config, database *db.Database, publishedAfter string) Model {
	ti := textinput.New()
	ti.Placeholder = "Enter campaign ID (e.g., 2175699)"
	ti.Focus()
	ti.CharLimit = 20
	ti.Width = 40

	ni := textinput.New()
	ni.Placeholder = "Enter name (optional, press Enter to skip)"
	ni.CharLimit = 50
	ni.Width = 50

	di := textinput.New()
	di.Placeholder = "YYYY-MM-DD or YYYY-MM-DD HH:mm[:ss] (optional)"
	di.CharLimit = 19
	di.Width = 40

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(accent)

	vp := viewport.New(80, 20)

	return Model{
		keys:           newKeyMap(),
		help:           help.New(),
		cfg:            cfg,
		state:          stateInput,
		client:         api.NewClient(cfg.Cookies),
		database:       database,
		input:          ti,
		nameInput:      ni,
		dateInput:      di,
		spinner:        s,
		viewport:       vp,
		width:          80,
		height:         24,
		clipboardLinks: make([]string, 0),
		cursorHistory:  make([]string, 0),
		currentPage:    1,
		publishedAfter: publishedAfter,
		lists:          listsState{open: map[int64]bool{}},
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.loadCampaigns())
}

func (m Model) loadCampaigns() tea.Cmd {
	return func() tea.Msg {
		if m.database == nil {
			return CampaignsLoadedMsg{Campaigns: nil}
		}
		campaigns, _ := m.database.ListCampaigns()
		return CampaignsLoadedMsg{Campaigns: campaigns}
	}
}

// typing reports whether a text input has the keyboard, so letter and number
// keys belong to it rather than to a binding.
func (m Model) typing() bool {
	return m.tab == tabPosts && m.state == stateInput && m.inputStep != 0
}

// Update handles messages and updates state
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Clear status message on any key press
		m.statusMessage = ""
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.sizeViewport()
		if m.state == stateDetails {
			m.viewport.SetContent(m.renderDetailsContent())
		}
		return m, nil

	case PostsFetchedMsg:
		if msg.Err != nil {
			if rl := asRateLimit(msg.Err); rl != nil {
				return m.enterRateLimit(rl, "posts", msg.Cursor, "")
			}
			m.state = stateError
			m.err = msg.Err
			return m, nil
		}
		m.rateLimitAttempt = 0
		m.posts = msg.Posts
		m.nextCursor = msg.NextCursor
		m.hasMorePages = msg.HasMore
		m.totalPosts = msg.Total

		// Client-side date filtering (local time)
		if m.publishedAfter != "" {
			filterDate, err := datetime.ParseLocal(m.publishedAfter)
			if err == nil {
				var filtered []models.Post
				for _, post := range m.posts {
					if post.PublishedAt.After(filterDate) || post.PublishedAt.Equal(filterDate) {
						filtered = append(filtered, post)
					}
				}
				m.posts = filtered
			} else {
				m.statusMessage = "✗ Invalid date/time filter; showing all posts"
			}
		}

		// Sort posts by published date (most recent first)
		sort.Slice(m.posts, func(i, j int) bool {
			return m.posts[i].PublishedAt.After(m.posts[j].PublishedAt)
		})
		// Update cache status for each post
		for i := range m.posts {
			if m.database != nil {
				cached, _ := m.database.IsPostDetailsCached(m.posts[i].ID)
				m.posts[i].DetailsCached = cached
			}
		}
		m.state = stateList
		m.cursor = 0
		if msg.FromCache {
			m.statusMessage = "📦 Loaded from cache"
		}
		return m, nil

	case PostDetailsFetchedMsg:
		if msg.Err != nil {
			if rl := asRateLimit(msg.Err); rl != nil {
				return m.enterRateLimit(rl, "details", "", msg.PostID)
			}
			m.state = stateError
			m.err = msg.Err
			return m, nil
		}
		m.rateLimitAttempt = 0
		m.postDetails = msg.Details
		m.linkCursor = 0
		// Save to cache
		if m.database != nil && msg.Details != nil {
			linksJSON, _ := json.Marshal(msg.Details.YouTubeLinks)
			m.database.SavePostDetails(msg.Details.ID, msg.Details.Description, string(linksJSON))
			// Update the post's cached status
			for i := range m.posts {
				if m.posts[i].ID == msg.Details.ID {
					m.posts[i].DetailsCached = true
					break
				}
			}
		}
		m.state = stateDetails
		m.viewport.SetContent(m.renderDetailsContent())
		m.viewport.GotoTop()
		return m, nil

	case CampaignsLoadedMsg:
		m.savedCampaigns = msg.Campaigns
		// Start in ID input mode if no saved campaigns, otherwise selection mode
		if len(m.savedCampaigns) == 0 {
			m.inputStep = 1
			m.input.Focus()
		} else {
			m.inputStep = 0
		}
		return m, nil

	case rateLimitTickMsg:
		if m.state != stateRateLimited {
			return m, nil // cancelled while waiting
		}
		if time.Now().Before(m.rateLimitUntil) {
			return m, rateLimitTick()
		}
		return m.resumePending()

	case runsLoadedMsg, runStartedMsg, runLinkMsg, runLogMsg, runStatusMsg, runDoneMsg:
		return m.updateLists(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// Handle viewport scrolling in details view
	if m.state == stateDetails {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C quits from anywhere, a text box included.
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.typing() {
		return m.handleInputKeys(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.sizeViewport()
		return m, nil
	case key.Matches(msg, m.keys.PostsTab):
		m.tab = tabPosts
		return m, nil
	case key.Matches(msg, m.keys.ListsTab):
		m.tab = tabLists
		return m, m.loadRuns()
	}

	if m.clipboardKeysActive() {
		if handled, ok := m.handleClipboardKeys(msg); ok {
			return handled, nil
		}
	}

	if m.tab == tabLists {
		return m.handleListsKeys(msg)
	}

	switch m.state {
	case stateInput:
		return m.handleInputKeys(msg)
	case stateList:
		return m.handleListKeys(msg)
	case stateDetails:
		return m.handleDetailsKeys(msg)
	case stateError:
		return m.handleErrorKeys(msg)
	case stateRateLimited:
		return m.handleRateLimitKeys(msg)
	}
	return m, nil
}

// clipboardKeysActive reports whether the clipboard keys apply here. The rate
// limit screen is left out because c cancels the wait there.
func (m Model) clipboardKeysActive() bool {
	if m.tab == tabLists {
		return true
	}
	switch m.state {
	case stateList, stateDetails:
		return true
	case stateInput:
		return m.inputStep == 0
	}
	return false
}

func (m Model) handleClipboardKeys(msg tea.KeyMsg) (Model, bool) {
	switch {
	case key.Matches(msg, m.keys.Copy):
		// Copy clipboard to system clipboard
		if len(m.clipboardLinks) > 0 {
			text := strings.Join(m.clipboardLinks, "\n")
			if err := clipboard.WriteAll(text); err == nil {
				m.statusMessage = fmt.Sprintf("✓ Copied %d links to clipboard!", len(m.clipboardLinks))
			} else {
				m.statusMessage = "✗ Failed to copy to clipboard"
			}
		} else {
			m.statusMessage = "Clipboard is empty"
		}
	case key.Matches(msg, m.keys.Remove):
		// Remove selected link from clipboard
		if len(m.clipboardLinks) == 0 {
			return m, true
		}
		m.clipboardLinks = append(m.clipboardLinks[:m.clipboardCursor], m.clipboardLinks[m.clipboardCursor+1:]...)
		if m.clipboardCursor >= len(m.clipboardLinks) && m.clipboardCursor > 0 {
			m.clipboardCursor--
		}
		m.statusMessage = "Removed link from clipboard"
	case key.Matches(msg, m.keys.Clear):
		m.clipboardLinks = make([]string, 0)
		m.clipboardCursor = 0
		m.statusMessage = "Cleared clipboard"
	case key.Matches(msg, m.keys.ClipUp):
		if m.clipboardCursor > 0 {
			m.clipboardCursor--
		}
	case key.Matches(msg, m.keys.ClipDown):
		if m.clipboardCursor < len(m.clipboardLinks)-1 {
			m.clipboardCursor++
		}
	default:
		return m, false
	}
	if m.state == stateDetails {
		// The details view marks links already in the clipboard.
		m.viewport.SetContent(m.renderDetailsContent())
	}
	return m, true
}

// addToClipboard appends the links not already collected and returns how many
// were new.
func (m *Model) addToClipboard(links ...string) int {
	added := 0
	for _, link := range links {
		exists := false
		for _, existing := range m.clipboardLinks {
			if existing == link {
				exists = true
				break
			}
		}
		if !exists {
			m.clipboardLinks = append(m.clipboardLinks, link)
			added++
		}
	}
	return added
}

func (m Model) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.posts)-1 {
			m.cursor++
		}
	case key.Matches(msg, m.keys.Open):
		if len(m.posts) > 0 {
			post := m.posts[m.cursor]
			// Check cache first
			if m.database != nil && post.DetailsCached {
				cached, err := m.database.GetPost(post.ID)
				if err == nil && cached != nil && cached.DetailsCached {
					m.cachedDetails = cached
					m.postDetails = &models.PostDetails{
						ID:          cached.ID,
						Title:       cached.Title,
						Description: cached.Description,
					}
					if cached.YouTubeLinks != "" {
						json.Unmarshal([]byte(cached.YouTubeLinks), &m.postDetails.YouTubeLinks)
					}
					m.linkCursor = 0
					m.state = stateDetails
					m.viewport.SetContent(m.renderDetailsContent())
					m.viewport.GotoTop()
					return m, tea.ClearScreen
				}
			}
			// Fetch from API
			m.state = stateLoading
			m.loadingMsg = "Fetching post details..."
			return m, tea.Batch(m.spinner.Tick, m.fetchPostDetails(post.ID))
		}
	case key.Matches(msg, m.keys.Refresh):
		// Refresh current page (from cache if available)
		m.state = stateLoading
		m.loadingMsg = "Refreshing posts..."
		// Get the cursor for the current page (empty for page 1, last history item otherwise)
		cursor := ""
		if m.currentPage > 1 && len(m.cursorHistory) > 0 {
			cursor = m.cursorHistory[len(m.cursorHistory)-1]
		}
		return m, tea.Batch(m.spinner.Tick, m.fetchPosts(cursor, false))
	case key.Matches(msg, m.keys.ForceRefresh):
		// Force refresh - clear cache and go back to page 1
		if m.database != nil {
			m.database.ClearCampaignPages(m.campaignID)
		}
		m.currentPage = 1
		m.cursorHistory = make([]string, 0)
		m.state = stateLoading
		m.loadingMsg = "Force refreshing posts..."
		return m, tea.Batch(m.spinner.Tick, m.fetchPosts("", true))
	case key.Matches(msg, m.keys.NextPage):
		if m.canGoNext() {
			// Save current cursor to history for going back
			if m.currentPage == 1 {
				m.cursorHistory = append(m.cursorHistory, "")
			}
			m.cursorHistory = append(m.cursorHistory, m.nextCursor)
			m.currentPage++
			m.state = stateLoading
			m.loadingMsg = fmt.Sprintf("Loading page %d...", m.currentPage)
			return m, tea.Batch(m.spinner.Tick, m.fetchPosts(m.nextCursor, false))
		}
	case key.Matches(msg, m.keys.PrevPage):
		if m.currentPage > 1 && len(m.cursorHistory) > 0 {
			m.currentPage--
			// Pop the current cursor from history
			m.cursorHistory = m.cursorHistory[:len(m.cursorHistory)-1]
			// Get the previous cursor
			cursor := ""
			if len(m.cursorHistory) > 0 {
				cursor = m.cursorHistory[len(m.cursorHistory)-1]
			}
			m.state = stateLoading
			m.loadingMsg = fmt.Sprintf("Loading page %d...", m.currentPage)
			return m, tea.Batch(m.spinner.Tick, m.fetchPosts(cursor, false))
		}
	case key.Matches(msg, m.keys.Back):
		m.state = stateInput
		m.input.SetValue("")
		m.inputStep = 0
		return m, m.loadCampaigns()
	}
	return m, nil
}

// canGoNext reports whether a next page exists. A filter that removed posts
// leaves fewer than a page, which means the rest is older than the filter,
// not that the campaign has run out.
func (m Model) canGoNext() bool {
	if m.publishedAfter != "" && len(m.posts) < 20 {
		return false
	}
	return m.hasMorePages && m.nextCursor != ""
}

func (m Model) handleDetailsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.state = stateList
		m.postDetails = nil
		m.cachedDetails = nil
		m.linkCursor = 0
		return m, nil
	case key.Matches(msg, m.keys.ForceRefresh):
		// Force refresh this post's details
		if len(m.posts) > 0 {
			post := m.posts[m.cursor]
			if m.database != nil {
				m.database.ClearPostDetails(post.ID)
				post.DetailsCached = false
				m.posts[m.cursor] = post
			}
			m.state = stateLoading
			m.loadingMsg = "Force refreshing post details..."
			return m, tea.Batch(m.spinner.Tick, m.fetchPostDetails(post.ID))
		}
	case key.Matches(msg, m.keys.Up):
		// Navigate YouTube links
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 && m.linkCursor > 0 {
			m.linkCursor--
			m.viewport.SetContent(m.renderDetailsContent())
		}
	case key.Matches(msg, m.keys.Down):
		// Navigate YouTube links
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 && m.linkCursor < len(m.postDetails.YouTubeLinks)-1 {
			m.linkCursor++
			m.viewport.SetContent(m.renderDetailsContent())
		}
	case key.Matches(msg, m.keys.AddLink):
		// Add selected YouTube link to clipboard
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 {
			if m.addToClipboard(m.postDetails.YouTubeLinks[m.linkCursor]) == 0 {
				m.statusMessage = "Link already in clipboard"
			} else {
				m.statusMessage = "✓ Added link to clipboard"
			}
			m.viewport.SetContent(m.renderDetailsContent())
		}
	case key.Matches(msg, m.keys.AddAll):
		// Add ALL YouTube links to clipboard
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 {
			if added := m.addToClipboard(m.postDetails.YouTubeLinks...); added > 0 {
				m.statusMessage = fmt.Sprintf("✓ Added %d links to clipboard", added)
			} else {
				m.statusMessage = "All links already in clipboard"
			}
			m.viewport.SetContent(m.renderDetailsContent())
		}
	case key.Matches(msg, m.keys.PageUp):
		m.viewport.HalfViewUp()
	case key.Matches(msg, m.keys.PageDown):
		m.viewport.HalfViewDown()
	}
	return m, nil
}

func (m Model) handleErrorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Retry):
		m.state = stateLoading
		m.loadingMsg = "Retrying..."
		// Retry with current page's cursor (bypass cache on retry)
		cursor := ""
		if m.currentPage > 1 && len(m.cursorHistory) > 0 {
			cursor = m.cursorHistory[len(m.cursorHistory)-1]
		}
		return m, tea.Batch(m.spinner.Tick, m.fetchPosts(cursor, true))
	case key.Matches(msg, m.keys.Back):
		m.state = stateInput
		m.input.SetValue("")
		m.inputStep = 0
		return m, m.loadCampaigns()
	}
	return m, nil
}

// asRateLimit returns the rate limit error inside err, or nil.
func asRateLimit(err error) *api.RateLimitError {
	var rl *api.RateLimitError
	if errors.As(err, &rl) {
		return rl
	}
	return nil
}

func rateLimitTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return rateLimitTickMsg(t) })
}

// enterRateLimit parks the UI on a countdown instead of failing outright. It
// honours Retry-After when Patreon sends one and backs off linearly when it does
// not, but gives up when the wait or the attempt count gets unreasonable.
func (m Model) enterRateLimit(rl *api.RateLimitError, kind, cursor, postID string) (tea.Model, tea.Cmd) {
	m.rateLimitAttempt++

	wait := rl.RetryAfter
	if wait <= 0 {
		wait = time.Duration(m.rateLimitAttempt) * uiRateLimitBaseWait
	}

	if m.rateLimitAttempt > uiRateLimitMaxAttempts {
		m.state = stateError
		m.err = fmt.Errorf("still rate limited by Patreon (HTTP %d) after %d attempts; try again later",
			rl.StatusCode, uiRateLimitMaxAttempts)
		m.rateLimitAttempt = 0
		return m, nil
	}
	if wait > uiRateLimitMaxWait {
		m.state = stateError
		m.err = fmt.Errorf("Patreon asked to wait %s (HTTP %d), longer than the %s limit; try again later",
			wait.Round(time.Second), rl.StatusCode, uiRateLimitMaxWait)
		m.rateLimitAttempt = 0
		return m, nil
	}

	m.rateLimitStatus = rl.StatusCode
	m.rateLimitUntil = time.Now().Add(wait)
	m.pendingKind = kind
	m.pendingCursor = cursor
	m.pendingPostID = postID
	m.state = stateRateLimited
	return m, rateLimitTick()
}

// resumePending re-issues the request that was rate limited.
func (m Model) resumePending() (tea.Model, tea.Cmd) {
	m.state = stateLoading
	m.loadingMsg = "Retrying after rate limit..."
	if m.pendingKind == "details" {
		return m, tea.Batch(m.spinner.Tick, m.fetchPostDetails(m.pendingPostID))
	}
	// Bypass the cache: the cache is what failed to satisfy this request.
	return m, tea.Batch(m.spinner.Tick, m.fetchPosts(m.pendingCursor, true))
}

func (m Model) handleRateLimitKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Retry):
		return m.resumePending()
	case key.Matches(msg, m.keys.CancelWait):
		m.state = stateError
		m.err = fmt.Errorf("rate limited by Patreon (HTTP %d); wait cancelled", m.rateLimitStatus)
		m.rateLimitAttempt = 0
		return m, nil
	}
	return m, nil
}

func (m Model) handleInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.inputStep {
	case 1: // Entering campaign ID
		switch {
		case key.Matches(msg, m.keys.Confirm):
			if m.input.Value() != "" {
				// Move to name entry step
				m.pendingID = m.input.Value()
				m.inputStep = 2
				m.input.Blur()
				m.nameInput.SetValue("")
				m.nameInput.Focus()
				return m, textinput.Blink
			}
		case key.Matches(msg, m.keys.Cancel):
			// If we have saved campaigns, go back to selection mode
			if len(m.savedCampaigns) > 0 {
				m.inputStep = 0
				m.input.SetValue("")
				m.input.Blur()
				return m, m.loadCampaigns()
			}
			// Otherwise quit
			return m, tea.Quit
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		return m, nil

	case 2: // Entering campaign name
		switch {
		case key.Matches(msg, m.keys.Confirm):
			// Move to date filter step
			m.campaignName = m.nameInput.Value()
			m.inputStep = 3
			m.nameInput.Blur()
			m.dateInput.SetValue(m.publishedAfter)
			m.dateInput.Focus()
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Cancel):
			// Go back to ID entry
			m.inputStep = 1
			m.nameInput.Blur()
			m.input.Focus()
			return m, textinput.Blink
		default:
			var cmd tea.Cmd
			m.nameInput, cmd = m.nameInput.Update(msg)
			return m, cmd
		}

	case 3: // Entering date filter
		switch {
		case key.Matches(msg, m.keys.Confirm):
			input := strings.TrimSpace(m.dateInput.Value())
			if input != "" {
				if _, err := datetime.ParseLocal(input); err != nil {
					m.statusMessage = fmt.Sprintf("✗ Invalid date/time: %v", err)
					m.dateInput.Focus()
					return m, nil
				}
			}
			m.publishedAfter = input
			m.dateInput.Blur()

			if m.editingDateOnly {
				// Just editing the filter, go back to selection
				m.inputStep = 0
				m.editingDateOnly = false
				return m, nil
			}

			// Adding a new campaign - save it and fetch posts
			m.campaignID = m.pendingID
			if m.database != nil {
				m.database.SaveCampaign(m.campaignID, m.campaignName)
			}
			m.currentPage = 1
			m.cursorHistory = make([]string, 0)
			m.state = stateLoading
			m.loadingMsg = "Fetching posts..."
			m.pendingID = ""
			return m, tea.Batch(m.spinner.Tick, m.fetchPosts("", false))
		case key.Matches(msg, m.keys.Cancel):
			if m.editingDateOnly {
				// Go back to selection mode
				m.inputStep = 0
				m.dateInput.Blur()
				m.editingDateOnly = false
				return m, nil
			}
			// Go back to name entry
			m.inputStep = 2
			m.dateInput.Blur()
			m.nameInput.Focus()
			return m, textinput.Blink
		default:
			var cmd tea.Cmd
			m.dateInput, cmd = m.dateInput.Update(msg)
			return m, cmd
		}

	default: // inputStep 0: Selection mode (choosing from saved campaigns)
		switch {
		case key.Matches(msg, m.keys.Up):
			if m.campaignCursor > 0 {
				m.campaignCursor--
			}
		case key.Matches(msg, m.keys.Down):
			if m.campaignCursor < len(m.savedCampaigns)-1 {
				m.campaignCursor++
			}
		case key.Matches(msg, m.keys.Select):
			if len(m.savedCampaigns) > 0 {
				selected := m.savedCampaigns[m.campaignCursor]
				m.campaignID = selected.ID
				m.campaignName = selected.Name
				m.currentPage = 1
				m.cursorHistory = make([]string, 0)
				m.state = stateLoading
				m.loadingMsg = "Fetching posts..."
				return m, tea.Batch(m.spinner.Tick, m.fetchPosts("", false))
			}
		case key.Matches(msg, m.keys.NewCampaign):
			// Switch to input mode to add new campaign
			m.inputStep = 1
			m.input.SetValue("")
			m.input.Focus()
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Delete):
			// Delete selected campaign
			if len(m.savedCampaigns) > 0 {
				selected := m.savedCampaigns[m.campaignCursor]
				if m.database != nil {
					m.database.DeleteCampaign(selected.ID)
				}
				// Reload campaigns
				return m, m.loadCampaigns()
			}
		case key.Matches(msg, m.keys.Filter):
			// Edit date filter
			m.inputStep = 3
			m.editingDateOnly = true
			m.dateInput.SetValue(m.publishedAfter)
			m.dateInput.Focus()
			return m, textinput.Blink
		case key.Matches(msg, m.keys.Exit):
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) fetchPosts(cursor string, forceRefresh bool) tea.Cmd {
	return func() tea.Msg {
		// Check cache first (unless force refresh)
		if !forceRefresh && m.database != nil {
			cachedPage, err := m.database.GetPage(m.campaignID, cursor)
			if err == nil && cachedPage != nil {
				// Parse the cached posts JSON
				var posts []models.Post
				if err := json.Unmarshal([]byte(cachedPage.PostsJSON), &posts); err == nil {
					return PostsFetchedMsg{
						Posts:      posts,
						NextCursor: cachedPage.NextCursor,
						HasMore:    cachedPage.HasMore,
						Total:      0,
						FromCache:  true,
						Cursor:     cursor,
					}
				}
			}
		}

		// Fetch from API
		page, err := m.client.FetchPosts(m.campaignID, 20, cursor)
		if err != nil {
			return PostsFetchedMsg{Err: err, Cursor: cursor}
		}

		// Save campaign and posts to cache
		if m.database != nil {
			m.database.SaveCampaign(m.campaignID, "")

			// Save individual posts
			for _, post := range page.Posts {
				cachedPost := &db.CachedPost{
					ID:                 post.ID,
					CampaignID:         m.campaignID,
					Type:               post.Type,
					PostType:           post.PostType,
					Title:              post.Title,
					PatreonURL:         post.PatreonURL,
					CurrentUserCanView: post.CurrentUserCanView,
					PublishedAt:        post.PublishedAt,
				}
				m.database.SavePost(cachedPost)
			}

			// Save the page to cache
			postsJSON, _ := json.Marshal(page.Posts)
			m.database.SavePage(m.campaignID, cursor, string(postsJSON), page.NextCursor, page.HasMore)
		}

		return PostsFetchedMsg{
			Posts:      page.Posts,
			NextCursor: page.NextCursor,
			HasMore:    page.HasMore,
			Total:      page.Total,
			FromCache:  false,
			Cursor:     cursor,
		}
	}
}

func (m Model) fetchPostDetails(postID string) tea.Cmd {
	return func() tea.Msg {
		details, err := m.client.FetchPostDetails(postID)
		return PostDetailsFetchedMsg{Details: details, Err: err, PostID: postID}
	}
}

// ---- Chrome -------------------------------------------------------------

// mainWidth is the width of the left frame. The clipboard frame takes the
// rest when the terminal is wide enough to hold both.
func (m Model) mainWidth() int {
	if m.width >= wideLayout {
		return m.width - clipboardPanelWidth - 1
	}
	return m.width
}

func (m Model) renderFooter() string {
	return footerStyle.Width(m.width).Render(m.help.View(m.currentKeys()))
}

// bodyHeight is what the tab bar and the footer leave for the frames.
func (m Model) bodyHeight() int {
	return max(m.height-lipgloss.Height(m.renderTabs())-lipgloss.Height(m.renderFooter()), frameRows+1)
}

// sizeViewport fits the details viewport inside the main frame. It runs on a
// resize and when the footer grows or shrinks.
func (m *Model) sizeViewport() {
	m.viewport.Width = max(m.mainWidth()-frameCols, 10)
	m.viewport.Height = max(m.bodyHeight()-frameRows, 1)
}

func (m Model) renderTabs() string {
	var tabs []string
	for i, name := range tabNames {
		style := tabInactiveStyle
		if tab(i) == m.tab {
			style = tabActiveStyle
		}
		tabs = append(tabs, style.Render(name))
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)

	// A running list shows on both tabs, so leaving the Lists tab does not
	// hide that a run is still talking to Patreon.
	if m.lists.running {
		n := 0
		if m.lists.live != nil {
			n = len(m.lists.live.Links)
		}
		bar += "  " + m.spinner.View() + dimStyle.Render(fmt.Sprintf(" making a list, %d links so far", n))
	}
	if m.statusMessage != "" {
		switch {
		case strings.HasPrefix(m.statusMessage, "✓"):
			bar += "  " + successStyle.Render(m.statusMessage)
		case strings.HasPrefix(m.statusMessage, "✗"):
			bar += "  " + errorStyle.Render(m.statusMessage)
		default:
			bar += "  " + dimStyle.Render(m.statusMessage)
		}
	}
	return tabBarStyle.MaxWidth(m.width).Render(bar)
}

// View renders the TUI
func (m Model) View() string {
	top, bottom := m.renderTabs(), m.renderFooter()
	h := m.bodyHeight()
	w := m.mainWidth()

	var main string
	if m.tab == tabLists {
		main = m.viewLists(w, h)
	} else {
		switch m.state {
		case stateInput:
			main = m.viewInput(w, h)
		case stateLoading:
			main = box(titleStyle.Render("Loading"), fmt.Sprintf("%s %s", m.spinner.View(), m.loadingMsg), w, h)
		case stateList:
			main = m.viewList(w, h)
		case stateDetails:
			main = box(titleStyle.Render("Post"), m.viewport.View(), w, h)
		case stateError:
			main = box(errorStyle.Render("Error"), errorStyle.Render(fmt.Sprintf("Error: %v", m.err)), w, h)
		case stateRateLimited:
			main = m.viewRateLimited(w, h)
		}
	}

	body := main
	if m.width >= wideLayout {
		body = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", m.viewClipboard(clipboardPanelWidth, h))
	}
	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

// ---- Views --------------------------------------------------------------

// viewClipboard renders the clipboard frame beside the main one.
func (m Model) viewClipboard(w, h int) string {
	title := titleStyle.Render("📋 Clipboard") + dimStyle.Render(fmt.Sprintf(" (%d)", len(m.clipboardLinks)))
	if len(m.clipboardLinks) == 0 {
		return box(title, dimStyle.Render("No links collected.\nPress a on a link to add it."), w, h)
	}

	inner := w - frameCols
	maxVisible := max(h-frameRows, 1)
	start := 0
	if m.clipboardCursor >= maxVisible {
		start = m.clipboardCursor - maxVisible + 1
	}
	end := min(start+maxVisible, len(m.clipboardLinks))

	var lines []string
	for i := start; i < end; i++ {
		link := pad(m.clipboardLinks[i], inner)
		if i == m.clipboardCursor {
			lines = append(lines, clipboardSelectedStyle.Render(link))
		} else {
			lines = append(lines, clipboardLinkStyle.Render(link))
		}
	}
	return box(title, strings.Join(lines, "\n"), w, h)
}

func (m Model) viewRateLimited(w, h int) string {
	remaining := time.Until(m.rateLimitUntil)
	if remaining < 0 {
		remaining = 0
	}

	var b strings.Builder
	b.WriteString(errorStyle.Render(fmt.Sprintf("⏳ Rate limited by Patreon (HTTP %d)", m.rateLimitStatus)))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Retrying in %02d:%02d  (attempt %d of %d)",
		int(remaining.Seconds())/60, int(remaining.Seconds())%60,
		m.rateLimitAttempt, uiRateLimitMaxAttempts))

	return box(errorStyle.Render("Rate limited"), b.String(), w, h)
}

func (m Model) viewInput(w, h int) string {
	var b strings.Builder
	title := "New campaign"

	switch m.inputStep {
	case 1: // Entering campaign ID
		b.WriteString("Enter a new campaign ID:\n\n")
		b.WriteString(m.input.View())

	case 2: // Entering campaign name
		b.WriteString(fmt.Sprintf("Campaign ID: %s\n\n", m.pendingID))
		b.WriteString("Enter a name for this campaign (optional):\n\n")
		b.WriteString(m.nameInput.View())

	case 3: // Entering date filter
		if m.editingDateOnly {
			title = "Date filter"
			b.WriteString("Edit date filter:\n\n")
		} else {
			b.WriteString(fmt.Sprintf("Campaign ID: %s\n", m.pendingID))
			if m.campaignName != "" {
				b.WriteString(fmt.Sprintf("Name: %s\n\n", m.campaignName))
			} else {
				b.WriteString("\n")
			}
			b.WriteString("Filter posts after date (optional):\n\n")
		}
		b.WriteString(m.dateInput.View())
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("Format: YYYY-MM-DD or YYYY-MM-DD HH:mm[:ss]"))

	default: // inputStep 0: Selection mode
		title = "Campaigns"
		if len(m.savedCampaigns) > 0 {
			b.WriteString("Select a campaign:\n\n")

			for i, campaign := range m.savedCampaigns {
				displayName := campaign.ID
				if campaign.Name != "" {
					displayName = fmt.Sprintf("%s (%s)", campaign.Name, campaign.ID)
				}

				if i == m.campaignCursor {
					b.WriteString(selectedStyle.Render(fmt.Sprintf("▶ %s", displayName)))
				} else {
					b.WriteString(normalStyle.Render(fmt.Sprintf("  %s", displayName)))
				}
				b.WriteString("\n")
			}

			// Show current date filter
			if m.publishedAfter != "" {
				b.WriteString(fmt.Sprintf("\n📅 Filter: posts after %s", m.publishedAfter))
			}
		} else {
			b.WriteString("No saved campaigns.\n\n")
			b.WriteString("Enter a campaign ID:\n\n")
			b.WriteString(m.input.View())
		}
	}

	return box(titleStyle.Render(title), b.String(), w, h)
}

func (m Model) viewList(w, h int) string {
	// Build status with pagination info
	pageInfo := fmt.Sprintf("Page %d", m.currentPage)
	if m.canGoNext() {
		pageInfo += " →"
	}
	if m.currentPage > 1 {
		pageInfo = "← " + pageInfo
	}
	pageInfo += fmt.Sprintf(" (%d posts)", len(m.posts))
	if m.publishedAfter != "" {
		pageInfo += fmt.Sprintf(" • 📅 after %s", m.publishedAfter)
	}
	// Build campaign display with name if available
	campaignDisplay := m.campaignID
	if m.campaignName != "" {
		campaignDisplay = fmt.Sprintf("%s (%s)", m.campaignName, m.campaignID)
	}
	title := titleStyle.Render("Posts") + dimStyle.Render(fmt.Sprintf(" %s • %s", campaignDisplay, pageInfo))

	var main strings.Builder

	// Columns: cache mark, post type, title, access. The row styles add a
	// column of padding each side, and each " │ " separator takes three.
	inner := w - frameCols
	titleWidth := max(inner-2-2-12-6-3*3, 15)
	// The leading space stands in for the row styles' left padding.
	header := " " + pad("💾", 2) + " │ " + pad("POST TYPE", 12) + " │ " + pad("TITLE", titleWidth) + " │ " + "ACCESS"
	main.WriteString(headerStyle.Render(header))
	main.WriteString("\n")

	// Rows left after the header (2 lines) and the selected post block (5).
	visiblePosts := max(h-frameRows-2-5, 1)
	visiblePosts = min(visiblePosts, len(m.posts))

	// Scrolling logic
	start := 0
	if m.cursor >= visiblePosts {
		start = m.cursor - visiblePosts + 1
	}
	end := min(start+visiblePosts, len(m.posts))

	for i := start; i < end; i++ {
		post := m.posts[i]

		// Cache indicator
		var cacheIndicator string
		if post.DetailsCached {
			cacheIndicator = cachedStyle.Render("✓")
		} else {
			cacheIndicator = notCachedStyle.Render("·")
		}

		// Format access status
		var access string
		if post.CurrentUserCanView {
			access = canViewStyle.Render("✓ Yes")
		} else {
			access = cannotViewStyle.Render("✗ No")
		}

		line := pad(cacheIndicator, 2) + " │ " + pad(typeStyle.Render(post.PostType), 12) + " │ " +
			pad(post.Title, titleWidth) + " │ " + access

		if i == m.cursor {
			main.WriteString(selectedStyle.Render(line))
		} else {
			main.WriteString(normalStyle.Render(line))
		}
		main.WriteString("\n")
	}

	// Show selected post details
	if len(m.posts) > 0 {
		selected := m.posts[m.cursor]
		main.WriteString("\n")
		main.WriteString(headerStyle.Render("Selected Post"))
		main.WriteString("\n")
		urlText := "https://www.patreon.com" + selected.PatreonURL
		main.WriteString(fmt.Sprintf("  URL: %s\n", urlStyle.Render(urlText)))
		main.WriteString(fmt.Sprintf("  Published: %s", selected.PublishedAt.Format("2006-01-02 15:04")))
	}

	return box(title, main.String(), w, h)
}

func (m Model) renderDetailsContent() string {
	if m.postDetails == nil {
		return "No details available"
	}

	var b strings.Builder

	b.WriteString(headerStyle.Render(m.postDetails.Title))
	b.WriteString("\n")
	if m.postDetails.PostType != "" {
		b.WriteString(typeStyle.Render("  " + m.postDetails.PostType))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// YouTube Links section
	if len(m.postDetails.YouTubeLinks) > 0 {
		b.WriteString(youtubeStyle.Render("📺 YouTube Links"))
		b.WriteString(" (use ↑/↓ to select, 'a' to add)\n")
		for i, link := range m.postDetails.YouTubeLinks {
			// Check if link is in clipboard
			inClipboard := false
			for _, clipLink := range m.clipboardLinks {
				if clipLink == link {
					inClipboard = true
					break
				}
			}

			prefix := "  "
			suffix := ""
			if inClipboard {
				suffix = " ✓"
			}

			if i == m.linkCursor {
				b.WriteString(linkSelectedStyle.Render(fmt.Sprintf("%s▶ %s%s", prefix, link, suffix)))
			} else {
				b.WriteString(fmt.Sprintf("%s  %s%s", prefix, urlStyle.Render(link), cachedStyle.Render(suffix)))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	} else {
		b.WriteString(notCachedStyle.Render("No YouTube links found"))
		b.WriteString("\n\n")
	}

	// Description section — always shown; includes embed metadata when present
	b.WriteString(headerStyle.Render("📝 Description"))
	b.WriteString("\n")
	if m.postDetails.Description != "" {
		wrapped := wordWrap(m.postDetails.Description, m.viewport.Width-4)
		b.WriteString(descriptionStyle.Render(wrapped))
	} else {
		b.WriteString(notCachedStyle.Render("  No description available"))
	}
	b.WriteString("\n")

	return b.String()
}

// wordWrap wraps text to the specified width, preserving newlines as paragraph breaks.
func wordWrap(text string, width int) string {
	if width <= 0 {
		width = 80
	}
	var result strings.Builder
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		words := strings.Fields(line)
		lineLen := 0
		for _, word := range words {
			if lineLen+len(word)+1 > width && lineLen > 0 {
				result.WriteString("\n")
				lineLen = 0
			}
			if lineLen > 0 {
				result.WriteString(" ")
				lineLen++
			}
			result.WriteString(word)
			lineLen += len(word)
		}
	}
	return result.String()
}
