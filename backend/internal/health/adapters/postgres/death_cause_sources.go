package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// What a tenant's Health Config contributes to the cause-of-death list (maintainer decision
// 2026-09-25, docs/decisions/health-config-authoring.md -> "Cause of death").
//
// Both reads are TENANT-scoped and bounded by what a farm authors -- a handful of diagnosis
// types and a few dozen diseases -- and each rides an existing index: the published-register
// read the one-published-per-class unique index, the disease read the
// (tenant_id, disease_key, age_band, version) unique key.
const (
	// The problem rules of every PUBLISHED register whose diagnosis type is not retired --
	// exactly the documents the engine diagnoses from. Only the three fields the list needs
	// leave the database; the questions and clauses stay behind.
	//
	// A rule with no `kind` is a problem: the loader defaults it that way (applyRuleDefaults).
	sqlDeathCauseRegisterRules = `
SELECT r.animal_class,
       coalesce(nullif(btrim(r.document->>'register_version'), ''), r.register_label),
       coalesce(rule->>'id', ''),
       coalesce(rule->>'treats', '')
FROM health_diagnosis_register_versions r
CROSS JOIN LATERAL jsonb_array_elements(coalesce(r.document->'rules', '[]'::jsonb)) AS rule
WHERE r.tenant_id = $1::uuid
  AND r.status = 'published'
  AND coalesce(nullif(rule->>'kind', ''), 'problem') = 'problem'
  AND NOT EXISTS (
    SELECT 1 FROM health_diagnosis_types t
    WHERE t.tenant_id = r.tenant_id AND t.type_key = r.animal_class AND t.status = 'retired'
  )`

	// projection-review: membership=health_protocol_versions rows of this tenant, every status (a retired disease must keep its name on history); group_key=disease_key alone, the disease's stable identity -- one output row per disease across both age bands and every version; join_cardinality=no joins, so nothing can fan out, and the aggregates (array_agg ordered pick, bool_or) are over that one disease's own version rows; pagination=none, a tenant authors a few dozen diseases and the whole list is the death form's vocabulary; scope=tenant_id only, and "active" (bool_or published) is computed per disease, never across diseases
	// One row per disease ever authored on the treatment tab. ACTIVE means a version is
	// published today in either age band; the name is the published version's where there is
	// one, else the newest -- so a retired disease still reads under the name it last had.
	sqlDeathCauseAuthoredDiseases = `
SELECT disease_key,
       (array_agg(display_name ORDER BY (status = 'published') DESC, version DESC))[1],
       bool_or(status = 'published')
FROM health_protocol_versions
WHERE tenant_id = $1::uuid
GROUP BY disease_key`
)

// DeathCauseSources reads the tenant's authored diagnoses and diseases for the death form.
func (r *Repository) DeathCauseSources(ctx context.Context, tenantID string) (domain.DeathCauseTenantSources, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var out domain.DeathCauseTenantSources

	rulesBound := sqlbind.MustBind(sqlDeathCauseRegisterRules, tenantID)
	rows, err := r.pool.Query(ctx, rulesBound.SQL(), rulesBound.Args()...)
	if err != nil {
		return out, fmt.Errorf("health: read published register rules: %w", err)
	}
	seenVersion := map[string]struct{}{}
	for rows.Next() {
		var class, version, id, treats string
		if err := rows.Scan(&class, &version, &id, &treats); err != nil {
			rows.Close()
			return out, fmt.Errorf("health: scan published register rule: %w", err)
		}
		if _, ok := seenVersion[version]; !ok && strings.TrimSpace(version) != "" {
			seenVersion[version] = struct{}{}
			out.RegisterVersions = append(out.RegisterVersions, version)
		}
		out.RegisterRules = append(out.RegisterRules, domain.RegisterRule{
			ID:     strings.TrimSpace(id),
			Label:  domain.DeathCauseLabel(strings.TrimSpace(id)),
			Class:  class,
			Treats: strings.TrimSpace(treats),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("health: read published register rules: %w", err)
	}

	diseasesBound := sqlbind.MustBind(sqlDeathCauseAuthoredDiseases, tenantID)
	rows, err = r.pool.Query(ctx, diseasesBound.SQL(), diseasesBound.Args()...)
	if err != nil {
		return out, fmt.Errorf("health: read authored diseases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d domain.AuthoredDisease
		if err := rows.Scan(&d.Key, &d.Label, &d.Active); err != nil {
			return out, fmt.Errorf("health: scan authored disease: %w", err)
		}
		out.Diseases = append(out.Diseases, d)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("health: read authored diseases: %w", err)
	}
	return out, nil
}
