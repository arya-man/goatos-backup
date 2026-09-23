// Every relation in the catalogue, held to both halves of the bargain:
// it FIRES on the bug it was written for, and it stays QUIET on a correct answer,
// on a farm with nothing to report, and on the shapes production actually returns.
//
// A relation that cannot be shown to catch its own bug is not coverage, and a
// relation that fires on a correct page is worse than not having it. Both halves
// are asserted here, per relation, by name.
import assert from "node:assert/strict";
import test from "node:test";
import { loadCatalogue, relationFindings } from "./check-api-contracts.mjs";

const catalogue = loadCatalogue();
const entry = (name) => catalogue.endpoints.find((item) => item.name === name);
const only = (name, ruleName) => entry(name).relations.filter((rule) => rule.name === ruleName);

function fires(endpoint, ruleName, payload, why) {
  const { findings, notAttempted } = relationFindings(payload, only(endpoint, ruleName));
  assert.ok(findings.length > 0, `${ruleName} should have caught ${why}; not attempted: ${JSON.stringify(notAttempted)}`);
}
function quiet(endpoint, ruleName, payload, why) {
  const { findings } = relationFindings(payload, only(endpoint, ruleName));
  assert.equal(findings.length, 0, `${ruleName} cried wolf on ${why}: ${JSON.stringify(findings)}`);
}

const shed = (over = {}) => ({
  parks: [], period_start: "2026-08-24", period_end: "2026-09-23", by_load: [], lump_weighing_dates: [],
  summary: { animals_weighed: 791, individual_animals_weighed: 452, lump_sum_animals_weighed: 339, total_weight_kg: 20886.4,
    average_weight_kg: 26.4, sheds_weighed: 17, sheds_in_scope: 24, at_or_above_30kg: 120, at_or_above_35kg: 41, threshold_basis_animals: 791 },
  rows: [{ park_id: "cbe", location_id: "L1", partition_label: "Part 1", animals_weighed: 220, total_weight_kg: 5742, average_weight_kg: 26.1 }],
  ...over,
});

test("Weights: the two ways of weighing add up to the animals the page claims (b2d61bd54)", () => {
  quiet("weighing_shed_weights", "every weighed animal is in one of the two ways of weighing", shed(), "a correct Weights answer");
  fires("weighing_shed_weights", "every weighed animal is in one of the two ways of weighing",
    shed({ summary: { ...shed().summary, lump_sum_animals_weighed: 263 } }), "452 + 263 against a claimed 791");
});

test("Weights: an average is its own total over its own head count (fa22ed273)", () => {
  quiet("weighing_shed_weights", "each pen's average on the Weights page is its own total over its own head count", shed(), "a correct pen row");
  fires("weighing_shed_weights", "each pen's average on the Weights page is its own total over its own head count",
    shed({ rows: [{ park_id: "cbe", location_id: "L1", partition_label: "Part 1", animals_weighed: 220, total_weight_kg: 5742, average_weight_kg: 261 }] }),
    "an average ten times its own arithmetic");
});

test("Weights: one physical pen is one row, but two farms may share a pen name (2b0320963)", () => {
  fires("weighing_shed_weights", "a pen appears once on the Weights page",
    shed({ rows: [
      { park_id: "cbe", location_id: "L1", partition_label: "Part 1", animals_weighed: 120, total_weight_kg: 3132, average_weight_kg: 26.1 },
      { park_id: "cbe", location_id: "L1", partition_label: "Part 1", animals_weighed: 100, total_weight_kg: 2610, average_weight_kg: 26.1 }] }),
    "one pen split across two rows");
  // CBE and CPT each really do have a Castro 1. Reporting that as a duplicate would
  // undo 2b0320963, which exists because shed names repeat across farms on purpose.
  quiet("weighing_shed_weights", "a pen appears once on the Weights page",
    shed({ rows: [
      { park_id: "cbe", location_id: "L1", partition_label: "", animals_weighed: 54, total_weight_kg: 1425.6, average_weight_kg: 26.4 },
      { park_id: "cpt", location_id: "L9", partition_label: "", animals_weighed: 31, total_weight_kg: 818.4, average_weight_kg: 26.4 }] }),
    "two farms that each have a pen of the same name");
});

const grow = (over = {}) => ({
  parks: [], period_start: "2026-08-24", period_end: "2026-09-23", distribution: [], sale_readiness: {},
  headline: { average_adg_g_per_day: 137.1, headline_animals: 433, losing_animal_count: 1, pair_count: 211 },
  eligibility: { animals_with_two_plus_weighs: 433, total_animals_weighed: 791 },
  weekly_gain: [{ week_start: "2026-09-15", average_adg_g_per_day: 141, animals: 433 }],
  by_park: [{ park_id: "cbe", headline_animals: 300 }, { park_id: "cpt", headline_animals: 133 }],
  shed_leaderboard: [{ park_id: "cbe", location_id: "L1", partition_label: "Part 1", n: 220 }],
  losing_animals: [{ scanned_identifier: "SF-048", previous_weight_kg: 28, latest_weight_kg: 26.4, days_between: 8, adg_g_per_day: -200 }],
  ...over,
});

test("Growth: a daily gain is the weight gained over the days it took (d085b2a4d)", () => {
  quiet("weighing_leadership_growth", "an animal's daily gain is the weight it gained over the days it took", grow(), "a correct Growth answer");
  fires("weighing_leadership_growth", "an animal's daily gain is the weight it gained over the days it took",
    grow({ losing_animals: [{ scanned_identifier: "SF-048", previous_weight_kg: 24, latest_weight_kg: 26.3, days_between: 8, adg_g_per_day: 764 }] }),
    "2.3 kg over 8 days reported as 764 g/day instead of 287");
});

test("Growth: the tile and the list it opens agree, and the farms add up to the herd", () => {
  fires("weighing_leadership_growth", "the losing-animals tile and the list it opens agree",
    grow({ headline: { ...grow().headline, losing_animal_count: 15 } }), "a tile of 15 over a list of 1");
  fires("weighing_leadership_growth", "the farms add up to the herd on the Growth page",
    grow({ headline: { ...grow().headline, headline_animals: 791 } }), "300 + 133 against a herd of 791");
  quiet("weighing_leadership_growth", "the farms add up to the herd on the Growth page",
    grow({ by_park: [] }), "a single farm in scope, where there are no per-farm rows");
});

test("Growth: animals weighed and no gain to show for them is a broken page (2c4d21c76)", () => {
  fires("weighing_leadership_growth", "a farm that weighed animals has gain figures to show for it",
    grow({ weekly_gain: [] }), "433 animals weighed and not one weekly point");
  quiet("weighing_leadership_growth", "a farm that weighed animals has gain figures to show for it",
    grow({ headline: { ...grow().headline, headline_animals: 0 }, weekly_gain: [] }), "a period in which nothing was weighed");
});

const cmd = (kpis = {}) => ({ source: "live", shedVaccineMatrix: [], shedVaccineColumns: [], weeklyGiven: [],
  verificationQueue: [], driveOptions: [], driveOptionsTruncated: false,
  kpis: { targets: 100, missedNotGiven: 2, dosesVerified: 70, awaitingVerification: 12, reworkNeeded: 3,
    overdueNotGiven: 8, scheduledAhead: 2, closedWithoutDose: 3, ...kpis } });

test("Vaccination board: the tiles add up to the total above them (dad4b3c18)", () => {
  quiet("vaccination_command", "the vaccination board's tiles add up to its own total", cmd(), "a correct board");
  fires("vaccination_command", "the vaccination board's tiles add up to its own total",
    cmd({ closedWithoutDose: 0 }), "Total 100 over tiles adding up to 97");
  quiet("vaccination_command", "the vaccination board's tiles add up to its own total",
    cmd({ targets: 0, missedNotGiven: 0, dosesVerified: 0, awaitingVerification: 0, reworkNeeded: 0, overdueNotGiven: 0, scheduledAhead: 0, closedWithoutDose: 0 }),
    "a farm with no vaccination due at all");
});

const lt = (over = {}) => ({ business_date: "2026-09-23", generated_at: "2026-09-23T10:00:00+05:30", is_live_day: true,
  combo: {}, activity: [], attention: [], verification: {}, filter_options: {}, operators_truncated: false, sheds_truncated: false,
  kpis: { scheduled_administrations: 298, closed_administrations: 210, awaiting_close: 88, remaining: 88, proof_videos_received: 298 },
  operators: [{ operator_id: "o1", scheduled_administrations: 200, closed_administrations: 150, remaining: 50 },
              { operator_id: "o2", scheduled_administrations: 98, closed_administrations: 60, remaining: 38 }],
  sheds: [{ shed_id: "s1", scheduled_administrations: 200 }, { shed_id: "s2", scheduled_administrations: 98 }],
  operators_total: 2, sheds_total: 2, ...over });

test("Live tracker: outstanding work is the plan less what actually closed (51bae949a)", () => {
  quiet("vaccination_live_tracker", "what the drive still owes is what was planned less what actually closed", lt(), "a correct tracker");
  fires("vaccination_live_tracker", "what the drive still owes is what was planned less what actually closed",
    lt({ kpis: { scheduled_administrations: 298, closed_administrations: 9, awaiting_close: 289, remaining: 0, proof_videos_received: 298 } }),
    "298 planned, 298 proofed, 9 closed, and it reads as finished");
  fires("vaccination_live_tracker", "each operator's outstanding work is their plan less what they closed",
    lt({ operators: [{ operator_id: "o1", scheduled_administrations: 200, closed_administrations: 200, remaining: 100 }] }),
    "an operator who is finished reading as half done");
});

test("Live tracker: a capped list is not a disagreement", () => {
  fires("vaccination_live_tracker", "the operators listed match the number of operators claimed",
    lt({ operators_total: 7 }), "a board claiming 7 operators and listing 2");
  quiet("vaccination_live_tracker", "the operators listed match the number of operators claimed",
    lt({ operators_total: 7, operators_truncated: true }), "a list the response says it cut short at a cap");
});

test("Live tracker: an operator appears once (0def56a5a)", () => {
  fires("vaccination_live_tracker", "an operator appears once on the live tracker",
    lt({ operators: [{ operator_id: "o1", scheduled_administrations: 149, closed_administrations: 105, remaining: 44 },
                     { operator_id: "o1", scheduled_administrations: 149, closed_administrations: 105, remaining: 44 }] }),
    "the same operator listed twice");
});

const adh = (summary = {}) => ({ source: "live", total_count: 1, projection: {}, rows: [],
  summary: { expected_count: 500, completed_count: 400, adherence_percent: 80, ...summary } });

test("Adherence: the percentage is its own completed over its own expected", () => {
  quiet("protocol_adherence", "the adherence percentage is its own completed over its own expected", adh(), "a correct answer");
  fires("protocol_adherence", "the adherence percentage is its own completed over its own expected",
    adh({ adherence_percent: 12 }), "a percentage that disagrees with the counts behind it");
  quiet("protocol_adherence", "the adherence percentage is its own completed over its own expected",
    adh({ expected_count: 0, completed_count: 0, adherence_percent: 0 }), "nothing expected yet, so there is no share to take");
});

test("a relation that cannot be checked reports itself, and never counts as a pass", () => {
  const { findings, notAttempted } = relationFindings({}, entry("weighing_shed_weights").relations);
  assert.equal(findings.length, 0, "an empty answer is not a disagreement");
  assert.equal(notAttempted.length, entry("weighing_shed_weights").relations.length,
    "every relation must say it could not be checked, rather than going quiet and reading as proof");
  for (const item of notAttempted) assert.ok(item.why, `${item.name} must say why it was not checked`);
});

test("every relation in the catalogue is named, sourced and readable by a farm manager", () => {
  for (const item of catalogue.endpoints) {
    for (const rule of item.relations ?? []) {
      assert.ok(rule.name, `${item.name} has an unnamed relation`);
      assert.ok(rule.sourceCommits?.length, `${rule.name} names no commit it comes from`);
      assert.doesNotMatch(rule.humanFailure, /[a-z]+_[a-z_]+|\[\]|\bnull\b|\bNaN\b/,
        `${rule.name} would print something technical into Slack`);
    }
  }
});

test("a page that only works once it is warm is not a passing page", async () => {
  const { checkEntry } = await import("./check-api-contracts.mjs");
  const item = entry("weighing_shed_weights");
  const good = JSON.stringify({ parks: [], rows: [], by_load: [], lump_weighing_dates: [], period_start: "a", period_end: "b",
    summary: { animals_weighed: 1, average_weight_kg: 26, total_weight_kg: 26, individual_animals_weighed: 1, lump_sum_animals_weighed: 0,
      sheds_weighed: 1, sheds_in_scope: 1, at_or_above_30kg: 0, at_or_above_35kg: 0, threshold_basis_animals: 1 } });
  const answering = (failFirst) => { let n = 0; return async () => { n += 1; const bad = n <= failFirst;
    return { ok: !bad, status: bad ? 500 : 200, url: "https://api/x", headers: { get: () => null }, text: async () => (bad ? "{}" : good) }; }; };
  const run = (failFirst) => checkEntry(item, { baseUrl: "https://api.mesha.sg", headers: {}, samples: 3, warmup: 3, fetchImpl: answering(failFirst) });

  const cold = await run(3);
  assert.equal(cold.passed, false, "a page that fails every time it is asked cold must not report a pass");
  assert.ok(cold.findings.some((f) => f.code === "cold-start-failure"));
  assert.equal((await run(0)).passed, true, "a page that always works must pass");
  assert.equal((await run(1)).passed, true, "one cold blip is not worth waking anyone for");
});
