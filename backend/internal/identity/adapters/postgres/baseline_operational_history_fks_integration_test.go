package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestBaselineOperationalHistoryForeignKeys guards the clean-slate baseline:
// operational history/audit tables must keep their parent FKs and reject orphan rows.
func TestBaselineOperationalHistoryForeignKeys(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, _ := startCorrectionWriteDB(t, ctx)
	defer pool.Close()

	expected := map[string]string{
		"goat_identity_events_tenant_id_fkey":           "FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id)",
		"goat_identity_events_goat_id_fkey":             "FOREIGN KEY (goat_id) REFERENCES goats(goat_id)",
		"goat_identity_events_decision_id_fkey":         "FOREIGN KEY (decision_id) REFERENCES identity_decisions(decision_id)",
		"goat_identity_events_goat_tenant_fk":           "FOREIGN KEY (tenant_id, goat_id) REFERENCES goats(tenant_id, goat_id)",
		"goat_identity_events_decision_tenant_fk":       "FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id)",
		"audit_log_tenant_id_fkey":                      "FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id)",
		"audit_log_decision_id_fkey":                    "FOREIGN KEY (decision_id) REFERENCES identity_decisions(decision_id)",
		"audit_log_decision_tenant_fk":                  "FOREIGN KEY (tenant_id, decision_id) REFERENCES identity_decisions(tenant_id, decision_id)",
		"obligation_status_events_tenant_id_fkey":       "FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id)",
		"obligation_status_events_obligation_tenant_fk": "FOREIGN KEY (tenant_id, obligation_id) REFERENCES obligation_instances(tenant_id, obligation_id)",
	}

	for name, want := range expected {
		var definition string
		var valid bool
		if err := pool.QueryRow(ctx, `
			SELECT pg_get_constraintdef(oid), convalidated
			FROM pg_constraint
			WHERE conname = $1`, name).Scan(&definition, &valid); err != nil {
			t.Fatalf("missing restored constraint %s: %v", name, err)
		}
		if !valid {
			t.Fatalf("restored constraint %s remains NOT VALID", name)
		}
		if normalizeConstraintDef(definition) != normalizeConstraintDef(want) {
			t.Fatalf("constraint %s definition = %q, want %q", name, definition, want)
		}
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO goat_identity_events
		  (identity_event_id, tenant_id, goat_id, event_type, event_version,
		   occurred_at, recorded_at, payload, decision_id, idempotency_key)
		VALUES
		  (gen_random_uuid(), $1, '10000000-0000-4000-8000-000000000001',
		   'guard.invalid-decision', 1, now(), now(), '{}'::jsonb,
		   '70000000-0000-4000-8000-000000000099', 'baseline-invalid-decision')`, meshaTenant); err == nil {
		t.Fatal("orphaned goat_identity_events decision row was accepted")
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO audit_log
		  (audit_id, tenant_id, actor_type, action, resource_type, decision_id, metadata)
		VALUES
		  (gen_random_uuid(), $1, 'guard', 'invalid-decision', 'audit',
		   '70000000-0000-4000-8000-000000000099', '{}'::jsonb)`, meshaTenant); err == nil {
		t.Fatal("orphaned audit_log decision row was accepted")
	}
}

func normalizeConstraintDef(definition string) string {
	return strings.Join(strings.Fields(strings.ToLower(definition)), " ")
}
