package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/protocol/ports"
)

const testTenantID = "00000000-0000-4000-8000-000000000001"

func TestPublishVersionWritesDurableOutboxEvent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID,
		Code:     "vaccination.publish.outbox",
		Name:     "Vaccination Publish Outbox",
		Category: "vaccination",
		Status:   "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    protocolID,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, versionID, nil, "publish-outbox-key"); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, versionID, nil, "publish-outbox-key"); err != nil {
		t.Fatalf("publish replay: %v", err)
	}
	otherProtocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID,
		Code:     "vaccination.publish.other",
		Name:     "Vaccination Publish Other",
		Category: "vaccination",
		Status:   "draft",
	})
	if err != nil {
		t.Fatalf("create other definition: %v", err)
	}
	otherVersionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    otherProtocolID,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create other version: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, otherVersionID, nil, "publish-outbox-key"); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("publish key reused for different version err=%v, want ErrIdempotencyConflict", err)
	}

	var status, eventType, aggregateType, aggregateID, idempotencyKey string
	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT pv.status, om.event_type, om.aggregate_type, om.aggregate_id::text, om.idempotency_key, om.payload
FROM protocol_versions pv
JOIN outbox_messages om
  ON om.tenant_id = pv.tenant_id
 AND om.aggregate_id = pv.protocol_version_id
WHERE pv.tenant_id = $1
  AND pv.protocol_version_id = $2::uuid`, testTenantID, versionID).Scan(
		&status, &eventType, &aggregateType, &aggregateID, &idempotencyKey, &payload,
	); err != nil {
		t.Fatalf("read publish outbox: %v", err)
	}
	if status != "published" || eventType != "protocol.version.published" || aggregateType != "protocol_version" || aggregateID != versionID {
		t.Fatalf("status/event = %s/%s/%s/%s, want published protocol.version.published protocol_version %s", status, eventType, aggregateType, aggregateID, versionID)
	}
	if idempotencyKey != "protocol:version:published:"+versionID {
		t.Fatalf("idempotency key = %q", idempotencyKey)
	}
	var publishID string
	if err := pool.QueryRow(ctx, `
SELECT result_id::text
FROM idempotency_keys
WHERE idempotency_key = $1`, testTenantID+":protocol.version.publish:publish-outbox-key").Scan(&publishID); err != nil {
		t.Fatalf("read publish idempotency key: %v", err)
	}
	if publishID != versionID {
		t.Fatalf("publish idempotency result = %s, want %s", publishID, versionID)
	}
	var envelope struct {
		EventType       string `json:"event_type"`
		AggregateType   string `json:"aggregate_type"`
		AggregateID     string `json:"aggregate_id"`
		SubjectType     string `json:"subject_type"`
		VisibilityScope struct {
			TenantID string `json:"tenant_id"`
		} `json:"visibility_scope"`
		Payload struct {
			ProtocolVersionID string `json:"protocol_version_id"`
			Category          string `json:"category"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode outbox envelope: %v", err)
	}
	if envelope.EventType != "protocol.version.published" ||
		envelope.AggregateType != "protocol_version" ||
		envelope.AggregateID != versionID ||
		envelope.SubjectType != "protocol_version" ||
		envelope.VisibilityScope.TenantID != testTenantID ||
		envelope.Payload.ProtocolVersionID != versionID ||
		envelope.Payload.Category != "vaccination" {
		t.Fatalf("unexpected envelope: %#v", envelope)
	}
}

func TestProtocolCreateWritesAreIdempotentAndAudited(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	actorID := "90000000-0000-4000-8000-000000000101"
	defInput := domain.NewDefinition{
		TenantID:       testTenantID,
		Code:           "vaccination.idempotent.create",
		Name:           "Vaccination Idempotent Create",
		Category:       "vaccination",
		Status:         "draft",
		CreatedBy:      &actorID,
		IdempotencyKey: "def-key-0001",
	}
	protocolID, err := repo.CreateDefinition(ctx, defInput)
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	replayedProtocolID, err := repo.CreateDefinition(ctx, defInput)
	if err != nil {
		t.Fatalf("replay definition: %v", err)
	}
	if replayedProtocolID != protocolID {
		t.Fatalf("definition replay = %s, want %s", replayedProtocolID, protocolID)
	}
	defInput.Name = "Different Name"
	if _, err := repo.CreateDefinition(ctx, defInput); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("definition conflict err=%v, want ErrIdempotencyConflict", err)
	}

	versionInput := domain.NewVersion{
		TenantID:       testTenantID,
		ProtocolID:     protocolID,
		ScopeType:      "tenant",
		Version:        1,
		Status:         "draft",
		EffectiveFrom:  time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:        []byte(`{"source":{"source_system":"vaccinations_db"}}`),
		ProofPolicy:    []byte(`{"required_proofs":["video"]}`),
		DraftedBy:      &actorID,
		IdempotencyKey: "version-key-0001",
	}
	versionID, err := repo.CreateVersion(ctx, versionInput)
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	replayedVersionID, err := repo.CreateVersion(ctx, versionInput)
	if err != nil {
		t.Fatalf("replay version: %v", err)
	}
	if replayedVersionID != versionID {
		t.Fatalf("version replay = %s, want %s", replayedVersionID, versionID)
	}
	versionInput.VersionLabel = "different"
	if _, err := repo.CreateVersion(ctx, versionInput); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("version conflict err=%v, want ErrIdempotencyConflict", err)
	}

	ruleInput := domain.NewRule{
		TenantID:          testTenantID,
		ProtocolVersionID: versionID,
		DoseCode:          "PPR-1",
		Sequence:          1,
		TriggerType:       "post_arrival",
		OffsetDays:        0,
		DueWindowDays:     7,
		Repeat:            "none",
		CatchUp:           "immediate",
		EligibilityJSON:   []byte(`{"health":["healthy","sick"]}`),
		ProofPolicy:       []byte(`{"required_proofs":["video"]}`),
		SortOrder:         1,
		CreatedBy:         &actorID,
		IdempotencyKey:    "rule-key-0001",
	}
	ruleID, err := repo.CreateRule(ctx, ruleInput)
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	replayedRuleID, err := repo.CreateRule(ctx, ruleInput)
	if err != nil {
		t.Fatalf("replay rule: %v", err)
	}
	if replayedRuleID != ruleID {
		t.Fatalf("rule replay = %s, want %s", replayedRuleID, ruleID)
	}
	ruleInput.DueWindowDays = 10
	if _, err := repo.CreateRule(ctx, ruleInput); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("rule conflict err=%v, want ErrIdempotencyConflict", err)
	}

	var defCount, versionCount, ruleCount, auditCount, humanRuleAuditCount, idemCount int
	if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM protocol_definitions WHERE tenant_id=$1::uuid AND code='vaccination.idempotent.create'),
  (SELECT count(*) FROM protocol_versions WHERE tenant_id=$1::uuid AND protocol_id=$2::uuid),
  (SELECT count(*) FROM protocol_rules WHERE tenant_id=$1::uuid AND protocol_version_id=$3::uuid),
  (SELECT count(*) FROM audit_log WHERE tenant_id=$1::uuid AND action IN ('protocol.definition.created','protocol.version.created','protocol.rule.created')),
  (SELECT count(*) FROM audit_log WHERE tenant_id=$1::uuid AND action='protocol.rule.created' AND actor_id=$7::uuid AND actor_type='human'),
  (SELECT count(*) FROM idempotency_keys WHERE idempotency_key IN ($4, $5, $6))`,
		testTenantID,
		protocolID,
		versionID,
		testTenantID+":protocol.definition.create:def-key-0001",
		testTenantID+":protocol.version.create:version-key-0001",
		testTenantID+":protocol.rule.create:rule-key-0001",
		actorID,
	).Scan(&defCount, &versionCount, &ruleCount, &auditCount, &humanRuleAuditCount, &idemCount); err != nil {
		t.Fatalf("read idempotent create evidence: %v", err)
	}
	if defCount != 1 || versionCount != 1 || ruleCount != 1 {
		t.Fatalf("row counts def/version/rule = %d/%d/%d, want 1/1/1", defCount, versionCount, ruleCount)
	}
	if auditCount != 3 {
		t.Fatalf("audit count = %d, want 3", auditCount)
	}
	if humanRuleAuditCount != 1 {
		t.Fatalf("human rule audit count = %d, want 1", humanRuleAuditCount)
	}
	if idemCount != 3 {
		t.Fatalf("idempotency key count = %d, want 3", idemCount)
	}
}

func TestPublishedProtocolConfigRejectsMutableWrites(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID,
		Code:     "vaccination.published.delete.guard",
		Name:     "Published Delete Guard",
		Category: "vaccination",
		Status:   "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    protocolID,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	ruleID, err := repo.CreateRule(ctx, domain.NewRule{
		TenantID:          testTenantID,
		ProtocolVersionID: versionID,
		DoseCode:          "PPR-1",
		Sequence:          1,
		TriggerType:       "post_arrival",
		OffsetDays:        0,
		DueWindowDays:     7,
		MinGapDays:        0,
		Repeat:            "none",
		CatchUp:           "immediate",
		EligibilityJSON:   []byte(`{}`),
		ProofPolicy:       []byte(`{}`),
		SortOrder:         1,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	triggerID, err := repo.CreateTrigger(ctx, domain.NewTrigger{
		TenantID:          testTenantID,
		ProtocolVersionID: versionID,
		TriggerType:       "schedule",
		TriggerConfig:     []byte(`{}`),
		IsActive:          true,
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, versionID, nil); err != nil {
		t.Fatalf("publish version: %v", err)
	}

	assertDeleteBlocked := func(name, expected string, statement string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, statement, args...); err == nil {
			t.Fatalf("%s delete succeeded, want immutable published config error", name)
		} else if !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s delete error = %v, want %q guard", name, err, expected)
		}
	}

	assertDeleteBlocked("rule", "published config is immutable", `DELETE FROM protocol_rules WHERE tenant_id=$1::uuid AND rule_id=$2::uuid`, testTenantID, ruleID)
	assertDeleteBlocked("trigger", "published config is immutable", `DELETE FROM protocol_triggers WHERE tenant_id=$1::uuid AND trigger_id=$2::uuid`, testTenantID, triggerID)
	assertDeleteBlocked("version", "published or retired protocol version", `DELETE FROM protocol_versions WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid`, testTenantID, versionID)

	assertUpdateBlocked := func(name, expected string, statement string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, statement, args...); err == nil {
			t.Fatalf("%s update succeeded, want immutable published config error", name)
		} else if !strings.Contains(err.Error(), expected) {
			t.Fatalf("%s update error = %v, want %q guard", name, err, expected)
		}
	}

	assertUpdateBlocked("rule", "published config is immutable", `UPDATE protocol_rules SET offset_days = offset_days + 1 WHERE tenant_id=$1::uuid AND rule_id=$2::uuid`, testTenantID, ruleID)
	assertUpdateBlocked("trigger", "published config is immutable", `UPDATE protocol_triggers SET is_active = false WHERE tenant_id=$1::uuid AND trigger_id=$2::uuid`, testTenantID, triggerID)
	assertUpdateBlocked("version", "published protocol version", `UPDATE protocol_versions SET rule_dsl = '{"changed":true}'::jsonb WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid`, testTenantID, versionID)

	var ruleCount, triggerCount, versionCount int
	if err := pool.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM protocol_rules WHERE tenant_id=$1::uuid AND rule_id=$2::uuid),
  (SELECT count(*) FROM protocol_triggers WHERE tenant_id=$1::uuid AND trigger_id=$3::uuid),
  (SELECT count(*) FROM protocol_versions WHERE tenant_id=$1::uuid AND protocol_version_id=$4::uuid)`,
		testTenantID, ruleID, triggerID, versionID).Scan(&ruleCount, &triggerCount, &versionCount); err != nil {
		t.Fatalf("count guarded rows: %v", err)
	}
	if ruleCount != 1 || triggerCount != 1 || versionCount != 1 {
		t.Fatalf("guarded row counts = rule %d trigger %d version %d, want all 1", ruleCount, triggerCount, versionCount)
	}
}

// TestListEffectiveVaccinationVersionsForGoatPicksParkOverride proves the per-goat effective-version
// selection is scope-aware: when a tenant-default AND a park-scoped published version of the SAME
// protocol are both effective at as_of (protocol_versions enforces non-overlap per scope, so they
// coexist), a goat in that park is matched to the park override and a goat outside it to the tenant
// default — never both, and never another park's calendar.
func TestListEffectiveVaccinationVersionsForGoatPicksParkOverride(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.scope.override", Name: "Scope Override",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}

	parkA := "00000000-0000-4000-8000-00000000a001"
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PARKA', 'Park A', 'active')`, parkA, testTenantID); err != nil {
		t.Fatalf("park location: %v", err)
	}

	// Tenant default effective from Jun 1; park override effective from Jun 5 — both open-ended, so both
	// are effective at as_of=Jun 20 under the per-scope non-overlap constraint.
	tenantVer, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create tenant version: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, tenantVer, nil); err != nil {
		t.Fatalf("publish tenant version: %v", err)
	}
	parkVer, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "park", ScopeID: &parkA, Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create park version: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, parkVer, nil); err != nil {
		t.Fatalf("publish park version: %v", err)
	}

	asOf := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	// Goat in park A → park override wins (most-specific scope), exactly one row.
	if got, err := repo.ListEffectiveVaccinationVersionsForGoat(ctx, testTenantID, parkA, asOf); err != nil {
		t.Fatalf("list for park goat: %v", err)
	} else if len(got) != 1 || got[0] != parkVer {
		t.Fatalf("park goat: got %v, want [%s] (park override)", got, parkVer)
	}

	// Goat with no park → tenant default only.
	if got, err := repo.ListEffectiveVaccinationVersionsForGoat(ctx, testTenantID, "", asOf); err != nil {
		t.Fatalf("list for no-park goat: %v", err)
	} else if len(got) != 1 || got[0] != tenantVer {
		t.Fatalf("no-park goat: got %v, want [%s] (tenant default)", got, tenantVer)
	}

	// Goat in a different park (no override published for it) → tenant default, never park A's calendar.
	parkB := "00000000-0000-4000-8000-00000000b002"
	if got, err := repo.ListEffectiveVaccinationVersionsForGoat(ctx, testTenantID, parkB, asOf); err != nil {
		t.Fatalf("list for other-park goat: %v", err)
	} else if len(got) != 1 || got[0] != tenantVer {
		t.Fatalf("other-park goat: got %v, want [%s] (tenant default)", got, tenantVer)
	}
}

func TestListEffectiveVaccinationVersionsForGoatUsesKolkataCutoverDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.cutover.tz", Name: "Cutover TZ",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}

	v1End := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	v1, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), EffectiveTo: &v1End,
		RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, v1, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	v2, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 2, Status: "draft",
		EffectiveFrom: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, v2, nil); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	// 2026-06-30T20:00Z is already 2026-07-01 in Asia/Kolkata, the tenant operating calendar.
	asOf := time.Date(2026, 6, 30, 20, 0, 0, 0, time.UTC)
	got, err := repo.ListEffectiveVaccinationVersionsForGoat(ctx, testTenantID, "", asOf)
	if err != nil {
		t.Fatalf("list effective: %v", err)
	}
	if len(got) != 1 || got[0] != v2 {
		t.Fatalf("got %v, want [%s] for Kolkata cutover date", got, v2)
	}
}
