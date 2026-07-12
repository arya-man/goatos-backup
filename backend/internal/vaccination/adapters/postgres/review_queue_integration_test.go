package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestListRecordedCompletionsQueue checks the Verification queue read: only still-recorded
// completions, earliest administered first, accepted ones excluded.
func TestListRecordedCompletionsQueue(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.queue", Name: "Queue", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const g = "30000000-0000-4000-8000-0000000000a9"
	seedGenGoat(t, ctx, pool, g, "alive")

	doses := int32(1)
	mk := func(key string, administered time.Time) string {
		obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: g, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: administered, Status: "scheduled", IdempotencyKey: "q-" + key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", key, applied, err)
		}
		cid, applied, err := vacc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: obID, GoatID: g, Doses: &doses, RouteSite: "SC",
			AdministeredAt: administered, Status: "recorded", IdempotencyKey: "qc-" + key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", key, applied, err)
		}
		return cid
	}
	cidEarly := mk("early", time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC))
	cidLate := mk("late", time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC))
	cidAccepted := mk("accepted", time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC))
	if _, applied, err := vacc.AcceptCompletion(ctx, impTenant, cidAccepted, nil, nil); err != nil || !applied {
		t.Fatalf("accept: applied=%v err=%v", applied, err)
	}

	queue, err := vacc.ListRecordedCompletions(ctx, impTenant, "", nil, 100)
	if err != nil {
		t.Fatalf("list recorded: %v", err)
	}
	if len(queue.Items) != 2 || queue.TotalCount != 2 || queue.NextCursor != nil {
		t.Fatalf("want 2 recorded (accepted excluded), got %#v", queue)
	}
	if queue.Items[0].CompletionID != cidEarly || queue.Items[1].CompletionID != cidLate {
		t.Fatalf("want earliest administered first [%s,%s], got [%s,%s]", cidEarly, cidLate, queue.Items[0].CompletionID, queue.Items[1].CompletionID)
	}
}

func TestListRecordedCompletionsQueueIncludesSOPReviewHandle(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.queue.task", Name: "Queue Task", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const (
		g    = "30000000-0000-4000-8000-0000000000c9"
		sop  = "30000000-0000-4000-8000-0000000000ca"
		ver  = "30000000-0000-4000-8000-0000000000cb"
		task = "30000000-0000-4000-8000-0000000000cc"
		sub  = "30000000-0000-4000-8000-0000000000cd"
		item = "30000000-0000-4000-8000-0000000000ce"
	)
	seedGenGoat(t, ctx, pool, g, "alive")
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_definitions (sop_id, tenant_id, code, name, status)
VALUES ($1, $2, 'vaccination.queue_task', 'Vaccination Queue Task', 'active')`, sop, impTenant); err != nil {
		t.Fatalf("sop: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, published_at)
VALUES ($1, $2, $3, 1, 'v1', 'published', '{}'::jsonb, '{}'::jsonb, now())`, ver, impTenant, sop); err != nil {
		t.Fatalf("sop version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, scope_type, scope_id, state, row_version)
VALUES ($1, $2, $3, $4, 'vaccination_drive', 'Drive', 'shed', $5, 'needs_review', 4)`, task, impTenant, sop, ver, impCbe); err != nil {
		t.Fatalf("task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_submissions (submission_id, tenant_id, task_id, sop_version_id, submitted_by, idempotency_key, state, answers, proof_refs)
VALUES ($1, $2, $3, $4, $5, 'queue-task-sub', 'needs_review', '{}'::jsonb, '[]'::jsonb)`, sub, impTenant, task, ver, impParty); err != nil {
		t.Fatalf("submission: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sop_submission_items (item_id, tenant_id, submission_id, task_id, goat_id, item_key, state, result)
VALUES ($1, $2, $3, $4, $5::uuid, 'goat:' || $5::text, 'needs_review', '{}'::jsonb)`, item, impTenant, sub, task, g); err != nil {
		t.Fatalf("item: %v", err)
	}
	obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
		TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: g, ScopeType: "tenant", ScopeID: impTenant,
		DueAt: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "queue-task-ob", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("obligation: applied=%v err=%v", applied, err)
	}
	doses := int32(1)
	itemID := item
	cid, applied, err := vacc.RecordCompletion(ctx, vaccdomain.NewCompletion{
		TenantID: impTenant, ObligationID: obID, GoatID: g, SopSubmissionItemID: &itemID, Doses: &doses, RouteSite: "SC",
		AdministeredAt: time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), Status: "recorded", IdempotencyKey: "queue-task-completion",
	})
	if err != nil || !applied {
		t.Fatalf("record: applied=%v err=%v", applied, err)
	}

	queue, err := vacc.ListRecordedCompletions(ctx, impTenant, "", nil, 100)
	if err != nil {
		t.Fatalf("list recorded: %v", err)
	}
	if len(queue.Items) != 1 || queue.TotalCount != 1 || queue.Items[0].CompletionID != cid || queue.Items[0].SOPTaskID != task || queue.Items[0].SOPTaskVersion != 4 {
		t.Fatalf("queue task handle = %#v", queue)
	}
}

// TestListRecordedCompletionsParkScope proves the verification queue honors the top-bar park scope:
// a recorded completion in CBE and one in another park, then parkID=CBE must exclude the other park.
func TestListRecordedCompletionsParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.queuepark", Name: "QueuePark", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const parkB = "00000000-0000-4000-8000-0000000030b2"
	const gA = "30000000-0000-4000-8000-0000000000b1"
	const gB = "30000000-0000-4000-8000-0000000000b2"
	// gA lives in CBE (impCbe). Create a second park and gB inside it.
	seedGenGoat(t, ctx, pool, gA, "alive")
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PKB', 'Park B', 'active')`, parkB, impTenant); err != nil {
		t.Fatalf("park B: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, management_stage, dob)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, 'K1', DATE '2026-05-01')`, gB, impTenant, impParty, parkB); err != nil {
		t.Fatalf("gB: %v", err)
	}

	doses := int32(1)
	rec := func(goat, key string, administered time.Time) string {
		obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: administered, Status: "scheduled", IdempotencyKey: "qp-" + key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", key, applied, err)
		}
		cid, applied, err := vacc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: obID, GoatID: goat, Doses: &doses, RouteSite: "SC",
			AdministeredAt: administered, Status: "recorded", IdempotencyKey: "qpc-" + key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", key, applied, err)
		}
		return cid
	}
	cidCbe := rec(gA, "cbe", time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC))
	cidParkB := rec(gB, "parkb", time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC))

	// All parks: both recorded completions present.
	all, err := vacc.ListRecordedCompletions(ctx, impTenant, "", nil, 100)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all.Items) != 2 || all.TotalCount != 2 {
		t.Fatalf("all parks: want 2 recorded, got %#v", all)
	}

	// Scoped to CBE: only the CBE completion; the other park is excluded.
	cbe, err := vacc.ListRecordedCompletions(ctx, impTenant, impCbe, nil, 100)
	if err != nil {
		t.Fatalf("list cbe: %v", err)
	}
	if len(cbe.Items) != 1 || cbe.TotalCount != 1 || cbe.Items[0].CompletionID != cidCbe {
		t.Fatalf("park=CBE: want only %s, got %#v", cidCbe, cbe)
	}

	// Scoped to park B: only park B's completion.
	parkBRows, err := vacc.ListRecordedCompletions(ctx, impTenant, parkB, nil, 100)
	if err != nil {
		t.Fatalf("list parkB: %v", err)
	}
	if len(parkBRows.Items) != 1 || parkBRows.TotalCount != 1 || parkBRows.Items[0].CompletionID != cidParkB {
		t.Fatalf("park=B: want only %s, got %#v", cidParkB, parkBRows)
	}
}

func TestListRecordedCompletionsQueueCursorPagination(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.queue.cursor", Name: "Queue Cursor", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	goatIDs := []string{
		"30000000-0000-4000-8000-0000000000d1",
		"30000000-0000-4000-8000-0000000000d2",
		"30000000-0000-4000-8000-0000000000d3",
	}
	for _, goatID := range goatIDs {
		seedGenGoat(t, ctx, pool, goatID, "alive")
	}
	doses := int32(1)
	administeredAt := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	recorded := make([]string, 0, len(goatIDs))
	for i, goatID := range goatIDs {
		key := fmt.Sprintf("cursor-%d", i)
		obID, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: impTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "tenant", ScopeID: impTenant,
			DueAt: administeredAt, Status: "scheduled", IdempotencyKey: "qcursor-ob-" + key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", key, applied, err)
		}
		cid, applied, err := vacc.RecordCompletion(ctx, vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: obID, GoatID: goatID, Doses: &doses, RouteSite: "SC",
			AdministeredAt: administeredAt, Status: "recorded", IdempotencyKey: "qcursor-" + key,
		})
		if err != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", key, applied, err)
		}
		recorded = append(recorded, cid)
	}

	page1, err := vacc.ListRecordedCompletions(ctx, impTenant, "", nil, 2)
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 2 || page1.TotalCount != 3 || page1.NextCursor == nil {
		t.Fatalf("page1 = %#v", page1)
	}
	cursor, err := vaccdomain.DecodeRecordedCompletionCursor(*page1.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	if cursor.AdministeredAt != administeredAt || cursor.CompletionID != page1.Items[1].CompletionID {
		t.Fatalf("cursor = %+v, page1 second row = %#v", cursor, page1.Items[1])
	}
	page2, err := vacc.ListRecordedCompletions(ctx, impTenant, "", &cursor, 2)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 1 || page2.TotalCount != 3 || page2.NextCursor != nil {
		t.Fatalf("page2 = %#v", page2)
	}
	seen := map[string]struct{}{}
	for _, item := range append(page1.Items, page2.Items...) {
		if _, exists := seen[item.CompletionID]; exists {
			t.Fatalf("duplicate completion %s across pages", item.CompletionID)
		}
		seen[item.CompletionID] = struct{}{}
	}
	for _, cid := range recorded {
		if _, ok := seen[cid]; !ok {
			t.Fatalf("missing completion %s across pages", cid)
		}
	}
}
