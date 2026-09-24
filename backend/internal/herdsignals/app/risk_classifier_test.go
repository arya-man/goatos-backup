package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// N1: steady state is change-driven -- a pass over an unchanged herd classifies nothing; a pass
// after K tags reported (past their re-evaluation floor) classifies exactly those K; the pen baseline is not re-applied when no
// pen moved past the threshold.
func TestRiskClassifierIsChangeDriven(t *testing.T) {
	repo := manyMappedTags(300)
	svc := NewService(repo)
	ctx := context.Background()
	if _, err := svc.RecomputeRisk(ctx, "tenant-1"); err != nil {
		t.Fatal(err)
	}
	st, err := svc.riskPass(ctx, "tenant-1", true, riskMaxQueueBatchesPerTick)
	if err != nil {
		t.Fatal(err)
	}
	if st.Processed != 0 || st.PensMoved != 0 {
		t.Fatalf("unchanged herd: processed %d, pens moved %d; want 0/0", st.Processed, st.PensMoved)
	}
	repo.mu.Lock()
	later := time.Now().Add(6 * time.Minute) // past the 5-minute re-evaluation floor
	for i := 0; i < 7; i++ {
		repo.livePages[i*10].LastSeenAt = later
	}
	repo.mu.Unlock()
	repo.resolvedValueCnt = 0
	st, err = svc.riskPass(ctx, "tenant-1", false, riskMaxQueueBatchesPerTick)
	if err != nil {
		t.Fatal(err)
	}
	if st.Processed != 7 {
		t.Fatalf("after 7 tags reported: processed %d, want 7", st.Processed)
	}
	if repo.resolvedValueCnt > 14 {
		t.Fatalf("enriched %d identifier values for 7 changed tags", repo.resolvedValueCnt)
	}
}

// N1: a pen is re-scored only when its median moves past the threshold, and then only its tags.
func TestRiskPenRescoredOnlyWhenMedianMoves(t *testing.T) {
	stored := map[string]ports.PenMedians{"p1": {MotionMedian: f(100), TempMedian: f(37)}, "p2": {MotionMedian: f(50), TempMedian: f(37)}, "gone": {}}
	live := map[string]ports.PenMedians{"p1": {MotionMedian: f(105), TempMedian: f(37.1)}, "p2": {MotionMedian: f(70), TempMedian: f(37)}, "new": {MotionMedian: f(1)}}
	moved, vanished := movedPens(stored, live)
	if _, ok := moved["p1"]; ok {
		t.Fatalf("p1 moved 5%% / 0.1C (below threshold) but was re-scored")
	}
	if _, ok := moved["p2"]; !ok {
		t.Fatalf("p2 moved 40%% but was not re-scored")
	}
	if _, ok := moved["new"]; !ok || len(vanished) != 1 || vanished[0] != "gone" {
		t.Fatalf("new/vanished pens: moved=%v vanished=%v", moved, vanished)
	}
}

func f(v float64) *float64 { return &v }

// N1: a tick is skipped while the previous one is still running.
func TestRiskClassifierSkipsTickWhilePreviousRuns(t *testing.T) {
	repo := manyMappedTags(3)
	repo.tenantsBlock = make(chan struct{})
	svc := NewService(repo)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.RunRiskClassifier(ctx, 10*time.Millisecond); close(done) }()
	time.Sleep(120 * time.Millisecond)
	repo.mu.Lock()
	calls := repo.tenantCalls
	repo.mu.Unlock()
	close(repo.tenantsBlock)
	cancel()
	<-done
	if calls != 1 {
		t.Fatalf("ticks started while the first was still running: %d, want 1", calls)
	}
}

// N2: a tag the classifier has not reached is shown as classifying (no risk fields) and is
// excluded from the risk filter and its summary -- one consistent answer.
func TestUnclassifiedTagIsConsistentlyClassifying(t *testing.T) {
	repo := manyMappedTags(4) // pattern "inactive" => live classifier would score it
	svc := NewService(repo)
	ctx := context.Background()
	actor := domain.Actor{TenantID: "tenant-1", UserID: "u"}
	resp, err := svc.ListLive(ctx, actor, nil, nil, nil, nil, nil, nil, nil, nil, "", 10, domain.LiveSort{})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range resp.Items {
		if !it.RiskClassifying || it.RiskState != nil || len(it.RiskReasons) != 0 || it.GroupMotionDeltaPct != nil {
			t.Fatalf("unclassified row %s: classifying=%v state=%v reasons=%v", it.TagID, it.RiskClassifying, it.RiskState, it.RiskReasons)
		}
	}
	risk := "attention"
	resp, err = svc.ListLive(ctx, actor, nil, nil, nil, nil, nil, nil, &risk, nil, "", 10, domain.LiveSort{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 0 || resp.Summary.TagsSeen != 0 {
		t.Fatalf("risk filter before classification: %d items / summary %d, want 0/0", len(resp.Items), resp.Summary.TagsSeen)
	}
}

// N3: state, score, reasons and deltas on a row all come from the persisted classification,
// never re-scored against live inputs.
func TestRowRiskComesFromPersistedSource(t *testing.T) {
	repo := manyMappedTags(2)
	now := time.Now().Add(time.Hour)
	high := "high"
	score := int16(4)
	repo.livePages[0].RiskState, repo.livePages[0].RiskScore = &high, &score
	repo.livePages[0].RiskReasons = []string{"persisted reason"}
	repo.livePages[0].RiskGroupMotionDeltaPct, repo.livePages[0].RiskGroupTempDeltaC = f(-80), f(2)
	repo.livePages[0].RiskEvaluatedAt = &now
	svc := NewService(repo)
	resp, err := svc.ListLive(context.Background(), domain.Actor{TenantID: "tenant-1", UserID: "u"}, nil, nil, nil, nil, nil, nil, nil, nil, "", 10, domain.LiveSort{})
	if err != nil {
		t.Fatal(err)
	}
	it := resp.Items[0]
	if it.RiskState == nil || *it.RiskState != "high" || it.RiskScore != 4 || len(it.RiskReasons) != 1 || it.RiskReasons[0] != "persisted reason" ||
		it.GroupMotionDeltaPct == nil || *it.GroupMotionDeltaPct != -80 || it.GroupTempDeltaC == nil || *it.GroupTempDeltaC != 2 || it.RiskClassifying {
		t.Fatalf("row risk = %+v, want the persisted high/4/reason/-80/+2", it)
	}
}

// N5: the CSV export's risk filter is the persisted classification (same as the page), not a
// re-scored in-memory cohort.
func TestExportRiskFilterUsesPersistedClassification(t *testing.T) {
	repo := manyMappedTags(3)
	now := time.Now().Add(time.Hour)
	high := "high"
	repo.livePages[1].RiskState, repo.livePages[1].RiskEvaluatedAt = &high, &now
	repo.livePages[0].RiskEvaluatedAt, repo.livePages[2].RiskEvaluatedAt = &now, &now
	svc := NewService(repo)
	var buf bytes.Buffer
	if err := svc.ExportCSV(context.Background(), domain.Actor{TenantID: "tenant-1", UserID: "u"}, nil, nil, nil, nil, nil, nil, &high, nil, domain.LiveSort{}, &buf); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[1][1] != repo.livePages[1].TagID {
		t.Fatalf("export rows = %v, want only the persisted-high tag %s", recs, repo.livePages[1].TagID)
	}
}

// N4 (maintainer 2026-09-24): the comparison group is the pen the animal resides in -- a divided
// shed's Part 1 and Part 2 are separate groups; an undivided shed (even one whose name ends in a
// number) is one group.
func TestRiskPenGroupIsThePartitionNotTheShed(t *testing.T) {
	now := time.Now().UTC()
	divided := "30000000-0000-4000-8000-00000000000d"
	whole := "30000000-0000-4000-8000-00000000000w"
	p1, p2, wholeLbl := "Part 1", "Part 2", "whole"
	repo := &fakeRepo{resolvedTags: map[string]string{}, goats: map[string]ports.GoatData{}}
	add := func(tag string, motion int64, shed string, label *string) {
		m := motion
		repo.livePages = append(repo.livePages, domain.TagLatest{TagID: tag, LastSeenAt: now, PatternState: "normal", MappingState: "mapped", MotionDelta: &m})
		goat := "goat-" + tag
		repo.resolvedTags[tag] = goat
		s := shed
		repo.goats[goat] = ports.GoatData{DisplayID: tag, ShedID: &s, PartitionLabel: label}
	}
	add("P1A", 100, divided, &p1)
	add("P1B", 100, divided, &p1)
	add("P2A", 10, divided, &p2)
	add("P2B", 10, divided, &p2)
	add("W1", 40, whole, nil)
	add("W2", 60, whole, &wholeLbl)
	svc := NewService(repo)
	if _, err := svc.RecomputeRisk(context.Background(), "tenant-1"); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for k := range repo.penMedians {
		keys[k] = true
	}
	for _, want := range []string{divided + "#1", divided + "#2", whole + "#whole"} {
		if !keys[want] {
			t.Fatalf("pen keys = %v, missing %s", keys, want)
		}
	}
	if len(keys) != 3 {
		t.Fatalf("pen keys = %v, want exactly Part 1, Part 2 and the undivided shed", keys)
	}
	byTag := map[string]domain.TagLatest{}
	for _, tag := range repo.livePages {
		byTag[tag.TagID] = tag
	}
	// Part 2 animals sit AT their own pen's median (10), not 90%% below the shed-wide median (55).
	if p := byTag["P2A"].RiskGroupMotionDeltaPct; p == nil || *p != 0 {
		t.Fatalf("P2A pen delta = %v, want 0 vs its own partition", p)
	}
	if st := byTag["P2A"].RiskState; st != nil {
		t.Fatalf("P2A risk = %v, want none (it is normal for its partition)", *st)
	}
	// The undivided shed's 'whole' and absent partition rows are one group (median 50).
	if p := byTag["W1"].RiskGroupMotionDeltaPct; p == nil || *p != -20 {
		t.Fatalf("W1 pen delta = %v, want -20 vs the whole shed's median 50", p)
	}
}
