-- +goose Up
-- Vaccination SOP form_dsl/proof_policy rewrite (moved from 000003 for lock safety).
--
-- Heavy DML that rewrites form_dsl/proof_policy for vaccination.drive / vaccination.session
-- SOP versions from batch-level proof to per-goat-row proof capture. This was originally
-- in 000003_r50_forward_compatibility.sql but has been separated to release DDL locks quickly
-- on hot tables (000003 now holds locks only for fast DDL/table creates). (R50-015 P0)
--
-- Guarded by subject_scope so a clean install (already migrated inside 000001's own tail),
-- a prior run of 000003, or a run of this migration is a no-op when already applied.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND COALESCE(sv.proof_policy ->> 'subject_scope', '') <> 'goat'
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    jsonb_set(
      jsonb_set(
        sv.form_dsl,
        '{fields}',
        COALESCE((
          SELECT jsonb_agg(
            CASE
              WHEN field ->> 'key' = 'goat_ids' THEN
                jsonb_set(
                  field,
                  '{description}',
                  to_jsonb('Scan each goat RFID exactly when the vaccine is given. The scan timestamp is the vaccination timestamp.'::text),
                  true
                )
              ELSE field
            END
            ORDER BY ordinal
          )
          FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
            WITH ORDINALITY AS entries(field, ordinal)
          WHERE field ->> 'key' NOT IN (
            'shed_video', 'vial_lot_video', 'administration_video',
            'extra_video_1', 'extra_video_1_caption', 'extra_video_2', 'extra_video_2_caption'
          )
        ), '[]'::jsonb),
        true
      ),
      '{rules}',
      COALESCE((
        SELECT jsonb_agg(rule ORDER BY ordinal)
        FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'rules', '[]'::jsonb))
          WITH ORDINALITY AS entries(rule, ordinal)
        WHERE COALESCE(rule ->> 'field', '') NOT IN (
          'shed_video', 'vial_lot_video', 'administration_video',
          'extra_video_1_caption', 'extra_video_2_caption'
        )
      ), '[]'::jsonb),
      true
    ) || jsonb_build_object(
      'goat_row_proof',
      jsonb_build_object(
        'subject_scope', 'goat',
        'capture_source', 'in_app_camera',
        'minimum_clips', 1,
        'maximum_clips', 5,
        'one_clip_covers_same_handling_vaccines', true
      )
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'subject_scope', 'goat',
      'expected_subjects', jsonb_build_array('goat'),
      'minimum_count', 1,
      'minimum_count_per_subject', 1,
      'maximum_count_per_subject', 5,
      'capture_source', 'in_app_camera',
      'one_clip_covers_same_handling_vaccines', true,
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;

-- +goose Down
-- Best-effort revert of the goat-scan SOP rewrite back to a batch-level proof shape.
-- Lossy: restores an approximate pre-goat-scan form_dsl/proof_policy shape for the
-- vaccination.drive / vaccination.session SOP versions rather than replaying the exact
-- historical batch-level JSON.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND COALESCE(sv.proof_policy ->> 'subject_scope', '') = 'goat'
),
reverted AS (
  SELECT
    sv.sop_version_id,
    (sv.form_dsl - 'goat_row_proof') AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = reverted.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'subject_scope', 'batch',
      'expected_subjects', jsonb_build_array('shed', 'vial_lot', 'administration'),
      'minimum_count', 3,
      'maximum_count', 5,
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    updated_at = now()
FROM reverted
WHERE sv.sop_version_id = reverted.sop_version_id;
