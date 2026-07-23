package bootstrap

// Proves the P1 fix on the real path: a scoped question carrying park_label
// (or shed_id) in sub.Params must reach the underlying service query, not get
// silently dropped by the reader closure. Each fake below captures the exact
// query it received; the assertions fail if that query is unscoped.

import (
	"context"
	"testing"

	locationsdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
	locationsports "github.com/vgoats/goatos/backend/internal/locations/ports"
	processintegritydomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	procurementdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// --- fakes ---

type fakeParkResolver struct {
	labelToID map[string]string
}

func (f *fakeParkResolver) ResolveParkID(ctx context.Context, tenantID, parkLabel string) (string, bool, error) {
	id, ok := f.labelToID[parkLabel]
	return id, ok, nil
}

type fakeLocationsLister struct {
	// byName simulates the locations read model: only these parks exist.
	byName map[string]string // name -> location_id
}

func (f *fakeLocationsLister) ListLocations(ctx context.Context, params locationsports.ListParams, traceID string) (*locationsdomain.LocationListResponse, error) {
	id, ok := f.byName[params.Search]
	if !ok {
		return &locationsdomain.LocationListResponse{}, nil
	}
	return &locationsdomain.LocationListResponse{
		Items: []locationsdomain.LocationSummary{{LocationID: id, LocationType: "park", Name: params.Search}},
	}, nil
}

type fakeActionCenterLister struct {
	captured processintegritydomain.Query
}

func (f *fakeActionCenterLister) ActionCenter(ctx context.Context, q processintegritydomain.Query) (processintegritydomain.ActionCenterResponse, error) {
	f.captured = q
	return processintegritydomain.ActionCenterResponse{}, nil
}

type fakeOpsKernelHealthLister struct {
	captured processintegritydomain.Query
}

func (f *fakeOpsKernelHealthLister) ControlTower(ctx context.Context, q processintegritydomain.Query) (processintegritydomain.ControlTowerResponse, error) {
	f.captured = q
	return processintegritydomain.ControlTowerResponse{}, nil
}

type fakeProcurementLoadLister struct {
	captured procurementdomain.LoadQuery
}

func (f *fakeProcurementLoadLister) ListLoads(ctx context.Context, q procurementdomain.LoadQuery) (procurementdomain.LoadListResult, error) {
	f.captured = q
	return procurementdomain.LoadListResult{}, nil
}

// --- tests ---

// TestActionCenterReader_ParkLabelReachesQuery is the flagship P1 regression
// test: before the fix, buildActionCenterReader's predecessor (the inline
// closure in api.go) never read params["park_label"] at all, so q.ParkID
// stayed nil and the question silently ran tenant-wide. This test fails
// against that old behavior and passes against the fix.
func TestActionCenterReader_ParkLabelReachesQuery(t *testing.T) {
	fakeSvc := &fakeActionCenterLister{}
	resolver := &fakeParkResolver{labelToID: map[string]string{"Castro 1": "park-uuid-castro-1"}}
	reader := buildActionCenterReader(fakeSvc, resolver)

	_, err := reader(context.Background(), "tenant-1", map[string]any{"park_label": "Castro 1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeSvc.captured.ParkID == nil {
		t.Fatal("park_label did NOT reach the service query: q.ParkID is nil -- scoped question would silently run tenant-wide")
	}
	if *fakeSvc.captured.ParkID != "park-uuid-castro-1" {
		t.Fatalf("q.ParkID = %q, want resolved park-uuid-castro-1", *fakeSvc.captured.ParkID)
	}
}

// TestActionCenterReader_UnresolvableParkLabelFailsClosed proves the reader
// does NOT fall back to tenant-wide data when the label can't be resolved --
// it must error instead of silently answering unscoped.
func TestActionCenterReader_UnresolvableParkLabelFailsClosed(t *testing.T) {
	fakeSvc := &fakeActionCenterLister{}
	resolver := &fakeParkResolver{labelToID: map[string]string{}}
	reader := buildActionCenterReader(fakeSvc, resolver)

	_, err := reader(context.Background(), "tenant-1", map[string]any{"park_label": "Nonexistent Park"})
	if err == nil {
		t.Fatal("expected an error when park_label does not resolve, got nil (would silently answer tenant-wide)")
	}
}

// TestActionCenterReader_ShedIDReachesQuery covers the shed/partition follow-up:
// shed_id is a plain shed-location ID field on processintegritydomain.Query and
// must reach q.ShedID directly (no resolver needed, unlike park_label).
func TestActionCenterReader_ShedIDReachesQuery(t *testing.T) {
	fakeSvc := &fakeActionCenterLister{}
	resolver := &fakeParkResolver{}
	reader := buildActionCenterReader(fakeSvc, resolver)

	_, err := reader(context.Background(), "tenant-1", map[string]any{"shed_id": "shed-uuid-gandhi-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeSvc.captured.ShedID == nil || *fakeSvc.captured.ShedID != "shed-uuid-gandhi-1" {
		t.Fatal("shed_id did NOT reach the service query: q.ShedID is nil/mismatched")
	}
}

// TestActionCenterReader_WorkStateStillMapped is a regression guard: the
// pre-existing work_state mapping (the one param that was never dropped)
// must keep working after the refactor into buildActionCenterReader.
func TestActionCenterReader_WorkStateStillMapped(t *testing.T) {
	fakeSvc := &fakeActionCenterLister{}
	resolver := &fakeParkResolver{}
	reader := buildActionCenterReader(fakeSvc, resolver)

	_, err := reader(context.Background(), "tenant-1", map[string]any{"work_state": "overdue"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeSvc.captured.WorkState == nil || string(*fakeSvc.captured.WorkState) != "overdue" {
		t.Fatal("work_state did not reach the service query")
	}
}

// TestOpsKernelHealthReader_ShedIDReachesQuery: same shed-scoping discipline
// for the newly-wired operations_kernel_health tool.
func TestOpsKernelHealthReader_ShedIDReachesQuery(t *testing.T) {
	fakeSvc := &fakeOpsKernelHealthLister{}
	reader := buildOpsKernelHealthReader(fakeSvc)

	_, err := reader(context.Background(), "tenant-1", map[string]any{"shed_id": "shed-uuid-gandhi-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeSvc.captured.ShedID == nil || *fakeSvc.captured.ShedID != "shed-uuid-gandhi-1" {
		t.Fatal("shed_id did NOT reach operations_kernel_health's service query")
	}
}

// TestProcurementReader_StatusReachesQuery_NoParkField proves procurement's
// LoadQuery has no park field to drop in the first place: park_label in
// params must be silently ignored (not erroring, not crashing), while status
// (the one real, honored param) reaches the query.
func TestProcurementReader_StatusReachesQuery_NoParkField(t *testing.T) {
	fakeSvc := &fakeProcurementLoadLister{}
	reader := buildProcurementReader(fakeSvc)

	_, err := reader(context.Background(), "tenant-1", map[string]any{"status": "in_transit", "park_label": "Castro 1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fakeSvc.captured.Status != "in_transit" {
		t.Fatalf("status = %q, want in_transit", fakeSvc.captured.Status)
	}
	// procurementdomain.LoadQuery has no park field at all, so there is
	// nothing further to assert -- this documents the limitation rather than
	// faking scoping the pipeline cannot honor.
}

// TestLocationsParkResolver_ExactNameMatch exercises the resolver used by
// action_center_obligations against a fake locations read model.
func TestLocationsParkResolver_ExactNameMatch(t *testing.T) {
	lister := &fakeLocationsLister{byName: map[string]string{"Castro 1": "park-uuid-castro-1"}}
	resolver := newLocationsParkResolver(lister)

	id, ok, err := resolver.ResolveParkID(context.Background(), "tenant-1", "Castro 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || id != "park-uuid-castro-1" {
		t.Fatalf("got id=%q ok=%v, want park-uuid-castro-1/true", id, ok)
	}

	_, ok, err = resolver.ResolveParkID(context.Background(), "tenant-1", "Nonexistent Park")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an unknown park label")
	}
}
