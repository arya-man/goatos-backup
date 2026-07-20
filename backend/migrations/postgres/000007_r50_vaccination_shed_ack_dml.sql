-- +goose Up
-- Vaccination shed completion = acknowledgement, not a manual medical form (R50-shed-ack).
--
-- The operator already does the real work at ANIMAL level: scan each goat's RFID + attach one
-- live camera proof clip per goat row (mig 000006). Shed completion is only a final
-- acknowledgement that every expected animal in the shed has been scanned and proofed — there is
-- nothing left to fill in manually. This migration strips the remaining manual-medical-form
-- fields (vaccine batch, cold chain, dose, route/site, administered-at, adverse reaction) and
-- their block/required rules from the vaccination.drive / vaccination.session SOP versions'
-- form_dsl. `administered_at` becomes a server-derived value (submit time), never an operator
-- answer; `dose_ml_given` / `route_site` are no longer collected; `cold_chain_verified` no
-- longer gates submission; `adverse_reaction[_notes]` is no longer a form field (adverse events
-- are handled via the existing problem-report path, tracked separately from the acknowledgement).
--
-- Idempotent: guarded on presence of 'cold_chain_verified' in the SOP version's fields, so a
-- clean install (already stripped by 000001's own tail rewrite) or a prior run of this migration
-- is a no-op.

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb)) AS f(field)
      WHERE field ->> 'key' IN (
        'vaccine_lot_id', 'cold_chain_verified', 'dose_ml_given', 'route_site',
        'administered_at', 'adverse_reaction', 'adverse_reaction_notes'
      )
    )
),
rewritten AS (
  SELECT
    sv.sop_version_id,
    jsonb_set(
      jsonb_set(
        sv.form_dsl,
        '{fields}',
        COALESCE((
          SELECT jsonb_agg(field ORDER BY ordinal)
          FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb))
            WITH ORDINALITY AS entries(field, ordinal)
          WHERE field ->> 'key' NOT IN (
            'vaccine_lot_id', 'cold_chain_verified', 'dose_ml_given', 'route_site',
            'administered_at', 'adverse_reaction', 'adverse_reaction_notes'
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
          'cold_chain_verified', 'adverse_reaction_notes'
        )
        AND COALESCE(rule -> 'when' ->> 'field', '') NOT IN (
          'cold_chain_verified', 'adverse_reaction'
        )
      ), '[]'::jsonb),
      true
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = rewritten.form_dsl,
    updated_at = now()
FROM rewritten
WHERE sv.sop_version_id = rewritten.sop_version_id;

-- Relax vaccination_completions NOT NULL constraints that assumed a filled manual answer.
-- administered_at keeps a server-side default (submit/scan time) so it is never actually NULL in
-- practice, but the column no longer needs to reject a NULL insert if a caller derives it later
-- in the same transaction. Lock-safe: SET NOT NULL is being dropped (cheap catalog-only change,
-- does not require a table scan); adverse_reaction/cold_chain_verified already default false and
-- are left as-is since they are booleans with harmless defaults, not fillable answers anymore.
ALTER TABLE public.vaccination_completions
  ALTER COLUMN administered_at DROP NOT NULL,
  ALTER COLUMN administered_at SET DEFAULT now();

-- +goose Down
-- Best-effort revert: restore NOT NULL on administered_at (backfilling any NULLs to now() first
-- so the constraint can be re-added), and re-add the manual-medical fields/rules to the SOP
-- versions' form_dsl in their pre-shed-ack shape. Lossy: does not restore historical answer data.

UPDATE public.vaccination_completions
SET administered_at = now()
WHERE administered_at IS NULL;

ALTER TABLE public.vaccination_completions
  ALTER COLUMN administered_at DROP DEFAULT,
  ALTER COLUMN administered_at SET NOT NULL;

WITH vaccination_sops AS (
  SELECT sv.sop_version_id
  FROM public.sop_versions sv
  JOIN public.sop_definitions sd
    ON sd.tenant_id = sv.tenant_id
   AND sd.sop_id = sv.sop_id
  WHERE sd.code IN ('vaccination.drive', 'vaccination.session')
    AND NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb)) AS f(field)
      WHERE field ->> 'key' = 'cold_chain_verified'
    )
),
reverted AS (
  SELECT
    sv.sop_version_id,
    jsonb_set(
      jsonb_set(
        sv.form_dsl,
        '{fields}',
        COALESCE(sv.form_dsl -> 'fields', '[]'::jsonb) || jsonb_build_array(
          jsonb_build_object('key', 'vaccine_lot_id', 'type', 'vaccine_batch_picker', 'label', 'Vaccine batch', 'required', true, 'option_source', 'inventory.vaccine_lots.fefo'),
          jsonb_build_object('key', 'cold_chain_verified', 'type', 'boolean', 'label', 'Cold chain verified', 'required', true),
          jsonb_build_object('key', 'dose_ml_given', 'type', 'number', 'label', 'Dose given', 'required', true),
          jsonb_build_object('key', 'route_site', 'type', 'select', 'label', 'Route / site', 'required', true, 'option_source', 'vaccination.route_sites'),
          jsonb_build_object('key', 'administered_at', 'type', 'date_time', 'label', 'Administered at', 'required', true),
          jsonb_build_object('key', 'adverse_reaction', 'type', 'boolean', 'label', 'Adverse reaction observed', 'required', true),
          jsonb_build_object('key', 'adverse_reaction_notes', 'type', 'text', 'label', 'Adverse reaction notes', 'required', false)
        ),
        true
      ),
      '{rules}',
      COALESCE(sv.form_dsl -> 'rules', '[]'::jsonb) || jsonb_build_array(
        jsonb_build_object('type', 'block_submission_if', 'when', jsonb_build_object('field', 'cold_chain_verified', 'value', false, 'operator', 'equals'), 'message', 'Cold chain must be verified before submitting the drive.'),
        jsonb_build_object('type', 'required_if', 'when', jsonb_build_object('field', 'adverse_reaction', 'value', true, 'operator', 'equals'), 'field', 'adverse_reaction_notes', 'message', 'Adverse reaction notes are required when a reaction is observed.')
      ),
      true
    ) AS form_dsl
  FROM public.sop_versions sv
  JOIN vaccination_sops ids ON ids.sop_version_id = sv.sop_version_id
)
UPDATE public.sop_versions sv
SET form_dsl = reverted.form_dsl,
    updated_at = now()
FROM reverted
WHERE sv.sop_version_id = reverted.sop_version_id;
