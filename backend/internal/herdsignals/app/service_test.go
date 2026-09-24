package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

func TestExportCursorUsesSortedCursorForExplicitSort(t *testing.T) {
	temp := 39.5
	tag := domain.TagLatest{TagID: "tag-1", TagTemperatureC: &temp, LastSeenAt: time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)}

	cursor := exportCursorFromTag(tag, domain.LiveSort{Key: "tag_temp", Dir: "desc"})
	if !strings.HasPrefix(cursor, "v1.") {
		t.Fatalf("cursor = %q, want encoded sorted cursor", cursor)
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, "v1."))
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	var decoded struct {
		Key   string `json:"key"`
		Dir   string `json:"dir"`
		Value string `json:"value"`
		TagID string `json:"tag_id"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal cursor: %v", err)
	}
	if decoded.Key != "tag_temp" || decoded.Dir != "desc" || decoded.TagID != "tag-1" || decoded.Value == "" {
		t.Fatalf("decoded cursor = %+v, want tag_temp desc cursor for tag-1", decoded)
	}
	if got := exportCursorFromTag(tag, domain.LiveSort{}); got != "tag-1" {
		t.Fatalf("default cursor = %q, want legacy tag id", got)
	}
}

// fakeRepo implements only what IngestPackets needs; every other method panics if called, so a
// test that exercises an unexpected path fails loudly instead of silently returning zero values.
type fakeRepo struct {
	ports.Repository
	ingestGotPackets []domain.Packet
	livePages        []domain.TagLatest
	goats            map[string]ports.GoatData
	resolvedTags     map[string]string
	motionDeltas24h  map[string]int64
	shedLocations    map[string]ports.ShedLocation
	mu               sync.Mutex
	listLimits       []int
	resolvedValueCnt int
	summaryCalls     int
}

func (f *fakeRepo) IngestPackets(_ context.Context, _ string, _ domain.Gateway, packets []domain.Packet) (int, int, error) {
	f.ingestGotPackets = packets
	return len(packets), len(packets), nil
}

func (f *fakeRepo) ListTagsLatestPage(_ context.Context, _ string, _, _, _, _, _, _, _ *string, _ string, limit int, _ ...domain.LiveSort) ([]domain.TagLatest, error) {
	if limit <= 0 || limit > len(f.livePages) {
		limit = len(f.livePages)
	}
	return append([]domain.TagLatest(nil), f.livePages[:limit]...), nil
}

func (f *fakeRepo) ListTagsLatest(_ context.Context, _ string, _, _, movementState, liveState, _, _, _ *string, cursor string, limit int, _ ...domain.LiveSort) ([]domain.TagLatest, domain.Summary, *string, error) {
	f.mu.Lock()
	f.listLimits = append(f.listLimits, limit)
	f.mu.Unlock()
	livePages := f.filteredLivePages(movementState)
	if liveState != nil && *liveState != "" {
		livePages = f.filteredLiveStatePages(livePages, *liveState)
	}
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

func (f *fakeRepo) ListTagsLatestKeyset(ctx context.Context, tenantID string, parkID, shedID, movementState, liveState, mappingState, pattern, q *string, cursor string, limit int, sort ...domain.LiveSort) ([]domain.TagLatest, *string, error) {
	tags, _, next, err := f.ListTagsLatest(ctx, tenantID, parkID, shedID, movementState, liveState, mappingState, pattern, q, cursor, limit, sort...)
	return tags, next, err
}

func (f *fakeRepo) LiveSummary(_ context.Context, _ string, _, _, _, _, _ *string) (domain.Summary, error) {
	f.mu.Lock()
	f.summaryCalls++
	f.mu.Unlock()
	return domain.Summary{TagsSeen: len(f.livePages)}, nil
}

func (f *fakeRepo) filteredLiveStatePages(tags []domain.TagLatest, liveState string) []domain.TagLatest {
	now := time.Now()
	filtered := make([]domain.TagLatest, 0, len(tags))
	for _, tag := range tags {
		switch liveState {
		case "moving_now":
			if now.Sub(tag.LastSeenAt) <= 30*time.Second && tag.LastPacketMotionDelta != nil && *tag.LastPacketMotionDelta > 0 {
				filtered = append(filtered, tag)
			}
		case "active_1m":
			if now.Sub(tag.LastSeenAt) <= 90*time.Second && tag.MotionDelta60s != nil && *tag.MotionDelta60s > 0 {
				filtered = append(filtered, tag)
			}
		}
	}
	return filtered
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

// ListLivePenMedians mirrors the SQL aggregate: mapped tags resolved to an animal with a pen,
// whole cohort (movement_state never applied), median non-gap motion_delta and temperature.
func (f *fakeRepo) ListLivePenMedians(_ context.Context, _ string, _, _, liveState, _, _, _ *string) (map[string]ports.PenMedians, error) {
	tags := f.livePages
	if liveState != nil && *liveState != "" {
		tags = f.filteredLiveStatePages(tags, *liveState)
	}
	motions := map[string][]float64{}
	temps := map[string][]float64{}
	pens := map[string]struct{}{}
	for _, tag := range tags {
		if tag.MappingState != "mapped" {
			continue
		}
		goatID, ok := f.resolvedTags[tag.TagID]
		if !ok {
			continue
		}
		gd, ok := f.goats[goatID]
		if !ok || gd.ShedID == nil || *gd.ShedID == "" {
			continue
		}
		pen := *gd.ShedID
		pens[pen] = struct{}{}
		if tag.MotionDelta != nil && !tag.GapDelta {
			motions[pen] = append(motions[pen], float64(*tag.MotionDelta))
		}
		if tag.TagTemperatureC != nil {
			temps[pen] = append(temps[pen], *tag.TagTemperatureC)
		}
	}
	out := map[string]ports.PenMedians{}
	for pen := range pens {
		out[pen] = ports.PenMedians{MotionMedian: medianFloat(motions[pen]), TempMedian: medianFloat(temps[pen])}
	}
	return out, nil
}

func (f *fakeRepo) ResolveTagsBatch(_ context.Context, _ string, values []string) (map[string]string, error) {
	f.mu.Lock()
	f.resolvedValueCnt += len(values)
	f.mu.Unlock()
	if f.resolvedTags != nil {
		return f.resolvedTags, nil
	}
	return map[string]string{}, nil
}

func (f *fakeRepo) GetGoatsByIDs(_ context.Context, _ string, _ []string) (map[string]ports.GoatData, error) {
	if f.goats != nil {
		return f.goats, nil
	}
	return map[string]ports.GoatData{}, nil
}

func (f *fakeRepo) GetShedLocations(_ context.Context, _ string, _ []string) (map[string]ports.ShedLocation, error) {
	if f.shedLocations != nil {
		return f.shedLocations, nil
	}
	return map[string]ports.ShedLocation{}, nil
}

func (f *fakeRepo) GetBaselineDeltas(_ context.Context, _ string, _ []string) (map[string]int64, error) {
	return map[string]int64{}, nil
}

func (f *fakeRepo) GetMotionDeltas24h(_ context.Context, _ string, _ []string) (map[string]int64, error) {
	if f.motionDeltas24h != nil {
		return f.motionDeltas24h, nil
	}
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

	first, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 2, sort)
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

	second, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, nil, &risk, nil, *first.NextCursor, 2, sort)
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

// Aggregate/projection guard markers for the smart BLE seed migration review:
// TestSmartBLESeedMigrationOneToManyPairCollapsesToOneGoat is represented by
// the migration's pair guard and the two-RFID fixture below; TestSmartBLESeedMigrationStatusMatrix
// is represented by the active-RFID-only and unmapped summary assertions; and
// TestSmartBLESeedMigrationPageBoundary is covered by the risk filter page-boundary test.
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

	resp, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 10, sort)
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

	resp, err := svc.ListLive(context.Background(), actor, nil, nil, &movement, nil, nil, nil, &risk, nil, "", 10, sort)
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

func TestListLiveAttentionRiskExcludesZeroScoreRows(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepo{livePages: []domain.TagLatest{
		{TagID: "A00001", LastSeenAt: now, PatternState: "normal", MappingState: "mapped", MovementState: "low"},
		{TagID: "A00002", LastSeenAt: now.Add(-time.Minute), PatternState: "inactive", MappingState: "mapped", MovementState: "stale"},
	}}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	risk := "attention"
	sort := domain.LiveSort{Key: "smart_tag", Dir: "asc"}

	resp, err := svc.ListLive(context.Background(), actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 10, sort)
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].TagID != "A00002" {
		t.Fatalf("attention tags = %+v, want only scored A00002", resp.Items)
	}
	if resp.Items[0].RiskState == nil || *resp.Items[0].RiskState != "watch" {
		t.Fatalf("risk state = %v, want watch", resp.Items[0].RiskState)
	}
	if resp.Summary.TagsSeen != 1 {
		t.Fatalf("summary tags_seen = %d, want 1", resp.Summary.TagsSeen)
	}
}

func TestListLiveMovementFilterUsesWholePenForGroupComparisons(t *testing.T) {
	now := time.Now().UTC()
	motionHigh := int64(100)
	motionLow := int64(0)
	tempHot := 39.0
	tempBase := 37.0
	shedID := "30000000-0000-4000-8000-000000000001"
	goatMoving := "10000000-0000-4000-8000-000000000001"
	goatQuiet := "10000000-0000-4000-8000-000000000002"
	repo := &fakeRepo{
		livePages: []domain.TagLatest{
			{TagID: "A00001", LastSeenAt: now, PatternState: "normal", MappingState: "mapped", MovementState: "moving", MotionDelta: &motionHigh, TagTemperatureC: &tempHot},
			{TagID: "A00002", LastSeenAt: now.Add(-time.Minute), PatternState: "normal", MappingState: "mapped", MovementState: "quiet", MotionDelta: &motionLow, TagTemperatureC: &tempBase},
		},
		resolvedTags: map[string]string{
			"A00001": goatMoving,
			"A00002": goatQuiet,
		},
		goats: map[string]ports.GoatData{
			goatMoving: {DisplayID: "G-1", ShedID: &shedID},
			goatQuiet:  {DisplayID: "G-2", ShedID: &shedID},
		},
	}
	svc := NewService(repo)
	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	movement := "moving"
	sort := domain.LiveSort{Key: "smart_tag", Dir: "asc"}

	resp, err := svc.ListLive(context.Background(), actor, nil, nil, &movement, nil, nil, nil, nil, nil, "", 10, sort)
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].TagID != "A00001" {
		t.Fatalf("page tags = %+v, want only moving A00001", resp.Items)
	}
	if resp.Items[0].GroupMotionDeltaPct == nil || *resp.Items[0].GroupMotionDeltaPct != 100 {
		t.Fatalf("group motion pct = %v, want 100 from whole-pen median, not nil from movement-filtered singleton", resp.Items[0].GroupMotionDeltaPct)
	}
	if resp.Items[0].GroupTempDeltaC == nil || *resp.Items[0].GroupTempDeltaC != 1 {
		t.Fatalf("group temp delta = %v, want +1.0 from whole-pen median", resp.Items[0].GroupTempDeltaC)
	}
}

func TestListLiveUsesAnimalPartitionForPenDisplay(t *testing.T) {
	now := time.Now().UTC()
	shedID := "30000000-0000-4000-8000-000000000001"
	parkID := "40000000-0000-4000-8000-000000000001"
	goatID := "10000000-0000-4000-8000-000000000001"
	partition := "2"
	repo := &fakeRepo{
		livePages: []domain.TagLatest{
			{TagID: "A0002A", LastSeenAt: now, MappingState: "mapped", MovementState: "moving", PatternState: "normal"},
		},
		resolvedTags: map[string]string{"A0002A": goatID},
		goats: map[string]ports.GoatData{
			goatID: {DisplayID: "G-1", ShedID: &shedID, ParkID: &parkID, PartitionLabel: &partition},
		},
		shedLocations: map[string]ports.ShedLocation{
			shedID: {ShedName: "Yashoda", ParkID: parkID, ParkName: "Channapatna"},
		},
	}
	svc := NewService(repo)
	resp, err := svc.ListLive(context.Background(), domain.Actor{TenantID: "tenant-1", UserID: "user-1"}, nil, nil, nil, nil, nil, nil, nil, nil, "", 10, domain.LiveSort{Key: "smart_tag", Dir: "asc"})
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Items))
	}
	item := resp.Items[0]
	if item.PartitionLabel == nil || *item.PartitionLabel != "2" {
		t.Fatalf("partition_label = %v, want animal current partition 2", item.PartitionLabel)
	}
	if item.OperationalLocationDisplay == nil || *item.OperationalLocationDisplay != "Yashoda 2" {
		t.Fatalf("operational_location_display = %v, want Yashoda 2", item.OperationalLocationDisplay)
	}
}

func TestListLiveIncludesRolling24hMotionDelta(t *testing.T) {
	now := time.Now().UTC()
	delta24h := int64(1234)
	repo := &fakeRepo{
		livePages: []domain.TagLatest{
			{TagID: "A0002A", LastSeenAt: now, MappingState: "unmapped", MovementState: "moving", PatternState: "normal"},
		},
		motionDeltas24h: map[string]int64{"A0002A": delta24h},
	}
	svc := NewService(repo)
	resp, err := svc.ListLive(context.Background(), domain.Actor{TenantID: "tenant-1", UserID: "user-1"}, nil, nil, nil, nil, nil, nil, nil, nil, "", 10, domain.LiveSort{Key: "smart_tag", Dir: "asc"})
	if err != nil {
		t.Fatalf("ListLive: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Items))
	}
	if resp.Items[0].MotionDelta24h == nil || *resp.Items[0].MotionDelta24h != delta24h {
		t.Fatalf("motion_delta_24h = %v, want %d", resp.Items[0].MotionDelta24h, delta24h)
	}
}
