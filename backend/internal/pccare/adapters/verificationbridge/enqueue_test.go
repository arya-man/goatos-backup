package verificationbridge

import (
	"context"
	"testing"
	"time"

	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccaredomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type fakeCategoryRegistry struct {
	defs []verificationdomain.CategoryDefinition
}

func (f *fakeCategoryRegistry) RegisterCategory(def verificationdomain.CategoryDefinition) error {
	f.defs = append(f.defs, def)
	return nil
}

type fakeVerificationCreator struct {
	calls []verificationdomain.CreateItem
}

func (f *fakeVerificationCreator) CreateItem(_ context.Context, in verificationdomain.CreateItem) (verificationdomain.CreateItemResult, error) {
	f.calls = append(f.calls, in)
	return verificationdomain.CreateItemResult{Created: true}, nil
}

// Maintainer decision 2026-09-02: the vaccine-stock check is approved by the PC Director on
// the module's own stock-verdict route — the verifier queue must not offer an inventory page,
// and the four verifier-reviewed categories must all stay registered.
func TestRegisterCategoriesExcludesInventoryVaccine(t *testing.T) {
	reg := &fakeCategoryRegistry{}
	if err := RegisterCategories(reg); err != nil {
		t.Fatalf("RegisterCategories: %v", err)
	}
	got := map[string]verificationdomain.CategoryDefinition{}
	for _, def := range reg.defs {
		got[def.Category] = def
	}
	if _, ok := got[pccaredomain.VerificationCategoryInventoryVaccine]; ok {
		t.Fatalf("inventory_vaccine must NOT be a verifier category (director-approved since 2026-09-02): %#v", got)
	}
	for _, workCategory := range pccaredomain.VerifierReviewedCategories {
		if _, ok := got[pccaredomain.VerificationCategoryFor(workCategory)]; !ok {
			t.Fatalf("verifier-reviewed category %q missing from registered categories: %#v", workCategory, got)
		}
	}
	if len(got) != len(pccaredomain.VerifierReviewedCategories) {
		t.Fatalf("registered %d categories, want exactly the %d verifier-reviewed ones: %#v",
			len(got), len(pccaredomain.VerifierReviewedCategories), got)
	}
}

// The bridge itself refuses a director-approved category, even when called directly — the
// defense-in-depth twin of the pending-verification consumer's skip.
func TestEnqueueInventoryVaccineNeverCreatesAVerifierItem(t *testing.T) {
	creator := &fakeVerificationCreator{}
	enq := New(creator)
	capturedAt := time.Date(2026, time.August, 26, 7, 30, 0, 0, time.UTC)
	if err := enq.EnqueuePCCareVerification(context.Background(), pccareapp.VerificationEnqueueRequest{
		TenantID:            "tenant-1",
		TaskID:              "task-1",
		Category:            pccaredomain.CategoryInventoryVaccine,
		ParkID:              "park-1",
		VaccineLabel:        "FMD",
		PlannedBusinessDate: "2026-08-26",
		MediaRefs: []ports.LabeledRef{{
			ProofRef: "proof-fridge-stock",
			Label:    "Fridge stock proof",
		}},
		OperatorID:     "operator-1",
		CapturedAt:     capturedAt,
		IdempotencyKey: "pc-care-verification:task-1:8",
	}); err != nil {
		t.Fatalf("EnqueuePCCareVerification: %v", err)
	}
	if len(creator.calls) != 0 {
		t.Fatalf("CreateItem calls = %d, want 0 — stock work is the PC Director's, never the verifier's", len(creator.calls))
	}
}
