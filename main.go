package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"patreon-posts/internal/config"
	"patreon-posts/internal/db"
	"patreon-posts/internal/ui"
)

func main() {
	// Parse command line flags
	cookiesFlag := flag.String("cookies", "", "Patreon session cookies (or set via config file)")
	configPath := flag.String("config", "", "Path to config file (default: ~/.patreon-posts.json)")
	dbPath := flag.String("db", "", "Path to SQLite database (default: ~/.patreon-posts.db)")
	flag.Parse()

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

	// Warn if no cookies provided
	if cookies == "" {
		fmt.Println("⚠️  No cookies provided. You may not be able to view patron-only content.")
		fmt.Printf("   Set cookies in %s or use --cookies flag.\n\n", cfgPath)
	}

	// Create and run the TUI
	model := ui.NewModel(cookies, database)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		os.Exit(1)
	}
}
