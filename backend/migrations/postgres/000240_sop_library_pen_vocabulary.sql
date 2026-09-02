-- +goose Up
-- seed-fixture-guard:ignore: rewrites seeded SOP display COPY only. No schema change, no
-- HRMS/source fixture change, no read-model change, and no sop_code / field_key / step
-- identity is touched -- an executing task keys on those, never on the sentence beside them.
--
-- The operational location is called a PEN on every screen (maintainer decision 2026-09-02).
-- The admin-web page contracts and the Go-composed labels were renamed in the same change;
-- these four SOP library documents are the remaining SEEDED copy, so they need a forward
-- migration rather than an edit to 000001 / 000175, which STG has already applied and
-- checksums.
--
-- Two of these sentences carried BOTH words with different meanings, and those are rewritten
-- by hand rather than substituted. Feed TRANSPORT is deliberately SHED-GRAIN -- one trip per
-- physical building, never one per pen (AGENTS.md, migration 000152) -- so its copy now says
-- "physical location" instead of "shed"; saying "pen" there would state the opposite of the
-- rule it exists to explain. Only the wording moves; the grain does not.

UPDATE sop_definitions
   SET description = 'Pen or cohort vaccination drive execution with backend-owned proof and verification.'
 WHERE code = 'vaccination.drive'
   AND description = 'Shed or cohort vaccination drive execution with backend-owned proof and verification.';

UPDATE sop_definitions
   SET description = replace(description, 'a shed session', 'a pen session')
 WHERE code = 'feed.direction'
   AND description LIKE '%a shed session%';

UPDATE sop_definitions
   SET description = replace(
         replace(description, 'per active physical shed', 'per active physical location'),
         'a shed load is staged as one trip', 'the whole load is staged as one trip')
 WHERE code = 'feed.transport'
   AND description LIKE '%physical shed%';

UPDATE sop_definitions
   SET description = replace(
         replace(
           replace(description, 'assigns sheds', 'assigns pens'),
           'video(s) per shed', 'video(s) per pen'),
         'never checked against a shed', 'never checked against a pen')
 WHERE code = 'weighing.session'
   AND description LIKE '%assigns sheds%';

-- form_dsl is display copy in the same document. Each replacement names the exact sentence it
-- rewrites, so a document that does not carry it is left alone rather than pattern-mangled.
-- +goose StatementBegin
DO $$
DECLARE
  pairs text[][] := ARRAY[
    -- 'Shed' (feed.transport) and 'Shed / pen' (feed.direction, feed.packing) both land on
    -- 'Pen'. The Down below therefore restores each by SOP CODE, not by the new string, which
    -- on its own could not say which of the two a document started from.
    ['"label": "Shed"',                        '"label": "Pen"'],
    ['"label": "Shed / pen"',                  '"label": "Pen"'],
    ['"label": "Source shed / pen"',           '"label": "Source pen"'],
    ['"label": "Destination shed / pen"',      '"label": "Destination pen"'],
    ['"label": "Birth location (shed / pen)"', '"label": "Birth location (pen)"'],
    ['"label": "Shed proof videos"',           '"label": "Pen proof videos"'],
    ['"label": "Shed proof video(s) (lump-sum)"', '"label": "Pen proof video(s) (lump-sum)"'],
    ['"label": "Load, stage and film the shed trip"', '"label": "Load, stage and film the trip"'],
    ['"label": "CEO assigns sheds (planning is CEO-only; the Growth Director monitors and may execute)"',
     '"label": "CEO assigns pens (planning is CEO-only; the Growth Director monitors and may execute)"'],
    ['"label": "Scan and weigh - free-flow, no roster, no expected counts, no shed-RFID check"',
     '"label": "Scan and weigh - free-flow, no roster, no expected counts, no pen-RFID check"'],
    ['Shed moves are within one park only.',   'Pen moves are within one park only.'],
    ['Add 1 required shed-level video before submit;', 'Add 1 required pen-level video before submit;'],
    ['A lump-sum weigh needs the shed video(s).', 'A lump-sum weigh needs the pen video(s).'],
    ['lump-sum weighs the shed as one total',  'lump-sum weighs the pen as one total'],
    ['never validated against a shed, roster or herd record - the system cannot know what is in a shed and must not try.',
     'never validated against a pen, roster or herd record - the system cannot know what is in a pen and must not try.'],
    ['staged outside the shed.',               'staged outside the pen.'],
    ['One task and one video for the whole shed. Pens are packed as separate bags but loaded and staged as one trip.',
     'One task and one video for the whole location. Pens are packed as separate bags but loaded and staged as one trip.']
  ];
  i int;
BEGIN
  FOR i IN 1 .. array_length(pairs, 1) LOOP
    UPDATE sop_versions
       SET form_dsl = replace(form_dsl::text, pairs[i][1], pairs[i][2])::jsonb
     WHERE form_dsl::text LIKE '%' || replace(replace(pairs[i][1], '\', '\\'), '%', '\%') || '%';
  END LOOP;
END
$$;
-- +goose StatementEnd


-- verification_items.subject_label is COMPOSED AT ENQUEUE and stored, so renaming the Go
-- composers only reaches items created from now on. These two prefixes are the whole set the
-- composers ever produced ('Whole shed · ...' from weighing lump-sum, 'Shed move · ...' from a
-- shifting completion); everything after the prefix is location/weight/count detail and is left
-- exactly as it is.
SET lock_timeout = '5s';
UPDATE verification_items
   SET subject_label = 'Whole pen' || substr(subject_label, length('Whole shed') + 1)
 WHERE subject_label LIKE 'Whole shed%';

UPDATE verification_items
   SET subject_label = 'Pen move' || substr(subject_label, length('Shed move') + 1)
 WHERE subject_label LIKE 'Shed move%';
RESET lock_timeout;

-- +goose Down
-- Copy-only rewrite: the Down restores the exact sentences 000001 / 000175 seeded.
UPDATE sop_definitions
   SET description = 'Shed or cohort vaccination drive execution with backend-owned proof and verification.'
 WHERE code = 'vaccination.drive'
   AND description = 'Pen or cohort vaccination drive execution with backend-owned proof and verification.';

UPDATE sop_definitions
   SET description = replace(description, 'a pen session', 'a shed session')
 WHERE code = 'feed.direction'
   AND description LIKE '%a pen session%';

UPDATE sop_definitions
   SET description = replace(
         replace(description, 'per active physical location', 'per active physical shed'),
         'the whole load is staged as one trip', 'a shed load is staged as one trip')
 WHERE code = 'feed.transport'
   AND description LIKE '%physical location%';

UPDATE sop_definitions
   SET description = replace(
         replace(
           replace(description, 'assigns pens', 'assigns sheds'),
           'video(s) per pen', 'video(s) per shed'),
         'never checked against a pen', 'never checked against a shed')
 WHERE code = 'weighing.session'
   AND description LIKE '%assigns pens%';

-- +goose StatementBegin
DO $$
DECLARE
  pairs text[][] := ARRAY[
    ['"label": "Source pen"',                  '"label": "Source shed / pen"'],
    ['"label": "Destination pen"',             '"label": "Destination shed / pen"'],
    ['"label": "Birth location (pen)"',        '"label": "Birth location (shed / pen)"'],
    ['"label": "Pen proof videos"',            '"label": "Shed proof videos"'],
    ['"label": "Pen proof video(s) (lump-sum)"', '"label": "Shed proof video(s) (lump-sum)"'],
    ['"label": "Load, stage and film the trip"', '"label": "Load, stage and film the shed trip"'],
    ['"label": "CEO assigns pens (planning is CEO-only; the Growth Director monitors and may execute)"',
     '"label": "CEO assigns sheds (planning is CEO-only; the Growth Director monitors and may execute)"'],
    ['"label": "Scan and weigh - free-flow, no roster, no expected counts, no pen-RFID check"',
     '"label": "Scan and weigh - free-flow, no roster, no expected counts, no shed-RFID check"'],
    ['Pen moves are within one park only.',    'Shed moves are within one park only.'],
    ['Add 1 required pen-level video before submit;', 'Add 1 required shed-level video before submit;'],
    ['A lump-sum weigh needs the pen video(s).', 'A lump-sum weigh needs the shed video(s).'],
    ['lump-sum weighs the pen as one total',   'lump-sum weighs the shed as one total'],
    ['never validated against a pen, roster or herd record - the system cannot know what is in a pen and must not try.',
     'never validated against a shed, roster or herd record - the system cannot know what is in a shed and must not try.'],
    ['staged outside the pen.',                'staged outside the shed.'],
    ['One task and one video for the whole location. Pens are packed as separate bags but loaded and staged as one trip.',
     'One task and one video for the whole shed. Pens are packed as separate bags but loaded and staged as one trip.']
  ];
  i int;
BEGIN
  FOR i IN 1 .. array_length(pairs, 1) LOOP
    UPDATE sop_versions
       SET form_dsl = replace(form_dsl::text, pairs[i][1], pairs[i][2])::jsonb
     WHERE form_dsl::text LIKE '%' || replace(replace(pairs[i][1], '\', '\\'), '%', '\%') || '%';
  END LOOP;

  -- The two originals that collapsed to '"label": "Pen"', restored by owning SOP.
  UPDATE sop_versions v
     SET form_dsl = replace(v.form_dsl::text, '"label": "Pen"', '"label": "Shed"')::jsonb
    FROM sop_definitions d
   WHERE d.sop_id = v.sop_id AND d.code = 'feed.transport';

  UPDATE sop_versions v
     SET form_dsl = replace(v.form_dsl::text, '"label": "Pen"', '"label": "Shed / pen"')::jsonb
    FROM sop_definitions d
   WHERE d.sop_id = v.sop_id AND d.code IN ('feed.direction', 'feed.packing');
END
$$;
-- +goose StatementEnd

SET lock_timeout = '5s';
UPDATE verification_items
   SET subject_label = 'Whole shed' || substr(subject_label, length('Whole pen') + 1)
 WHERE subject_label LIKE 'Whole pen%';

UPDATE verification_items
   SET subject_label = 'Shed move' || substr(subject_label, length('Pen move') + 1)
 WHERE subject_label LIKE 'Pen move%';
RESET lock_timeout;
