-- +goose Up
-- FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md).
--
-- The three feed SOPs (feed.direction, feed.packing, feed.transport -- library documents since
-- migration 000175) stop being documents: their `feed` section is the CARD the crew runs at each
-- stage of the feed chain -- the capture slots (key, title, hint, kind video/photo/either,
-- compulsory or not) and the questions -- authored on /feed/sops.
--
-- DAY ONE IS THE CURRENT BEHAVIOUR. The section added IN PLACE to each tenant's currently
-- published version is the seeded document the code compiles for a tenant with no authored
-- version (feeddirection/domain/sopseed/*.json, embedded verbatim; pinned by
-- TestMigrationEmbedsTheSeededFeedSOP): the distribution card's feed weight PHOTO + feed VIDEO +
-- water VIDEO under the exact proof-register field keys the phones have always stamped, one
-- packing video per bag, one transport video per shed, one wastage video per experiment pen.
--
-- THE PIN. A sheet is stamped with the version in force when it is ISSUED
-- (feed_direction_issues.sop_version); every distribution / packing / wastage card of that sheet
-- runs on it. A transport task is stamped when it is materialized. NULL = the seeded rules
-- (every sheet and task that exists today).
--
-- THE EVIDENCE ROWS carry {slot key: proof ref} (sop_proofs) and the authored answers
-- (sop_answers); the legacy fixed columns mirror the seeded slots and are backfilled, so every
-- pre-existing reader (verifier items, leadership reads, exports, the Work Board) still reads.
--
-- seed-migration-guard:ignore owner=raviteja issue=feed-sop-cards reason=published-feed-sop-library-seed-added-in-place;runtime-sheets-pin-version;no-clean-slate-seed-command-replays-it expiry=2026-12-31
-- seed-fixture-guard:ignore: feed SOP library cards only; no vaccination HRMS source rows, fixture inputs, protocol schedule, roster, or vaccination seed contract changes.

ALTER TABLE public.feed_direction_issues
  ADD COLUMN IF NOT EXISTS sop_version integer,
  ADD COLUMN IF NOT EXISTS packing_sop_version integer;
COMMENT ON COLUMN public.feed_direction_issues.sop_version IS
  'feed.direction SOP version the sheet was ISSUED under; its distribution and wastage cards run on it to the end (FEED SOP, 2026-09-16). NULL = the seeded cards (pre-SOP behaviour).';
COMMENT ON COLUMN public.feed_direction_issues.packing_sop_version IS
  'feed.packing SOP version the sheet was ISSUED under; every bag of the sheet is packed on it (FEED SOP, 2026-09-16). NULL = the seeded card.';

ALTER TABLE public.feed_transport_tasks
  ADD COLUMN IF NOT EXISTS sop_version integer;
COMMENT ON COLUMN public.feed_transport_tasks.sop_version IS
  'feed.transport SOP version the task was materialized under (FEED SOP, 2026-09-16). NULL = the seeded card.';

ALTER TABLE public.feed_distribution_completions
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.feed_distribution_completions.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the distribution card asked for on this pen-session (FEED SOP, 2026-09-16). feed_weight_proof_ref / distribution_proof_ref / water_proof_ref mirror the seeded slots.';
COMMENT ON COLUMN public.feed_distribution_completions.sop_answers IS
  'The crew''s answers to the distribution card''s authored questions, keyed by question id, validated against the sheet''s pinned version at submit.';
UPDATE public.feed_distribution_completions
SET sop_proofs = (
  CASE WHEN feed_weight_proof_ref IS NOT NULL AND feed_weight_proof_ref <> '' THEN jsonb_build_object('feed_distribution_feed_weight_photo', feed_weight_proof_ref) ELSE '{}'::jsonb END
  || CASE WHEN distribution_proof_ref IS NOT NULL AND distribution_proof_ref <> '' THEN jsonb_build_object('feed_distribution_video', distribution_proof_ref) ELSE '{}'::jsonb END
  || CASE WHEN water_proof_ref IS NOT NULL AND water_proof_ref <> '' THEN jsonb_build_object('feed_distribution_water_video', water_proof_ref) ELSE '{}'::jsonb END
)
WHERE sop_proofs = '{}'::jsonb;

ALTER TABLE public.feed_packing_completions
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.feed_packing_completions.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the packing card asked for on this bag (FEED SOP, 2026-09-16). packing_proof_ref mirrors the seeded feed_packing_video slot.';
UPDATE public.feed_packing_completions
SET sop_proofs = jsonb_build_object('feed_packing_video', packing_proof_ref)
WHERE sop_proofs = '{}'::jsonb AND packing_proof_ref IS NOT NULL AND packing_proof_ref <> '';

ALTER TABLE public.feed_transport_attempts
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.feed_transport_attempts.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the transport card asked for on this attempt (FEED SOP, 2026-09-16). proof_ref mirrors the seeded feed_transport_video slot.';
UPDATE public.feed_transport_attempts
SET sop_proofs = jsonb_build_object('feed_transport_video', proof_ref)
WHERE sop_proofs = '{}'::jsonb AND proof_ref IS NOT NULL AND proof_ref <> '';

ALTER TABLE public.feed_wastage_completions
  ADD COLUMN IF NOT EXISTS sop_proofs jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS sop_answers jsonb NOT NULL DEFAULT '{}'::jsonb;
COMMENT ON COLUMN public.feed_wastage_completions.sop_proofs IS
  '{slot key: proof artifact ref} for every capture the wastage card asked for on this pen-day (FEED SOP, 2026-09-16). wastage_proof_ref mirrors the seeded feed_wastage_video slot.';
UPDATE public.feed_wastage_completions
SET sop_proofs = jsonb_build_object('feed_wastage_video', wastage_proof_ref)
WHERE sop_proofs = '{}'::jsonb AND wastage_proof_ref IS NOT NULL AND wastage_proof_ref <> '';

-- THE LEGACY CHECKS NAMED FIXED SLOTS. A card that drops a slot (no water video) or renames one
-- would fail them, so each is replaced by "an evidence row awaiting or past verification carries at
-- least one capture" -- the card itself guarantees at least one compulsory slot. The legacy columns
-- keep mirroring the seeded slot (or, when the card no longer has it, the first capture) so every
-- pre-existing reader still finds a ref.
ALTER TABLE public.feed_distribution_completions
  DROP CONSTRAINT IF EXISTS feed_distribution_completions_proof_check,
  DROP CONSTRAINT IF EXISTS feed_distribution_completions_weight_proof_check;
ALTER TABLE public.feed_distribution_completions
  ADD CONSTRAINT feed_distribution_completions_sop_proofs_check
  CHECK (status NOT IN ('pending_verification', 'completed') OR (jsonb_typeof(sop_proofs) = 'object' AND sop_proofs <> '{}'::jsonb)) NOT VALID;
ALTER TABLE public.feed_packing_completions
  DROP CONSTRAINT IF EXISTS feed_packing_completions_proof_check;
ALTER TABLE public.feed_packing_completions
  ADD CONSTRAINT feed_packing_completions_sop_proofs_check
  CHECK (status NOT IN ('pending_verification', 'completed') OR (jsonb_typeof(sop_proofs) = 'object' AND sop_proofs <> '{}'::jsonb)) NOT VALID;
ALTER TABLE public.feed_wastage_completions
  DROP CONSTRAINT IF EXISTS feed_wastage_completions_proof_check;
ALTER TABLE public.feed_wastage_completions
  ADD CONSTRAINT feed_wastage_completions_sop_proofs_check
  CHECK (status NOT IN ('pending_verification', 'completed') OR (jsonb_typeof(sop_proofs) = 'object' AND sop_proofs <> '{}'::jsonb)) NOT VALID;

-- feed.direction: add the seeded `feed` section IN PLACE to the currently published version (the version
-- a farm sees on deploy is the one it already had, now carrying the card).
-- seed-migration-guard:ignore owner=raviteja issue=feed-sop-cards reason=published-feed-sop-library-seed-added-in-place;runtime-sheets-pin-version;no-clean-slate-seed-command-replays-it expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl || jsonb_build_object('feed', $seed${
  "schema_version": "goatos.sop-feed.v1",
  "distribution": {
    "instruction": "Weigh the feed, film it being given out, and film the water being given out. Any operator on the park may record any capture; the pen is submitted once every compulsory capture is in.",
    "proofs": [
      {"key": "feed_distribution_feed_weight_photo", "title": "Feed weight photo", "hint": "The weighed feed on the scale, reading visible, before it is given out.", "kind": "photo", "required": true},
      {"key": "feed_distribution_video", "title": "Feed distribution video", "hint": "Feed actually being given to the animals in this pen.", "kind": "video", "required": true},
      {"key": "feed_distribution_water_video", "title": "Water distribution video", "hint": "Water being given to the animals in this pen.", "kind": "video", "required": true}
    ],
    "questions": []
  },
  "wastage": {
    "instruction": "Film the leftover feed in the pen before it is cleared.",
    "proofs": [
      {"key": "feed_wastage_video", "title": "Leftover feed video", "hint": "The leftover feed in the trough before it is cleared; the verifier reads the weight off the clip.", "kind": "video", "required": true}
    ],
    "questions": []
  }
}$seed$::jsonb)
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  AND d.code = 'feed.direction'
  AND v.status = 'published'
  AND NOT (v.form_dsl ? 'feed');

-- feed.packing: add the seeded `feed` section IN PLACE to the currently published version (the version
-- a farm sees on deploy is the one it already had, now carrying the card).
-- seed-migration-guard:ignore owner=raviteja issue=feed-sop-cards reason=published-feed-sop-library-seed-added-in-place;runtime-sheets-pin-version;no-clean-slate-seed-command-replays-it expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl || jsonb_build_object('feed', $seed${
  "schema_version": "goatos.sop-feed.v1",
  "packing": {
    "instruction": "Pack one pen and one session per bag and film it being weighed out and packed.",
    "proofs": [
      {"key": "feed_packing_video", "title": "Packing proof video", "hint": "This session's share being weighed out and packed for this pen.", "kind": "video", "required": true}
    ],
    "questions": []
  }
}$seed$::jsonb)
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  AND d.code = 'feed.packing'
  AND v.status = 'published'
  AND NOT (v.form_dsl ? 'feed');

-- feed.transport: add the seeded `feed` section IN PLACE to the currently published version (the version
-- a farm sees on deploy is the one it already had, now carrying the card).
-- seed-migration-guard:ignore owner=raviteja issue=feed-sop-cards reason=published-feed-sop-library-seed-added-in-place;runtime-sheets-pin-version;no-clean-slate-seed-command-replays-it expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl || jsonb_build_object('feed', $seed${
  "schema_version": "goatos.sop-feed.v1",
  "transport": {
    "instruction": "Film the packed feed loaded and staged outside the shed before the cutoff.",
    "proofs": [
      {"key": "feed_transport_video", "title": "Transport video", "hint": "The shed's packed feed loaded and staged outside the shed.", "kind": "video", "required": true}
    ],
    "questions": []
  }
}$seed$::jsonb)
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  AND d.code = 'feed.transport'
  AND v.status = 'published'
  AND NOT (v.form_dsl ? 'feed');

-- +goose Down
ALTER TABLE public.feed_wastage_completions DROP CONSTRAINT IF EXISTS feed_wastage_completions_sop_proofs_check;
ALTER TABLE public.feed_packing_completions DROP CONSTRAINT IF EXISTS feed_packing_completions_sop_proofs_check;
ALTER TABLE public.feed_distribution_completions DROP CONSTRAINT IF EXISTS feed_distribution_completions_sop_proofs_check;
-- seed-migration-guard:ignore owner=raviteja issue=feed-sop-cards reason=published-feed-sop-library-seed-added-in-place;runtime-sheets-pin-version;no-clean-slate-seed-command-replays-it expiry=2026-12-31
UPDATE public.sop_versions v
SET form_dsl = v.form_dsl - 'feed'
FROM public.sop_definitions d
WHERE d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id AND d.code IN ('feed.direction', 'feed.packing', 'feed.transport');
ALTER TABLE public.feed_wastage_completions DROP COLUMN IF EXISTS sop_answers, DROP COLUMN IF EXISTS sop_proofs;
ALTER TABLE public.feed_transport_attempts DROP COLUMN IF EXISTS sop_answers, DROP COLUMN IF EXISTS sop_proofs;
ALTER TABLE public.feed_packing_completions DROP COLUMN IF EXISTS sop_answers, DROP COLUMN IF EXISTS sop_proofs;
ALTER TABLE public.feed_distribution_completions DROP COLUMN IF EXISTS sop_answers, DROP COLUMN IF EXISTS sop_proofs;
ALTER TABLE public.feed_transport_tasks DROP COLUMN IF EXISTS sop_version;
ALTER TABLE public.feed_direction_issues DROP COLUMN IF EXISTS packing_sop_version, DROP COLUMN IF EXISTS sop_version;
