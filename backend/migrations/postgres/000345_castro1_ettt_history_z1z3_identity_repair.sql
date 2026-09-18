-- +goose Up
-- Keep Z1+Z3 and ET+TT as separate vaccine identities. Some Z1+Z3 rules were
-- cloned from ET+TT timing rows and still carried ET+TT source metadata in
-- eligibility_json. Repair those identity fields, then record the maintainer-
-- accepted CBE Castro 1/2/3 ET+TT source-sheet course as pre-arrival history so
-- future ET+TT revaccination can chain from the completed course.

ALTER TABLE public.protocol_rules DISABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rules pr
SET eligibility_json = jsonb_set(
        jsonb_set(
          pr.eligibility_json,
          '{source_dose_code}',
          to_jsonb(replace(pr.eligibility_json->>'source_dose_code', 'et_tt_', 'z1_z3_')),
          true
        ),
        '{matrix_row_id}',
        to_jsonb(replace(COALESCE(NULLIF(pr.eligibility_json->>'matrix_row_id', ''), 'real-seed-z1_z3'), 'real-seed-et_tt', 'real-seed-z1_z3')),
        true
      )
WHERE upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
  AND (
    pr.eligibility_json->>'source_dose_code' LIKE 'et_tt_%'
    OR pr.eligibility_json->>'matrix_row_id' = 'real-seed-et_tt'
  );

ALTER TABLE public.protocol_rules ENABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rule_dimensions prd
SET source_dose_code = CASE
      WHEN prd.source_dose_code LIKE 'et_tt_%' THEN replace(prd.source_dose_code, 'et_tt_', 'z1_z3_')
      ELSE prd.source_dose_code
    END,
    matrix_row_id = replace(prd.matrix_row_id, 'real-seed-et_tt', 'real-seed-z1_z3'),
    schedule_json = CASE
      WHEN prd.schedule_json IS NULL THEN prd.schedule_json
      ELSE jsonb_set(
        jsonb_set(
          prd.schedule_json,
          '{source_dose_code}',
          to_jsonb(replace(COALESCE(prd.schedule_json->>'source_dose_code', prd.schedule_json->>'dose_code'), 'et_tt_', 'z1_z3_')),
          true
        ),
        '{matrix_row_id}',
        to_jsonb(replace(COALESCE(NULLIF(prd.schedule_json->>'matrix_row_id', ''), 'real-seed-z1_z3'), 'real-seed-et_tt', 'real-seed-z1_z3')),
        true
      )
    END
WHERE upper(btrim(COALESCE(prd.vaccine_code, ''))) = 'Z1_Z3'
  AND (
    prd.source_dose_code LIKE 'et_tt_%'
    OR prd.matrix_row_id = 'real-seed-et_tt'
    OR prd.schedule_json->>'source_dose_code' LIKE 'et_tt_%'
    OR prd.schedule_json->>'matrix_row_id' = 'real-seed-et_tt'
  );

UPDATE public.protocol_rule_lineage prl
SET identity_key = replace(replace(prl.identity_key, 'et_tt_', 'z1_z3_'), 'real-seed-et_tt', 'real-seed-z1_z3')
FROM public.protocol_rules pr
WHERE pr.rule_id = prl.rule_id
  AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
  AND (prl.identity_key LIKE '%et_tt_%' OR prl.identity_key LIKE '%real-seed-et_tt%');

-- Restore ET+TT as its own active vaccine family when a published version has the
-- Z1+Z3 rows but lost ET+TT. This deliberately clones only real ET_TT rows; it
-- does not reinterpret Z1+Z3 as ET+TT.
ALTER TABLE public.protocol_rules DISABLE TRIGGER protocol_rules_require_draft_version_trg;

WITH target_versions AS (
  SELECT pv.tenant_id, pv.protocol_version_id
  FROM public.protocol_versions pv
  WHERE pv.status = 'published'
    AND EXISTS (
      SELECT 1
      FROM public.protocol_rules z
      WHERE z.tenant_id = pv.tenant_id
        AND z.protocol_version_id = pv.protocol_version_id
        AND upper(btrim(COALESCE(z.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
    )
    AND NOT EXISTS (
      SELECT 1
      FROM public.protocol_rules et
      WHERE et.tenant_id = pv.tenant_id
        AND et.protocol_version_id = pv.protocol_version_id
        AND upper(btrim(COALESCE(et.eligibility_json->'vaccine'->>'code', ''))) = 'ET_TT'
    )
),
source_rules AS (
  SELECT DISTINCT ON (pr.tenant_id, pr.dose_code)
         pr.*
  FROM public.protocol_rules pr
  JOIN public.protocol_versions pv
    ON pv.tenant_id = pr.tenant_id
   AND pv.protocol_version_id = pr.protocol_version_id
  WHERE upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'ET_TT'
    AND pr.dose_code IN ('et_tt_kid_4w', 'et_tt_kid_7w', 'et_tt_adult_w1', 'et_tt_adult_w2', 'et_tt_revac')
  ORDER BY pr.tenant_id, pr.dose_code, pv.published_at DESC NULLS LAST, pr.created_at DESC
),
inserted_rules AS (
  INSERT INTO public.protocol_rules (
    rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
    offset_days, due_window_days, min_gap_days, repeat, repeat_until_after_age,
    catch_up, eligibility_json, sop_version_id, proof_policy, withdrawal_days,
    sort_order, created_at
  )
  SELECT gen_random_uuid(), tv.tenant_id, tv.protocol_version_id, sr.dose_code,
         sr.sequence, sr.trigger_type, sr.offset_days, sr.due_window_days,
         sr.min_gap_days, sr.repeat, sr.repeat_until_after_age, sr.catch_up,
         sr.eligibility_json, sr.sop_version_id, sr.proof_policy,
         sr.withdrawal_days, sr.sort_order, now()
  FROM target_versions tv
  JOIN source_rules sr ON sr.tenant_id = tv.tenant_id
  WHERE NOT EXISTS (
    SELECT 1
    FROM public.protocol_rules existing
    WHERE existing.tenant_id = tv.tenant_id
      AND existing.protocol_version_id = tv.protocol_version_id
      AND existing.dose_code = sr.dose_code
  )
  RETURNING tenant_id, protocol_version_id, rule_id, dose_code
),
source_rule_map AS (
  SELECT ir.tenant_id,
         ir.protocol_version_id,
         ir.rule_id,
         ir.dose_code,
         sr.rule_id AS source_rule_id,
         sr.protocol_version_id AS source_protocol_version_id
  FROM inserted_rules ir
  JOIN source_rules sr
    ON sr.tenant_id = ir.tenant_id
   AND sr.dose_code = ir.dose_code
),
inserted_dimensions AS (
  INSERT INTO public.protocol_rule_dimensions (
    protocol_rule_dimension_id, tenant_id, protocol_version_id, rule_id,
    category, ruleset_family, matrix_row_id, selector_key, dose_code,
    source_dose_code, vaccine_code, vaccine_type, pathogen_class,
    compatibility_group, species, animal_stage, sex, breed, lifecycle,
    health, reproductive, min_age_days, max_age_days, trigger_type,
    sequence, offset_days, due_window_days, min_gap_days, repeat,
    catch_up, max_delay_days, revaccination_interval_days, eligibility_json,
    vaccine_json, schedule_json, created_at, procurement_purpose
  )
  SELECT gen_random_uuid(), m.tenant_id, m.protocol_version_id, m.rule_id,
         d.category, d.ruleset_family, d.matrix_row_id, d.selector_key,
         d.dose_code, d.source_dose_code, d.vaccine_code, d.vaccine_type,
         d.pathogen_class, d.compatibility_group, d.species, d.animal_stage,
         d.sex, d.breed, d.lifecycle, d.health, d.reproductive,
         d.min_age_days, d.max_age_days, d.trigger_type, d.sequence,
         d.offset_days, d.due_window_days, d.min_gap_days, d.repeat,
         d.catch_up, d.max_delay_days, d.revaccination_interval_days,
         d.eligibility_json, d.vaccine_json, d.schedule_json, now(),
         d.procurement_purpose
  FROM source_rule_map m
  JOIN public.protocol_rule_dimensions d
    ON d.tenant_id = m.tenant_id
   AND d.protocol_version_id = m.source_protocol_version_id
   AND d.rule_id = m.source_rule_id
  RETURNING 1
)
INSERT INTO public.protocol_rule_lineage (
  tenant_id, protocol_version_id, rule_id, identity_key, content_fingerprint, created_at
)
SELECT m.tenant_id, m.protocol_version_id, m.rule_id,
       l.identity_key, l.content_fingerprint, now()
FROM source_rule_map m
JOIN public.protocol_rule_lineage l
  ON l.tenant_id = m.tenant_id
 AND l.protocol_version_id = m.source_protocol_version_id
 AND l.rule_id = m.source_rule_id
ON CONFLICT DO NOTHING;

ALTER TABLE public.protocol_rules ENABLE TRIGGER protocol_rules_require_draft_version_trg;

-- projection-review: membership=CBE Castro ET+TT source facts keyed by tenant, animal_key, and source dose; group_key=(tenant_id, animal_key, source_dose_code) for match_counts and goat_id for the final linked-animal assertion; join_cardinality=goat_identifiers is unique on tenant plus normalized_value and procurement_load_goats is a fallback, then count(DISTINCT goat_id) plus rn=1 rejects ambiguous one-to-many identity matches before writing history; pagination=none, this is a bounded one-off migration repair over the full source-fact set and not a paged display; scope=tenant_id plus TEMP-CBE-CASTRO1/2/3 lineage and ET+TT vaccine header
CREATE TEMP TABLE cbe_castro_ettt_source_facts AS
SELECT DISTINCT ON (vsf.tenant_id, vsf.animal_key, vsf.dose_code)
       vsf.tenant_id,
       vsf.source_fact_id,
       vsf.animal_key,
       lower(btrim(vsf.animal_key)) AS normalized_animal_key,
       substring(vsf.lineage_key FROM 'TEMP-CBE-(CASTRO[0-9]+)-') AS source_cohort,
       vsf.dose_code AS source_dose_code,
       CASE
         WHEN vsf.dose_code = 'first' THEN 'et_tt_kid_4w'
         WHEN vsf.dose_code = 'booster' THEN 'et_tt_kid_7w'
       END AS protocol_dose_code,
       CASE
         WHEN vsf.dose_code = 'first' THEN 1
         WHEN vsf.dose_code = 'booster' THEN 2
       END AS sequence,
       CASE
         WHEN vsf.dose_code = 'first' THEN DATE '2026-06-26'
         WHEN vsf.dose_code = 'booster' THEN DATE '2026-07-20'
       END AS administered_date_ist
FROM public.vaccination_source_facts vsf
WHERE vsf.vaccine_header = 'ET+TT'
  AND vsf.lineage_key ~ '^TEMP-CBE-CASTRO[123]-'
  AND vsf.source_value = 'Done - date unknown'
  AND vsf.dose_code IN ('first', 'booster')
ORDER BY vsf.tenant_id, vsf.animal_key, vsf.dose_code, vsf.created_at;

CREATE TEMP TABLE cbe_castro_ettt_linked_goats AS
WITH identifier_matches AS (
  SELECT sf.*, gi.goat_id, 1 AS match_rank
  FROM cbe_castro_ettt_source_facts sf
  JOIN public.goat_identifiers gi
    ON gi.tenant_id = sf.tenant_id
   AND gi.normalized_value = sf.normalized_animal_key
   AND gi.status = 'active'
   AND gi.identifier_type IN ('animal_identifier_1', 'temporary_tag')
),
procurement_matches AS (
  SELECT sf.*, plg.goat_id, 2 AS match_rank
  FROM cbe_castro_ettt_source_facts sf
  JOIN public.procurement_load_goats plg
    ON plg.tenant_id = sf.tenant_id
   AND lower(btrim(plg.animal_identifier_1)) = sf.normalized_animal_key
),
all_matches AS (
  SELECT * FROM identifier_matches
  UNION ALL
  SELECT * FROM procurement_matches
),
match_counts AS (
  SELECT tenant_id, animal_key, source_dose_code, count(DISTINCT goat_id) AS matched_goat_count
  FROM all_matches
  GROUP BY tenant_id, animal_key, source_dose_code
),
ranked_matches AS (
  SELECT *
  FROM (
    SELECT m.*,
           mc.matched_goat_count,
           row_number() OVER (
             PARTITION BY m.tenant_id, m.animal_key, m.source_dose_code
             ORDER BY m.match_rank, m.goat_id
           ) AS rn
    FROM all_matches m
    JOIN match_counts mc
      ON mc.tenant_id = m.tenant_id
     AND mc.animal_key = m.animal_key
     AND mc.source_dose_code = m.source_dose_code
  ) deduped
  WHERE rn = 1
)
SELECT rm.tenant_id, rm.source_fact_id, rm.animal_key, rm.normalized_animal_key,
       rm.source_cohort, rm.source_dose_code, rm.protocol_dose_code, rm.sequence,
       rm.administered_date_ist, rm.goat_id, rm.matched_goat_count, g.dob
FROM ranked_matches rm
JOIN public.goats g
  ON g.tenant_id = rm.tenant_id
 AND g.goat_id = rm.goat_id
WHERE g.dob IS NOT NULL;

DO $$
DECLARE
  source_animals integer;
  linked_goats integer;
  ambiguous_keys integer;
BEGIN
  SELECT count(DISTINCT animal_key) INTO source_animals
  FROM cbe_castro_ettt_source_facts;

  SELECT count(DISTINCT goat_id) INTO linked_goats
  FROM cbe_castro_ettt_linked_goats;

  SELECT count(*) INTO ambiguous_keys
  FROM cbe_castro_ettt_linked_goats
  WHERE matched_goat_count <> 1;

  IF source_animals > 0 AND linked_goats <> 204 THEN
    RAISE EXCEPTION 'CBE Castro ET+TT repair expected 204 linked goats, got %', linked_goats;
  END IF;

  IF source_animals > 0 AND ambiguous_keys <> 0 THEN
    RAISE EXCEPTION 'CBE Castro ET+TT repair found % ambiguous source identifier matches', ambiguous_keys;
  END IF;
END $$;

DELETE FROM public.vaccination_prearrival_history_entries
WHERE idempotency_key LIKE 'castro1-ettt-history:%';

WITH latest_et_rules AS (
  SELECT DISTINCT ON (pr.tenant_id, pr.dose_code)
         pr.tenant_id,
         pr.protocol_version_id,
         pr.rule_id,
         pr.dose_code,
         pr.sequence
  FROM public.protocol_rules pr
  JOIN public.protocol_versions pv
    ON pv.tenant_id = pr.tenant_id
   AND pv.protocol_version_id = pr.protocol_version_id
  WHERE pv.status = 'published'
    AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'ET_TT'
    AND pr.dose_code IN ('et_tt_kid_4w', 'et_tt_kid_7w')
  ORDER BY pr.tenant_id, pr.dose_code, pv.published_at DESC NULLS LAST, pr.created_at DESC
),
accepted_history AS (
  SELECT lg.tenant_id,
         lg.goat_id,
         'procurement_pc_handoff'::text AS source_system,
         'cbe-castro-ettt-source-fact:' || lg.source_fact_id::text AS source_event_id,
         er.protocol_version_id,
         er.rule_id,
         'ET_TT'::text AS vaccine_code,
         er.dose_code,
         er.sequence,
         (lg.administered_date_ist::timestamp AT TIME ZONE 'Asia/Kolkata') AS administered_at,
         'kid'::text AS schedule_path,
         'accepted'::text AS review_status,
         (lg.administered_date_ist::timestamp AT TIME ZONE 'Asia/Kolkata') AS reviewed_at,
         jsonb_build_object(
           'source', 'vaccination_source_facts',
           'source_fact_id', lg.source_fact_id,
           'animal_key', lg.animal_key,
           'source_cohort', lg.source_cohort,
           'source_value', 'Done - date unknown',
           'date_basis', 'maintainer_provided_cbe_castro_ettt_dates',
           'dob', lg.dob,
           'administered_date_ist', lg.administered_date_ist
         ) AS claim,
         'cbe-castro-ettt-history:' || lg.goat_id::text || ':' || er.dose_code AS idempotency_key,
         encode(digest(
           lg.goat_id::text || '|' || er.dose_code || '|' || lg.administered_date_ist::text,
           'sha256'
         ), 'hex') AS request_fingerprint
  FROM cbe_castro_ettt_linked_goats lg
  JOIN latest_et_rules er
    ON er.tenant_id = lg.tenant_id
   AND er.dose_code = lg.protocol_dose_code
)
INSERT INTO public.vaccination_prearrival_history_entries (
  tenant_id, goat_id, source_system, source_event_id, protocol_version_id, rule_id,
  vaccine_code, dose_code, sequence, administered_at, schedule_path, review_status,
  rejection_reason, reviewed_by, reviewed_at, claim, idempotency_key, request_fingerprint
)
SELECT tenant_id, goat_id, source_system, source_event_id, protocol_version_id, rule_id,
       vaccine_code, dose_code, sequence, administered_at, schedule_path, review_status,
       NULL::text, NULL::uuid, reviewed_at, claim, idempotency_key, request_fingerprint
FROM accepted_history
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET source_event_id = EXCLUDED.source_event_id,
    protocol_version_id = EXCLUDED.protocol_version_id,
    rule_id = EXCLUDED.rule_id,
    vaccine_code = EXCLUDED.vaccine_code,
    dose_code = EXCLUDED.dose_code,
    sequence = EXCLUDED.sequence,
    administered_at = EXCLUDED.administered_at,
    schedule_path = EXCLUDED.schedule_path,
    review_status = EXCLUDED.review_status,
    rejection_reason = NULL,
    reviewed_at = EXCLUDED.reviewed_at,
    claim = EXCLUDED.claim,
    request_fingerprint = EXCLUDED.request_fingerprint;

DO $$
DECLARE
  source_animals integer;
  history_rows integer;
  active_rule_bound_rows integer;
  bad_z1z3_rows integer;
BEGIN
  SELECT count(DISTINCT animal_key) INTO source_animals
  FROM cbe_castro_ettt_source_facts;

  SELECT count(*) INTO history_rows
  FROM public.vaccination_prearrival_history_entries
  WHERE idempotency_key LIKE 'cbe-castro-ettt-history:%'
    AND review_status = 'accepted'
    AND vaccine_code = 'ET_TT'
    AND dose_code IN ('et_tt_kid_4w', 'et_tt_kid_7w');

  IF source_animals > 0 AND history_rows <> 408 THEN
    RAISE EXCEPTION 'CBE Castro ET+TT repair expected 408 accepted history rows, got %', history_rows;
  END IF;

  SELECT count(*) INTO active_rule_bound_rows
  FROM public.vaccination_prearrival_history_entries h
  JOIN public.protocol_versions pv
    ON pv.tenant_id = h.tenant_id
   AND pv.protocol_version_id = h.protocol_version_id
   AND pv.status = 'published'
  JOIN public.protocol_rules pr
    ON pr.tenant_id = h.tenant_id
   AND pr.protocol_version_id = h.protocol_version_id
   AND pr.rule_id = h.rule_id
   AND upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'ET_TT'
  WHERE h.idempotency_key LIKE 'cbe-castro-ettt-history:%'
    AND h.review_status = 'accepted'
    AND h.vaccine_code = 'ET_TT'
    AND h.dose_code IN ('et_tt_kid_4w', 'et_tt_kid_7w');

  IF source_animals > 0 AND active_rule_bound_rows <> 408 THEN
    RAISE EXCEPTION 'CBE Castro ET+TT repair expected 408 history rows bound to published ET+TT rules, got %', active_rule_bound_rows;
  END IF;

  SELECT count(*) INTO bad_z1z3_rows
  FROM public.protocol_rules pr
  WHERE upper(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) = 'Z1_Z3'
    AND (
      pr.eligibility_json->>'source_dose_code' LIKE 'et_tt_%'
      OR pr.eligibility_json->>'matrix_row_id' = 'real-seed-et_tt'
    );

  IF bad_z1z3_rows <> 0 THEN
    RAISE EXCEPTION 'CBE Castro ET+TT repair expected zero bad Z1+Z3 identity rows, got %', bad_z1z3_rows;
  END IF;
END $$;

-- +goose Down
DELETE FROM public.vaccination_prearrival_history_entries
WHERE idempotency_key LIKE 'castro1-ettt-history:%'
   OR idempotency_key LIKE 'cbe-castro-ettt-history:%';
