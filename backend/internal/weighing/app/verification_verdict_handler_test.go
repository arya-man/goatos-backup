package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const (
	verdictTenant      = "00000000-0000-4000-8000-000000000001"
	verdictObservation = "00000000-0000-4000-8000-000000000901"
	verdictVerifier    = "00000000-0000-4000-8000-000000000111"
)

// verdictStore records every write the consumer issues and can replay the
// idempotent "already applied" answer the real store gives on redelivery.
type verdictStore struct {
	calls    []domain.VerificationVerdict
	applied  map[string]bool
	failWith error
}

func newVerdictStore() *verdictStore { return &verdictStore{applied: map[string]bool{}} }

func (s *verdictStore) ApplyVerificationVerdict(_ context.Context, verdict domain.VerificationVerdict) (domain.VerificationVerdictResult, error) {
	s.calls = append(s.calls, verdict)
	if s.failWith != nil {
		return domain.VerificationVerdictResult{}, s.failWith
	}
	// The real store keys idempotency on the verification EVENT id, so a
	// redelivery of the same event replays the original result and writes nothing.
	first := !s.applied[verdict.EventID]
	s.applied[verdict.EventID] = true
	return domain.VerificationVerdictResult{
		Applied:       first,
		ObservationID: verdict.ObservationID,
		Status:        verdict.Status,
	}, nil
}

func verdictEvent(t *testing.T, eventType, eventID, module, refType, refID, reason string) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"verified_by": verdictVerifier,
		"reason":      reason,
		"source": map[string]string{
			"module":   module,
			"ref_type": refType,
			"ref_id":   refID,
		},
	})
	if err != nil {
		t.Fatalf("marshal verdict payload: %v", err)
	}
	return eventbus.Event{ID: eventID, Type: eventType, TenantID: verdictTenant, Payload: payload}
}

// The consumer must exist on BOTH verdict event types. Before this handler existed
// weighing enqueued a verification item with no consumer at all, so every verdict
// was a silent drop.
func TestVerificationVerdictHandlerSubscribesToBothVerdictEvents(t *testing.T) {
	bus := &recordingBus{subs: map[string]int{}}
	NewVerificationVerdictHandler(newVerdictStore(), nil).Register(bus)
	for _, eventType := range []string{"verification.verdict.approved", "verification.verdict.rework"} {
		if bus.subs[eventType] != 1 {
			t.Fatalf("%s subscribers=%d, want 1", eventType, bus.subs[eventType])
		}
	}
}

func TestVerificationVerdictHandlerAppliesApprovedAndReworkForBothWeighingRefTypes(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		refType    string
		wantStatus string
	}{
		{name: "approved_animal", eventType: "verification.verdict.approved", refType: domain.VerificationRefTypeAnimal, wantStatus: domain.VerificationStatusVerified},
		{name: "approved_shed", eventType: "verification.verdict.approved", refType: domain.VerificationRefTypeShed, wantStatus: domain.VerificationStatusVerified},
		{name: "rework_animal", eventType: "verification.verdict.rework", refType: domain.VerificationRefTypeAnimal, wantStatus: domain.VerificationStatusRework},
		{name: "rework_shed", eventType: "verification.verdict.rework", refType: domain.VerificationRefTypeShed, wantStatus: domain.VerificationStatusRework},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newVerdictStore()
			handler := NewVerificationVerdictHandler(store, nil)
			event := verdictEvent(t, tt.eventType, "event-1", domain.VerificationModuleWeighing, tt.refType, verdictObservation, "blurry video")

			if err := handler.HandleEvent(context.Background(), event); err != nil {
				t.Fatalf("HandleEvent errored: %v", err)
			}
			if len(store.calls) != 1 {
				t.Fatalf("store writes=%d, want 1", len(store.calls))
			}
			got := store.calls[0]
			if got.Status != tt.wantStatus {
				t.Fatalf("verdict status=%q, want %q", got.Status, tt.wantStatus)
			}
			if got.RefType != tt.refType || got.ObservationID != verdictObservation {
				t.Fatalf("verdict refType=%q observation=%q, want %q/%q", got.RefType, got.ObservationID, tt.refType, verdictObservation)
			}
			if got.EventID != "event-1" {
				t.Fatalf("verdict EventID=%q; idempotency must key on the verification event id", got.EventID)
			}
			if got.VerifiedBy != verdictVerifier || got.Reason != "blurry video" {
				t.Fatalf("verdict verifiedBy=%q reason=%q, want %q/%q", got.VerifiedBy, got.Reason, verdictVerifier, "blurry video")
			}
		})
	}
}

// Redelivery is the normal case on an at-least-once bus. The consumer must not
// error, and the store must report Applied=false for the replay so no second side
// effect is attributed to it.
func TestVerificationVerdictHandlerIsReplaySafeOnRedelivery(t *testing.T) {
	store := newVerdictStore()
	handler := NewVerificationVerdictHandler(store, nil)
	event := verdictEvent(t, "verification.verdict.approved", "event-replay", domain.VerificationModuleWeighing, domain.VerificationRefTypeAnimal, verdictObservation, "")

	for i := 0; i < 3; i++ {
		if err := handler.HandleEvent(context.Background(), event); err != nil {
			t.Fatalf("delivery %d errored: %v", i+1, err)
		}
	}
	if len(store.calls) != 3 {
		t.Fatalf("store invocations=%d, want 3 (idempotency lives in the store, keyed on event id)", len(store.calls))
	}
	for i, call := range store.calls {
		if call.EventID != "event-replay" {
			t.Fatalf("delivery %d used event id %q, want the stable verification event id", i+1, call.EventID)
		}
	}
	if len(store.applied) != 1 {
		t.Fatalf("distinct applied event ids=%d, want 1", len(store.applied))
	}
}

// Strict filtering: a verdict from any other producer, or on a ref_type weighing
// does not own, must pass through completely untouched.
func TestVerificationVerdictHandlerIgnoresForeignModuleAndRefTypes(t *testing.T) {
	tests := []struct {
		name    string
		module  string
		refType string
	}{
		{name: "feed_module", module: "feed", refType: "feed_distribution_completion"},
		{name: "counts_module", module: "counts", refType: "shifting_event"},
		{name: "vaccination_module", module: "vaccination", refType: "sop_submission"},
		{name: "weighing_module_foreign_reftype", module: domain.VerificationModuleWeighing, refType: "shifting_move"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newVerdictStore()
			handler := NewVerificationVerdictHandler(store, nil)
			event := verdictEvent(t, "verification.verdict.approved", "event-1", tt.module, tt.refType, verdictObservation, "")
			if err := handler.HandleEvent(context.Background(), event); err != nil {
				t.Fatalf("HandleEvent errored: %v", err)
			}
			if len(store.calls) != 0 {
				t.Fatalf("foreign verdict reached the weighing store %d time(s)", len(store.calls))
			}
		})
	}
}

// An unrelated event type on the same bus is not ours.
func TestVerificationVerdictHandlerIgnoresUnrelatedEventTypes(t *testing.T) {
	store := newVerdictStore()
	handler := NewVerificationVerdictHandler(store, nil)
	event := verdictEvent(t, "verification.item.pending", "event-1", domain.VerificationModuleWeighing, domain.VerificationRefTypeAnimal, verdictObservation, "")
	if err := handler.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(store.calls) != 0 {
		t.Fatalf("pending item reached the verdict store %d time(s)", len(store.calls))
	}
}

// A verdict naming an observation this tenant does not have can never succeed on
// retry, so it must fail PERMANENTLY (straight to the DLQ) rather than spin.
func TestVerificationVerdictHandlerFailsPermanentlyOnMissingObservation(t *testing.T) {
	store := newVerdictStore()
	store.failWith = ports.ErrNotFound
	handler := NewVerificationVerdictHandler(store, nil)
	event := verdictEvent(t, "verification.verdict.rework", "event-1", domain.VerificationModuleWeighing, domain.VerificationRefTypeAnimal, verdictObservation, "")

	err := handler.HandleEvent(context.Background(), event)
	if err == nil {
		t.Fatal("missing observation returned nil; want a permanent error")
	}
	if !eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v is retryable; a missing observation is unrecoverable", err)
	}
}

// A transient store failure must stay RETRYABLE so the durable bus redelivers.
func TestVerificationVerdictHandlerRetriesTransientStoreFailure(t *testing.T) {
	store := newVerdictStore()
	store.failWith = errors.New("connection reset")
	handler := NewVerificationVerdictHandler(store, nil)
	event := verdictEvent(t, "verification.verdict.approved", "event-1", domain.VerificationModuleWeighing, domain.VerificationRefTypeAnimal, verdictObservation, "")

	err := handler.HandleEvent(context.Background(), event)
	if err == nil {
		t.Fatal("transient failure returned nil")
	}
	if eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v is permanent; a transient store failure must be retried", err)
	}
}

type recordingBus struct{ subs map[string]int }

func (b *recordingBus) Subscribe(eventType string, _ eventbus.Handler) { b.subs[eventType]++ }
func (b *recordingBus) Publish(context.Context, eventbus.Event) error  { return nil }

// verdictEventWithEvidence is verdictEvent plus the source.evidence_id the
// verification module now publishes.
func verdictEventWithEvidence(t *testing.T, eventID, refID, evidenceID string) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"verified_by": verdictVerifier,
		"source": map[string]string{
			"module":      domain.VerificationModuleWeighing,
			"ref_type":    domain.VerificationRefTypeAnimal,
			"ref_id":      refID,
			"evidence_id": evidenceID,
		},
	})
	if err != nil {
		t.Fatalf("marshal verdict payload: %v", err)
	}
	return eventbus.Event{ID: eventID, Type: "verification.verdict.approved", TenantID: verdictTenant, Payload: payload}
}

// The store's stale-evidence guard can only fire if the verdict names the proof it
// was rendered against. This consumer used to drop source.evidence_id on the floor,
// so every production verdict reached the guard empty and took its skip branch: a
// late verdict for an older video was applied to whichever video was attached when
// it landed. The id must survive the payload -> store hop intact.
func TestVerificationVerdictHandlerCarriesEvidenceIDToTheStore(t *testing.T) {
	store := newVerdictStore()
	handler := NewVerificationVerdictHandler(store, nil)
	evidenceID := "00000000-0000-4000-8000-0000000009e1"

	if err := handler.HandleEvent(context.Background(), verdictEventWithEvidence(t, "event-evidence", verdictObservation, evidenceID)); err != nil {
		t.Fatalf("HandleEvent errored: %v", err)
	}
	if len(store.calls) != 1 {
		t.Fatalf("store writes=%d, want 1", len(store.calls))
	}
	if got := store.calls[0].EvidenceProofID; got != evidenceID {
		t.Fatalf("verdict EvidenceProofID=%q, want %q; without it the store cannot tell which proof was reviewed", got, evidenceID)
	}
}

// A verdict whose evidence has been superseded can never become applicable: the
// attached proof only moves further away from what was reviewed. It must fail
// PERMANENTLY, and as its own typed class rather than as an unclassified store
// error that the bus would retry forever.
func TestVerificationVerdictHandlerFailsPermanentlyOnStaleEvidence(t *testing.T) {
	store := newVerdictStore()
	store.failWith = ports.ErrStaleEvidence
	handler := NewVerificationVerdictHandler(store, nil)

	err := handler.HandleEvent(context.Background(), verdictEventWithEvidence(t, "event-stale", verdictObservation, "00000000-0000-4000-8000-0000000009e2"))
	if err == nil {
		t.Fatal("stale evidence returned nil; the verdict must not be reported as applied")
	}
	if !eventbus.IsPermanentError(err) {
		t.Fatalf("err=%v is retryable; a superseded proof never becomes current again", err)
	}
	if !errors.Is(err, ports.ErrStaleEvidence) {
		t.Fatalf("err=%v does not unwrap to ErrStaleEvidence; the class must survive to the DLQ", err)
	}
}
