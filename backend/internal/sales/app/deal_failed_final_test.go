package app

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// DEAL FAILED IS FINAL (maintainer decision 2026-09-25). The status picker a deal offers is the
// backend's call: every status for a live deal, none for a failed one; the server refuses a move
// out of Deal Failed with farm copy; and closing a failed deal is refused before the feed store is
// even asked about it.
func TestDealFailedIsFinal(t *testing.T) {
	for _, next := range domain.Statuses {
		if !domain.StatusChangeAllowed(domain.StatusDealClosed, next) {
			t.Fatalf("a live deal must be settable to %s", next)
		}
		if want := next == domain.StatusDealFailed; domain.StatusChangeAllowed(domain.StatusDealFailed, next) != want {
			t.Fatalf("failed -> %s allowed=%v, want %v", next, !want, want)
		}
	}
	if got := domain.NextStatuses(domain.StatusDealFailed); got == nil || len(got) != 0 {
		t.Fatalf("a failed deal offers no status, got %#v", got)
	}
	if got := domain.NextStatuses(domain.StatusInDiscussion); !reflect.DeepEqual(got, domain.Statuses) {
		t.Fatalf("a live deal offers every status, got %v", got)
	}

	repo := &fakeRepo{dealStatus: domain.StatusDealFailed}
	svc := NewSalesService(repo)
	if _, err := svc.SetDealStatus(context.Background(), "t", "d1", domain.StatusDealClosed, false, "actor"); !errors.Is(err, domain.ErrDealFailedIsFinal) {
		t.Fatalf("closing a failed deal must be refused as final, got %v", err)
	}
	if repo.statusDealID != "" {
		t.Fatalf("a refused change must never reach the repository, got %q", repo.statusDealID)
	}
	mapped := SalesHTTPError(domain.ErrDealFailedIsFinal)
	if mapped == nil || mapped.HTTPStatus != http.StatusConflict || mapped.Code != "sale_deal_failed_is_final" || mapped.Message == "" {
		t.Fatalf("final refusal maps to %+v, want 409 sale_deal_failed_is_final with farm copy", mapped)
	}
}
