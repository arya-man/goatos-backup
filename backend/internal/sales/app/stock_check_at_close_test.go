package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// STOCK CHECK ONLY AT CLOSE (maintainer decision 2026-09-25). An OPEN feed sale (In Discussion,
// Advance Paid) takes nothing off the store, so recording one never asks the short-stock question;
// the question comes once, when the sale closes -- at record for a sale recorded as closed (the
// default when no status is named), or at the status change to Deal Closed.
func TestRecordingAnOpenFeedSaleNeverAsksTheStore(t *testing.T) {
	for _, status := range []string{domain.StatusInDiscussion, domain.StatusAdvancePaid} {
		repo := &feedRepo{}
		store := &feedStore{balances: map[string]float64{"CPT/Maize": 100}}
		s := NewSalesService(repo).WithFeedStock(store)
		w := feedSaleWrite(2000)
		w.Status = status
		if _, err := s.CreateDeal(context.Background(), tenant, w, "actor", "open-"+status); err != nil {
			t.Fatalf("recording a %s feed sale must not ask the store, got %v", status, err)
		}
		if repo.createCalls != 1 {
			t.Fatalf("%s: the open sale must record, calls=%d", status, repo.createCalls)
		}
	}
	for _, status := range []string{"", domain.StatusDealClosed} {
		repo := &feedRepo{}
		store := &feedStore{balances: map[string]float64{"CPT/Maize": 100}}
		s := NewSalesService(repo).WithFeedStock(store)
		w := feedSaleWrite(2000)
		w.Status = status
		var short domain.ErrFeedStockShort
		if _, err := s.CreateDeal(context.Background(), tenant, w, "actor", "closed-"+status); !errors.As(err, &short) {
			t.Fatalf("recording a sale as closed (%q) must still ask the store, got %v", status, err)
		}
	}
}
