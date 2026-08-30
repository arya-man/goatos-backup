-- +goose Up
-- seed-fixture-guard:ignore: repairs published vaccination protocol metadata for the runtime Blue Tongue booster interval; no HRMS source rows, fixture inputs, or import contract change
-- Data repair for the user-confirmed Blue Tongue sheep kid course:
-- dose 1 at 16w/112d, dose 2/booster at 19w/133d with a 21d minimum gap.
--
-- Some staging databases carry the stale derived row blue_tongue_kid_20w
-- at 140d/28d. Source seeding now writes blue_tongue_kid_19w correctly; this
-- migration repairs already-published protocol_rules rows without touching the
-- Goat Pox 20w live-live spacing exception.

CREATE OR REPLACE FUNCTION pg_temp.goatos_canonical_json(value jsonb)
RETURNS text
LANGUAGE sql
STABLE
AS $$
  SELECT CASE jsonb_typeof(value)
    WHEN 'object' THEN
      COALESCE(
        (
          SELECT '{' || string_agg(to_jsonb(key)::text || ':' || pg_temp.goatos_canonical_json(val), ',' ORDER BY key) || '}'
          FROM jsonb_each(value) AS obj(key, val)
        ),
        '{}'
      )
    WHEN 'array' THEN
      COALESCE(
        (
          SELECT '[' || string_agg(pg_temp.goatos_canonical_json(val), ',' ORDER BY ord) || ']'
          FROM jsonb_array_elements(value) WITH ORDINALITY AS arr(val, ord)
        ),
        '[]'
      )
    ELSE value::text
  END
$$;

CREATE OR REPLACE FUNCTION pg_temp.goatos_rule_content_fingerprint(
  trigger_type text,
  offset_days integer,
  due_window_days integer,
  min_gap_days integer,
  repeat text,
  repeat_until_after_age text,
  catch_up text,
  sop_version_id uuid,
  withdrawal_days integer,
  eligibility_json jsonb,
  proof_policy jsonb
)
RETURNS text
LANGUAGE sql
STABLE
AS $$
  SELECT encode(
    public.digest(
      concat_ws(E'\n',
        'trigger_type=' || lower(btrim(COALESCE(trigger_type, ''))),
        'offset_days=' || COALESCE(offset_days, 0)::text,
        'due_window_days=' || COALESCE(due_window_days, 0)::text,
        'min_gap_days=' || COALESCE(min_gap_days, 0)::text,
        'repeat=' || lower(btrim(COALESCE(repeat, ''))),
        'repeat_until_after_age=' || lower(btrim(COALESCE(repeat_until_after_age, ''))),
        'catch_up=' || lower(btrim(COALESCE(catch_up, ''))),
        'sop_version_id=' || COALESCE(sop_version_id::text, ''),
        'withdrawal_days=' || COALESCE(withdrawal_days::text, ''),
        'eligibility_json=' || pg_temp.goatos_canonical_json(COALESCE(eligibility_json, 'null'::jsonb)),
        'proof_policy=' || pg_temp.goatos_canonical_json(COALESCE(proof_policy, 'null'::jsonb))
      )::bytea,
      'sha256'
    ),
    'hex'
  )
$$;

ALTER TABLE public.protocol_rules DISABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rules pr
SET dose_code = 'blue_tongue_kid_19w',
    offset_days = 133,
    min_gap_days = 21
WHERE pr.dose_code = 'blue_tongue_kid_20w'
  AND pr.offset_days = 140
  AND pr.min_gap_days = 28
  AND lower(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) IN ('blue_tongue', 'blue tongue')
  AND lower(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'name', 'blue tongue'))) = 'blue tongue'
  AND (
    lower(btrim(COALESCE(pr.eligibility_json->'eligibility'->>'species', ''))) = 'sheep'
    OR EXISTS (
      SELECT 1
      FROM jsonb_array_elements_text(
        CASE jsonb_typeof(pr.eligibility_json->'eligibility'->'species')
          WHEN 'array' THEN pr.eligibility_json->'eligibility'->'species'
          ELSE '[]'::jsonb
        END
      ) AS species(value)
      WHERE lower(btrim(species.value)) = 'sheep'
    )
  );

ALTER TABLE public.protocol_rules ENABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rule_dimensions prd
SET dose_code = 'blue_tongue_kid_19w',
    source_dose_code = CASE
      WHEN source_dose_code = 'blue_tongue_kid_20w' THEN 'blue_tongue_kid_19w'
      ELSE source_dose_code
    END,
    offset_days = 133,
    min_gap_days = 21,
    schedule_json = jsonb_set(
      jsonb_set(
        jsonb_set(
          jsonb_set(schedule_json, '{dose_code}', to_jsonb('blue_tongue_kid_19w'::text), true),
          '{source_dose_code}', to_jsonb('blue_tongue_kid_19w'::text), true
        ),
        '{offset_days}', to_jsonb(133), true
      ),
      '{min_gap_days}', to_jsonb(21), true
    )
WHERE prd.vaccine_code = 'BLUE_TONGUE'
  AND prd.dose_code = 'blue_tongue_kid_20w'
  AND prd.offset_days = 140
  AND prd.min_gap_days = 28
  AND prd.species = 'sheep';

UPDATE public.protocol_rule_lineage prl
SET identity_key = '11:blue_tongue|19:blue_tongue_kid_19w|1:2',
    content_fingerprint = pg_temp.goatos_rule_content_fingerprint(
      pr.trigger_type,
      pr.offset_days,
      pr.due_window_days,
      pr.min_gap_days,
      pr.repeat,
      pr.repeat_until_after_age,
      pr.catch_up,
      pr.sop_version_id,
      pr.withdrawal_days,
      pr.eligibility_json,
      pr.proof_policy
    )
FROM public.protocol_rules pr
WHERE prl.tenant_id = pr.tenant_id
  AND prl.rule_id = pr.rule_id
  AND pr.dose_code = 'blue_tongue_kid_19w'
  AND pr.offset_days = 133
  AND pr.min_gap_days = 21
  AND prl.identity_key = '11:blue_tongue|19:blue_tongue_kid_20w|1:2';

-- +goose Down
ALTER TABLE public.protocol_rules DISABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rules pr
SET dose_code = 'blue_tongue_kid_20w',
    offset_days = 140,
    min_gap_days = 28
WHERE pr.dose_code = 'blue_tongue_kid_19w'
  AND pr.offset_days = 133
  AND pr.min_gap_days = 21
  AND lower(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'code', ''))) IN ('blue_tongue', 'blue tongue')
  AND lower(btrim(COALESCE(pr.eligibility_json->'vaccine'->>'name', 'blue tongue'))) = 'blue tongue'
  AND (
    lower(btrim(COALESCE(pr.eligibility_json->'eligibility'->>'species', ''))) = 'sheep'
    OR EXISTS (
      SELECT 1
      FROM jsonb_array_elements_text(
        CASE jsonb_typeof(pr.eligibility_json->'eligibility'->'species')
          WHEN 'array' THEN pr.eligibility_json->'eligibility'->'species'
          ELSE '[]'::jsonb
        END
      ) AS species(value)
      WHERE lower(btrim(species.value)) = 'sheep'
    )
  );

ALTER TABLE public.protocol_rules ENABLE TRIGGER protocol_rules_require_draft_version_trg;

UPDATE public.protocol_rule_dimensions prd
SET dose_code = 'blue_tongue_kid_20w',
    source_dose_code = CASE
      WHEN source_dose_code = 'blue_tongue_kid_19w' THEN 'blue_tongue_kid_20w'
      ELSE source_dose_code
    END,
    offset_days = 140,
    min_gap_days = 28,
    schedule_json = jsonb_set(
      jsonb_set(
        jsonb_set(
          jsonb_set(schedule_json, '{dose_code}', to_jsonb('blue_tongue_kid_20w'::text), true),
          '{source_dose_code}', to_jsonb('blue_tongue_kid_20w'::text), true
        ),
        '{offset_days}', to_jsonb(140), true
      ),
      '{min_gap_days}', to_jsonb(28), true
    )
WHERE prd.vaccine_code = 'BLUE_TONGUE'
  AND prd.dose_code = 'blue_tongue_kid_19w'
  AND prd.offset_days = 133
  AND prd.min_gap_days = 21
  AND prd.species = 'sheep';

UPDATE public.protocol_rule_lineage prl
SET identity_key = '11:blue_tongue|19:blue_tongue_kid_20w|1:2',
    content_fingerprint = pg_temp.goatos_rule_content_fingerprint(
      pr.trigger_type,
      pr.offset_days,
      pr.due_window_days,
      pr.min_gap_days,
      pr.repeat,
      pr.repeat_until_after_age,
      pr.catch_up,
      pr.sop_version_id,
      pr.withdrawal_days,
      pr.eligibility_json,
      pr.proof_policy
    )
FROM public.protocol_rules pr
WHERE prl.tenant_id = pr.tenant_id
  AND prl.rule_id = pr.rule_id
  AND pr.dose_code = 'blue_tongue_kid_20w'
  AND pr.offset_days = 140
  AND pr.min_gap_days = 28
  AND prl.identity_key = '11:blue_tongue|19:blue_tongue_kid_19w|1:2';
