package autoupdate

import (
	"testing"
	"time"
)

func TestNextRunUsesMidnightUTC(t *testing.T) {
	after := time.Date(2026, 9, 27, 2, 0, 0, 0, time.FixedZone("local", 7200))
	next, err := NextRun(DefaultExpression, after)
	want := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	if err != nil || !next.Equal(want) || next.Location() != time.UTC {
		t.Fatalf("next=%v %v", next, err)
	}
}
func TestNextRunRejectsUnsupportedExpression(t *testing.T) {
	for _, expression := range []string{"* * * * * *", "@daily", "CRON_TZ=UTC 0 0 * * *", "TZ=UTC 0 0 * *", "*/0 * * * *", "0 0 30 2 *", ""} {
		if _, err := NextRun(expression, time.Now()); err == nil {
			t.Errorf("accepted %q", expression)
		}
	}
}
func TestNextRunUsesStandardDayOrSemantics(t *testing.T) {
	next, err := NextRun("0 0 1 * MON", time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil || next.Day() != 28 {
		t.Fatalf("next=%v %v", next, err)
	}
}
