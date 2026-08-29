package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20).
//
// The first test here is the REPORTED FAILURE, modelled on the production path: a save that
// relabels the item bumps row_version, and the approve pressed straight afterwards carries the
// version the screen still holds. It is written against the OLD two-act shape on purpose, so it
// stays a live demonstration of why the single act exists.

const measurementCategory = "weighing"

// stubApplier stands in for a producing module. relabels models the real appliers' side effect:
// every producer write restates the item's subject label, which bumps row_version.
type stubApplier struct {
	repo      *fakeRepo
	itemID    string
	applies   []MeasurementApply
	applyErr  error
	recorded  bool
	recordErr error
	asked     int
}

type competingVerdictApplier struct {
	*stubApplier
	svc            *Service
	item           domain.Item
	competitorDone chan error
}

func (a *competingVerdictApplier) ApplyMeasurement(ctx context.Context, in MeasurementApply) error {
	a.competitorDone = make(chan error, 1)
	go func() {
		_, err := a.svc.RecordVerdict(context.Background(), domain.Verdict{
			TenantID: a.item.TenantID, ItemID: a.item.ItemID, Decision: domain.DecisionRejected,
			Reason: "competing decision", VerifierID: testTenant, RowVersion: a.item.RowVersion,
			IdempotencyKey: "competing-verdict-key",
		})
		a.competitorDone <- err
	}()
	select {
	case err := <-a.competitorDone:
		if err != nil {
			return errors.New("competing verdict was not fenced during measurement: " + err.Error())
		}
		return errors.New("competing verdict was not fenced during measurement")
	case <-time.After(25 * time.Millisecond):
	}
	return a.stubApplier.ApplyMeasurement(ctx, in)
}

func (s *stubApplier) ApplyMeasurement(_ context.Context, in MeasurementApply) error {
	if s.applyErr != nil {
		return s.applyErr
	}
	s.applies = append(s.applies, in)
	s.relabel()
	return nil
}

// relabel is what every real applier's service does after it writes: the subject label states the
// number, so it is recomposed, and that UPDATE carries row_version = row_version + 1.
func (s *stubApplier) relabel() {
	item, ok := s.repo.items[s.itemID]
	if !ok {
		return
	}
	item.RowVersion++
	s.repo.items[s.itemID] = item
}

func (s *stubApplier) HasRecordedMeasurement(context.Context, string, domain.SourceRef) (bool, error) {
	s.asked++
	if s.recordErr != nil {
		return false, s.recordErr
	}
	return s.recorded, nil
}

// newMeasurementService wires a service with one measurable category and returns the item raised
// under it, ready to decide.
func newMeasurementService(t *testing.T, spec *domain.MeasurementCorrectionSpec) (*Service, *fakeRepo, *stubApplier, domain.Item) {
	t.Helper()
	repo := newFakeRepo()
	svc := NewService(repo, fakeMedia{})
	if err := svc.RegisterCategory(domain.CategoryDefinition{
		Vertical: "growth", Module: "weighing", Category: measurementCategory,
		MeasurementCorrection: spec,
	}); err != nil {
		t.Fatalf("RegisterCategory: %v", err)
	}
	result, err := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "growth", Module: "weighing", Category: measurementCategory,
		Source:    domain.SourceRef{Module: "weighing", RefType: "weighing_observation", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	applier := &stubApplier{repo: repo, itemID: result.Item.ItemID}
	if spec != nil {
		if err := svc.RegisterMeasurementApplier(measurementCategory, applier); err != nil {
			t.Fatalf("RegisterMeasurementApplier: %v", err)
		}
	}
	return svc, repo, applier, result.Item
}

func weighingSpec() *domain.MeasurementCorrectionSpec {
	return &domain.MeasurementCorrectionSpec{
		Title: "Correct the weight", Help: "It replaces the weight recorded here.",
		ValueLabel: "Corrected weight (kg)", SubmitLabel: "Save corrected weight",
		CountLabel: "Goats on the scale", CountRefTypes: []string{"weighing_shed"},
	}
}

func wastageSpec() *domain.MeasurementCorrectionSpec {
	return &domain.MeasurementCorrectionSpec{
		Title: "Record the wastage", Help: "It replaces any wastage weight recorded here.",
		ValueLabel: "Measured wastage (kg)", SubmitLabel: "Save wastage weight",
		RequiredForApprove: true,
	}
}

// TestSavingThenApprovingIsTheDefectThisReplaces is the reported failure, reproduced.
//
// Save the number (which relabels, bumping row_version), then approve with the version the screen
// still shows -- exactly what the two-button card did -- and the version-fenced verdict matches
// nothing. She pressed Approve and nothing happened.
func TestSavingThenApprovingIsTheDefectThisReplaces(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, weighingSpec())

	// The old separate save: the producer route wrote the number and relabelled the item.
	applier.relabel()

	// The approve she pressed next, carrying the version the screen loaded with.
	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion,
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 {
		t.Fatalf("err = %v, want the 409 the old save-then-approve produced", err)
	}
}

// TestApproveCarriesTheNumberInOneAct is the same verifier, one press.
func TestApproveCarriesTheNumberInOneAct(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, weighingSpec())

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 41.5, Reason: "scale reads 41.5"},
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", decided.Status)
	}
	if len(applier.applies) != 1 {
		t.Fatalf("applies = %d, want the number written exactly once", len(applier.applies))
	}
	applied := applier.applies[0]
	if applied.Value != 41.5 || applied.Reason != "scale reads 41.5" {
		t.Fatalf("applied = %+v, want her value and note", applied)
	}
	// The target is the ITEM's own source, never anything the client named.
	if applied.Source.RefID != item.Source.RefID || applied.Source.RefType != item.Source.RefType {
		t.Fatalf("target = %+v, want the item's own source %+v", applied.Source, item.Source)
	}
	// Derived from the verdict key so a replayed approve re-applies the same reading, and distinct
	// from it because the two land in different modules' idempotency tables.
	if applied.IdempotencyKey != "verdict-key-1:measurement" {
		t.Fatalf("idempotency key = %q, want the verdict key suffixed", applied.IdempotencyKey)
	}
}

func TestApproveMeasurementFencesCompetingVerdictUntilThePairLands(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, weighingSpec())
	racing := &competingVerdictApplier{stubApplier: applier, svc: svc, item: item}
	svc.measurementAppliers[measurementCategory] = racing

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 41.5},
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", decided.Status)
	}
	select {
	case err := <-racing.competitorDone:
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 {
			t.Fatalf("competing verdict err = %v, want 409 after the first verdict lands", err)
		}
	case <-time.After(time.Second):
		t.Fatal("competing verdict did not finish after the first verdict released the fence")
	}
}

// TestApproveWithNoNumberLeavesTheOperatorsWeightAlone is the normal weighing case: she agrees with
// the recorded weight, so she just approves. No producer write may happen.
func TestApproveWithNoNumberLeavesTheOperatorsWeightAlone(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, weighingSpec())

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", decided.Status)
	}
	if len(applier.applies) != 0 {
		t.Fatalf("applies = %d, want the operator's weight untouched", len(applier.applies))
	}
	if applier.asked != 0 {
		t.Fatalf("asked = %d, want no recorded-value lookup on an optional category", applier.asked)
	}
}

// TestRejectDiscardsTheTypedNumber: rejection sends the work back to be recorded again, so a value
// typed before she changed her mind must never reach the producer.
func TestRejectDiscardsTheTypedNumber(t *testing.T) {
	svc, repo, applier, item := newMeasurementService(t, weighingSpec())

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionRejected,
		Reason: "clip too dark to read", VerifierID: testTenant, RowVersion: item.RowVersion,
		IdempotencyKey: "verdict-key-1",
		Measurement:    &domain.VerdictMeasurement{Value: 41.5},
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusRejected {
		t.Fatalf("status = %s, want rejected", decided.Status)
	}
	if len(applier.applies) != 0 {
		t.Fatalf("applies = %d, want nothing written on a reject", len(applier.applies))
	}
	// Dropped, not merely unused: the value must not travel into the verdict record either, or a
	// later reader of that row would take it for a number the verifier stood behind.
	if repo.lastVerdict.Measurement != nil {
		t.Fatalf("measurement = %+v, want it dropped before the verdict is stored", repo.lastVerdict.Measurement)
	}
}

// TestWastageApproveNeedsANumber: the reading is born on her screen, so an approve carrying none
// and finding none recorded is refused BEFORE the verdict -- and the item stays decidable.
func TestWastageApproveNeedsANumber(t *testing.T) {
	svc, repo, applier, item := newMeasurementService(t, wastageSpec())
	applier.recorded = false

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "measurement_required" {
		t.Fatalf("err = %v, want measurement_required", err)
	}
	if repo.items[item.ItemID].Status != domain.StatusPending {
		t.Fatalf("status = %s, want the item still decidable", repo.items[item.ItemID].Status)
	}
}

// TestWastageApproveAcceptsAZeroReading: an empty trough is a real, good measurement, and must not
// be mistaken for "she typed nothing".
func TestWastageApproveAcceptsAZeroReading(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, wastageSpec())

	if _, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 0},
	}); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if len(applier.applies) != 1 || applier.applies[0].Value != 0 {
		t.Fatalf("applies = %+v, want a recorded zero", applier.applies)
	}
}

// TestWastageApproveAllowsAnAlreadyMeasuredPen keeps an installed APK that still uses the separate
// save button working: it measured first, so its number-less approve must go through.
func TestWastageApproveAllowsAnAlreadyMeasuredPen(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, wastageSpec())
	applier.recorded = true
	// The old client's save relabelled the item, so its version moved -- and this approve now
	// carries the fresh one, which is what its own refresh gives it.
	applier.relabel()

	decided, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion + 1, IdempotencyKey: "verdict-key-1",
	})
	if err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if decided.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", decided.Status)
	}
	if applier.asked != 1 {
		t.Fatalf("asked = %d, want the recorded-value lookup consulted once", applier.asked)
	}
}

// TestApproveStopsWhenTheProducerRefusesTheNumber: an approve is irreversible, so a refused write
// must take the whole decision down rather than leave an approved item beside a number that never
// landed.
func TestApproveStopsWhenTheProducerRefusesTheNumber(t *testing.T) {
	svc, repo, applier, item := newMeasurementService(t, weighingSpec())
	applier.applyErr = errors.New("bucket already closed")

	if _, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 41.5},
	}); err == nil {
		t.Fatal("RecordVerdict succeeded, want the producer's refusal to stop the approve")
	}
	if repo.items[item.ItemID].Status != domain.StatusPending {
		t.Fatalf("status = %s, want the item left undecided", repo.items[item.ItemID].Status)
	}
}

// TestStaleScreenIsRefusedBeforeAnythingIsWritten: the fence still holds, and it holds BEFORE the
// producer write -- applying her reading to a record someone else already acted on is the failure
// the version check exists to stop.
func TestStaleScreenIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, weighingSpec())

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion + 7, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 41.5},
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.HTTPStatus != 409 {
		t.Fatalf("err = %v, want 409 conflict", err)
	}
	if len(applier.applies) != 0 {
		t.Fatalf("applies = %d, want nothing written from a stale screen", len(applier.applies))
	}
}

// TestCountIsRefusedOnARefTypeThatCarriesNone: an individual capture weighs exactly one animal, so
// a head count there is a value with no meaning and the producer refuses it. Naming the field is
// better than failing the whole approve with the producer's own wording.
func TestCountIsRefusedOnARefTypeThatCarriesNone(t *testing.T) {
	svc, _, applier, item := newMeasurementService(t, weighingSpec())
	count := 38

	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: item.RowVersion, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 41.5, Count: &count},
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "measurement_count_not_supported" {
		t.Fatalf("err = %v, want measurement_count_not_supported", err)
	}
	if len(applier.applies) != 0 {
		t.Fatalf("applies = %d, want nothing written", len(applier.applies))
	}
}

// TestNumberIsRefusedForACategoryThatDeclaresNone: accepting a value we can store nowhere would
// tell the verifier her reading landed when nothing wrote it.
func TestNumberIsRefusedForACategoryThatDeclaresNone(t *testing.T) {
	svc, _ := newTestService()
	result, _ := svc.CreateItem(context.Background(), domain.CreateItem{
		TenantID: testTenant, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: testTenant},
		MediaRefs: []string{"proof-1"}, IdempotencyKey: "key-1",
	})
	_, err := svc.RecordVerdict(context.Background(), domain.Verdict{
		TenantID: testTenant, ItemID: result.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: testTenant, RowVersion: 1, IdempotencyKey: "verdict-key-1",
		Measurement: &domain.VerdictMeasurement{Value: 41.5},
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "measurement_not_supported" {
		t.Fatalf("err = %v, want measurement_not_supported", err)
	}
}
