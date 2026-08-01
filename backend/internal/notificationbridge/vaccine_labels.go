package notificationbridge

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VaccineLabelResolver resolves human-readable vaccine labels (e.g., "ET+TT", "PPR · Booster")
// for a small, bounded set of vaccination obligation references. Like LocationNameResolver,
// it performs ONE batched query, never per-notification lookup.
//
// This enrichment is optional: a transient lookup failure or missing obligation degrades
// the push copy (falls back to generic wording) rather than blocking notification delivery.
//
// Canonical source for label formatting: backend/internal/vaccinationexecution/domain/labels.go
// (VaccinationDoseDisplayLabel function).
type VaccineLabelResolver struct {
	pool *pgxpool.Pool
}

// NewVaccineLabelResolver builds a resolver over the shared connection pool. A nil pool is accepted:
// ResolveVaccineLabels then always returns an empty map rather than panicking.
func NewVaccineLabelResolver(pool *pgxpool.Pool) *VaccineLabelResolver {
	return &VaccineLabelResolver{pool: pool}
}

// ResolveVaccineLabels returns vaccination_rules.id -> human-readable vaccine label for every rule_id
// in ruleIDs that exists for tenantID, via ONE query -- never one lookup per rule, never per notification.
// Call sites pass the small deduped set of rule IDs a single business event actually needs.
//
// Errors are swallowed to an empty map: a transient lookup failure degrades the copy (falls back to
// the generic wording) rather than blocking the notification.
func (r *VaccineLabelResolver) ResolveVaccineLabels(ctx context.Context, tenantID string, ruleIDs ...string) map[string]string {
	out := map[string]string{}
	if r == nil || r.pool == nil || strings.TrimSpace(tenantID) == "" {
		return out
	}
	seen := make(map[string]bool, len(ruleIDs))
	clean := make([]string, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		clean = append(clean, id)
	}
	if len(clean) == 0 {
		return out
	}
	rows, err := r.pool.Query(ctx, `
SELECT rule_id::text, vaccine_label
FROM vaccination_rules
WHERE tenant_id = $1::uuid AND rule_id = ANY($2::uuid[])`, tenantID, clean)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			continue
		}
		out[id] = label
	}
	return out
}

// vaccineLabelOrFallback renders a resolved human label, or a neutral fallback ("vaccination")
// when the lookup is unavailable/empty -- never a raw code and never a blank segment in the copy.
func vaccineLabelOrFallback(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "vaccination"
	}
	return label
}
