package autoupdate

import (
	"github.com/robfig/cron/v3"
	"strings"
	"time"
)

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func NextRun(expression string, after time.Time) (time.Time, error) {
	if len(expression) > 256 || len(strings.Fields(expression)) != 5 || strings.Contains(expression, "=") {
		return time.Time{}, ErrInvalid
	}
	schedule, err := parser.Parse(expression)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	next := schedule.Next(after.UTC())
	if next.IsZero() || !next.After(after) {
		return time.Time{}, ErrInvalid
	}
	return next.UTC(), nil
}
