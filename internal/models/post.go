package models

import "time"

// PatreonResponse represents the top-level API response
type PatreonResponse struct {
	Data []PostData `json:"data"`
}

// PostData represents a single post in the response
type PostData struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Attributes PostAttributes `json:"attributes"`
}

// PostAttributes contains the post details we care about
type PostAttributes struct {
	PostType           string    `json:"post_type"`
	Title              string    `json:"title"`
	PatreonURL         string    `json:"patreon_url"`
	CurrentUserCanView bool      `json:"current_user_can_view"`
	PublishedAt        time.Time `json:"published_at"`
	CommenterCount     int       `json:"commenter_count"`
}

// Post is a simplified view of the post for display
type Post struct {
	ID                 string
	Type               string
	PostType           string
	Title              string
	PatreonURL         string
	CurrentUserCanView bool
	PublishedAt        time.Time
	DetailsCached      bool // Whether the post details have been fetched and cached
}

// FromPostData converts API response data to our simplified Post model
func FromPostData(data PostData) Post {
	return Post{
		ID:                 data.ID,
		Type:               data.Type,
		PostType:           data.Attributes.PostType,
		Title:              data.Attributes.Title,
		PatreonURL:         data.Attributes.PatreonURL,
		CurrentUserCanView: data.Attributes.CurrentUserCanView,
		PublishedAt:        data.Attributes.PublishedAt,
		DetailsCached:      false,
	}
}

// PostDetailResponse represents the API response for a single post
type PostDetailResponse struct {
	Data PostDetailData `json:"data"`
}

// PostDetailData represents the post data in detail response
type PostDetailData struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"`
	Attributes PostDetailAttributes `json:"attributes"`
}

// PostDetailAttributes contains detailed post attributes
type PostDetailAttributes struct {
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	PostType    string    `json:"post_type"`
	PublishedAt time.Time `json:"published_at"`
	Embed       Embed     `json:"embed"`
}

// Embed contains embedded content info
type Embed struct {
	URL         string `json:"url"`
	Provider    string `json:"provider"`
	Description string `json:"description"`
}

// PostDetails contains the extracted details from a post
type PostDetails struct {
	ID           string
	Title        string
	Content      string
	Description  string // HTML-stripped content
	PostType     string
	PublishedAt  time.Time
	YouTubeLinks []string
}
