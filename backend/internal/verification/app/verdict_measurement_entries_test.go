package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// THE APPROVE CARRIES THE NUMBERS -- one per field (maintainer decision 2026-08-21).
//
// Feed packing extends the 2026-08-20 single-value rule: the item carries the pen-session's feed
// items as blind entry boxes (MeasurementFields), the verifier fills every one, and the approve
// carries the whole set. These tests pin the four load-bearing edges: completeness is enforced
// against the ITEM's own fields, an unknown key is refused by name, a fields-less item degrades to
// a judge-the-video approve instead of stranding, and the applier receives entries plus the item's
// field list so the producer can store labels without re-reads.

const entriesCategory = "feed_packing_test"

// newEntriesService wires a per-item-fields category and returns an item enqueued with the given
// fields, ready to decide.
func newEntriesService(t *testing.T, fields []domain.MeasurementField) (*Service, *stubApplier, domain.Item) {
	t.Helper()
	repo := newFakeRepo()
	svc := NewService(repo, fakeMedia{})
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "feed", Module: "feed", Category: entriesCategory,
		MeasurementCorrection: &domain.MeasurementCorrectionSpec{
			Title: "Record the packed quantities", Help: "Enter the packed weight for each feed item.",
			ValueLabel: "Packed quantity (kg)", SubmitLabel: "Save packed quantities",
			RequiredForApprove: true, PerItemFields: true,
		},
	}); err != nil {
		t.Fatalf("RegisterCategory: %v", err)
	}
	result, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "feed", Module: "feed", Category: entriesCategory,
		Source:            domain.SourceRef{Module: "feed", RefType: "feed_packing_completion", RefID: testTenant},
		MediaRefs:         []string{"proof-1"},
		MeasurementFields: fields,
		IdempotencyKey:    "entries-key-1",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	applier := &stubApplier{repo: repo, itemID: result.Item.ItemID}
	if err := svc.RegisterMeasurementApplier(entriesCategory, applier); err != nil {
		t.Fatalf("RegisterMeasurementApplier: %v", err)
	}
	return svc, applier, result.Item
}

func packingFields() []domain.MeasurementField {
	return []domain.MeasurementField{
		{Key: "maize", Label: "Maize"},
		{Key: "soya", Label: "Soya"},
	}
}

func TestApproveWithAllEntriesLandsThemOnTheApplier(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-1",
		Measurement: &domain.VerdictMeasurement{Entries: []domain.MeasurementEntry{
			{Key: "maize", Value: 12.5},
			{Key: "soya", Value: 0}, // zero is a real observation: "this item was not packed"
		}},
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %q, want approved", decided.Status)
	}
	if len(applier.applies) != 1 {
		t.Fatalf("applier calls = %d, want 1", len(applier.applies))
	}
	got := applier.applies[0]
	if len(got.Entries) != 2 || got.Entries[0] != (domain.MeasurementEntry{Key: "maize", Value: 12.5}) {
		t.Errorf("entries = %+v, want both readings", got.Entries)
	}
	// The item's own field list rides along so the producer stores display labels without a re-read.
	if len(got.Fields) != 2 || got.Fields[1].Label != "Soya" {
		t.Errorf("fields = %+v, want the item's declared fields", got.Fields)
	}
}

func TestApproveMissingOneEntryIsRefusedNamingTheField(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-2",
		Measurement: &domain.VerdictMeasurement{Entries: []domain.MeasurementEntry{
			{Key: "maize", Value: 12.5},
		}},
	})
	if err == nil {
		t.Fatal("approve with a missing entry landed; every box must be filled")
	}
	if !strings.Contains(err.Error(), "measurement_required") && !strings.Contains(err.Error(), "record every value") {
		t.Errorf("error = %v, want the completeness refusal", err)
	}
	if len(applier.applies) != 0 {
		t.Errorf("applier was called %d times on a refused approve", len(applier.applies))
	}
}

func TestApproveWithNoMeasurementAtAllIsRefusedOnAFieldedItem(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())
	applier.recorded = false

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-3",
	})
	if err == nil {
		t.Fatal("blank approve landed on a required per-field item with nothing recorded")
	}
	// An item whose completion was already measured (an approve replay after a prior successful
	// apply) is still approvable.
	applier.recorded = true
	if _, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-4",
	}); err != nil {
		t.Fatalf("already-measured blank approve refused: %v", err)
	}
}

func TestApproveWithUnknownEntryKeyIsRefused(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-5",
		Measurement: &domain.VerdictMeasurement{Entries: []domain.MeasurementEntry{
			{Key: "maize", Value: 12.5},
			{Key: "soya", Value: 4},
			{Key: "bhusa", Value: 2}, // the item never declared this box
		}},
	})
	if err == nil {
		t.Fatal("approve naming an undeclared field landed; the value would vanish inside the producer")
	}
	if len(applier.applies) != 0 {
		t.Errorf("applier was called %d times on a refused approve", len(applier.applies))
	}
}

// A per-field item enqueued with NO fields is the producer's deliberate fail-open (frozen sheet
// unreadable at submit). It must stay approvable as a judge-the-video item, not strand.
func TestFieldlessPerItemItemApprovesWithoutAMeasurement(t *testing.T) {
	svc, applier, item := newEntriesService(t, nil)

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-6",
	})
	if err != nil {
		t.Fatalf("fields-less approve refused: %v -- the item is stranded unapprovable", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %q, want approved", decided.Status)
	}
	if applier.asked != 0 {
		t.Errorf("HasRecordedMeasurement consulted %d times; a fields-less item requires nothing", applier.asked)
	}
	if len(applier.applies) != 0 {
		t.Errorf("applier called %d times with nothing to apply", len(applier.applies))
	}
}

// A reject never carries the readings: rejection sends the bag back to be packed and filmed again,
// so a value written now describes work about to be redone.
func TestRejectDropsEntries(t *testing.T) {
	svc, applier, item := newEntriesService(t, packingFields())

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionRejected,
		Reason: "no video visible", VerifierID: testTenant, RowVersion: item.RowVersion,
		IdempotencyKey: "reject-key-1",
		Measurement: &domain.VerdictMeasurement{Entries: []domain.MeasurementEntry{
			{Key: "maize", Value: 12.5},
			{Key: "soya", Value: 4},
		}},
	})
	if err != nil {
		t.Fatalf("RecordVerdict reject: %v", err)
	}
	if decided.Status != domain.StatusRejected {
		t.Fatalf("status = %q, want rejected", decided.Status)
	}
	if len(applier.applies) != 0 {
		t.Errorf("applier called %d times on a reject; readings must be dropped", len(applier.applies))
	}
}

// Entries on an item that declares NO fields (weighing, wastage) are refused, not silently dropped.
func TestEntriesOnASingleValueItemAreRefused(t *testing.T) {
	svc, _, _, item := newMeasurementService(t, weighingSpec())

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: item.TenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "approve-key-7",
		Measurement: &domain.VerdictMeasurement{Entries: []domain.MeasurementEntry{
			{Key: "maize", Value: 12.5},
		}},
	})
	if err == nil {
		t.Fatal("per-field entries landed on a single-value item; they have nowhere to go")
	}
}
