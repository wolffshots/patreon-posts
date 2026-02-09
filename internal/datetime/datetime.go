package datetime

import (
    "fmt"
    "strings"
    "time"
)

const (
    DateLayout            = "2006-01-02"
    DateTimeLayout        = "2006-01-02 15:04:05"
    DateTimeNoSecondsLayout = "2006-01-02 15:04"
)

// ParseLocal parses a date or date-time in local time.
// Supported formats:
// - YYYY-MM-DD
// - YYYY-MM-DD HH:mm
// - YYYY-MM-DD HH:mm:ss
func ParseLocal(input string) (time.Time, error) {
    trimmed := strings.TrimSpace(input)
    if trimmed == "" {
        return time.Time{}, fmt.Errorf("empty date/time")
    }

    layouts := []string{DateTimeLayout, DateTimeNoSecondsLayout, DateLayout}
    for _, layout := range layouts {
        if parsed, err := time.ParseInLocation(layout, trimmed, time.Local); err == nil {
            return parsed, nil
        }
    }

    return time.Time{}, fmt.Errorf("invalid date/time %q, expected YYYY-MM-DD or YYYY-MM-DD HH:mm[:ss]", input)
}

// FormatLocal formats a time in local time using the full date-time layout.
func FormatLocal(t time.Time) string {
    return t.Local().Format(DateTimeLayout)
}
