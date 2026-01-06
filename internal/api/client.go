package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"patreon-posts/internal/models"
)

// YouTube URL patterns
var youtubePatterns = []*regexp.Regexp{
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/watch\?v=([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/embed/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://youtu\.be/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/v/([a-zA-Z0-9_-]{11})`),
	regexp.MustCompile(`https?://(?:www\.)?youtube\.com/shorts/([a-zA-Z0-9_-]{11})`),
}

const baseURL = "https://www.patreon.com/api"

// Client handles Patreon API requests
type Client struct {
	httpClient *http.Client
	cookies    string
}

// NewClient creates a new Patreon API client
func NewClient(cookies string) *Client {
	return &Client{
		httpClient: &http.Client{},
		cookies:    cookies,
	}
}

// FetchPosts retrieves posts for a given campaign ID with pagination support
// cursor can be empty string or "null" for the first page
func (c *Client) FetchPosts(campaignID string, count int, cursor string) (*models.PostsPage, error) {
	endpoint := fmt.Sprintf("%s/campaigns/%s/posts", baseURL, campaignID)

	params := url.Values{}
	params.Set("include", "user.campaign.current_user_pledge,access_rules.tier.null,moderator_actions,primary_image")
	params.Set("fields[post]", "commenter_count,current_user_can_view,image,thumbnail,insights_last_updated_at,patreon_url,post_type,published_at,title,upgrade_url,view_count,is_preview_blurred")
	params.Set("fields[access_rule]", "access_rule_type")
	params.Set("fields[reward]", "amount_cents,id")
	params.Set("fields[user]", "[]")
	params.Set("fields[campaign]", "[]")
	params.Set("fields[pledge]", "amount_cents")
	params.Set("fields[primary-image]", "image_icon,image_small,image_medium,image_large,primary_image_type,alt_text,image_colors,is_fallback,prefer_alternate_display,id")

	// Handle cursor for pagination
	if cursor == "" {
		params.Set("page[cursor]", "null")
	} else {
		params.Set("page[cursor]", cursor)
	}

	params.Set("page[count]", fmt.Sprintf("%d", count))
	params.Set("filter[is_by_creator]", "true")
	params.Set("filter[contains_exclusive_posts]", "true")
	params.Set("sort", "-recency_weighted_engagement")
	params.Set("json-api-use-default-includes", "false")
	params.Set("json-api-version", "1.0")

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
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
	endpoint := fmt.Sprintf("%s/posts/%s", baseURL, postID)

	params := url.Values{}
	params.Set("fields[post]", "content,embed,title,post_type,published_at,patreon_url")
	params.Set("json-api-version", "1.0")

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var detailResp models.PostDetailResponse
	if err := json.Unmarshal(body, &detailResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	details := &models.PostDetails{
		ID:          detailResp.Data.ID,
		Title:       detailResp.Data.Attributes.Title,
		Content:     detailResp.Data.Attributes.Content,
		PostType:    detailResp.Data.Attributes.PostType,
		PublishedAt: detailResp.Data.Attributes.PublishedAt,
	}

	// Extract YouTube links from content and embed
	allContent := details.Content
	if detailResp.Data.Attributes.Embed.URL != "" {
		allContent += " " + detailResp.Data.Attributes.Embed.URL
	}
	details.YouTubeLinks = ExtractYouTubeLinks(allContent)

	// Strip HTML for description
	details.Description = stripHTML(details.Content)

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
