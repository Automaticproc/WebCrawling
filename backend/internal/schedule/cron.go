// Package schedule evaluates 5-field cron expressions in Asia/Hong_Kong.
package schedule

import (
	"time"

	"github.com/robfig/cron/v3"

	"catalog/internal/timeutil"
)

// Next returns the next fire time strictly after now, evaluated in Hong Kong.
func Next(expr string, now time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(now.In(timeutil.HKT)), nil
}

// Valid reports whether expr is a standard 5-field cron expression.
func Valid(expr string) bool {
	_, err := cron.ParseStandard(expr)
	return err == nil
}

// Due reports whether expr fires during the current Hong Kong minute of now.
func Due(expr string, now time.Time) bool {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return false
	}
	now = now.In(timeutil.HKT)
	prev := now.Add(-90 * time.Second)
	next := sched.Next(prev)
	return next.Year() == now.Year() &&
		next.Month() == now.Month() &&
		next.Day() == now.Day() &&
		next.Hour() == now.Hour() &&
		next.Minute() == now.Minute() &&
		!next.After(now)
}
