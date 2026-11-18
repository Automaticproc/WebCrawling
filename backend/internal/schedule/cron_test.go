package schedule

import (
	"testing"
	"time"

	"catalog/internal/timeutil"
)

func TestDailyEightHongKong(t *testing.T) {
	loc := timeutil.HKT
	morning := time.Date(2026, 11, 17, 7, 30, 0, 0, loc)
	if Due(timeutil.DefaultCron, morning) {
		t.Fatal("07:30 should not be due")
	}
	at := time.Date(2026, 11, 17, 8, 0, 10, 0, loc)
	if !Due(timeutil.DefaultCron, at) {
		t.Fatal("08:00 HKT should be due")
	}
	later := time.Date(2026, 11, 17, 8, 1, 0, 0, loc)
	if Due(timeutil.DefaultCron, later) {
		t.Fatal("08:01 should not be due")
	}
	next, err := Next(timeutil.DefaultCron, morning)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 17, 8, 0, 0, 0, loc)
	if !next.Equal(want) {
		t.Fatalf("next %s want %s", next, want)
	}
	nextAfter, err := Next(timeutil.DefaultCron, at)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := time.Date(2026, 11, 18, 8, 0, 0, 0, loc)
	if !nextAfter.Equal(wantNext) {
		t.Fatalf("next after 08:00 %s want %s", nextAfter, wantNext)
	}
	utcMorning := time.Date(2026, 11, 17, 0, 0, 5, 0, time.UTC) // 08:00 HKT
	if !Due(timeutil.DefaultCron, utcMorning) {
		t.Fatal("00:00 UTC is 08:00 HKT and should be due")
	}
}
