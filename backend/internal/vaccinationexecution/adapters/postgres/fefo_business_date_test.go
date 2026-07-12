package postgres

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func TestVaccineLotDisabledReasonUsesIndiaBusinessDate(t *testing.T) {
	expiry := func(year int, month time.Month, day int) pgtype.Date {
		return pgtype.Date{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), Valid: true}
	}

	beforeIndiaMidnight := biztime.BusinessDate(time.Date(2026, 7, 11, 18, 29, 0, 0, time.UTC))
	atIndiaMidnight := biztime.BusinessDate(time.Date(2026, 7, 11, 18, 30, 0, 0, time.UTC))
	if beforeIndiaMidnight != "2026-07-11" || atIndiaMidnight != "2026-07-12" {
		t.Fatalf("business dates before=%s at=%s", beforeIndiaMidnight, atIndiaMidnight)
	}

	tests := []struct {
		name, status, available, businessDate, want string
		expiry                                      pgtype.Date
	}{
		{name: "today remains usable before India midnight", status: "active", available: "10", expiry: expiry(2026, 7, 11), businessDate: beforeIndiaMidnight},
		{name: "same date expires at next India business day", status: "active", available: "10", expiry: expiry(2026, 7, 11), businessDate: atIndiaMidnight, want: "lot_expired"},
		{name: "new business day lot remains usable", status: "active", available: "10", expiry: expiry(2026, 7, 12), businessDate: atIndiaMidnight},
		{name: "status takes precedence", status: "depleted", available: "0", expiry: expiry(2026, 7, 11), businessDate: atIndiaMidnight, want: "lot_depleted"},
		{name: "empty active lot is disabled", status: "active", available: "0", expiry: expiry(2026, 7, 12), businessDate: atIndiaMidnight, want: "no_available_quantity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vaccineLotDisabledReason(tt.status, tt.available, tt.expiry, tt.businessDate); got != tt.want {
				t.Fatalf("reason=%q want=%q", got, tt.want)
			}
		})
	}
}
