package cli

import (
    "encoding/json"
    "errors"
    "fmt"
    "math/rand"
    "strings"
    "time"

    "patreon-posts/internal/api"
    "patreon-posts/internal/config"
    "patreon-posts/internal/datetime"
    "patreon-posts/internal/db"
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
        fmt.Printf("📅 Filtering posts after last extract-links run: %s\n", datetime.FormatLocal(filterDate))
    } else if afterTrim != "" {
        parsed, err := datetime.ParseLocal(afterTrim)
        if err != nil {
            return fmt.Errorf("invalid date/time '%s': %w", afterDate, err)
        }
        filterDate = parsed
        fmt.Printf("📅 Filtering posts after: %s\n", datetime.FormatLocal(filterDate))
    }

	client := api.NewClient(cfg.Cookies)
	minDelayMs := cfg.GetRequestDelayMinMs()
	maxDelayMs := cfg.GetRequestDelayMaxMs()

	fmt.Printf("⏱️  Request delays: %dms - %dms\n", minDelayMs, maxDelayMs)
	fmt.Printf("📦 Processing %d campaign(s)...\n\n", len(cfg.Campaigns))

	var allLinks []string
	seenLinks := make(map[string]bool)

	for _, campaign := range cfg.Campaigns {
		campaignName := campaign.Name
		if campaignName == "" {
			campaignName = campaign.ID
		}
		fmt.Printf("🎯 Campaign: %s\n", campaignName)

        links, err := extractLinksFromCampaign(client, database, campaign.ID, filterDate, minDelayMs, maxDelayMs, forceRefresh)
        if err != nil {
            fmt.Printf("   ⚠️  Error: %v\n", err)
            continue
        }

		// Deduplicate links
		for _, link := range links {
			if !seenLinks[link] {
				seenLinks[link] = true
				allLinks = append(allLinks, link)
			}
		}

		fmt.Printf("   ✅ Found %d unique YouTube link(s)\n\n", len(links))

		// Random delay between campaigns
		randomDelay(minDelayMs, maxDelayMs)
	}

	if len(allLinks) == 0 {
		fmt.Println("❌ No YouTube links found")
		return nil
	}

	// Print links
	fmt.Printf("\n🎬 YouTube Links (%d total):\n", len(allLinks))
	fmt.Println(strings.Repeat("─", 60))
	for _, link := range allLinks {
		fmt.Println(link)
	}
	fmt.Println(strings.Repeat("─", 60))

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

	for {
		pageCount++
		fmt.Printf("   📄 Fetching page %d...\n", pageCount)

		page, err := client.FetchPosts(campaignID, 50, cursor)
		if err != nil {
			return allLinks, fmt.Errorf("failed to fetch posts: %w", err)
		}

		// Random delay after fetching page
		randomDelay(minDelayMs, maxDelayMs)

		// Process posts
		for _, post := range page.Posts {
			// Skip posts before filter date
			if !filterDate.IsZero() && post.PublishedAt.Before(filterDate) {
				// Since posts are sorted by date descending, we can stop early
				fmt.Printf("   ⏭️  Reached posts before filter date, stopping\n")
				return allLinks, nil
			}

			postsProcessed++

            // Check if we have cached details and we are not forcing a refresh
            if !forceRefresh {
                cached, err := database.GetPost(post.ID)
                if err == nil && cached != nil && cached.DetailsCached {
                    // Use cached YouTube links
                    if cached.YouTubeLinks != "" {
                        var links []string
                        if err := json.Unmarshal([]byte(cached.YouTubeLinks), &links); err == nil {
                            allLinks = append(allLinks, links...)
                        }
                    }
                    continue
                }
            }

            // Fetch post details from API
            details, err := client.FetchPostDetails(post.ID)
            if err != nil {
                // If authentication issue, return early so caller can handle it
                if errors.Is(err, api.ErrAuthRequired) {
                    return allLinks, fmt.Errorf("authentication error while fetching post %s: %w", post.ID, err)
                }
                fmt.Printf("   ⚠️  Failed to fetch post %s: %v\n", post.ID, err)
                randomDelay(minDelayMs, maxDelayMs)
                continue
            }

            // Cache (or overwrite) the details
            linksJSON, _ := json.Marshal(details.YouTubeLinks)
            database.SavePostDetails(post.ID, details.Description, string(linksJSON))

            allLinks = append(allLinks, details.YouTubeLinks...)

			// Random delay after each post detail fetch
			randomDelay(minDelayMs, maxDelayMs)
		}

		fmt.Printf("   📊 Processed %d posts so far\n", postsProcessed)

		// Check if there are more pages
		if !page.HasMore || page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	return allLinks, nil
}

// randomDelay sleeps for a random duration between min and max milliseconds
func randomDelay(minMs, maxMs int) {
	if maxMs <= minMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	delay := minMs + rand.Intn(maxMs-minMs)
	time.Sleep(time.Duration(delay) * time.Millisecond)
}
