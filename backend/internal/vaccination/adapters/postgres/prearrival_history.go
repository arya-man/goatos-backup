package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

// IngestPreArrivalHistory persists one goat's REVIEWED pre-arrival supplier-claimed vaccination
// history (BUG-017).
//
// Write-path contract (AGENTS.md idempotency):
//   - every entry carries a stable idempotency key plus a semantic payload fingerprint, and both
//     are written in the SAME statement (hence the same transaction) as the row itself;
//   - an exact replay conflicts on (tenant_id, idempotency_key), inserts nothing, and returns the
//     original outcome with no new side effects;
//   - a same-key/different-payload replay is REJECTED with ports.ErrIdempotencyConflict rather
//     than silently overwriting an already-reviewed medical claim.
//
// Scale shape: one set-based statement over UNNEST arrays, never a per-entry loop.
func (r *Repository) IngestPreArrivalHistory(ctx context.Context, in domain.PreArrivalHistoryIngest) (domain.PreArrivalHistoryIngestResult, error) {
	var res domain.PreArrivalHistoryIngestResult
	if len(in.Entries) == 0 {
		return res, nil
	}
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	n := len(in.Entries)
	keys := make([]string, 0, n)
	fingerprints := make([]string, 0, n)
	versionIDs := make([]string, 0, n)
	ruleIDs := make([]string, 0, n)
	vaccineCodes := make([]string, 0, n)
	doseCodes := make([]string, 0, n)
	sequences := make([]int32, 0, n)
	administered := make([]time.Time, 0, n)
	paths := make([]string, 0, n)
	statuses := make([]string, 0, n)
	reasons := make([]string, 0, n)
	claims := make([]string, 0, n)
	wantFingerprint := make(map[string]string, n)

	for _, entry := range in.Entries {
		keys = append(keys, entry.IdempotencyKey)
		fingerprints = append(fingerprints, entry.RequestFingerprint)
		versionIDs = append(versionIDs, entry.ProtocolVersionID)
		ruleIDs = append(ruleIDs, entry.RuleID)
		vaccineCodes = append(vaccineCodes, entry.VaccineCode)
		doseCodes = append(doseCodes, entry.DoseCode)
		sequences = append(sequences, entry.Sequence)
		administeredAt := entry.AdministeredAt
		if administeredAt.IsZero() {
			// A claim with no usable date is always rejected; anchor the row at review time so the
			// NOT NULL column stays honest and the rejected entry is still surfaceable.
			administeredAt = in.ReviewedAt
		}
		administered = append(administered, administeredAt.UTC())
		paths = append(paths, entry.SchedulePath)
		statuses = append(statuses, entry.ReviewStatus)
		reasons = append(reasons, entry.RejectionReason)
		claim := string(entry.Claim)
		if claim == "" {
			claim = "{}"
		}
		claims = append(claims, claim)
		wantFingerprint[entry.IdempotencyKey] = entry.RequestFingerprint
		switch entry.ReviewStatus {
		case domain.PreArrivalHistoryReviewAccepted:
			res.Accepted++
		default:
			res.Rejected++
		}
	}

	reviewedBy := ""
	if in.ReviewedBy != nil {
		reviewedBy = *in.ReviewedBy
	}
	sourceSystem := in.SourceSystem
	if sourceSystem == "" {
		sourceSystem = domain.PreArrivalHistorySourceProcurementHandoff
	}

	rows, err := r.pool.Query(ctx, `
WITH input AS (
  SELECT * FROM unnest(
    $7::text[], $8::text[], $9::text[], $10::text[], $11::text[], $12::text[],
    $13::int[], $14::timestamptz[], $15::text[], $16::text[], $17::text[], $18::text[]
  ) AS t(idempotency_key, request_fingerprint, protocol_version_id, rule_id, vaccine_code,
         dose_code, sequence, administered_at, schedule_path, review_status, rejection_reason, claim)
), ins AS (
  INSERT INTO vaccination_prearrival_history_entries (
    tenant_id, goat_id, source_system, source_event_id, protocol_version_id, rule_id,
    vaccine_code, dose_code, sequence, administered_at, schedule_path, review_status,
    rejection_reason, reviewed_by, reviewed_at, claim, idempotency_key, request_fingerprint
  )
  SELECT $1::uuid, $2::uuid, $3::text, $4::text,
         nullif(btrim(i.protocol_version_id), '')::uuid,
         nullif(btrim(i.rule_id), '')::uuid,
         i.vaccine_code, i.dose_code, i.sequence, i.administered_at, i.schedule_path,
         i.review_status, nullif(btrim(i.rejection_reason), ''),
         nullif(btrim($5::text), '')::uuid, $6::timestamptz,
         i.claim::jsonb, i.idempotency_key, i.request_fingerprint
  FROM input i
  ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
  RETURNING idempotency_key
)
SELECT i.idempotency_key,
       (ins.idempotency_key IS NOT NULL) AS inserted,
       COALESCE(existing.request_fingerprint, '') AS existing_fingerprint
FROM input i
LEFT JOIN ins ON ins.idempotency_key = i.idempotency_key
LEFT JOIN vaccination_prearrival_history_entries existing
  ON existing.tenant_id = $1::uuid
 AND existing.idempotency_key = i.idempotency_key`,
		in.TenantID, in.GoatID, sourceSystem, in.SourceEventID, reviewedBy, in.ReviewedAt.UTC(),
		keys, fingerprints, versionIDs, ruleIDs, vaccineCodes, doseCodes,
		sequences, administered, paths, statuses, reasons, claims)
	if err != nil {
		return domain.PreArrivalHistoryIngestResult{}, fmt.Errorf("vaccination: ingest pre-arrival history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, existingFingerprint string
		var inserted bool
		if err := rows.Scan(&key, &inserted, &existingFingerprint); err != nil {
			return domain.PreArrivalHistoryIngestResult{}, fmt.Errorf("vaccination: scan pre-arrival history ingest: %w", err)
		}
		if inserted {
			res.Inserted++
			continue
		}
		if existingFingerprint != "" && existingFingerprint != wantFingerprint[key] {
			return domain.PreArrivalHistoryIngestResult{}, fmt.Errorf(
				"vaccination: pre-arrival history %s: %w", key, ports.ErrIdempotencyConflict)
		}
		res.Replayed++
	}
	if err := rows.Err(); err != nil {
		return domain.PreArrivalHistoryIngestResult{}, fmt.Errorf("vaccination: pre-arrival history ingest rows: %w", err)
	}
	return res, nil
}
