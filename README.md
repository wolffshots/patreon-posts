# Patreon Posts Viewer

A terminal UI application to browse Patreon posts for campaigns you're subscribed to. Features SQLite caching and YouTube link extraction.

## Features

- Browse posts from any Patreon campaign
- View post details with description and embedded content
- **YouTube link extraction** - Automatically finds YouTube videos (including shortlinks)
- **SQLite caching** - Posts and details are cached locally for faster access
- Cache status indicators show which posts have been fetched
- Force refresh option to bypass cache

## Installation

```bash
go build -o patreon-posts .
```

## Usage

### Basic Usage

```bash
# Run with campaign ID prompt
./patreon-posts

# Run with cookies from command line
./patreon-posts --cookies "session_id=abc123; patreon_device_id=xyz789"

# Specify custom database path
./patreon-posts --db /path/to/cache.db
```

### Configuration

Create a config file at `~/.patreon-posts.json`:

```json
{
  "cookies": "session_id=YOUR_SESSION_ID; patreon_device_id=YOUR_DEVICE_ID",
  "campaigns": [
    { "id": "2175699", "name": "Hat Films" },
    { "id": "1234567", "name": "Another Creator" }
  ]
}
```

The `campaigns` array is optional and seeds the database with saved campaigns that appear in the selection list.

Then simply run:

```bash
./patreon-posts
```

### Data Storage

- **Config file**: `~/.patreon-posts.json` - Stores cookies and campaign seeds
- **Database**: `~/.patreon-posts.db` - SQLite cache for posts, pages, and saved campaigns

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
| `n` / `a` | Add new campaign (enter ID manually) |
| `d` / `Delete` | Delete selected campaign |
| `Esc` / `Ctrl+C` | Quit |

Campaigns are automatically saved when you fetch posts from them.

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
| `PgUp` / `PgDn` | Page up/down |
| `R` | Force refresh this post's details |
| `Esc` / `Backspace` | Back to posts list |
| `q` | Quit |

## Clipboard Panel

The right side of the screen shows a clipboard panel where you can collect YouTube links:

- **Add links**: In post details view, navigate to a YouTube link and press `a` or `Enter`
- **Add all**: Press `A` to add all YouTube links from the current post
- **Navigate**: Use `[` and `]` to move through clipboard items
- **Remove**: Press `x` to remove the selected link, or `X` to clear all
- **Copy**: Press `c` or `y` to copy all links to your system clipboard

Links already in the clipboard are marked with ✓ in the post details view.

## Cache Status

In the posts list, the first column shows cache status:
- `✓` (green) - Post details have been fetched and cached
- `·` (gray) - Post details not yet cached

## Finding Campaign IDs

Campaign IDs can be found in the Patreon API URLs. For example:
- `https://www.patreon.com/api/campaigns/2175699/posts` → Campaign ID is `2175699`

You can find this by:
1. Opening Network tab in browser DevTools
2. Navigating to a creator's posts page
3. Looking for API calls to `/api/campaigns/{id}/posts`
