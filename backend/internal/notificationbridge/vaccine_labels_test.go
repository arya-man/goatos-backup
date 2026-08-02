package notificationbridge_test

// Real-Postgres test for VaccineLabelResolver (C19b/C19c). Proves the resolver reads the REAL
// protocol schema (protocol_rules -> protocol_versions -> protocol_definitions), not the
// nonexistent `vaccination_rules` table the resolver used to query, and derives the label via the
// same canonical formatter every other module uses rather than a fictitious vaccine_label column.

import (
	"context"
	"log/slog"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	vlTenant      = "fb000000-0000-4000-8000-0000000000c1"
	vlProtocolID  = "fb000000-0000-4000-8000-0000000000c2"
	vlVersionID   = "fb000000-0000-4000-8000-0000000000c3"
	vlRuleETTT    = "fb000000-0000-4000-8000-0000000000c4"
	vlRulePPR     = "fb000000-0000-4000-8000-0000000000c5"
	vlOtherTenant = "fb000000-0000-4000-8000-0000000000c6"
	vlUnknownRule = "fb000000-0000-4000-8000-0000000000c9"
)

// TestVaccineLabelResolver_ResolvesRealProtocolSchema seeds real protocol_definitions /
// protocol_versions / protocol_rules rows and asserts ResolveVaccineLabels returns the derived
// human label (not a raw dose_code, not empty) for each real rule id, nothing for an unknown rule
// id, and nothing across a tenant boundary.
func TestVaccineLabelResolver_ResolvesRealProtocolSchema(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)

	exec(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'VLabel Test Tenant', 'active')`,
		vlTenant)
	exec(t, ctx, pool, "other tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'VLabel Other Tenant', 'active')`,
		vlOtherTenant)
	exec(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination.vlabel_test', 'PPR Protocol', 'vaccination', 'active')`,
		vlProtocolID, vlTenant)
	exec(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (
		   protocol_version_id, tenant_id, protocol_id, scope_type, version, version_label,
		   status, effective_from, rule_dsl, proof_policy
		 ) VALUES ($1, $2, $3, 'tenant', 1, 'v1', 'draft', now()::date, '{}'::jsonb, '{}'::jsonb)`,
		vlVersionID, vlTenant, vlProtocolID)
	exec(t, ctx, pool, "rule ET_TT",
		`INSERT INTO protocol_rules (
		   rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
		   offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, proof_policy, sort_order
		 ) VALUES ($1, $2, $3, 'ET_TT_7W', 1, 'calendar', 0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{}'::jsonb, 1)`,
		vlRuleETTT, vlTenant, vlVersionID)
	exec(t, ctx, pool, "rule PPR",
		`INSERT INTO protocol_rules (
		   rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
		   offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, proof_policy, sort_order
		 ) VALUES ($1, $2, $3, 'PPR_FIRST', 2, 'calendar', 0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{}'::jsonb, 2)`,
		vlRulePPR, vlTenant, vlVersionID)

	exec(t, ctx, pool, "publish version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE tenant_id = $1 AND protocol_version_id = $2`,
		vlTenant, vlVersionID)

	resolver := notificationbridge.NewVaccineLabelResolver(pool, slog.Default())

	labels := resolver.ResolveVaccineLabels(ctx, vlTenant, vlRuleETTT, vlRulePPR, vlUnknownRule)

	if got, want := labels[vlRuleETTT], "ET+TT"; got != want {
		t.Errorf("ET_TT_7W label = %q, want %q", got, want)
	}
	if label := labels[vlRulePPR]; label == "" || label == "PPR_FIRST" {
		t.Errorf("PPR_FIRST label = %q, want a derived human label (non-empty, not the raw dose_code)", label)
	}
	if _, ok := labels[vlUnknownRule]; ok {
		t.Errorf("unknown rule id must not resolve to any label, got %q", labels[vlUnknownRule])
	}

	// Tenant isolation: the same rule id under a different tenant must not leak across.
	crossTenant := resolver.ResolveVaccineLabels(ctx, vlOtherTenant, vlRuleETTT)
	if _, ok := crossTenant[vlRuleETTT]; ok {
		t.Errorf("rule id resolved across tenant boundary: got %q", crossTenant[vlRuleETTT])
	}

	// Nil pool / empty tenant / no rule ids must degrade to an empty map, never panic.
	var nilResolver *notificationbridge.VaccineLabelResolver
	if out := nilResolver.ResolveVaccineLabels(ctx, vlTenant, vlRuleETTT); len(out) != 0 {
		t.Errorf("nil resolver must return empty map, got %v", out)
	}
	if out := resolver.ResolveVaccineLabels(ctx, ""); len(out) != 0 {
		t.Errorf("empty tenant must return empty map, got %v", out)
	}
	if out := resolver.ResolveVaccineLabels(ctx, vlTenant); len(out) != 0 {
		t.Errorf("no rule ids must return empty map, got %v", out)
	}

}
