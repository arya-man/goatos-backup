package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
	"testing"
	"time"
)

type pre274CreateCampaign struct {
	TenantID          string
	ParkID            string
	PeriodStartDate   string
	PeriodEndDate     string
	StartBusinessDate string
	PlannedCapPerDay  int
	OperatorUserID    string
	// FastingOperatorUserID is the SECOND operator assigned at create: the
	// person who removes feed and water the evening before the weigh date
	// (maintainer decision 2026-09-03, see domain/fasting.go). Required on
	// create; park-scoped exactly like bucket operators. The evening shift and
	// the weighing shift are different people, which is why this is its own
	// assignment and never defaults to the weighing operator.
	FastingOperatorUserID string
	IdempotencyKey        string
	Sheds                 []domain.CreateCampaignShed
	CreatedBy             string
}
type pre274SubmitFastingShed struct {
	TenantID       string
	FastingTaskID  string
	CampaignShedID string
	FeedProofRef   string
	WaterProofRef  string
	IdempotencyKey string
	SubmittedBy    string
}

func TestReview274LegacyCampaignReplayFingerprint(t *testing.T) {
	legacy := pre274CreateCampaign{TenantID: repoTenant, ParkID: repoPark, OperatorUserID: repoOperator, FastingOperatorUserID: repoOperator, CreatedBy: repoOperator, IdempotencyKey: "network-retry", PeriodStartDate: "2026-09-20", PeriodEndDate: "2026-09-20", StartBusinessDate: "2026-09-20", PlannedCapPerDay: 100}
	raw, _ := json.Marshal(legacy)
	var current domain.CreateCampaign
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	oldHash := idempotencyFingerprint(legacy)
	newHash := domain.RequestFingerprint(current)
	if oldHash != newHash {
		t.Fatalf("identical pre-deploy request no longer replays: old=%s new=%s", oldHash, newHash)
	}
}
func TestReview274LegacyRemovalReplayFingerprint(t *testing.T) {
	legacy := pre274SubmitFastingShed{TenantID: repoTenant, FastingTaskID: repoPark, CampaignShedID: repoPark, FeedProofRef: repoOperator, WaterProofRef: repoTenant, IdempotencyKey: "network-retry", SubmittedBy: repoOperator}
	raw, _ := json.Marshal(legacy)
	var current domain.SubmitFastingShed
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	current.Proofs = domain.RemovalProofRefs{"feed_video": current.FeedProofRef, "water_video": current.WaterProofRef}
	current.SlotKinds = map[string]string{"feed_video": "video", "water_video": "video"}
	current.OrderedRefs = []string{current.FeedProofRef, current.WaterProofRef}
	current.Answers = domain.SOPAnswers{}
	oldHash := idempotencyFingerprint(legacy)
	newHash := idempotencyFingerprint(current)
	if oldHash != newHash {
		t.Fatalf("identical pre-deploy removal no longer replays: old=%s new=%s", oldHash, newHash)
	}
}

func TestLegacyFingerprintCompatibilityDoesNotHideChangedRequests(t *testing.T) {
	c := domain.CreateCampaign{IdempotencyKey: "same", PlannedCapPerDay: 100}
	original := domain.RequestFingerprint(c)
	off := false
	c.FeedWaterRemovalRequested = &off
	if domain.RequestFingerprint(c) == original {
		t.Fatal("explicit removal choice disappeared")
	}
	c.FeedWaterRemovalRequested = nil
	c.PlannedCapPerDay = 200
	if domain.RequestFingerprint(c) == original {
		t.Fatal("changed cap disappeared")
	}
	c.PlannedCapPerDay = 100
	c.RemoveFasting = true
	if domain.RequestFingerprint(c) != original {
		t.Fatal("derived removal state changed the client fingerprint")
	}
	cmd := domain.SubmitFastingShed{FeedProofRef: "feed", WaterProofRef: "water", Proofs: domain.RemovalProofRefs{"feed_video": "feed", "water_video": "water"}}
	base := idempotencyFingerprint(cmd)
	cmd.Answers = domain.SOPAnswers{"why": json.RawMessage(`"yes"`)}
	if idempotencyFingerprint(cmd) == base {
		t.Fatal("answers disappeared")
	}
	cmd.Answers = nil
	cmd.Proofs["extra"] = "photo"
	if idempotencyFingerprint(cmd) == base {
		t.Fatal("extra capture disappeared")
	}
	delete(cmd.Proofs, "extra")
	cmd.Proofs["feed_video"] = "changed"
	if idempotencyFingerprint(cmd) == base {
		t.Fatal("changed capture disappeared")
	}
}
func TestLegacyUpdateFingerprint(t *testing.T) {
	legacy := pre274CreateCampaign{TenantID: repoTenant, IdempotencyKey: "edit", PlannedCapPerDay: 100}
	raw, _ := json.Marshal(legacy)
	var current domain.UpdateCampaign
	if err := json.Unmarshal(raw, &current); err != nil {
		t.Fatal(err)
	}
	old := idempotencyFingerprint(struct {
		CampaignID string
		Command    pre274CreateCampaign
	}{"campaign", legacy})
	got := idempotencyFingerprint(struct {
		CampaignID string
		Command    domain.UpdateCampaign
	}{"campaign", current})
	if got != old {
		t.Fatal("legacy update fingerprint changed")
	}
}

func TestLegacyRemovalReplayAfterDeployment(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	taskID := seedFastingFixture(t, ctx, pool, "2026-09-04")
	repo := NewRepository(pool, 5*time.Second)
	cmd := fastingSubmitShedA(taskID, "legacy-deployment")
	first, err := repo.SubmitFastingShed(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	legacy := pre274SubmitFastingShed{cmd.TenantID, cmd.FastingTaskID, cmd.CampaignShedID, cmd.FeedProofRef, cmd.WaterProofRef, cmd.IdempotencyKey, cmd.SubmittedBy}
	// Migration boundary fixture: preserve a fingerprint written by the old binary.
	if _, err := pool.Exec(ctx, `UPDATE weighing_idempotency_records SET request_fingerprint=$1 WHERE tenant_id=$2::uuid AND event_type=$3 AND idempotency_key=$4`, idempotencyFingerprint(legacy), repoTenant, fastingSubmitAction, cmd.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	svc := weighingapp.NewService(repo).WithFastingStore(repo)
	actor := domain.Actor{TenantID: repoTenant, UserID: cmd.SubmittedBy, PermissionsResolved: true, Permissions: []string{permissions.WeighingExecute}}
	replay, err := svc.SubmitFastingShed(ctx, actor, cmd)
	if err != nil || replay.FastingShedID != first.Card.FastingShedID || replay.RowVersion != first.Card.RowVersion {
		t.Fatalf("replay changed evidence: %+v %v", replay, err)
	}
	cmd.FeedProofRef, cmd.WaterProofRef = cmd.WaterProofRef, cmd.FeedProofRef
	if _, err := svc.SubmitFastingShed(ctx, actor, cmd); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("changed request replayed: %v", err)
	}
}
func TestLegacyHydratedCampaignReplayBeforeCurrentRules(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	cmd := domain.CreateCampaign{TenantID: repoTenant, ParkID: repoPark, PeriodStartDate: "2026-11-04", PeriodEndDate: "2026-11-04", StartBusinessDate: "2026-11-04", PlannedCapPerDay: 100, OperatorUserID: repoOperator, CreatedBy: repoOperator, IdempotencyKey: "legacy-hydrated", FastingOperatorUserID: repoOperator, Sheds: []domain.CreateCampaignShed{{LocationID: lcpShedOne, LocationType: "shed", DisplayName: "CBE Godel 1 - Part 8", WeighingCategory: domain.CategoryPerShedPartition}}}
	first, err := repo.CreateCampaign(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	// Old creates persisted the partition-hydrated command, not the wire request.
	canonical := cmd
	canonical.Sheds = append([]domain.CreateCampaignShed(nil), cmd.Sheds...)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	canonical.Sheds, err = repo.hydrateCreateCampaignShedPartitions(ctx, tx, repoTenant, canonical.Sheds)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	legacy := pre274CreateCampaign{canonical.TenantID, canonical.ParkID, canonical.PeriodStartDate, canonical.PeriodEndDate, canonical.StartBusinessDate, canonical.PlannedCapPerDay, canonical.OperatorUserID, canonical.FastingOperatorUserID, canonical.IdempotencyKey, canonical.Sheds, canonical.CreatedBy}
	if _, err := pool.Exec(ctx, `UPDATE weighing_idempotency_records SET request_fingerprint=$1 WHERE tenant_id=$2::uuid AND event_type='weighing.campaign_created' AND idempotency_key=$3`, idempotencyFingerprint(legacy), repoTenant, cmd.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	cmd.Sheds = append([]domain.CreateCampaignShed(nil), cmd.Sheds...)
	cmd.Sheds[0].PartitionLabel = ""
	svc := weighingapp.NewService(repo).WithClock(func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) })
	actor := domain.Actor{TenantID: repoTenant, UserID: repoOperator, PermissionsResolved: true, Permissions: []string{permissions.WeighingPlan}}
	replay, err := svc.CreateCampaign(ctx, actor, cmd)
	if err != nil || replay.CampaignID != first.CampaignID {
		t.Fatalf("legacy create refused after date cutoff: %+v %v", replay, err)
	}
	cmd.PlannedCapPerDay++
	if _, ok, err := repo.CampaignByIdempotencyKey(ctx, cmd); err != nil || ok {
		t.Fatalf("changed request matched old fingerprint: %v %v", ok, err)
	}
}
