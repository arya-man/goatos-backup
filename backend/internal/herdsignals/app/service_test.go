package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// fakeRepo implements only what IngestPackets needs; every other method panics if called, so a
// test that exercises an unexpected path fails loudly instead of silently returning zero values.
type fakeRepo struct {
	ports.Repository
	ingestGotPackets []domain.Packet
	livePages        []domain.TagLatest
}

func (f *fakeRepo) IngestPackets(_ context.Context, _ string, _ domain.Gateway, packets []domain.Packet) (int, int, error) {
	f.ingestGotPackets = packets
	return len(packets), len(packets), nil
}

func (f *fakeRepo) ListTagsLatestPage(_ context.Context, _ string, _, _, _, _, _, _ *string, _ string, limit int, _ ...domain.LiveSort) ([]domain.TagLatest, error) {
	if limit <= 0 || limit > len(f.livePages) {
		limit = len(f.livePages)
	}
	return append([]domain.TagLatest(nil), f.livePages[:limit]...), nil
}

func (f *fakeRepo) ListTagsLatest(_ context.Context, _ string, _, _, movementState, _, _, _ *string, cursor string, limit int, _ ...domain.LiveSort) ([]domain.TagLatest, domain.Summary, *string, error) {
	livePages := f.filteredLivePages(movementState)
	start := 0
	if cursor != "" {
		for i, tag := range livePages {
			if tag.TagID == cursor {
				start = i + 1
				break
			}
		}
	}
	if limit <= 0 {
		limit = len(livePages)
	}
	end := start + limit
	if end > len(livePages) {
		end = len(livePages)
	}
	var next *string
	if end < len(livePages) && end > start {
		v := livePages[end-1].TagID
		next = &v
	}
	return append([]domain.TagLatest(nil), livePages[start:end]...), domain.Summary{}, next, nil
}

func (f *fakeRepo) filteredLivePages(movementState *string) []domain.TagLatest {
	if movementState == nil || *movementState == "" {
		return f.livePages
	}
	filtered := make([]domain.TagLatest, 0, len(f.livePages))
	for _, tag := range f.livePages {
		if tag.MovementState == *movementState {
			filtered = append(filtered, tag)
		}
	}
	return filtered
}

func (f *fakeRepo) ResolveTagsBatch(_ context.Context, _ string, _ []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (f *fakeRepo) GetGoatsByIDs(_ context.Context, _ string, _ []string) (map[string]ports.GoatData, error) {
	return map[string]ports.GoatData{}, nil
}

func (f *fakeRepo) GetShedLocations(_ context.Context, _ string, _ []string) (map[string]ports.ShedLocation, error) {
	return map[string]ports.ShedLocation{}, nil
}

func (f *fakeRepo) GetBaselineDeltas(_ context.Context, _ string, _ []string) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (f *fakeRepo) GetBatteryHistory(_ context.Context, _ string, _ []string, _ int) (map[string]ports.BatteryHistoryPoint, error) {
	return map[string]ports.BatteryHistoryPoint{}, nil
}

// TestIngestPacketsUsesPerPacketGatewaySeenAt is the direct proof for the gateway-payload audit's
// top item: a batch's packets must carry their OWN gateway timestamps, not all be collapsed to
// the single envelope-level relay time.
func TestIngestPacketsUsesPerPacketGatewaySeenAt(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	t1 := "2026-01-01T00:00:00Z" // packet's own gateway clock
	t2 := "2026-01-01T00:00:05Z"
	req := domain.IngestRequest{
		GatewayID:   "gw-1",
		GatewaySeen: "2026-01-01T00:00:10Z", // envelope/relay time: later than either packet's own time
		Packets: []domain.IngestPacket{
			{TagID: "tag-a", TagMAC: "aa:aa:aa:aa:aa:aa", SeenAt: "2026-01-01T00:00:00Z", GatewaySeenAt: &t1},
			{TagID: "tag-b", TagMAC: "bb:bb:bb:bb:bb:bb", SeenAt: "2026-01-01T00:00:05Z", GatewaySeenAt: &t2},
			{TagID: "tag-c", TagMAC: "cc:cc:cc:cc:cc:cc", SeenAt: "2026-01-01T00:00:08Z"}, // no per-packet value: must fall back
		},
	}

	if _, err := svc.IngestPackets(context.Background(), actor, req); err != nil {
		t.Fatalf("IngestPackets: %v", err)
	}

	if len(repo.ingestGotPackets) != 3 {
		t.Fatalf("got %d packets, want 3", len(repo.ingestGotPackets))
	}

	got := make(map[string]string, 3)
	for _, p := range repo.ingestGotPackets {
		if p.GatewaySeenAt == nil {
			t.Fatalf("packet %s has nil GatewaySeenAt, want a value", p.TagID)
		}
		got[p.TagID] = p.GatewaySeenAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	if got["tag-a"] != t1 {
		t.Errorf("tag-a gateway_seen_at = %s, want %s (its own value, not collapsed to the envelope)", got["tag-a"], t1)
	}
	if got["tag-b"] != t2 {
		t.Errorf("tag-b gateway_seen_at = %s, want %s (its own value)", got["tag-b"], t2)
	}
	if got["tag-c"] != req.GatewaySeen {
		t.Errorf("tag-c (no per-packet value) gateway_seen_at = %s, want fallback %s", got["tag-c"], req.GatewaySeen)
	}
	if got["tag-a"] == got["tag-b"] {
		t.Error("tag-a and tag-b were collapsed to the same gateway_seen_at -- the bug this test exists to catch")
	}
}

// TestIngestPacketsStampsReceivedAtFromServerClockNotCaller is the direct proof for the security
// review's HIGH item: a caller-supplied far-future (or otherwise attacker-chosen) seen_at must
// never become received_at. All packets in one call share the server-stamped instant.
func TestIngestPacketsStampsReceivedAtFromServerClockNotCaller(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	before := time.Now().UTC()

	req := domain.IngestRequest{
		GatewayID:   "gw-1",
		GatewaySeen: "2026-01-01T00:00:10Z",
		Packets: []domain.IngestPacket{
			// Attacker-chosen far-future seen_at: must NOT become ReceivedAt (would otherwise
			// permanently freeze this tag's advance-only "latest" guard).
			{TagID: "tag-a", TagMAC: "aa:aa:aa:aa:aa:aa", SeenAt: "2099-01-01T00:00:00Z"},
			{TagID: "tag-b", TagMAC: "bb:bb:bb:bb:bb:bb", SeenAt: "not-a-timestamp"}, // malformed: must still ingest
		},
	}

	if _, err := svc.IngestPackets(context.Background(), actor, req); err != nil {
		t.Fatalf("IngestPackets: %v", err)
	}
	after := time.Now().UTC()

	if len(repo.ingestGotPackets) != 2 {
		t.Fatalf("got %d packets, want 2 (malformed seen_at must not drop the packet)", len(repo.ingestGotPackets))
	}

	for _, p := range repo.ingestGotPackets {
		if p.ReceivedAt.Before(before) || p.ReceivedAt.After(after) {
			t.Errorf("tag %s ReceivedAt = %v, want between %v and %v (server clock, not caller-supplied)", p.TagID, p.ReceivedAt, before, after)
		}
		if p.ReceivedAt.Year() > 2027 {
			t.Errorf("tag %s ReceivedAt = %v, want NOT the caller's far-future value", p.TagID, p.ReceivedAt)
		}
	}

	if repo.ingestGotPackets[0].ReceivedAt != repo.ingestGotPackets[1].ReceivedAt {
		t.Error("packets in the same ingest call must share the same server-stamped ReceivedAt")
	}

	// tag-a's malformed... wait tag-a has a VALID (if far-future) timestamp; tag-b's is malformed.
	var tagB *domain.Packet
	for i := range repo.ingestGotPackets {
		if repo.ingestGotPackets[i].TagID == "tag-b" {
			tagB = &repo.ingestGotPackets[i]
		}
	}
	if tagB == nil {
		t.Fatal("tag-b missing from ingested packets")
	}
	if tagB.DeviceSeenAt != nil {
		t.Errorf("tag-b DeviceSeenAt = %v, want nil (its seen_at was malformed)", tagB.DeviceSeenAt)
	}
}

func TestListLiveRiskFilterPaginatesAfterFilteredRowsAndKeepsWholeSummary(t *testing.T) {
	now := time.Now().UTC()
	falseValue := false
	repo := &fakeRepo{livePages: []domain.TagLatest{
		{TagID: "A00001", LastSeenAt: now, PatternState: "inactive", MappingState: "unmapped", TemperatureSensorOK: &falseValue},
		{TagID: "A00002", LastSeenAt: now.Add(-time.Minute), PatternState: "inactive", MappingState: "unmapped", TemperatureSensorOK: &falseValue},
		{TagID: "A00003", LastSeenAt: now.Add(-2 * time.Minute), PatternState: "inactive", MappingState: "unmapped", TemperatureSensorOK: &falseValue},
	}}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	risk := "high"
	sort := domain.LiveSort{Key: "smart_tag", Dir: "asc"}

	first, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, &risk, nil, "", 2, sort)
	if err != nil {
		t.Fatalf("ListLive first page: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("first page len = %d, want 2", len(first.Items))
	}
	if first.Items[0].TagID != "A00001" || first.Items[1].TagID != "A00002" {
		t.Fatalf("first page tags = %v, want A00001/A00002", []string{first.Items[0].TagID, first.Items[1].TagID})
	}
	if first.NextCursor == nil || *first.NextCursor == "A00002" {
		t.Fatalf("next cursor = %v, want opaque risk cursor", first.NextCursor)
	}
	if first.Summary.TagsSeen != 3 || first.Summary.UnmappedTags != 3 {
		t.Fatalf("summary = %+v, want whole filtered set of 3 unmapped tags", first.Summary)
	}

	second, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, &risk, nil, *first.NextCursor, 2, sort)
	if err != nil {
		t.Fatalf("ListLive second page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].TagID != "A00003" {
		t.Fatalf("second page tags = %+v, want only A00003", second.Items)
	}
	if second.NextCursor != nil {
		t.Fatalf("second next cursor = %v, want nil", *second.NextCursor)
	}
	if second.Summary.TagsSeen != 3 || second.Summary.UnmappedTags != 3 {
		t.Fatalf("second summary = %+v, want whole filtered set of 3 unmapped tags", second.Summary)
	}
}

func TestListLiveRiskFilterWalksPastRepositoryPageBoundary(t *testing.T) {
	now := time.Now().UTC()
	falseValue := false
	pages := make([]domain.TagLatest, liveSignalCohortPageSize+1)
	for i := range pages {
		pages[i] = domain.TagLatest{
			TagID:               fmt.Sprintf("A%05d", i+1),
			LastSeenAt:          now.Add(-time.Duration(i) * time.Minute),
			PatternState:        "normal",
			MappingState:        "unmapped",
			MovementState:       "moving",
			TemperatureSensorOK: &falseValue,
		}
	}
	pages[len(pages)-1].PatternState = "inactive"
	repo := &fakeRepo{livePages: pages}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	risk := "high"
	sort := domain.LiveSort{Key: "smart_tag", Dir: "asc"}

	resp, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, &risk, nil, "", 10, sort)
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].TagID != "A05001" {
		t.Fatalf("risk-filtered tags = %+v, want A05001 past the first repository page", resp.Items)
	}
	if resp.Summary.TagsSeen != 1 {
		t.Fatalf("summary tags_seen = %d, want 1", resp.Summary.TagsSeen)
	}
}

func TestListLiveRiskSummaryKeepsMovementBreakdownWhole(t *testing.T) {
	now := time.Now().UTC()
	falseValue := false
	repo := &fakeRepo{livePages: []domain.TagLatest{
		{TagID: "A00001", LastSeenAt: now, PatternState: "inactive", MappingState: "unmapped", MovementState: "moving", TemperatureSensorOK: &falseValue},
		{TagID: "A00002", LastSeenAt: now.Add(-time.Minute), PatternState: "inactive", MappingState: "unmapped", MovementState: "stale", TemperatureSensorOK: &falseValue},
	}}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	risk := "high"
	movement := "stale"
	sort := domain.LiveSort{Key: "smart_tag", Dir: "asc"}

	resp, err := svc.ListLive(context.Background(), actor, nil, nil, &movement, nil, nil, &risk, nil, "", 10, sort)
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].TagID != "A00002" {
		t.Fatalf("page tags = %+v, want only stale A00002", resp.Items)
	}
	if resp.Summary.TagsSeen != 2 || resp.Summary.Moving != 1 || resp.Summary.Stale != 1 {
		t.Fatalf("summary = %+v, want risk-filtered movement breakdown across both rows", resp.Summary)
	}
}
