import assert from "node:assert/strict";
import test from "node:test";

import { ageBandLabel, toDeathRows, toDiseaseRows } from "./health-analytics-rows.ts";

// The real lib/format.fmtDate, reimplemented here for the test only because the module under
// test takes the formatter as an argument. Kept byte-identical in OUTPUT shape (DD-MM-YYYY).
const fmtDate = (iso) => {
  const [y, m, d] = iso.split("-");
  return `${d}-${m}-${y}`;
};

const AGE_BANDS = { adult: "Adult", kid: "Kid", both: "Both", unknown: "Not recorded" };
const DEATH_LABELS = { never: "No case on record", unattributed: "Not attributed", noTag: "No tag recorded" };

function deathRow(overrides) {
  return {
    goat_id: "g-1",
    display_id: "G-710002",
    tag: "982000123456789",
    operational_location_display: "Mandela 1 - Part 3",
    park_label: "CPT",
    business_date: "2026-09-02",
    age_band: "kid",
    attribution: "attributed",
    disease_label: "Fever",
    never_diagnosed: false,
    cause_recorded: true,
    days_under_treatment: 3,
    ...overrides,
  };
}

// A RECORDED cause and an INFERRED one both read as attributed, and they are not the same
// claim: the first is the disease the operator named on the death form, the second says
// only that a case happened to be open when the animal died. The row must carry which it
// is, or the table shows a guess and a fact in identical type.
test("a death row distinguishes a recorded cause from an inferred one", () => {
  const [recorded] = toDeathRows([deathRow()], DEATH_LABELS, AGE_BANDS, fmtDate);
  assert.equal(recorded.attributed, true);
  assert.equal(recorded.causeRecorded, true);

  const [inferred] = toDeathRows(
    [deathRow({ cause_recorded: false })],
    DEATH_LABELS,
    AGE_BANDS,
    fmtDate,
  );
  assert.equal(inferred.attributed, true);
  assert.equal(inferred.causeRecorded, false);
});

// A wire that has not been regenerated yet, or an older server, sends no flag at all. That
// must read as NOT recorded: claiming a cause the farm never named is the one direction
// this feature must never fail in.
test("a missing cause_recorded flag reads as inferred, never as recorded", () => {
  const row = deathRow();
  delete row.cause_recorded;
  const [mapped] = toDeathRows([row], DEATH_LABELS, AGE_BANDS, fmtDate);
  assert.equal(mapped.causeRecorded, false);
});

test("an attributed death carries its disease and its days under treatment", () => {
  const [row] = toDeathRows([deathRow()], DEATH_LABELS, AGE_BANDS, fmtDate);
  assert.equal(row.attributed, true);
  assert.equal(row.diseaseLabel, "Fever");
  assert.equal(row.daysUnderTreatment, 3);
  assert.equal(row.ageBandLabel, "Kid");
});

// THE RULE THIS PAGE TURNS ON. No cause of death is recorded anywhere, so a death with no case open
// has no disease -- and the mapping DROPS one even if the wire carries it. The page must not be
// the place a clinical fact is invented, and a backend regression must not be able to make it
// one.
test("an unattributed death is never given a disease, even if the wire sends one", () => {
  const [row] = toDeathRows(
    [deathRow({ attribution: "unattributed", disease_label: "Fever", days_under_treatment: 9 })],
    DEATH_LABELS,
    AGE_BANDS,
    fmtDate,
  );
  assert.equal(row.attributed, false);
  assert.equal(row.diseaseLabel, "");
  assert.equal(row.daysUnderTreatment, null);
});

// "No case on record" and "the case had already closed" are different facts about the animal.
// Both are unattributed; collapsing them would hide the detection gap the page exists to show.
test("never-diagnosed and closed-case deaths carry different labels", () => {
  const [never] = toDeathRows(
    [deathRow({ attribution: "unattributed", disease_label: "", never_diagnosed: true, days_under_treatment: null })],
    DEATH_LABELS,
    AGE_BANDS,
    fmtDate,
  );
  const [closed] = toDeathRows(
    [deathRow({ attribution: "unattributed", disease_label: "", never_diagnosed: false, days_under_treatment: null })],
    DEATH_LABELS,
    AGE_BANDS,
    fmtDate,
  );
  assert.equal(never.attributionLabel, "No case on record");
  assert.equal(closed.attributionLabel, "Not attributed");
  assert.notEqual(never.attributionLabel, closed.attributionLabel);
});

// An animal with no identifier is a DATA GAP and says so. An empty cell would read as a loading
// failure, and the farm reads an animal by its RFID.
test("an animal with no tag renders the backend's own gap copy", () => {
  const [row] = toDeathRows([deathRow({ tag: "" })], DEATH_LABELS, AGE_BANDS, fmtDate);
  assert.equal(row.animalLabel, "No tag recorded");
});

// The farm reads DD-MM-YYYY. Rendering the wire's ISO date would put the API's own format in
// front of the operator (admin-web Date Display Rule), while sorting must still use the ISO
// value or the column orders by day-of-month.
test("a death row renders DD-MM-YYYY and keeps the ISO value for sorting", () => {
  const [row] = toDeathRows([deathRow({ business_date: "2026-09-03" })], DEATH_LABELS, AGE_BANDS, fmtDate);
  assert.equal(row.date, "03-09-2026");
  assert.equal(row.sortDate, "2026-09-03");
});

test("age band falls back to the contract's not-recorded copy for an unknown value", () => {
  assert.equal(ageBandLabel(AGE_BANDS, "adult"), "Adult");
  assert.equal(ageBandLabel(AGE_BANDS, "both"), "Both");
  assert.equal(ageBandLabel(AGE_BANDS, ""), "Not recorded");
  assert.equal(ageBandLabel(AGE_BANDS, "weanling"), "Not recorded");
});

// The page renders the backend's rate verbatim. Re-deriving died/new_cases here would be the
// banned read-time rollup and would disagree with the CSV export of the same field.
test("the disease board carries the backend's own case-fatality figure", () => {
  const [row] = toDiseaseRows(
    [
      {
        key: "FEVER",
        key_kind: "register_rule",
        label: "Fever",
        age_bands: "adult",
        new_cases: 3,
        open_cases: 1,
        recovered: 1,
        died: 1,
        case_fatality_pct: 33.3,
      },
    ],
    AGE_BANDS,
  );
  assert.equal(row.caseFatalityPct, 33.3);
  assert.equal(row.key, "FEVER");
  assert.equal(row.label, "Fever");
});
