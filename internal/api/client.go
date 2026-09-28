package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"patreon-posts/internal/models"
)

// YouTube URL patterns
var youtubePatterns = []*regexp.Regexp{
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/watch\?v=([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/embed/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://youtu\.be/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/v/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/shorts/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/live/([a-zA-Z0-9_-]{11})`),
}

// pmNode represents a node in a ProseMirror JSON document
type pmNode struct {
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	Content []pmNode `json:"content"`
	Marks   []pmMark `json:"marks"`
}

// pmMark represents a mark (e.g. link, bold) applied to a text node
type pmMark struct {
	Type  string  `json:"type"`
	Attrs pmAttrs `json:"attrs"`
}

// pmAttrs holds the attributes for ProseMirror marks and nodes
type pmAttrs struct {
	Href string `json:"href"`
}

// parseContentJSON walks a ProseMirror JSON document string and returns
// human-readable plain text and all href values found in link marks.
func parseContentJSON(jsonStr string) (text string, links []string) {
	if jsonStr == "" {
		return "", nil
	}
	var doc pmNode
	if err := json.Unmarshal([]byte(jsonStr), &doc); err != nil {
		return "", nil
	}

	var sb strings.Builder
	var hrefs []string

	var walk func(node pmNode)
	walk = func(node pmNode) {
		switch node.Type {
		case "text":
			sb.WriteString(node.Text)
			for _, mark := range node.Marks {
				if mark.Type == "link" && mark.Attrs.Href != "" {
					hrefs = append(hrefs, mark.Attrs.Href)
				}
			}
		case "hardBreak":
			sb.WriteString("\n")
		case "paragraph", "heading":
			for _, child := range node.Content {
				walk(child)
			}
			sb.WriteString("\n")
		case "listItem":
			sb.WriteString("• ")
			for _, child := range node.Content {
				walk(child)
			}
		default:
			for _, child := range node.Content {
				walk(child)
			}
		}
	}

	walk(doc)
	return strings.TrimSpace(sb.String()), hrefs
}

const defaultBaseURL = "https://www.patreon.com/api"

// Client handles Patreon API requests
type Client struct {
	httpClient *http.Client
	cookies    string
	baseURL    string
}

// NewClient creates a new Patreon API client
func NewClient(cookies string) *Client {
	return &Client{
		// A timeout, so an unattended run cannot hang on a stalled connection.
		httpClient: &http.Client{Timeout: time.Minute},
		cookies:    cookies,
		baseURL:    defaultBaseURL,
	}
}

// ErrAuthRequired is returned when the API indicates authentication is required
// (e.g., cookies missing, expired, or otherwise invalid).
var ErrAuthRequired = errors.New("authentication required; cookies may be invalid or expired")

// RateLimitError is returned when Patreon asks the client to slow down.
// RetryAfter is zero when the response carried no usable Retry-After header.
type RateLimitError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limited by Patreon (HTTP %d), retry after %s", e.StatusCode, e.RetryAfter.Round(time.Second))
	}
	return fmt.Sprintf("rate limited by Patreon (HTTP %d), no Retry-After given", e.StatusCode)
}

// parseRetryAfter reads a Retry-After header, which RFC 9110 allows to be either
// a delay in seconds or an HTTP date. An unparsable or past value yields zero.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// isAuthError reports whether a failed response means the cookies are invalid.
// Only the status line is trusted: body text is not, because Patreon's generic
// error pages mention words like "session" and "login" regardless of the cause,
// which would misreport rate limits and server errors as auth failures.
func isAuthError(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return true
	}
	// A redirect to a login page means the session is gone.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		loc := strings.ToLower(resp.Header.Get("Location"))
		return strings.Contains(loc, "login") || strings.Contains(loc, "signin")
	}
	return false
}

// truncateBody keeps error messages readable when Patreon returns an HTML page.
func truncateBody(body []byte) string {
	const max = 200
	s := strings.TrimSpace(string(body))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// doRequest performs a GET and classifies the outcome. Both fetchers share it so
// auth and rate-limit handling cannot drift apart between them.
func (c *Client) doRequest(fullURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, nil
	case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode == http.StatusServiceUnavailable:
		return nil, &RateLimitError{
			StatusCode: resp.StatusCode,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	case isAuthError(resp):
		return nil, ErrAuthRequired
	default:
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, truncateBody(body))
	}
}

// FetchPosts retrieves posts for a given campaign ID with pagination support
// cursor can be empty string or "null" for the first page
func (c *Client) FetchPosts(campaignID string, count int, cursor string) (*models.PostsPage, error) {
	endpoint := fmt.Sprintf("%s/campaigns/%s/posts", c.baseURL, campaignID)

	params := url.Values{}
	// Only request the fields we actually use
	params.Set("fields[post]", "current_user_can_view,patreon_url,post_type,published_at,title")
	// No includes needed - we don't use any related data
	params.Set("json-api-use-default-includes", "false")

	// Handle cursor for pagination
	if cursor == "" {
		params.Set("page[cursor]", "null")
	} else {
		params.Set("page[cursor]", cursor)
	}

	params.Set("page[count]", fmt.Sprintf("%d", count))
	params.Set("filter[is_by_creator]", "true")
	params.Set("filter[contains_exclusive_posts]", "true")

	// Sort by most recent first
	params.Set("sort", "-published_at")
	params.Set("json-api-version", "1.0")

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	body, err := c.doRequest(fullURL)
	if err != nil {
		return nil, err
	}

	var patreonResp models.PatreonResponse
	if err := json.Unmarshal(body, &patreonResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	posts := make([]models.Post, len(patreonResp.Data))
	for i, data := range patreonResp.Data {
		posts[i] = models.FromPostData(data)
	}

	// Extract cursor from links.next URL if present
	nextCursor := extractCursorFromURL(patreonResp.Links.Next)
	hasMore := patreonResp.Links.Next != ""

	page := &models.PostsPage{
		Posts:      posts,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      0, // Patreon doesn't provide total count
	}

	return page, nil
}

// extractCursorFromURL parses the page[cursor] parameter from a Patreon next URL
func extractCursorFromURL(nextURL string) string {
	if nextURL == "" {
		return ""
	}

	parsed, err := url.Parse(nextURL)
	if err != nil {
		return ""
	}

	// Get the page[cursor] query parameter
	cursor := parsed.Query().Get("page[cursor]")
	return cursor
}

// FetchPostDetails retrieves the full content of a single post
func (c *Client) FetchPostDetails(postID string) (*models.PostDetails, error) {
	endpoint := fmt.Sprintf("%s/posts/%s", c.baseURL, postID)

	params := url.Values{}
	params.Set("fields[post]", "content,content_json_string,embed,title,post_type,published_at,patreon_url")
	params.Set("json-api-version", "1.0")

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	body, err := c.doRequest(fullURL)
	if err != nil {
		return nil, err
	}

	var detailResp models.PostDetailResponse
	if err := json.Unmarshal(body, &detailResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Determine content: prefer HTML content field, fall back to ProseMirror JSON
	content := detailResp.Data.Attributes.Content
	var parsedLinks []string
	isHTML := content != ""
	if content == "" && detailResp.Data.Attributes.ContentJSONString != "" {
		content, parsedLinks = parseContentJSON(detailResp.Data.Attributes.ContentJSONString)
	}

	embed := detailResp.Data.Attributes.Embed

	details := &models.PostDetails{
		ID:          detailResp.Data.ID,
		Title:       detailResp.Data.Attributes.Title,
		Content:     content,
		PostType:    detailResp.Data.Attributes.PostType,
		PublishedAt: detailResp.Data.Attributes.PublishedAt,
	}

	// Search all available content sources for YouTube links
	linkSources := []string{content}
	linkSources = append(linkSources, parsedLinks...)
	if embed.URL != "" {
		linkSources = append(linkSources, embed.URL)
	}
	if embed.HTML != "" {
		linkSources = append(linkSources, embed.HTML)
	}
	details.YouTubeLinks = ExtractYouTubeLinks(strings.Join(linkSources, " "))

	// Build human-readable description
	var descParts []string
	var textContent string
	if isHTML {
		textContent = stripHTML(content)
	} else {
		textContent = content
	}
	// Fall back to embed description if we have no text content
	if textContent == "" && embed.Description != "" {
		textContent = embed.Description
	}
	if textContent != "" {
		descParts = append(descParts, textContent)
	}
	// Append embed metadata when present
	if embed.Subject != "" || embed.URL != "" {
		var embedInfo strings.Builder
		if embed.Provider != "" {
			embedInfo.WriteString("[" + embed.Provider + "]")
		}
		if embed.Subject != "" {
			if embedInfo.Len() > 0 {
				embedInfo.WriteString(" ")
			}
			embedInfo.WriteString(`"` + embed.Subject + `"`)
		}
		if embed.URL != "" {
			if embedInfo.Len() > 0 {
				embedInfo.WriteString("\n")
			}
			embedInfo.WriteString(embed.URL)
		}
		descParts = append(descParts, embedInfo.String())
	}
	details.Description = strings.Join(descParts, "\n\n")

	return details, nil
}

// ExtractYouTubeLinks finds all YouTube video URLs in the given text
func ExtractYouTubeLinks(content string) []string {
	seen := make(map[string]bool)
	var links []string

	for _, pattern := range youtubePatterns {
		matches := pattern.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				videoID := match[1]
				fullURL := "https://www.youtube.com/watch?v=" + videoID
				if !seen[videoID] {
					seen[videoID] = true
					links = append(links, fullURL)
				}
			}
		}
	}

	return links
}

// Pre-compiled regexes for HTML stripping
var (
	scriptTagRe  = regexp.MustCompile(`(?i)<script[^>]*>[\s\S]*?</script>`)
	styleTagRe   = regexp.MustCompile(`(?i)<style[^>]*>[\s\S]*?</style>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
	whitespaceRe = regexp.MustCompile(`\s+`)
)

// stripHTML removes HTML tags and decodes entities
func stripHTML(html string) string {
	// Remove script tags with content
	html = scriptTagRe.ReplaceAllString(html, "")

	// Remove style tags with content
	html = styleTagRe.ReplaceAllString(html, "")

	// Remove HTML tags
	text := htmlTagRe.ReplaceAllString(html, "")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")

	// Clean up whitespace
	text = whitespaceRe.ReplaceAllString(text, " ")
	text = strings.TrimSpace(text)

	return text
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:146.0) Gecko/20100101 Firefox/146.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	// Note: Don't set Accept-Encoding manually - Go's http.Transport handles it automatically
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("Referer", "https://www.patreon.com/")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	if c.cookies != "" {
		req.Header.Set("Cookie", c.cookies)
	}
}
