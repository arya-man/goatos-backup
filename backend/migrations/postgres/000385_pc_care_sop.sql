-- +goose Up
-- 000385_pc_care_sop.sql
--
-- PC CARE SOP (maintainer decision 2026-09-22, docs/decisions/pc-care-sop.md).
-- The five hands-on-the-animal PC Care categories (deworming, anti protozoan, ticks removal,
-- hoof trimming, hair trimming) stop running on a slot table typed into Go. What the operator
-- captures per animal, the questions answered at submit, and whether a tablet-in-feed
-- deworming carries the evening-before feed & water removal (required / OPTIONAL / off, which
-- categories, the SOP's own evening, the removal card's captures and questions) are the
-- `pc_care` section of the PUBLISHED pc_care.tasks version, authored on /pc-care/sops.
--
-- Rollout-safe, every change:
--
--   1. pc_care_tasks.sop_version / pc_care_rounds.sop_version pin the version a task was
--      PLANNED on; NULL on every existing task = the seeded rules, which are the pre-SOP
--      behaviour byte for byte (removal optional on deworming, one video per animal on the
--      2-second jobs, before / while / after on the trimming jobs, no questions).
--   2. pc_care_tasks.required_slot_keys snapshots the compulsory per-animal slot keys of the
--      pinned version at create, so every readiness read (submit, Work Board, the kernel) is
--      one set-based jsonb predicate and never resolves rules per task. Backfilled from the
--      legacy category table for existing tasks.
--   3. pc_care_task_animals.sop_proofs {slot key: proof ref} + sop_proof_meta {slot key:
--      {captured_by, captured_at, kind}} replace the four fixed proof columns as the source of
--      truth; the legacy columns mirror the seeded keys (video, before_video, during_video,
--      after_video) and are backfilled into the map.
--   4. pc_care_removal_pen_proofs.sop_proofs {slot key: proof ref}; feed_proof_ref /
--      water_proof_ref mirror the seeded feed_video / water_video slots.
--   5. pc_care_tasks.sop_answers holds the answers to the pinned version's questions, given
--      once per task at submit; '{}' on every existing row (the seed asks none).
--   6. pc_care_task_proofs.slot_key is no longer limited to the four fixed keys: the removal
--      card's slots are authored.
--   7. The pc_care.tasks definition + v1 (the seed verbatim, pinned by
--      TestMigrationEmbedsTheSeededPCCareSOP) for every tenant. There was no PC Care SOP
--      before this migration, so there is no in-place add.
--
-- seed-fixture-guard:ignore: SOP library document + nullable/defaulted pc_care columns;
-- no vaccination / HRMS / goats schema moves.

ALTER TABLE public.pc_care_tasks
  ADD COLUMN IF NOT EXISTS sop_version integer,
  ADD COLUMN IF NOT EXISTS slot_keys text[] NOT NULL DEFAULT '{}'::text[],
  ADD COLUMN IF NOT EXISTS required_slot_keys text[] NOT NULL DEFAULT '{}'::text[],
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.pc_care_tasks.sop_version IS
  'pc_care.tasks SOP version the task was PLANNED on and runs under to the end (PC CARE SOP, 2026-09-22). NULL = the seeded rules (pre-SOP behaviour).';
COMMENT ON COLUMN public.pc_care_tasks.slot_keys IS
  'EVERY capture slot key of the pinned version''s card for this task, compulsory or not, snapshotted at create: the store refuses a capture aimed at a slot the card does not ask for, under the task row lock.';
COMMENT ON COLUMN public.pc_care_tasks.required_slot_keys IS
  'The compulsory capture slot keys of the pinned version for this task''s category (per animal; for a removal card, per pen), snapshotted at create so readiness is one jsonb predicate (sop_proofs ?& required_slot_keys).';
COMMENT ON COLUMN public.pc_care_tasks.sop_answers IS
  'The operators'' answers to the pinned version''s questions, given once per task at submit, keyed by question id.';

ALTER TABLE public.pc_care_rounds
  ADD COLUMN IF NOT EXISTS sop_version integer;
COMMENT ON COLUMN public.pc_care_rounds.sop_version IS
  'pc_care.tasks SOP version the round was PLANNED on; every pen task and the removal card carry the same pin.';

-- Existing tasks ran the seeded slot table; snapshot its keys so their readiness predicate and
-- their accepted slot set are exactly what they were. Every seeded slot is compulsory, so the two
-- lists are identical on a backfilled row; they diverge only where a farm authors an OPTIONAL one.
UPDATE public.pc_care_tasks
SET slot_keys = CASE category
  WHEN 'deworming' THEN ARRAY['video']
  WHEN 'anti_protozoan' THEN ARRAY['video']
  WHEN 'ticks_removal' THEN ARRAY['video']
  WHEN 'hoof_trimming' THEN ARRAY['before_video', 'during_video', 'after_video']
  WHEN 'hair_trimming' THEN ARRAY['before_video', 'during_video', 'after_video']
  WHEN 'feed_water_removal' THEN ARRAY['feed_video', 'water_video']
  WHEN 'inventory_vaccine' THEN ARRAY['stock_fridge_photo', 'stock_fridge_video']
  ELSE '{}'::text[]
END
WHERE slot_keys = '{}'::text[];

UPDATE public.pc_care_tasks
SET required_slot_keys = CASE category
  WHEN 'deworming' THEN ARRAY['video']
  WHEN 'anti_protozoan' THEN ARRAY['video']
  WHEN 'ticks_removal' THEN ARRAY['video']
  WHEN 'hoof_trimming' THEN ARRAY['before_video', 'during_video', 'after_video']
  WHEN 'hair_trimming' THEN ARRAY['before_video', 'during_video', 'after_video']
  WHEN 'feed_water_removal' THEN ARRAY['feed_video', 'water_video']
  WHEN 'inventory_vaccine' THEN ARRAY['stock_fridge_photo', 'stock_fridge_video']
  ELSE '{}'::text[]
END
WHERE required_slot_keys = '{}'::text[];

ALTER TABLE public.pc_care_task_animals
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS sop_proof_meta jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.pc_care_task_animals.sop_proofs IS
  '{slot key: proof ref} for every capture the pinned version asks for on this animal (PC CARE SOP, 2026-09-22). video_/before_/during_/after_proof_ref mirror the seeded keys.';
COMMENT ON COLUMN public.pc_care_task_animals.sop_proof_meta IS
  '{slot key: {captured_by, captured_at, kind}} beside sop_proofs: who took each capture, when, and which kind the proof register judged it (video / photo).';
UPDATE public.pc_care_task_animals
SET sop_proofs = (
  CASE WHEN video_proof_ref IS NOT NULL THEN jsonb_build_object('video', video_proof_ref) ELSE '{}'::jsonb END
  || CASE WHEN before_proof_ref IS NOT NULL THEN jsonb_build_object('before_video', before_proof_ref) ELSE '{}'::jsonb END
  || CASE WHEN during_proof_ref IS NOT NULL THEN jsonb_build_object('during_video', during_proof_ref) ELSE '{}'::jsonb END
  || CASE WHEN after_proof_ref IS NOT NULL THEN jsonb_build_object('after_video', after_proof_ref) ELSE '{}'::jsonb END
),
sop_proof_meta = (
  CASE WHEN video_proof_ref IS NOT NULL THEN jsonb_build_object('video', jsonb_build_object('captured_by', video_captured_by, 'captured_at', video_captured_at, 'kind', 'video')) ELSE '{}'::jsonb END
  || CASE WHEN before_proof_ref IS NOT NULL THEN jsonb_build_object('before_video', jsonb_build_object('captured_by', before_captured_by, 'captured_at', before_captured_at, 'kind', 'video')) ELSE '{}'::jsonb END
  || CASE WHEN during_proof_ref IS NOT NULL THEN jsonb_build_object('during_video', jsonb_build_object('captured_by', during_captured_by, 'captured_at', during_captured_at, 'kind', 'video')) ELSE '{}'::jsonb END
  || CASE WHEN after_proof_ref IS NOT NULL THEN jsonb_build_object('after_video', jsonb_build_object('captured_by', after_captured_by, 'captured_at', after_captured_at, 'kind', 'video')) ELSE '{}'::jsonb END
)
WHERE sop_proofs = '{}'::jsonb;

ALTER TABLE public.pc_care_removal_pen_proofs
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.pc_care_removal_pen_proofs.sop_proofs IS
  '{slot key: proof ref} for every capture the pinned version''s removal card asks for on this pen (PC CARE SOP, 2026-09-22). feed_proof_ref / water_proof_ref mirror the seeded feed_video / water_video slots.';
UPDATE public.pc_care_removal_pen_proofs
SET sop_proofs = (
  CASE WHEN feed_proof_ref IS NOT NULL AND feed_proof_ref <> '' THEN jsonb_build_object('feed_video', feed_proof_ref) ELSE '{}'::jsonb END
  || CASE WHEN water_proof_ref IS NOT NULL AND water_proof_ref <> '' THEN jsonb_build_object('water_video', water_proof_ref) ELSE '{}'::jsonb END
)
WHERE sop_proofs = '{}'::jsonb;

-- The slot vocabulary of a task-level proof is the pinned document's now, not a fixed list.
ALTER TABLE public.pc_care_task_proofs
  DROP CONSTRAINT IF EXISTS pc_care_task_proofs_slot_check;
ALTER TABLE public.pc_care_task_proofs
  ADD CONSTRAINT pc_care_task_proofs_slot_check
  CHECK (slot_key ~ '^[a-z][a-z0-9_]{0,47}$') NOT VALID;

-- seed-migration-guard:ignore owner=claude issue=pc-care-sop reason=library-document seed for an existing flow; idempotent definition + version insert, no read-model or clean-slate change expiry=2026-12-31
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'pc_care.tasks', 'Preventive Care tasks',
       'Deworming, anti protozoan, ticks removal, hoof trimming and hair trimming: what the operator captures per animal, the questions answered at submit, and whether a tablet-in-feed deworming removes feed and water the evening before (required, optional or off, which categories, the evening, the removal card''s captures and questions). The capture mode, free-flow scan, whole-task submit and per-task verification stay the module''s.',
       'active', 'action', 'module', 'pc_care'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

-- seed-migration-guard:ignore owner=claude issue=pc-care-sop reason=library-document seed for an existing flow; idempotent definition + version insert, no read-model or clean-slate change expiry=2026-12-31
INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Preventive Care tasks v1', 'published',
jsonb_build_object(
  'schema_version', 'goatos.sop-form.v1',
  'sop_code', 'pc_care.tasks',
  'title', 'Preventive Care tasks',
  'fields', jsonb_build_array(),
  'pc_care', $seed${
  "schema_version": "goatos.sop-pc-care.v1",
  "feed_water_removal": {
    "mode": "optional",
    "applies_to": ["deworming"],
    "cutoff_time": "",
    "instruction": "Remove feed and water from every pen in this round the evening before the deworming. Film the feed being removed and the water being removed as two separate live-camera videos per pen and submit before midnight.",
    "proofs": [
      {"key": "feed_video", "title": "Feed removal video", "hint": "Show the feed being taken out of this pen", "kind": "video", "required": true},
      {"key": "water_video", "title": "Water removal video", "hint": "Show the water being taken out of this pen", "kind": "video", "required": true}
    ],
    "questions": []
  },
  "categories": {
    "deworming": {
      "instruction": "Scan each animal and film the dose being given.",
      "proofs": [
        {"key": "video", "title": "Deworming video", "hint": "Show the dose being given to this animal", "kind": "video", "required": true}
      ],
      "questions": []
    },
    "anti_protozoan": {
      "instruction": "Scan each animal and film the dose being given.",
      "proofs": [
        {"key": "video", "title": "Anti Protozoan video", "hint": "Show the dose being given to this animal", "kind": "video", "required": true}
      ],
      "questions": []
    },
    "ticks_removal": {
      "instruction": "Scan each animal and film the ticks being removed.",
      "proofs": [
        {"key": "video", "title": "Ticks removal video", "hint": "Show the ticks being removed from this animal", "kind": "video", "required": true}
      ],
      "questions": []
    },
    "hoof_trimming": {
      "instruction": "Pick each animal off the pen roster and record the three clips: before, while and after the trimming.",
      "proofs": [
        {"key": "before_video", "title": "Before trimming", "hint": "Show the animal's hooves before the work", "kind": "video", "required": true},
        {"key": "during_video", "title": "While trimming", "hint": "Record the hooves being trimmed", "kind": "video", "required": true, "min_seconds": 10},
        {"key": "after_video", "title": "After trimming", "hint": "Show the trimmed hooves after the work", "kind": "video", "required": true}
      ],
      "questions": []
    },
    "hair_trimming": {
      "instruction": "Pick each animal off the pen roster and record the three clips: before, while and after the trimming.",
      "proofs": [
        {"key": "before_video", "title": "Before trimming", "hint": "Show the animal's coat before the work", "kind": "video", "required": true},
        {"key": "during_video", "title": "While trimming", "hint": "Record the hair being trimmed", "kind": "video", "required": true, "min_seconds": 10},
        {"key": "after_video", "title": "After trimming", "hint": "Show the trimmed coat after the work", "kind": "video", "required": true}
      ],
      "questions": []
    }
  }
}$seed$::jsonb
),
'{"subject_scope": "goat", "types": ["video", "photo"], "required": true, "minimum_count": 1, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture", "photo.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the PC Care slot table as the pc_care rules section (2026-09-22)."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'pc_care.tasks'
  AND NOT EXISTS (
    SELECT 1 FROM public.sop_versions v
    WHERE v.tenant_id = sd.tenant_id AND v.sop_id = sd.sop_id AND v.status = 'published'
  )
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
-- Forward-only on the document (a version is history). The columns are dropped; the legacy
-- proof columns still carry the seeded captures, so nothing pre-existing is lost.
ALTER TABLE public.pc_care_task_proofs DROP CONSTRAINT IF EXISTS pc_care_task_proofs_slot_check;
ALTER TABLE public.pc_care_removal_pen_proofs DROP COLUMN IF EXISTS sop_proofs;
ALTER TABLE public.pc_care_task_animals DROP COLUMN IF EXISTS sop_proof_meta, DROP COLUMN IF EXISTS sop_proofs;
ALTER TABLE public.pc_care_rounds DROP COLUMN IF EXISTS sop_version;
ALTER TABLE public.pc_care_tasks DROP COLUMN IF EXISTS sop_answers, DROP COLUMN IF EXISTS required_slot_keys, DROP COLUMN IF EXISTS slot_keys, DROP COLUMN IF EXISTS sop_version;
