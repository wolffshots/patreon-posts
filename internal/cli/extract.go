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
	"patreon-posts/internal/models"
)

var (
	delayLineActive bool
	delayLineWidth  int
)

// Reporter receives an extraction's progress. The CLI prints it. The TUI shows
// it inside the list the run is filling. Every field is optional.
type Reporter struct {
	Log    func(text string)               // log output, newlines included
	Status func(line string)               // a line that replaces the last one, such as a countdown
	Start  func(run db.LinkRun)            // the run now exists in the database
	Link   func(runID int64, l db.RunLink) // the run found a new link
}

// Terminal reports to stdout. On a terminal it redraws the countdown in place.
// On a pipe it leaves Status unset, so a countdown prints one plain line.
func Terminal() Reporter {
	r := Reporter{Log: func(text string) {
		clearDelayLine()
		fmt.Print(text)
	}}
	if isTTY() {
		r.Status = writeDelayLine
	}
	return r
}

func (r Reporter) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log(fmt.Sprintf(format, args...))
	}
}

// ExtractYouTubeLinks goes through all campaigns, fetches posts after the given
// date, extracts YouTube links and reports them. Each link is saved to the
// database as it is found, grouped under a run that starts now, so a run that
// is killed part way still leaves its links behind.
// If forceRefresh is true, post details will be re-fetched even if cached.
func ExtractYouTubeLinks(cfg *config.Config, database *db.Database, afterDate string, forceRefresh bool, r Reporter) error {
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
		r.logf("[extract] filtering posts after last extract-links run: %s\n", datetime.FormatLocal(filterDate))
	} else if afterTrim != "" {
		parsed, err := datetime.ParseLocal(afterTrim)
		if err != nil {
			return fmt.Errorf("invalid date/time '%s': %w", afterDate, err)
		}
		filterDate = parsed
		r.logf("[extract] filtering posts after: %s\n", datetime.FormatLocal(filterDate))
	}

	after := ""
	if !filterDate.IsZero() {
		after = datetime.FormatLocal(filterDate)
	}
	run := db.LinkRun{StartedAt: time.Now(), After: after}
	runID, err := database.StartLinkRun(run.StartedAt, after)
	if err != nil {
		return err
	}
	run.ID = runID
	if r.Start != nil {
		r.Start(run)
	}
	// failures names each campaign that did not finish, so the saved list
	// says it may be short.
	var failures []string
	defer func() {
		if err := database.FinishLinkRun(runID, time.Now(), strings.Join(failures, "; ")); err != nil {
			r.logf("[extract] warning: failed to mark run finished: %v\n", err)
		}
	}()

	client := api.NewClient(cfg.Cookies)
	minDelayMs := cfg.GetRequestDelayMinMs()
	maxDelayMs := cfg.GetRequestDelayMaxMs()

	r.logf("[extract] request delays: %dms - %dms\n", minDelayMs, maxDelayMs)
	r.logf("[extract] processing %d campaign(s)\n\n", len(cfg.Campaigns))

	var allLinks []string
	seenLinks := make(map[string]bool)

	for i, campaign := range cfg.Campaigns {
		campaignName := campaign.Name
		if campaignName == "" {
			campaignName = campaign.ID
		}
		r.logf("[campaign %d/%d] %s (%s)\n", i+1, len(cfg.Campaigns), campaignName, campaign.ID)

		found := func(post models.Post, links []string) {
			for _, link := range dedupe(links, seenLinks) {
				l := db.RunLink{URL: link, CampaignID: campaign.ID, PostID: post.ID, PostTitle: post.Title, PublishedAt: post.PublishedAt}
				if err := database.AddRunLink(runID, len(allLinks), l); err != nil {
					r.logf("    [extract] warning: failed to save %s: %v\n", link, err)
				}
				allLinks = append(allLinks, link)
				if r.Link != nil {
					r.Link(runID, l)
				}
			}
		}

		links, err := extractLinksFromCampaign(client, database, campaign.ID, filterDate, minDelayMs, maxDelayMs, forceRefresh, r, found)
		if err != nil {
			r.logf("[campaign %s] error: %v\n", campaign.ID, err)
			failures = append(failures, fmt.Sprintf("%s: %v", campaignName, err))

			// One campaign failing on its own is survivable, but a rate limit or
			// a dead session applies to every campaign. Carrying on would just
			// repeat the same failure three more times.
			var rl *api.RateLimitError
			if errors.As(err, &rl) || errors.Is(err, api.ErrAuthRequired) {
				r.logf("[extract] stopping: the remaining campaigns would hit the same error\n")
				break
			}
			continue
		}

		r.logf("[campaign %s] found %d unique YouTube link(s)\n\n", campaign.ID, len(links))

		// Random delay between campaigns
		if i < len(cfg.Campaigns)-1 {
			r.randomDelay(minDelayMs, maxDelayMs, "between campaigns")
		}
	}

	if len(allLinks) == 0 {
		r.logf("[summary] no YouTube links found\n")
	} else {
		r.logf("\n[summary] YouTube Links (%d total):\n", len(allLinks))
		r.logf("%s\n", strings.Repeat("-", 60))
		for _, link := range allLinks {
			r.logf("%s\n", link)
		}
		r.logf("%s\n", strings.Repeat("-", 60))
	}

	// A failed campaign is an error, so the caller does not record this run
	// for --after last and the next run covers the posts it missed.
	if len(failures) > 0 {
		return fmt.Errorf("%d campaign(s) failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

// dedupe returns the links not already in seen, recording them as it goes.
func dedupe(links []string, seen map[string]bool) []string {
	var out []string
	for _, link := range links {
		if !seen[link] {
			seen[link] = true
			out = append(out, link)
		}
	}
	return out
}

// extractLinksFromCampaign fetches all posts for a campaign and extracts YouTube
// links. It hands each post's links to found as soon as it has them.
func extractLinksFromCampaign(
	client *api.Client,
	database *db.Database,
	campaignID string,
	filterDate time.Time,
	minDelayMs, maxDelayMs int,
	forceRefresh bool,
	r Reporter,
	found func(post models.Post, links []string),
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
		r.logf("  [page %d] fetching posts\n", pageCount)

		page, err := withRateLimitRetry(r, fmt.Sprintf("page %d", pageCount), func() (*models.PostsPage, error) {
			return client.FetchPosts(campaignID, 50, cursor)
		})
		if err != nil {
			return allLinks, fmt.Errorf("failed to fetch posts: %w", err)
		}
		r.logf("  [page %d] received %d post(s)\n", pageCount, len(page.Posts))

		// Random delay after fetching page
		r.randomDelay(minDelayMs, maxDelayMs, "after page fetch")

		// Process posts
		for i, post := range page.Posts {
			// Skip posts before filter date
			if !filterDate.IsZero() && post.PublishedAt.Before(filterDate) {
				// Since posts are sorted by date descending, we can stop early
				postsSkippedByDate++
				r.logf("  [page %d] reached post older than filter date; stopping early\n", pageCount)
				r.logf("  [summary] pages=%d processed=%d cache=%d fetched=%d failed=%d skippedByDate=%d links=%d\n",
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
			r.logf(
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
							found(post, links)
							cachedCount = len(links)
						}
					}
					postsFromCache++
					r.logf("    [post %d] source=cache links=%d\n", postsProcessed, cachedCount)
					if i < len(page.Posts)-1 {
						r.randomDelay(minDelayMs, maxDelayMs, "between post processing")
					}
					continue
				}
			}

			// Fetch post details from API
			r.logf("    [post %d] source=api fetching details\n", postsProcessed)
			details, err := withRateLimitRetry(r, "post "+post.ID, func() (*models.PostDetails, error) {
				return client.FetchPostDetails(post.ID)
			})
			if err != nil {
				// If authentication issue, return early so caller can handle it
				if errors.Is(err, api.ErrAuthRequired) {
					r.logf("    [post %d] auth error\n", postsProcessed)
					return allLinks, fmt.Errorf("authentication error while fetching post %s: %w", post.ID, err)
				}
				// A rate limit that outlived the retry loop means backing off
				// further is pointless; keeping on would only make it worse.
				var rl *api.RateLimitError
				if errors.As(err, &rl) {
					r.logf("    [post %d] still rate limited, stopping campaign\n", postsProcessed)
					return allLinks, err
				}
				postsFailed++
				r.logf("    [post %d] fetch failed: %v\n", postsProcessed, err)
				r.randomDelay(minDelayMs, maxDelayMs, "after post fetch failure")
				continue
			}

			// Cache (or overwrite) the details
			linksJSON, _ := json.Marshal(details.YouTubeLinks)
			database.SavePostDetails(post.ID, details.Description, string(linksJSON))

			allLinks = append(allLinks, details.YouTubeLinks...)
			found(post, details.YouTubeLinks)
			postsFetched++
			r.logf("    [post %d] source=api fetched links=%d\n", postsProcessed, len(details.YouTubeLinks))

			// Random delay after each post detail fetch
			if i < len(page.Posts)-1 {
				r.randomDelay(minDelayMs, maxDelayMs, "between post processing")
			}
		}

		r.logf("  [page %d] done: processed=%d totalLinks=%d\n", pageCount, postsProcessed, len(allLinks))

		// Check if there are more pages
		if !page.HasMore || page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	r.logf("  [summary] pages=%d processed=%d cache=%d fetched=%d failed=%d skippedByDate=%d links=%d\n",
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
func (r Reporter) randomDelay(minMs, maxMs int, label string) {
	delay := chooseDelayMs(minMs, maxMs)
	if delay <= 0 {
		return
	}
	r.countdown("[delay] "+label, time.Duration(delay)*time.Millisecond)
}

// countdown waits for the given duration, redrawing the status line each
// second, or logging one static line when the reporter has no status line.
func (r Reporter) countdown(prefix string, total time.Duration) {
	if total <= 0 {
		return
	}

	if r.Status == nil {
		r.logf("%s %s/%s\n", prefix, formatDuration(total), formatDuration(total))
		time.Sleep(total)
		return
	}

	remaining := total
	for remaining > 0 {
		r.Status(fmt.Sprintf("%s %s/%s", prefix, formatDuration(remaining), formatDuration(total)))

		step := time.Second
		if remaining < step {
			step = remaining
		}
		time.Sleep(step)
		remaining -= step
	}
	r.Status(fmt.Sprintf("%s %s/%s", prefix, formatDuration(0), formatDuration(total)))
}

// Rate limit policy. These are deliberately fixed: they are a backoff ceiling,
// not a preference, and a user-tunable value here only risks hammering Patreon.
const (
	rateLimitMaxAttempts = 5
	rateLimitMaxWait     = 15 * time.Minute
	rateLimitBaseWait    = 60 * time.Second
)

// withRateLimitRetry runs fn, waiting and retrying while Patreon reports a rate
// limit. It honours Retry-After when given and backs off linearly when not. It
// gives up rather than waiting when the server asks for longer than the cap.
func withRateLimitRetry[T any](r Reporter, label string, fn func() (T, error)) (T, error) {
	var zero T
	for attempt := 1; ; attempt++ {
		result, err := fn()

		var rl *api.RateLimitError
		if !errors.As(err, &rl) {
			return result, err
		}

		if attempt >= rateLimitMaxAttempts {
			return zero, fmt.Errorf("%s: %w (gave up after %d attempts)", label, err, attempt)
		}

		wait := rl.RetryAfter
		if wait <= 0 {
			wait = time.Duration(attempt) * rateLimitBaseWait
		}
		if wait > rateLimitMaxWait {
			return zero, fmt.Errorf("%s: %w (asked to wait %s, over the %s cap)",
				label, err, wait.Round(time.Second), rateLimitMaxWait)
		}

		source := "Retry-After"
		if rl.RetryAfter <= 0 {
			source = "backoff"
		}
		r.logf("[rate-limit] %s: HTTP %d, waiting %s (%s), attempt %d/%d\n",
			label, rl.StatusCode, formatDuration(wait), source, attempt+1, rateLimitMaxAttempts)
		r.countdown(fmt.Sprintf("[rate-limit] %s resuming in", label), wait)
	}
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
