-- +goose Up
-- SOP Library coverage for the shipped Counts and Feed workflows (maintainer request 2026-08-18).
--
-- The SOP Library (/sops, backed by sop_definitions + sop_versions) documented only
-- vaccination.drive, shifting (a stale 2026-07-16 v1) and a feed.direction draft skeleton.
-- This migration makes the library describe every proof/verification workflow that actually
-- ships today, WITHOUT wiring any of them into task execution: sop_tasks are only ever
-- created for vaccination.drive (obligation sweeper -> sopbridge). Counts and Feed keep
-- their own canonical tables (counts_approval_requests / shifting_events /
-- feed_*_completions / feed transport tasks); these rows are the authoritative library
-- documents of those flows, not a second execution engine.
--
-- Documents mirror the LOCKED decisions verbatim (AGENTS.md):
--   * Birth / Death are APPROVAL-gated captures (counts_approver), evidence required at
--     capture, applied only on approval. No verifier verdict step exists for them.
--   * Shifting v2 replaces the stale v1: approve-FIRST (2026-08-09), raiser tag toggle
--     stage_mode keep_current|destination_stage (2026-08-15), completion applies the
--     movement, verification is post-task evidence review (no rollback), high priority
--     carries three live-camera videos (2026-07-29).
--   * Feed distribution: THREE mandatory proofs per the live contract
--     (FeedDistributionCompleteRequest): feed-weight PHOTO taken on the scale before
--     distribution, distribution VIDEO, and water VIDEO (video-only since 2026-08-11);
--     session completed only on verifier APPROVE (2026-07-26).
--   * Feed packing: one bag per pen per session, one video per bag (2026-08-11 grain),
--     verifier-gated; a head-count correction reopens EVERY session of the pen.
--   * Feed transport: ONE daily task per physical shed - never per pen (2026-08-12),
--     one fresh in-app-camera video, verifier-gated, every rework needs a new video.
--
-- Idempotent per tenant; safe on an empty tenant table.

-- ---------------------------------------------------------------------------
-- 1. counts.birth - Birth Recording (approval-gated)
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'counts.birth', 'Birth Recording',
       'Record a birth on the herd register. Evidence is captured at submission; the newborn joins the register only after a named Counts approver approves the request.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Birth Recording v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "counts.birth",
  "title": "Birth Recording",
  "fields": [
    {"key": "dam_rfid", "type": "goat_lookup", "label": "Mother RFID", "required": true,
     "description": "Must resolve to a female on the herd register. The server stores the canonical mother, never the raw text."},
    {"key": "litter_size", "type": "select", "label": "Litter size", "options": ["1", "2", "3"], "required": true,
     "description": "Twins and triplets create one register entry per kid. Each kid receives a server-generated provisional tag; the permanent RFID is attached later through the Tag the kid step."},
    {"key": "birth_location", "type": "location_picker", "label": "Birth location (shed / pen)", "required": true, "option_source": "locations.active"},
    {"key": "dob", "type": "date_time", "label": "Date of birth", "required": true,
     "description": "Must not be after the entry date."},
    {"key": "time_of_birth", "type": "text", "label": "Time of birth (optional)", "required": false,
     "description": "24-hour IST wall clock, e.g. 06:45. Anchors the birth follow-up steps; unknown falls back to 07:00."},
    {"key": "species", "type": "select", "label": "Species", "options": ["goat", "sheep"], "required": true},
    {"key": "breed", "type": "text", "label": "Breed", "required": true},
    {"key": "sex", "type": "select", "label": "Sex", "options": ["female", "male"], "required": true},
    {"key": "birth_weight_kg", "type": "number", "label": "Birth weight (kg)", "required": false, "min": 0},
    {"key": "birth_video", "type": "video_proof", "label": "Birth proof video (live camera)", "required": true, "proof_action": "video.capture",
     "description": "Live in-app camera evidence of the newborn(s) with the mother - no gallery or import."},
    {"key": "first_colostrum", "type": "select", "label": "1st Colostrum given (immediately after delivery)", "options": ["yes", "no"], "required": true,
     "description": "The first colostrum feed is part of the delivery sequence. The scheduled colostrum rounds unlock one by one at their fixed times only after this first feed."},
    {"key": "notes", "type": "text", "label": "Notes", "required": false}
  ],
  "rules": [
    {"type": "block_submission_if", "when": {"field": "dam_rfid", "operator": "empty"},
     "message": "Mother RFID is required."},
    {"type": "proof_required_if", "when": {"field": "litter_size", "operator": "not_empty"},
     "field": "birth_video", "message": "Live-camera proof of the newborn(s) is required before approval."}
  ],
  "workflow": {
    "nodes": [
      {"key": "record_birth", "type": "operator_execution", "label": "Record birth (live-camera evidence)"},
      {"key": "approver_review", "type": "approval", "label": "Counts approver review"},
      {"key": "registered", "type": "accepted", "label": "Newborn(s) on the register (provisional tag)"},
      {"key": "rejected", "type": "rejected", "label": "Rejected"},
      {"key": "first_colostrum", "type": "operator_execution", "label": "1st Colostrum - immediately after delivery"},
      {"key": "colostrum_rounds", "type": "operator_execution", "label": "Scheduled colostrum rounds - unlock one by one at fixed times"},
      {"key": "tag_kid", "type": "operator_execution", "label": "Tag the kid - permanent RFID, always the final kid task"},
      {"key": "evidence_review", "type": "proof_verification", "label": "Birth evidence review (one item per mother or child)"},
      {"key": "verified", "type": "accepted", "label": "Evidence verified"},
      {"key": "rework", "type": "rework", "label": "Evidence rework"}
    ],
    "edges": [
      {"from": "record_birth", "to": "approver_review", "condition": "submitted"},
      {"from": "approver_review", "to": "registered", "condition": "approver_approves"},
      {"from": "approver_review", "to": "rejected", "condition": "approver_rejects"},
      {"from": "record_birth", "to": "first_colostrum", "condition": "delivery_sequence"},
      {"from": "first_colostrum", "to": "colostrum_rounds", "condition": "rounds_unlock_at_fixed_times"},
      {"from": "colostrum_rounds", "to": "tag_kid", "condition": "all_rounds_complete"},
      {"from": "tag_kid", "to": "evidence_review", "condition": "workflow_complete"},
      {"from": "evidence_review", "to": "verified", "condition": "verifier_approves"},
      {"from": "evidence_review", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "batch", "types": ["video"], "required": true, "minimum_count": 1, "approval_before_apply": true, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped Counts birth approval workflow."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'counts.birth'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2. counts.death - Death Recording (guardrailed critical exit, approval-gated)
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'counts.death', 'Death Recording',
       'Guardrailed critical exit: only the dead + died pairing is accepted. Applied only after a named Counts approver approves; on approval the animal exits the register and its open work is cancelled.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Death Recording v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "counts.death",
  "title": "Death Recording",
  "fields": [
    {"key": "animal_rfid", "type": "animal_id_scan", "label": "Animal RFID", "required": true,
     "description": "Scan or look up the animal that died."},
    {"key": "reason", "type": "text", "label": "What happened", "required": true,
     "description": "The operator account of the death, 3 to 500 characters."},
    {"key": "occurred_at", "type": "date_time", "label": "When the death occurred", "required": false,
     "description": "Defaults to the time the event is recorded."},
    {"key": "death_video", "type": "video_proof", "label": "Record death video (live camera)", "required": true, "proof_action": "video.capture",
     "description": "First of exactly two operator steps. Live in-app camera only - no gallery or import. Kept as a durable local draft; nothing uploads until Submit."},
    {"key": "post_mortem_video", "type": "video_proof", "label": "Record post-mortem video (live camera)", "required": true, "proof_action": "video.capture",
     "description": "Second of exactly two operator steps. Submit is enabled only when both drafts exist."}
  ],
  "rules": [
    {"type": "block_submission_if", "when": {"field": "reason", "operator": "empty"},
     "message": "An account of the death is required."},
    {"type": "proof_required_if", "when": {"field": "animal_rfid", "operator": "not_empty"},
     "field": "death_video", "message": "The death video is required before approval."},
    {"type": "proof_required_if", "when": {"field": "animal_rfid", "operator": "not_empty"},
     "field": "post_mortem_video", "message": "The post-mortem video is required before approval."}
  ],
  "workflow": {
    "nodes": [
      {"key": "operator_submission", "type": "operator_execution", "label": "Record death + post-mortem videos (the only two operator steps)"},
      {"key": "approver_review", "type": "approval", "label": "Counts approver review"},
      {"key": "applied", "type": "accepted", "label": "Animal exited as dead, open work cancelled"},
      {"key": "rejected", "type": "rejected", "label": "Rejected"},
      {"key": "evidence_review", "type": "proof_verification", "label": "Media verification (backend state, never a third operator step)"},
      {"key": "verified", "type": "accepted", "label": "Evidence verified"},
      {"key": "rework", "type": "rework", "label": "Evidence rework"}
    ],
    "edges": [
      {"from": "operator_submission", "to": "approver_review", "condition": "submitted"},
      {"from": "approver_review", "to": "applied", "condition": "approver_approves"},
      {"from": "approver_review", "to": "rejected", "condition": "approver_rejects"},
      {"from": "applied", "to": "evidence_review", "condition": "media_review"},
      {"from": "evidence_review", "to": "verified", "condition": "verifier_approves"},
      {"from": "evidence_review", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "goat", "types": ["video"], "required": true, "minimum_count": 2, "approval_before_apply": true, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped Counts critical-death approval workflow."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'counts.death'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3. shifting v2 - approve-first, tag toggle, post-task evidence review.
--    Retire the stale published v1 first (one published version per SOP).
-- ---------------------------------------------------------------------------
UPDATE public.sop_versions sv
SET status = 'retired', retired_at = now(), updated_at = now(), row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code = 'shifting'
  AND sv.version = 1
  AND sv.status = 'published';

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 2, 'Shifting v2', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "shifting",
  "title": "Shifting",
  "fields": [
    {"key": "category", "type": "select", "label": "Category", "options": ["growth", "health", "breeding", "delivery"], "required": true},
    {"key": "priority", "type": "select", "label": "Priority", "options": ["low", "high"], "required": true,
     "description": "High priority is due the second it is approved. Low priority is planned work: raised before 13:30 IST it is due the next day, at or after 13:30 IST the day after that."},
    {"key": "goat_ids", "type": "goat_lookup", "label": "Animals", "repeat": true, "required": true},
    {"key": "source_location_id", "type": "location_picker", "label": "Source shed / pen", "required": true, "option_source": "locations.active"},
    {"key": "destination_location_id", "type": "location_picker", "label": "Destination shed / pen", "required": true, "option_source": "locations.active",
     "description": "Shed moves are within one park only. Leaving a park is a terminal exit, never a move."},
    {"key": "stage_mode", "type": "select", "label": "Tag on arrival", "options": ["destination_stage", "keep_current"], "required": true,
     "description": "Two backend-owned answers, never a stage of your own: the animals adopt the destination pen tag (default) or keep the tag they already carry. The server resolves the actual tag; an unavailable option is greyed out with a backend-owned reason."},
    {"key": "comment", "type": "text", "label": "Why are the animals being shifted", "required": false,
     "description": "Shown to the Park Head deciding the approval and to the verifier reviewing the evidence."},
    {"key": "shifting_video", "type": "video_proof", "label": "Shifting proof video", "required": true, "proof_action": "video.capture",
     "description": "Mandatory live-camera video at operator completion. Completion applies the movement: location, tag and counts move in the same transaction."},
    {"key": "feed_packing_video", "type": "video_proof", "label": "Feed packing proof video (high priority)", "required": false, "proof_action": "video.capture",
     "description": "High priority embeds feed packing inside Shifting. Shifting-scoped only; it never creates or completes the separate Feed Packing workflow."},
    {"key": "feeding_video", "type": "video_proof", "label": "Feeding proof video (high priority)", "required": false, "proof_action": "video.capture",
     "description": "Configured feed being given to the moved animals, resolved from the destination Feed Config."}
  ],
  "rules": [
    {"type": "block_submission_if", "when": {"field": "destination_location_id", "operator": "empty"},
     "message": "Destination location is required."},
    {"type": "proof_required_if", "when": {"field": "goat_ids", "operator": "not_empty"},
     "field": "shifting_video", "message": "The shifting video is required to complete the movement."},
    {"type": "proof_required_if", "when": {"field": "priority", "operator": "equals", "value": "high"},
     "field": "feed_packing_video", "message": "A high-priority movement requires the feed packing video."},
    {"type": "proof_required_if", "when": {"field": "priority", "operator": "equals", "value": "high"},
     "field": "feeding_video", "message": "A high-priority movement requires the feeding video."}
  ],
  "workflow": {
    "nodes": [
      {"key": "raise", "type": "operator_execution", "label": "Raise movement"},
      {"key": "park_head_approval", "type": "approval", "label": "Park Head approval"},
      {"key": "operator_completion", "type": "operator_execution", "label": "Operator completion with proof"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Evidence review"},
      {"key": "accepted", "type": "accepted", "label": "Evidence verified"},
      {"key": "rework", "type": "rework", "label": "Evidence rework"},
      {"key": "rejected", "type": "rejected", "label": "Rejected by Park Head"}
    ],
    "edges": [
      {"from": "raise", "to": "park_head_approval", "condition": "submitted"},
      {"from": "park_head_approval", "to": "operator_completion", "condition": "approver_authorizes"},
      {"from": "park_head_approval", "to": "rejected", "condition": "approver_rejects"},
      {"from": "operator_completion", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "task", "types": ["video"], "required": true, "minimum_count": 1, "high_priority_minimum_count": 3, "approval_before_execution": true, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Replaces the pre-approval-gate v1: approve-first (2026-08-09), stage_mode tag toggle (2026-08-15), completion applies the movement, verification is post-task evidence review."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'shifting'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 4. feed.direction - replace the never-published draft skeleton with the real
--    verifier-gated distribution document and publish it.
-- ---------------------------------------------------------------------------
UPDATE public.sop_versions sv
SET version_label = 'Feed Distribution v1',
    status = 'published',
    published_at = now(),
    form_dsl =
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "feed.direction",
  "title": "Feed Distribution",
  "fields": [
    {"key": "pen", "type": "location_picker", "label": "Shed / pen", "required": true, "option_source": "locations.active"},
    {"key": "session", "type": "number", "label": "Session", "required": true, "min": 1,
     "description": "The feed session being served (morning / evening per the session template)."},
    {"key": "feed_weight_photo", "type": "photo_proof", "label": "Weight of feed proof photo", "required": true, "proof_action": "photo.capture",
     "description": "Captured FIRST, while the feed is still on the scale - after distribution there is nothing left to weigh. Must be a photo. The distribution video proves the feed reached the animals; only this capture proves HOW MUCH, and the verifier checks it against the expected ration."},
    {"key": "distribution_video", "type": "video_proof", "label": "Feed distribution proof video", "required": true, "proof_action": "video.capture",
     "description": "Mandatory video of the configured feed being distributed to this pen."},
    {"key": "water_video", "type": "video_proof", "label": "Water distribution proof video", "required": true, "proof_action": "video.capture",
     "description": "Video only: a photo of a full trough proves a trough is full, not that this operator filled it today. A completion missing any of the three proofs is refused."}
  ],
  "rules": [
    {"type": "proof_required_if", "when": {"field": "pen", "operator": "not_empty"},
     "field": "feed_weight_photo", "message": "The feed weight photo is required, taken before the feed is given out."},
    {"type": "proof_required_if", "when": {"field": "pen", "operator": "not_empty"},
     "field": "distribution_video", "message": "The feed distribution video is required."},
    {"type": "proof_required_if", "when": {"field": "pen", "operator": "not_empty"},
     "field": "water_video", "message": "The water distribution video is required."}
  ],
  "workflow": {
    "nodes": [
      {"key": "weigh_feed", "type": "operator_execution", "label": "Weigh the feed and photograph it on the scale"},
      {"key": "operator_submission", "type": "operator_execution", "label": "Distribute feed + water and submit all three proofs"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Verifier review (one verdict covers all three proofs)"},
      {"key": "accepted", "type": "accepted", "label": "Session completed"},
      {"key": "rework", "type": "rework", "label": "Rework - re-shoot"}
    ],
    "edges": [
      {"from": "weigh_feed", "to": "operator_submission", "condition": "weighed_before_distribution"},
      {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
    proof_policy = '{"subject_scope": "shed", "types": ["photo", "video"], "required": true, "minimum_count": 3, "verify_before_apply": true}'::jsonb,
    compatibility = '{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
    validation_report = '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Replaces the v1 skeleton with the shipped verifier-gated distribution flow (2026-07-26): a session completes only after one verifier approval covering all three proofs (feed-weight photo, distribution video, water video)."}]}'::jsonb,
    updated_at = now(),
    row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code = 'feed.direction'
  AND sv.version = 1
  AND sv.status = 'draft';

UPDATE public.sop_definitions
SET status = 'active',
    name = 'Feed Distribution',
    description = 'Feed distribution for a shed session. The operator submits three mandatory proofs - a feed-weight photo taken on the scale before distribution, the distribution video, and the water video; the session is completed only after one verifier approval covering all three.',
    updated_at = now(),
    row_version = row_version + 1
WHERE code = 'feed.direction'
  AND status = 'draft';

-- ---------------------------------------------------------------------------
-- 5. feed.packing - one bag per pen per session, verifier-gated.
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'feed.packing', 'Feed Packing',
       'Pack one pen and one session per bag. A pen morning and evening are two separate bags: two cards, two videos, two verifications. Completed only after verifier approval; a head-count correction reopens every session of the pen.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Feed Packing v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "feed.packing",
  "title": "Feed Packing",
  "fields": [
    {"key": "pen", "type": "location_picker", "label": "Shed / pen", "required": true, "option_source": "locations.active",
     "description": "The pen is part of the bag identity: pens on different rations are packed and proved separately."},
    {"key": "session", "type": "number", "label": "Session", "required": true, "min": 1,
     "description": "Required, 1 or higher. One clip proves one bag, so morning and evening shares are filmed on their own."},
    {"key": "packing_video", "type": "video_proof", "label": "Packing proof video", "required": true, "proof_action": "video.capture",
     "description": "Video of this session share being weighed out and packed for this pen."}
  ],
  "rules": [
    {"type": "block_submission_if", "when": {"field": "session", "operator": "empty"},
     "message": "Session is required - a bag with no session never resolves to a worklist line."},
    {"type": "proof_required_if", "when": {"field": "pen", "operator": "not_empty"},
     "field": "packing_video", "message": "The packing video is required."}
  ],
  "workflow": {
    "nodes": [
      {"key": "operator_submission", "type": "operator_execution", "label": "Pack and film the bag"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Verifier review (expected ration for that session shown)"},
      {"key": "accepted", "type": "accepted", "label": "Bag completed"},
      {"key": "rework", "type": "rework", "label": "Rework - re-shoot"}
    ],
    "edges": [
      {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "task", "types": ["video"], "required": true, "minimum_count": 1, "verify_before_apply": true}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped verifier-gated packing flow at the restored shed-session grain (2026-08-11): one clip cannot prove two bags."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'feed.packing'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 6. feed.transport - one daily task per shed, verifier-gated.
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'feed.transport', 'Feed Transport',
       'One daily transport task per active physical shed - never per feed session and never per pen: a shed load is staged as one trip and proved with one video. Verifier approval completes it; every rework needs a new video and rejected attempts remain history.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Feed Transport v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "feed.transport",
  "title": "Feed Transport",
  "fields": [
    {"key": "shed", "type": "location_picker", "label": "Shed", "required": true, "option_source": "locations.active",
     "description": "One task and one video for the whole shed. Pens are packed as separate bags but loaded and staged as one trip."},
    {"key": "transport_video", "type": "video_proof", "label": "Transport proof video", "required": true, "proof_action": "video.capture",
     "description": "One mandatory fresh in-app-camera video of the packed feed loaded and staged outside the shed. Packed feed must be staged by 15:00 for next-day service."}
  ],
  "rules": [
    {"type": "proof_required_if", "when": {"field": "shed", "operator": "not_empty"},
     "field": "transport_video", "message": "The transport video is required before submission."}
  ],
  "workflow": {
    "nodes": [
      {"key": "operator_submission", "type": "operator_execution", "label": "Load, stage and film the shed trip"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Verifier review"},
      {"key": "accepted", "type": "accepted", "label": "Completed"},
      {"key": "rework", "type": "rework", "label": "Rework - new video required, prior attempts kept as history"}
    ],
    "edges": [
      {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "shed", "types": ["video"], "required": true, "minimum_count": 1, "verify_before_apply": true}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped daily shed-grain transport verification flow (2026-08-12): pen grain belongs to packing and distribution, never transport."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'feed.transport'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
-- Remove the library documents this migration added and restore the prior shapes.
-- Safe because none of these SOP codes ever creates sop_tasks/submissions (only
-- vaccination.drive does); the deletes below cannot orphan execution rows.

DELETE FROM public.sop_versions sv
USING public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code IN ('counts.birth', 'counts.death', 'feed.packing', 'feed.transport');

DELETE FROM public.sop_definitions
WHERE code IN ('counts.birth', 'counts.death', 'feed.packing', 'feed.transport');

-- shifting: drop v2, restore v1 as the published version.
DELETE FROM public.sop_versions sv
USING public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code = 'shifting'
  AND sv.version = 2;

UPDATE public.sop_versions sv
SET status = 'published', retired_at = NULL, updated_at = now(), row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code = 'shifting'
  AND sv.version = 1
  AND sv.status = 'retired';

-- feed.direction: back to the pre-existing draft skeleton.
UPDATE public.sop_versions sv
SET version_label = 'v1-skeleton',
    status = 'draft',
    published_at = NULL,
    form_dsl = '{"steps": [{"key": "shed_feed_video", "type": "video", "label": "Shed feed video"}, {"key": "feed_lot", "type": "scan", "label": "Feed lot proof"}, {"key": "quantity_fed", "type": "number", "label": "Quantity fed"}, {"key": "head_count", "type": "number", "label": "Head count"}, {"key": "fed_at", "type": "datetime", "label": "Fed at"}, {"key": "est_vs_used", "type": "number", "label": "Estimated vs used quantity"}, {"key": "verifier_review", "type": "review", "label": "Verifier review"}]}'::jsonb,
    proof_policy = '{"required": ["shed_feed_video", "feed_lot", "quantity_fed"], "verify_capability": "proof.verify"}'::jsonb,
    compatibility = '{}'::jsonb,
    validation_report = '{}'::jsonb,
    updated_at = now(),
    row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code = 'feed.direction'
  AND sv.version = 1;

UPDATE public.sop_definitions
SET status = 'draft',
    name = 'Feed direction',
    description = 'Shed feed direction execution + proof skeleton.',
    updated_at = now(),
    row_version = row_version + 1
WHERE code = 'feed.direction';
