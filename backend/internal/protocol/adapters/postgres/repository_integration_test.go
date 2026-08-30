package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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

	// One draft per scope is now a database rule, so the first draft has to stop being a
	// draft before a second version can be created here. Retiring it keeps what this test is
	// actually about -- that the backend allocates the next version number itself -- since
	// allocation counts every version, whatever its status.
	if _, err := pool.Exec(ctx, `
UPDATE protocol_versions SET status = 'retired'
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`, testTenantID, versionID); err != nil {
		t.Fatalf("retire first draft: %v", err)
	}

	autoVersionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:       testTenantID,
		ProtocolID:     protocolID,
		ScopeType:      "tenant",
		Version:        0,
		Status:         "draft",
		EffectiveFrom:  time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:        []byte(`{"ruleset_family":"vaccination.matrix"}`),
		ProofPolicy:    []byte(`{"required_proofs":["video"]}`),
		DraftedBy:      &actorID,
		IdempotencyKey: "version-key-auto",
	})
	if err != nil {
		t.Fatalf("create backend-allocated version: %v", err)
	}
	var allocatedVersion int32
	if err := pool.QueryRow(ctx, `
SELECT version
FROM protocol_versions
WHERE tenant_id = $1::uuid
  AND protocol_version_id = $2::uuid`, testTenantID, autoVersionID).Scan(&allocatedVersion); err != nil {
		t.Fatalf("read backend-allocated version: %v", err)
	}
	if allocatedVersion != 2 {
		t.Fatalf("backend allocated version = %d, want 2", allocatedVersion)
	}

	// Rules attach to the draft, which is now the backend-allocated one: the first version
	// was retired above so a second could be created at all.
	ruleInput := domain.NewRule{
		TenantID:          testTenantID,
		ProtocolVersionID: autoVersionID,
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
		// The rule count is read against the version the rule was attached to.
		autoVersionID,
		testTenantID+":protocol.definition.create:def-key-0001",
		testTenantID+":protocol.version.create:version-key-0001",
		testTenantID+":protocol.rule.create:rule-key-0001",
		actorID,
	).Scan(&defCount, &versionCount, &ruleCount, &auditCount, &humanRuleAuditCount, &idemCount); err != nil {
		t.Fatalf("read idempotent create evidence: %v", err)
	}
	if defCount != 1 || versionCount != 2 || ruleCount != 1 {
		t.Fatalf("row counts def/version/rule = %d/%d/%d, want 1/2/1", defCount, versionCount, ruleCount)
	}
	if auditCount != 4 {
		t.Fatalf("audit count = %d, want 4", auditCount)
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

func TestPublishVersionWithDerivedRulesReplacesOverlappingVaccinationMatrixFamily(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	firstProtocol, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.matrix.closeout.a", Name: "Matrix A",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create first definition: %v", err)
	}
	secondProtocol, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.matrix.closeout.b", Name: "Matrix B",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create second definition: %v", err)
	}
	firstVersion, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    firstProtocol,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       vaccinationMatrixRuleDSL(),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create first version: %v", err)
	}
	firstRules, firstDims := derivedMatrixRows(firstVersion, "10000000-0000-4000-8000-000000000001", "et_tt")
	if err := repo.PublishVersionWithDerivedRules(ctx, testTenantID, domain.Version{
		ProtocolVersionID: firstVersion,
		ProtocolID:        firstProtocol,
		ScopeType:         "tenant",
		Status:            "draft",
		RuleDsl:           vaccinationMatrixRuleDSL(),
	}, firstRules, firstDims, nil, nil, "", "matrix-family-first"); err != nil {
		t.Fatalf("publish first matrix version: %v", err)
	}

	secondVersion, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    secondProtocol,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		RuleDsl:       vaccinationMatrixRuleDSL(),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create second version: %v", err)
	}
	secondRules, secondDims := derivedMatrixRows(secondVersion, "10000000-0000-4000-8000-000000000002", "ppr")
	if err := repo.PublishVersionWithDerivedRules(ctx, testTenantID, domain.Version{
		ProtocolVersionID: secondVersion,
		ProtocolID:        secondProtocol,
		ScopeType:         "tenant",
		Status:            "draft",
		RuleDsl:           vaccinationMatrixRuleDSL(),
	}, secondRules, secondDims, nil, nil, "", "matrix-family-overlap"); err != nil {
		t.Fatalf("publish replacement matrix: %v", err)
	}
	var firstStatus, secondStatus string
	if err := pool.QueryRow(ctx, `
SELECT first.status, second.status
FROM protocol_versions first, protocol_versions second
WHERE first.tenant_id = $1
  AND first.protocol_version_id = $2
  AND second.tenant_id = $1
  AND second.protocol_version_id = $3`, testTenantID, firstVersion, secondVersion).Scan(&firstStatus, &secondStatus); err != nil {
		t.Fatalf("read replacement statuses: %v", err)
	}
	if firstStatus != "retired" || secondStatus != "published" {
		t.Fatalf("replacement statuses first=%s second=%s, want retired/published", firstStatus, secondStatus)
	}
	var retiredOutboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1
  AND event_type = 'protocol.version.retired'
  AND aggregate_id = $2::uuid`, testTenantID, firstVersion).Scan(&retiredOutboxCount); err != nil {
		t.Fatalf("count retired outbox: %v", err)
	}
	if retiredOutboxCount != 1 {
		t.Fatalf("retired outbox count = %d, want 1", retiredOutboxCount)
	}
	var retiredEventType, retiredAggregateType, retiredAggregateID, retiredIdempotencyKey string
	var retiredPayload []byte
	if err := pool.QueryRow(ctx, `
SELECT event_type, aggregate_type, aggregate_id::text, idempotency_key, payload
FROM outbox_messages
WHERE tenant_id = $1
  AND event_type = 'protocol.version.retired'
  AND aggregate_id = $2::uuid`, testTenantID, firstVersion).Scan(
		&retiredEventType, &retiredAggregateType, &retiredAggregateID, &retiredIdempotencyKey, &retiredPayload,
	); err != nil {
		t.Fatalf("read retired outbox: %v", err)
	}
	expectedRetiredKey := "protocol:version:retired:" + firstVersion + ":by:" + secondVersion
	if retiredEventType != "protocol.version.retired" || retiredAggregateType != "protocol_version" || retiredAggregateID != firstVersion || retiredIdempotencyKey != expectedRetiredKey {
		t.Fatalf("retired outbox = %s/%s/%s/%s, want protocol.version.retired/protocol_version/%s/%s", retiredEventType, retiredAggregateType, retiredAggregateID, retiredIdempotencyKey, firstVersion, expectedRetiredKey)
	}
	var retiredEnvelope struct {
		EventType   string `json:"event_type"`
		AggregateID string `json:"aggregate_id"`
		Payload     struct {
			ProtocolVersionID           string `json:"protocol_version_id"`
			ReplacedByProtocolVersionID string `json:"replaced_by_protocol_version_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(retiredPayload, &retiredEnvelope); err != nil {
		t.Fatalf("decode retired outbox envelope: %v", err)
	}
	if retiredEnvelope.EventType != "protocol.version.retired" ||
		retiredEnvelope.AggregateID != firstVersion ||
		retiredEnvelope.Payload.ProtocolVersionID != firstVersion ||
		retiredEnvelope.Payload.ReplacedByProtocolVersionID != secondVersion {
		t.Fatalf("unexpected retired envelope: %#v", retiredEnvelope)
	}

	parkID := "00000000-0000-4000-8000-00000000c003"
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'park', 'PARKC', 'Park C', 'active')`, parkID, testTenantID); err != nil {
		t.Fatalf("park location: %v", err)
	}
	parkVersion, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    secondProtocol,
		ScopeType:     "park",
		ScopeID:       &parkID,
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		RuleDsl:       vaccinationMatrixRuleDSL(),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create park version: %v", err)
	}
	parkRules, parkDims := derivedMatrixRows(parkVersion, "10000000-0000-4000-8000-000000000003", "park_et_tt")
	if err := repo.PublishVersionWithDerivedRules(ctx, testTenantID, domain.Version{
		ProtocolVersionID: parkVersion,
		ProtocolID:        secondProtocol,
		ScopeType:         "park",
		ScopeID:           parkID,
		Status:            "draft",
		RuleDsl:           vaccinationMatrixRuleDSL(),
	}, parkRules, parkDims, nil, nil, "", "matrix-family-park-override"); err != nil {
		t.Fatalf("park-scoped matrix should coexist with tenant default: %v", err)
	}
}

func TestPublishVersionRetiresPreviousPlanPublishedAnchors(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.anchor.lifecycle", Name: "Vaccination Anchor Lifecycle",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	v1, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    protocolID,
		ScopeType:     "tenant",
		Version:       1,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"category":"vaccination"}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, v1, nil, "anchor-lifecycle-v1"); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key, request_hash
) VALUES (
  '10000000-0000-4000-8000-000000000211', $1::uuid, $2::uuid, 'PPR', 'ppr_kid_16w', '2026-09-08',
  'tenant', '{}'::jsonb, 'old plan anchor', 'vaccination_plan_publish', 'old-plan-anchor', repeat('a', 64)
)`, testTenantID, v1); err != nil {
		t.Fatalf("seed old anchor: %v", err)
	}

	v2, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID:      testTenantID,
		ProtocolID:    protocolID,
		ScopeType:     "tenant",
		Version:       2,
		Status:        "draft",
		EffectiveFrom: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"category":"vaccination"}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, v2, nil, "anchor-lifecycle-v2"); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	var canceled bool
	var reason string
	if err := pool.QueryRow(ctx, `
SELECT canceled_at IS NOT NULL, COALESCE(cancel_reason, '')
FROM vaccination_anchor_events
WHERE tenant_id = $1::uuid
  AND idempotency_key = 'old-plan-anchor'`, testTenantID).Scan(&canceled, &reason); err != nil {
		t.Fatalf("read old anchor: %v", err)
	}
	if !canceled || reason != "replaced_by_protocol_publish" {
		t.Fatalf("old anchor canceled=%v reason=%q, want replaced_by_protocol_publish", canceled, reason)
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

func vaccinationMatrixRuleDSL() []byte {
	return []byte(`{"category":"vaccination","ruleset_family":"vaccination.matrix","vaccine":{"code":"vaccination.matrix","name":"Preventive Care vaccination matrix","type":"matrix"},"matrix_rows":[{"row_id":"all","eligibility":{"species":["all"],"animal_stage":["all"],"sex":["all"],"breed":["all"]},"vaccine":{"code":"ET_TT","type":"killed"},"schedule":[{"dose_code":"et_tt"}]}],"schedule":[{"dose_code":"et_tt","trigger_type":"post_arrival","offset_days":0,"due_window_days":7}]}`)
}

func derivedMatrixRows(versionID, ruleID, doseCode string) ([]domain.NewRule, []domain.RuleDimension) {
	eligibility := []byte(`{"matrix_row_id":"all","source_dose_code":"` + doseCode + `","eligibility":{"species":["all"],"animal_stage":["all"],"sex":["all"],"breed":["all"],"lifecycle":["alive"]},"vaccine":{"code":"ET_TT","type":"killed","pathogen_class":"bacterial"}}`)
	schedule := []byte(`{"dose_code":"` + doseCode + `","trigger_type":"post_arrival","offset_days":0,"due_window_days":7}`)
	rules := []domain.NewRule{{
		RuleID:            ruleID,
		TenantID:          testTenantID,
		ProtocolVersionID: versionID,
		DoseCode:          doseCode,
		Sequence:          1,
		TriggerType:       "post_arrival",
		DueWindowDays:     7,
		Repeat:            "none",
		CatchUp:           "immediate",
		EligibilityJSON:   eligibility,
		ProofPolicy:       []byte(`{}`),
		SortOrder:         1,
	}}
	dimensions := []domain.RuleDimension{{
		Category:          "vaccination",
		RulesetFamily:     "vaccination.matrix",
		ProtocolVersionID: versionID,
		RuleID:            ruleID,
		MatrixRowID:       "all",
		SelectorKey:       "all",
		DoseCode:          doseCode,
		SourceDoseCode:    doseCode,
		VaccineCode:       "ET_TT",
		VaccineType:       "killed",
		PathogenClass:     "bacterial",
		Species:           "all",
		AnimalStage:       "all",
		Sex:               "all",
		Breed:             "all",
		Lifecycle:         "alive",
		Health:            "all",
		Reproductive:      "all",
		TriggerType:       "post_arrival",
		Sequence:          1,
		DueWindowDays:     7,
		EligibilityJSON:   eligibility,
		VaccineJSON:       []byte(`{"code":"ET_TT","type":"killed","pathogen_class":"bacterial"}`),
		ScheduleJSON:      schedule,
	}}
	return rules, dimensions
}

// TestSyncVaccinationCapacityConfigUpsertsAndReturnsStored proves the publish-time capacity sync SQL:
// it upserts vaccination_capacity_config for the tenant and returns the stored row (used for the
// post-publish parity check), and a re-sync overwrites it (derived read model).
func TestSyncVaccinationCapacityConfigUpsertsAndReturnsStored(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	want := domain.PublishedCapacity{MaxPerDay: 137, MaxBufferDays: 7, CapacityScope: "tenant", OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap"}
	got, err := repo.SyncVaccinationCapacityConfig(ctx, testTenantID, want)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got != want {
		t.Fatalf("sync returned %+v, want %+v (parity check would fail)", got, want)
	}

	var mp, mb int
	var scope, overflow string
	if err := pool.QueryRow(ctx,
		`SELECT max_per_day, max_buffer_days, capacity_scope, overflow_policy
		 FROM vaccination_capacity_config WHERE tenant_id = $1::uuid`, testTenantID).
		Scan(&mp, &mb, &scope, &overflow); err != nil {
		t.Fatalf("read stored capacity: %v", err)
	}
	if mp != 137 || mb != 7 || scope != "tenant" || overflow != "split_within_safe_window_last_safe_may_exceed_cap" {
		t.Fatalf("stored capacity = %d/%d/%s/%s, want 137/7/tenant/split...", mp, mb, scope, overflow)
	}

	want2 := domain.PublishedCapacity{MaxPerDay: 200, MaxBufferDays: 2, CapacityScope: "tenant", OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap"}
	got2, err := repo.SyncVaccinationCapacityConfig(ctx, testTenantID, want2)
	if err != nil {
		t.Fatalf("re-sync: %v", err)
	}
	if got2 != want2 {
		t.Fatalf("re-sync returned %+v, want %+v", got2, want2)
	}
}

// TestPublishVersionWithCapacityEnqueuesCapacityChangedForEveryConfiguredPark reproduces a real
// gap: publishing a version with capacity synced vaccination_capacity_config (tenant-scoped)
// transactionally, but never told OperatorConfigReplanHandler anything changed -- so a park's
// already-planned future vaccination drives were never released/replanned to reflect the new
// capacity policy. vaccination_capacity_config has no park scope (PK is tenant_id alone), so the
// fix fans out vaccination.capacity.changed to every park that has an operator-assignment config
// in the tenant. A park with NO operator-assignment config must get no event (nothing to replan).
func TestPublishVersionWithCapacityEnqueuesCapacityChangedForEveryConfiguredPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const parkA = "00000000-0000-4000-8000-0000000cb001"
	const parkB = "00000000-0000-4000-8000-0000000cb002"
	const parkUnconfigured = "00000000-0000-4000-8000-0000000cb003"
	const operatorA = "00000000-0000-4000-8000-0000000cb011"
	const operatorB = "00000000-0000-4000-8000-0000000cb012"

	for _, park := range []string{parkA, parkB, parkUnconfigured} {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $1, 'Park ' || $1, 'active')
ON CONFLICT (location_id) DO NOTHING`, park, testTenantID); err != nil {
			t.Fatalf("seed park %s: %v", park, err)
		}
	}
	for i, op := range []string{operatorA, operatorB} {
		park := []string{parkA, parkB}[i]
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3, 'Cascade Op', 'active', 'operator', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, op, testTenantID, "OP-PUB-CAP-"+string(rune('A'+i)), park); err != nil {
			t.Fatalf("seed operator %d: %v", i, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_operator_assignment_config (tenant_id, park_id, active_operators_per_day, default_operator_id, row_version)
VALUES ($1::uuid, $2::uuid, 1, $3::uuid, 1)
ON CONFLICT (tenant_id, park_id) DO NOTHING`, testTenantID, park, op); err != nil {
			t.Fatalf("seed operator-assignment config for park %d: %v", i, err)
		}
	}

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.capacity.publishcascade", Name: "Capacity Publish Cascade",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	capacity := domain.PublishedCapacity{
		MaxPerDay: 175, CapacityScope: "tenant", MaxBufferDays: 5,
		OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap",
	}
	if err := repo.PublishVersionWithCapacity(ctx, testTenantID, versionID, nil, capacity, "capacity-publish-cascade-key"); err != nil {
		t.Fatalf("publish with capacity: %v", err)
	}

	for _, park := range []string{parkA, parkB} {
		n := countRows(ctx, t, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'vaccination.capacity.changed' AND aggregate_id = $2::uuid`,
			testTenantID, park)
		if n != 1 {
			t.Fatalf("vaccination.capacity.changed outbox rows for configured park %s = %d, want 1", park, n)
		}
	}
	n := countRows(ctx, t, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'vaccination.capacity.changed' AND aggregate_id = $2::uuid`,
		testTenantID, parkUnconfigured)
	if n != 0 {
		t.Fatalf("vaccination.capacity.changed outbox rows for unconfigured park = %d, want 0", n)
	}
}

// TestPublishVersionWithCapacityRollsBackOnSyncFailure is the atomicity regression guard for the
// publish/capacity-sync split-transaction bug: when the in-transaction capacity sync FAILS, the whole
// publish must roll back — the version stays draft, NO publish side effect (outbox event) is emitted,
// and the operational vaccination_capacity_config row is left untouched. A rule must never become
// "published" while its capacity did not save.
func TestPublishVersionWithCapacityRollsBackOnSyncFailure(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.capacity.rollback", Name: "Capacity Rollback Guard",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	// Capture the FULL operational capacity row BEFORE the failed publish (baseline tenant is seeded).
	var perDayBefore, bufBefore, rowVerBefore int
	var scopeBefore, policyBefore string
	if err := pool.QueryRow(ctx,
		`SELECT max_per_day, max_buffer_days, capacity_scope, overflow_policy, row_version
		 FROM vaccination_capacity_config WHERE tenant_id = $1::uuid`,
		testTenantID).Scan(&perDayBefore, &bufBefore, &scopeBefore, &policyBefore, &rowVerBefore); err != nil {
		t.Fatalf("read capacity before: %v", err)
	}

	// An invalid overflow_policy violates the vaccination_capacity_config CHECK constraint, so the in-tx
	// capacity upsert errors — exactly the "capacity did not save" case the publish must not survive.
	badCapacity := domain.PublishedCapacity{
		MaxPerDay: 50, CapacityScope: "tenant", MaxBufferDays: 5, OverflowPolicy: "not-a-real-policy",
	}
	if err := repo.PublishVersionWithCapacity(ctx, testTenantID, versionID, nil, badCapacity, "capacity-rollback-key"); err == nil {
		t.Fatalf("publish with a failing capacity sync must return an error, got nil")
	}

	// The version must NOT be published.
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM protocol_versions WHERE tenant_id = $1 AND protocol_version_id = $2::uuid`,
		testTenantID, versionID).Scan(&status); err != nil {
		t.Fatalf("read version status: %v", err)
	}
	if status != "draft" {
		t.Fatalf("failed capacity sync must leave the version draft, got status=%q", status)
	}

	// No publish side effect: no outbox event was emitted for this version.
	var outboxCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id = $1 AND aggregate_id = $2::uuid`,
		testTenantID, versionID).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 0 {
		t.Fatalf("failed capacity sync must emit no outbox event, got %d", outboxCount)
	}

	// No publish side effect: no audit row for this version's publish.
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND resource_id = $2::uuid AND action = 'protocol.version.published'`,
		testTenantID, versionID); n != 0 {
		t.Fatalf("failed capacity sync must write no publish audit row, got %d", n)
	}

	// No publish side effect: no publish idempotency reservation survives the rollback.
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM idempotency_keys WHERE tenant_id = $1 AND scope = 'protocol.version.publish'`,
		testTenantID); n != 0 {
		t.Fatalf("failed capacity sync must leave no publish idempotency reservation, got %d", n)
	}

	// The FULL operational capacity row must be untouched by the rolled-back write.
	var perDayAfter, bufAfter, rowVerAfter int
	var scopeAfter, policyAfter string
	if err := pool.QueryRow(ctx,
		`SELECT max_per_day, max_buffer_days, capacity_scope, overflow_policy, row_version
		 FROM vaccination_capacity_config WHERE tenant_id = $1::uuid`,
		testTenantID).Scan(&perDayAfter, &bufAfter, &scopeAfter, &policyAfter, &rowVerAfter); err != nil {
		t.Fatalf("read capacity after: %v", err)
	}
	if perDayAfter != perDayBefore || bufAfter != bufBefore || scopeAfter != scopeBefore ||
		policyAfter != policyBefore || rowVerAfter != rowVerBefore {
		t.Fatalf("failed capacity sync must not mutate the operational row: before=%d/%d/%q/%q/v%d after=%d/%d/%q/%q/v%d",
			perDayBefore, bufBefore, scopeBefore, policyBefore, rowVerBefore,
			perDayAfter, bufAfter, scopeAfter, policyAfter, rowVerAfter)
	}
}

// countRows scans a single COUNT(*) query, failing the test on error. Shared by the capacity-publish
// rollback guards to keep the "no side effect" assertions terse.
func countRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

// TestPublishVersionWithDerivedRulesRollsBackOnCapacityFailure is the atomicity regression guard for the
// vaccination.matrix publish path, which carries MORE in-transaction side effects than the plain path —
// derived protocol_rules + protocol_rule_dimensions, retiring overlapping published versions, audit, and
// outbox — all BEFORE the capacity upsert. A failing capacity sync must roll every one of them back: the
// version stays draft, no derived rules/dimensions persist, no outbox event, no audit row, no idempotency
// reservation, and the operational capacity row is untouched.
func TestPublishVersionWithDerivedRulesRollsBackOnCapacityFailure(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.matrix.capacity.rollback", Name: "Matrix Capacity Rollback Guard",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       vaccinationMatrixRuleDSL(), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	rules, dims := derivedMatrixRows(versionID, "20000000-0000-4000-8000-000000000001", "et_tt")

	var perDayBefore, bufBefore, rowVerBefore int
	var scopeBefore, policyBefore string
	if err := pool.QueryRow(ctx,
		`SELECT max_per_day, max_buffer_days, capacity_scope, overflow_policy, row_version
		 FROM vaccination_capacity_config WHERE tenant_id = $1::uuid`,
		testTenantID).Scan(&perDayBefore, &bufBefore, &scopeBefore, &policyBefore, &rowVerBefore); err != nil {
		t.Fatalf("read capacity before: %v", err)
	}

	// Invalid overflow_policy → the in-tx capacity upsert violates the CHECK and fails the matrix publish.
	badCapacity := &domain.PublishedCapacity{
		MaxPerDay: 50, CapacityScope: "tenant", MaxBufferDays: 5, OverflowPolicy: "not-a-real-policy",
	}
	err = repo.PublishVersionWithDerivedRules(ctx, testTenantID, domain.Version{
		ProtocolVersionID: versionID,
		ProtocolID:        protocolID,
		ScopeType:         "tenant",
		Status:            "draft",
		RuleDsl:           vaccinationMatrixRuleDSL(),
	}, rules, dims, nil, badCapacity, "", "matrix-capacity-rollback-key")
	if err == nil {
		t.Fatalf("matrix publish with a failing capacity sync must return an error, got nil")
	}

	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM protocol_versions WHERE tenant_id = $1 AND protocol_version_id = $2::uuid`,
		testTenantID, versionID).Scan(&status); err != nil {
		t.Fatalf("read version status: %v", err)
	}
	if status != "draft" {
		t.Fatalf("failed capacity sync must leave the matrix version draft, got status=%q", status)
	}

	// Every matrix side effect must have rolled back.
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM protocol_rules WHERE tenant_id = $1 AND protocol_version_id = $2::uuid`,
		testTenantID, versionID); n != 0 {
		t.Fatalf("failed capacity sync must persist no derived rules, got %d", n)
	}
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM protocol_rule_dimensions WHERE tenant_id = $1 AND protocol_version_id = $2::uuid`,
		testTenantID, versionID); n != 0 {
		t.Fatalf("failed capacity sync must persist no rule dimensions, got %d", n)
	}
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id = $1 AND aggregate_id = $2::uuid`,
		testTenantID, versionID); n != 0 {
		t.Fatalf("failed capacity sync must emit no outbox event, got %d", n)
	}
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND resource_id = $2::uuid AND action = 'protocol.version.published'`,
		testTenantID, versionID); n != 0 {
		t.Fatalf("failed capacity sync must write no publish audit row, got %d", n)
	}
	if n := countRows(ctx, t, pool,
		`SELECT count(*) FROM idempotency_keys WHERE tenant_id = $1 AND scope = 'protocol.version.publish'`,
		testTenantID); n != 0 {
		t.Fatalf("failed capacity sync must leave no publish idempotency reservation, got %d", n)
	}

	var perDayAfter, bufAfter, rowVerAfter int
	var scopeAfter, policyAfter string
	if err := pool.QueryRow(ctx,
		`SELECT max_per_day, max_buffer_days, capacity_scope, overflow_policy, row_version
		 FROM vaccination_capacity_config WHERE tenant_id = $1::uuid`,
		testTenantID).Scan(&perDayAfter, &bufAfter, &scopeAfter, &policyAfter, &rowVerAfter); err != nil {
		t.Fatalf("read capacity after: %v", err)
	}
	if perDayAfter != perDayBefore || bufAfter != bufBefore || scopeAfter != scopeBefore ||
		policyAfter != policyBefore || rowVerAfter != rowVerBefore {
		t.Fatalf("failed capacity sync must not mutate the operational row: before=%d/%d/%q/%q/v%d after=%d/%d/%q/%q/v%d",
			perDayBefore, bufBefore, scopeBefore, policyBefore, rowVerBefore,
			perDayAfter, bufAfter, scopeAfter, policyAfter, rowVerAfter)
	}
}

// TestPublishVersionWithCapacityEnqueuesCapacityChangedForParkWithFutureWorkOnly reproduces the
// fallback/no-config gap: a park can have FUTURE planned vaccination work (an obligation batch +
// drive assignments) without any vaccination_operator_assignment_config row (fallback/default
// planning). Fanning the tenant-wide capacity change out only from
// vaccination_operator_assignment_config skips such a park, leaving its already-planned future
// drive rows stale against the new capacity policy. A park with NEITHER a config NOR future work
// must still get no event.
func TestPublishVersionWithCapacityEnqueuesCapacityChangedForParkWithFutureWorkOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const parkFuture = "00000000-0000-4000-8000-0000000cc001"
	const parkPast = "00000000-0000-4000-8000-0000000cc002"
	const parkIdle = "00000000-0000-4000-8000-0000000cc003"

	for _, park := range []string{parkFuture, parkPast, parkIdle} {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $1, 'Park ' || $1, 'active')
ON CONFLICT (location_id) DO NOTHING`, park, testTenantID); err != nil {
			t.Fatalf("seed park %s: %v", park, err)
		}
	}

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.capacity.futurework", Name: "Capacity Future Work Cascade",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	today := biztime.BusinessDayStart(time.Now())
	future := today.AddDate(0, 0, 3).Format("2006-01-02")
	past := today.AddDate(0, 0, -30).Format("2006-01-02")

	seedDrive := func(park, plannedDate, batchID string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, planned_date, status, estimated_targets)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, $5::date, 'planned', 10)`,
			batchID, testTenantID, versionID, park, plannedDate); err != nil {
			t.Fatalf("seed batch for %s: %v", park, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, physical_shed, partition_label, animal_count)
VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, 'S1', 'whole', 10)`,
			testTenantID, batchID, plannedDate, park); err != nil {
			t.Fatalf("seed drive assignment for %s: %v", park, err)
		}
	}
	seedDrive(parkFuture, future, "00000000-0000-4000-8000-0000000cc0b1")
	seedDrive(parkPast, past, "00000000-0000-4000-8000-0000000cc0b2")

	capacity := domain.PublishedCapacity{
		MaxPerDay: 181, CapacityScope: "tenant", MaxBufferDays: 4,
		OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap",
	}
	if err := repo.PublishVersionWithCapacity(ctx, testTenantID, versionID, nil, capacity, "capacity-future-work-key"); err != nil {
		t.Fatalf("publish with capacity: %v", err)
	}

	countFor := func(park string) int {
		return countRows(ctx, t, pool, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'vaccination.capacity.changed' AND aggregate_id = $2::uuid`,
			testTenantID, park)
	}
	if n := countFor(parkFuture); n != 1 {
		t.Fatalf("vaccination.capacity.changed rows for park with future work but no operator config = %d, want 1", n)
	}
	if n := countFor(parkPast); n != 0 {
		t.Fatalf("vaccination.capacity.changed rows for park with only PAST work = %d, want 0", n)
	}
	if n := countFor(parkIdle); n != 0 {
		t.Fatalf("vaccination.capacity.changed rows for idle park = %d, want 0", n)
	}

	// Replay must stay idempotent: republishing the same version must not duplicate the fan-out.
	if err := repo.PublishVersionWithCapacity(ctx, testTenantID, versionID, nil, capacity, "capacity-future-work-key"); err != nil {
		t.Fatalf("republish with capacity: %v", err)
	}
	if n := countFor(parkFuture); n != 1 {
		t.Fatalf("after replay, vaccination.capacity.changed rows for park with future work = %d, want 1", n)
	}
}

// TestDiscardVersionRemovesADraftThatHasRules exercises the case every fake-backed
// test missed: a draft with rules attached.
//
// protocol_rules_version_tenant_fk carries no ON DELETE CASCADE, so the original
// single-statement delete raised a foreign-key violation and the endpoint answered
// 500 for exactly the drafts anyone would want to discard -- an authored one. Only a
// real database can catch that, which is why this test is here rather than beside the
// handler tests.
func TestDiscardVersionRemovesADraftThatHasRules(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	actorID := "90000000-0000-4000-8000-000000000101"

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.discard.rules", Name: "Discard With Rules",
		Category: "vaccination", Status: "draft", CreatedBy: &actorID, IdempotencyKey: "discard-def-1",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"ruleset_family":"vaccination.matrix"}`),
		ProofPolicy:   []byte(`{"required_proofs":["video"]}`),
		DraftedBy:     &actorID, IdempotencyKey: "discard-version-1",
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	if _, err := repo.CreateRule(ctx, domain.NewRule{
		TenantID: testTenantID, ProtocolVersionID: versionID, DoseCode: "discard_probe", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{}`), IdempotencyKey: "discard-rule-1",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	var rules int
	if err := pool.QueryRow(ctx, `select count(*) from protocol_rules where protocol_version_id = $1`, versionID).Scan(&rules); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if rules != 1 {
		t.Fatalf("rules before discard = %d, want 1", rules)
	}

	if err := repo.DiscardVersion(ctx, testTenantID, versionID); err != nil {
		t.Fatalf("discard a draft that has rules: %v", err)
	}

	var versions int
	if err := pool.QueryRow(ctx, `select count(*) from protocol_versions where protocol_version_id = $1`, versionID).Scan(&versions); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if versions != 0 {
		t.Fatalf("version rows after discard = %d, want 0", versions)
	}
	if err := pool.QueryRow(ctx, `select count(*) from protocol_rules where protocol_version_id = $1`, versionID).Scan(&rules); err != nil {
		t.Fatalf("count rules after: %v", err)
	}
	if rules != 0 {
		t.Fatalf("rule rows after discard = %d, want 0", rules)
	}
}

// TestDiscardVersionRefusesAPublishedVersionAndKeepsItsRules proves the draft-only
// predicate holds in SQL: the published row AND its rules survive untouched, so no
// caller can erase what a farm was told to do.
func TestDiscardVersionRefusesAPublishedVersionAndKeepsItsRules(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	actorID := "90000000-0000-4000-8000-000000000101"

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.discard.published", Name: "Discard Published",
		Category: "vaccination", Status: "draft", CreatedBy: &actorID, IdempotencyKey: "discard-def-2",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	versionID, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"ruleset_family":"vaccination.matrix"}`),
		ProofPolicy:   []byte(`{"required_proofs":["video"]}`),
		DraftedBy:     &actorID, IdempotencyKey: "discard-version-2",
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	if _, err := repo.CreateRule(ctx, domain.NewRule{
		TenantID: testTenantID, ProtocolVersionID: versionID, DoseCode: "keep_me", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{}`), IdempotencyKey: "discard-rule-2",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := repo.PublishVersion(ctx, testTenantID, versionID, &actorID, "discard-publish-2"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if err := repo.DiscardVersion(ctx, testTenantID, versionID); !errors.Is(err, ports.ErrVersionNotDraft) {
		t.Fatalf("discard published err = %v, want ErrVersionNotDraft", err)
	}

	var versions, rules int
	if err := pool.QueryRow(ctx, `select count(*) from protocol_versions where protocol_version_id = $1`, versionID).Scan(&versions); err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from protocol_rules where protocol_version_id = $1`, versionID).Scan(&rules); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if versions != 1 || rules != 1 {
		t.Fatalf("after a refused discard: versions=%d rules=%d, want 1 and 1", versions, rules)
	}
}

// Saving an edited plan is a REPLACE: the draft on screen becomes a new draft and the old row
// goes. Both halves have to happen in one transaction, because one draft per plan is enforced
// in the database -- create-then-discard is refused outright, and discard-then-create destroys
// the farm's work whenever the create then fails.
func TestReplaceDraftVersionSwapsTheDraftAndIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	actorID := "90000000-0000-4000-8000-000000000101"

	protocolID, err := repo.CreateDefinition(ctx, domain.NewDefinition{
		TenantID: testTenantID, Code: "vaccination.replace.draft", Name: "Replace Draft",
		Category: "vaccination", Status: "draft", CreatedBy: &actorID, IdempotencyKey: "replace-def-1",
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	original, err := repo.CreateVersion(ctx, domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"ruleset_family":"vaccination.matrix"}`),
		ProofPolicy:   []byte(`{"required_proofs":["video"]}`),
		DraftedBy:     &actorID, IdempotencyKey: "replace-version-1",
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	if _, err := repo.CreateRule(ctx, domain.NewRule{
		TenantID: testTenantID, ProtocolVersionID: original, DoseCode: "replace_probe", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, Repeat: "none", CatchUp: "immediate",
		EligibilityJSON: []byte(`{}`), IdempotencyKey: "replace-rule-1",
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}

	edited := domain.NewVersion{
		TenantID: testTenantID, ProtocolID: protocolID, ScopeType: "tenant", Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"ruleset_family":"vaccination.matrix","edited":true}`),
		ProofPolicy:   []byte(`{"required_proofs":["video"]}`),
		DraftedBy:     &actorID, IdempotencyKey: "replace-swap-1",
	}
	replacement, err := repo.ReplaceDraftVersion(ctx, edited, original)
	if err != nil {
		t.Fatalf("replace draft: %v", err)
	}
	if replacement == original {
		t.Fatal("replace returned the same version id")
	}

	// Exactly one draft, and it is the replacement. Anything else means the invariant this
	// call exists to satisfy was broken by the call itself.
	var drafts int
	var draftID string
	if err := pool.QueryRow(ctx, `
SELECT count(*), coalesce(max(protocol_version_id::text), '')
FROM protocol_versions
WHERE tenant_id = $1::uuid AND protocol_id = $2::uuid AND status = 'draft'`,
		testTenantID, protocolID).Scan(&drafts, &draftID); err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if drafts != 1 || draftID != replacement {
		t.Fatalf("drafts = %d (id %s), want exactly the replacement %s", drafts, draftID, replacement)
	}
	// The old draft's rules went with it: they described a plan that no longer exists.
	var orphanRules int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM protocol_rules WHERE protocol_version_id = $1::uuid`, original).Scan(&orphanRules); err != nil {
		t.Fatalf("count orphan rules: %v", err)
	}
	if orphanRules != 0 {
		t.Fatalf("old draft left %d rules behind", orphanRules)
	}

	// A retry cannot tell whether the first attempt committed -- the id it holds is already
	// gone. Replaying the same key must return the same replacement, not fail.
	replayed, err := repo.ReplaceDraftVersion(ctx, edited, original)
	if err != nil {
		t.Fatalf("replay replace: %v", err)
	}
	if replayed != replacement {
		t.Fatalf("replay returned %s, want the original replacement %s", replayed, replacement)
	}

	// And a replace aimed at something that is not a draft is refused rather than quietly
	// creating a second one.
	if _, err := pool.Exec(ctx,
		`UPDATE protocol_versions SET status = 'published' WHERE protocol_version_id = $1::uuid`, replacement); err != nil {
		t.Fatalf("publish replacement: %v", err)
	}
	edited.IdempotencyKey = "replace-swap-2"
	if _, err := repo.ReplaceDraftVersion(ctx, edited, replacement); !errors.Is(err, ports.ErrVersionNotDraft) {
		t.Fatalf("replace of a published version err = %v, want ErrVersionNotDraft", err)
	}
}
