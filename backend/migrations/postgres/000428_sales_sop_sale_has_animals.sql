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
-- already open is untouched here (it keeps the steps it started with); 000429 repairs those.
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

-- +goose Down
-- Forward-only on the document: removing the condition would re-stamp an unfinishable tag step on
-- every manure / feed sale. The condition is inert without the code that reads it.
SELECT 1;
