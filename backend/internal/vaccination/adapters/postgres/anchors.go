package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
	vaccports "github.com/vgoats/goatos/backend/internal/vaccination/ports"
)

func (r *Repository) PreviewAnchor(ctx context.Context, in domain.AnchorCommand) (domain.AnchorPreview, error) {
	return r.anchorPreview(ctx, in, false)
}

func (r *Repository) CreateAnchor(ctx context.Context, in domain.AnchorCommand) (domain.AnchorPreview, error) {
	return r.anchorPreview(ctx, in, true)
}

func (r *Repository) anchorPreview(ctx context.Context, in domain.AnchorCommand, apply bool) (domain.AnchorPreview, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if err := validateAnchorCommand(in, apply); err != nil {
		return domain.AnchorPreview{}, err
	}
	tenant, err := pgconv.UUID(in.TenantID)
	if err != nil {
		return domain.AnchorPreview{}, fmt.Errorf("vaccination: anchor tenant id: %w", err)
	}
	var actor pgtype.UUID
	if strings.TrimSpace(in.CreatedBy) != "" {
		if err := actor.Scan(strings.TrimSpace(in.CreatedBy)); err != nil {
			return domain.AnchorPreview{}, fmt.Errorf("vaccination: anchor created_by: %w", err)
		}
	}
	anchorDate := in.AnchorDate.In(biztime.DefaultLocation())
	anchorDateText := anchorDate.Format("2006-01-02")
	scopePayload := json.RawMessage(in.Scope.Payload)
	if len(scopePayload) == 0 {
		scopePayload = json.RawMessage(`{}`)
	}
	eventID := uuid.NewString()
	if apply {
		if existing, found, err := r.anchorByIdempotency(ctx, tenant, in.IdempotencyKey, in.RequestHash); err != nil {
			return domain.AnchorPreview{}, err
		} else if found {
			preview, err := r.anchorPreviewRows(ctx, tenant, in, anchorDateText)
			if err != nil {
				return domain.AnchorPreview{}, err
			}
			preview.AnchorEventID = existing
			preview.Applied = false
			preview.PreviewOnly = false
			return preview, nil
		}
	}
	preview, err := r.anchorPreviewRows(ctx, tenant, in, anchorDateText)
	if err != nil {
		return domain.AnchorPreview{}, err
	}
	if preview.TotalResolvedAnimals == 0 {
		return preview, fmt.Errorf("%w: scope resolves to no live animals", domain.ErrInvalidAnchor)
	}
	if len(preview.RuleOptions) == 0 {
		if in.DoseCode != "" {
			return preview, fmt.Errorf("%w: dose_code does not belong to vaccine", domain.ErrInvalidAnchor)
		}
		return preview, fmt.Errorf("%w: vaccine_code does not map to an active vaccination rule", domain.ErrInvalidAnchor)
	}
	if in.DoseCode != "" && preview.DoseCode == "" {
		return preview, fmt.Errorf("%w: dose_code does not belong to vaccine", domain.ErrInvalidAnchor)
	}
	if in.DoseCode == "" && len(preview.RuleOptions) != 1 {
		return preview, fmt.Errorf("%w: dose_code is required when more than one active dose exists for vaccine", domain.ErrInvalidAnchor)
	}
	if preview.EligibleAnimals == 0 {
		return preview, fmt.Errorf("%w: no eligible animals for anchor", domain.ErrInvalidAnchor)
	}
	if !apply {
		preview.PreviewOnly = true
		return preview, nil
	}
	versionID := preview.RuleOptions[0].ProtocolVersionID
	tag, err := r.pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, suppress_before_anchor, chain_future_from_anchor, enforce_age_eligibility,
  reason, source_system, source_ref, created_by, idempotency_key, request_hash
) VALUES (
  $1::uuid, $2, $3::uuid, $4, NULLIF($5, ''), $6::date,
  $7, $8::jsonb, $9, $10, $11,
  $12, $13, NULLIF($14, ''), $15, $16, $17
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`,
		eventID, tenant, versionID, preview.VaccineCode, preview.DoseCode, anchorDateText,
		in.Scope.Type, scopePayload, in.SuppressBeforeAnchor, in.ChainFutureFromAnchor, in.EnforceAgeEligibility,
		in.Reason, in.SourceSystem, in.SourceRef, actor, in.IdempotencyKey, in.RequestHash)
	if err != nil {
		return domain.AnchorPreview{}, fmt.Errorf("vaccination: create anchor: %w", err)
	}
	preview.AnchorEventID = eventID
	preview.Applied = tag.RowsAffected() == 1
	preview.PreviewOnly = false
	if !preview.Applied {
		if existing, found, err := r.anchorByIdempotency(ctx, tenant, in.IdempotencyKey, in.RequestHash); err != nil {
			return domain.AnchorPreview{}, err
		} else if found {
			preview.AnchorEventID = existing
		}
	}
	return preview, nil
}

func validateAnchorCommand(in domain.AnchorCommand, apply bool) error {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.VaccineCode) == "" || in.AnchorDate.IsZero() || strings.TrimSpace(in.Reason) == "" {
		return fmt.Errorf("%w: tenant_id, vaccine_code, anchor_date, and reason are required", domain.ErrInvalidAnchor)
	}
	switch strings.TrimSpace(in.Scope.Type) {
	case "tenant", "park", "shed", "partition", "animal_set":
	default:
		return fmt.Errorf("%w: invalid scope_type", domain.ErrInvalidAnchor)
	}
	if !json.Valid(in.Scope.Payload) {
		return fmt.Errorf("%w: scope_payload must be valid JSON", domain.ErrInvalidAnchor)
	}
	if strings.TrimSpace(in.SourceSystem) == "" {
		return fmt.Errorf("%w: source_system is required", domain.ErrInvalidAnchor)
	}
	if apply && strings.TrimSpace(in.IdempotencyKey) == "" {
		return fmt.Errorf("%w: idempotency key is required", domain.ErrInvalidAnchor)
	}
	return nil
}

func (r *Repository) anchorByIdempotency(ctx context.Context, tenant pgtype.UUID, key, requestHash string) (string, bool, error) {
	var id, storedHash string
	err := r.pool.QueryRow(ctx, `
SELECT vaccination_anchor_event_id::text, request_hash
FROM vaccination_anchor_events
WHERE tenant_id = $1 AND idempotency_key = $2`, tenant, key).Scan(&id, &storedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("vaccination: read anchor idempotency: %w", err)
	}
	if storedHash != "" && requestHash != "" && storedHash != requestHash {
		return "", false, vaccports.ErrIdempotencyConflict
	}
	return id, true, nil
}

func (r *Repository) anchorPreviewRows(ctx context.Context, tenant pgtype.UUID, in domain.AnchorCommand, anchorDate string) (domain.AnchorPreview, error) {
	rows, err := r.pool.Query(ctx, anchorPreviewSQL, tenant, in.VaccineCode, in.DoseCode, anchorDate, in.Scope.Type, json.RawMessage(in.Scope.Payload), in.EnforceAgeEligibility)
	if err != nil {
		return domain.AnchorPreview{}, fmt.Errorf("vaccination: preview anchor: %w", err)
	}
	defer rows.Close()
	out := domain.AnchorPreview{
		VaccineCode:           strings.TrimSpace(in.VaccineCode),
		DoseCode:              strings.TrimSpace(in.DoseCode),
		AnchorDate:            anchorDate,
		ScopeType:             in.Scope.Type,
		EligibleSample:        []domain.AnchorAnimal{},
		UnderageSample:        []domain.AnchorAnimal{},
		SpeciesMismatchSample: []domain.AnchorAnimal{},
		RuleOptions:           []domain.AnchorRuleOption{},
	}
	ruleSeen := map[string]struct{}{}
	for rows.Next() {
		var bucket, goatID, identifier, species, reason string
		var ruleJSON []byte
		if err := rows.Scan(&bucket, &goatID, &identifier, &species, &reason, &ruleJSON); err != nil {
			return domain.AnchorPreview{}, fmt.Errorf("vaccination: scan anchor preview: %w", err)
		}
		switch bucket {
		case "rule":
			var rule domain.AnchorRuleOption
			if err := json.Unmarshal(ruleJSON, &rule); err != nil {
				return domain.AnchorPreview{}, fmt.Errorf("vaccination: decode anchor rule: %w", err)
			}
			key := rule.ProtocolVersionID + "|" + rule.RuleID
			if _, ok := ruleSeen[key]; !ok {
				ruleSeen[key] = struct{}{}
				out.RuleOptions = append(out.RuleOptions, rule)
				if len(ruleSeen) == 1 || out.DoseCode == "" {
					out.DoseCode = rule.DoseCode
				}
			}
		case "eligible":
			out.EligibleAnimals++
			out.EligibleSample = append(out.EligibleSample, domain.AnchorAnimal{GoatID: goatID, Identifier: identifier, Species: species})
		case "underage":
			out.ExcludedUnderageAnimals++
			out.UnderageSample = append(out.UnderageSample, domain.AnchorAnimal{GoatID: goatID, Identifier: identifier, Species: species, Reason: reason})
		case "species_mismatch":
			out.SpeciesMismatchAnimals++
			out.SpeciesMismatchSample = append(out.SpeciesMismatchSample, domain.AnchorAnimal{GoatID: goatID, Identifier: identifier, Species: species, Reason: reason})
		case "resolved_total":
			out.TotalResolvedAnimals++
		case "before_open":
			out.OpenRowsBeforeAnchor++
		case "same_day_open":
			out.SameDayRowsPreserved++
		}
	}
	if err := rows.Err(); err != nil {
		return domain.AnchorPreview{}, fmt.Errorf("vaccination: anchor preview rows: %w", err)
	}
	return out, nil
}

const anchorPreviewSQL = `
WITH rules AS (
  SELECT DISTINCT ON (lower(btrim(COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', '')))), lower(btrim(pr.dose_code)))
         pv.protocol_version_id::text,
         pv.protocol_id::text,
         pr.rule_id::text,
         COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', '')) AS vaccine_code,
         pr.dose_code,
         pr.sequence,
         pr.trigger_type,
         pr.offset_days,
         COALESCE(pr.eligibility_json -> 'eligibility' -> 'species', pr.eligibility_json -> 'species', pv.rule_dsl -> 'eligibility' -> 'species', '["all"]'::jsonb) AS species_json
  FROM protocol_versions pv
  JOIN protocol_definitions pd ON pd.tenant_id = pv.tenant_id AND pd.protocol_id = pv.protocol_id AND pd.category = 'vaccination'
  JOIN protocol_rules pr ON pr.tenant_id = pv.tenant_id AND pr.protocol_version_id = pv.protocol_version_id
  JOIN protocol_rule_lineage l ON l.tenant_id = pr.tenant_id AND l.protocol_version_id = pr.protocol_version_id AND l.rule_id = pr.rule_id
  LEFT JOIN LATERAL (
    SELECT row AS matrix_row
    FROM jsonb_array_elements(COALESCE(pv.rule_dsl -> 'matrix_rows', '[]'::jsonb)) row
    WHERE lower(btrim(row -> 'vaccine' ->> 'code')) = lower(btrim(COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', ''))))
    LIMIT 1
  ) matrix ON true
  LEFT JOIN LATERAL (
    SELECT sched AS matrix_schedule
    FROM jsonb_array_elements(COALESCE(matrix.matrix_row -> 'schedule', '[]'::jsonb)) sched
    WHERE NULLIF(sched ->> 'sequence', '')::integer = pr.sequence
      AND COALESCE(NULLIF(sched ->> 'trigger_type', ''), pr.trigger_type) = pr.trigger_type
      AND COALESCE(NULLIF(sched ->> 'offset_days', '')::integer, pr.offset_days) = pr.offset_days
    LIMIT 1
  ) schedule_alias ON true
  WHERE pv.tenant_id = $1
    AND pv.status = 'published'
    AND pv.effective_from <= $4::date
    AND (pv.effective_to IS NULL OR pv.effective_to > $4::date)
    AND lower(btrim(COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', '')))) = lower(btrim($2))
    AND (
      NULLIF($3, '') IS NULL
      OR lower(btrim(pr.dose_code)) = lower(btrim($3))
      OR lower(btrim(COALESCE(schedule_alias.matrix_schedule ->> 'dose_code', ''))) = lower(btrim($3))
      OR lower(btrim(COALESCE(schedule_alias.matrix_schedule ->> 'source_dose_code', ''))) = lower(btrim($3))
    )
  ORDER BY lower(btrim(COALESCE(NULLIF(pr.eligibility_json -> 'vaccine' ->> 'code', ''), NULLIF(pv.rule_dsl -> 'vaccine' ->> 'code', '')))), lower(btrim(pr.dose_code)), pv.effective_from DESC, pv.protocol_version_id
),
resolved AS (
  SELECT g.goat_id,
         COALESCE(gi.identifier_value, g.goat_id::text) AS identifier,
         COALESCE(NULLIF(g.species, ''), '') AS species,
         g.dob,
         g.park_id,
         g.shed_id,
         COALESCE(gsp.partition_label, 'whole') AS partition_label
  FROM goats g
  LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id AND gsp.shed_id = g.shed_id
  LEFT JOIN LATERAL (
    SELECT identifier_value
    FROM goat_identifiers gi
    WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id AND gi.status = 'active' AND gi.identifier_type = 'animal_identifier_1'
    ORDER BY gi.is_primary_for_goat DESC, gi.valid_from DESC, gi.identifier_value
    LIMIT 1
  ) gi ON true
  WHERE g.tenant_id = $1
    AND g.lifecycle_status = 'alive'
    AND (
      $5 = 'tenant'
      OR ($5 = 'animal_set' AND $6::jsonb ? 'animal_ids' AND ($6::jsonb -> 'animal_ids') ? g.goat_id::text)
      OR ($5 = 'park' AND COALESCE($6::jsonb ->> 'park_id', '') = g.park_id::text)
      OR ($5 = 'shed' AND COALESCE($6::jsonb ->> 'shed_id', '') = g.shed_id::text)
      OR ($5 = 'partition' AND COALESCE($6::jsonb ->> 'shed_id', '') = g.shed_id::text AND COALESCE($6::jsonb ->> 'partition_label', '') = COALESCE(gsp.partition_label, 'whole'))
    )
),
classified AS (
  SELECT r.*,
         rule.protocol_version_id,
         rule.protocol_id,
         rule.rule_id,
         rule.vaccine_code,
         rule.dose_code,
         rule.sequence,
         rule.trigger_type,
         rule.offset_days,
         rule.species_json,
         CASE
           WHEN NOT (
             rule.species_json ? 'all'
             OR EXISTS (SELECT 1 FROM jsonb_array_elements_text(rule.species_json) s(value) WHERE lower(btrim(s.value)) = lower(btrim(r.species)))
           ) THEN 'species_mismatch'
           WHEN $7::boolean AND rule.trigger_type = 'birth_age' AND (r.dob IS NULL OR r.dob + rule.offset_days > $4::date) THEN 'underage'
           ELSE 'eligible'
         END AS bucket
  FROM resolved r
  JOIN rules rule ON true
),
open_rows AS (
  SELECT CASE WHEN oi.due_at::date < $4::date THEN 'before_open' ELSE 'same_day_open' END AS bucket,
         oi.target_id::text AS goat_id
  FROM obligation_instances oi
  JOIN classified c ON c.goat_id = oi.target_id AND c.bucket = 'eligible'
  JOIN protocol_rule_lineage current_lineage
    ON current_lineage.tenant_id = $1
   AND current_lineage.protocol_version_id::text = c.protocol_version_id
   AND current_lineage.rule_id::text = c.rule_id
  JOIN protocol_rule_lineage obligation_lineage
    ON obligation_lineage.tenant_id = oi.tenant_id
   AND obligation_lineage.protocol_version_id = oi.protocol_version_id
   AND obligation_lineage.rule_id = oi.rule_id
   AND obligation_lineage.identity_key = current_lineage.identity_key
  WHERE oi.tenant_id = $1
    AND oi.target_type = 'goat'
    AND oi.status IN ('scheduled', 'due', 'in_progress', 'deferred')
    AND oi.due_at::date <= $4::date
)
SELECT 'rule', '', '', '', '', jsonb_build_object(
  'protocol_version_id', protocol_version_id,
  'protocol_id', protocol_id,
  'rule_id', rule_id,
  'vaccine_code', vaccine_code,
  'dose_code', dose_code,
  'sequence', sequence,
  'trigger_type', trigger_type,
  'offset_days', offset_days,
  'species', COALESCE((SELECT jsonb_agg(value) FROM jsonb_array_elements_text(species_json)), '[]'::jsonb)
)::text::bytea
FROM rules
UNION ALL
SELECT 'resolved_total', goat_id::text, identifier, species, '', NULL::bytea
FROM resolved
UNION ALL
SELECT bucket, goat_id::text, identifier, species,
       CASE WHEN bucket = 'underage' THEN 'younger_than_rule_offset' WHEN bucket = 'species_mismatch' THEN 'species_not_allowed_for_rule' ELSE '' END,
       NULL::bytea
FROM classified
UNION ALL
SELECT bucket, goat_id, '', '', '', NULL::bytea
FROM open_rows
ORDER BY 1, 3, 2`
