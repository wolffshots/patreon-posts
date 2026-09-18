// Package cookies reads Patreon session cookies from a local browser profile so
// the session does not have to be pasted into the config by hand.
//
// Only Firefox-family browsers are supported. They store cookie values as plain
// text in a SQLite file (moz_cookies), so no key material is needed. Chromium
// browsers encrypt values and are deliberately not handled here.
package cookies

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// EnvVarName holds a ready-made Cookie header when Type is "env". This is the
// hook for running in a container, where no browser profile exists.
//
// ponytail: "env" is the whole container story for now. It reads the header once
// at startup, so a session that dies mid-run still needs a restart. If the
// companion-app idea happens, add a Source type that polls or subscribes to it
// and have the api client re-read cookies on ErrAuthRequired.
const EnvVarName = "PATREON_COOKIES"

// Source describes where to read cookies from.
type Source struct {
	// Type selects the reader: firefox, zen, librewolf, waterfox, env.
	// An empty Type disables lookup and the stored cookie string is used as-is.
	Type string `json:"type"`
	// Path points straight at a cookies.sqlite file or at a profile directory.
	// When set it overrides the built-in per-browser profile roots.
	Path string `json:"path,omitempty"`
	// Profile names a directory under the browser's profile root. When empty
	// the profile holding the most recently used Patreon session wins.
	Profile string `json:"profile,omitempty"`
}

// Jar is the result of a lookup.
type Jar struct {
	Header        string    // ready to use as the Cookie request header
	Names         []string  // cookie names found, for logging
	SessionExpiry time.Time // expiry of session_id, zero if absent or a session cookie
	LastAccessed  time.Time // most recent access of session_id
	Profile       string    // profile directory the cookies came from
}

// patreonHosts are the first-party hosts whose cookies Patreon's web API reads.
var patreonHosts = []string{"www.patreon.com", ".patreon.com", "patreon.com"}

// Resolve returns the cookies for the given source. A source with an empty Type
// returns an empty Jar and no error, so callers can fall back to a stored value.
func Resolve(src Source) (Jar, error) {
	switch strings.ToLower(strings.TrimSpace(src.Type)) {
	case "", "none":
		return Jar{}, nil
	case "env":
		v := strings.TrimSpace(os.Getenv(EnvVarName))
		if v == "" {
			return Jar{}, fmt.Errorf("cookie source is %q but %s is empty", src.Type, EnvVarName)
		}
		return Jar{Header: v, Profile: EnvVarName}, nil
	case "firefox", "zen", "librewolf", "waterfox":
		return resolveFirefox(src)
	default:
		return Jar{}, fmt.Errorf("unknown cookie source type %q (want firefox, zen, librewolf, waterfox or env)", src.Type)
	}
}

func resolveFirefox(src Source) (Jar, error) {
	// An explicit path wins: it is either the jar itself or a profile directory.
	if src.Path != "" {
		path := src.Path
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path = filepath.Join(path, "cookies.sqlite")
		}
		jar, err := readJar(path)
		if err != nil {
			return Jar{}, err
		}
		if jar.Header == "" {
			return Jar{}, fmt.Errorf("no live Patreon session in %s; sign in to Patreon in that browser", path)
		}
		return jar, nil
	}

	root, err := profileRoot(src.Type)
	if err != nil {
		return Jar{}, err
	}

	// A named profile is used directly so the choice stays predictable.
	if src.Profile != "" {
		path := filepath.Join(root, src.Profile, "cookies.sqlite")
		jar, err := readJar(path)
		if err != nil {
			return Jar{}, err
		}
		if jar.Header == "" {
			return Jar{}, fmt.Errorf("no live Patreon session in profile %q (%s); sign in to Patreon in that browser", src.Profile, path)
		}
		jar.Profile = src.Profile
		return jar, nil
	}

	// Otherwise pick the profile whose Patreon session was used most recently.
	// Profiles are often stale, so newest-wins avoids silently reading a dead one.
	entries, err := os.ReadDir(root)
	if err != nil {
		return Jar{}, fmt.Errorf("failed to list %s profiles in %s: %w", src.Type, root, err)
	}
	var best Jar
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		jar, err := readJar(filepath.Join(root, e.Name(), "cookies.sqlite"))
		if err != nil || jar.Header == "" {
			continue
		}
		jar.Profile = e.Name()
		if jar.LastAccessed.After(best.LastAccessed) {
			best = jar
		}
	}
	if best.Header == "" {
		return Jar{}, fmt.Errorf("no %s profile under %s has a live Patreon session; sign in to Patreon there, or set cookie_source.profile", src.Type, root)
	}
	return best, nil
}

// profileRoot returns the directory holding profile directories for a browser.
func profileRoot(browser string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	browser = strings.ToLower(browser)

	// Directory names differ per platform and per browser fork.
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		if browser == "firefox" {
			candidates = []string{filepath.Join(appData, "Mozilla", "Firefox", "Profiles")}
		} else {
			candidates = []string{filepath.Join(appData, browser, "Profiles")}
		}
	case "darwin":
		support := filepath.Join(home, "Library", "Application Support")
		if browser == "firefox" {
			candidates = []string{filepath.Join(support, "Firefox", "Profiles")}
		} else {
			candidates = []string{filepath.Join(support, browser, "Profiles")}
		}
	default: // linux and friends
		if browser == "firefox" {
			candidates = []string{
				filepath.Join(home, ".mozilla", "firefox"),
				filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox"),
				filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox"),
			}
		} else {
			candidates = []string{filepath.Join(home, "."+browser)}
		}
	}

	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("no %s profile directory found (looked in %s)", browser, strings.Join(candidates, ", "))
}

// readJar copies the cookie database aside and reads the Patreon cookies from it.
// The copy is needed because the browser keeps the original open while running.
func readJar(path string) (Jar, error) {
	if _, err := os.Stat(path); err != nil {
		return Jar{}, fmt.Errorf("cookie database not found at %s: %w", path, err)
	}

	tmp, err := os.MkdirTemp("", "patreon-cookies-")
	if err != nil {
		return Jar{}, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// The -wal file carries writes not yet folded into the main file, so copy
	// the whole set or a recent login can be missed.
	local := filepath.Join(tmp, "cookies.sqlite")
	if err := copyFile(path, local); err != nil {
		return Jar{}, fmt.Errorf("failed to copy cookie database: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = copyFile(path+suffix, local+suffix)
	}

	database, err := sql.Open("sqlite", local)
	if err != nil {
		return Jar{}, fmt.Errorf("failed to open cookie database: %w", err)
	}
	defer database.Close()

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(patreonHosts)), ",")
	args := make([]any, len(patreonHosts))
	for i, h := range patreonHosts {
		args[i] = h
	}

	// Partitioned cookies belong to Patreon embedded in some other site and are
	// not sent on first-party requests, so they are excluded. Container cookies
	// (userContextId) are kept: a login often lives in a container or workspace.
	query := `
		SELECT name, value, expiry, lastAccessed
		FROM moz_cookies
		WHERE host IN (` + placeholders + `)
		  AND originAttributes NOT LIKE '%partitionKey=%'
		ORDER BY lastAccessed ASC
	`
	rows, err := database.Query(query, args...)
	if err != nil {
		return Jar{}, fmt.Errorf("failed to query cookies: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	values := map[string]string{}
	var order []string
	var jar Jar
	var hasSession bool

	for rows.Next() {
		var name, value string
		var expiry, lastAccessed int64
		if err := rows.Scan(&name, &value, &expiry, &lastAccessed); err != nil {
			return Jar{}, err
		}
		exp := expiryTime(expiry)
		if !exp.IsZero() && exp.Before(now) {
			continue // already dead, sending it helps nobody
		}
		if _, seen := values[name]; !seen {
			order = append(order, name)
		}
		// Rows arrive oldest first, so a later row overwrites with a fresher value.
		values[name] = value
		if name == "session_id" {
			hasSession = true
			jar.SessionExpiry = exp
			jar.LastAccessed = time.UnixMicro(lastAccessed)
		}
	}
	if err := rows.Err(); err != nil {
		return Jar{}, err
	}
	// session_id is the only cookie that authenticates. Without a live one the
	// rest are worthless, and returning them would let a caller overwrite a
	// stored working session with a signed-out one.
	if len(order) == 0 || !hasSession {
		return Jar{}, nil
	}

	parts := make([]string, 0, len(order))
	for _, name := range order {
		parts = append(parts, name+"="+values[name])
	}
	jar.Header = strings.Join(parts, "; ")
	jar.Names = order
	return jar, nil
}

// expiryTime converts a moz_cookies expiry to a time. Firefox stores seconds,
// but some forks (Zen) store milliseconds, so the unit is inferred by magnitude.
// A zero expiry marks a session cookie and yields the zero time.
func expiryTime(v int64) time.Time {
	switch {
	case v <= 0:
		return time.Time{}
	case v > 1e12: // seconds this large would be the year 33658
		return time.UnixMilli(v)
	default:
		return time.Unix(v, 0)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
