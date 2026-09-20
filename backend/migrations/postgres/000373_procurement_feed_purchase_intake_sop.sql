-- +goose Up
-- seed-fixture-guard:ignore: one closed-list CHECK widening plus one seeded module SOP and two
-- task types; no vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- PROCUREMENT IS SOP-DRIVEN END TO END (maintainer decision 2026-09-20), the FEED half. Buying a
-- feed load produced a ledger row and nothing else: no weighbridge slip, no record that the truck
-- ever arrived beyond a status word, no link at all between the load and the aflatoxin test the
-- toxin module was quietly running on it, and no step chasing the balance. The work is now the
-- published `procurement.feed_purchase_intake` SOP, authored on /procurement/sops with the same
-- List | Flow editor, and RUN by the tasks engine: recording a purchase opens one workflow keyed
-- on the load (subject_ref_id = feed_purchases.feed_purchase_id, no animal).
--
-- TWO of its steps are the ENGINE's, and both for the same reason -- the fact they record already
-- has an owner elsewhere, and a tap would let the two disagree. The arrival step is completed by
-- the ledger's own delivery write (procurement.feed_purchase.reached), and the aflatoxin step by
-- an ACCEPTED toxin round (procurement.toxin_test.accepted, new in 000374). A rejected or void
-- round mints a retest and announces nothing, so the step stays open: the load is still owed a
-- test.

-- 1. The feed workflow's template key joins the closed list. The module (`procurement`) was
--    already widened by 000371.
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting', 'sales_deal', 'animal_purchase_intake', 'feed_purchase_intake') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');

-- 2. Two new Task Type Registry rows, for every tenant. Embedded verbatim from
--    tasks/domain/sopseed/task_types_procurement_feed.json (pinned by
--    TestMigrationEmbedsTheFeedPurchaseSeed); earlier task-type files are already applied and are
--    never edited.
INSERT INTO public.sop_task_types (tenant_id, task_type_key, name, description, category_scope, answer_kind, engine_hook, parameter_schema, sort_order)
SELECT t.tenant_id, x.key, x.name, x.description, x.category_scope, x.answer_kind, x.engine_hook, x.parameter_schema, 120 + e.ordinality
FROM public.tenants t
CROSS JOIN LATERAL jsonb_array_elements($seed$[
  {
    "key": "feed_purchase_reached",
    "name": "Mark the load as reached",
    "category_scope": ["action", "commodity"],
    "answer_kind": "none",
    "engine_hook": "feed_purchase_reached",
    "description": "The feed load arriving at the farm. Completed by the engine when the load is marked delivered on the feed purchase ledger — never by a tap, so a load cannot read \"arrived\" while the ledger still has it on the road.",
    "parameter_schema": {"type": "object", "properties": {}}
  },
  {
    "key": "toxin_test_accepted",
    "name": "Aflatoxin test signed off",
    "category_scope": ["action", "commodity"],
    "answer_kind": "none",
    "engine_hook": "toxin_test_accepted",
    "description": "The load's aflatoxin strip test, run and reviewed in the Toxin module. Completed by the engine when a round is ACCEPTED; a rejected or void round mints a retest and leaves this open, because the load is still owed a test.",
    "parameter_schema": {"type": "object", "properties": {}}
  }
]$seed$::jsonb) WITH ORDINALITY AS e(row, ordinality)
CROSS JOIN LATERAL jsonb_to_record(e.row) AS x(key text, name text, description text, category_scope jsonb, answer_kind text, engine_hook text, parameter_schema jsonb)
ON CONFLICT (tenant_id, task_type_key) DO NOTHING;

-- 3. The feed purchase SOP, published v1 for every tenant. Embedded verbatim from
--    tasks/domain/sopseed/procurement_feed_purchase_intake.json.
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'procurement.feed_purchase_intake', 'Feed purchase',
       'What a bought feed load owes: the weighbridge slip, the arrival, the aflatoxin test and the money -- and who does each step.',
       'active', 'action', 'module', 'procurement'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Feed purchase v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'procurement.feed_purchase_intake',
         'title', 'Feed purchase',
         'fields', jsonb_build_array(),
         'follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "feed_purchase_intake", "module": "procurement", "label": "Feed purchase", "subject": "feed_load",
      "steps": [
        {"key": "weighbridge_slip", "task_type": "photo_record", "title": "Photo of the weighbridge slip", "detail": "One photo of the slip the seller gives with the load, showing the weight loaded.", "proof": {"photo": 1}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "mark_reached", "task_type": "feed_purchase_reached", "title": "Mark the load as reached", "detail": "Record the day the truck arrived and the weight that came off it. This step completes on its own once the load is marked delivered.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "unloading_photo", "task_type": "photo_record", "title": "Photo of the feed in the store", "detail": "One photo of the load stacked where it will be kept.", "proof": {"photo": 1}, "schedule": {"kind": "immediately"}, "owner": "park_head"},
        {"key": "toxin_test", "task_type": "toxin_test_accepted", "title": "Aflatoxin test signed off", "detail": "The load's strip test is run and reviewed in the Toxin module. This step completes on its own when a round is accepted; a rejected or void round mints a retest and this stays open.", "proof": {}, "schedule": {"kind": "immediately"}},
        {"key": "vendor_paid", "task_type": "record_yes_no", "title": "Has the vendor been paid in full?", "detail": "Answer Yes when the payments recorded on this load cover its cost. Answer No if a balance is still due.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "settle_balance", "task_type": "do_and_confirm", "title": "Settle the balance and record the payment", "detail": "Pay what is still due on this load and record each payment against it as it goes out.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director", "when_answer": {"step": "vendor_paid", "op": "eq", "value": ["no"]}}
      ]
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text", "video_proof", "photo_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded feed purchase SOP (migration 000373)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'procurement.feed_purchase_intake'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
DELETE FROM public.workflow_actions wa USING public.workflow_instances w
WHERE w.tenant_id = wa.tenant_id AND w.workflow_id = wa.workflow_id AND w.template_key = 'feed_purchase_intake';
DELETE FROM public.workflow_instances WHERE template_key = 'feed_purchase_intake';
ALTER TABLE public.workflow_instances DROP CONSTRAINT IF EXISTS workflow_instances_template_key_check;
ALTER TABLE public.workflow_instances ADD CONSTRAINT workflow_instances_template_key_check
  CHECK (template_key IN ('birth_kid', 'birth_mother', 'death', 'reconcile', 'shifting', 'sales_deal', 'animal_purchase_intake') OR template_key ~ '^general:general\.[a-z][a-z0-9_]*$');
DELETE FROM public.sop_versions sv USING public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'procurement.feed_purchase_intake';
DELETE FROM public.sop_definitions WHERE code = 'procurement.feed_purchase_intake';
DELETE FROM public.sop_task_types WHERE task_type_key IN ('feed_purchase_reached', 'toxin_test_accepted');
