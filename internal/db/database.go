package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Database handles SQLite operations
type Database struct {
	db *sql.DB
}

// Campaign represents a cached campaign
type Campaign struct {
	ID       string
	Name     string
	CachedAt time.Time
}

// CachedPost represents a cached post with extracted content
type CachedPost struct {
	ID                 string
	CampaignID         string
	Type               string
	PostType           string
	Title              string
	PatreonURL         string
	CurrentUserCanView bool
	PublishedAt        time.Time
	Description        string
	YouTubeLinks       string // JSON array of links
	CachedAt           time.Time
	DetailsCached      bool
}

// DefaultDBPath returns the default database path
func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".patreon-posts.db"), nil
}

// Open opens or creates the database
func Open(path string) (*Database, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	d := &Database{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return d, nil
}

// Close closes the database
func (d *Database) Close() error {
	return d.db.Close()
}

func (d *Database) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS campaigns (
		id TEXT PRIMARY KEY,
		name TEXT,
		cached_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY,
		campaign_id TEXT NOT NULL,
		type TEXT,
		post_type TEXT,
		title TEXT,
		patreon_url TEXT,
		current_user_can_view BOOLEAN,
		published_at DATETIME,
		description TEXT,
		youtube_links TEXT,
		cached_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		details_cached BOOLEAN DEFAULT FALSE,
		FOREIGN KEY (campaign_id) REFERENCES campaigns(id)
	);

	CREATE INDEX IF NOT EXISTS idx_posts_campaign ON posts(campaign_id);

	CREATE TABLE IF NOT EXISTS campaign_pages (
		campaign_id TEXT NOT NULL,
		cursor TEXT NOT NULL,
		posts_json TEXT NOT NULL,
		next_cursor TEXT,
		has_more BOOLEAN,
		cached_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (campaign_id, cursor),
		FOREIGN KEY (campaign_id) REFERENCES campaigns(id)
	);
	`

	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// SaveCampaign saves or updates a campaign
func (d *Database) SaveCampaign(id, name string) error {
	_, err := d.db.Exec(`
		INSERT INTO campaigns (id, name, cached_at) 
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET 
			name = excluded.name,
			cached_at = CURRENT_TIMESTAMP
	`, id, name)
	return err
}

// SavePost saves or updates a post (basic info from list)
func (d *Database) SavePost(post *CachedPost) error {
	_, err := d.db.Exec(`
		INSERT INTO posts (id, campaign_id, type, post_type, title, patreon_url, 
			current_user_can_view, published_at, cached_at, details_cached)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, FALSE)
		ON CONFLICT(id) DO UPDATE SET
			type = excluded.type,
			post_type = excluded.post_type,
			title = excluded.title,
			patreon_url = excluded.patreon_url,
			current_user_can_view = excluded.current_user_can_view,
			published_at = excluded.published_at,
			cached_at = CURRENT_TIMESTAMP
	`, post.ID, post.CampaignID, post.Type, post.PostType, post.Title,
		post.PatreonURL, post.CurrentUserCanView, post.PublishedAt)
	return err
}

// SavePostDetails saves the detailed content of a post
func (d *Database) SavePostDetails(postID, description, youtubeLinks string) error {
	_, err := d.db.Exec(`
		UPDATE posts SET 
			description = ?,
			youtube_links = ?,
			details_cached = TRUE
		WHERE id = ?
	`, description, youtubeLinks, postID)
	return err
}

// GetPost retrieves a cached post by ID
func (d *Database) GetPost(postID string) (*CachedPost, error) {
	row := d.db.QueryRow(`
		SELECT id, campaign_id, type, post_type, title, patreon_url,
			current_user_can_view, published_at, description, youtube_links,
			cached_at, details_cached
		FROM posts WHERE id = ?
	`, postID)

	var post CachedPost
	var desc, links sql.NullString
	var publishedAt sql.NullTime

	err := row.Scan(
		&post.ID, &post.CampaignID, &post.Type, &post.PostType,
		&post.Title, &post.PatreonURL, &post.CurrentUserCanView,
		&publishedAt, &desc, &links, &post.CachedAt, &post.DetailsCached,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if publishedAt.Valid {
		post.PublishedAt = publishedAt.Time
	}
	if desc.Valid {
		post.Description = desc.String
	}
	if links.Valid {
		post.YouTubeLinks = links.String
	}

	return &post, nil
}

// GetPostsByCampaign retrieves all cached posts for a campaign
func (d *Database) GetPostsByCampaign(campaignID string) ([]CachedPost, error) {
	rows, err := d.db.Query(`
		SELECT id, campaign_id, type, post_type, title, patreon_url,
			current_user_can_view, published_at, description, youtube_links,
			cached_at, details_cached
		FROM posts WHERE campaign_id = ?
		ORDER BY published_at DESC
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []CachedPost
	for rows.Next() {
		var post CachedPost
		var desc, links sql.NullString
		var publishedAt sql.NullTime

		err := rows.Scan(
			&post.ID, &post.CampaignID, &post.Type, &post.PostType,
			&post.Title, &post.PatreonURL, &post.CurrentUserCanView,
			&publishedAt, &desc, &links, &post.CachedAt, &post.DetailsCached,
		)
		if err != nil {
			return nil, err
		}

		if publishedAt.Valid {
			post.PublishedAt = publishedAt.Time
		}
		if desc.Valid {
			post.Description = desc.String
		}
		if links.Valid {
			post.YouTubeLinks = links.String
		}

		posts = append(posts, post)
	}

	return posts, rows.Err()
}

// IsPostDetailsCached checks if a post has cached details
func (d *Database) IsPostDetailsCached(postID string) (bool, error) {
	var cached bool
	err := d.db.QueryRow(`
		SELECT details_cached FROM posts WHERE id = ?
	`, postID).Scan(&cached)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return cached, err
}

// ClearCampaignCache removes all cached data for a campaign
func (d *Database) ClearCampaignCache(campaignID string) error {
	_, err := d.db.Exec(`DELETE FROM posts WHERE campaign_id = ?`, campaignID)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`DELETE FROM campaigns WHERE id = ?`, campaignID)
	return err
}

// ClearPostDetails clears the cached details for a post
func (d *Database) ClearPostDetails(postID string) error {
	_, err := d.db.Exec(`
		UPDATE posts SET 
			description = NULL,
			youtube_links = NULL,
			details_cached = FALSE
		WHERE id = ?
	`, postID)
	return err
}

// CachedPage represents a cached page of posts
type CachedPage struct {
	CampaignID string
	Cursor     string
	PostsJSON  string
	NextCursor string
	HasMore    bool
	CachedAt   time.Time
}

// SavePage saves a page of posts to the cache
func (d *Database) SavePage(campaignID, cursor, postsJSON, nextCursor string, hasMore bool) error {
	_, err := d.db.Exec(`
		INSERT INTO campaign_pages (campaign_id, cursor, posts_json, next_cursor, has_more, cached_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(campaign_id, cursor) DO UPDATE SET
			posts_json = excluded.posts_json,
			next_cursor = excluded.next_cursor,
			has_more = excluded.has_more,
			cached_at = CURRENT_TIMESTAMP
	`, campaignID, cursor, postsJSON, nextCursor, hasMore)
	return err
}

// GetPage retrieves a cached page of posts
func (d *Database) GetPage(campaignID, cursor string) (*CachedPage, error) {
	row := d.db.QueryRow(`
		SELECT campaign_id, cursor, posts_json, next_cursor, has_more, cached_at
		FROM campaign_pages
		WHERE campaign_id = ? AND cursor = ?
	`, campaignID, cursor)

	var page CachedPage
	var nextCursor sql.NullString

	err := row.Scan(&page.CampaignID, &page.Cursor, &page.PostsJSON, &nextCursor, &page.HasMore, &page.CachedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if nextCursor.Valid {
		page.NextCursor = nextCursor.String
	}

	return &page, nil
}

// ClearCampaignPages clears all cached pages for a campaign
func (d *Database) ClearCampaignPages(campaignID string) error {
	_, err := d.db.Exec(`DELETE FROM campaign_pages WHERE campaign_id = ?`, campaignID)
	return err
}

// ClearPage clears a specific cached page
func (d *Database) ClearPage(campaignID, cursor string) error {
	_, err := d.db.Exec(`DELETE FROM campaign_pages WHERE campaign_id = ? AND cursor = ?`, campaignID, cursor)
	return err
}
