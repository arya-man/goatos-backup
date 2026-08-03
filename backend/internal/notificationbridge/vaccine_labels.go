package notificationbridge

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// VaccineLabelResolver resolves human-readable vaccine labels (e.g., "ET+TT", "PPR · Booster")
// for a small, bounded set of vaccination protocol rule ids. Like LocationNameResolver,
// it performs ONE batched query, never per-notification lookup.
//
// This enrichment is optional: a transient lookup failure or missing rule degrades
// the push copy (falls back to generic wording) rather than blocking notification delivery.
//
// C19b (confirmed defect): an earlier version of this resolver queried a `vaccination_rules`
// table with a `vaccine_label` column. Neither exists in the schema (see
// backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql). The real protocol schema
// is protocol_rules(rule_id, tenant_id, protocol_version_id, dose_code) -> protocol_versions
// (protocol_version_id, protocol_id) -> protocol_definitions(protocol_id, name), and there is NO
// precomputed label column anywhere -- the label is DERIVED. This resolver now reads the real
// tables and derives the label via the same canonical formatter every other module uses
// (vaccinationdomain.DoseDisplayLabel, mirrored by
// vaccinationexecution/domain.VaccinationDoseDisplayLabel), so calendar, execution, and this
// notification path can never render three different names for the same dose.
type VaccineLabelResolver struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewVaccineLabelResolver builds a resolver over the shared connection pool. A nil pool is accepted:
// ResolveVaccineLabels then always returns an empty map rather than panicking. A nil logger is also
// accepted (falls back to slog.Default()) -- lookup failures are still surfaced at WARN rather than
// swallowed silently, since a query that only ever returns an empty map on error previously hid
// both genuine outages and programmer mistakes (e.g. passing a non-rule-id argument) with no signal.
func NewVaccineLabelResolver(pool *pgxpool.Pool, logger *slog.Logger) *VaccineLabelResolver {
	if logger == nil {
		logger = slog.Default()
	}
	return &VaccineLabelResolver{pool: pool, logger: logger}
}

// ResolveVaccineLabels returns protocol_rules.rule_id -> human-readable vaccine label for every rule
// id in ruleIDs that exists for tenantID, via ONE query -- never one lookup per rule, never per
// notification. Call sites MUST pass real protocol_rules.rule_id values (uuids); passing anything
// else (a verification category string, a submission id, etc.) will not match any row and is logged
// at WARN so the mismatch is visible instead of silently degrading to generic copy forever.
//
// Errors are logged at WARN and then swallowed to an empty map: a transient lookup failure degrades
// the copy (falls back to the generic wording) rather than blocking the notification.
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
SELECT pr.rule_id::text, pd.name, pr.dose_code
FROM protocol_rules pr
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id AND pv.protocol_version_id = pr.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
WHERE pr.tenant_id = $1::uuid AND pr.rule_id = ANY($2::uuid[])`, tenantID, clean)
	if err != nil {
		r.logger.WarnContext(ctx, "vaccine label lookup failed, falling back to generic copy",
			"tenant_id", tenantID, "rule_id_count", len(clean), "error", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, protocolName, doseCode string
		if err := rows.Scan(&id, &protocolName, &doseCode); err != nil {
			r.logger.WarnContext(ctx, "vaccine label row scan failed", "tenant_id", tenantID, "error", err)
			continue
		}
		out[id] = vaccinationdomain.DoseDisplayLabel(protocolName, doseCode)
	}
	if err := rows.Err(); err != nil {
		r.logger.WarnContext(ctx, "vaccine label lookup iteration failed", "tenant_id", tenantID, "error", err)
	}
	return out
}

// ResolveVaccineLabelsForTask returns the distinct dose labels of the vaccination obligations the
// SOP task sopTaskID carried, derived from the same protocol schema (and the same canonical
// formatter) as ResolveVaccineLabels.
//
// It exists because the verification producer contract never carried a rule id at all: an item
// created from a vaccination SOP submission (sopbridge/vaccination_submission.go) carries
// Category="vaccination_proof" and Source.TaskID -- the sop task -- and nothing dose-shaped. The
// only durable link from that task back to the doses it discharged is
// obligation_instances(tenant_id, sop_task_id) -> rule_id, which is what this reads.
//
// A task may discharge a COMBO (several rules in one visit), which is exactly what a label such as
// "ET+TT" means, so several labels are a normal answer, not an error. The read is bounded by
// maxTaskVaccineLabels so a pathological task can never turn one push into an unbounded string;
// it walks idx_obligation_instances_sop_task and is one query per approved item, in line with the
// single batched location read beside it.
//
// Errors degrade to no labels (generic copy) after a WARN, never a blocked notification.
func (r *VaccineLabelResolver) ResolveVaccineLabelsForTask(ctx context.Context, tenantID, sopTaskID string) []string {
	if r == nil || r.pool == nil || strings.TrimSpace(tenantID) == "" {
		return nil
	}
	sopTaskID = strings.TrimSpace(sopTaskID)
	if !looksLikeUUID(sopTaskID) {
		return nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT pd.name, pr.dose_code
FROM obligation_instances oi
JOIN protocol_rules pr
  ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
JOIN protocol_versions pv
  ON pv.tenant_id = pr.tenant_id AND pv.protocol_version_id = pr.protocol_version_id
JOIN protocol_definitions pd
  ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id
WHERE oi.tenant_id = $1::uuid AND oi.sop_task_id = $2::uuid
ORDER BY pd.name, pr.dose_code
LIMIT $3`, tenantID, sopTaskID, maxTaskVaccineLabels)
	if err != nil {
		r.logger.WarnContext(ctx, "vaccine label lookup by sop task failed, falling back to generic copy",
			"tenant_id", tenantID, "sop_task_id", sopTaskID, "error", err)
		return nil
	}
	defer rows.Close()
	labels := make([]string, 0, maxTaskVaccineLabels)
	for rows.Next() {
		var protocolName, doseCode string
		if err := rows.Scan(&protocolName, &doseCode); err != nil {
			r.logger.WarnContext(ctx, "vaccine label row scan failed", "tenant_id", tenantID, "error", err)
			continue
		}
		if label := vaccinationdomain.DoseDisplayLabel(protocolName, doseCode); label != "" {
			labels = append(labels, label)
		}
	}
	if err := rows.Err(); err != nil {
		r.logger.WarnContext(ctx, "vaccine label lookup by sop task iteration failed",
			"tenant_id", tenantID, "error", err)
	}
	return labels
}

// maxTaskVaccineLabels caps how many dose labels one push may name. A real combo visit is two or
// three doses; anything beyond that is data noise and must not be allowed to grow the notification
// body without bound.
const maxTaskVaccineLabels = 3

// vaccineLabelOrFallback renders a resolved human label, or a neutral fallback ("vaccination")
// when the lookup is unavailable/empty -- never a raw code and never a blank segment in the copy.
func vaccineLabelOrFallback(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "vaccination"
	}
	return label
}

// looksLikeUUID is a cheap shape check (36 chars, dashes in the RFC-4122 positions) used only to
// avoid firing a doomed ::uuid[] cast query (and its resulting WARN-logged error) when a call site
// passes a non-uuid string where a rule_id is expected -- see the C19c call-site comment in
// verification_notify_consumer.go. It is NOT a validity/checksum check.
func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}
