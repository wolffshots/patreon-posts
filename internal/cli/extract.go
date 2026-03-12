package cli

import (
    "encoding/json"
    "errors"
    "fmt"
    "math"
    "math/rand"
    "os"
    "strings"
    "time"

    "golang.org/x/term"

    "patreon-posts/internal/api"
    "patreon-posts/internal/config"
    "patreon-posts/internal/datetime"
    "patreon-posts/internal/db"
)

var (
    delayLineActive bool
    delayLineWidth  int
)

// ExtractYouTubeLinks goes through all campaigns, fetches posts after the given date,
// extracts YouTube links, copies them to clipboard, and prints them to terminal.
// If forceRefresh is true, post details will be re-fetched even if cached.
func ExtractYouTubeLinks(cfg *config.Config, database *db.Database, afterDate string, forceRefresh bool) error {
    if len(cfg.Campaigns) == 0 {
        return fmt.Errorf("no campaigns configured in config file")
    }

    // Parse date filter. Support special value "last" to mean the last time
    // extract-links was run (from the DB history).
    var filterDate time.Time
    afterTrim := strings.TrimSpace(afterDate)
    if strings.EqualFold(afterTrim, "last") {
        // Prefer runs that included --extract-links flag
        lastRun, err := database.GetLastRunByFlag("--extract-links")
        if err != nil {
            return fmt.Errorf("error reading last extract-links run info: %w", err)
        }
        if lastRun == nil {
            return fmt.Errorf("no previous run found that used --extract-links; run once with --extract-links to initialize")
        }
        filterDate = lastRun.RunAt
        logf("[extract] filtering posts after last extract-links run: %s\n", datetime.FormatLocal(filterDate))
    } else if afterTrim != "" {
        parsed, err := datetime.ParseLocal(afterTrim)
        if err != nil {
            return fmt.Errorf("invalid date/time '%s': %w", afterDate, err)
        }
        filterDate = parsed
        logf("[extract] filtering posts after: %s\n", datetime.FormatLocal(filterDate))
    }

	client := api.NewClient(cfg.Cookies)
	minDelayMs := cfg.GetRequestDelayMinMs()
	maxDelayMs := cfg.GetRequestDelayMaxMs()

    logf("[extract] request delays: %dms - %dms\n", minDelayMs, maxDelayMs)
    logf("[extract] processing %d campaign(s)\n\n", len(cfg.Campaigns))

	var allLinks []string
	seenLinks := make(map[string]bool)

    for i, campaign := range cfg.Campaigns {
		campaignName := campaign.Name
		if campaignName == "" {
			campaignName = campaign.ID
		}
        logf("[campaign %d/%d] %s (%s)\n", i+1, len(cfg.Campaigns), campaignName, campaign.ID)

        links, err := extractLinksFromCampaign(client, database, campaign.ID, filterDate, minDelayMs, maxDelayMs, forceRefresh)
        if err != nil {
            logf("[campaign %s] error: %v\n", campaign.ID, err)
            continue
        }

		// Deduplicate links
		for _, link := range links {
			if !seenLinks[link] {
				seenLinks[link] = true
				allLinks = append(allLinks, link)
			}
		}

        logf("[campaign %s] found %d unique YouTube link(s)\n\n", campaign.ID, len(links))

		// Random delay between campaigns
        if i < len(cfg.Campaigns)-1 {
            randomDelay(minDelayMs, maxDelayMs, "between campaigns")
        }
	}

	if len(allLinks) == 0 {
        logln("[summary] no YouTube links found")
		return nil
	}

	// Print links
    logf("\n[summary] YouTube Links (%d total):\n", len(allLinks))
    logln(strings.Repeat("-", 60))
	for _, link := range allLinks {
        logln(link)
	}
    logln(strings.Repeat("-", 60))

	return nil
}

// extractLinksFromCampaign fetches all posts for a campaign and extracts YouTube links
func extractLinksFromCampaign(
    client *api.Client,
    database *db.Database,
    campaignID string,
    filterDate time.Time,
    minDelayMs, maxDelayMs int,
    forceRefresh bool,
) ([]string, error) {
	var allLinks []string
	cursor := ""
	pageCount := 0
	postsProcessed := 0
    postsSkippedByDate := 0
    postsFromCache := 0
    postsFetched := 0
    postsFailed := 0

	for {
		pageCount++
        logf("  [page %d] fetching posts\n", pageCount)

		page, err := client.FetchPosts(campaignID, 50, cursor)
		if err != nil {
			return allLinks, fmt.Errorf("failed to fetch posts: %w", err)
		}
        logf("  [page %d] received %d post(s)\n", pageCount, len(page.Posts))

		// Random delay after fetching page
        randomDelay(minDelayMs, maxDelayMs, "after page fetch")

		// Process posts
        for i, post := range page.Posts {
			// Skip posts before filter date
			if !filterDate.IsZero() && post.PublishedAt.Before(filterDate) {
				// Since posts are sorted by date descending, we can stop early
                postsSkippedByDate++
                logf("  [page %d] reached post older than filter date; stopping early\n", pageCount)
                logf("  [summary] pages=%d processed=%d cache=%d fetched=%d failed=%d skippedByDate=%d links=%d\n",
                    pageCount,
                    postsProcessed,
                    postsFromCache,
                    postsFetched,
                    postsFailed,
                    postsSkippedByDate,
                    len(allLinks),
                )
				return allLinks, nil
			}

			postsProcessed++
            logf(
                "    [post %d] id=%s published=%s type=%s title=\"%s\"\n",
                postsProcessed,
                post.ID,
                post.PublishedAt.Format("2006-01-02 15:04"),
                post.PostType,
                truncate(post.Title, 70),
            )

            // Check if we have cached details and we are not forcing a refresh
            if !forceRefresh {
                cached, err := database.GetPost(post.ID)
                if err == nil && cached != nil && cached.DetailsCached {
                    // Use cached YouTube links
                    cachedCount := 0
                    if cached.YouTubeLinks != "" {
                        var links []string
                        if err := json.Unmarshal([]byte(cached.YouTubeLinks), &links); err == nil {
                            allLinks = append(allLinks, links...)
                            cachedCount = len(links)
                        }
                    }
                    postsFromCache++
                    logf("    [post %d] source=cache links=%d\n", postsProcessed, cachedCount)
                    if i < len(page.Posts)-1 {
                        randomDelay(minDelayMs, maxDelayMs, "between post processing")
                    }
                    continue
                }
            }

            // Fetch post details from API
            logf("    [post %d] source=api fetching details\n", postsProcessed)
            details, err := client.FetchPostDetails(post.ID)
            if err != nil {
                // If authentication issue, return early so caller can handle it
                if errors.Is(err, api.ErrAuthRequired) {
                    logf("    [post %d] auth error\n", postsProcessed)
                    return allLinks, fmt.Errorf("authentication error while fetching post %s: %w", post.ID, err)
                }
                postsFailed++
                logf("    [post %d] fetch failed: %v\n", postsProcessed, err)
                randomDelay(minDelayMs, maxDelayMs, "after post fetch failure")
                continue
            }

            // Cache (or overwrite) the details
            linksJSON, _ := json.Marshal(details.YouTubeLinks)
            database.SavePostDetails(post.ID, details.Description, string(linksJSON))

            allLinks = append(allLinks, details.YouTubeLinks...)
            postsFetched++
            logf("    [post %d] source=api fetched links=%d\n", postsProcessed, len(details.YouTubeLinks))

			// Random delay after each post detail fetch
            if i < len(page.Posts)-1 {
                randomDelay(minDelayMs, maxDelayMs, "between post processing")
            }
		}

        logf("  [page %d] done: processed=%d totalLinks=%d\n", pageCount, postsProcessed, len(allLinks))

		// Check if there are more pages
		if !page.HasMore || page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

    logf("  [summary] pages=%d processed=%d cache=%d fetched=%d failed=%d skippedByDate=%d links=%d\n",
        pageCount,
        postsProcessed,
        postsFromCache,
        postsFetched,
        postsFailed,
        postsSkippedByDate,
        len(allLinks),
    )

	return allLinks, nil
}

// randomDelay sleeps for a random duration between min and max milliseconds
func randomDelay(minMs, maxMs int, label string) {
    delay := chooseDelayMs(minMs, maxMs)
    if delay <= 0 {
        return
    }

    if !isTTY() {
        logf("[delay] %s %s/%s\n", label, formatDuration(time.Duration(delay)*time.Millisecond), formatDuration(time.Duration(delay)*time.Millisecond))
        time.Sleep(time.Duration(delay) * time.Millisecond)
        return
    }

    remainingMs := delay
    total := time.Duration(delay) * time.Millisecond
    for remainingMs > 0 {
        remaining := time.Duration(remainingMs) * time.Millisecond
        writeDelayLine(fmt.Sprintf("[delay] %s %s/%s", label, formatDuration(remaining), formatDuration(total)))

        step := 1000
        if remainingMs < step {
            step = remainingMs
        }
        time.Sleep(time.Duration(step) * time.Millisecond)
        remainingMs -= step
    }
    writeDelayLine(fmt.Sprintf("[delay] %s %s/%s", label, formatDuration(0), formatDuration(total)))
}

func chooseDelayMs(minMs, maxMs int) int {
    if minMs < 0 {
        minMs = 0
    }
    if maxMs < 0 {
        maxMs = 0
    }
    if maxMs <= minMs {
        return minMs
    }
    return minMs + rand.Intn(maxMs-minMs)
}

func isTTY() bool {
    return term.IsTerminal(int(os.Stdout.Fd()))
}

func writeDelayLine(line string) {
    delayLineActive = true
    if len(line) > delayLineWidth {
        delayLineWidth = len(line)
    }
    padded := line + strings.Repeat(" ", delayLineWidth-len(line))
    fmt.Printf("\r%s", padded)
}

func clearDelayLine() {
    if !delayLineActive {
        return
    }
    fmt.Printf("\r%s\r", strings.Repeat(" ", delayLineWidth))
    delayLineActive = false
    delayLineWidth = 0
}

func logf(format string, args ...any) {
    clearDelayLine()
    fmt.Printf(format, args...)
}

func logln(args ...any) {
    clearDelayLine()
    fmt.Println(args...)
}

func formatDuration(d time.Duration) string {
    if d < 0 {
        d = 0
    }
    seconds := int(math.Ceil(d.Seconds()))
    if seconds < 0 {
        seconds = 0
    }
    mins := seconds / 60
    secs := seconds % 60
    return fmt.Sprintf("%02d:%02d", mins, secs)
}

func truncate(s string, maxLen int) string {
    s = strings.TrimSpace(s)
    if maxLen <= 0 || len(s) <= maxLen {
        return s
    }
    if maxLen <= 3 {
        return s[:maxLen]
    }
    return s[:maxLen-3] + "..."
}
