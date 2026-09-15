package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

const rulesTenant = "7f000000-0000-4000-8000-0000000000ee"

// The rules source is the ONLY place that names sop_versions on weighing's behalf. Proved on a
// migrated database: migration 000314 leaves every tenant's published weighing.session version
// carrying the seeded section (the pre-SOP behaviour); a tenant created AFTER it runs the seeded
// rules (version 0); publishing v2 with a different removal mode is read as the published rules
// on the very next call while v1 stays readable by number for the tasks pinned to it; and a
// version the farm never published is refused by name.
func TestRulesSourceReadsPublishedAndPinnedVersions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	src := NewRulesSource(pool, 5*time.Second)

	// 000314: no published weighing.session version lacks the section.
	var lacking int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sop_versions v
JOIN sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE d.code = 'weighing.session' AND v.status = 'published' AND NOT (v.form_dsl ? 'weighing')`).Scan(&lacking); err != nil {
		t.Fatalf("count versions lacking the section: %v", err)
	}
	if lacking != 0 {
		t.Fatalf("migration 000314 left %d published weighing.session version(s) without the weighing section", lacking)
	}

	// A tenant created after the migration: no version, the seeded rules, version 0.
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Rules Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, rulesTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	rules, err := src.PublishedRules(ctx, rulesTenant)
	if err != nil {
		t.Fatalf("published rules on an unauthored tenant: %v", err)
	}
	if rules.Version != 0 || rules.FeedWaterRemoval.Mode != domain.RemovalModeRequired {
		t.Fatalf("unauthored tenant rules = v%d/%s, want the seed v0/required", rules.Version, rules.FeedWaterRemoval.Mode)
	}

	// Publish v1 = the seeded document (what 000314 does for an existing tenant), then v2 with
	// the removal optional and a question; v1 is retired.
	var sopID string
	if err := pool.QueryRow(ctx, `INSERT INTO sop_definitions (tenant_id, code, name, description, status)
VALUES ($1::uuid, 'weighing.session', 'Weighing Session', 'test', 'active') RETURNING sop_id::text`, rulesTenant).Scan(&sopID); err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	seed := domain.SeededWeighingSOPJSON()
	v1 := map[string]any{"schema_version": "goatos.sop-form.v1", "sop_code": "weighing.session", "title": "Weighing Session", "fields": []any{}, "rules": []any{}, "weighing": json.RawMessage(seed)}
	v1JSON, _ := json.Marshal(v1)
	if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES ($1::uuid, $2::uuid, 1, 'v1', 'retired', $3::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid": true}'::jsonb, now())`, rulesTenant, sopID, v1JSON); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	var dsl domain.WeighingSOP
	if err := json.Unmarshal(seed, &dsl); err != nil {
		t.Fatal(err)
	}
	dsl.FeedWaterRemoval.Mode = domain.RemovalModeOptional
	dsl.FeedWaterRemoval.Questions = []domain.SOPQuestion{{ID: "all_pens", Kind: domain.SOPQuestionChoice, Title: "Every pen emptied?", Required: true, Options: []domain.SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}
	dsl.Capture.LumpSum.VideoMax = 3
	v2 := map[string]any{"schema_version": "goatos.sop-form.v1", "sop_code": "weighing.session", "title": "Weighing Session", "fields": []any{}, "rules": []any{}, "weighing": dsl}
	v2JSON, _ := json.Marshal(v2)
	if _, err := pool.Exec(ctx, `INSERT INTO sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES ($1::uuid, $2::uuid, 2, 'v2', 'published', $3::jsonb, '{}'::jsonb, '{}'::jsonb, '{"valid": true}'::jsonb, now())`, rulesTenant, sopID, v2JSON); err != nil {
		t.Fatalf("seed v2: %v", err)
	}

	published, err := src.PublishedRules(ctx, rulesTenant)
	if err != nil {
		t.Fatalf("published rules: %v", err)
	}
	if published.Version != 2 || published.FeedWaterRemoval.Mode != domain.RemovalModeOptional || len(published.FeedWaterRemoval.Questions) != 1 || published.Capture.LumpSum.VideoMax != 3 {
		t.Fatalf("published = v%d/%s/%d questions/max %d, want v2/optional/1/3", published.Version, published.FeedWaterRemoval.Mode, len(published.FeedWaterRemoval.Questions), published.Capture.LumpSum.VideoMax)
	}
	// A task pinned to v1 still runs on v1 (retired is still readable by number).
	pinned, err := src.RulesVersion(ctx, rulesTenant, 1)
	if err != nil {
		t.Fatalf("pinned v1: %v", err)
	}
	if pinned.Version != 1 || pinned.FeedWaterRemoval.Mode != domain.RemovalModeRequired || pinned.Capture.LumpSum.VideoMax != domain.MaxShedProofArtifacts {
		t.Fatalf("pinned v1 = v%d/%s/max %d, want v1/required/%d", pinned.Version, pinned.FeedWaterRemoval.Mode, pinned.Capture.LumpSum.VideoMax, domain.MaxShedProofArtifacts)
	}
	if _, err := src.RulesVersion(ctx, rulesTenant, 9); !errors.Is(err, ports.ErrSOPVersionUnknown) {
		t.Fatalf("unpublished version err = %v, want ErrSOPVersionUnknown", err)
	}
	if zero, err := src.RulesVersion(ctx, rulesTenant, 0); err != nil || zero.Version != 0 {
		t.Fatalf("version 0 = v%d err %v, want the seed", zero.Version, err)
	}
}
