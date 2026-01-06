package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"patreon-posts/internal/api"
	"patreon-posts/internal/db"
	"patreon-posts/internal/models"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF424D")).
			Background(lipgloss.Color("#1a1a2e")).
			Padding(0, 2).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00D4AA")).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#3d3d5c"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1a1a2e")).
			Background(lipgloss.Color("#FF424D")).
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

	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FF424D")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666680")).
			Italic(true).
			MarginTop(1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff6b6b")).
			Bold(true).
			Padding(1)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666680")).
			Background(lipgloss.Color("#1a1a2e")).
			Padding(0, 1)

	cachedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D4AA")).
			Bold(true)

	notCachedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666680"))

	youtubeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	descriptionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#b0b0b0")).
				PaddingLeft(2)

	// Clipboard panel styles
	clipboardPanelStyle = lipgloss.NewStyle().
				Padding(0, 1).
				MarginLeft(1)

	clipboardTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FF424D")).
				MarginBottom(1)

	clipboardLinkStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5c9eff"))

	clipboardSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1a2e")).
				Background(lipgloss.Color("#5c9eff")).
				Bold(true)

	clipboardEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#666680")).
				Italic(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00D4AA")).
			Bold(true)

	linkSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1a2e")).
				Background(lipgloss.Color("#FF0000")).
				Bold(true)
)

// View states
type viewState int

const (
	stateInput viewState = iota
	stateLoading
	stateList
	stateDetails
	stateError
)

const clipboardPanelWidth = 45

// Model represents the TUI state
type Model struct {
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
}

// PostsFetchedMsg is sent when posts are fetched
type PostsFetchedMsg struct {
	Posts      []models.Post
	NextCursor string
	HasMore    bool
	Total      int
	Err        error
}

// PostDetailsFetchedMsg is sent when post details are fetched
type PostDetailsFetchedMsg struct {
	Details *models.PostDetails
	Err     error
}

// CacheUpdatedMsg is sent when cache status is updated
type CacheUpdatedMsg struct {
	PostID string
	Cached bool
}

// NewModel creates a new TUI model
func NewModel(cookies string, database *db.Database) Model {
	ti := textinput.New()
	ti.Placeholder = "Enter campaign ID (e.g., 2175699)"
	ti.Focus()
	ti.CharLimit = 20
	ti.Width = 40

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF424D"))

	vp := viewport.New(80, 20)

	return Model{
		state:          stateInput,
		client:         api.NewClient(cookies),
		database:       database,
		input:          ti,
		spinner:        s,
		viewport:       vp,
		width:          80,
		height:         24,
		clipboardLinks: make([]string, 0),
		cursorHistory:  make([]string, 0),
		currentPage:    1,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages and updates state
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Clear status message on any key press
	if _, ok := msg.(tea.KeyMsg); ok {
		m.statusMessage = ""
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle global keys first
		switch msg.String() {
		case "ctrl+c", "esc":
			// Always allow quit with Ctrl+C or Esc from input screen
			if m.state == stateInput {
				return m, tea.Quit
			}
		case "q":
			// Only quit with 'q' if not in input mode
			if m.state != stateInput {
				return m, tea.Quit
			}
		case "c", "y":
			// Copy clipboard to system clipboard (works in list and details view)
			if m.state == stateList || m.state == stateDetails {
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
				return m, nil
			}
		case "x":
			// Remove selected link from clipboard (works in list and details view)
			if (m.state == stateList || m.state == stateDetails) && len(m.clipboardLinks) > 0 {
				m.clipboardLinks = append(m.clipboardLinks[:m.clipboardCursor], m.clipboardLinks[m.clipboardCursor+1:]...)
				if m.clipboardCursor >= len(m.clipboardLinks) && m.clipboardCursor > 0 {
					m.clipboardCursor--
				}
				m.statusMessage = "Removed link from clipboard"
				return m, nil
			}
		case "X":
			// Clear entire clipboard
			if m.state == stateList || m.state == stateDetails {
				m.clipboardLinks = make([]string, 0)
				m.clipboardCursor = 0
				m.statusMessage = "Cleared clipboard"
				return m, nil
			}
		case "[":
			// Move clipboard cursor up
			if (m.state == stateList || m.state == stateDetails) && m.clipboardCursor > 0 {
				m.clipboardCursor--
				return m, nil
			}
		case "]":
			// Move clipboard cursor down
			if (m.state == stateList || m.state == stateDetails) && m.clipboardCursor < len(m.clipboardLinks)-1 {
				m.clipboardCursor++
				return m, nil
			}
		}

		// Handle state-specific keys
		switch m.state {
		case stateInput:
			if msg.String() == "enter" && m.input.Value() != "" {
				m.campaignID = m.input.Value()
				m.currentPage = 1
				m.cursorHistory = make([]string, 0)
				m.state = stateLoading
				m.loadingMsg = "Fetching posts..."
				return m, tea.Batch(m.spinner.Tick, m.fetchPosts(""))
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd

		case stateList:
			return m.handleListKeys(msg)

		case stateDetails:
			return m.handleDetailsKeys(msg)

		case stateError:
			return m.handleErrorKeys(msg)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		mainWidth := msg.Width - clipboardPanelWidth - 3
		if mainWidth < 40 {
			mainWidth = 40
		}
		m.viewport.Width = mainWidth - 4
		m.viewport.Height = msg.Height - 10
		return m, nil

	case PostsFetchedMsg:
		if msg.Err != nil {
			m.state = stateError
			m.err = msg.Err
			return m, nil
		}
		m.posts = msg.Posts
		m.nextCursor = msg.NextCursor
		m.hasMorePages = msg.HasMore
		m.totalPosts = msg.Total
		// Update cache status for each post
		for i := range m.posts {
			if m.database != nil {
				cached, _ := m.database.IsPostDetailsCached(m.posts[i].ID)
				m.posts[i].DetailsCached = cached
			}
		}
		m.state = stateList
		m.cursor = 0
		return m, nil

	case PostDetailsFetchedMsg:
		if msg.Err != nil {
			m.state = stateError
			m.err = msg.Err
			return m, nil
		}
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

func (m Model) handleListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.posts)-1 {
			m.cursor++
		}
	case "enter":
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
					return m, nil
				}
			}
			// Fetch from API
			m.state = stateLoading
			m.loadingMsg = "Fetching post details..."
			return m, tea.Batch(m.spinner.Tick, m.fetchPostDetails(post.ID))
		}
	case "r":
		// Refresh current page
		m.state = stateLoading
		m.loadingMsg = "Refreshing posts..."
		// Get the cursor for the current page (empty for page 1, last history item otherwise)
		cursor := ""
		if m.currentPage > 1 && len(m.cursorHistory) > 0 {
			cursor = m.cursorHistory[len(m.cursorHistory)-1]
		}
		return m, tea.Batch(m.spinner.Tick, m.fetchPosts(cursor))
	case "R":
		// Force refresh - go back to page 1
		m.currentPage = 1
		m.cursorHistory = make([]string, 0)
		m.state = stateLoading
		m.loadingMsg = "Force refreshing posts..."
		return m, tea.Batch(m.spinner.Tick, m.fetchPosts(""))
	case "n", "l", "right":
		// Next page
		if m.hasMorePages && m.nextCursor != "" {
			// Save current cursor to history for going back
			if m.currentPage == 1 {
				m.cursorHistory = append(m.cursorHistory, "")
			}
			m.cursorHistory = append(m.cursorHistory, m.nextCursor)
			m.currentPage++
			m.state = stateLoading
			m.loadingMsg = fmt.Sprintf("Loading page %d...", m.currentPage)
			return m, tea.Batch(m.spinner.Tick, m.fetchPosts(m.nextCursor))
		}
	case "p", "h", "left":
		// Previous page
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
			return m, tea.Batch(m.spinner.Tick, m.fetchPosts(cursor))
		}
	case "esc":
		m.state = stateInput
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) handleDetailsKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.state = stateList
		m.postDetails = nil
		m.cachedDetails = nil
		m.linkCursor = 0
		return m, nil
	case "R":
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
	case "up", "k":
		// Navigate YouTube links
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 && m.linkCursor > 0 {
			m.linkCursor--
			m.viewport.SetContent(m.renderDetailsContent())
		}
	case "down", "j":
		// Navigate YouTube links
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 && m.linkCursor < len(m.postDetails.YouTubeLinks)-1 {
			m.linkCursor++
			m.viewport.SetContent(m.renderDetailsContent())
		}
	case "a", "enter":
		// Add selected YouTube link to clipboard
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 {
			link := m.postDetails.YouTubeLinks[m.linkCursor]
			// Check if already in clipboard
			for _, existing := range m.clipboardLinks {
				if existing == link {
					m.statusMessage = "Link already in clipboard"
					return m, nil
				}
			}
			m.clipboardLinks = append(m.clipboardLinks, link)
			m.statusMessage = "✓ Added link to clipboard"
		}
	case "A":
		// Add ALL YouTube links to clipboard
		if m.postDetails != nil && len(m.postDetails.YouTubeLinks) > 0 {
			added := 0
			for _, link := range m.postDetails.YouTubeLinks {
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
			if added > 0 {
				m.statusMessage = fmt.Sprintf("✓ Added %d links to clipboard", added)
			} else {
				m.statusMessage = "All links already in clipboard"
			}
		}
	case "pgup":
		m.viewport.HalfViewUp()
	case "pgdown":
		m.viewport.HalfViewDown()
	}
	return m, nil
}

func (m Model) handleErrorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		m.state = stateLoading
		m.loadingMsg = "Retrying..."
		// Retry with current page's cursor
		cursor := ""
		if m.currentPage > 1 && len(m.cursorHistory) > 0 {
			cursor = m.cursorHistory[len(m.cursorHistory)-1]
		}
		return m, tea.Batch(m.spinner.Tick, m.fetchPosts(cursor))
	case "esc":
		m.state = stateInput
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) fetchPosts(cursor string) tea.Cmd {
	return func() tea.Msg {
		page, err := m.client.FetchPosts(m.campaignID, 20, cursor)
		if err != nil {
			return PostsFetchedMsg{Err: err}
		}

		// Save campaign and posts to cache
		if m.database != nil {
			m.database.SaveCampaign(m.campaignID, "")
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
		}

		return PostsFetchedMsg{
			Posts:      page.Posts,
			NextCursor: page.NextCursor,
			HasMore:    page.HasMore,
			Total:      page.Total,
		}
	}
}

func (m Model) fetchPostDetails(postID string) tea.Cmd {
	return func() tea.Msg {
		details, err := m.client.FetchPostDetails(postID)
		return PostDetailsFetchedMsg{Details: details, Err: err}
	}
}

// renderClipboardPanel renders the right-side clipboard panel
func (m Model) renderClipboardPanel(height int, topPadding int) string {
	var b strings.Builder

	// Add top padding to align with main content
	for i := 0; i < topPadding; i++ {
		b.WriteString("\n")
	}

	b.WriteString(clipboardTitleStyle.Render("📋 Clipboard"))
	b.WriteString(fmt.Sprintf(" (%d)", len(m.clipboardLinks)))
	b.WriteString("\n")

	if len(m.clipboardLinks) == 0 {
		b.WriteString(clipboardEmptyStyle.Render("No links collected"))
		b.WriteString("\n")
		b.WriteString(clipboardEmptyStyle.Render("Use 'a' in post view"))
		b.WriteString("\n")
		b.WriteString(clipboardEmptyStyle.Render("to add links"))
	} else {
		// Show links with selection
		maxVisible := height - 6
		if maxVisible < 3 {
			maxVisible = 3
		}

		start := 0
		if m.clipboardCursor >= maxVisible {
			start = m.clipboardCursor - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.clipboardLinks) {
			end = len(m.clipboardLinks)
		}

		for i := start; i < end; i++ {
			link := m.clipboardLinks[i]
			// Extract video ID for display
			displayLink := link
			if len(displayLink) > clipboardPanelWidth-8 {
				displayLink = displayLink[:clipboardPanelWidth-11] + "..."
			}

			if i == m.clipboardCursor {
				b.WriteString(clipboardSelectedStyle.Render(fmt.Sprintf(" %s ", displayLink)))
			} else {
				b.WriteString(clipboardLinkStyle.Render(fmt.Sprintf(" %s", displayLink)))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("[/] nav • x del • X clear"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("c/y copy to system"))

	// Add status message if present
	if m.statusMessage != "" {
		b.WriteString("\n\n")
		if strings.HasPrefix(m.statusMessage, "✓") {
			b.WriteString(successStyle.Render(m.statusMessage))
		} else if strings.HasPrefix(m.statusMessage, "✗") {
			b.WriteString(errorStyle.Render(m.statusMessage))
		} else {
			b.WriteString(notCachedStyle.Render(m.statusMessage))
		}
	}

	content := b.String()
	return clipboardPanelStyle.Width(clipboardPanelWidth - 4).Render(content)
}

// View renders the TUI
func (m Model) View() string {
	switch m.state {
	case stateInput:
		return m.viewInput()
	case stateLoading:
		return m.viewLoading()
	case stateList:
		return m.viewList()
	case stateDetails:
		return m.viewDetails()
	case stateError:
		return m.viewError()
	}
	return ""
}

func (m Model) viewInput() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("🎨 Patreon Posts Viewer"))
	b.WriteString("\n\n")
	b.WriteString("Enter the campaign ID to fetch posts:\n\n")
	b.WriteString(inputStyle.Render(m.input.View()))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Press Enter to fetch • Ctrl+C to quit"))

	return b.String()
}

func (m Model) viewLoading() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("🎨 Patreon Posts Viewer"))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("%s %s", m.spinner.View(), m.loadingMsg))

	return b.String()
}

func (m Model) viewList() string {
	mainWidth := m.width - clipboardPanelWidth - 3
	if mainWidth < 40 {
		mainWidth = 40
	}

	// Build main content
	var main strings.Builder

	main.WriteString(titleStyle.Render("🎨 Patreon Posts Viewer"))
	main.WriteString("\n")
	// Build status with pagination info
	pageInfo := fmt.Sprintf("Page %d", m.currentPage)
	if m.hasMorePages {
		pageInfo += " →"
	}
	if m.currentPage > 1 {
		pageInfo = "← " + pageInfo
	}
	pageInfo += fmt.Sprintf(" (%d posts)", len(m.posts))
	main.WriteString(statusBarStyle.Render(fmt.Sprintf("Campaign: %s • %s", m.campaignID, pageInfo)))
	main.WriteString("\n\n")

	// Header with cache column - adjust widths for narrower main panel
	titleWidth := mainWidth - 45
	if titleWidth < 15 {
		titleWidth = 15
	}
	header := fmt.Sprintf("%-3s │ %-12s │ %-*s │ %-6s", "💾", "POST TYPE", titleWidth, "TITLE", "ACCESS")
	main.WriteString(headerStyle.Render(header))
	main.WriteString("\n")

	// Calculate visible posts based on height
	visiblePosts := m.height - 12
	if visiblePosts < 5 {
		visiblePosts = 5
	}
	if visiblePosts > len(m.posts) {
		visiblePosts = len(m.posts)
	}

	// Scrolling logic
	start := 0
	if m.cursor >= visiblePosts {
		start = m.cursor - visiblePosts + 1
	}
	end := start + visiblePosts
	if end > len(m.posts) {
		end = len(m.posts)
	}

	for i := start; i < end; i++ {
		post := m.posts[i]

		// Cache indicator
		var cacheIndicator string
		if post.DetailsCached {
			cacheIndicator = cachedStyle.Render("✓")
		} else {
			cacheIndicator = notCachedStyle.Render("·")
		}

		// Truncate title if too long
		title := post.Title
		if len(title) > titleWidth {
			title = title[:titleWidth-3] + "..."
		}

		// Format access status
		var access string
		if post.CurrentUserCanView {
			access = canViewStyle.Render("✓ Yes")
		} else {
			access = cannotViewStyle.Render("✗ No")
		}

		postType := post.PostType
		if len(postType) > 12 {
			postType = postType[:9] + "..."
		}

		line := fmt.Sprintf("%-3s │ %-12s │ %-*s │ %s",
			cacheIndicator,
			typeStyle.Render(postType),
			titleWidth,
			title,
			access,
		)

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
		if len(urlText) > mainWidth-8 {
			urlText = urlText[:mainWidth-11] + "..."
		}
		main.WriteString(fmt.Sprintf("  URL: %s\n", urlStyle.Render(urlText)))
		main.WriteString(fmt.Sprintf("  Published: %s\n", selected.PublishedAt.Format("2006-01-02 15:04")))
	}

	main.WriteString(helpStyle.Render("↑/k ↓/j nav • Enter view • n/→ p/← pages • r/R refresh • c copy • q quit"))

	// Render clipboard panel (4 lines padding to align with title + status + header)
	clipboardPanel := m.renderClipboardPanel(m.height, 4)

	// Join main and clipboard panel side by side
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(mainWidth).Render(main.String()),
		"  ",
		clipboardPanel,
	)
}

func (m Model) viewDetails() string {
	mainWidth := m.width - clipboardPanelWidth - 3
	if mainWidth < 40 {
		mainWidth = 40
	}

	var main strings.Builder

	main.WriteString(titleStyle.Render("🎨 Post Details"))
	main.WriteString("\n\n")
	main.WriteString(m.viewport.View())
	main.WriteString("\n")
	main.WriteString(helpStyle.Render("↑/k ↓/j nav links • a add • A add all • c copy • esc back • q quit"))

	// Render clipboard panel (2 lines padding to align with title)
	clipboardPanel := m.renderClipboardPanel(m.height, 3)

	// Join main and clipboard panel side by side
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(mainWidth).Render(main.String()),
		"  ",
		clipboardPanel,
	)
}

func (m Model) renderDetailsContent() string {
	if m.postDetails == nil {
		return "No details available"
	}

	var b strings.Builder

	b.WriteString(headerStyle.Render(m.postDetails.Title))
	b.WriteString("\n\n")

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

	// Description section
	b.WriteString(headerStyle.Render("📝 Description"))
	b.WriteString("\n")
	if m.postDetails.Description != "" {
		// Word wrap the description
		wrapped := wordWrap(m.postDetails.Description, m.viewport.Width-4)
		b.WriteString(descriptionStyle.Render(wrapped))
	} else {
		b.WriteString(notCachedStyle.Render("  No description available"))
	}
	b.WriteString("\n")

	return b.String()
}

func (m Model) viewError() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("🎨 Patreon Posts Viewer"))
	b.WriteString("\n\n")
	b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("r retry • esc back • q quit"))

	return b.String()
}

// wordWrap wraps text to the specified width
func wordWrap(text string, width int) string {
	if width <= 0 {
		width = 80
	}
	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
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
		if i < len(words)-1 && lineLen > 0 {
			// Continue
		}
	}

	return result.String()
}
