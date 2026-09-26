package domain

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// SaleClockAnchor is the instant a sale workflow's clock starts from (2026-09-26). Every
// sales.deal step is due "immediately", and immediately used to mean the RECORDING instant, so an
// open sale planned for a later day (In Discussion / Advance Paid) read "Overdue" the moment it was
// saved. The work a sale owes happens on the sale's own day: a sale dated AFTER the recording's
// business day (Asia/Kolkata) anchors at the start of that day; a sale dated on the recording day or
// earlier -- or with no readable date -- anchors on the recording, exactly as before. Business days,
// never hour arithmetic.
func SaleClockAnchor(recordedAt time.Time, saleDate string) time.Time {
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(saleDate), biztime.DefaultLocation())
	if err != nil {
		return recordedAt
	}
	if biztime.BusinessDate(day) <= biztime.BusinessDate(recordedAt) {
		return recordedAt
	}
	return biztime.BusinessDayStart(day)
}
