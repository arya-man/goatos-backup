package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// A planned sale's clock is its own business day; a sale dated today or earlier is the recording.
func TestSaleClockAnchorIsTheSaleDayOnlyWhenItIsAhead(t *testing.T) {
	ist := biztime.DefaultLocation()
	recorded := time.Date(2026, 9, 26, 23, 30, 0, 0, ist)
	if got := SaleClockAnchor(recorded, "2026-10-01"); !got.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, ist)) {
		t.Fatalf("future sale anchor = %s", got)
	}
	for _, d := range []string{"2026-09-26", "2026-09-20", "", "not-a-date"} {
		if got := SaleClockAnchor(recorded, d); !got.Equal(recorded) {
			t.Fatalf("sale date %q must anchor on recording, got %s", d, got)
		}
	}
	// 23:30 IST on the 26th is the 26th in IST even though it is 18:00 UTC -- never a UTC day.
	if got := SaleClockAnchor(recorded.UTC(), "2026-09-27"); !got.Equal(time.Date(2026, 9, 27, 0, 0, 0, 0, ist)) {
		t.Fatalf("next-day sale anchor = %s", got)
	}
}
