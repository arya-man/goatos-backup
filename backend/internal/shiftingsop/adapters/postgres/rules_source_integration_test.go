package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

const rulesTenant = "7f000000-0000-4000-8000-0000000000ec"

// The rules source is the ONLY place that names sop_versions on counts' behalf. A tenant with no
// version runs the seed (v0); a published v2 is read on the very next call while v1 stays readable
// by number for the movements pinned to it; a batch read resolves each version once; an
// unpublished version is refused by name.
func TestRulesSourcePublishedVersionAndSeedFallback(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	src := NewRulesSource(pool, 5*time.Second)

	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Shifting Rules Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, rulesTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	rules, err := src.PublishedRules(ctx, rulesTenant)
	if err != nil || rules.Version != 0 || len(rules.CompletionSlots("high")) != 3 {
		t.Fatalf("unauthored tenant = v%d err=%v, want the seed", rules.Version, err)
	}

	var sopID string
	if err := pool.QueryRow(ctx, `INSERT INTO sop_definitions (tenant_id, code, name, description, status)
VALUES ($1::uuid, 'shifting', 'Shifting', 'test', 'active') RETURNING sop_id::text`, rulesTenant).Scan(&sopID); err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	seed := domain.SeededShiftingSOPJSON()
	v1 := map[string]any{"schema_version": "goatos.sop-form.v1", "sop_code": "shifting", "title": "Shifting", "fields": []any{}, "rules": []any{}, "shifting": json.RawMessage(seed)}
	v1JSON, _ := json.Marshal(v1)
	if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES ($1::uuid, $2::uuid, 1, 'v1', 'retired', $3::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid": true}'::jsonb, now())`, rulesTenant, sopID, v1JSON); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	var dsl domain.ShiftingSOP
	if err := json.Unmarshal(seed, &dsl); err != nil {
		t.Fatal(err)
	}
	dsl.Raise.Questions = []authored.Question{{ID: "why", Kind: authored.QuestionText, Title: "Why move", Required: true}}
	v2 := map[string]any{"schema_version": "goatos.sop-form.v1", "sop_code": "shifting", "title": "Shifting", "fields": []any{}, "rules": []any{}, "shifting": dsl}
	v2JSON, _ := json.Marshal(v2)
	if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES ($1::uuid, $2::uuid, 2, 'v2', 'published', $3::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid": true}'::jsonb, now())`, rulesTenant, sopID, v2JSON); err != nil {
		t.Fatalf("seed v2: %v", err)
	}
	// A version predating the section reads the seed under its own number.
	v3 := map[string]any{"schema_version": "goatos.sop-form.v1", "sop_code": "shifting", "title": "Shifting", "fields": []any{}, "rules": []any{}}
	v3JSON, _ := json.Marshal(v3)
	if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES ($1::uuid, $2::uuid, 3, 'v3', 'retired', $3::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid": true}'::jsonb, now())`, rulesTenant, sopID, v3JSON); err != nil {
		t.Fatalf("seed v3: %v", err)
	}

	published, err := src.PublishedRules(ctx, rulesTenant)
	if err != nil || published.Version != 2 || len(published.Raise.Questions) != 1 {
		t.Fatalf("published = v%d/%d raise questions err=%v, want v2/1", published.Version, len(published.Raise.Questions), err)
	}
	pinned, err := src.RulesVersion(ctx, rulesTenant, 1)
	if err != nil || pinned.Version != 1 || len(pinned.Raise.Questions) != 0 {
		t.Fatalf("pinned v1 = %+v err=%v", pinned, err)
	}
	batch, err := src.RulesVersions(ctx, rulesTenant, []int{0, 1, 2, 3, 9})
	if err != nil || len(batch) != 4 || batch[3].Version != 3 || len(batch[3].CompletionSlots("low")) != 1 {
		t.Fatalf("batch = %+v err=%v", batch, err)
	}
	if _, err := src.RulesVersion(ctx, rulesTenant, 9); !errors.Is(err, ports.ErrShiftingSOPVersionUnknown) {
		t.Fatalf("unpublished err = %v", err)
	}
	// The cache answers a second read of an immutable version without touching the table.
	if _, err := pool.Exec(ctx, `DELETE FROM sop_versions WHERE tenant_id = $1::uuid AND version = 1`, rulesTenant); err != nil {
		t.Fatal(err)
	}
	if again, err := src.RulesVersion(ctx, rulesTenant, 1); err != nil || again.Version != 1 {
		t.Fatalf("cached v1 = %+v err=%v", again, err)
	}
}

// TestMigration000340AddsSectionInPlace: on a migrated database no published `shifting` version
// lacks the section, and every one that carries it validates.
func TestMigration000340AddsSectionInPlace(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	var lacking int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_versions v
JOIN sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE d.code = 'shifting' AND v.status = 'published' AND NOT (v.form_dsl ? 'shifting')`).Scan(&lacking); err != nil {
		t.Fatal(err)
	}
	if lacking != 0 {
		t.Fatalf("migration 000340 left %d published shifting version(s) without the section", lacking)
	}
	rows, err := pool.Query(ctx, `SELECT v.version, v.form_dsl FROM sop_versions v
JOIN sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE d.code = 'shifting' AND v.status = 'published'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var version int
		var raw []byte
		if err := rows.Scan(&version, &raw); err != nil {
			t.Fatal(err)
		}
		rules, err := parseRules(version, raw)
		if err != nil {
			t.Fatalf("v%d: %v", version, err)
		}
		if len(rules.CompletionSlots("high")) != 3 {
			t.Fatalf("v%d is not the seeded document: %+v", version, rules)
		}
	}
	// Columns exist and default to NULL (metadata-only ALTER).
	var nullable string
	if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'shifting_events' AND column_name = 'raise_capture_evidence'`).Scan(&nullable); err != nil || nullable != "YES" {
		t.Fatalf("raise_capture_evidence nullable=%q err=%v", nullable, err)
	}
}
