package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// B13: campaignNotAcceptedBuckets (close.go) caps its bucket list at
// domain.CloseNotAcceptedSampleLimit (100) for the audit/idempotency sample.
// CloseCampaign used to hand that SAME capped slice to enqueueCampaignClosed,
// so the weighing.campaign.closed outbox payload only ever carried the first
// 100 not-accepted buckets (ordered by display_name). The notification
// consumer (weighing_lifecycle_notify_consumer.go) iterates payload.Buckets
// and notifies each bucket's operator -- so any operator whose only bucket(s)
// sorted past the cap was never told the campaign closed.
//
// This test seeds 102 distinct operators, each with exactly one not-accepted
// bucket in the SAME campaign, named so the alphabetically-last bucket
// (and its operator) would have been dropped by the old capped-at-100 payload.
// It asserts every one of the 102 operators appears in the outbox event's
// operator summary list after the fix.
func TestCloseCampaignNotifiesEveryNotAcceptedOperatorBeyondSampleCap(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	// Migration 000065 binds a bucket's operator to the bucket's park through an
	// ACTIVE user_scope_grants row (weighing_campaign_sheds_operator_park_bound
	// trigger); the shared fixture's operator needs one before any
	// weighing_campaign_sheds write, same as TestReopenScopeAllowsLumpSumResubmit.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// The base fixture already has two not-accepted buckets (repoAnimalScope,
	// repoShedScope), both owned by repoOperator. Add enough MORE distinct
	// operators/buckets to exceed domain.CloseNotAcceptedSampleLimit.
	const extraOperators = domain.CloseNotAcceptedSampleLimit + 2
	wantOperators := map[string]bool{repoOperator: true}
	for i := 0; i < extraOperators; i++ {
		operatorID := fmt.Sprintf("00000000-0000-4000-9000-%012d", i+1)
		locationID := fmt.Sprintf("00000000-0000-4000-9100-%012d", i+1)
		campaignShedID := fmt.Sprintf("00000000-0000-4000-9200-%012d", i+1)
		// "Z-" prefix + zero-padded index sorts AFTER every other seeded bucket
		// display_name ("Gandhi 1 - Part 1", "Q1"), so the highest-index bucket
		// here is guaranteed to fall outside any LIMIT 100 ORDER BY display_name.
		label := fmt.Sprintf("Z-Operator-%03d", i+1)

		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
			repoTenant, operatorID, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, locationID, repoTenant, label, repoPark)
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', $5, 'per_shed_partition', $6::uuid, 1)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
			campaignShedID, repoCampaign, repoTenant, locationID, label, operatorID)

		wantOperators[operatorID] = true
	}
	if len(wantOperators) <= domain.CloseNotAcceptedSampleLimit {
		t.Fatalf("test setup error: only %d distinct operators, want > %d", len(wantOperators), domain.CloseNotAcceptedSampleLimit)
	}

	result, err := repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID:       repoTenant,
		CampaignID:     repoCampaign,
		Reason:         "monsoon",
		ClosedBy:       repoVerifier,
		IdempotencyKey: "close:campaign-fanout-1",
	})
	if err != nil {
		t.Fatalf("close campaign: %v", err)
	}
	if result.NotAcceptedCount != len(wantOperators)+1 {
		// +1 because repoOperator owns TWO not-accepted buckets in the base fixture
		// (repoAnimalScope AND repoShedScope), while every extra operator owns one.
		t.Fatalf("NotAcceptedCount=%d, want %d", result.NotAcceptedCount, len(wantOperators)+1)
	}

	gotOperators := notifiedOperatorsFromOutbox(t, ctx, pool)
	if len(gotOperators) != len(wantOperators) {
		t.Fatalf("distinct operators notified via outbox payload=%d, want %d (payload dropped operators past the sample cap)", len(gotOperators), len(wantOperators))
	}
	for operatorID := range wantOperators {
		if !gotOperators[operatorID] {
			t.Fatalf("operator %s owns a not-accepted bucket but is MISSING from the weighing.campaign.closed notification payload", operatorID)
		}
	}
}

func notifiedOperatorsFromOutbox(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]bool {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, `
SELECT payload->'payload'->'operators'
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='weighing.campaign.closed'`, repoTenant).Scan(&raw); err != nil {
		t.Fatalf("read campaign close outbox payload: %v", err)
	}
	var operators []struct {
		OperatorID  string   `json:"operator_id"`
		BucketCount int      `json:"bucket_count"`
		ShedLabels  []string `json:"shed_labels"`
	}
	if err := json.Unmarshal(raw, &operators); err != nil {
		t.Fatalf("decode outbox operators: %v", err)
	}
	got := make(map[string]bool, len(operators))
	for _, operator := range operators {
		if operator.BucketCount <= 0 {
			t.Fatalf("operator %s bucket_count=%d, want positive", operator.OperatorID, operator.BucketCount)
		}
		if len(operator.ShedLabels) > campaignClosedOperatorLabelSampleLimit {
			t.Fatalf("operator %s shed label sample has %d labels, want <= %d", operator.OperatorID, len(operator.ShedLabels), campaignClosedOperatorLabelSampleLimit)
		}
		got[operator.OperatorID] = true
	}
	return got
}
