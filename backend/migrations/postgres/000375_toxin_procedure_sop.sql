-- +goose Up
-- seed-fixture-guard:ignore: one nullable-default column on the toxin round plus one seeded
-- module SOP; no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- THE TOXIN PROCEDURE IS AUTHORED (maintainer decision 2026-09-20: procurement SOP-driven end to
-- end, with "steps authored, the toxin engine keeps state"). How many steps the aflatoxin test
-- has, what each one tells the tester to do, whether it is filmed or photographed, how long the
-- extract sits and which step each wait gates were SEVEN GO CONSTANTS. A kit change, a farm that
-- centrifuges, or one word of a wrong instruction meant a backend release. They are now
-- `form_dsl.toxin` of the published `procurement.toxin_test` SOP.
--
-- WHAT IS NOT AUTHORED, deliberately: the round's state machine, the retest minting, the
-- CEO/CXO-only verdict, the reading vocabulary (Negative / Positive / Invalid) and the
-- server-clock enforcement of every gate. The document says what the procedure IS; the engine
-- still decides what happens when a strip comes back void, and no published version can change
-- that. That is what made it safe to open a medically-gated flow at all.

-- 1. Every round records the procedure it RUNS. Stamped at creation and never changed: a version
--    published mid-test must not move the steps under the tester's feet, and a round that ran the
--    old seven steps stays readable as the test it actually was. A RETEST is new work and is
--    minted on whatever is published then, so a corrected instruction reaches the next attempt.
--    Existing rounds are version 1 -- the seeded document, which compiles to exactly the steps
--    they have been running.
ALTER TABLE public.toxin_test_tasks
  ADD COLUMN IF NOT EXISTS sop_version integer NOT NULL DEFAULT 1;

-- 2. The procedure SOP, published v1 for every tenant, embedded verbatim from
--    toxin/domain/toxinseed/toxin_test.json (pinned by TestMigrationEmbedsTheSeededProcedure,
--    which also proves it compiles to the legacy Steps() step for step).
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'procurement.toxin_test', 'Aflatoxin test',
       'The steps of the aflatoxin strip test on a purchased feed load: what the tester does, what is filmed, and how long each wait lasts.',
       'active', 'action', 'module', 'procurement'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Aflatoxin test v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'procurement.toxin_test',
         'title', 'Aflatoxin test',
         'fields', jsonb_build_array(),
         'toxin', $seed${
  "schema_version": "goatos.sop-toxin.v1",
  "steps": [
    {
      "no": 1,
      "kind": "video",
      "title": "Take the sample",
      "instruction": "Take the feed sample out of this load on camera."
    },
    {
      "no": 2,
      "kind": "video",
      "title": "Grind and weigh",
      "instruction": "Grind the sample and weigh out 5 g on camera."
    },
    {
      "no": 3,
      "kind": "video",
      "title": "Mix and shake",
      "instruction": "Add the extraction solution and shake for 3 minutes on camera."
    },
    {
      "no": 4,
      "kind": "wait",
      "title": "Let it sit",
      "instruction": "Leave the mixture to settle for 1 hour. The next step unlocks when the hour has passed.",
      "wait_minutes": 60
    },
    {
      "no": 5,
      "kind": "video",
      "title": "Dilute and fill the well",
      "instruction": "Draw the clear liquid, dilute it as the kit table directs, and fill the microwell on camera.",
      "gate_after_step": 3,
      "gate_minutes": 60
    },
    {
      "no": 6,
      "kind": "video",
      "title": "Place the strip",
      "instruction": "After 3 minutes in the well, place the test strip and let it develop for 8 minutes — on camera.",
      "gate_after_step": 5,
      "gate_minutes": 3
    },
    {
      "no": 7,
      "kind": "photo_reading",
      "title": "Read the strip",
      "instruction": "Remove the pad, read the strip within 1 minute, photograph it immediately and record the reading.",
      "gate_after_step": 6,
      "gate_minutes": 8
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": true, "minimum_count": 1, "verify_before_apply": false, "approval_before_apply": true}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text", "video_proof", "photo_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded aflatoxin procedure (migration 000375)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'procurement.toxin_test'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'procurement.toxin_test';
DELETE FROM public.sop_definitions WHERE code = 'procurement.toxin_test';
ALTER TABLE public.toxin_test_tasks DROP COLUMN IF EXISTS sop_version;
