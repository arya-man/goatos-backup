-- +goose Up
-- Vaccination proof mode is SOP-controlled. Current Mesha staging/default SOP uses one shed-level
-- video bundle (1 mandatory, up to 5 total) captured from camera or gallery on the shed submit
-- screen. Per-goat video remains a valid SOP mode; this migration changes the published
-- vaccination defaults, it does not delete the per-goat code path.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    (
      jsonb_set(
        jsonb_set(
          (sv.form_dsl - 'goat_row_proof') || jsonb_build_object(
            'shed_video',
            jsonb_build_object(
              'subject_scope', 'shed',
              'capture_source', 'in_app_camera',
              'allowed_capture_sources', jsonb_build_array('in_app_camera', 'gallery_picker'),
              'minimum_clips', 1,
              'maximum_clips', 5
            )
          ),
          '{fields}',
          COALESCE((
            SELECT jsonb_agg(field ORDER BY ordinal)
            FROM (
              SELECT field, ordinal
              FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
                WITH ORDINALITY AS entries(field, ordinal)
              WHERE field ->> 'key' <> 'goat_row_proof'
                AND field ->> 'key' <> 'shed_video'
                AND field ->> 'key' <> 'goat_ids'
              UNION ALL
              SELECT jsonb_build_object(
                'key', 'goat_ids',
                'label', 'Goats vaccinated',
                'type', 'goat_scan',
                'required', true,
                'repeat', true,
                'description', 'Scan each goat RFID exactly when the vaccine is given. The scan timestamp is the vaccination timestamp.'
              ), 9998
              UNION ALL
              SELECT jsonb_build_object(
                'key', 'shed_video',
                'label', 'Shed proof videos',
                'type', 'video_proof',
                'required', true,
                'repeat', true,
                'proof_subject', 'shed',
                'help_text', 'Add 1 required shed-level video before submit; camera or gallery allowed, up to 5 videos.'
              ), 9999
            ) fields
          ), '[]'::jsonb),
          true
        ),
        '{rules}',
        COALESCE((sv.form_dsl -> 'rules'), '[]'::jsonb),
        true
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
      'proof_mode', 'shed_level_video',
      'subject_scope', 'shed',
      'expected_subjects', jsonb_build_array('shed'),
      'minimum_count', 1,
      'maximum_count', 5,
      'maximum_count_per_subject', 5,
      'capture_source', 'in_app_camera',
      'allowed_capture_sources', jsonb_build_array('in_app_camera', 'gallery_picker'),
      'verify_capability', 'proof.verify',
      'verify_before_apply', true,
      'retention_policy', 'operational_90d'
    ),
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;

-- +goose Down
-- Revert to the prior per-goat video proof default.
WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    jsonb_set((sv.form_dsl - 'shed_video') || jsonb_build_object(
      'goat_row_proof',
      jsonb_build_object(
        'subject_scope', 'goat',
        'capture_source', 'in_app_camera',
        'minimum_clips', 1,
        'maximum_clips', 5,
        'one_clip_covers_same_handling_vaccines', true
      )
    ), '{fields}', COALESCE((
      SELECT jsonb_agg(field ORDER BY ordinal)
      FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
        WITH ORDINALITY AS entries(field, ordinal)
      WHERE field ->> 'key' <> 'shed_video'
    ), '[]'::jsonb), true) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    proof_policy = jsonb_build_object(
      'types', jsonb_build_array('video'),
      'required', true,
      'proof_mode', 'per_goat_video',
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
