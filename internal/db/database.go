package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"patreon-posts/internal/datetime"

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

	// One connection serialises every statement. The TUI writes from an
	// extraction goroutine while the update loop writes the post cache, and two
	// connections would race for SQLite's write lock and fail with "database is
	// locked". No caller nests a query inside an open rows loop, which is the
	// one thing a single connection cannot do.
	db.SetMaxOpenConns(1)

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

	CREATE TABLE IF NOT EXISTS app_state (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		last_run_at TEXT NOT NULL,
		last_run_flags TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS app_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_at TEXT NOT NULL,
		run_flags TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS link_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at TEXT NOT NULL,
		after TEXT NOT NULL,
		finished_at TEXT,
		error TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS run_links (
		run_id INTEGER NOT NULL,
		position INTEGER NOT NULL,
		link TEXT NOT NULL,
		campaign_id TEXT NOT NULL,
		post_id TEXT NOT NULL,
		post_title TEXT NOT NULL,
		published_at TEXT NOT NULL,
		PRIMARY KEY (run_id, position),
		FOREIGN KEY (run_id) REFERENCES link_runs(id)
	);
	`

	_, err := d.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// LastRunInfo represents metadata about the most recent run.
type LastRunInfo struct {
	RunAt time.Time
	Flags []string
}

// SaveLastRun stores the last run timestamp and provided flags.
func (d *Database) SaveLastRun(runAt time.Time, flags []string) error {
	flagsJSON, err := json.Marshal(flags)
	if err != nil {
		return fmt.Errorf("failed to marshal run flags: %w", err)
	}

	runAtStr := datetime.FormatLocal(runAt)
	// Insert into run history
	if _, err = d.db.Exec(`
        INSERT INTO app_runs (run_at, run_flags) VALUES (?, ?)
    `, runAtStr, string(flagsJSON)); err != nil {
		return fmt.Errorf("failed to insert run history: %w", err)
	}

	// Update single-row app_state for quick access to last run
	_, err = d.db.Exec(`
        INSERT INTO app_state (id, last_run_at, last_run_flags)
        VALUES (1, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            last_run_at = excluded.last_run_at,
            last_run_flags = excluded.last_run_flags
    `, runAtStr, string(flagsJSON))
	if err != nil {
		return fmt.Errorf("failed to save last run info: %w", err)
	}
	return nil
}

// GetLastRunByFlag returns the most recent run record where the flags text
// contains the provided substring (simple heuristic). If no matching run is
// found, returns (nil, nil).
func (d *Database) GetLastRunByFlag(flagSubstring string) (*LastRunInfo, error) {
	row := d.db.QueryRow(`
        SELECT run_at, run_flags
        FROM app_runs
        WHERE run_flags LIKE ?
        ORDER BY run_at DESC
        LIMIT 1
    `, "%"+flagSubstring+"%")

	var runAtStr string
	var flagsJSON string
	if err := row.Scan(&runAtStr, &flagsJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	runAt, err := datetime.ParseLocal(runAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse run timestamp %q: %w", runAtStr, err)
	}

	var flags []string
	if err := json.Unmarshal([]byte(flagsJSON), &flags); err != nil {
		return nil, fmt.Errorf("failed to parse run flags: %w", err)
	}

	return &LastRunInfo{RunAt: runAt, Flags: flags}, nil
}

// GetLastRun retrieves the most recent run metadata.
func (d *Database) GetLastRun() (*LastRunInfo, error) {
	row := d.db.QueryRow(`
        SELECT last_run_at, last_run_flags
        FROM app_state
        WHERE id = 1
    `)

	var runAtStr string
	var flagsJSON string
	if err := row.Scan(&runAtStr, &flagsJSON); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	runAt, err := datetime.ParseLocal(runAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse last run timestamp %q: %w", runAtStr, err)
	}

	var flags []string
	if err := json.Unmarshal([]byte(flagsJSON), &flags); err != nil {
		return nil, fmt.Errorf("failed to parse last run flags: %w", err)
	}

	return &LastRunInfo{RunAt: runAt, Flags: flags}, nil
}

// LinkRun is one pass of link extraction and the links it found, in the order
// it printed them.
type LinkRun struct {
	ID         int64
	StartedAt  time.Time
	After      string    // the filter the run used, empty for none
	FinishedAt time.Time // zero while the run is going, or when it died
	Error      string
	Links      []RunLink
}

// RunLink is one link a run found, with the post it came from.
type RunLink struct {
	URL         string
	CampaignID  string
	PostID      string
	PostTitle   string
	PublishedAt time.Time
}

// StartLinkRun records a run before it finds anything, so the links it saves
// as it goes survive the run being killed.
func (d *Database) StartLinkRun(startedAt time.Time, after string) (int64, error) {
	res, err := d.db.Exec(`INSERT INTO link_runs (started_at, after) VALUES (?, ?)`,
		datetime.FormatLocal(startedAt), after)
	if err != nil {
		return 0, fmt.Errorf("failed to record link run: %w", err)
	}
	return res.LastInsertId()
}

// AddRunLink stores the link at position in a run's list.
func (d *Database) AddRunLink(runID int64, position int, l RunLink) error {
	_, err := d.db.Exec(`
		INSERT INTO run_links (run_id, position, link, campaign_id, post_id, post_title, published_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, runID, position, l.URL, l.CampaignID, l.PostID, l.PostTitle, datetime.FormatLocal(l.PublishedAt))
	return err
}

// FinishLinkRun marks a run as over. runErr is what cut it short, if anything.
func (d *Database) FinishLinkRun(runID int64, finishedAt time.Time, runErr string) error {
	_, err := d.db.Exec(`UPDATE link_runs SET finished_at = ?, error = ? WHERE id = ?`,
		datetime.FormatLocal(finishedAt), runErr, runID)
	return err
}

// ListLinkRuns returns every run with its links, newest run first.
func (d *Database) ListLinkRuns() ([]LinkRun, error) {
	rows, err := d.db.Query(`
		SELECT id, started_at, after, COALESCE(finished_at, ''), error
		FROM link_runs ORDER BY started_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	var runs []LinkRun
	index := map[int64]int{}
	for rows.Next() {
		var r LinkRun
		var started, finished string
		if err := rows.Scan(&r.ID, &started, &r.After, &finished, &r.Error); err != nil {
			rows.Close()
			return nil, err
		}
		r.StartedAt, _ = datetime.ParseLocal(started)
		if finished != "" {
			r.FinishedAt, _ = datetime.ParseLocal(finished)
		}
		index[r.ID] = len(runs)
		runs = append(runs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// A second query rather than one per run, and after the first has closed,
	// because the pool holds a single connection.
	rows, err = d.db.Query(`
		SELECT run_id, link, campaign_id, post_id, post_title, published_at
		FROM run_links ORDER BY run_id, position
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var l RunLink
		var published string
		if err := rows.Scan(&id, &l.URL, &l.CampaignID, &l.PostID, &l.PostTitle, &published); err != nil {
			return nil, err
		}
		l.PublishedAt, _ = datetime.ParseLocal(published)
		if i, ok := index[id]; ok {
			runs[i].Links = append(runs[i].Links, l)
		}
	}
	return runs, rows.Err()
}

// SaveCampaign saves or updates a campaign
func (d *Database) SaveCampaign(id, name string) error {
	_, err := d.db.Exec(`
		INSERT INTO campaigns (id, name, cached_at) 
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET 
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE campaigns.name END,
			cached_at = CURRENT_TIMESTAMP
	`, id, name)
	return err
}

// SavedCampaign represents a saved campaign for selection
type SavedCampaign struct {
	ID       string
	Name     string
	CachedAt time.Time
}

// ListCampaigns returns all saved campaigns
func (d *Database) ListCampaigns() ([]SavedCampaign, error) {
	rows, err := d.db.Query(`
		SELECT id, COALESCE(name, ''), cached_at
		FROM campaigns
		ORDER BY cached_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var campaigns []SavedCampaign
	for rows.Next() {
		var c SavedCampaign
		if err := rows.Scan(&c.ID, &c.Name, &c.CachedAt); err != nil {
			return nil, err
		}
		campaigns = append(campaigns, c)
	}
	return campaigns, rows.Err()
}

// GetCampaign retrieves a campaign by ID
func (d *Database) GetCampaign(id string) (*SavedCampaign, error) {
	row := d.db.QueryRow(`
		SELECT id, COALESCE(name, ''), cached_at
		FROM campaigns WHERE id = ?
	`, id)

	var c SavedCampaign
	err := row.Scan(&c.ID, &c.Name, &c.CachedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteCampaign removes a campaign and all its data
func (d *Database) DeleteCampaign(id string) error {
	// Delete pages first
	if _, err := d.db.Exec(`DELETE FROM campaign_pages WHERE campaign_id = ?`, id); err != nil {
		return err
	}
	// Delete posts
	if _, err := d.db.Exec(`DELETE FROM posts WHERE campaign_id = ?`, id); err != nil {
		return err
	}
	// Delete campaign
	_, err := d.db.Exec(`DELETE FROM campaigns WHERE id = ?`, id)
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
