package app

import (
	"context"
	"errors"
	"testing"
	"time"

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

// A sale recorded AS closed happened on the day it names, so that day cannot be in the future;
// an OPEN sale may be planned ahead, up to MaxSaleDateDaysAhead (the web drawer's window), and
// no further for any status.
func TestSaleDateWindowAtRecord(t *testing.T) {
	record := func(status string, daysAhead int) error {
		repo := &feedRepo{}
		s := NewSalesService(repo)
		s.now = func() time.Time { return time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC) }
		w := feedSaleWrite(10)
		w.Status = status
		w.SaleDate = time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC).AddDate(0, 0, daysAhead).Format("2006-01-02")
		_, err := s.CreateDeal(context.Background(), tenant, w, "actor", "window-"+status)
		return err
	}
	var v domain.ErrDealValidation
	for _, st := range []string{"", domain.StatusDealClosed} {
		if err := record(st, 0); err != nil {
			t.Fatalf("a sale closed today must record, got %v", err)
		}
		if err := record(st, 1); !errors.As(err, &v) || v.Field != "sale_date" {
			t.Fatalf("a sale recorded closed (%q) for tomorrow must be refused on sale_date, got %v", st, err)
		}
	}
	if err := record(domain.StatusAdvancePaid, domain.MaxSaleDateDaysAhead); err != nil {
		t.Fatalf("an open sale planned inside the window must record, got %v", err)
	}
	if err := record(domain.StatusInDiscussion, domain.MaxSaleDateDaysAhead+1); !errors.As(err, &v) || v.Field != "sale_date" {
		t.Fatalf("a sale past the window must be refused, got %v", err)
	}
}
