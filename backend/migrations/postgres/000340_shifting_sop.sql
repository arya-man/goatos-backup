-- +goose Up
-- SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md).
--
-- The `shifting` SOP (a library document since migration 000175; its follow_up track is dormant)
-- now carries a `shifting` section: the RAISE extras (questions / optional captures shown to the
-- park head before approval), the COMPLETION card (the captures and questions every completion
-- carries) and the HIGH-PRIORITY card (added to the completion of a high-priority movement),
-- authored on /counts/sops.
--
-- DAY ONE IS THE CURRENT BEHAVIOUR. The section added IN PLACE to each tenant's currently published
-- version is the seeded document the code compiles for a tenant with no authored version
-- (counts/domain/sopseed/shifting.json, embedded verbatim; pinned by
-- TestMigrationEmbedsTheSeededShiftingSOP): nothing at raise, one shifting video per completion,
-- the feed packing + feed given clips on a high-priority one, under the exact proof-register field
-- keys the phones have always stamped (shifting_<step>_video).
--
-- THE PIN. A movement is stamped with the version in force when it is RAISED (sop_version); its
-- raise card was judged on it and its completion runs on it to the end. NULL = the seeded rules
-- (every movement that exists today).
--
-- THE EVIDENCE COLUMNS carry {slot key: proof ref} (raise_sop_proofs / sop_proofs) and the
-- authored answers (raise_sop_answers / sop_answers); raise_capture_evidence is the snapshot the
-- park head approved (rows, media, missing note) in the shared CountsApprovalCapture shape. The
-- legacy fixed columns (proof_ref, feed_packing_proof_ref, feed_given_proof_ref) keep mirroring the
-- seeded slots so every pre-existing reader still finds a ref. No defaults: metadata-only ALTER.
--
-- seed-fixture-guard:ignore: shifting SOP card section added in place to the published `shifting`
-- library version + nullable shifting_events evidence columns; no vaccination HRMS source rows,
-- fixture inputs, protocol schedule, roster or vaccination seed contract changes.
ALTER TABLE public.shifting_events
  ADD COLUMN IF NOT EXISTS sop_version integer,
  ADD COLUMN IF NOT EXISTS raise_sop_proofs jsonb,
  ADD COLUMN IF NOT EXISTS raise_sop_answers jsonb,
  ADD COLUMN IF NOT EXISTS raise_capture_evidence jsonb,
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb,
  ADD COLUMN IF NOT EXISTS sop_answers jsonb;
COMMENT ON COLUMN public.shifting_events.sop_version IS
  'shifting SOP version the movement was RAISED under; the raise card was judged on it and the completion runs on it to the end (SHIFTING SOP, 2026-09-16). NULL = the seeded rules (pre-SOP behaviour).';
COMMENT ON COLUMN public.shifting_events.raise_sop_proofs IS
  '{slot key: proof artifact ref} for every capture the raise card asked for (SHIFTING SOP, 2026-09-16). NULL on rows raised before the section existed or by a phone that sent none.';
COMMENT ON COLUMN public.shifting_events.raise_sop_answers IS
  'The raiser''s answers to the raise card''s authored questions, keyed by question id, validated against the pinned version at raise.';
COMMENT ON COLUMN public.shifting_events.raise_capture_evidence IS
  'Snapshot of what the park head approved: {version_label, rows[{label,value,group}], media[{proof_id,label,kind}], missing_note} in the shared CountsApprovalCapture shape. Taken at raise; never recomposed.';
COMMENT ON COLUMN public.shifting_events.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the completion (+ high_priority) card asked for (SHIFTING SOP, 2026-09-16). proof_ref / feed_packing_proof_ref / feed_given_proof_ref mirror the seeded slots.';
COMMENT ON COLUMN public.shifting_events.sop_answers IS
  'The operator''s answers to the completion (+ high_priority) card''s authored questions, keyed by question id, validated against the movement''s pinned version at completion.';

-- shifting: add the seeded `shifting` section IN PLACE to the currently published version (the
-- version a farm sees on deploy is the one it already had, now carrying the cards).
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl || jsonb_build_object('shifting', $seed${
  "schema_version": "goatos.sop-shifting.v1",
  "raise": {
    "instruction": "",
    "proofs": [],
    "questions": []
  },
  "completion": {
    "instruction": "",
    "proofs": [
      {"key": "shifting_shifting_video", "title": "Shifting video", "hint": "Live camera only. This evidence is reviewed after the task; it does not control the herd move.", "kind": "video", "required": true}
    ],
    "questions": []
  },
  "high_priority": {
    "instruction": "This packing proof belongs only to this Shifting task.",
    "proofs": [
      {"key": "shifting_packing_video", "title": "Feed packing video", "hint": "Live camera only. The configured ration being weighed out and packed for the moved animals.", "kind": "video", "required": true},
      {"key": "shifting_feeding_video", "title": "Feed given to animal video", "hint": "Live camera only. The configured feed being given to the moved animal(s) in the destination pen.", "kind": "video", "required": true}
    ],
    "questions": []
  }
}$seed$::jsonb)
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  AND d.code = 'shifting'
  AND v.status = 'published'
  AND NOT (v.form_dsl ? 'shifting');

-- +goose Down
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl - 'shifting'
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id AND d.code = 'shifting';
ALTER TABLE public.shifting_events
  DROP COLUMN IF EXISTS sop_answers,
  DROP COLUMN IF EXISTS sop_proofs,
  DROP COLUMN IF EXISTS raise_capture_evidence,
  DROP COLUMN IF EXISTS raise_sop_answers,
  DROP COLUMN IF EXISTS raise_sop_proofs,
  DROP COLUMN IF EXISTS sop_version;
