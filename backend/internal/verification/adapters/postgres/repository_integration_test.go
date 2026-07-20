package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

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

func TestCloseSubmissionAcceptsVaccinationCompletions_RealPostgres(t *testing.T) {
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
	sopID := "00000000-0000-4000-8000-000000000204"
	sopVersionID := "00000000-0000-4000-8000-000000000205"
	taskID := "00000000-0000-4000-8000-000000000206"
	submissionID := "00000000-0000-4000-8000-000000000207"
	goatID := "00000000-0000-4000-8000-000000000208"
	batchID := "00000000-0000-4000-8000-000000000209"
	obligationID := "00000000-0000-4000-8000-000000000210"
	itemID := "00000000-0000-4000-8000-000000000211"
	completionID := "00000000-0000-4000-8000-000000000212"

	if _, err := pool.Exec(ctx, `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'person', 'Verifier', 'active');
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($2::uuid, $3::uuid, 'vaccination.close-submission', 'Vaccination close submission', 'vaccination', 'published');
INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy)
VALUES ($4::uuid, $3::uuid, $2::uuid, 'tenant', 1, 'published', now(), '{}'::jsonb, '{}'::jsonb);
INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy)
VALUES ($5::uuid, $3::uuid, $4::uuid, 'primary', 1, 'manual', '{}'::jsonb, '{}'::jsonb);
INSERT INTO sops (sop_id, tenant_id, code, name, category, status)
VALUES ($6::uuid, $3::uuid, 'vaccination.close-submission', 'Vaccination close submission', 'vaccination', 'published');
INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy)
VALUES ($7::uuid, $3::uuid, $6::uuid, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb);
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
VALUES ($8::uuid, $3::uuid, $6::uuid, $7::uuid, 'vaccination_drive', 'Vaccination drive', 'submitted', 'tenant', $3::uuid);
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex)
VALUES ($9::uuid, $3::uuid, 'alive', 'goat', $1::uuid, 'female');
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets, sop_task_id)
VALUES ($10::uuid, $3::uuid, $4::uuid, 'tenant', $3::uuid, 'in_progress', 1, $8::uuid);
INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, sop_task_id, idempotency_key)
VALUES ($11::uuid, $3::uuid, $4::uuid, $5::uuid, $10::uuid, 'goat', $9::uuid, 'tenant', $3::uuid, now(), 'in_progress', $8::uuid, 'verify-close-obligation');
INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, answers, state)
VALUES ($12::uuid, $3::uuid, $8::uuid, $7::uuid, $1::uuid, 'verify-close-submission', '{}'::jsonb, 'submitted');
INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state)
VALUES ($13::uuid, $3::uuid, $12::uuid, $8::uuid, $9::uuid, 'dose', 'needs_review');
INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, sop_submission_item_id, administered_at, status, idempotency_key, recorded_by)
VALUES ($14::uuid, $3::uuid, $11::uuid, $10::uuid, $9::uuid, $13::uuid, now(), 'recorded', 'verify-close-completion', $1::uuid);`,
		actorID, protocolID, tenantID, versionID, ruleID, sopID, sopVersionID, taskID,
		goatID, batchID, obligationID, submissionID, itemID, completionID); err != nil {
		t.Fatalf("seed vaccination close submission: %v", err)
	}

	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:       "vaccination",
			SubmissionID: &submissionID,
			RefType:      "vaccination_goat",
			RefID:        goatID,
		},
		MediaRefs: []string{"proof-close-submission"}, CapturedAt: time.Now().In(biztime.DefaultLocation()),
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
	if _, err := repo.CloseSubmission(ctx, domain.CloseSubmissionAction{
		TenantID: tenantID, SubmissionID: submissionID, ActorID: actorID,
	}); err != nil {
		t.Fatalf("CloseSubmission: %v", err)
	}

	var completionStatus, obligationStatus, batchStatus string
	if err := pool.QueryRow(ctx, `
SELECT vc.status, oi.status, ob.status
FROM vaccination_completions vc
JOIN obligation_instances oi ON oi.tenant_id = vc.tenant_id AND oi.obligation_id = vc.obligation_id
JOIN obligation_batches ob ON ob.tenant_id = vc.tenant_id AND ob.batch_id = vc.batch_id
WHERE vc.tenant_id = $1::uuid AND vc.completion_id = $2::uuid`,
		tenantID, completionID).Scan(&completionStatus, &obligationStatus, &batchStatus); err != nil {
		t.Fatalf("read accepted completion state: %v", err)
	}
	if completionStatus != "accepted" || obligationStatus != "completed" || batchStatus != "completed" {
		t.Fatalf("states completion/obligation/batch = %s/%s/%s, want accepted/completed/completed",
			completionStatus, obligationStatus, batchStatus)
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
