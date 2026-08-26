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

func TestRegisterCategoriesIncludesInventoryVaccine(t *testing.T) {
	reg := &fakeCategoryRegistry{}
	if err := RegisterCategories(reg); err != nil {
		t.Fatalf("RegisterCategories: %v", err)
	}
	got := map[string]verificationdomain.CategoryDefinition{}
	for _, def := range reg.defs {
		got[def.Category] = def
	}
	def, ok := got[pccaredomain.VerificationCategoryInventoryVaccine]
	if !ok {
		t.Fatalf("inventory_vaccine category missing from registered categories: %#v", got)
	}
	if def.Vertical != pccaredomain.VerificationVerticalPreventiveCare ||
		def.Module != pccaredomain.VerificationModulePCCare ||
		def.NavigationModule != pccaredomain.VerificationModulePCCare ||
		def.PageKey != pccaredomain.VerificationCategoryInventoryVaccine ||
		def.PageLabel != "Vaccine Inventory" {
		t.Fatalf("inventory category def = %+v, want PC Care Vaccine Inventory verifier page", def)
	}
}

func TestEnqueueInventoryVaccineCreatesVerifierItemWithFridgeProof(t *testing.T) {
	creator := &fakeVerificationCreator{}
	enq := New(creator)
	capturedAt := time.Date(2026, time.August, 26, 7, 30, 0, 0, time.UTC)
	if err := enq.EnqueuePCCareVerification(context.Background(), pccareapp.VerificationEnqueueRequest{
		TenantID:            "tenant-1",
		TaskID:              "task-1",
		Category:            pccaredomain.CategoryInventoryVaccine,
		ParkID:              "park-1",
		ShedID:              "shed-1",
		ShedName:            "Mandela",
		PartitionLabel:      "7",
		PlannedBusinessDate: "2026-08-26",
		MediaRefs: []ports.LabeledRef{{
			ProofRef: "proof-fridge-stock",
			Label:    "Fridge stock proof",
		}},
		AnimalCount:    0,
		OperatorID:     "director-1",
		CapturedAt:     capturedAt,
		IdempotencyKey: "pc-care-verification:task-1:8",
	}); err != nil {
		t.Fatalf("EnqueuePCCareVerification: %v", err)
	}
	if len(creator.calls) != 1 {
		t.Fatalf("CreateItem calls = %d, want 1", len(creator.calls))
	}
	item := creator.calls[0]
	if item.Vertical != pccaredomain.VerificationVerticalPreventiveCare ||
		item.Module != pccaredomain.VerificationModulePCCare ||
		item.Category != pccaredomain.VerificationCategoryInventoryVaccine {
		t.Fatalf("item route = %s/%s/%s, want preventive_care/pc_care/inventory_vaccine", item.Vertical, item.Module, item.Category)
	}
	if item.SubjectLabel == nil || *item.SubjectLabel != "Vaccine Inventory · Mandela 7" {
		t.Fatalf("subject label = %v, want Vaccine Inventory · Mandela 7", item.SubjectLabel)
	}
	if len(item.MediaRefs) != 1 || item.MediaRefs[0] != "proof-fridge-stock" {
		t.Fatalf("media refs = %v, want fridge proof", item.MediaRefs)
	}
	if item.Source.Module != pccaredomain.VerificationModulePCCare ||
		item.Source.RefType != pccaredomain.VerificationRefTypeTask ||
		item.Source.RefID != "task-1" {
		t.Fatalf("source = %+v, want PC Care task source", item.Source)
	}
	if len(item.ContextRows) != 2 ||
		item.ContextRows[0].Label != "Work" || item.ContextRows[0].Value != "Vaccine Inventory" ||
		item.ContextRows[1].Label != "Planned for" || item.ContextRows[1].Value != "2026-08-26" {
		t.Fatalf("context rows = %+v, want work and planned date with no animal-count row", item.ContextRows)
	}
	if item.OperatorID == nil || *item.OperatorID != "director-1" ||
		item.ShedID == nil || *item.ShedID != "shed-1" ||
		item.ParkID == nil || *item.ParkID != "park-1" ||
		item.PartitionLabel == nil || *item.PartitionLabel != "7" ||
		!item.CapturedAt.Equal(capturedAt) ||
		item.IdempotencyKey != "pc-care-verification:task-1:8" {
		t.Fatalf("item metadata = %+v, want inventory task metadata preserved", item)
	}
}
