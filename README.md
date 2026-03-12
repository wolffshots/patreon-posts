# Patreon Posts Viewer

A terminal UI application to browse Patreon posts for campaigns you're subscribed to. Features SQLite caching, YouTube link extraction, and a batch CLI mode.

## Features

- Browse posts from any Patreon campaign in an interactive TUI
- View post details with readable description and embedded content
- **Post type display** — shows `video_embed`, `podcast`, `image_file`, `text_only`, etc. in both the list and detail views
- **ProseMirror content parsing** — renders Patreon's rich-text JSON format (`content_json_string`) as readable text, preserving paragraph structure
- **YouTube link extraction** — finds YouTube videos across all URL formats:
  - `youtube.com/watch?v=`, `/embed/`, `/live/`, `/shorts/`, `/v/`
  - `youtu.be/` short links
  - Links embedded in post text (via link marks in the JSON content)
  - Embed iframe HTML (`embed.html`) and embed direct URL (`embed.url`)
- **Embed metadata** — for `video_embed` posts, shows the embed provider, title, and URL in the description section
- **SQLite caching** — posts and details are cached locally for faster access
- Cache status indicators show which posts have been fetched and cached
- Force refresh option to bypass cache (global `-R` or per-post `R`)
- **Batch extraction mode** (`--extract-links`) — non-interactive CLI that iterates all configured campaigns, extracts YouTube links, and prints them to stdout

## Installation

```bash
go build -o patreon-posts .
```

## Usage

### Basic Usage

```bash
# Run the interactive TUI
./patreon-posts

# Run with cookies from command line
./patreon-posts --cookies "session_id=abc123; patreon_device_id=xyz789"

# Only show posts published after a date
./patreon-posts --after 2024-01-01

# Only show posts since the last time the app was run
./patreon-posts --after last

# Specify custom config and database paths
./patreon-posts --config /path/to/config.json --db /path/to/cache.db
```

### Batch YouTube Link Extraction

The `--extract-links` flag runs non-interactively. It iterates all campaigns listed in the config file, fetches post details, extracts YouTube links, and prints them to stdout.

During extraction, the CLI now provides detailed runtime progress:
- Campaign and page progress logs
- Per-post processing logs (post ID, publish time, type, title)
- Source path logs (`cache` vs `api`)
- In-place delay countdown in a static line using `remaining/total` format (for example `00:03/00:05`)

When output is redirected to a file or pipe, delay updates fall back to regular log lines (no carriage-return animation).

```bash
# Extract links from all configured campaigns
./patreon-posts --extract-links

# Only extract links from posts published after a date
./patreon-posts --extract-links --after 2024-01-01

# Only extract links from posts newer than the last extraction run
./patreon-posts --extract-links --after last

# Force re-fetch post details even if cached
./patreon-posts --extract-links --force-refresh
```

### All Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--cookies` | `""` | Patreon session cookies string |
| `--config` | `~/.patreon-posts.json` | Path to config file |
| `--db` | `~/.patreon-posts.db` | Path to SQLite database |
| `--after` | `""` | Filter posts to those published after this date (`YYYY-MM-DD`, `YYYY-MM-DD HH:mm[:ss]`, or `last`) |
| `--extract-links` | `false` | Run in batch extraction mode instead of TUI |
| `--force-refresh` | `false` | Re-fetch post details even if already cached (used with `--extract-links`) |

### Configuration

Create a config file at `~/.patreon-posts.json`:

```json
{
  "cookies": "session_id=YOUR_SESSION_ID; patreon_device_id=YOUR_DEVICE_ID",
  "campaigns": [
    { "id": "2175699", "name": "Hat Films" },
    { "id": "1234567", "name": "Another Creator" }
  ],
  "request_delay_min_ms": 500,
  "request_delay_max_ms": 1500
}
```

| Field | Description |
|-------|-------------|
| `cookies` | Patreon session cookies (see below) |
| `campaigns` | Optional list of campaign seeds. These are saved into the database and appear in the TUI selection list on first launch |
| `request_delay_min_ms` | Minimum random delay (ms) between API requests in `--extract-links` mode (default 500) |
| `request_delay_max_ms` | Maximum random delay (ms) between API requests in `--extract-links` mode (default 1500) |

Campaigns added through the TUI are persisted in the SQLite database and do not need to be listed in the config file.

### Data Storage

- **Config file**: `~/.patreon-posts.json` — Stores cookies, campaign seeds, and request delay settings
- **Database**: `~/.patreon-posts.db` — SQLite cache for posts, pages, details, saved campaigns, and run history

### Getting Your Cookies

1. Open your browser's Developer Tools (F12)
2. Go to the Network tab
3. Navigate to a Patreon page you're logged into
4. Find a request to `patreon.com/api/...`
5. Copy the `Cookie` header value

**Required cookies:**
- `session_id` - Your session token
- `patreon_device_id` - Device identifier

## Controls

### Campaign Selection

When you start the app, you'll see a list of saved campaigns (if any):

| Key | Action |
|-----|--------|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `Enter` | Select campaign and load posts |
| `n` / `a` | Add new campaign (enter ID, optional name, optional date filter) |
| `f` | Edit the date filter for the current session |
| `d` / `Delete` | Delete selected campaign |
| `c` / `y` | Copy collected clipboard links to system clipboard |
| `x` | Remove selected clipboard link |
| `X` | Clear entire clipboard |
| `[` / `]` | Navigate clipboard panel |
| `Esc` / `Ctrl+C` | Quit |

Campaigns are saved when you first fetch posts from them and persist in the database.

### Posts List

| Key | Action |
|-----|--------|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `Enter` | View post details (fetches & caches YouTube links) |
| `n` / `→` / `l` | Next page |
| `p` / `←` / `h` | Previous page |
| `r` | Refresh current page |
| `R` | **Force refresh** (clear cache, back to page 1) |
| `c` / `y` | Copy clipboard links to system clipboard |
| `x` | Remove selected link from clipboard |
| `X` | Clear entire clipboard |
| `[` / `]` | Navigate clipboard |
| `Esc` | Go back to campaign selection |
| `q` / `Ctrl+C` | Quit |

### Post Details View

The detail view shows:
- **Post title** and **post type** (e.g. `video_embed`, `podcast`, `image_file`, `text_only`)
- **YouTube Links** — extracted from all available sources (post body text, link marks in rich-text content, embed URL, embed iframe HTML). If no YouTube links are found, a clear message is shown.
- **Description** — the post's readable text content, including embed metadata (provider, title, and direct URL) for `video_embed` posts. Preserves paragraph breaks from the original rich-text.

| Key | Action |
|-----|--------|
| `↑` / `k` | Navigate YouTube links |
| `↓` / `j` | Navigate YouTube links |
| `a` / `Enter` | Add selected YouTube link to clipboard |
| `A` | Add ALL YouTube links to clipboard |
| `c` / `y` | Copy clipboard links to system clipboard |
| `x` | Remove selected link from clipboard |
| `X` | Clear entire clipboard |
| `[` / `]` | Navigate clipboard |
| `PgUp` / `PgDn` | Scroll description |
| `R` | Force refresh this post's details (bypass cache) |
| `Esc` / `Backspace` | Back to posts list |
| `q` | Quit |

## Clipboard Panel

The right side of the screen shows a clipboard panel where you can collect YouTube links:

- **Add links**: In post details view, navigate to a YouTube link and press `a` or `Enter`
- **Add all**: Press `A` to add all YouTube links from the current post
- **Navigate**: Use `[` and `]` to move through clipboard items
- **Remove**: Press `x` to remove the selected link, or `X` to clear all
- **Copy**: Press `c` or `y` to copy all links to your system clipboard (one URL per line)

Links already in the clipboard are marked with ✓ in the post details view.

## Cache Status

In the posts list, the first column shows cache status:
- `✓` (green) — Post details have been fetched and cached
- `·` (gray) — Post details not yet fetched

Cached post details include the parsed description, embed metadata, and extracted YouTube links. Use `R` on any cached post to force a re-fetch.

## YouTube Link Detection

Links are extracted from all available sources:

| Source | Example |
|--------|---------|
| Post body text and link marks | `https://youtu.be/VIDEO_ID` in the post description |
| Embed direct URL | `embed.url` field (e.g. `youtube.com/live/VIDEO_ID`) |
| Embed iframe HTML | `embed.html` `<iframe src="...embed/VIDEO_ID...">` |

Supported URL formats: `/watch?v=`, `/embed/`, `/live/`, `/shorts/`, `/v/`, `youtu.be/`

Duplicate video IDs are deduplicated — all formats resolve to a canonical `https://www.youtube.com/watch?v=VIDEO_ID` URL.

## Finding Campaign IDs

Campaign IDs can be found in Patreon API URLs. For example:
- `https://www.patreon.com/api/campaigns/2175699/posts` → Campaign ID is `2175699`

You can find this by:
1. Opening Network tab in browser DevTools
2. Navigating to a creator's posts page on Patreon
3. Looking for API calls to `/api/campaigns/{id}/posts`
