-- +goose Up
-- seed-fixture-guard:ignore: in-place condition on the published sales.deal SOP document; no
-- vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- A SALE WITHOUT ANIMALS OWES NO TAGGING (maintainer decision 2026-09-25, docs/decisions/sales-sop.md
-- -> "A sale without animals"). Every recorded sale opens the `sales_deal` workflow, and the seeded
-- document (000369) stamps "Tag the animals sold" on all of them. The tag step completes only from
-- the tagging confirm, and the confirm refuses a deal with no animal count -- so a manure, feed or
-- other-item sale, or an animal sale recorded with a blank head count, carried a step nobody could
-- ever finish and a card that stayed open and overdue forever.
--
-- The three animal steps -- tag_animals, loading_video, dispatch_note (the gate pass) -- now carry
-- `"when": "sale_has_animals"`. The opener decides the value from the deal's lines (a live-animal
-- line with a head count above zero, carried on sales.deal.recorded as has_live_animals) and a
-- sale without animals opens with its payment steps only.
--
-- 000369 is NOT edited (STG records migration checksums). The condition is added IN PLACE to each
-- tenant's currently PUBLISHED sales.deal version -- the 000315 shape -- so the version a farm sees
-- on deploy is the one it already had. Only those three step keys are touched, and only where the
-- step carries no condition of its own, so an authored version keeps every other edit. A workflow
-- already open is untouched here (it keeps the steps it started with); 000444 repairs those.
-- The seeded document is tasks/domain/sopseed/sales_deal.json (pinned by
-- TestMigrationEmbedsTheSalesSeed, which also pins this file's three keys).

-- seed-migration-guard:ignore owner=manohark issue=sales-workflow-conditions reason=in-place-condition-on-published-sales-sop-document expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = jsonb_set(v.form_dsl, '{follow_up,tracks}', (
      SELECT jsonb_agg(
               CASE WHEN t.track->>'key' = 'sales_deal' AND jsonb_typeof(t.track->'steps') = 'array'
                    THEN jsonb_set(t.track, '{steps}', (
                           SELECT jsonb_agg(
                                    CASE WHEN st.step->>'key' IN ('tag_animals', 'loading_video', 'dispatch_note')
                                              AND COALESCE(st.step->>'when', '') = ''
                                         THEN st.step || '{"when": "sale_has_animals"}'::jsonb
                                         ELSE st.step END
                                    ORDER BY st.ord)
                           FROM jsonb_array_elements(t.track->'steps') WITH ORDINALITY AS st(step, ord)))
                    ELSE t.track END
               ORDER BY t.ord)
      FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') WITH ORDINALITY AS t(track, ord)
    )),
    updated_at = now()
FROM public.sop_definitions sd
WHERE sd.tenant_id = v.tenant_id AND sd.sop_id = v.sop_id
  AND sd.code = 'sales.deal'
  AND v.status = 'published'
  AND jsonb_typeof(v.form_dsl->'follow_up'->'tracks') = 'array'
  AND EXISTS (
    SELECT 1
    FROM jsonb_array_elements(v.form_dsl->'follow_up'->'tracks') AS t(track),
         jsonb_array_elements(CASE WHEN jsonb_typeof(t.track->'steps') = 'array' THEN t.track->'steps' ELSE '[]'::jsonb END) AS st(step)
    WHERE t.track->>'key' = 'sales_deal'
      AND st.step->>'key' IN ('tag_animals', 'loading_video', 'dispatch_note')
      AND COALESCE(st.step->>'when', '') = ''
  );

-- A tenant with no sales.deal SOP at all (created after 000369) gets the definition and v1 as the
-- CURRENT seeded document -- tasks/domain/sopseed/sales_deal.json, embedded verbatim (pinned by
-- TestMigrationEmbedsTheSalesSeed). Every tenant that existed at 000369 is patched above instead.
-- seed-migration-guard:ignore owner=manohark issue=sales-workflow-conditions reason=seed-sales-sop-for-tenants-created-after-000369 expiry=2026-12-31
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key, kind, module_key)
SELECT t.tenant_id, 'sales.deal', 'Sale',
       'What happens after a sale is recorded: tagging the animals, loading them, the money -- and who does each step.',
       'active', 'action', 'module', 'sales'
FROM public.tenants t
WHERE NOT EXISTS (SELECT 1 FROM public.sop_definitions sd WHERE sd.tenant_id = t.tenant_id AND sd.code = 'sales.deal')
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Sale v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'sales.deal',
         'title', 'Sale',
         'fields', jsonb_build_array(),
         'follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "sales_deal", "module": "sales", "label": "Sale", "subject": "sale",
      "steps": [
        {"key": "tag_animals", "task_type": "sale_tag_animals", "title": "Tag the animals sold", "detail": "Scan or type the tag of every animal on this sale, enter its weight, and confirm. This step completes on its own once the tagging is confirmed.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "park_head", "when": "sale_has_animals"},
        {"key": "loading_video", "task_type": "video_record", "title": "Record the animals being loaded", "detail": "One live video of the tagged animals walking onto the buyer's vehicle, ear tags visible.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}, "owner": "park_head", "when": "sale_has_animals"},
        {"key": "dispatch_note", "task_type": "photo_record", "title": "Photo of the gate pass", "detail": "One photo of the signed gate pass or dispatch note handed to the buyer's driver.", "proof": {"photo": 1}, "schedule": {"kind": "immediately"}, "owner": "park_head", "when": "sale_has_animals"},
        {"key": "full_payment", "task_type": "record_yes_no", "title": "Has the buyer paid in full?", "detail": "Answer Yes when the receipts on the sale cover its value. Answer No if a balance is still due.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director"},
        {"key": "collect_balance", "task_type": "do_and_confirm", "title": "Collect the balance and record the receipt", "detail": "Follow up with the buyer for the amount still due and record each receipt on the sale as it comes in.", "proof": {}, "schedule": {"kind": "immediately"}, "owner": "procurement_director", "when_answer": {"step": "full_payment", "op": "eq", "value": ["no"]}}
      ]
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "task", "types": ["video", "photo"], "required": false, "minimum_count": 0, "verify_before_apply": false, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["boolean", "select", "multiselect", "number", "text", "video_proof", "photo_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded sale SOP (migration 000443)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'sales.deal'
  AND NOT EXISTS (SELECT 1 FROM public.sop_versions v WHERE v.tenant_id = sd.tenant_id AND v.sop_id = sd.sop_id)
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
-- Forward-only on the document: removing the condition would re-stamp an unfinishable tag step on
-- every manure / feed sale. The condition is inert without the code that reads it.
SELECT 1;
