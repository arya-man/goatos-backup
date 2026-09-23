import test from "node:test";
import assert from "node:assert/strict";
import { comparability, runChecks, summary, layerSentence } from "./check-delta.mjs";
import {
  DELTA_CHECKS, checkByName, readSection, saleFeedReductionDay, earliestShiftingEffectiveDay
} from "./delta-checks.mjs";
import {
  addDays, bindSince, businessDate, isBusinessDate, loadCatalogue, movementSince,
  parseProvenance, snapshotIntegrity
} from "./capture-delta-snapshot.mjs";
import { assertSelectOnly } from "./check-data-sanity.mjs";

const PROVENANCE = { schemaVersion: "000377", databaseName: "goatos", farms: ["F1"], appVersion: null };

function snap(day, sections, overrides = {}) {
  return {
    takenAt: `${day}T04:00:00.000Z`,
    businessDate: day,
    movement: { since: "2026-09-21" },
    takenAgainst: { ...PROVENANCE, ...(overrides.takenAgainst ?? {}) },
    sections,
    ...overrides
  };
}

test("every reading in the catalogue is a single capped read-only statement", () => {
  const catalogue = loadCatalogue();
  assert.ok(catalogue.sections.length >= 10);
  for (const section of catalogue.sections) {
    assertSelectOnly(bindSince(section.sql, "2026-09-22"), section.name);
    assert.ok(section.describes && !/\bselect\b|_id\b/i.test(section.describes), `${section.name} describes itself in farm words`);
  }
});

test("only a date can be bound into a reading's sql", () => {
  assert.throws(() => bindSince("select 1 limit 1", "2026-09-22; drop table goats"), /not a date/);
  assert.throws(() => bindSince("select 1 limit 1", ""), /not a date/);
  assert.equal(bindSince("x $SINCE y", "2026-09-22"), "x '2026-09-22' y");
});

test("a reading is filed under the farm's own day, not the machine's", () => {
  // 20:00 UTC on the 21st is 01:30 IST on the 22nd: the farm's day has already turned over.
  assert.equal(businessDate(new Date("2026-09-21T20:00:00Z")), "2026-09-22");
  assert.equal(businessDate(new Date("2026-09-21T10:00:00Z")), "2026-09-21");
  assert.ok(isBusinessDate("2026-09-22"));
  assert.ok(!isBusinessDate("22/09/2026"));
});

test("a first reading says what window it took instead of quietly reading everything", () => {
  assert.deepEqual(movementSince("2026-09-21", "2026-09-22"), { since: "2026-09-21", bounded: "the previous snapshot's day" });
  const first = movementSince(null, "2026-09-22");
  assert.equal(first.since, addDays("2026-09-22", -3));
  assert.match(first.bounded, /no previous snapshot/);
});

test("a reading counts what was read, never what was listed", () => {
  const integrity = snapshotIntegrity({ sections: { a: { read: true }, b: { read: false }, c: { read: true, capped: true } } });
  assert.deepEqual(integrity, { sectionsListed: 3, sectionsRead: 2, sectionsNotRead: 1, sectionsCapped: 1, complete: false });
  assert.ok(snapshotIntegrity({ sections: { a: { read: true, capped: false } } }).complete);
  // No sections at all is not a complete reading of a quiet farm.
  assert.ok(!snapshotIntegrity({ sections: {} }).complete);
});

test("provenance survives the round trip so two readings can be told apart", () => {
  assert.deepEqual(parseProvenance(["000377", "goatos", "F1,F2"]), { schemaVersion: "000377", databaseName: "goatos", farms: ["F1", "F2"] });
  assert.deepEqual(parseProvenance([]).farms, []);
});

test("two readings of different things are refused rather than compared", () => {
  const before = snap("2026-09-21", {});
  assert.ok(comparability(before, snap("2026-09-22", {})).comparable);
  assert.match(comparability(null, snap("2026-09-22", {})).reason, /only one reading/);
  assert.match(comparability(before, snap("2026-09-21", {})).reason, /same farm day/);
  assert.match(comparability(snap("2026-09-22", {}), before).reason, /wrong way round/);
  assert.match(
    comparability(before, snap("2026-09-22", {}, { takenAgainst: { ...PROVENANCE, schemaVersion: "000378" } })).reason,
    /records were upgraded/
  );
  assert.match(
    comparability(before, snap("2026-09-22", {}, { takenAgainst: { ...PROVENANCE, databaseName: "other" } })).reason,
    /different stores/
  );
  assert.match(
    comparability(
      snap("2026-09-21", {}, { takenAgainst: { ...PROVENANCE, appVersion: "a" } }),
      snap("2026-09-22", {}, { takenAgainst: { ...PROVENANCE, appVersion: "b" } })
    ).reason,
    /new version of the software/
  );
  assert.match(
    comparability(before, snap("2026-09-22", {}, { takenAgainst: { ...PROVENANCE, farms: ["F1", "F2"] } })).reason,
    /different farms/
  );
  assert.match(
    comparability(before, snap("2026-09-22", {}, { movement: { since: "2026-09-19" } })).reason,
    /do not line up/
  );
  // A reading that does not say what it was taken against can never be compared safely.
  assert.match(
    comparability(before, snap("2026-09-22", {}, { takenAgainst: { ...PROVENANCE, schemaVersion: null } })).reason,
    /does not record what it was taken against/
  );
});

test("a refused comparison renders no verdict on any check", () => {
  const { ledger, findings } = runChecks(snap("2026-09-21", {}), snap("2026-09-21", {}));
  assert.equal(findings.length, 0);
  assert.equal(ledger.length, DELTA_CHECKS.length);
  for (const row of ledger) {
    assert.equal(row.ran, false);
    assert.equal(row.outcome, "not checked");
    assert.match(row.reason, /same farm day/);
  }
  const stats = summary(ledger);
  assert.equal(stats.ran, 0);
  assert.match(layerSentence([], stats, comparability(snap("2026-09-21", {}), snap("2026-09-21", {}))), /were not checked/);
});

test("a missing reading is never an empty one", () => {
  assert.equal(readSection({ sections: {} }, "pen_live_counts", "today").ok, false);
  assert.equal(readSection({ sections: { pen_live_counts: { read: false } } }, "pen_live_counts", "today").ok, false);
  assert.equal(readSection({ sections: { pen_live_counts: { read: true, capped: true, rows: [] } } }, "pen_live_counts", "today").ok, false);
  assert.deepEqual(readSection({ sections: { pen_live_counts: { read: true, rows: [] } } }, "pen_live_counts", "today"), { ok: true, rows: [] });
});

test("the farm's own correction clock decides when a sale reaches the feed sheet", () => {
  // Tagged before the cutoff: tomorrow's sheet can still be corrected.
  assert.equal(saleFeedReductionDay("2026-09-21", "10:00", "14:00"), "2026-09-22");
  // Tagged at the cutoff: tomorrow's sheet is already batched, so the day after.
  assert.equal(saleFeedReductionDay("2026-09-21", "14:00", "14:00"), "2026-09-23");
  assert.equal(saleFeedReductionDay("2026-09-21", "16:30", "14:00"), "2026-09-23");
  // A park that corrects later gives the same sale a different, earlier feed day — which is the
  // whole point of reading the park's own clock rather than a constant.
  assert.equal(saleFeedReductionDay("2026-09-21", "16:30", "17:00"), "2026-09-22");
  // No clock on record falls back to the next day, exactly as the farm's own rule does.
  assert.equal(saleFeedReductionDay("2026-09-21", "16:30", null), "2026-09-22");
  assert.equal(saleFeedReductionDay(null, "10:00", "14:00"), null);
});

test("the afternoon cutoff decides the earliest day a pen move may be counted", () => {
  assert.equal(earliestShiftingEffectiveDay("2026-09-21", "13:29"), "2026-09-22");
  assert.equal(earliestShiftingEffectiveDay("2026-09-21", "13:30"), "2026-09-23");
  assert.equal(earliestShiftingEffectiveDay("2026-09-21", "09:00"), "2026-09-22");
  assert.equal(earliestShiftingEffectiveDay("2026-09-21", null), null);
});

test("a pen move planned further out than the rule's earliest day is not an offence", () => {
  const check = checkByName("a_pen_move_raised_late_is_treated_as_tomorrows_work");
  const rows = (effective) => ({
    shifting_raised: { read: true, capped: false, rows: [{ farm: "F1", move: "M1", priority: "normal", raised_on: "2026-09-21", raised_at_time: "09:00", effective_on: effective }] }
  });
  const before = snap("2026-09-21", { shifting_raised: { read: true, capped: false, rows: [] } });
  assert.equal(check.run(before, snap("2026-09-22", rows("2026-09-30"))).breaches.length, 0);
  assert.equal(check.run(before, snap("2026-09-22", rows("2026-09-21"))).breaches.length, 1);
  // A high-priority move is due at once, so this check says nothing about it.
  const high = { shifting_raised: { read: true, capped: false, rows: [{ farm: "F1", move: "M2", priority: "high", raised_on: "2026-09-21", raised_at_time: "15:00", effective_on: "2026-09-21" }] } };
  assert.deepEqual(check.run(before, snap("2026-09-22", high)), { examined: 0, breaches: [] });
});

test("a park with nobody named to walk its pens is owed no visit and is never accused", () => {
  const check = checkByName("a_pen_worked_yesterday_is_owed_a_visit_and_has_none");
  const sections = (visitors) => ({
    pen_care_submitted: { read: true, capped: false, rows: [{ farm: "F1", park: "P9", pen_shed: "S1", worked_on: "2026-09-21", handovers: "1" }] },
    pen_visit_tasks: { read: true, capped: false, rows: [] },
    pen_visit_visitors: { read: true, capped: false, rows: visitors }
  });
  const before = snap("2026-09-21", {
    pen_care_submitted: { read: true, capped: false, rows: [] },
    pen_visit_tasks: { read: true, capped: false, rows: [] },
    pen_visit_visitors: { read: true, capped: false, rows: [] }
  });
  assert.deepEqual(check.run(before, snap("2026-09-22", sections([]))), { examined: 0, breaches: [] });
  assert.equal(check.run(before, snap("2026-09-22", sections([{ farm: "F1", park: "P9", visitor: "U1" }]))).breaches.length, 1);
});

test("work handed in today is not yet late for its next-day visit", () => {
  const check = checkByName("a_pen_worked_yesterday_is_owed_a_visit_and_has_none");
  const today = {
    pen_care_submitted: { read: true, capped: false, rows: [{ farm: "F1", park: "P9", pen_shed: "S1", worked_on: "2026-09-22", handovers: "1" }] },
    pen_visit_tasks: { read: true, capped: false, rows: [] },
    pen_visit_visitors: { read: true, capped: false, rows: [{ farm: "F1", park: "P9", visitor: "U1" }] }
  };
  const before = snap("2026-09-21", { pen_care_submitted: { read: true, capped: false, rows: [] }, pen_visit_tasks: { read: true, capped: false, rows: [] }, pen_visit_visitors: today.pen_visit_visitors });
  assert.deepEqual(check.run(before, snap("2026-09-22", today)), { examined: 0, breaches: [] });
});

test("a herd total that rose is never reported as animals vanishing", () => {
  const check = checkByName("the_herd_total_moved_without_anything_leaving_or_arriving");
  const before = snap("2026-09-21", {
    living_total: { read: true, capped: false, rows: [{ farm: "F1", living: "100" }] },
    exited_animals_open_vaccination: { read: true, capped: false, rows: [] }
  });
  const rose = snap("2026-09-22", {
    living_total: { read: true, capped: false, rows: [{ farm: "F1", living: "104" }] },
    exited_animals_open_vaccination: { read: true, capped: false, rows: [] }
  });
  assert.equal(check.run(before, rose).breaches.length, 0);
  const vanished = snap("2026-09-22", {
    living_total: { read: true, capped: false, rows: [{ farm: "F1", living: "70" }] },
    exited_animals_open_vaccination: { read: true, capped: false, rows: [] }
  });
  assert.equal(check.run(before, vanished).breaches.length, 1);
});

test("nothing to compare is kept apart from everything being fine", () => {
  const empty = { read: true, capped: false, rows: [] };
  const before = snap("2026-09-21", { drive_progress: empty });
  const after = snap("2026-09-22", { drive_progress: empty });
  const { ledger } = runChecks(before, after, [checkByName("a_vaccination_round_went_backwards")]);
  assert.equal(ledger[0].ran, true);
  assert.equal(ledger[0].outcome, "nothing to compare");
  assert.equal(ledger[0].examined, 0);
  assert.match(layerSentence([], summary(ledger), { comparable: true }), /Nothing that these checks watch changed/);
});

test("every finding a person reads is in farm words", () => {
  const banned = /\b(select|sql|tenant_id|shed_id|goat_id|uuid|null|json|column|row_version|table|api|payload|status code)\b/i;
  for (const check of DELTA_CHECKS) {
    for (const field of ["question", "countUnit"]) {
      assert.ok(!banned.test(check[field]), `${check.name}: ${field} leaks a technical word — ${check[field]}`);
    }
    assert.ok(check.page?.title && check.page?.path?.startsWith("/"), `${check.name} names the screen it shows up on`);
    assert.ok(check.rule.length > 40, `${check.name} names the written rule it came from`);
  }
});
