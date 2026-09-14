-- +goose Up
-- 000308_sop_driven_herd_operations.sql
--
-- seed-fixture-guard:ignore: DDL here is the SOP task-type / category registries and the tasks
-- engine's workflow columns; sop_versions is only NAMED to publish the seeded herd-operations
-- documents by DML. No vaccination / HRMS / goats seed schema moves.
--
-- SOP-DRIVEN HERD OPERATIONS (maintainer decision 2026-09-13, docs/decisions/sop-driven-herd-operations.md).
-- SUPERSEDES the 000175 header ("library documents, not a second execution engine") for the
-- Herd Operations SOPs, the birth/death "templates are CODE-DEFINED" decision, and the
-- reconcile "exactly one video" decision.
--
-- From this migration on, the birth kid / birth mother / death / shifting-completion /
-- reconcile follow-up steps -- WHICH questions, WHICH proof (video x n, photo x n), and WHEN each
-- is due -- are compiled at workflow open from the `follow_up` section of the PUBLISHED
-- sop_versions.form_dsl for the SOP code, and pinned on the workflow (sop_version_id). The
-- maintainer edits them on /counts/sops; open workflows keep the version they opened with.
--
-- Three things land here, in this order:
--   1. META-CONFIG (goatOS_Config_Framework.pdf v2): the Category Registry and the Task Type
--      Registry as DATA. The builder reads them; the engine matches a step's task type to an
--      engine_hook (numeric kg, RFID promotion, kid-pen fallback, colostrum lens, death evidence,
--      pen return). Editing UI for the registries is a later phase (/config).
--   2. workflow_instances.sop_version_id (the pin) and the per-step SOP attributes on
--      workflow_actions, BACKFILLED for every existing row so the engine's generalized gates
--      (hard_time_gate, wait_for_all, requires, after_action_key) reproduce the old key-matched
--      behaviour on rows stamped before this migration.
--   3. The seeded follow_up documents as NEW PUBLISHED VERSIONS of counts.birth (v2),
--      counts.death (v2), shifting (v3) and the NEW counts.reconcile (v1). Each compiles
--      byte-for-byte to the Go template it replaces (tasks/domain golden test), so day-one
--      behaviour does not move. The JSON below is embedded VERBATIM from
--      backend/internal/tasks/domain/sopseed/*.json and pinned by
--      TestMigrationEmbedsTheSeededDocuments -- edit the JSON files, not this SQL.
--
-- seed-migration-guard:ignore owner=manohar issue=sop-driven-herd-operations reason=registry-and-library-seed;idempotent-per-tenant-inserts-plus-backfill;no-clean-slate-seed-command-replays-it expiry=2026-12-31

-- ---------------------------------------------------------------------------
-- 1. Meta-config registries
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS public.sop_categories (
  tenant_id uuid NOT NULL,
  category_key text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  sort_order integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT sop_categories_pkey PRIMARY KEY (tenant_id, category_key),
  CONSTRAINT sop_categories_status_chk CHECK (status IN ('active', 'retired'))
);

CREATE TABLE IF NOT EXISTS public.sop_task_types (
  tenant_id uuid NOT NULL,
  task_type_key text NOT NULL,
  name text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- Which categories may use this step kind (builder filter); empty = every category.
  category_scope jsonb NOT NULL DEFAULT '[]'::jsonb,
  -- What the operator answers: none | yes_no | select | multiselect | number | text.
  answer_kind text NOT NULL DEFAULT 'none',
  -- Server behaviour attached to the step: '' | weigh_kg | tag_kid | record_pen | colostrum_feed | death_evidence | return_to_pen.
  engine_hook text NOT NULL DEFAULT '',
  -- JSON Schema of the parameters the builder asks for when this type is picked.
  parameter_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  sort_order integer NOT NULL DEFAULT 0,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version integer NOT NULL DEFAULT 1,
  CONSTRAINT sop_task_types_pkey PRIMARY KEY (tenant_id, task_type_key),
  CONSTRAINT sop_task_types_answer_kind_chk CHECK (answer_kind IN ('none', 'yes_no', 'select', 'multiselect', 'number', 'text')),
  CONSTRAINT sop_task_types_status_chk CHECK (status IN ('active', 'retired'))
);

INSERT INTO public.sop_categories (tenant_id, category_key, name, description, sort_order)
SELECT t.tenant_id, c.key, c.name, c.description, c.sort_order
FROM public.tenants t
CROSS JOIN LATERAL jsonb_to_recordset($seed$[
  {"key": "commodity", "name": "Commodity", "description": "Physical goods administered or consumed: medicine, vaccine, feed, supplements.", "sort_order": 1},
  {"key": "problem", "name": "Problem", "description": "Health or operational issues that arise: fever, injury, low ADG, mastitis.", "sort_order": 2},
  {"key": "event", "name": "Event", "description": "Scheduled or recurring administrative activity: data collection, filings, updates.", "sort_order": 3},
  {"key": "action", "name": "Action", "description": "Direct operational task on animals or groups: birth, death, shifting, reconcile, weaning, tagging.", "sort_order": 4},
  {"key": "equipment", "name": "Equipment / Asset", "description": "Durable assets needing upkeep: water pump, weighing scale, shed structure, feeder, vehicle.", "sort_order": 5}
]$seed$::jsonb) AS c(key text, name text, description text, sort_order int)
ON CONFLICT (tenant_id, category_key) DO NOTHING;

INSERT INTO public.sop_task_types (tenant_id, task_type_key, name, description, category_scope, answer_kind, engine_hook, parameter_schema, sort_order)
SELECT t.tenant_id, x.key, x.name, x.description, x.category_scope, x.answer_kind, x.engine_hook, x.parameter_schema, e.ordinality
FROM public.tenants t
CROSS JOIN LATERAL jsonb_array_elements($seed$[
  {"key": "record_yes_no", "name": "Question (yes / no)", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "yes_no", "engine_hook": "", "description": "The operator answers Yes or No. Add a video or photo count when the answer must be proven.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "record_select", "name": "Question (pick one)", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "select", "engine_hook": "", "description": "The operator picks exactly one of the authored choices.", "parameter_schema": {"type": "object", "required": ["options"], "properties": {"options": {"type": "array", "minItems": 1, "items": {"type": "string"}}, "proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "record_multiselect", "name": "Question (pick many)", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "multiselect", "engine_hook": "", "description": "The operator ticks every choice that applies.", "parameter_schema": {"type": "object", "required": ["options"], "properties": {"options": {"type": "array", "minItems": 1, "items": {"type": "string"}}, "proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "record_number", "name": "Question (number)", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "number", "engine_hook": "", "description": "The operator types a number.", "parameter_schema": {"type": "object", "properties": {"unit": {"type": "string"}, "proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "record_text", "name": "Question (text)", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "text", "engine_hook": "", "description": "The operator types a short note.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "do_and_confirm", "name": "Do & confirm", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "none", "engine_hook": "", "description": "The operator performs the step and marks it done, with the proof count you set.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "video_record", "name": "Record video", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "none", "engine_hook": "", "description": "One or more live in-app camera videos.", "parameter_schema": {"type": "object", "required": ["proof"], "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "photo_record", "name": "Take photo", "category_scope": ["action", "problem", "event", "equipment", "commodity"], "answer_kind": "none", "engine_hook": "", "description": "One or more live in-app camera photos.", "parameter_schema": {"type": "object", "required": ["proof"], "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "weigh", "name": "Weigh (kg)", "category_scope": ["action"], "answer_kind": "number", "engine_hook": "weigh_kg", "description": "Numeric kilograms; the server validates the range and records the weight on the animal.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "tag", "name": "Tag the animal (RFID)", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "tag_kid", "description": "Opens the RFID assignment; the step cannot complete until the permanent tag is recorded.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "record_pen", "name": "Record pen", "category_scope": ["action"], "answer_kind": "text", "engine_hook": "record_pen", "description": "The operator picks the pen the animal is in; becomes the park's kid pen.", "parameter_schema": {"type": "object"}},
  {"key": "feed_colostrum", "name": "Colostrum feed", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "colostrum_feed", "description": "A colostrum feed counted by the Colostrum page.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "death_evidence", "name": "Death evidence video", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "death_evidence", "description": "A death-trail video released to the verifier once the death is approved.", "parameter_schema": {"type": "object", "required": ["proof"], "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "return_to_pen", "name": "Return animal to pen", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "return_to_pen", "description": "The animal is physically walked back to its registered pen.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "verify", "name": "Verify", "category_scope": ["action", "problem", "commodity", "equipment"], "answer_kind": "yes_no", "engine_hook": "", "description": "Confirm a value or a state before the next step.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "administer", "name": "Administer", "category_scope": ["commodity"], "answer_kind": "none", "engine_hook": "", "description": "Give the medicine / vaccine / supplement.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "inspect", "name": "Inspect", "category_scope": ["equipment", "action"], "answer_kind": "yes_no", "engine_hook": "", "description": "Look at the asset or animal and report its condition.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "transfer", "name": "Transfer", "category_scope": ["action"], "answer_kind": "none", "engine_hook": "", "description": "Move an animal or group.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "clean", "name": "Clean", "category_scope": ["equipment", "action"], "answer_kind": "none", "engine_hook": "", "description": "Clean the asset or pen.", "parameter_schema": {"type": "object", "properties": {"proof": {"$ref": "#/$defs/proof"}}}},
  {"key": "calculate", "name": "Calculate", "category_scope": ["commodity"], "answer_kind": "number", "engine_hook": "", "description": "Work out a dose or quantity from a formula.", "parameter_schema": {"type": "object", "properties": {"formula": {"type": "string"}, "unit": {"type": "string"}}}},
  {"key": "schedule_next", "name": "Schedule next", "category_scope": ["commodity", "event", "equipment"], "answer_kind": "none", "engine_hook": "", "description": "Queue the next occurrence at the given frequency.", "parameter_schema": {"type": "object", "properties": {"frequency_days": {"type": "integer", "minimum": 1}, "occurrences": {"type": "integer", "minimum": 1}}}}
]$seed$::jsonb) WITH ORDINALITY AS e(row, ordinality)
CROSS JOIN LATERAL jsonb_to_record(e.row) AS x(key text, name text, description text, category_scope jsonb, answer_kind text, engine_hook text, parameter_schema jsonb)
ON CONFLICT (tenant_id, task_type_key) DO NOTHING;


-- An Item (sop_definitions) now names its category and may chain to other items (P2).
ALTER TABLE public.sop_definitions ADD COLUMN IF NOT EXISTS category_key text;
ALTER TABLE public.sop_definitions ADD COLUMN IF NOT EXISTS subcategory text NOT NULL DEFAULT '';
ALTER TABLE public.sop_definitions ADD COLUMN IF NOT EXISTS triggers jsonb NOT NULL DEFAULT '[]'::jsonb;

-- ---------------------------------------------------------------------------
-- 2. The pin and the per-step SOP attributes
-- ---------------------------------------------------------------------------
ALTER TABLE public.workflow_instances ADD COLUMN IF NOT EXISTS sop_version_id uuid;

ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS task_type text NOT NULL DEFAULT '';
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS answer_type text NOT NULL DEFAULT 'none';
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS engine_hook text NOT NULL DEFAULT '';
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS proof_min_videos integer NOT NULL DEFAULT 0;
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS proof_min_photos integer NOT NULL DEFAULT 0;
-- Every proof captured for the step, in order, as [{"ref": "...", "kind": "video|photo"}].
-- proof_ref keeps carrying the FIRST video for readers that predate this column.
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS proof_refs jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS hard_time_gate boolean NOT NULL DEFAULT false;
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS wait_for_all boolean NOT NULL DEFAULT false;
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS requires_keys jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS after_action_key text NOT NULL DEFAULT '';
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS after_offset_seconds integer NOT NULL DEFAULT 0;

-- Backfill: rows stamped by the old Go templates get the attributes those templates implied, so
-- the generalized gates behave exactly as the key-matched code did on every open workflow.
UPDATE public.workflow_actions SET
  proof_min_videos = CASE WHEN requires_video THEN 1 ELSE 0 END,
  proof_refs = CASE WHEN proof_ref IS NOT NULL AND proof_ref <> '' THEN jsonb_build_array(jsonb_build_object('ref', proof_ref, 'kind', 'video')) ELSE '[]'::jsonb END,
  answer_type = CASE
    WHEN action_type = 'question_select' THEN 'select'
    WHEN action_key = 'take_weight' THEN 'number'
    WHEN action_key = 'record_shed' THEN 'text'
    WHEN action_type = 'question' THEN 'yes_no'
    ELSE 'none' END,
  engine_hook = CASE action_key
    WHEN 'take_weight' THEN 'weigh_kg'
    WHEN 'tag_the_kid' THEN 'tag_kid'
    WHEN 'record_shed' THEN 'record_pen'
    WHEN 'first_colostrum' THEN 'colostrum_feed'
    WHEN 'death_video' THEN 'death_evidence'
    WHEN 'post_mortem_video' THEN 'death_evidence'
    ELSE CASE WHEN section = 'colostrum_session' THEN 'colostrum_feed' ELSE '' END END,
  hard_time_gate = (section = 'colostrum_session' OR action_key = 'ors_water_2'),
  wait_for_all = (action_key = 'tag_the_kid'),
  requires_keys = CASE WHEN section = 'colostrum_session' THEN '["first_colostrum"]'::jsonb ELSE '[]'::jsonb END,
  after_action_key = CASE WHEN action_key = 'ors_water_2' THEN 'ors_water_1' ELSE '' END,
  after_offset_seconds = CASE WHEN action_key = 'ors_water_2' THEN 3000 ELSE 0 END
WHERE task_type = '';

-- ---------------------------------------------------------------------------
-- 3. Seeded follow_up documents, added to the published versions in place (reconcile is new: v1)
-- ---------------------------------------------------------------------------
UPDATE public.sop_definitions SET category_key = 'action' WHERE code IN ('counts.birth', 'counts.death', 'shifting') AND category_key IS NULL;

INSERT INTO public.sop_definitions (tenant_id, code, name, description, status, category_key)
SELECT t.tenant_id, 'counts.reconcile', 'Pen Reconcile',
       'An animal weighed in a pen other than the one the register names is walked back to its registered pen. The register is truth; the card closes when the animal is returned and the proof is verified.',
       'active', 'action'
FROM public.tenants t
ON CONFLICT (tenant_id, code) DO NOTHING;


-- counts.birth: the seeded follow_up is added IN PLACE to the currently published version, so the
-- version number a farm already sees stays the same (STG: v1 = "the current flow"). No workflow is
-- pinned to any version before this migration, and the library document it extends carried no
-- execution semantics, so nothing running can change. A later web publish creates v+1 as usual.
UPDATE public.sop_versions sv
SET form_dsl = sv.form_dsl || jsonb_build_object('follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "birth_kid", "module": "birth", "label": "Kid", "subject": "kid",
      "steps": [
        {"key": "kid_clean", "task_type": "record_yes_no", "title": "Is the kid clean?", "detail": "Confirm the kid has been cleaned and dried after delivery.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "iodine_dipping", "task_type": "do_and_confirm", "title": "Iodine dipping of umbilical cord", "detail": "Dip the kid's umbilical cord in iodine solution to prevent infection.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "front_teeth_check", "task_type": "record_yes_no", "title": "Are the front teeth outside the lower gum?", "detail": "Check the kid's mouth: the front teeth should be visible outside the lower gum.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "suck_reflex", "task_type": "record_yes_no", "title": "Does the kid have a suck reflex?", "detail": "Place a clean finger in the kid's mouth and confirm it starts sucking.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "first_colostrum", "task_type": "feed_colostrum", "title": "1st Colostrum", "detail": "Feed the first colostrum and record a video using the in-app camera.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "take_weight", "task_type": "weigh", "title": "Take Weight of Kid", "detail": "Weigh the kid and enter the exact weight in kilograms (kg).", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "kid_standing", "task_type": "record_yes_no", "title": "Is the kid standing?", "detail": "One hour after birth, confirm the kid is standing on its own.", "proof": {"video": 1}, "schedule": {"kind": "after_event", "offset_minutes": 60}},
        {"key": "record_shed", "task_type": "record_pen", "title": "Record pen", "detail": "Record which pen this kid is in. This pen becomes the park's kid pen, so the next kid born here is placed there automatically.", "proof": {}, "schedule": {"kind": "immediately"}, "when": "kid_pen_unresolved"},
        {"key": "colostrum_series", "task_type": "feed_colostrum", "section": "colostrum_session", "title": "Colostrum", "title_pattern": "{ordinal} Colostrum", "detail": "Feed colostrum at the {time} session on the {day_label}.", "proof": {"video": 1}, "schedule": {"kind": "series", "times": ["07:00", "11:00", "15:00", "18:30", "22:00"], "days": 2, "pre_notify_minutes": 15, "key_pattern": "colostrum_day_{day}_{hhmm}", "ordinal_start": 2}, "hard_time_gate": true, "requires": ["first_colostrum"]},
        {"key": "tag_the_kid", "task_type": "tag", "title": "Tag the kid", "detail": "Scan or enter the permanent RFID, then record one tagging video. The same goat record is retained and its temporary identifier is retired.", "proof": {"video": 1}, "schedule": {"kind": "at_fixed_time", "day_offset": 2, "time": "07:00"}, "wait_for_all": true}
      ]
    },
    {
      "key": "birth_mother", "module": "birth", "label": "Mother", "subject": "mother",
      "steps": [
        {"key": "babies_still_inside", "task_type": "record_yes_no", "title": "Are any babies still inside?", "detail": "Check whether the mother is still in labour with another kid inside.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "mother_licking", "task_type": "record_yes_no", "title": "Is the mother licking her babies?", "detail": "Confirm the mother has accepted the kids and is licking them clean.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "mothers_medicine", "task_type": "administer", "title": "Mother's Medicine", "detail": "Chocolate Injection at 1.5 ml SQ\nMeloxicam Paracetamol at 4 ml IM\nExapar at 20 ml\nGlucoboost at 100 ml mix with 150gms Concentrate", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "ors_water_1", "task_type": "do_and_confirm", "title": "ORS water", "detail": "Give the mother ORS water to drink after delivery.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "mother_eating", "task_type": "record_yes_no", "title": "Is the mother eating?", "detail": "Confirm the mother has started eating after delivery.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "ors_water_2", "task_type": "do_and_confirm", "title": "ORS water (2nd round)", "detail": "Give the mother a second round of ORS water exactly 50 minutes after the first round was given.", "proof": {"video": 1}, "schedule": {"kind": "after_step", "step": "ors_water_1", "offset_minutes": 50}, "hard_time_gate": true}
      ]
    }
  ]
}$seed$::jsonb),
    validation_report = COALESCE(sv.validation_report, '{}'::jsonb) || '{"seeded_follow_up": "Seeded from the code template this version replaces (migration 000308)."}'::jsonb,
    updated_at = now(), row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'counts.birth'
  AND sv.status = 'published' AND NOT (sv.form_dsl ? 'follow_up');

-- counts.death: the seeded follow_up is added IN PLACE to the currently published version, so the
-- version number a farm already sees stays the same (STG: v1 = "the current flow"). No workflow is
-- pinned to any version before this migration, and the library document it extends carried no
-- execution semantics, so nothing running can change. A later web publish creates v+1 as usual.
UPDATE public.sop_versions sv
SET form_dsl = sv.form_dsl || jsonb_build_object('follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "death", "module": "death", "label": "Death evidence", "subject": "animal",
      "steps": [
        {"key": "death_video", "task_type": "death_evidence", "title": "Record death video", "detail": "Record the dead animal with its ear tag clearly visible using the in-app camera.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}},
        {"key": "post_mortem_video", "task_type": "death_evidence", "title": "Record post-mortem video", "detail": "Record with the timestamp visible. Show the carcass and the post-mortem site in one continuous take using the in-app camera.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}}
      ]
    }
  ]
}$seed$::jsonb),
    validation_report = COALESCE(sv.validation_report, '{}'::jsonb) || '{"seeded_follow_up": "Seeded from the code template this version replaces (migration 000308)."}'::jsonb,
    updated_at = now(), row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'counts.death'
  AND sv.status = 'published' AND NOT (sv.form_dsl ? 'follow_up');

-- shifting: the seeded follow_up is added IN PLACE to the currently published version, so the
-- version number a farm already sees stays the same (STG: v1 = "the current flow"). No workflow is
-- pinned to any version before this migration, and the library document it extends carried no
-- execution semantics, so nothing running can change. A later web publish creates v+1 as usual.
UPDATE public.sop_versions sv
SET form_dsl = sv.form_dsl || jsonb_build_object('follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "shifting", "module": "shifting", "label": "Pen move", "subject": "movement",
      "steps": [
        {"key": "move_video", "task_type": "video_record", "title": "Record the move", "detail": "Record every shifted animal with its ear tag visible inside the destination pen, in one live in-app camera video.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}}
      ]
    }
  ]
}$seed$::jsonb),
    validation_report = COALESCE(sv.validation_report, '{}'::jsonb) || '{"seeded_follow_up": "Seeded from the code template this version replaces (migration 000308)."}'::jsonb,
    updated_at = now(), row_version = sv.row_version + 1
FROM public.sop_definitions sd
WHERE sd.tenant_id = sv.tenant_id AND sd.sop_id = sv.sop_id AND sd.code = 'shifting'
  AND sv.status = 'published' AND NOT (sv.form_dsl ? 'follow_up');

-- counts.reconcile v1: brand new document (there was no library row); capture fields describe the
-- card the weighing consumer raises, follow_up is the operator's work.
INSERT INTO public.sop_versions (tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
SELECT sd.tenant_id, sd.sop_id, 1, 'Pen Reconcile v1', 'published',
       jsonb_build_object(
         'schema_version', 'goatos.sop-form.v1',
         'sop_code', 'counts.reconcile',
         'title', 'Pen Reconcile',
         'fields', jsonb_build_array(
           jsonb_build_object('key', 'scanned_identifier', 'type', 'animal_id_scan', 'label', 'Animal RFID', 'required', true, 'description', 'The tag exactly as the weighing operator scanned it.'),
           jsonb_build_object('key', 'registered_pen', 'type', 'location_picker', 'label', 'Registered pen', 'required', true, 'description', 'Where the register says the animal lives -- the pen to return it to.')
         ),
         'follow_up', $seed${
  "schema_version": "goatos.sop-followup.v1",
  "tracks": [
    {
      "key": "reconcile", "module": "reconcile", "label": "Pen return", "subject": "animal",
      "steps": [
        {"key": "return_to_pen", "task_type": "return_to_pen", "title": "Return the animal to its registered pen", "detail": "Walk the animal back to the pen the register names and record one live video showing the ear tag inside that pen.", "proof": {"video": 1}, "schedule": {"kind": "immediately"}}
      ]
    }
  ]
}$seed$::jsonb
       ),
       '{"subject_scope": "goat", "types": ["video"], "required": true, "minimum_count": 1, "verify_before_apply": true, "approval_before_apply": false}'::jsonb,
       '{"min_app_version": "0.2.0", "supported_field_types": ["animal_id_scan", "location_picker", "video_proof", "photo_proof", "boolean", "select", "multiselect", "number", "text"], "supported_proof_actions": ["photo.capture", "video.capture"], "supported_rule_operators": ["equals", "not_equals", "empty", "not_empty", "in"]}'::jsonb,
       '{"valid": true, "errors": [], "warnings": [{"code": "seeded", "field": "form_dsl", "message": "Seeded from the shipped pen reconciliation card (migration 000308)."}]}'::jsonb,
       now()
FROM public.sop_definitions sd
WHERE sd.code = 'counts.reconcile'
ON CONFLICT (tenant_id, sop_id, version) DO NOTHING;

-- +goose Down
-- Forward-only: the new versions and columns are additive. Retiring them is a maintainer decision.
