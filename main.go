package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"patreon-posts/internal/cli"
	"patreon-posts/internal/config"
	"patreon-posts/internal/datetime"
	"patreon-posts/internal/db"
	"patreon-posts/internal/ui"
)

func main() {
	// Parse command line flags
	cookiesFlag := flag.String("cookies", "", "Patreon session cookies (or set via config file)")
	configPath := flag.String("config", "", "Path to config file (default: ~/.patreon-posts.json)")
	dbPath := flag.String("db", "", "Path to SQLite database (default: ~/.patreon-posts.db)")
	afterFlag := flag.String("after", "", "Only show posts published after this date/time (YYYY-MM-DD or YYYY-MM-DD HH:mm[:ss]) or 'last'")
	extractLinks := flag.Bool("extract-links", false, "Extract YouTube links from all campaigns and copy to clipboard")
	flag.Parse()

	runFlags := collectRunFlags()

	// Determine config path
	cfgPath := *configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = config.DefaultConfigPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Load config
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Use cookies from flag or config
	cookies := *cookiesFlag
	if cookies == "" {
		cookies = cfg.Cookies
	}

	// Determine database path
	databasePath := *dbPath
	if databasePath == "" {
		var err error
		databasePath, err = db.DefaultDBPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Open database
	database, err := db.Open(databasePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Seed campaigns from config if present
	for _, campaign := range cfg.Campaigns {
		database.SaveCampaign(campaign.ID, campaign.Name)
	}

	// Warn if no cookies provided
	if cookies == "" {
		fmt.Println("⚠️  No cookies provided. You may not be able to view patron-only content.")
		fmt.Printf("   Set cookies in %s or use --cookies flag.\n\n", cfgPath)
	}

	// Use published_after from flag or config
	afterInput := strings.TrimSpace(*afterFlag)
	publishedAfter := afterInput
	if publishedAfter == "" {
		publishedAfter = strings.TrimSpace(cfg.PublishedAfter)
	}

	if strings.EqualFold(publishedAfter, "last") {
		lastRun, err := database.GetLastRun()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading last run info: %v\n", err)
			os.Exit(1)
		}
		if lastRun == nil {
			fmt.Fprintln(os.Stderr, "Error: no previous run found. Run once without --after last to initialize.")
			os.Exit(1)
		}
		publishedAfter = datetime.FormatLocal(lastRun.RunAt)
		fmt.Printf("📅 Using last run time: %s\n", publishedAfter)
	} else if publishedAfter != "" {
		if _, err := datetime.ParseLocal(publishedAfter); err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --after value: %v\n", err)
			os.Exit(1)
		}
	}

	// Handle extract-links mode
	if *extractLinks {
		if err := cli.ExtractYouTubeLinks(cfg, database, publishedAfter); err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting links: %v\n", err)
			os.Exit(1)
		}
		if err := database.SaveLastRun(time.Now(), runFlags); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save last run info: %v\n", err)
		}
		return
	}

	// Create and run the TUI
	model := ui.NewModel(cookies, database, publishedAfter)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		os.Exit(1)
	}

	if err := database.SaveLastRun(time.Now(), runFlags); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to save last run info: %v\n", err)
	}
}

func collectRunFlags() []string {
	var flags []string
	flag.CommandLine.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "cookies":
			flags = append(flags, "--cookies=REDACTED")
		case "extract-links":
			if f.Value.String() == "true" {
				flags = append(flags, "--extract-links")
			} else {
				flags = append(flags, fmt.Sprintf("--%s=%s", f.Name, f.Value.String()))
			}
		default:
			flags = append(flags, fmt.Sprintf("--%s=%s", f.Name, f.Value.String()))
		}
	})
	return flags
}
