// Package timeutil pins scheduling and source timestamps to Asia/Hong_Kong.
package timeutil

import (
	"time"
	_ "time/tzdata"
)

const DefaultCron = "0 8 * * *"

// HKT is the only zone used for schedules and for naive timestamps from source pages.
var HKT = mustLoc("Asia/Hong_Kong")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// ParseSourceTime interprets a naive page timestamp as Hong Kong time and returns UTC.
func ParseSourceTime(value string) *time.Time {
	value = trim(value)
	if value == "" {
		return nil
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		time.RFC3339,
	}
	for _, layout := range layouts {
		var (
			t   time.Time
			err error
		)
		if layout == time.RFC3339 {
			t, err = time.Parse(layout, value)
		} else {
			t, err = time.ParseInLocation(layout, value, HKT)
		}
		if err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

// ParseLastMod parses a sitemap lastmod date or RFC3339 timestamp.
// A date-only value is midnight in Hong Kong.
func ParseLastMod(value string) (time.Time, bool) {
	value = trim(value)
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.ParseInLocation("2006-01-02", value, HKT); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// OnOrAfterDay reports whether mod falls on the same Hong Kong calendar day as since, or later.
// A zero mod (undated sitemap entry) is included.
func OnOrAfterDay(mod, since time.Time) bool {
	if mod.IsZero() {
		return true
	}
	return mod.In(HKT).Format("2006-01-02") >= since.In(HKT).Format("2006-01-02")
}

func trim(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
