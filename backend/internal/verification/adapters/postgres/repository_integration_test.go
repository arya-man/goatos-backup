package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

func newTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), $1, 'active') RETURNING tenant_id::text`,
		"verification-test-tenant",
	).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func TestListQueueKeepsSiblingPartitionsSeparate_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	var parkID, shedID string
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES (gen_random_uuid(), $1::uuid, 'park', 'verification-partition-park', 'North Park', 'active')
RETURNING location_id::text`, tenantID).Scan(&parkID); err != nil {
		t.Fatalf("insert park: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'shed', 'verification-partition-shed', 'Castro', 'active')
RETURNING location_id::text`, tenantID, parkID).Scan(&shedID); err != nil {
		t.Fatalf("insert shed: %v", err)
	}

	for _, partition := range []string{"1", "2"} {
		created, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:         domain.SourceRef{Module: "vaccination", RefType: "vaccination_goat", RefID: tenantID},
			MediaRefs:      []string{"proof-partition-" + partition},
			ParkID:         &parkID,
			ShedID:         &shedID,
			PartitionLabel: &partition,
			CapturedAt:     time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "verification-partition-" + partition,
		})
		if err != nil {
			t.Fatalf("CreateItem(partition=%s): %v", partition, err)
		}
		if !created.Created {
			t.Fatalf("CreateItem(partition=%s) did not create a row", partition)
		}
	}

	options, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{TenantID: tenantID})
	if err != nil {
		t.Fatalf("ListQueueFilterOptions: %v", err)
	}
	if len(options.Sheds) != 2 {
		t.Fatalf("shed options = %+v, want one option per partition", options.Sheds)
	}
	for index, partition := range []string{"1", "2"} {
		option := options.Sheds[index]
		if option.ID != shedID+"#"+partition {
			t.Fatalf("option[%d].ID = %q, want %q", index, option.ID, shedID+"#"+partition)
		}
		if option.PartitionLabel == nil || *option.PartitionLabel != partition {
			t.Fatalf("option[%d].PartitionLabel = %v, want %q", index, option.PartitionLabel, partition)
		}
		if option.Label != "Castro - "+partition || option.OperationalLocationDisplay != "Castro - "+partition {
			t.Fatalf("option[%d] display = %+v", index, option)
		}
	}

	partitionRows, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		ShedID:   shedID + "#1",
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("ListQueue(partition 1): %v", err)
	}
	if len(partitionRows) != 1 || partitionRows[0].PartitionLabel == nil || *partitionRows[0].PartitionLabel != "1" {
		t.Fatalf("partition rows = %+v, want only partition 1", partitionRows)
	}

	wholeShedRows, err := repo.ListQueue(ctx, ports.ListQueueParams{TenantID: tenantID, ShedID: shedID, Limit: 20})
	if err != nil {
		t.Fatalf("ListQueue(bare shed UUID): %v", err)
	}
	if len(wholeShedRows) != 2 {
		t.Fatalf("bare shed UUID returned %d rows, want both partitions", len(wholeShedRows))
	}

	partitionOptions, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		ShedID:   shedID + "#2",
	})
	if err != nil {
		t.Fatalf("ListQueueFilterOptions(partition 2): %v", err)
	}
	if partitionOptions.Counts.Pending != 1 {
		t.Fatalf("partition 2 pending count = %d, want 1", partitionOptions.Counts.Pending)
	}
}

func TestCreateItemIsIdempotentOnReplay_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "sop_submission",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1", "proof-2"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "vaccination:submission:sub-1",
	}

	first, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("first CreateItem: %v", err)
	}
	if !first.Created {
		t.Fatal("first CreateItem should mint a new row")
	}
	if first.Item.Status != domain.StatusPending {
		t.Fatalf("status = %s, want pending", first.Item.Status)
	}
	if len(first.Item.MediaRefs) != 2 {
		t.Fatalf("media_refs = %v, want 2 entries", first.Item.MediaRefs)
	}

	second, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("replay CreateItem: %v", err)
	}
	if second.Created {
		t.Fatal("replay with the same idempotency key must not mint a new row")
	}
	if second.Item.ItemID != first.Item.ItemID {
		t.Fatalf("replay returned a different item id: %s vs %s", second.Item.ItemID, first.Item.ItemID)
	}

	var itemCount, outboxCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM verification_items WHERE tenant_id = $1::uuid", tenantID).Scan(&itemCount); err != nil {
		t.Fatalf("count verification_items: %v", err)
	}
	if itemCount != 1 {
		t.Fatalf("verification_items rows = %d, want 1 (idempotent)", itemCount)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2", tenantID, EventItemPending).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox_messages: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox_messages rows for %s = %d, want 1 (idempotent, one event per item)", EventItemPending, outboxCount)
	}
}

func TestRecordVerdictLifecycle_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "vaccination:submission:sub-2",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	verifierID := tenantID // any UUID is acceptable for this fixture

	// A reject without a reason must fail at the storage layer too (defense in depth behind the
	// app-layer 422 gate) -- verification_items_reject_reason_check.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion,
	})
	if err == nil {
		t.Fatal("expected the reject-without-reason CHECK constraint to reject the write")
	}

	// Stale row_version must be reported as a conflict.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion + 99,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}

	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict approve: %v", err)
	}
	if approved.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", approved.Status)
	}
	if approved.VerifiedBy == nil || *approved.VerifiedBy != verifierID {
		t.Fatalf("verified_by = %v, want %s", approved.VerifiedBy, verifierID)
	}
	if approved.RowVersion != created.Item.RowVersion+1 {
		t.Fatalf("row_version = %d, want %d", approved.RowVersion, created.Item.RowVersion+1)
	}

	// A stale retry against the now-superseded row_version is a conflict, not a silent no-op --
	// proves optimistic concurrency actually advanced.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict on the now-stale row_version", err)
	}

	var verdictOutboxCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2", tenantID, EventVerdictApproved).Scan(&verdictOutboxCount); err != nil {
		t.Fatalf("count verdict outbox_messages: %v", err)
	}
	if verdictOutboxCount != 1 {
		t.Fatalf("outbox_messages rows for %s = %d, want 1", EventVerdictApproved, verdictOutboxCount)
	}

	// 404 for an item that does not exist.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: "00000000-0000-4000-8000-999999999999", Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: 1,
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestApprovedDriveClosesAtomicallyAndReplaysIdempotently_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	submissionID := "00000000-0000-4000-8000-000000000042"
	create := func(key, goatID string) domain.Item {
		result, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source: domain.SourceRef{
				Module:       "vaccination",
				SubmissionID: &submissionID,
				RefType:      "vaccination_goat",
				RefID:        goatID,
			},
			MediaRefs: []string{"proof-" + key}, CapturedAt: time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "vaccination:submission:" + submissionID + ":" + key,
		})
		if err != nil {
			t.Fatalf("CreateItem(%s): %v", key, err)
		}
		return result.Item
	}
	first := create("goat-1", "00000000-0000-4000-8000-000000000101")
	second := create("goat-2", "00000000-0000-4000-8000-000000000102")
	approve := func(item domain.Item) {
		if _, err := repo.RecordVerdict(ctx, domain.Verdict{
			TenantID: tenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
			VerifierID: tenantID, RowVersion: item.RowVersion,
		}); err != nil {
			t.Fatalf("approve %s: %v", item.ItemID, err)
		}
	}
	approve(first)

	ready, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", Status: domain.StatusApproved,
		ReadyForClosure: true, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListQueue partial approval: %v", err)
	}
	if len(ready) != 0 {
		t.Fatalf("partial drive appeared in leadership queue: %+v", ready)
	}
	if _, err := repo.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID: tenantID, SubmissionID: submissionID, ActorID: tenantID,
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("partial approval close err=%v, want ErrConflict", err)
	}

	approve(second)
	ready, err = repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", Status: domain.StatusApproved,
		ReadyForClosure: true, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListQueue complete approval: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("ready items=%d, want 2", len(ready))
	}

	closed, err := repo.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID: tenantID, SubmissionID: submissionID, ActorID: tenantID,
	})
	if err != nil {
		t.Fatalf("CloseSubmission: %v", err)
	}
	if len(closed) != 2 || closed[0].ClosedAt == nil || closed[1].ClosedAt == nil {
		t.Fatalf("closed=%+v", closed)
	}
	replayed, err := repo.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID: tenantID, SubmissionID: submissionID, ActorID: tenantID,
	})
	if err != nil || len(replayed) != 2 {
		t.Fatalf("replayed close=%+v err=%v", replayed, err)
	}
	var closedEvents int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND event_type = $2`, tenantID, EventItemClosed).Scan(&closedEvents); err != nil {
		t.Fatalf("count closed events: %v", err)
	}
	if closedEvents != 2 {
		t.Fatalf("closed outbox events=%d, want 2", closedEvents)
	}
}

func TestCloseVaccinationBatchMultipleDimensionsPaginationExecutionDateParkScopeStatusBuckets_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID
	protocolID := "00000000-0000-4000-8000-000000000201"
	versionID := "00000000-0000-4000-8000-000000000202"
	ruleID := "00000000-0000-4000-8000-000000000203"
	secondRuleID := "00000000-0000-4000-8000-000000000222"
	sopID := "00000000-0000-4000-8000-000000000204"
	sopVersionID := "00000000-0000-4000-8000-000000000205"
	taskID := "00000000-0000-4000-8000-000000000206"
	submissionID := "00000000-0000-4000-8000-000000000207"
	secondSubmissionID := "00000000-0000-4000-8000-000000000221"
	goatID := "00000000-0000-4000-8000-000000000208"
	batchID := "00000000-0000-4000-8000-000000000209"
	obligationID := "00000000-0000-4000-8000-000000000210"
	secondObligationID := "00000000-0000-4000-8000-000000000218"
	itemID := "00000000-0000-4000-8000-000000000211"
	secondItemID := "00000000-0000-4000-8000-000000000219"
	completionID := "00000000-0000-4000-8000-000000000212"
	rejectedCompletionID := "00000000-0000-4000-8000-000000000223"
	secondCompletionID := "00000000-0000-4000-8000-000000000220"
	parkID := "00000000-0000-4000-8000-000000000213"
	shedID := "00000000-0000-4000-8000-000000000214"
	otherShedID := "00000000-0000-4000-8000-000000000215"
	otherParkID := "00000000-0000-4000-8000-000000000216"
	administeredAt := time.Date(2026, time.July, 25, 5, 30, 0, 0, time.UTC)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed vaccination close submission: %v", err)
	}
	seedExec := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed vaccination close submission %s: %v", label, err)
		}
	}
	seedExec("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seedExec("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vaccination_close_submission', 'Vaccination close submission', 'vaccination', 'active')`, protocolID, tenantID)
	seedExec("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seedExec("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seedExec("second-protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'hs_primary', 2, 'manual_campaign', '{"vaccine":{"code":"HS"}}'::jsonb, '{}'::jsonb)`, secondRuleID, tenantID, versionID)
	seedExec("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vaccination_close_submission', 'Vaccination close submission', 'active')`, sopID, tenantID)
	seedExec("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seedExec("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'tenant', $2::uuid)`, taskID, tenantID, sopID, sopVersionID)
	seedExec("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female')`, goatID, tenantID, actorID)
	seedExec("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', $2::uuid, 'in_progress', 1, $4::uuid)`, batchID, tenantID, versionID, taskID)
	seedExec("obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'tenant', $2::uuid, now(), 'in_progress', $7::uuid, 'verify-close-obligation')`, obligationID, tenantID, versionID, ruleID, batchID, goatID, taskID)
	// A completed animal may have its obligation membership cleared by a later batch-membership
	// sync while vaccination_completions.batch_id still correctly names the executed logical drive.
	// Drive verification must count the completion, not only currently attached obligations.
	seedExec("detached-obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid, 'tenant', $2::uuid, now(), 'in_progress', $6::uuid, 'verify-close-detached-obligation')`, secondObligationID, tenantID, versionID, secondRuleID, goatID, taskID)
	seedExec("submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'verify-close-submission', '{}'::jsonb, 'submitted')`, submissionID, tenantID, taskID, sopVersionID, actorID)
	seedExec("accepted-submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state, accepted_at) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'verify-close-accepted-submission', '{}'::jsonb, 'accepted', now())`, secondSubmissionID, tenantID, taskID, sopVersionID, actorID)
	seedExec("submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'dose', 'needs_review')`, itemID, tenantID, submissionID, taskID, goatID)
	seedExec("second-submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'dose-2', 'accepted')`, secondItemID, tenantID, secondSubmissionID, taskID, goatID)
	seedExec("completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'recorded', 'verify-close-completion', $8::uuid)`, completionID, tenantID, obligationID, batchID, goatID, itemID, administeredAt, actorID)
	// Retain the rejected historical attempt beside the active successful retry. Closure membership
	// must count only recorded/accepted attempts or this valid retry can never become ready.
	seedExec("rejected-completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'rejected', 'verify-close-rejected-completion', $8::uuid)`, rejectedCompletionID, tenantID, obligationID, batchID, goatID, itemID, administeredAt.Add(-time.Hour), actorID)
	seedExec("second-completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), 'verify-close-second-completion', $8::uuid)`, secondCompletionID, tenantID, secondObligationID, batchID, goatID, secondItemID, administeredAt.Add(time.Hour), actorID)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed vaccination close submission: %v", err)
	}

	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:       "vaccination",
			SubmissionID: &submissionID,
			RefType:      "sop_submission",
			RefID:        submissionID,
		},
		MediaRefs: []string{"proof-close-submission"}, CapturedAt: time.Now().In(biztime.DefaultLocation()),
		ParkID: &parkID, ShedID: &shedID,
		IdempotencyKey: "vaccination:submission:" + submissionID + ":goat",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if _, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: actorID, RowVersion: created.Item.RowVersion,
	}); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: true,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(closures) != 1 || closures[0].BatchID != batchID || !closures[0].Ready || closures[0].TotalCount != 1 || closures[0].ApprovedCount != 1 || closures[0].VideoCount != 1 {
		t.Fatalf("closures=%+v, want ready batch %s with one distinct animal across two vaccine completions", closures, batchID)
	}
	// The four defects that made this drive un-closeable on 2026-08-08, pinned here because each
	// one shipped while the assertions above still passed.
	//
	// 1. A SUPERSEDED rejection must not count as outstanding work. This fixture keeps the rejected
	//    historical attempt beside the accepted retry; counting every historical verdict kept
	//    rejected_count above zero forever and the drive could never be closed.
	if closures[0].RejectedCount != 0 || closures[0].PendingCount != 0 {
		t.Fatalf("closures[0]=%+v, want rejected=0 pending=0 -- a superseded rejection is history, not outstanding work", closures[0])
	}
	// 2. The identity-bearing proof row must win the per-completion DISTINCT ON. Ranking by
	//    closed_at first picked rows with NULL item_id/shed_id, and every count derived from them
	//    collapsed to zero: the card read "0 sheds - 0/0 videos approved" on a drive with real
	//    videos in real sheds.
	if closures[0].ShedCount == 0 || closures[0].ApprovedVideos == 0 {
		t.Fatalf("closures[0]=%+v, want non-zero shed_count and approved_videos -- zero here means the DISTINCT ON picked a row with NULL item_id/shed_id", closures[0])
	}
	// 3. An approved completion flips to 'accepted'. A proofs CTE scoped to 'recorded' alone went
	//    empty exactly when the drive finished, so the drive became un-closeable BY BEING FULLY
	//    APPROVED. ApprovedVideos > 0 above only holds when 'accepted' is counted too.
	//
	// 4. A drive that is ready but NOT yet closed must report closed=false and be offered. Closed
	//    drives are returned with closed=true rather than filtered out, so the card can show a
	//    read-only Closed state instead of vanishing -- a card that disappears gives leadership no
	//    confirmation the close happened.
	if closures[0].Closed {
		t.Fatalf("closures[0]=%+v, want closed=false before anyone closes it", closures[0])
	}
	parkClosures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: true, ParkID: parkID,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures with park filter: %v", err)
	}
	if len(parkClosures) != 1 || parkClosures[0].BatchID != batchID {
		t.Fatalf("park-filtered closures=%+v, want ready batch %s", parkClosures, batchID)
	}
	otherParkClosures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: true, ParkID: otherParkID,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures with other park filter: %v", err)
	}
	if len(otherParkClosures) != 0 {
		t.Fatalf("other-park closures=%+v, want none", otherParkClosures)
	}
	shedClosures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: true, ShedID: shedID,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures with shed filter: %v", err)
	}
	if len(shedClosures) != 1 || shedClosures[0].BatchID != batchID {
		t.Fatalf("shed-filtered closures=%+v, want ready batch %s", shedClosures, batchID)
	}
	otherShedClosures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: true, ShedID: otherShedID,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures with other shed filter: %v", err)
	}
	if len(otherShedClosures) != 0 {
		t.Fatalf("other-shed closures=%+v, want none", otherShedClosures)
	}
	if _, err := repo.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID: tenantID, SubmissionID: submissionID, ActorID: actorID,
	}); err != nil {
		t.Fatalf("CloseSubmission before batch close: %v", err)
	}
	var submissionClosedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT completed_at FROM obligation_instances WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, tenantID, obligationID).Scan(&submissionClosedAt); err != nil {
		t.Fatalf("read CloseSubmission completed_at: %v", err)
	}
	if !submissionClosedAt.Equal(administeredAt) {
		t.Fatalf("CloseSubmission completed_at=%s, want operator administered_at %s", submissionClosedAt, administeredAt)
	}
	if _, err := repo.CloseVaccinationBatch(ctx, domain.CloseVaccinationBatchAction{
		TenantID: tenantID, BatchID: batchID, ActorID: actorID,
	}); err != nil {
		t.Fatalf("CloseVaccinationBatch: %v", err)
	}

	var completionStatus, obligationStatus, batchStatus, submissionItemState, submissionState, taskState string
	var obligationCompletedAt time.Time
	if err := pool.QueryRow(ctx, `
	SELECT vc.status, oi.status, ob.status, si.state, ss.state, st.state, oi.completed_at
	FROM vaccination_completions vc
	JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
	JOIN obligation_batches ob ON ob.tenant_id = vc.tenant_id AND ob.batch_id = vc.batch_id
JOIN sop_submission_items si ON si.tenant_id = vc.tenant_id AND si.item_id = vc.sop_submission_item_id
JOIN sop_submissions ss ON ss.tenant_id = si.tenant_id AND ss.submission_id = si.submission_id
JOIN sop_tasks st ON st.tenant_id = si.tenant_id AND st.task_id = si.task_id
WHERE vc.tenant_id = $1::uuid AND vc.completion_id = $2::uuid`,
		tenantID, completionID).Scan(
		&completionStatus,
		&obligationStatus,
		&batchStatus,
		&submissionItemState,
		&submissionState,
		&taskState,
		&obligationCompletedAt,
	); err != nil {
		t.Fatalf("read accepted completion state: %v", err)
	}
	if completionStatus != "accepted" || obligationStatus != "completed" || batchStatus != "completed" {
		t.Fatalf("states completion/obligation/batch = %s/%s/%s, want accepted/completed/completed",
			completionStatus, obligationStatus, batchStatus)
	}
	if submissionItemState != "accepted" || submissionState != "accepted" || taskState != "accepted" {
		t.Fatalf("states submission_item/submission/task = %s/%s/%s, want accepted/accepted/accepted",
			submissionItemState, submissionState, taskState)
	}
	if !obligationCompletedAt.Equal(administeredAt) {
		t.Fatalf("obligation completed_at = %s, want operator administered_at %s", obligationCompletedAt, administeredAt)
	}
	var secondCompletionStatus, secondObligationStatus string
	var secondObligationCompletedAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT vc.status, oi.status, oi.completed_at
FROM vaccination_completions vc
JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
WHERE vc.tenant_id = $1::uuid AND vc.completion_id = $2::uuid`, tenantID, secondCompletionID).Scan(
		&secondCompletionStatus,
		&secondObligationStatus,
		&secondObligationCompletedAt,
	); err != nil {
		t.Fatalf("read detached accepted completion state: %v", err)
	}
	if secondCompletionStatus != "accepted" || secondObligationStatus != "completed" {
		t.Fatalf("detached completion/obligation states = %s/%s, want accepted/completed", secondCompletionStatus, secondObligationStatus)
	}
	if want := administeredAt.Add(time.Hour); !secondObligationCompletedAt.Equal(want) {
		t.Fatalf("detached obligation completed_at = %s, want operator administered_at %s", secondObligationCompletedAt, want)
	}
	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND event_type='vaccination.completed'
  AND aggregate_id=$2::uuid`, tenantID, obligationID).Scan(&outboxCount); err != nil {
		t.Fatalf("count vaccination.completed outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("vaccination.completed outbox count = %d, want 1", outboxCount)
	}

	replayKey := "vaccination-batch-close-replay-repairs-open-item"
	if _, err := pool.Exec(ctx, `
UPDATE verification_items
SET closed_by = NULL,
    closed_at = NULL,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND item_id = $2::uuid;
UPDATE vaccination_completions
SET status = 'recorded',
    verified_by = NULL,
    verified_at = NULL,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND completion_id = $3::uuid;
UPDATE obligation_instances
SET status = 'in_progress',
    completed_at = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND obligation_id = $4::uuid;
UPDATE obligation_instances
SET status = 'canceled',
    completed_at = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND obligation_id = $9::uuid;
UPDATE obligation_batches
SET status = 'in_progress',
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND batch_id = $5::uuid;
UPDATE sop_submission_items
SET state = 'needs_review'
WHERE tenant_id = $1::uuid
  AND item_id = $6::uuid;
UPDATE sop_submissions
SET state = 'submitted',
    accepted_at = NULL
WHERE tenant_id = $1::uuid
  AND submission_id = $7::uuid;
UPDATE sop_tasks
SET state = 'submitted',
    verified_at = NULL,
    verified_by = NULL
WHERE tenant_id = $1::uuid
  AND task_id = $8::uuid`,
		pgx.QueryExecModeSimpleProtocol, tenantID, created.Item.ItemID, completionID, obligationID, batchID, itemID, submissionID, taskID, secondObligationID); err != nil {
		t.Fatalf("reset closed state for replay repair: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status, result_type, result_id, completed_at)
VALUES ($1, $2::uuid, 'verification.close-vaccination-batch', $3, 'completed', 'vaccination_batch', $4::uuid, now())`,
		scopedIdempotencyKey(tenantID, "verification.close-vaccination-batch", replayKey),
		tenantID,
		requestFingerprint(batchID, actorID),
		batchID); err != nil {
		t.Fatalf("seed completed close idempotency key: %v", err)
	}
	if _, err := repo.CloseVaccinationBatch(ctx, domain.CloseVaccinationBatchAction{
		TenantID: tenantID, BatchID: batchID, ActorID: actorID, IdempotencyKey: replayKey,
	}); err != nil {
		t.Fatalf("CloseVaccinationBatch replay with completed idempotency key should repair open item: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT vc.status, oi.status, ob.status, si.state, ss.state, st.state
FROM vaccination_completions vc
JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
JOIN obligation_batches ob ON ob.tenant_id = vc.tenant_id AND ob.batch_id = vc.batch_id
JOIN sop_submission_items si ON si.tenant_id = vc.tenant_id AND si.item_id = vc.sop_submission_item_id
JOIN sop_submissions ss ON ss.tenant_id = si.tenant_id AND ss.submission_id = si.submission_id
JOIN sop_tasks st ON st.tenant_id = si.tenant_id AND st.task_id = si.task_id
WHERE vc.tenant_id = $1::uuid AND vc.completion_id = $2::uuid`,
		tenantID, completionID).Scan(
		&completionStatus,
		&obligationStatus,
		&batchStatus,
		&submissionItemState,
		&submissionState,
		&taskState,
	); err != nil {
		t.Fatalf("read replay-repaired completion state: %v", err)
	}
	if completionStatus != "accepted" || obligationStatus != "completed" || batchStatus != "completed" ||
		submissionItemState != "accepted" || submissionState != "accepted" || taskState != "accepted" {
		t.Fatalf("replay-repaired states completion/obligation/batch/submission_item/submission/task = %s/%s/%s/%s/%s/%s, want accepted/completed/completed/accepted/accepted/accepted",
			completionStatus, obligationStatus, batchStatus, submissionItemState, submissionState, taskState)
	}
	if err := pool.QueryRow(ctx, `
SELECT status
FROM obligation_instances
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, tenantID, secondObligationID).Scan(&secondObligationStatus); err != nil {
		t.Fatalf("read terminal detached obligation after replay repair: %v", err)
	}
	if secondObligationStatus != "canceled" {
		t.Fatalf("terminal detached obligation status=%s, want canceled; verification replay must not mutate terminal history", secondObligationStatus)
	}
}

func TestListQueueKeysetIsBoundedAndOrdered_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	const total = 25
	base := time.Now().In(biztime.DefaultLocation()).Add(-time.Hour)
	for i := 0; i < total; i++ {
		_, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
			MediaRefs:      []string{"proof-1"},
			CapturedAt:     base.Add(time.Duration(i) * time.Minute),
			IdempotencyKey: "vaccination:submission:keyset-" + strconv.Itoa(i),
		})
		if err != nil {
			t.Fatalf("seed CreateItem[%d]: %v", i, err)
		}
	}

	pageSize := 10
	seen := map[string]bool{}
	var cursor *domain.Cursor
	pages := 0
	for {
		pages++
		if pages > total { // guard against a non-terminating loop
			t.Fatal("ListQueue did not terminate within the expected number of pages")
		}
		items, err := repo.ListQueue(ctx, ports.ListQueueParams{
			TenantID: tenantID, Category: "vaccination_proof", Status: domain.StatusPending,
			Cursor: cursor, Limit: pageSize + 1, // app layer requests Limit+1 to derive next_cursor
		})
		if err != nil {
			t.Fatalf("ListQueue: %v", err)
		}
		page := items
		hasNext := len(page) > pageSize
		if hasNext {
			page = page[:pageSize]
		}
		if len(page) > pageSize {
			t.Fatalf("page returned %d rows, want <= %d (bounded keyset)", len(page), pageSize)
		}
		for _, it := range page {
			if seen[it.ItemID] {
				t.Fatalf("item %s returned twice across pages -- keyset cursor did not advance", it.ItemID)
			}
			seen[it.ItemID] = true
		}
		if !hasNext {
			break
		}
		last := page[len(page)-1]
		cursor = &domain.Cursor{CapturedAt: last.CapturedAt, ItemID: last.ItemID}
	}
	if len(seen) != total {
		t.Fatalf("total items seen across pages = %d, want %d", len(seen), total)
	}
}

// TestListQueueFilterOptionsStatusCounts_OneToMany_PageBoundary_ParkScope_StatusBuckets_RealPostgres pins the fix for
// the banned "capped read-time rollup presented as truth" pattern (docs/decisions/
// scale-anti-patterns.md): the verifier mock's dot-legend pill counts must come from the
// database's own whole-filter aggregate (domain.QueueStatusCounts), never from grouping a fetched,
// keyset-limited page in app/frontend state. This seeds MORE than one page worth of pending items
// (25, against a 20-row page) plus a handful of approved/rejected rows, then asserts:
//  1. Counts.Pending EXCEEDS the page size (would be impossible if it were page-derived).
//  2. Counts.Pending matches a direct COUNT(*) against verification_items for the SAME scope.
//  3. pending + approved + rejected counts equal the total row count for the scope — the
//     disjointness claim in the projection-review marker, proven, not just asserted in a comment.
func TestListQueueFilterOptionsStatusCounts_OneToMany_PageBoundary_ParkScope_StatusBuckets_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	const pageSize = 20
	const pendingTotal = 25 // deliberately > pageSize, so a page-derived count would undercount.
	const approvedTotal = 4
	const rejectedTotal = 3

	base := time.Now().In(biztime.DefaultLocation()).Add(-time.Hour)
	var allIDs []string
	for i := 0; i < pendingTotal+approvedTotal+rejectedTotal; i++ {
		result, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
			MediaRefs:      []string{"proof-1"},
			CapturedAt:     base.Add(time.Duration(i) * time.Minute),
			IdempotencyKey: "vaccination:submission:status-counts-" + strconv.Itoa(i),
		})
		if err != nil {
			t.Fatalf("seed CreateItem[%d]: %v", i, err)
		}
		allIDs = append(allIDs, result.Item.ItemID)
	}
	// Flip a subset to approved/rejected directly (bypassing the verdict flow, which is exercised
	// elsewhere) so the count query is proven against a REAL status matrix, not all-pending.
	for i, id := range allIDs[pendingTotal : pendingTotal+approvedTotal] {
		_ = i
		if _, err := pool.Exec(ctx, `UPDATE verification_items SET status = 'approved' WHERE tenant_id = $1::uuid AND item_id = $2::uuid`, tenantID, id); err != nil {
			t.Fatalf("seed approved status: %v", err)
		}
	}
	for _, id := range allIDs[pendingTotal+approvedTotal:] {
		// verification_items_reject_reason_check requires a non-blank verdict_reason whenever
		// status = 'rejected'.
		if _, err := pool.Exec(ctx, `UPDATE verification_items SET status = 'rejected', verdict_reason = 'seeded for status-counts test' WHERE tenant_id = $1::uuid AND item_id = $2::uuid`, tenantID, id); err != nil {
			t.Fatalf("seed rejected status: %v", err)
		}
	}

	options, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", Status: domain.StatusPending,
		Limit: pageSize, // the PAGE read stays capped; the aggregate below must not be.
	})
	if err != nil {
		t.Fatalf("ListQueueFilterOptions: %v", err)
	}

	if options.Counts.Pending <= pageSize {
		t.Fatalf("Counts.Pending = %d, want > page size %d (a page-derived count could never exceed the page)", options.Counts.Pending, pageSize)
	}
	if options.Counts.Pending != pendingTotal {
		t.Fatalf("Counts.Pending = %d, want %d (whole-filter DB total)", options.Counts.Pending, pendingTotal)
	}
	if options.Counts.Approved != approvedTotal {
		t.Fatalf("Counts.Approved = %d, want %d", options.Counts.Approved, approvedTotal)
	}
	if options.Counts.Rejected != rejectedTotal {
		t.Fatalf("Counts.Rejected = %d, want %d", options.Counts.Rejected, rejectedTotal)
	}

	// Direct DB proof, independent of the repository code path under test.
	var dbPending, dbApproved, dbRejected int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM verification_items WHERE tenant_id = $1::uuid AND category = 'vaccination_proof' AND status = 'pending'`, tenantID).Scan(&dbPending); err != nil {
		t.Fatalf("direct pending count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM verification_items WHERE tenant_id = $1::uuid AND category = 'vaccination_proof' AND status = 'approved'`, tenantID).Scan(&dbApproved); err != nil {
		t.Fatalf("direct approved count: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM verification_items WHERE tenant_id = $1::uuid AND category = 'vaccination_proof' AND status = 'rejected'`, tenantID).Scan(&dbRejected); err != nil {
		t.Fatalf("direct rejected count: %v", err)
	}
	if dbPending != options.Counts.Pending || dbApproved != options.Counts.Approved || dbRejected != options.Counts.Rejected {
		t.Fatalf("repository counts (%d/%d/%d) do not match direct DB counts (%d/%d/%d)",
			options.Counts.Pending, options.Counts.Approved, options.Counts.Rejected, dbPending, dbApproved, dbRejected)
	}

	// Disjointness proof: the three buckets sum to the whole scoped row total — no double count,
	// no gap.
	var totalInScope int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM verification_items WHERE tenant_id = $1::uuid AND category = 'vaccination_proof'`, tenantID).Scan(&totalInScope); err != nil {
		t.Fatalf("direct total count: %v", err)
	}
	sum := options.Counts.Pending + options.Counts.Approved + options.Counts.Rejected
	if sum != totalInScope {
		t.Fatalf("pending+approved+rejected = %d, want %d (whole scoped total) — buckets are not disjoint/complete", sum, totalInScope)
	}
}

func TestListQueueFetchesDisplayLabels_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Seed workforce member for operator
	operatorUserID := "70000000-0000-4000-8000-000000000101"
	var workforceMemberID string
	err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_name, display_code, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, 'Ravi Operator', 'OP-001', 'active', 'operator')
RETURNING workforce_member_id::text`, tenantID, operatorUserID).Scan(&workforceMemberID)
	if err != nil {
		t.Fatalf("insert workforce_member: %v", err)
	}

	// Seed locations for shed and park
	var shedID, parkID string
	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, status)
VALUES ($1::uuid, 'shed', 'Shed A', 'active')
RETURNING location_id::text`, tenantID).Scan(&shedID)
	if err != nil {
		t.Fatalf("insert shed location: %v", err)
	}

	err = pool.QueryRow(ctx, `
INSERT INTO locations (tenant_id, location_type, name, status)
VALUES ($1::uuid, 'park', 'Park 1', 'active')
RETURNING location_id::text`, tenantID).Scan(&parkID)
	if err != nil {
		t.Fatalf("insert park location: %v", err)
	}

	// Create a verification item with operator, shed, and park references
	createResult, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:  tenantID,
		Vertical:  "preventive_care",
		Module:    "vaccination",
		Category:  "vaccination_proof",
		Source:    domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs: []string{"proof-1"},
		// Submission producers carry the authenticated user id. ListQueue must resolve that
		// through workforce_members.user_id, not expose it as a raw UUID in the app.
		OperatorID:     &operatorUserID,
		ShedID:         &shedID,
		ParkID:         &parkID,
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "vaccination:submission:display-labels",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	_ = createResult.Created

	// List the queue and verify labels are populated
	items, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "vaccination_proof",
		Status:   domain.StatusPending,
		Cursor:   nil,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListQueue returned %d items, want 1", len(items))
	}

	item := items[0]
	if item.OperatorID == nil || *item.OperatorID != operatorUserID {
		t.Fatalf("operator_id mismatch: got %v, want %s", item.OperatorID, operatorUserID)
	}
	if item.OperatorName == nil || *item.OperatorName != "Ravi Operator" {
		t.Fatalf("operator_name = %v, want 'Ravi Operator'", item.OperatorName)
	}

	if item.ShedID == nil || *item.ShedID != shedID {
		t.Fatalf("shed_id mismatch: got %v, want %s", item.ShedID, shedID)
	}
	if item.ShedLabel == nil || *item.ShedLabel != "Shed A" {
		t.Fatalf("shed_label = %v, want 'Shed A'", item.ShedLabel)
	}

	if item.ParkID == nil || *item.ParkID != parkID {
		t.Fatalf("park_id mismatch: got %v, want %s", item.ParkID, parkID)
	}
	if item.ParkLabel == nil || *item.ParkLabel != "Park 1" {
		t.Fatalf("park_label = %v, want 'Park 1'", item.ParkLabel)
	}

	// Verify that labels are never raw UUIDs (regression test for STATUS-003)
	if item.OperatorName != nil && isUUID(*item.OperatorName) {
		t.Fatalf("operator_name should not be a raw UUID: %s", *item.OperatorName)
	}
	if item.ShedLabel != nil && isUUID(*item.ShedLabel) {
		t.Fatalf("shed_label should not be a raw UUID: %s", *item.ShedLabel)
	}
	if item.ParkLabel != nil && isUUID(*item.ParkLabel) {
		t.Fatalf("park_label should not be a raw UUID: %s", *item.ParkLabel)
	}
}

func TestListQueueFetchesVerifierDisplayName_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	verifierUserID := "90000000-0000-4000-8000-000000000105"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_name, display_code, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, 'Jyothi Verifier', 'VER-105', 'active', 'verifier')`, tenantID, verifierUserID); err != nil {
		t.Fatalf("insert verifier workforce member: %v", err)
	}

	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID:       tenantID,
		Vertical:       "feed",
		Module:         "feed_direction",
		Category:       "feed_distribution",
		Source:         domain.SourceRef{Module: "feed_direction", RefType: "feed_distribution_completion", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "feed:distribution:verifier-display-name",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE verification_items
SET status = 'approved', verified_by = $1::uuid, verified_at = now()
WHERE tenant_id = $2::uuid AND item_id = $3::uuid`, verifierUserID, tenantID, created.Item.ItemID); err != nil {
		t.Fatalf("approve verification item: %v", err)
	}

	items, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		Category: "feed_distribution",
		Status:   domain.StatusApproved,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListQueue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListQueue returned %d items, want 1", len(items))
	}
	item := items[0]
	if item.VerifiedBy == nil || *item.VerifiedBy != verifierUserID {
		t.Fatalf("verified_by = %v, want %s", item.VerifiedBy, verifierUserID)
	}
	if item.VerifiedByName == nil || *item.VerifiedByName != "Jyothi Verifier" {
		t.Fatalf("verified_by_name = %v, want Jyothi Verifier", item.VerifiedByName)
	}
}

// isUUID is a simple check to detect if a string looks like a UUID (regression test for STATUS-003).
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	// Check for hex characters in UUID position groups
	for i, ch := range s {
		if ch == '-' && (i == 8 || i == 13 || i == 18 || i == 23) {
			continue
		}
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return false
		}
	}
	return true
}

// TestReadyVaccinationBatchClosuresMultipleDimensionsOneToMany_RealPostgres asserts that a single
// completion with multiple verification rows (rejection then approval cycle) counts once, not duplicated.
// BUG FIX: The latest_proofs CTE uses DISTINCT ON (batch_id, completion_id, goat_id) to deduplicate
// rows, ensuring proof_count matches completion_count even when a proof has multiple verdicts.

// TestReadyVaccinationBatchClosuresStatusMatrixEveryStatus_RealPostgres asserts that drives with
// all latest verdicts approved (no rejected/pending) are closeable, and drives with any outstanding
// rejected/pending verdict are not closeable. BUG FIX: proofs CTE filter includes both 'recorded'
// and 'accepted' statuses so approval verdicts are counted.

// TestReadyVaccinationBatchClosuresScopeHierarchyParkScope_RealPostgres asserts that park and shed
// filters correctly scope the readiness check and prevent cross-park visibility.

// TestReadyVaccinationBatchClosuresDateShiftScheduledDate_RealPostgres asserts that drives spanning
// multiple business dates roll up correctly and use Asia/Kolkata timezone for closed_at.

// TestReadyVaccinationBatchClosuresPaginationPageBoundary_RealPostgres asserts that the LIMIT 20
// pagination does not truncate a batch's rows mid-result; summary counts span the full filtered set,
// not just the visible page.

// TestReadyClosureCountsLatestVerdictPerProofAndExcludesSupersededRejections_RealPostgres is the
// adversarial companion to the fixture above. That one carries exactly ONE proof row per
// completion and every completion ends in a single verdict, so it cannot see the four defects that
// made a finished drive un-closeable on 2026-08-08 -- reverting the status filter or the
// DISTINCT ON ranking leaves it green. This fixture reproduces them.
//
// Shape (one batch, one park, TWO sheds, THREE animals):
//
//	goat A / shed A -- completion C1, verdict history reject -> reject -> approve. THREE
//	                   verification_items against the SAME submission, so latest_proofs must
//	                   collapse them to the newest verdict.
//	goat B / shed B -- completion C2 'accepted' (the redo), PLUS an archived
//	                   vaccination_completion_rejections row for the superseded attempt, which
//	                   carries its own verification_item. Both the expected CTE and the archive
//	                   branch of proofs must drop the archived attempt.
//	goat C / shed A -- completion C3 'accepted' with one approved proof. Volume, and the second
//	                   shed-A member so shed_count is a real 2.
//
// Every completion is 'accepted', which is the state a fully approved drive actually reaches, so
// a proofs CTE scoped to 'recorded' alone sees none of them.
func TestReadyClosureCountsLatestVerdictPerProofAndExcludesSupersededRejections_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID

	const (
		protocolID   = "00000000-0000-4000-8000-000000000301"
		versionID    = "00000000-0000-4000-8000-000000000302"
		ruleID       = "00000000-0000-4000-8000-000000000303"
		sopID        = "00000000-0000-4000-8000-000000000304"
		sopVersionID = "00000000-0000-4000-8000-000000000305"
		taskID       = "00000000-0000-4000-8000-000000000306"
		batchID      = "00000000-0000-4000-8000-000000000307"
		parkID       = "00000000-0000-4000-8000-000000000308"
		shedAID      = "00000000-0000-4000-8000-000000000309"
		shedBID      = "00000000-0000-4000-8000-00000000030a"
		otherParkID  = "00000000-0000-4000-8000-00000000030b"

		goatAID = "00000000-0000-4000-8000-000000000311"
		goatBID = "00000000-0000-4000-8000-000000000312"
		goatCID = "00000000-0000-4000-8000-000000000313"

		obligationAID = "00000000-0000-4000-8000-000000000321"
		obligationBID = "00000000-0000-4000-8000-000000000322"
		obligationCID = "00000000-0000-4000-8000-000000000323"

		submissionAID      = "00000000-0000-4000-8000-000000000331"
		submissionBID      = "00000000-0000-4000-8000-000000000332"
		submissionBOldID   = "00000000-0000-4000-8000-000000000333"
		submissionCID      = "00000000-0000-4000-8000-000000000334"
		submissionItemAID  = "00000000-0000-4000-8000-000000000341"
		submissionItemBID  = "00000000-0000-4000-8000-000000000342"
		submissionItemBOld = "00000000-0000-4000-8000-000000000343"
		submissionItemCID  = "00000000-0000-4000-8000-000000000344"
		completionAID      = "00000000-0000-4000-8000-000000000351"
		completionBID      = "00000000-0000-4000-8000-000000000352"
		completionBOldID   = "00000000-0000-4000-8000-000000000353"
		completionCID      = "00000000-0000-4000-8000-000000000354"

		// goat A's verdict history. All three are UNCLOSED, so the fixed DISTINCT ON ranking ties
		// on closed_at and falls through to `item_id DESC` -- which is why the APPROVED item
		// deliberately carries the HIGHEST id. That tiebreak is load-bearing and arbitrary; see the
		// note at the end of this test.
		itemAReject1 = "00000000-0000-4000-8000-000000000361"
		itemAReject2 = "00000000-0000-4000-8000-000000000362"
		itemAApprove = "00000000-0000-4000-8000-000000000363"
		itemB        = "00000000-0000-4000-8000-000000000371"
		itemBOld     = "00000000-0000-4000-8000-000000000372"
		itemC        = "00000000-0000-4000-8000-000000000381"
	)
	plannedDate := time.Date(2026, time.August, 7, 0, 0, 0, 0, biztime.DefaultLocation())
	administeredAt := biztime.BusinessDayStart(plannedDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed %s: %v", label, err)
		}
	}
	seed("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seed("park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, parkID, tenantID)
	seed("other-park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active')`, otherParkID, tenantID)
	seed("shed-a", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-CASTRO', 'Castro', 'active', $3::uuid)`, shedAID, tenantID, parkID)
	seed("shed-b", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-GANDHI', 'Gandhi', 'active', $3::uuid)`, shedBID, tenantID, parkID)
	seed("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vaccination_latest_verdict', 'Vaccination latest verdict', 'vaccination', 'active')`, protocolID, tenantID)
	seed("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seed("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seed("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vaccination_latest_verdict', 'Vaccination latest verdict', 'active')`, sopID, tenantID)
	seed("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seed("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'park', $5::uuid)`, taskID, tenantID, sopID, sopVersionID, parkID)
	seed("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, planned_date) VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'in_progress', 3, $5::uuid, $6::date)`, batchID, tenantID, versionID, parkID, taskID, plannedDate)
	// Two sheds in one park: shed_count must read 2, and planned_shed_count drives the "2 sheds"
	// batch label.
	seed("assignment-a", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Castro', 2)`, tenantID, batchID, plannedDate, parkID, shedAID)
	seed("assignment-b", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Gandhi', 1)`, tenantID, batchID, plannedDate, parkID, shedBID)

	type animal struct {
		goatID, obligationID, key string
	}
	for _, a := range []animal{
		{goatAID, obligationAID, "latest-verdict-a"},
		{goatBID, obligationBID, "latest-verdict-b"},
		{goatCID, obligationCID, "latest-verdict-c"},
	} {
		seed("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, shed_id) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid)`, a.goatID, tenantID, actorID, shedAID)
		seed("obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, $8::timestamptz, 'in_progress', $9::uuid, $10)`,
			a.obligationID, tenantID, versionID, ruleID, batchID, a.goatID, shedAID, administeredAt, taskID, a.key)
	}

	type submission struct {
		id, itemID, goatID, key, itemKey string
	}
	for _, s := range []submission{
		{submissionAID, submissionItemAID, goatAID, "latest-verdict-sub-a", "dose-a"},
		{submissionBID, submissionItemBID, goatBID, "latest-verdict-sub-b", "dose-b"},
		{submissionBOldID, submissionItemBOld, goatBID, "latest-verdict-sub-b-old", "dose-b-old"},
		{submissionCID, submissionItemCID, goatCID, "latest-verdict-sub-c", "dose-c"},
	} {
		seed("submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, '{}'::jsonb, 'submitted')`, s.id, tenantID, taskID, sopVersionID, actorID, s.key)
		seed("submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'needs_review')`, s.itemID, tenantID, s.id, taskID, s.goatID, s.itemKey)
	}

	// Every live completion is 'accepted' -- the state a fully approved drive actually reaches.
	type completion struct {
		id, obligationID, goatID, itemID, key string
	}
	for _, c := range []completion{
		{completionAID, obligationAID, goatAID, submissionItemAID, "latest-verdict-completion-a"},
		{completionBID, obligationBID, goatBID, submissionItemBID, "latest-verdict-completion-b"},
		{completionCID, obligationCID, goatCID, submissionItemCID, "latest-verdict-completion-c"},
	} {
		seed("completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), $9, $8::uuid)`,
			c.id, tenantID, c.obligationID, batchID, c.goatID, c.itemID, administeredAt, actorID, c.key)
	}

	// goat B was sent back and REDONE. The superseded attempt lives in the rejection archive AND
	// still has its own verification_item, so it is visible to BOTH the expected CTE and the
	// archive branch of proofs. Counting it on either side makes completion_count and proof_count
	// disagree and the readiness gate can never hold.
	seed("archived-rejection", `INSERT INTO vaccination_completion_rejections (rejection_id, completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, original_status, rejection_reason, recorded_by, original_idempotency_key, original_row_version, original_created_at, original_updated_at, rejected_by) VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'rejected', 'clip too dark', $8::uuid, 'latest-verdict-completion-b-old', 1, now(), now(), $8::uuid)`,
		completionBOldID, tenantID, obligationBID, batchID, goatBID, submissionItemBOld, administeredAt.Add(-2*time.Hour), actorID)

	// verification_items. Rejected rows MUST leave closed_at NULL
	// (verification_items_closed_approved_check) and MUST carry a verdict_reason
	// (verification_items_reject_reason_check). Nothing here is closed yet, so the drive is ready
	// but not closed.
	type item struct {
		id, submissionID, status, reason, shedID string
	}
	for _, it := range []item{
		{itemAReject1, submissionAID, "rejected", "shed sign not visible", shedAID},
		{itemAReject2, submissionAID, "rejected", "animal not identifiable", shedAID},
		{itemAApprove, submissionAID, "approved", "", shedAID},
		{itemB, submissionBID, "approved", "", shedBID},
		{itemBOld, submissionBOldID, "rejected", "clip too dark", shedBID},
		{itemC, submissionCID, "approved", "", shedAID},
	} {
		var reason any
		if it.reason != "" {
			reason = it.reason
		}
		seed("verification-item", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, verdict_reason, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, $5, $6, $7::uuid, $8::uuid, $9::timestamptz, $10::uuid, now(), $11)`,
			it.id, tenantID, taskID, it.submissionID, it.status, reason, parkID, it.shedID, administeredAt, actorID, "latest-verdict-item:"+it.id)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(closures) != 1 || closures[0].BatchID != batchID {
		t.Fatalf("closures=%+v, want exactly one ready batch %s -- a drive whose every animal's LATEST verdict is approved MUST be closeable", closures, batchID)
	}
	got := closures[0]
	if !got.Ready {
		t.Fatalf("closure=%+v, want ready=true", got)
	}
	// FIX 2 -- latest verdict per proof. goat A carries reject -> reject -> approve against ONE
	// completion. Counting every historical verification_item keeps rejected_completion_count above
	// zero forever, so `rejected_completion_count = 0` never holds and the drive returns nothing.
	// FIX 4 -- superseded archive. goat B's archived attempt must be dropped from BOTH the expected
	// CTE and the archive branch of proofs. Counting it on one side only makes completion_count and
	// proof_count disagree (4 vs 3) and `proof_count = completion_count` never holds.
	// Both defects are ALREADY proven by len(closures) == 1 above; the counts below pin the rest.
	if got.TotalCount != 3 {
		t.Fatalf("closure=%+v, want total_count=3 (three distinct animals; the archived superseded attempt is history, not a fourth member)", got)
	}
	if got.ApprovedCount != 3 || got.RejectedCount != 0 || got.PendingCount != 0 {
		t.Fatalf("closure=%+v, want approved=3 rejected=0 pending=0 -- a superseded rejection is history, not outstanding work", got)
	}
	// FIX 1 -- 'accepted' as well as 'recorded' in the proofs status filter, and FIX 3 -- rank
	// identity-bearing proof rows ahead of the degenerate NULL item_id/shed_id row. Either defect
	// collapses these to zero and the card reads "0 sheds - 0/0 videos approved" on a drive with
	// real videos in real sheds. proof_count still equals completion_count via the degenerate
	// branch, so the gate passes and the drive is returned LOOKING empty -- which is why these are
	// asserted as exact values, not merely non-zero.
	if got.ShedCount != 2 {
		t.Fatalf("closure=%+v, want shed_count=2 -- zero here means the proofs CTE lost the 'accepted' completions or the DISTINCT ON picked a row with NULL shed_id", got)
	}
	if got.VideoCount != 3 || got.ApprovedVideos != 3 || got.RejectedVideos != 0 || got.PendingVideos != 0 {
		t.Fatalf("closure=%+v, want videos total=3 approved=3 rejected=0 pending=0 -- one surviving latest-verdict proof per completion, all approved", got)
	}
	// Ready but NOT yet closed: nothing stamped closed_at, so the card must offer the action.
	if got.Closed {
		t.Fatalf("closure=%+v, want closed=false -- no verification item is closed yet", got)
	}
	if got.ParkID != parkID {
		t.Fatalf("closure=%+v, want park_id=%s", got, parkID)
	}

	// Park scope still holds with the multi-row history in place.
	parkScoped, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", ParkID: parkID,
	})
	if err != nil {
		t.Fatalf("park-scoped ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(parkScoped) != 1 || parkScoped[0].ShedCount != 2 {
		t.Fatalf("park-scoped closures=%+v, want the same batch with shed_count=2", parkScoped)
	}
	otherPark, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", ParkID: otherParkID,
	})
	if err != nil {
		t.Fatalf("other-park ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(otherPark) != 0 {
		t.Fatalf("other-park closures=%+v, want none", otherPark)
	}

	// A shed inside the drive still resolves the WHOLE drive's rollup -- readiness is a property of
	// the batch, not of the selected shed, so a director filtered to one shed still sees the real
	// 2-shed / 3-video totals rather than a shed-sized slice of them.
	shedScoped, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", ShedID: shedBID,
	})
	if err != nil {
		t.Fatalf("shed-scoped ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(shedScoped) != 1 || shedScoped[0].ShedCount != 2 || shedScoped[0].VideoCount != 3 {
		t.Fatalf("shed-B-scoped closures=%+v, want the whole batch with shed_count=2 video_count=3", shedScoped)
	}
	// A shed that is NOT in this drive must resolve nothing.
	foreignShed, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", ShedID: "00000000-0000-4000-8000-0000000003ff",
	})
	if err != nil {
		t.Fatalf("foreign-shed ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(foreignShed) != 0 {
		t.Fatalf("foreign-shed closures=%+v, want none", foreignShed)
	}

	// CROSS-CATEGORY MASKING. goat C's vaccination proof is approved, but add a REJECTED
	// vaccination proof for it plus an APPROVED item from ANOTHER category on the same submission,
	// giving the foreign row the highest item_id so it wins the DISTINCT ON. If the close path is
	// category-blind, the foreign approval is picked as goat C's "latest verdict", the real rejected
	// vaccination proof is skipped as superseded, and the batch closes with rework outstanding --
	// fail-OPEN. Found in review of 99fd332b6. Seeded, asserted, then removed so the rest of this
	// test keeps its original shape.
	const (
		itemCVaccRejected = "00000000-0000-4000-8000-0000000003a1"
		itemCForeign      = "00000000-0000-4000-8000-0000000003af"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, verdict_reason, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key)
VALUES
 ($1::uuid, $3::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $4::uuid, $5::uuid, 'sop_submission', $5::uuid, '["proof"]'::jsonb, 'rejected', 'dose not visible', $6::uuid, $7::uuid, $8::timestamptz, $9::uuid, now(), 'cross-category-vacc-rejected'),
 ($2::uuid, $3::uuid, 'growth', 'weighing', 'weighing_proof', 'weighing', $4::uuid, $5::uuid, 'sop_submission', $5::uuid, '["proof"]'::jsonb, 'approved', NULL, $6::uuid, $7::uuid, $8::timestamptz, $9::uuid, now(), 'cross-category-foreign-approved')`,
		itemCVaccRejected, itemCForeign, tenantID, taskID, submissionCID, parkID, shedAID, administeredAt, actorID); err != nil {
		t.Fatalf("seed cross-category items: %v", err)
	}
	if _, err := repo.CloseVaccinationBatch(ctx, domain.CloseVaccinationBatchAction{
		TenantID: tenantID, BatchID: batchID, ActorID: actorID,
	}); err == nil {
		t.Fatal("CloseVaccinationBatch SUCCEEDED with a rejected vaccination proof outstanding -- an approved item from another category masked it as a superseded verdict")
	} else {
		var notVerified *ports.ErrBatchNotFullyVerified
		if !errors.As(err, &notVerified) {
			t.Fatalf("close error = %v, want ErrBatchNotFullyVerified naming the blocked animal", err)
		}
	}
	// Drop ONLY the rejected vaccination proof. The foreign-category APPROVED item deliberately
	// SURVIVES into the successful close below: the close must neither return it nor publish a
	// close event for it. Deleting it here is what let that hole through review of 628eee913.
	if _, err := pool.Exec(ctx, `DELETE FROM verification_items WHERE tenant_id = $1::uuid AND item_id = $2::uuid`,
		tenantID, itemCVaccRejected); err != nil {
		t.Fatalf("remove cross-category rejected item: %v", err)
	}

	// THE READ MODEL AND THE WRITE PATH MUST AGREE. Found in review of 59ba8bac7 and reproduced
	// here before it was fixed: latest_proofs dedupes to the newest verdict, so this drive is
	// OFFERED, but CloseVaccinationBatch loaded every historical verification_item and blocked on
	// any non-approved one -- goat A's two superseded rejections. The observed failure was
	//   "ready list offered batch ... but CloseVaccinationBatch REFUSED it:
	//    verification: batch has unverified or rejected animals"
	// which is WORSE than the original defect: the button appears, the director taps it, and
	// nothing happens, so a broken drive is indistinguishable from a broken app. Offering an
	// action the write path will refuse is the bug -- assert the two halves agree.
	closedItems, err := repo.CloseVaccinationBatch(ctx, domain.CloseVaccinationBatchAction{
		TenantID: tenantID, BatchID: batchID, ActorID: actorID,
	})
	if err != nil {
		t.Fatalf("the ready list offered batch %s but CloseVaccinationBatch refused it: %v -- the read model and the close path disagree about superseded verdict history", batchID, err)
	}
	// A vaccination close must not speak for another module's evidence. The foreign weighing_proof
	// item is still present and was NOT stamped by the category-scoped update, so returning it or
	// publishing EventItemClosed for it announces a close that never happened -- to consumers that
	// act on it.
	for _, it := range closedItems {
		if it.ItemID == itemCForeign {
			t.Fatalf("CloseVaccinationBatch returned foreign-category item %s -- a vaccination close must return only vaccination proofs", itemCForeign)
		}
	}
	var foreignCloseEvents int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = $2 AND aggregate_id = $3::uuid`,
		tenantID, EventItemClosed, itemCForeign).Scan(&foreignCloseEvents); err != nil {
		t.Fatalf("count foreign close events: %v", err)
	}
	if foreignCloseEvents != 0 {
		t.Fatalf("%s events for foreign-category item = %d, want 0 -- it was never stamped closed", EventItemClosed, foreignCloseEvents)
	}
	var foreignClosedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT closed_at FROM verification_items WHERE tenant_id=$1::uuid AND item_id=$2::uuid`,
		tenantID, itemCForeign).Scan(&foreignClosedAt); err != nil {
		t.Fatalf("read foreign item closed_at: %v", err)
	}
	if foreignClosedAt != nil {
		t.Fatalf("foreign-category item closed_at = %v, want NULL -- a vaccination close must not stamp another module's proof", foreignClosedAt)
	}

	// The superseded rejections must still NOT be stamped closed: the CHECK constraint
	// verification_items_closed_approved_check forbids a closed non-approved row, and their history
	// value is that they stay readable as rejections.
	var closedRejected int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM verification_items
WHERE tenant_id = $1::uuid AND status = 'rejected' AND closed_at IS NOT NULL`, tenantID).Scan(&closedRejected); err != nil {
		t.Fatalf("count closed rejected items: %v", err)
	}
	if closedRejected != 0 {
		t.Fatalf("closed rejected verification_items = %d, want 0 -- a superseded rejection is skipped for blocking, never stamped closed", closedRejected)
	}

	// FRAGILITY, recorded deliberately rather than silently relied upon: all three of goat A's
	// items are unclosed, so the DISTINCT ON ranking ties on `closed_at` and the winner is decided
	// by `item_id DESC`. This fixture gives the APPROVED item the highest id. In production those
	// ids are random uuids, so which verdict wins among several UNCLOSED items for one completion
	// is a coin flip. The query has no monotonic verdict clock (verified_at is not in the ORDER BY)
	// to break that tie. Asserted here so the dependency is visible; reported as a live finding.
	var rankedStatus string
	if err := pool.QueryRow(ctx, `
SELECT vi.status
FROM verification_items vi
WHERE vi.tenant_id = $1::uuid
  AND vi.source_submission_id = $2::uuid
ORDER BY (vi.item_id IS NOT NULL) DESC, (vi.shed_id IS NOT NULL) DESC,
         vi.closed_at DESC NULLS LAST, vi.item_id DESC
LIMIT 1`, tenantID, submissionAID).Scan(&rankedStatus); err != nil {
		t.Fatalf("read ranked verdict: %v", err)
	}
	if rankedStatus != "approved" {
		t.Fatalf("ranked verdict for goat A = %s, want approved -- the ORDER BY tiebreak among unclosed items is item_id DESC", rankedStatus)
	}
}

// TestReadyClosureOneToMany_RealPostgres exercises OneToMany cardinality: one completion with multiple verdicts.
func TestReadyClosureOneToMany_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID

	const (
		protocolID       = "10000000-0000-4000-8000-000000000001"
		versionID        = "10000000-0000-4000-8000-000000000002"
		ruleID           = "10000000-0000-4000-8000-000000000003"
		sopID            = "10000000-0000-4000-8000-000000000004"
		sopVersionID     = "10000000-0000-4000-8000-000000000005"
		taskID           = "10000000-0000-4000-8000-000000000006"
		batchID          = "10000000-0000-4000-8000-000000000007"
		parkID           = "10000000-0000-4000-8000-000000000008"
		shedID           = "10000000-0000-4000-8000-000000000009"
		goatID           = "10000000-0000-4000-8000-000000000011"
		obligationID     = "10000000-0000-4000-8000-000000000021"
		submissionID     = "10000000-0000-4000-8000-000000000031"
		submissionItemID = "10000000-0000-4000-8000-000000000041"
		completionID     = "10000000-0000-4000-8000-000000000051"
		itemReject1      = "10000000-0000-4000-8000-000000000061"
		itemReject2      = "10000000-0000-4000-8000-000000000062"
		itemApprove      = "10000000-0000-4000-8000-000000000063"
	)

	plannedDate := time.Date(2026, time.August, 8, 0, 0, 0, 0, biztime.DefaultLocation())
	administeredAt := biztime.BusinessDayStart(plannedDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed %s: %v", label, err)
		}
	}

	seed("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seed("park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, parkID, tenantID)
	seed("shed", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-CASTRO', 'Castro', 'active', $3::uuid)`, shedID, tenantID, parkID)
	seed("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vacc_onetomany', 'Vacc OneToMany', 'vaccination', 'active')`, protocolID, tenantID)
	seed("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seed("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seed("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vacc_onetomany', 'Vacc OneToMany', 'active')`, sopID, tenantID)
	seed("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seed("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'park', $5::uuid)`, taskID, tenantID, sopID, sopVersionID, parkID)
	seed("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, planned_date) VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'in_progress', 1, $5::uuid, $6::date)`, batchID, tenantID, versionID, parkID, taskID, plannedDate)
	seed("assignment", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Castro', 1)`, tenantID, batchID, plannedDate, parkID, shedID)

	seed("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, shed_id) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid)`, goatID, tenantID, actorID, shedID)
	seed("obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, $8::timestamptz, 'in_progress', $9::uuid, $10)`, obligationID, tenantID, versionID, ruleID, batchID, goatID, shedID, administeredAt, taskID, "onetomany-obligation")
	seed("submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, '{}'::jsonb, 'submitted')`, submissionID, tenantID, taskID, sopVersionID, actorID, "onetomany-sub")
	seed("submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'needs_review')`, submissionItemID, tenantID, submissionID, taskID, goatID, "onetomany-item")
	seed("completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), $9, $8::uuid)`, completionID, tenantID, obligationID, batchID, goatID, submissionItemID, administeredAt, actorID, "onetomany-completion")

	now := time.Now()
	seed("item-reject1", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, verdict_reason, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'rejected', 'poor lighting', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, $9::timestamptz, $10)`, itemReject1, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, now.Add(-2*time.Second), "onetomany-reject1")
	seed("item-reject2", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, verdict_reason, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'rejected', 'animal not visible', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, $9::timestamptz, $10)`, itemReject2, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, now.Add(-1*time.Second), "onetomany-reject2")
	seed("item-approve", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'approved', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, $9::timestamptz, $10)`, itemApprove, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, now, "onetomany-approve")

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("OneToMany: expected 1 batch, got %d", len(closures))
	}
	if closures[0].ApprovedCount != 1 {
		t.Fatalf("OneToMany: approved_count should be 1 (picked latest approved verdict from 3), got %d", closures[0].ApprovedCount)
	}
}

// TestReadyClosureDateShift_RealPostgres exercises DateShift: verifies verified_at timestamp ordering in latest_proofs CTE.
func TestReadyClosureDateShift_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID

	const (
		protocolID       = "20000000-0000-4000-8000-000000000001"
		versionID        = "20000000-0000-4000-8000-000000000002"
		ruleID           = "20000000-0000-4000-8000-000000000003"
		sopID            = "20000000-0000-4000-8000-000000000004"
		sopVersionID     = "20000000-0000-4000-8000-000000000005"
		taskID           = "20000000-0000-4000-8000-000000000006"
		batchID          = "20000000-0000-4000-8000-000000000007"
		parkID           = "20000000-0000-4000-8000-000000000008"
		shedID           = "20000000-0000-4000-8000-000000000009"
		goatID           = "20000000-0000-4000-8000-000000000011"
		obligationID     = "20000000-0000-4000-8000-000000000021"
		submissionID     = "20000000-0000-4000-8000-000000000031"
		submissionItemID = "20000000-0000-4000-8000-000000000041"
		completionID     = "20000000-0000-4000-8000-000000000051"
		itemReject       = "20000000-0000-4000-8000-000000000062"
		itemApprove      = "20000000-0000-4000-8000-000000000061"
	)

	plannedDate := time.Date(2026, time.August, 8, 0, 0, 0, 0, biztime.DefaultLocation())
	administeredAt := biztime.BusinessDayStart(plannedDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed %s: %v", label, err)
		}
	}

	seed("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seed("park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, parkID, tenantID)
	seed("shed", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-CASTRO', 'Castro', 'active', $3::uuid)`, shedID, tenantID, parkID)
	seed("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vacc_dateshift', 'Vacc DateShift', 'vaccination', 'active')`, protocolID, tenantID)
	seed("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seed("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seed("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vacc_dateshift', 'Vacc DateShift', 'active')`, sopID, tenantID)
	seed("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seed("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'park', $5::uuid)`, taskID, tenantID, sopID, sopVersionID, parkID)
	seed("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, planned_date) VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'in_progress', 1, $5::uuid, $6::date)`, batchID, tenantID, versionID, parkID, taskID, plannedDate)
	seed("assignment", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Castro', 1)`, tenantID, batchID, plannedDate, parkID, shedID)

	seed("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, shed_id) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid)`, goatID, tenantID, actorID, shedID)
	seed("obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, $8::timestamptz, 'in_progress', $9::uuid, $10)`, obligationID, tenantID, versionID, ruleID, batchID, goatID, shedID, administeredAt, taskID, "dateshift-obligation")
	seed("submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, '{}'::jsonb, 'submitted')`, submissionID, tenantID, taskID, sopVersionID, actorID, "dateshift-sub")
	seed("submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'needs_review')`, submissionItemID, tenantID, submissionID, taskID, goatID, "dateshift-item")
	seed("completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), $9, $8::uuid)`, completionID, tenantID, obligationID, batchID, goatID, submissionItemID, administeredAt, actorID, "dateshift-completion")

	now := time.Now()
	oldTime := now.Add(-10 * time.Second)
	seed("item-reject", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, verdict_reason, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'rejected', 'poor quality', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, $9::timestamptz, $10)`, itemReject, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, oldTime, "dateshift-reject")
	seed("item-approve", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'approved', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, $9::timestamptz, $10)`, itemApprove, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, now, "dateshift-approve")

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("DateShift: expected 1 batch, got %d", len(closures))
	}
	// Should have latest (newest verified_at)
	if closures[0].ApprovedCount != 1 {
		t.Fatalf("DateShift: approved_count should be 1, got %d", closures[0].ApprovedCount)
	}
}

// TestReadyClosureParkScope_RealPostgres exercises ParkScope filtering.
func TestReadyClosureParkScope_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID

	const (
		protocolID       = "30000000-0000-4000-8000-000000000001"
		versionID        = "30000000-0000-4000-8000-000000000002"
		ruleID           = "30000000-0000-4000-8000-000000000003"
		sopID            = "30000000-0000-4000-8000-000000000004"
		sopVersionID     = "30000000-0000-4000-8000-000000000005"
		taskID           = "30000000-0000-4000-8000-000000000006"
		batchID          = "30000000-0000-4000-8000-000000000007"
		parkID           = "30000000-0000-4000-8000-000000000008"
		otherParkID      = "30000000-0000-4000-8000-000000000009"
		shedID           = "30000000-0000-4000-8000-000000000010"
		goatID           = "30000000-0000-4000-8000-000000000011"
		obligationID     = "30000000-0000-4000-8000-000000000021"
		submissionID     = "30000000-0000-4000-8000-000000000031"
		submissionItemID = "30000000-0000-4000-8000-000000000041"
		completionID     = "30000000-0000-4000-8000-000000000051"
		itemID           = "30000000-0000-4000-8000-000000000061"
	)

	plannedDate := time.Date(2026, time.August, 8, 0, 0, 0, 0, biztime.DefaultLocation())
	administeredAt := biztime.BusinessDayStart(plannedDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed %s: %v", label, err)
		}
	}

	seed("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seed("park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, parkID, tenantID)
	seed("other-park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active')`, otherParkID, tenantID)
	seed("shed", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-CASTRO', 'Castro', 'active', $3::uuid)`, shedID, tenantID, parkID)
	seed("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vacc_parkscope', 'Vacc ParkScope', 'vaccination', 'active')`, protocolID, tenantID)
	seed("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seed("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seed("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vacc_parkscope', 'Vacc ParkScope', 'active')`, sopID, tenantID)
	seed("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seed("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'park', $5::uuid)`, taskID, tenantID, sopID, sopVersionID, parkID)
	seed("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, planned_date) VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'in_progress', 1, $5::uuid, $6::date)`, batchID, tenantID, versionID, parkID, taskID, plannedDate)
	seed("assignment", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Castro', 1)`, tenantID, batchID, plannedDate, parkID, shedID)

	seed("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, shed_id) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid)`, goatID, tenantID, actorID, shedID)
	seed("obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, $8::timestamptz, 'in_progress', $9::uuid, $10)`, obligationID, tenantID, versionID, ruleID, batchID, goatID, shedID, administeredAt, taskID, "parkscope-obligation")
	seed("submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, '{}'::jsonb, 'submitted')`, submissionID, tenantID, taskID, sopVersionID, actorID, "parkscope-sub")
	seed("submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'needs_review')`, submissionItemID, tenantID, submissionID, taskID, goatID, "parkscope-item")
	seed("completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), $9, $8::uuid)`, completionID, tenantID, obligationID, batchID, goatID, submissionItemID, administeredAt, actorID, "parkscope-completion")
	seed("item", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'approved', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, now(), $9)`, itemID, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, "parkscope-item")

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	// Query all parks: should get 1 batch
	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("ParkScope (no filter): expected 1 batch, got %d", len(closures))
	}

	// Query specific park: should get 1 batch
	parkScoped, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", ParkID: parkID, OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures (park-scoped): %v", err)
	}
	if len(parkScoped) != 1 {
		t.Fatalf("ParkScope (with parkID): expected 1 batch, got %d", len(parkScoped))
	}

	// Query other park: should get 0 batches
	otherParkScoped, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", ParkID: otherParkID, OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures (other-park): %v", err)
	}
	if len(otherParkScoped) != 0 {
		t.Fatalf("ParkScope (other park): expected 0 batches, got %d", len(otherParkScoped))
	}
}

// TestReadyClosureStatusMatrix_RealPostgres exercises StatusMatrix across all verdict states.
func TestReadyClosureStatusMatrix_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID

	const (
		protocolID       = "40000000-0000-4000-8000-000000000001"
		versionID        = "40000000-0000-4000-8000-000000000002"
		ruleID           = "40000000-0000-4000-8000-000000000003"
		sopID            = "40000000-0000-4000-8000-000000000004"
		sopVersionID     = "40000000-0000-4000-8000-000000000005"
		taskID           = "40000000-0000-4000-8000-000000000006"
		batchID          = "40000000-0000-4000-8000-000000000007"
		parkID           = "40000000-0000-4000-8000-000000000008"
		shedID           = "40000000-0000-4000-8000-000000000009"
		goatID           = "40000000-0000-4000-8000-000000000011"
		obligationID     = "40000000-0000-4000-8000-000000000021"
		submissionID     = "40000000-0000-4000-8000-000000000031"
		submissionItemID = "40000000-0000-4000-8000-000000000041"
		completionID     = "40000000-0000-4000-8000-000000000051"
		itemApproved     = "40000000-0000-4000-8000-000000000061"
	)

	plannedDate := time.Date(2026, time.August, 8, 0, 0, 0, 0, biztime.DefaultLocation())
	administeredAt := biztime.BusinessDayStart(plannedDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed %s: %v", label, err)
		}
	}

	seed("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seed("park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, parkID, tenantID)
	seed("shed", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-CASTRO', 'Castro', 'active', $3::uuid)`, shedID, tenantID, parkID)
	seed("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vacc_statusmatrix', 'Vacc StatusMatrix', 'vaccination', 'active')`, protocolID, tenantID)
	seed("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seed("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seed("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vacc_statusmatrix', 'Vacc StatusMatrix', 'active')`, sopID, tenantID)
	seed("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seed("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'park', $5::uuid)`, taskID, tenantID, sopID, sopVersionID, parkID)
	seed("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, planned_date) VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'in_progress', 1, $5::uuid, $6::date)`, batchID, tenantID, versionID, parkID, taskID, plannedDate)
	seed("assignment", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Castro', 1)`, tenantID, batchID, plannedDate, parkID, shedID)

	seed("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, shed_id) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid)`, goatID, tenantID, actorID, shedID)
	seed("obligation", `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, $8::timestamptz, 'in_progress', $9::uuid, $10)`, obligationID, tenantID, versionID, ruleID, batchID, goatID, shedID, administeredAt, taskID, "statusmatrix-obligation")
	seed("submission", `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, '{}'::jsonb, 'submitted')`, submissionID, tenantID, taskID, sopVersionID, actorID, "statusmatrix-sub")
	seed("submission-item", `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'needs_review')`, submissionItemID, tenantID, submissionID, taskID, goatID, "statusmatrix-item")
	seed("completion", `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), $9, $8::uuid)`, completionID, tenantID, obligationID, batchID, goatID, submissionItemID, administeredAt, actorID, "statusmatrix-completion")
	seed("item-approved", `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'approved', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, now(), $9)`, itemApproved, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, "statusmatrix-approved")

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: false,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures: %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("StatusMatrix: expected 1 batch, got %d", len(closures))
	}
	if closures[0].ApprovedCount != 1 || closures[0].RejectedCount != 0 || closures[0].PendingCount != 0 {
		t.Fatalf("StatusMatrix: approved_count=%d rejected_count=%d pending_count=%d, want approved=1 rejected=0 pending=0",
			closures[0].ApprovedCount, closures[0].RejectedCount, closures[0].PendingCount)
	}
}

// TestReadyClosurePageBoundary_RealPostgres exercises PageBoundary pagination.
func TestReadyClosurePageBoundary_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	actorID := tenantID

	const (
		protocolID   = "50000000-0000-4000-8000-000000000001"
		versionID    = "50000000-0000-4000-8000-000000000002"
		ruleID       = "50000000-0000-4000-8000-000000000003"
		sopID        = "50000000-0000-4000-8000-000000000004"
		sopVersionID = "50000000-0000-4000-8000-000000000005"
		taskID       = "50000000-0000-4000-8000-000000000006"
		batchID      = "50000000-0000-4000-8000-000000000007"
		parkID       = "50000000-0000-4000-8000-000000000008"
		shedID       = "50000000-0000-4000-8000-000000000009"
	)

	plannedDate := time.Date(2026, time.August, 8, 0, 0, 0, 0, biztime.DefaultLocation())
	administeredAt := biztime.BusinessDayStart(plannedDate)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	seed := func(label, sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("seed %s: %v", label, err)
		}
	}

	seed("party", `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'person', 'Verifier', 'active')`, actorID)
	seed("park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active')`, parkID, tenantID)
	seed("shed", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id) VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-CASTRO', 'Castro', 'active', $3::uuid)`, shedID, tenantID, parkID)
	seed("protocol", `INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status) VALUES ($1::uuid, $2::uuid, 'vacc_pageboundary', 'Vacc PageBoundary', 'vaccination', 'active')`, protocolID, tenantID)
	seed("protocol-version", `INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', now(), '{}'::jsonb, '{}'::jsonb)`, versionID, tenantID, protocolID)
	seed("protocol-rule", `INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 'fmd_primary', 1, 'manual_campaign', '{"vaccine":{"code":"FMD"}}'::jsonb, '{}'::jsonb)`, ruleID, tenantID, versionID)
	seed("sop", `INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status) VALUES ($1::uuid, $2::uuid, 'vacc_pageboundary', 'Vacc PageBoundary', 'active')`, sopID, tenantID)
	seed("sop-version", `INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy) VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb)`, sopVersionID, tenantID, sopID)
	seed("task", `INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'park', $5::uuid)`, taskID, tenantID, sopID, sopVersionID, parkID)
	seed("batch", `INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id, planned_date) VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'in_progress', 2, $5::uuid, $6::date)`, batchID, tenantID, versionID, parkID, taskID, plannedDate)
	seed("assignment", `INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, animal_count) VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, 'Castro', 2)`, tenantID, batchID, plannedDate, parkID, shedID)

	// Create 2 goats with complete vaccination records
	for i := 0; i < 2; i++ {
		suffix := fmt.Sprintf("%012d", i)
		goatID := fmt.Sprintf("50000000-0000-4000-8000-%s", suffix)
		obligationID := fmt.Sprintf("50000000-0000-4000-8001-%s", suffix)
		submissionID := fmt.Sprintf("50000000-0000-4000-8002-%s", suffix)
		submissionItemID := fmt.Sprintf("50000000-0000-4000-8003-%s", suffix)
		completionID := fmt.Sprintf("50000000-0000-4000-8004-%s", suffix)
		itemID := fmt.Sprintf("50000000-0000-4000-8005-%s", suffix)

		seed(fmt.Sprintf("goat-%d", i), `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, shed_id) VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid)`, goatID, tenantID, actorID, shedID)
		seed(fmt.Sprintf("obligation-%d", i), `INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, $8::timestamptz, 'in_progress', $9::uuid, $10)`, obligationID, tenantID, versionID, ruleID, batchID, goatID, shedID, administeredAt, taskID, fmt.Sprintf("pageboundary-obligation-%d", i))
		seed(fmt.Sprintf("submission-%d", i), `INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, '{}'::jsonb, 'submitted')`, submissionID, tenantID, taskID, sopVersionID, actorID, fmt.Sprintf("pageboundary-sub-%d", i))
		seed(fmt.Sprintf("submission-item-%d", i), `INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, 'needs_review')`, submissionItemID, tenantID, submissionID, taskID, goatID, fmt.Sprintf("pageboundary-item-%d", i))
		seed(fmt.Sprintf("completion-%d", i), `INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, verified_by, verified_at, idempotency_key, recorded_by) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, $7::timestamptz, 'accepted', $8::uuid, now(), $9, $8::uuid)`, completionID, tenantID, obligationID, batchID, goatID, submissionItemID, administeredAt, actorID, fmt.Sprintf("pageboundary-completion-%d", i))
		seed(fmt.Sprintf("item-%d", i), `INSERT INTO verification_items (item_id, tenant_id, vertical, module, category, source_module, source_task_id, source_submission_id, source_ref_type, source_ref_id, media_refs, status, park_id, shed_id, captured_at, verified_by, verified_at, idempotency_key) VALUES ($1::uuid, $2::uuid, 'preventive_care', 'vaccination', 'vaccination_proof', 'vaccination', $3::uuid, $4::uuid, 'sop_submission', $4::uuid, '["proof"]'::jsonb, 'approved', $5::uuid, $6::uuid, $7::timestamptz, $8::uuid, now(), $9)`, itemID, tenantID, taskID, submissionID, parkID, shedID, administeredAt, actorID, fmt.Sprintf("pageboundary-item-%d", i))
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	closures, err := repo.ListReadyVaccinationBatchClosures(ctx, ports.ListQueueParams{
		TenantID: tenantID, Category: "vaccination_proof", OpenOnly: false, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListReadyVaccinationBatchClosures (limit=1): %v", err)
	}
	if len(closures) != 1 {
		t.Fatalf("PageBoundary (limit=1): expected 1 batch, got %d", len(closures))
	}
	if closures[0].TotalCount != 2 {
		t.Fatalf("PageBoundary (limit=1): batch should show total_count=2, got %d", closures[0].TotalCount)
	}
}

// A shed NAME is not unique across the farm. Castro, Gandhi, Godel 1, Godel 2, Mandela 1,
// Mandela 2 and Yashoda each exist in BOTH parks, so on 2026-08-12 nine of the sixty-seven shed
// options in the STG queue were exact duplicate labels sitting adjacent under this query's own
// ORDER BY -- two "Castro - 1" entries with nothing to tell them apart. The option VALUE was
// never wrong (the id is a shed UUID, never a name), so the filter worked; the reader simply
// could not see which shed she was choosing, and the park with more pens read as the only park
// present. The park now travels with the option so a client can group by it.
//
// The park is carried BESIDE the label, never folded into it: the label is the shed's operational
// location and oploc owns that string.
func TestShedFilterOptionsNameTheParkWhenTwoParksShareAShedName_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	// Two parks, each holding a shed with the SAME name -- the real farm's shape.
	parkIDs := map[string]string{}
	shedIDs := map[string]string{}
	for _, park := range []string{"Coimbatore", "Channapatna"} {
		var parkID, shedID string
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES (gen_random_uuid(), $1::uuid, 'park', $2, $3, 'active')
RETURNING location_id::text`, tenantID, "dup-park-"+park, park).Scan(&parkID); err != nil {
			t.Fatalf("insert park %s: %v", park, err)
		}
		if err := pool.QueryRow(ctx, `
INSERT INTO locations (location_id, tenant_id, parent_location_id, location_type, location_code, name, status)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'shed', $3, 'Castro', 'active')
RETURNING location_id::text`, tenantID, parkID, "dup-shed-"+park).Scan(&shedID); err != nil {
			t.Fatalf("insert shed in %s: %v", park, err)
		}
		parkIDs[park] = parkID
		shedIDs[park] = shedID

		partition := "1"
		parkID, shedID = parkIDs[park], shedIDs[park]
		if _, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "feed", Module: "feed_direction", Category: "feed_distribution",
			Source:         domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: tenantID},
			MediaRefs:      []string{"proof-dup-" + park},
			ParkID:         &parkID,
			ShedID:         &shedID,
			PartitionLabel: &partition,
			CapturedAt:     time.Now().In(biztime.DefaultLocation()),
			IdempotencyKey: "verification-dup-name-" + park,
		}); err != nil {
			t.Fatalf("CreateItem(%s): %v", park, err)
		}
	}

	options, err := repo.ListQueueFilterOptions(ctx, ports.ListQueueParams{TenantID: tenantID})
	if err != nil {
		t.Fatalf("ListQueueFilterOptions: %v", err)
	}
	if len(options.Sheds) != 2 {
		t.Fatalf("shed options = %+v, want one per park", options.Sheds)
	}

	// Ordered park-first, so a client groups by arrival without sorting again.
	if options.Sheds[0].ParkLabel != "Channapatna" || options.Sheds[1].ParkLabel != "Coimbatore" {
		t.Fatalf("shed options are not ordered park-first: %+v", options.Sheds)
	}
	for _, option := range options.Sheds {
		park := option.ParkLabel
		if park != "Channapatna" && park != "Coimbatore" {
			t.Fatalf("option %+v carries no usable park", option)
		}
		if option.ParkID != parkIDs[park] {
			t.Fatalf("option %+v park_id = %q, want %q", option, option.ParkID, parkIDs[park])
		}
		// The whole point: same label on both, told apart by park and by id.
		if option.Label != "Castro - 1" || option.OperationalLocationDisplay != "Castro - 1" {
			t.Fatalf("option display = %+v, want the oploc composition unchanged", option)
		}
		if option.ID != shedIDs[park]+"#1" {
			t.Fatalf("option %+v ID = %q, want the %s shed", option, option.ID, park)
		}
	}
	if options.Sheds[0].ID == options.Sheds[1].ID {
		t.Fatalf("both options resolve to the same shed: %+v", options.Sheds)
	}

	// And the twin the reader picks is the one she gets.
	rows, err := repo.ListQueue(ctx, ports.ListQueueParams{
		TenantID: tenantID,
		ShedID:   shedIDs["Channapatna"] + "#1",
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("ListQueue(Channapatna Castro - 1): %v", err)
	}
	if len(rows) != 1 || rows[0].ParkID == nil || *rows[0].ParkID != parkIDs["Channapatna"] {
		t.Fatalf("rows = %+v, want only the Channapatna shed's item", rows)
	}
}
