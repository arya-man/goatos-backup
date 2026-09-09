// THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09). A feed-packing reading more
// than 500 g from the plan is refused once by the backend (422 measurement_confirmation_required,
// one field error per flagged box, direction only) and lands when she ticks the confirmation and
// presses Accept again. These pin the web half of that contract, source-shape like the rest of this
// folder: the bounce carries the flagged keys and her typed values, the drawer seeds the boxes from
// the bounce, renders backend copy under the flagged box, holds Accept until the tick, and never
// composes a planned figure or a gap of its own.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");
const actionsSource = readFileSync(new URL("./actions.ts", import.meta.url), "utf8");
const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const serverSource = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");

test("the API wrapper carries the envelope's field errors, so the action can name the flagged boxes", () => {
  assert.match(serverSource, /fieldErrors\?: \{ field: string; code: string; message: string \}\[\];/);
  assert.match(serverSource, /fieldErrors: envelopeFieldErrors\(error\.body\)/);
});

test("the action forwards the tick only with per-field readings, and bounces the refusal with keys and values", () => {
  assert.match(
    actionsSource,
    /const acknowledged = String\(formData\.get\("variance_acknowledged"\) \?\? ""\) === "1";/,
    "the confirmation is read off the verdict form",
  );
  assert.match(
    actionsSource,
    /measurement: \{ entries, \.\.\.\(acknowledged \? \{ variance_acknowledged: true \} : \{\}\) \}/,
    "variance_acknowledged rides the entries block and is omitted when unticked",
  );
  assert.match(actionsSource, /result\.error\.code === VARIANCE_CONFIRM_CODE/);
  assert.match(actionsSource, /searchParams\.set\("va_fields", flagged\.join\(","\)\)/);
  assert.match(actionsSource, /searchParams\.set\("va_entries", typed\.join\(","\)\)/);
  // Only a direction CODE travels: the bounce is built from field codes, never from a message or a
  // number the backend did not send.
  assert.match(actionsSource, /\$\{fe\.field\.slice\(VARIANCE_FIELD_PREFIX\.length\)\}:\$\{fe\.code\}/);
});

test("the page parses the bounce and resets it wherever the verdict feedback is reset", () => {
  assert.match(pageSource, /fields: one\(sp, "va_fields"\), entries: one\(sp, "va_entries"\)/);
  const resets = pageSource.match(/va_code: null/g) ?? [];
  const fieldResets = pageSource.match(/va_fields: null/g) ?? [];
  const entryResets = pageSource.match(/va_entries: null/g) ?? [];
  assert.ok(resets.length >= 3, "the page resets verdict feedback in several places");
  assert.equal(fieldResets.length, resets.length, "every va_code reset must also drop va_fields");
  assert.equal(entryResets.length, resets.length, "every va_code reset must also drop va_entries");
  assert.match(pageSource, /"va_status", "va_code", "va_fields", "va_entries"\]/, "the filter form must not re-post the bounce");
});

test("the drawer seeds her readings from the bounce and warns under the flagged box with backend copy", () => {
  assert.match(drawerSource, /const bouncedEntries = varianceBounce \? parsePairs\(feedback\.entries\) : \{\};/);
  assert.match(drawerSource, /values: bouncedEntries,/, "the entry boxes are seeded from the bounce, not left empty");
  assert.match(drawerSource, /text\(`verdict\.variance\.\$\{activeVarianceWarnings\[field\.key\]\}`\)/, "the warning is backend copy keyed by the direction code");
  // A warning follows the VALUE that was flagged: editing the box clears it.
  assert.match(drawerSource, /\(entriesForItem\[key\] \?\? ""\)\.trim\(\) === \(bouncedEntries\[key\] \?\? ""\)\.trim\(\)/);
  // No planned figure, no gap: the drawer never subtracts or formats a kg difference.
  assert.doesNotMatch(drawerSource, /planned_kg|plannedKg|- Number\(/);
});

test("Accept is held until the confirmation is ticked, and the tick is a fresh act per item", () => {
  assert.match(drawerSource, /name="variance_acknowledged"/);
  assert.match(drawerSource, /disabled=\{verdictSettled \|\| !hasEvidence \|\| measurementMissing \|\| varianceUnconfirmed\}/);
  assert.match(drawerSource, /useState<string>\(""\)/, "the tick starts unticked; it is never seeded from the bounce");
  assert.match(drawerSource, /const varianceAcknowledged = varianceAcknowledgedFor === item\.item_id;/, "keyed by item like every other input");
});
