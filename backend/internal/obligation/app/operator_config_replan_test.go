package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// fakeOperatorConfigReplanRepo is an in-memory double for operatorConfigReplanRepository +
// shedParkResolver. claimed tracks (tenantID, eventID) with "pending"/"succeeded" status
// so a second claim attempt for the same key returns applied=false, exactly like the real
// ON CONFLICT DO NOTHING watermark row.
type fakeOperatorConfigReplanRepo struct {
	mu             sync.Mutex
	claimed        map[string]string // event key -> "pending" or "succeeded"
	recomputeCalls []recomputeCall
	shedToPark     map[string]string
	recomputeErr   error
}

type recomputeCall struct {
	tenantID, parkID string
	effectiveFrom    time.Time
}

func newFakeOperatorConfigReplanRepo() *fakeOperatorConfigReplanRepo {
	return &fakeOperatorConfigReplanRepo{claimed: map[string]string{}, shedToPark: map[string]string{}}
}

func (f *fakeOperatorConfigReplanRepo) ClaimOperatorConfigReplanWatermarkPending(_ context.Context, tenantID, _parkID, _eventType, eventID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + ":" + eventID
	if _, exists := f.claimed[key]; exists {
		return false, nil
	}
	f.claimed[key] = "pending"
	return true, nil
}

func (f *fakeOperatorConfigReplanRepo) MarkOperatorConfigReplanWatermarkSucceeded(_ context.Context, tenantID, eventID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + ":" + eventID
	if _, exists := f.claimed[key]; !exists {
		return fmt.Errorf("watermark not found for event %s", eventID)
	}
	f.claimed[key] = "succeeded"
	return nil
}

func (f *fakeOperatorConfigReplanRepo) GetOperatorConfigReplanWatermarkStatus(_ context.Context, tenantID, eventID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenantID + ":" + eventID
	status, exists := f.claimed[key]
	if !exists {
		return "", nil // not found
	}
	return status, nil
}

func (f *fakeOperatorConfigReplanRepo) RecomputeFutureVaccinationDrives(_ context.Context, tenantID, parkID string, effectiveFrom time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recomputeErr != nil {
		return 0, f.recomputeErr
	}
	f.recomputeCalls = append(f.recomputeCalls, recomputeCall{tenantID: tenantID, parkID: parkID, effectiveFrom: effectiveFrom})
	return 1, nil
}

func (f *fakeOperatorConfigReplanRepo) ParkIDForShed(_ context.Context, _tenantID, shedID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parkID, ok := f.shedToPark[shedID]
	if !ok {
		return "", fmt.Errorf("no park mapped for shed %s", shedID)
	}
	return parkID, nil
}

func (f *fakeOperatorConfigReplanRepo) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.recomputeCalls)
}

const (
	testTenantID = "00000000-0000-4000-8000-000000000001"
	testParkID   = "20000000-0000-4000-8000-000000000001"
	testShedID   = "30000000-0000-4000-8000-000000000001"
)

// TestOperatorConfigReplanHandlerCapacityChangedTriggersRecompute is the RED-then-GREEN target for the
// capacity/N producer path: HandleEvent on vaccination.capacity.changed calls
// RecomputeFutureVaccinationDrives for the event's park.
func TestOperatorConfigReplanHandlerCapacityChangedTriggersRecompute(t *testing.T) {
	repo := newFakeOperatorConfigReplanRepo()
	h := &OperatorConfigReplanHandler{repo: repo}

	err := h.HandleEvent(context.Background(), eventbus.Event{
		ID:       "vaccination.operator-assignment-config.capacity:" + testParkID + ":2",
		Type:     EventVaccinationCapacityChanged,
		TenantID: testTenantID,
		Key:      testParkID,
		Payload:  []byte(fmt.Sprintf(`{"park_id":%q}`, testParkID)),
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("recompute calls = %d, want 1", got)
	}
	if repo.recomputeCalls[0].parkID != testParkID {
		t.Fatalf("recompute parkID = %q, want %q", repo.recomputeCalls[0].parkID, testParkID)
	}
}

// TestOperatorConfigReplanHandlerRosterChangedTriggersRecompute proves the default-operator/roster
// producer path dispatches identically to the capacity path (shared payload shape).
func TestOperatorConfigReplanHandlerRosterChangedTriggersRecompute(t *testing.T) {
	repo := newFakeOperatorConfigReplanRepo()
	h := &OperatorConfigReplanHandler{repo: repo}

	err := h.HandleEvent(context.Background(), eventbus.Event{
		ID:       "vaccination.operator-assignment-config.roster:" + testParkID + ":2",
		Type:     EventVaccinationRosterChanged,
		TenantID: testTenantID,
		Key:      testParkID,
		Payload:  []byte(fmt.Sprintf(`{"park_id":%q}`, testParkID)),
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("recompute calls = %d, want 1", got)
	}
}

// TestOperatorConfigReplanHandlerLeaveChangedResolvesShedToPark proves the shed-scoped
// vaccination.leave.changed payload is resolved to its park before recompute is called.
func TestOperatorConfigReplanHandlerLeaveChangedResolvesShedToPark(t *testing.T) {
	repo := newFakeOperatorConfigReplanRepo()
	repo.shedToPark[testShedID] = testParkID
	h := &OperatorConfigReplanHandler{repo: repo}

	err := h.HandleEvent(context.Background(), eventbus.Event{
		ID:       "vaccination.leave-changed:absence-1",
		Type:     EventVaccinationLeaveChanged,
		TenantID: testTenantID,
		Key:      testShedID,
		Payload:  []byte(fmt.Sprintf(`{"scope_type":"shed","scope_id":%q}`, testShedID)),
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("recompute calls = %d, want 1", got)
	}
	if repo.recomputeCalls[0].parkID != testParkID {
		t.Fatalf("recompute parkID = %q, want resolved park %q", repo.recomputeCalls[0].parkID, testParkID)
	}
}

// TestOperatorConfigReplanHandlerIdempotentReplayIsNoOp is the mandatory idempotency test: firing the
// SAME event twice must call RecomputeFutureVaccinationDrives exactly once. The second HandleEvent call
// must fail the watermark claim (applied=false) and return nil without re-invoking recompute.
func TestOperatorConfigReplanHandlerIdempotentReplayIsNoOp(t *testing.T) {
	repo := newFakeOperatorConfigReplanRepo()
	h := &OperatorConfigReplanHandler{repo: repo}
	event := eventbus.Event{
		ID:       "vaccination.operator-assignment-config.capacity:" + testParkID + ":3",
		Type:     EventVaccinationCapacityChanged,
		TenantID: testTenantID,
		Key:      testParkID,
		Payload:  []byte(fmt.Sprintf(`{"park_id":%q}`, testParkID)),
	}

	if err := h.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("first HandleEvent: %v", err)
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("after first delivery, recompute calls = %d, want 1", got)
	}

	// Exact replay: same event id, at-least-once redelivery.
	if err := h.HandleEvent(context.Background(), event); err != nil {
		t.Fatalf("replayed HandleEvent: %v", err)
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("after replayed delivery, recompute calls = %d, want still 1 (idempotent no-op)", got)
	}
}

// TestOperatorConfigReplanHandlerConcurrentDuplicateDeliveryIsSerializedToOneRecompute is a handler-
// level sanity check (in-memory fake, NOT the mandatory Postgres concurrency-race test -- that is
// backend/internal/obligation/adapters/postgres/operator_config_replan_integration_test.go, which
// exercises the real ON CONFLICT DO NOTHING watermark row and the real shared tenant-sweep advisory
// lock against a live sweeper). It proves the HANDLER always calls ClaimOperatorConfigReplanWatermark
// before RecomputeFutureVaccinationDrives for every delivery, so two concurrent deliveries of the
// identical event id call the claim exactly twice and recompute at most once given a
// claim-serializes-duplicates repository (which the real Postgres unique constraint provides).
func TestOperatorConfigReplanHandlerConcurrentDuplicateDeliveryIsSerializedToOneRecompute(t *testing.T) {
	repo := newFakeOperatorConfigReplanRepo()
	h := &OperatorConfigReplanHandler{repo: repo}
	event := eventbus.Event{
		ID:       "vaccination.operator-assignment-config.capacity:" + testParkID + ":4",
		Type:     EventVaccinationCapacityChanged,
		TenantID: testTenantID,
		Key:      testParkID,
		Payload:  []byte(fmt.Sprintf(`{"park_id":%q}`, testParkID)),
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- h.HandleEvent(context.Background(), event)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent HandleEvent returned error: %v", err)
		}
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("concurrent duplicate delivery: recompute calls = %d, want exactly 1 (watermark must serialize)", got)
	}
}
