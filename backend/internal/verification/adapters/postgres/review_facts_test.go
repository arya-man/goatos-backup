package postgres

import (
	"testing"
	"time"
)

// These exercise computeActorFacts directly (no database): the derivation is where the CEO-facing
// "did the verifier actually watch it" number comes from, and both bugs below shipped as green code.
func ptr[T any](v T) *T { return &v }

func TestWatchFactsKeepProofTimelinesSeparate(t *testing.T) {
	base := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	proofA, proofB := "aaaaaaaa-0000-4000-8000-000000000001", "bbbbbbbb-0000-4000-8000-000000000002"
	// Half of a 60s video, then half of a 90s video. Both start at position 0, so pooling them onto
	// one timeline merged the two into a single [0,40s) island and invented a fraction neither had.
	rows := []reviewEventRow{
		{actorID: "actor", eventType: "video_play", occurredAt: base, positionMs: ptr(int64(0)), durationMs: ptr(int64(60000)), proofID: &proofA},
		{actorID: "actor", eventType: "video_pause", occurredAt: base.Add(30 * time.Second), positionMs: ptr(int64(30000)), durationMs: ptr(int64(60000)), proofID: &proofA},
		{actorID: "actor", eventType: "proof_switched", occurredAt: base.Add(31 * time.Second), proofID: &proofB},
		{actorID: "actor", eventType: "video_play", occurredAt: base.Add(32 * time.Second), positionMs: ptr(int64(0)), durationMs: ptr(int64(90000)), proofID: &proofB},
		{actorID: "actor", eventType: "video_pause", occurredAt: base.Add(72 * time.Second), positionMs: ptr(int64(40000)), durationMs: ptr(int64(90000)), proofID: &proofB},
	}
	facts := computeActorFacts("item", "actor", rows)

	if facts.WatchedDistinctMs != 70000 {
		t.Fatalf("watched across both proofs = %d ms, want 70000 (30s of A + 40s of B, not a merged 40s)", facts.WatchedDistinctMs)
	}
	if facts.ProofDurationMs != 150000 {
		t.Fatalf("total evidence duration = %d ms, want 150000 (60s + 90s, not max(60s,90s))", facts.ProofDurationMs)
	}
	if got := facts.WatchFraction; got < 0.46 || got > 0.47 {
		t.Fatalf("watch fraction = %v, want ~0.4667", got)
	}
	if facts.WatchedFull {
		t.Fatal("under half of the evidence watched must never read as fully watched")
	}
}

func TestWatchFactsRejectAnImpossibleWatchClaim(t *testing.T) {
	base := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	proof := "aaaaaaaa-0000-4000-8000-000000000001"
	// The forge: claim a ten-minute video was played start-to-finish one second apart. Positions and
	// durations come from the browser, so the only defence is wall-clock plausibility.
	rows := []reviewEventRow{
		{actorID: "actor", eventType: "video_play", occurredAt: base, positionMs: ptr(int64(0)), durationMs: ptr(int64(600000)), proofID: &proof},
		{actorID: "actor", eventType: "video_ended", occurredAt: base.Add(time.Second), positionMs: ptr(int64(600000)), durationMs: ptr(int64(600000)), proofID: &proof},
	}
	facts := computeActorFacts("item", "actor", rows)

	if facts.WatchedFull {
		t.Fatalf("a 600s video claimed watched in 1s must not read as fully watched (watched=%d ms)", facts.WatchedDistinctMs)
	}
	if facts.WatchedDistinctMs > 3000 {
		t.Fatalf("watched = %d ms, want <= ~2.5s: one second of real time at 2x plus slack", facts.WatchedDistinctMs)
	}
}

func TestWatchFactsAllowLegitimateDoubleSpeedReview(t *testing.T) {
	base := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	proof := "aaaaaaaa-0000-4000-8000-000000000001"
	// 60s of video reviewed at 2x in 30s of real time is honest work, and must not be penalised.
	rows := []reviewEventRow{
		{actorID: "actor", eventType: "video_play", occurredAt: base, positionMs: ptr(int64(0)), durationMs: ptr(int64(60000)), proofID: &proof},
		{actorID: "actor", eventType: "video_ended", occurredAt: base.Add(30 * time.Second), positionMs: ptr(int64(60000)), durationMs: ptr(int64(60000)), proofID: &proof},
	}
	facts := computeActorFacts("item", "actor", rows)

	if !facts.WatchedFull {
		t.Fatalf("2x review of the whole clip must count as fully watched (watched=%d of %d ms)", facts.WatchedDistinctMs, facts.ProofDurationMs)
	}
}
