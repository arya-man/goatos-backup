import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const board = readFileSync(new URL("./live-tracker-board.tsx", import.meta.url), "utf8");
const kpis = readFileSync(new URL("./live-tracker-kpis.tsx", import.meta.url), "utf8");
const operators = readFileSync(new URL("./live-tracker-operators.tsx", import.meta.url), "utf8");
const sheds = readFileSync(new URL("./live-tracker-sheds.tsx", import.meta.url), "utf8");
const combo = readFileSync(new URL("./live-tracker-combo.tsx", import.meta.url), "utf8");
const rail = readFileSync(new URL("./live-tracker-rail.tsx", import.meta.url), "utf8");
const poller = readFileSync(new URL("./live-poller.tsx", import.meta.url), "utf8");
const params = readFileSync(new URL("./params.ts", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

// Assertions about what the page RENDERS must not be satisfied or defeated by prose in a comment —
// several of these comments name the exact mock defect they exist to avoid reproducing.
function code(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

test("the board makes exactly one backend read", () => {
  const calls = board.match(/getVaccinationLiveTracker\(/g) ?? [];
  assert.equal(calls.length, 1, "the page must not fan out into several vaccination reads");
  assert.match(board, /export function loadLiveTracker/);
});

test("all three render branches exist — error, empty, full", () => {
  assert.match(board, /if \(!result\.ok\)/, "a failed read must render its own visible error card");
  assert.match(board, /result\.error\.message/, "the backend message must reach the screen");
  assert.match(board, /state\.error_title/);
  assert.match(board, /state\.empty_title/, "an empty-but-ok day still renders every section");
  assert.match(board, /action\.reset_filters/);
});

test("every one of the mock's six KPI tiles is present, at the documented grain", () => {
  for (const key of [
    "kpi.scheduled.label",
    "kpi.proofs.label",
    "kpi.scans.label",
    "kpi.remaining.label",
    "kpi.combo.label",
    "kpi.attention.label",
  ]) {
    assert.ok(kpis.includes(key), `missing KPI tile ${key}`);
  }
  assert.match(kpis, /kpis\.combo_animals/, "combo tile is ANIMAL grain and must read the animal count");
  assert.match(kpis, /kpis\.scheduled_administrations/, "scheduled tile is ADMINISTRATION grain");
});

test("the KPI cross-filter the mock implied is disabled with a visible reason, not silently inert", () => {
  assert.match(kpis, /aria-disabled="true"/);
  assert.match(kpis, /kpi\.cross_filter_disabled/);
});

test("mock sections all render: operators, sheds, combo, activity, attention, verification", () => {
  assert.match(operators, /section\.operators\.title/);
  assert.match(sheds, /section\.sheds\.title/);
  assert.match(combo, /section\.combo\.title/);
  assert.match(rail, /section\.activity\.title/);
  assert.match(rail, /section\.attention\.title/);
  assert.match(rail, /section\.verification\.title/);
});

test("no synthetic animal id is ever rendered — real tags only", () => {
  for (const [name, source] of Object.entries({ combo, rail, operators, sheds })) {
    assert.ok(!/GT-/.test(code(source)), `${name} must not render the mock's fabricated GT-##### id`);
  }
  assert.match(combo, /row\.primary_tag/);
  assert.match(combo, /row\.secondary_tag/, "a dual-tagged animal must surface its second tag");
});

test("combo rows open the shared Goat Passport local overlay", () => {
  assert.match(combo, /LocalOverlayLink/);
  assert.match(board, /LiveTrackerPassportDrawer/);
});

test("the three unbacked attention affordances stay visible and disabled with reasons", () => {
  assert.match(rail, /section\.attention\.nudge/);
  assert.match(rail, /section\.attention\.escalation/);
  assert.match(rail, /section\.attention\.pace/);
  const disabled = rail.match(/aria-disabled="true"/g) ?? [];
  assert.ok(disabled.length >= 3, "each unbacked affordance carries aria-disabled with a title reason");
});

test("the feed rate is measured, never the mock's hardcoded value", () => {
  assert.ok(!/~\d+\/min/.test(code(rail)), "the mock's hardcoded rate must not be reproduced");
  assert.match(rail, /activity\.observed_per_min/);
  assert.match(rail, /live\.feed_rate_unavailable/, "no measurable window must say so rather than guess");
});

test("the feed is announced to assistive technology", () => {
  assert.match(rail, /aria-live="polite"/);
});

test("polling re-runs the server tree and never stacks requests", () => {
  assert.match(poller, /router\.refresh\(\)/);
  assert.ok(!/useQuery|react-query/.test(code(poller)), "this page must not become the app's only client fetcher");
  assert.match(poller, /pendingRef\.current/, "a refresh in flight must suppress the next tick");
  assert.match(poller, /visibilityState/, "a hidden tab must not keep polling");
  assert.match(poller, /goat_passport/, "an open drawer must not be refreshed out from under the reader");
});

test("timestamps render in the business timezone, not the viewer's", () => {
  const format = readFileSync(new URL("./format.ts", import.meta.url), "utf8");
  assert.match(format, /Asia\/Kolkata/);
  assert.match(format, /Intl\.DateTimeFormat/);
});

test("scope-aware files never read a top-bar scope key or hand-roll a query string", () => {
  for (const [name, source] of Object.entries({ board, params })) {
    assert.ok(!/new URLSearchParams\(/.test(code(source)), `${name} must build links through scopeHref`);
    assert.ok(!/one\(sp, ?["']park["']\)/.test(code(source)), `${name} must read park through parseScope`);
  }
  assert.match(params, /parseScope/);
  assert.match(params, /scopeHref/);
});

test("dense tables pin their cells to one line, per the mock-anatomy rule", () => {
  assert.match(css, /table\.lt-operator-table th[\s\S]{0,400}white-space:nowrap/);
  assert.match(css, /table\.lt-shed-table th[\s\S]{0,400}white-space:nowrap/);
});

test("live tracker styles are scoped so they cannot restyle other boards", () => {
  const block = css.slice(css.indexOf("/vaccination/live-tracker — ported verbatim"));
  const unscoped = block
    .split("\n")
    .filter((line) => /^\.(feed|frow|legend|num|kpi|tag)\b/.test(line.trim()));
  assert.deepEqual(unscoped, [], "generic mock class names must stay under .lt-page");
});

test("counted labels do not read as broken singulars", () => {
  // "1 extra attempts" and "0 All combo animals →" both shipped in an early render against real stg
  // data. Small wrongness in a headline is expensive: it makes a reader distrust every other number.
  assert.match(sheds, /row\.extra_attempt_count > 1/, "the count only leads the label above one");
  const comboCode = code(combo);
  const truncatedBranch = comboCode.slice(comboCode.indexOf("rows_truncated ?"));
  const disabledBranch = truncatedBranch.slice(truncatedBranch.indexOf(") : ("));
  assert.ok(
    !/combo\.animal_count/.test(disabledBranch),
    "the untruncated button must not repeat the animal count the card header already shows",
  );
});
