-- +goose Up
-- SOP Library coverage for the shipped Milk and Weighing workflows (maintainer request 2026-08-22).
--
-- Follows the 000175 pattern exactly: these rows are authoritative LIBRARY DOCUMENTS of
-- flows that already ship, NOT a second execution engine — sop_tasks are only ever created
-- for vaccination.drive. Milk keeps its canonical tables (milk_preparation_completions /
-- milk_feeding_tasks) and Weighing keeps its own (weighing_campaigns / weighing_observations).
--
-- Documents mirror the LOCKED decisions verbatim (AGENTS.md):
--   * Milk Preparation: one farm-day task per park; answers include the mandatory
--     UHT-milk litres and citric-acid grams plus the conditional goat-milk trio
--     (quantity, boil, cool); 2-5 distinct step videos, one immutable attempt; ONE
--     verifier approval completes it, and the approve forwards the accepted attempt's
--     UHT litres into the feed stock ledger (maintainer decision 2026-08-22). The milk
--     is prepared today for TOMORROW's feeding.
--   * Milk Feeding: four daily farm-session tasks; each submission is one immutable
--     farm-session attempt carrying the watchlist answers and TWO distinct live-camera
--     videos (clean bottles, mixing + filling). Approval applies that session's
--     watchlist answers: two consecutive accepted Yes answers graduate a kid, a No
--     resets the streak.
--   * Weighing is SCAN-AND-SUBMIT (2026-08-03): planning is CEO-only; individual mode
--     is scan RFID + weight + video per animal, lump-sum is total weight + head count +
--     video(s) per shed; the ONLY business rule is no duplicate scan in the same bucket
--     before submit. NO shed-RFID validation, NO roster/expected counts, NO herd or
--     clinical lookups. Verification grain follows the EVIDENCE; the verifier's
--     corrected weight rides the Approve itself (2026-08-20), blank keeps the
--     operator's weight. Close is unconditional on resolved verification; reopen is the
--     only other verb.
--
-- Idempotent per tenant; safe on an empty tenant table.

-- ---------------------------------------------------------------------------
-- 1. milk.preparation - Milk Preparation (farm-day, verifier-gated)
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'milk.preparation', 'Milk Preparation',
       'Prepare the kid-milk for tomorrow: one task per farm per day. The operator records collected goat milk, the conditional goat-milk boil/cool steps, the UHT milk opened and the citric acid mixed, each proved by its own live-camera video in one immutable attempt. One verifier approval completes the farm-day; the accepted UHT litres also deplete the UHT Milk feed stock.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Milk Preparation v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "milk.preparation",
  "title": "Milk Preparation",
  "fields": [
    {"key": "farm", "type": "location_picker", "label": "Farm", "required": true, "option_source": "locations.active",
     "description": "One preparation per farm per day. The plan lists every milk cohort (K1 / K2 / K3) with its per-head ml across the four feeding sessions; today''s preparation feeds TOMORROW."},
    {"key": "morning_milk_collected_litres", "type": "number", "label": "Goat milk collected - morning (litres)", "required": true, "min": 0,
     "description": "May legitimately be zero."},
    {"key": "evening_milk_collected_litres", "type": "number", "label": "Goat milk collected - evening (litres)", "required": true, "min": 0},
    {"key": "goat_milk_used", "type": "select", "label": "Goat milk used in this preparation", "options": ["yes", "no"], "required": true,
     "description": "Yes unlocks the goat-milk quantity, boiling and cooling steps; No hides them - answering them anyway is rejected."},
    {"key": "goat_milk_quantity_litres", "type": "number", "label": "Goat milk quantity (litres)", "required": false, "min": 0,
     "description": "Required and positive when goat milk is used."},
    {"key": "boiling_temperature_c", "type": "number", "label": "Boiling temperature (C)", "required": false, "min": 0},
    {"key": "cooled_temperature_c", "type": "number", "label": "Cooled temperature (C)", "required": false, "min": 0},
    {"key": "uht_milk_quantity_litres", "type": "number", "label": "UHT milk quantity (litres)", "required": true, "min": 0,
     "description": "Always required and positive. On verifier approval this accepted answer is recorded into the UHT Milk feed stock ledger - the store depletes on the PREPARATION day, when the packets are opened."},
    {"key": "citric_acid_grams", "type": "number", "label": "Citric acid (grams)", "required": true, "min": 0,
     "description": "Always required and positive; the plan directs 5.5 g per litre of prepared milk."},
    {"key": "goat_milk_quantity_video", "type": "video_proof", "label": "Goat milk quantity video", "required": false, "proof_action": "video.capture",
     "description": "Required when goat milk is used. Live in-app camera only; one video cannot prove two steps."},
    {"key": "boiling_temperature_video", "type": "video_proof", "label": "Boiling temperature video", "required": false, "proof_action": "video.capture"},
    {"key": "cooled_temperature_video", "type": "video_proof", "label": "Cooled temperature video", "required": false, "proof_action": "video.capture"},
    {"key": "uht_milk_quantity_video", "type": "video_proof", "label": "UHT milk quantity video", "required": true, "proof_action": "video.capture"},
    {"key": "citric_acid_mixing_video", "type": "video_proof", "label": "Citric acid mixing video", "required": true, "proof_action": "video.capture"}
  ],
  "rules": [
    {"type": "block_submission_if", "when": {"field": "uht_milk_quantity_litres", "operator": "empty"},
     "message": "UHT milk quantity is required and must be positive."},
    {"type": "block_submission_if", "when": {"field": "citric_acid_grams", "operator": "empty"},
     "message": "Citric acid grams are required and must be positive."},
    {"type": "proof_required_if", "when": {"field": "farm", "operator": "not_empty"},
     "field": "uht_milk_quantity_video", "message": "The UHT milk quantity video is required."},
    {"type": "proof_required_if", "when": {"field": "farm", "operator": "not_empty"},
     "field": "citric_acid_mixing_video", "message": "The citric acid mixing video is required."},
    {"type": "proof_required_if", "when": {"field": "goat_milk_used", "operator": "equals", "value": "yes"},
     "field": "goat_milk_quantity_video", "message": "Goat milk quantity video is required when goat milk is used."},
    {"type": "proof_required_if", "when": {"field": "goat_milk_used", "operator": "equals", "value": "yes"},
     "field": "boiling_temperature_video", "message": "Boiling temperature video is required when goat milk is used."},
    {"type": "proof_required_if", "when": {"field": "goat_milk_used", "operator": "equals", "value": "yes"},
     "field": "cooled_temperature_video", "message": "Cooled temperature video is required when goat milk is used."}
  ],
  "workflow": {
    "nodes": [
      {"key": "plan", "type": "operator_execution", "label": "Read the day''s plan - per-cohort ml across four sessions, citric acid at 5.5 g/L"},
      {"key": "operator_submission", "type": "operator_execution", "label": "Prepare, answer and film each step (one immutable attempt, 2-5 distinct videos)"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Verifier review - one farm-day item carrying every step video"},
      {"key": "accepted", "type": "accepted", "label": "Preparation completed - UHT litres recorded into feed stock"},
      {"key": "rework", "type": "rework", "label": "Rework - a fresh attempt; rejected evidence stays immutable history"}
    ],
    "edges": [
      {"from": "plan", "to": "operator_submission", "condition": "prepared"},
      {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "task", "types": ["video"], "required": true, "minimum_count": 2, "maximum_count": 5, "verify_before_apply": true}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped farm-day milk preparation verification flow; the approve also records the accepted UHT litres into the feed stock ledger (2026-08-22)."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'milk.preparation'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 2. milk.feeding - Milk Feeding (four daily farm-session tasks, verifier-gated)
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'milk.feeding', 'Milk Feeding',
       'Give the prepared milk: four daily feeding sessions per farm. Each session is one immutable attempt carrying the drinking watchlist answers and two distinct live-camera videos - clean bottles, then mixing and filling. Verifier approval completes exactly one farm x date x session and applies that session''s watchlist answers.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Milk Feeding v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "milk.feeding",
  "title": "Milk Feeding",
  "fields": [
    {"key": "farm", "type": "location_picker", "label": "Farm", "required": true, "option_source": "locations.active"},
    {"key": "session", "type": "number", "label": "Feeding session", "required": true, "min": 1,
     "description": "One of the four daily sessions. Each session is its own task, its own attempt and its own verification."},
    {"key": "watchlist_answers", "type": "select", "label": "Watchlist - did each listed kid drink?", "options": ["yes", "no"], "repeat": true, "required": true,
     "description": "Every kid on the refusal watchlist must be answered exactly once. Two CONSECUTIVE verifier-accepted Yes answers graduate a kid off the list; a No resets the streak."},
    {"key": "new_refusals", "type": "goat_lookup", "label": "New refusals this session", "repeat": true, "required": false,
     "description": "A kid that refused to drink joins the watchlist with remarks."},
    {"key": "clean_bottles_video", "type": "video_proof", "label": "Clean bottles video", "required": true, "proof_action": "video.capture",
     "description": "Live in-app camera. Must be a different video from mixing and filling - one clip cannot prove two steps."},
    {"key": "mixing_and_filling_video", "type": "video_proof", "label": "Mixing and filling video", "required": true, "proof_action": "video.capture"}
  ],
  "rules": [
    {"type": "block_submission_if", "when": {"field": "session", "operator": "empty"},
     "message": "The feeding session is required."},
    {"type": "proof_required_if", "when": {"field": "farm", "operator": "not_empty"},
     "field": "clean_bottles_video", "message": "The clean bottles video is required."},
    {"type": "proof_required_if", "when": {"field": "farm", "operator": "not_empty"},
     "field": "mixing_and_filling_video", "message": "The mixing and filling video is required."}
  ],
  "workflow": {
    "nodes": [
      {"key": "materialize", "type": "operator_execution", "label": "Four session tasks materialize for the farm day"},
      {"key": "operator_submission", "type": "operator_execution", "label": "Feed, answer the watchlist and film both steps (one immutable attempt)"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Verifier review - one item per farm x date x session"},
      {"key": "accepted", "type": "accepted", "label": "Session completed - watchlist answers applied (two accepted Yes in a row graduates a kid)"},
      {"key": "rework", "type": "rework", "label": "Rework - re-shoot; prior attempts stay immutable history"}
    ],
    "edges": [
      {"from": "materialize", "to": "operator_submission", "condition": "session_due"},
      {"from": "operator_submission", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"}
    ]
  }
}'::jsonb,
'{"subject_scope": "task", "types": ["video"], "required": true, "minimum_count": 2, "verify_before_apply": true}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped four-session farm-grain milk feeding verification flow with the drinking watchlist (two consecutive accepted Yes answers graduate a kid; No resets the streak)."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'milk.feeding'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 3. weighing.session - Weighing Session (scan-and-submit, free-flow)
-- ---------------------------------------------------------------------------
INSERT INTO public.sop_definitions (tenant_id, code, name, description, status)
SELECT t.tenant_id, 'weighing.session', 'Weighing Session',
       'Scan-and-submit weighing. The CEO assigns sheds; the operator (or Growth Director) weighs them: individual is scan RFID + weight + video per animal, lump-sum is total weight + head count + video(s) per shed. The only business rule is that an animal cannot be scanned twice in the same bucket before submit - a scanned tag is stored verbatim and never checked against a shed, roster or herd record. Verification follows the evidence; close is unconditional on resolved verification.',
       'active'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;

INSERT INTO public.sop_versions
  (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Weighing Session v1', 'published',
'{
  "schema_version": "goatos.sop-form.v1",
  "sop_code": "weighing.session",
  "title": "Weighing Session",
  "fields": [
    {"key": "mode", "type": "select", "label": "Weighing mode", "options": ["individual", "lump_sum"], "required": true,
     "description": "Individual weighs animals one by one; lump-sum weighs the shed as one total. The mode decides the evidence grain, and verification follows the evidence."},
    {"key": "rfid", "type": "animal_id_scan", "label": "Animal RFID (individual)", "repeat": true, "required": false,
     "description": "Free-flow: the scanned tag is stored VERBATIM and is never validated against a shed, roster or herd record - the system cannot know what is in a shed and must not try. The one rule: the same tag cannot be scanned twice in this bucket before submit."},
    {"key": "weight_kg", "type": "number", "label": "Weight (kg) per animal (individual)", "required": false, "min": 0},
    {"key": "animal_video", "type": "video_proof", "label": "Per-animal proof video (individual)", "required": false, "proof_action": "video.capture",
     "description": "One video per animal means one review per animal - the verification grain follows the evidence."},
    {"key": "total_weight_kg", "type": "number", "label": "Total weight (kg) (lump-sum)", "required": false, "min": 0},
    {"key": "animal_count", "type": "number", "label": "Animal count (lump-sum)", "required": false, "min": 1},
    {"key": "shed_video", "type": "video_proof", "label": "Shed proof video(s) (lump-sum)", "required": false, "proof_action": "video.capture"}
  ],
  "rules": [
    {"type": "proof_required_if", "when": {"field": "mode", "operator": "equals", "value": "individual"},
     "field": "animal_video", "message": "Each weighed animal needs its own video."},
    {"type": "proof_required_if", "when": {"field": "mode", "operator": "equals", "value": "lump_sum"},
     "field": "shed_video", "message": "A lump-sum weigh needs the shed video(s)."}
  ],
  "workflow": {
    "nodes": [
      {"key": "plan", "type": "approval", "label": "CEO assigns sheds (planning is CEO-only; the Growth Director monitors and may execute)"},
      {"key": "capture", "type": "operator_execution", "label": "Scan and weigh - free-flow, no roster, no expected counts, no shed-RFID check"},
      {"key": "submit", "type": "operator_execution", "label": "Submit the bucket"},
      {"key": "proof_verification", "type": "proof_verification", "label": "Verifier review at the evidence grain - a corrected weight rides the Approve itself; blank keeps the operator''s weight"},
      {"key": "accepted", "type": "accepted", "label": "Verified"},
      {"key": "rework", "type": "rework", "label": "Rework - re-weigh and re-film"},
      {"key": "closed", "type": "accepted", "label": "Closed - only possible once no verification is pending; reopen is the only other verb"}
    ],
    "edges": [
      {"from": "plan", "to": "capture", "condition": "shed_assigned"},
      {"from": "capture", "to": "submit", "condition": "no_duplicate_scan_in_bucket"},
      {"from": "submit", "to": "proof_verification", "condition": "proof_required"},
      {"from": "proof_verification", "to": "accepted", "condition": "verifier_approves"},
      {"from": "proof_verification", "to": "rework", "condition": "verifier_rejects"},
      {"from": "accepted", "to": "closed", "condition": "close_gate_unconditional"}
    ]
  }
}'::jsonb,
'{"subject_scope": "goat", "lump_sum_subject_scope": "shed", "types": ["video"], "required": true, "minimum_count": 1, "verify_before_apply": false}'::jsonb,
'{"min_app_version": "0.2.0", "supported_field_types": ["text", "number", "date_time", "select", "multiselect", "goat_lookup", "animal_id_scan", "location_picker", "photo_proof", "video_proof"], "supported_proof_actions": ["video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
'{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the locked scan-and-submit weighing model (2026-08-03): free-flow capture, evidence-grain verification, approve-carries-the-corrected-weight (2026-08-20), unconditional close gate."}]}'::jsonb,
now()
FROM public.sop_definitions sd
WHERE sd.code = 'weighing.session'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
-- Remove the library documents this migration added. Safe because none of these SOP
-- codes ever creates sop_tasks/submissions (only vaccination.drive does).
DELETE FROM public.sop_versions sv
USING public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id
  AND sd.tenant_id = sv.tenant_id
  AND sd.code IN ('milk.preparation', 'milk.feeding', 'weighing.session');

DELETE FROM public.sop_definitions
WHERE code IN ('milk.preparation', 'milk.feeding', 'weighing.session');
