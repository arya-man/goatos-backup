-- +goose Up
-- 000314_weighing_sop_rules.sql
--
-- WEIGHING SOP (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
-- The Weighing Session SOP stops being a library document (000186): the rules a weighing
-- task runs under -- the capture modes the planner may pick, the default cap per day,
-- whether the evening-before feed & water removal is REQUIRED / OPTIONAL / OFF, the removal
-- card's instruction and proof-slot copy, the questions the removal operator answers, and
-- how many videos a lump-sum pen carries -- are the `weighing` section of the PUBLISHED
-- weighing.session version, authored on /weighing/sops.
--
-- Three changes, each rollout-safe:
--
--   1. weighing_campaigns.sop_version pins the version a task was PLANNED on; it runs on that
--      version to the end. NULL on every existing task = the seeded rules, which are the
--      pre-SOP behaviour byte for byte (removal required, both clips, cap 100, 1..5 videos).
--   2. weighing_fasting_shed_proofs.sop_answers stores the removal operator's answers to the
--      SOP's authored questions; '{}' on every existing row (the seed asks none).
--   3. The seeded `weighing` section is added IN PLACE to each tenant's currently PUBLISHED
--      weighing.session version (the 000308 shape), so the version a farm sees on deploy is
--      the one it already had, now carrying the rules; a tenant with no weighing.session yet
--      gets the 000186 definition + v1 with the section. The document below is the embedded
--      seed verbatim (pinned by TestMigrationEmbedsTheSeededWeighingSOP).
--
-- seed-fixture-guard:ignore: SOP library document + two nullable/defaulted weighing columns;
-- no vaccination / HRMS / goats schema moves.

ALTER TABLE public.weighing_campaigns
  ADD COLUMN IF NOT EXISTS sop_version integer;
COMMENT ON COLUMN public.weighing_campaigns.sop_version IS
  'weighing.session SOP version the task was PLANNED on and runs under to the end (WEIGHING SOP, 2026-09-15). NULL = the seeded rules (pre-SOP behaviour).';

ALTER TABLE public.weighing_fasting_shed_proofs
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.weighing_fasting_shed_proofs.sop_answers IS
  'The removal operator''s answers to the weighing SOP''s authored removal questions, keyed by question id, validated against the task''s pinned version at submit.';

-- seed-migration-guard:ignore owner=claude issue=weighing-sop reason=library-document seed for an existing flow; idempotent definition + version insert and an in-place section add on the published version, no read-model or clean-slate change expiry=2026-10-31
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'weighing.session', 'Weighing Session',
       'Scan-and-submit weighing. The CEO assigns pens; the operator (or Growth Director) weighs them: individual is scan RFID + weight + video per animal, lump-sum is total weight + video(s) per pen. The only business rule is that an animal cannot be scanned twice in the same bucket before submit. The SOP decides the capture modes offered, the default cap, whether feed and water are removed the evening before (and what the removal card asks), and how many videos a lump-sum pen carries.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

-- A tenant with no published weighing.session at all gets v1 = the 000186 document plus the
-- seeded section. (Every tenant that existed at 000186 already has one and is patched below.)
-- seed-migration-guard:ignore owner=claude issue=weighing-sop reason=library-document seed for an existing flow; idempotent definition + version insert and an in-place section add on the published version, no read-model or clean-slate change expiry=2026-10-31
INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Weighing Session v1', 'published',
jsonb_build_object(
  'schema_version', 'goatos.sop-form.v1',
  'sop_code', 'weighing.session',
  'title', 'Weighing Session',
  'fields', '[
    {"key": "mode", "type": "select", "label": "Weighing mode", "options": ["individual", "lump_sum"], "required": true},
    {"key": "rfid", "type": "animal_id_scan", "label": "Animal RFID (individual)", "repeat": true, "required": false},
    {"key": "weight_kg", "type": "number", "label": "Weight (kg) per animal (individual)", "required": false, "min": 0},
    {"key": "animal_video", "type": "video_proof", "label": "Per-animal proof video (individual)", "required": false, "proof_action": "video.capture"},
    {"key": "total_weight_kg", "type": "number", "label": "Total weight (kg) (lump-sum)", "required": false, "min": 0},
    {"key": "weighing_lump_sum_video", "type": "video_proof", "label": "Pen proof video(s) (lump-sum)", "required": false, "proof_action": "video.capture"}
  ]'::jsonb,
  'rules', '[]'::jsonb,
  'weighing', $seed${
  "schema_version": "goatos.sop-weighing.v1",
  "planning": {
    "modes": ["individual_animal", "per_shed_partition"],
    "default_cap_per_day": 100
  },
  "feed_water_removal": {
    "mode": "required",
    "instruction": "Remove feed and water from every selected pen the evening before the weighing. Film the feed being removed and the water being removed as two separate live-camera videos per pen and submit each pen before midnight.",
    "proofs": [
      {"key": "feed_video", "title": "Feed removed", "hint": "Live-camera video of this pen's feed being taken away.", "kind": "video", "required": true},
      {"key": "water_video", "title": "Water removed", "hint": "Live-camera video of this pen's water being taken away.", "kind": "video", "required": true}
    ],
    "questions": []
  },
  "capture": {
    "individual": {"video_required": true},
    "lump_sum": {"video_min": 1, "video_max": 5}
  }
}$seed$::jsonb
),
'{"subject_scope": "goat", "lump_sum_subject_scope": "shed", "types": ["video"], "required": true, "minimum_count": 1, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the locked scan-and-submit weighing model with the weighing SOP rules section (2026-09-15)."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'weighing.session'
  AND NOT EXISTS (
    SELECT 1 FROM public.sop_versions v
    WHERE v.tenant_id = sd.tenant_id AND v.sop_id = sd.sop_id AND v.status = 'published'
  )
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- Add the seeded section IN PLACE to every published weighing.session version that lacks it.
-- The published version keeps its number: no task is pinned to a version before this
-- migration, and the seeded rules are exactly what those tasks already do.
-- seed-migration-guard:ignore owner=claude issue=weighing-sop reason=in-place section add on the published library document; the seeded rules reproduce the pre-SOP behaviour expiry=2026-10-31
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl || jsonb_build_object('weighing', $seed${
  "schema_version": "goatos.sop-weighing.v1",
  "planning": {
    "modes": ["individual_animal", "per_shed_partition"],
    "default_cap_per_day": 100
  },
  "feed_water_removal": {
    "mode": "required",
    "instruction": "Remove feed and water from every selected pen the evening before the weighing. Film the feed being removed and the water being removed as two separate live-camera videos per pen and submit each pen before midnight.",
    "proofs": [
      {"key": "feed_video", "title": "Feed removed", "hint": "Live-camera video of this pen's feed being taken away.", "kind": "video", "required": true},
      {"key": "water_video", "title": "Water removed", "hint": "Live-camera video of this pen's water being taken away.", "kind": "video", "required": true}
    ],
    "questions": []
  },
  "capture": {
    "individual": {"video_required": true},
    "lump_sum": {"video_min": 1, "video_max": 5}
  }
}$seed$::jsonb)
FROM public.sop_definitions sd
WHERE sd.tenant_id = v.tenant_id AND sd.sop_id = v.sop_id
  AND sd.code = 'weighing.session'
  AND v.status = 'published'
  AND NOT (v.form_dsl ? 'weighing');

-- +goose Down
-- Forward-only on the document: the section is left on the published version (it is inert
-- without the code that reads it). The columns are dropped.
ALTER TABLE public.weighing_fasting_shed_proofs DROP COLUMN IF EXISTS sop_answers;
ALTER TABLE public.weighing_campaigns DROP COLUMN IF EXISTS sop_version;
