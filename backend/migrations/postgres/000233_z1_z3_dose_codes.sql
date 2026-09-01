-- +goose Up
-- Repair Z1+Z3 rules that were cloned from ET+TT timing but kept ET+TT dose
-- tokens. Vaccine code and dose code must not disagree in operator/mobile UI.

ALTER TABLE public.protocol_rules DISABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rules pr
SET dose_code = replace(pr.dose_code, 'et_tt_', 'z1_z3_')
WHERE pr.dose_code LIKE 'et_tt_%'
  AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
  AND NOT EXISTS (
    SELECT 1
    FROM public.protocol_rules existing
    WHERE existing.tenant_id = pr.tenant_id
      AND existing.protocol_version_id = pr.protocol_version_id
      AND existing.dose_code = replace(pr.dose_code, 'et_tt_', 'z1_z3_')
  );

ALTER TABLE public.protocol_rules ENABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rule_dimensions prd
SET dose_code = replace(prd.dose_code, 'et_tt_', 'z1_z3_'),
    source_dose_code = CASE
      WHEN prd.source_dose_code LIKE 'et_tt_%' THEN replace(prd.source_dose_code, 'et_tt_', 'z1_z3_')
      ELSE prd.source_dose_code
    END,
    schedule_json = CASE
      WHEN prd.schedule_json IS NULL THEN prd.schedule_json
      ELSE jsonb_set(
        jsonb_set(
          prd.schedule_json,
          '{dose_code}',
          to_jsonb(replace(prd.schedule_json->>'dose_code', 'et_tt_', 'z1_z3_')),
          true
        ),
        '{source_dose_code}',
        to_jsonb(replace(COALESCE(prd.schedule_json->>'source_dose_code', prd.schedule_json->>'dose_code'), 'et_tt_', 'z1_z3_')),
        true
      )
    END
WHERE prd.vaccine_code = 'Z1_Z3'
  AND prd.dose_code LIKE 'et_tt_%'
  AND EXISTS (
    SELECT 1
    FROM public.protocol_rules pr
    WHERE pr.rule_id = prd.rule_id
      AND pr.dose_code = replace(prd.dose_code, 'et_tt_', 'z1_z3_')
      AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
  );

UPDATE public.protocol_rule_lineage prl
SET identity_key = replace(prl.identity_key, 'et_tt_', 'z1_z3_')
FROM public.protocol_rules pr
WHERE pr.rule_id = prl.rule_id
  AND prl.identity_key LIKE '%et_tt_%'
  AND pr.dose_code LIKE 'z1_z3_%'
  AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3';

-- +goose Down
ALTER TABLE public.protocol_rules DISABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rules pr
SET dose_code = replace(pr.dose_code, 'z1_z3_', 'et_tt_')
WHERE pr.dose_code LIKE 'z1_z3_%'
  AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
  AND NOT EXISTS (
    SELECT 1
    FROM public.protocol_rules existing
    WHERE existing.tenant_id = pr.tenant_id
      AND existing.protocol_version_id = pr.protocol_version_id
      AND existing.dose_code = replace(pr.dose_code, 'z1_z3_', 'et_tt_')
  );

ALTER TABLE public.protocol_rules ENABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rule_dimensions prd
SET dose_code = replace(prd.dose_code, 'z1_z3_', 'et_tt_'),
    source_dose_code = CASE
      WHEN prd.source_dose_code LIKE 'z1_z3_%' THEN replace(prd.source_dose_code, 'z1_z3_', 'et_tt_')
      ELSE prd.source_dose_code
    END,
    schedule_json = CASE
      WHEN prd.schedule_json IS NULL THEN prd.schedule_json
      ELSE jsonb_set(
        jsonb_set(
          prd.schedule_json,
          '{dose_code}',
          to_jsonb(replace(prd.schedule_json->>'dose_code', 'z1_z3_', 'et_tt_')),
          true
        ),
        '{source_dose_code}',
        to_jsonb(replace(COALESCE(prd.schedule_json->>'source_dose_code', prd.schedule_json->>'dose_code'), 'z1_z3_', 'et_tt_')),
        true
      )
    END
WHERE prd.vaccine_code = 'Z1_Z3'
  AND prd.dose_code LIKE 'z1_z3_%'
  AND EXISTS (
    SELECT 1
    FROM public.protocol_rules pr
    WHERE pr.rule_id = prd.rule_id
      AND pr.dose_code = replace(prd.dose_code, 'z1_z3_', 'et_tt_')
      AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
  );

UPDATE public.protocol_rule_lineage prl
SET identity_key = replace(prl.identity_key, 'z1_z3_', 'et_tt_')
FROM public.protocol_rules pr
WHERE pr.rule_id = prl.rule_id
  AND prl.identity_key LIKE '%z1_z3_%'
  AND pr.dose_code LIKE 'et_tt_%'
  AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3';
