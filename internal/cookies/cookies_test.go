package cookies

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestExpiryTime(t *testing.T) {
	// Firefox stores seconds, Zen stores milliseconds. Both must land in 2026.
	secs := expiryTime(1795023244)
	if secs.Year() != 2026 {
		t.Errorf("seconds expiry: got year %d, want 2026", secs.Year())
	}
	millis := expiryTime(1795023244470)
	if millis.Year() != 2026 {
		t.Errorf("milliseconds expiry: got year %d, want 2026", millis.Year())
	}
	if !secs.Equal(millis.Truncate(time.Second)) {
		t.Errorf("the two units disagree: %s vs %s", secs, millis)
	}
	if !expiryTime(0).IsZero() {
		t.Error("a zero expiry marks a session cookie and must yield the zero time")
	}
}

// writeJar builds a moz_cookies database shaped like a real Firefox profile.
func writeJar(t *testing.T, rows [][]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cookies.sqlite")

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.Exec(`CREATE TABLE moz_cookies (
		id INTEGER PRIMARY KEY, originAttributes TEXT NOT NULL DEFAULT '',
		name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER,
		lastAccessed INTEGER, creationTime INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := database.Exec(`INSERT INTO moz_cookies
			(originAttributes, name, value, host, path, expiry, lastAccessed, creationTime)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, r...); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestReadJar(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).UnixMilli()
	past := time.Now().Add(-24 * time.Hour).UnixMilli()
	now := time.Now().UnixMicro()

	path := writeJar(t, [][]any{
		// A login inside a container, as Zen workspaces produce. Must be kept.
		{"^userContextId=1", "session_id", "live", "www.patreon.com", "/", future, now, now},
		{"^userContextId=1", "patreon_device_id", "dev", "www.patreon.com", "/", future, now, now},
		// Expired: sending it helps nobody.
		{"^userContextId=1", "dead_cookie", "x", "www.patreon.com", "/", past, now, now},
		// Third-party partitioned copy: never sent on first-party requests.
		{"^userContextId=1&partitionKey=%28https%2Cexample.com%29", "session_id", "partitioned", "www.patreon.com", "/", future, now + 500, now},
		// A different site entirely.
		{"", "session_id", "other", "www.example.com", "/", future, now, now},
	})

	jar, err := readJar(path)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(jar.Header, "session_id=live") {
		t.Errorf("container cookie missing from header: %q", jar.Header)
	}
	if strings.Contains(jar.Header, "partitioned") {
		t.Errorf("partitioned cookie must be excluded: %q", jar.Header)
	}
	if strings.Contains(jar.Header, "dead_cookie") {
		t.Errorf("expired cookie must be excluded: %q", jar.Header)
	}
	if strings.Contains(jar.Header, "other") {
		t.Errorf("non-patreon cookie must be excluded: %q", jar.Header)
	}
	if len(jar.Names) != 2 {
		t.Errorf("got %d cookies %v, want 2", len(jar.Names), jar.Names)
	}
	if jar.SessionExpiry.IsZero() {
		t.Error("session expiry should have been recorded")
	}
}

func TestReadJarNewestValueWins(t *testing.T) {
	future := time.Now().Add(24 * time.Hour).UnixMilli()
	old := time.Now().Add(-time.Hour).UnixMicro()
	fresh := time.Now().UnixMicro()

	// Same cookie name in two containers: the recently used one is the live one.
	path := writeJar(t, [][]any{
		{"^userContextId=2", "session_id", "stale", "www.patreon.com", "/", future, old, old},
		{"^userContextId=1", "session_id", "current", "www.patreon.com", "/", future, fresh, fresh},
	})

	jar, err := readJar(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jar.Header, "session_id=current") {
		t.Errorf("expected the most recently used value, got %q", jar.Header)
	}
}

// A signed-out profile still holds long-lived cookies like the device id. Those
// must not come back as a usable jar: the caller would store them over a working
// session and lose the fallback.
func TestReadJarExpiredSessionYieldsNothing(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).UnixMilli()
	future := time.Now().Add(365 * 24 * time.Hour).UnixMilli()
	now := time.Now().UnixMicro()

	path := writeJar(t, [][]any{
		{"^userContextId=1", "session_id", "dead", "www.patreon.com", "/", past, now, now},
		{"^userContextId=1", "patreon_device_id", "abc", "www.patreon.com", "/", future, now, now},
		{"^userContextId=1", "patreon_locale_code", "en-US", "www.patreon.com", "/", future, now, now},
	})

	jar, err := readJar(path)
	if err != nil {
		t.Fatal(err)
	}
	if jar.Header != "" {
		t.Errorf("got %q, want an empty jar when the session has expired", jar.Header)
	}
}

func TestResolveExpiredSessionIsReported(t *testing.T) {
	past := time.Now().Add(-24 * time.Hour).UnixMilli()
	future := time.Now().Add(365 * 24 * time.Hour).UnixMilli()
	now := time.Now().UnixMicro()

	path := writeJar(t, [][]any{
		{"^userContextId=1", "session_id", "dead", "www.patreon.com", "/", past, now, now},
		{"^userContextId=1", "patreon_device_id", "abc", "www.patreon.com", "/", future, now, now},
	})

	_, err := Resolve(Source{Type: "zen", Path: path})
	if err == nil {
		t.Fatal("an expired session must be reported, not returned as usable cookies")
	}
	if !strings.Contains(err.Error(), "sign in") {
		t.Errorf("error %q should tell the user what to do about it", err)
	}
}

func TestReadJarNoPatreonCookies(t *testing.T) {
	path := writeJar(t, [][]any{
		{"", "session_id", "x", "www.example.com", "/", time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMicro(), 0},
	})
	jar, err := readJar(path)
	if err != nil {
		t.Fatal(err)
	}
	if jar.Header != "" {
		t.Errorf("expected an empty jar, got %q", jar.Header)
	}
}

func TestResolveTypes(t *testing.T) {
	// An unset source is not an error: the stored cookie string is used instead.
	if jar, err := Resolve(Source{}); err != nil || jar.Header != "" {
		t.Errorf("empty type: got (%q, %v), want empty jar and no error", jar.Header, err)
	}
	if _, err := Resolve(Source{Type: "chrome"}); err == nil {
		t.Error("an unsupported browser should be rejected, not silently ignored")
	}

	t.Setenv(EnvVarName, "session_id=from-env")
	jar, err := Resolve(Source{Type: "env"})
	if err != nil {
		t.Fatal(err)
	}
	if jar.Header != "session_id=from-env" {
		t.Errorf("got %q, want the env var value", jar.Header)
	}

	t.Setenv(EnvVarName, "")
	if _, err := Resolve(Source{Type: "env"}); err == nil {
		t.Error("an empty env var should be reported, not returned as empty cookies")
	}
}

func TestResolveMissingPath(t *testing.T) {
	if _, err := Resolve(Source{Type: "firefox", Path: filepath.Join(os.TempDir(), "nope", "cookies.sqlite")}); err == nil {
		t.Error("a missing cookie database should be an error")
	}
}
