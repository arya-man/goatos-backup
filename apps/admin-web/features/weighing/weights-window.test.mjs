import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./weights.tsx", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

test("the period control is a calendar, not a fixed-window select", () => {
  assert.match(source, /kind: "daterange"/);
  // The two presets and the vocabulary behind them are GONE, not left unused: a stale option group
  // reads to the next author as a control that still exists somewhere.
  assert.doesNotMatch(source, /weighing_period/);
  assert.doesNotMatch(source, /periodDays/);
  assert.doesNotMatch(contract, /ID: "weighing_period"/);
  assert.doesNotMatch(contract, /"filter\.period\.4w"/);
  assert.doesNotMatch(contract, /"filter\.period\.12w"/);
});

test("the page lands on the 30 days before today, inclusive", () => {
  assert.match(source, /const DEFAULT_WINDOW_DAYS = 30;/);
  assert.match(source, /return \{ from: istDayPlus\(today, -\(DEFAULT_WINDOW_DAYS - 1\)\), to: today \};/);
  // istDayPlus is pure calendar arithmetic on an already-resolved IST day. Re-entering a timezone
  // here (or hardcoding +05:30) is what the shared helper exists to prevent.
  assert.match(source, /import \{ istDayPlus, todayIso \} from "@\/lib\/format";/);
  assert.doesNotMatch(source, /5\.5 \* 60/);
});

test("the default window is passed as NAMED fields, never spread", () => {
  // `{...defaultWindow(today)}` spreads `{from, to}` — the same two keys the SELECTED window uses —
  // and would silently overwrite the reader's choice, pinning the page to 30 days whatever they
  // picked. It typechecks and renders; only the data is wrong.
  assert.match(source, /defaultFrom: defaultWindow\(today\)\.from,\s*\n\s*defaultTo: defaultWindow\(today\)\.to,/);
  assert.doesNotMatch(source, /\.\.\.defaultWindow\(/);
});

test("a hand-edited window falls back instead of taking the page down", () => {
  // Inverted, malformed or absent parameters land on the default; a future end clamps to today,
  // because a weigh cannot have happened tomorrow.
  assert.match(source, /rawFrom <= rawTo/);
  assert.match(source, /rawFrom > today \? today : rawFrom/);
  assert.match(source, /return defaultWindow\(today\);/);
});

test("the headline row is five cards, and the gain figure is stated once", () => {
  // The sixth card printed the SAME number, denominator and sub-line as the "All parks — daily
  // gain" card in the row below it. Its copy keys are deleted too, so the duplicate cannot be
  // reinstated by pasting the markup back.
  assert.match(source, /className="grid g5 kpi-row"/);
  assert.doesNotMatch(source, /className="grid g6 kpi-row"/);
  assert.doesNotMatch(source, /"kpi\.gain\.label"/);
  assert.doesNotMatch(contract, /"kpi\.gain\.label":/);
  assert.doesNotMatch(contract, /"kpi\.gain\.sub":/);
  // g5 needs its own responsive ladder; without it the row falls back to one column.
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(css, /\.g5\{grid-template-columns:repeat\(5,minmax\(0,1fr\)\)\}/);
  assert.match(css, /@media\(max-width:1400px\)\{\.g5\{/);
  assert.match(css, /@media\(max-width:640px\)\{\.g5\{grid-template-columns:1fr\}\}/);
});

test("daily gain survives a park-scoped page", () => {
  // The gain row used to render ONLY when the page showed more than one park, because the deleted
  // headline card carried the figure in every other scope. Keeping that condition would have
  // removed the growth number entirely from a park-scoped page — the one thing this screen is for.
  assert.doesNotMatch(source, /perParkGain\.length > 0 \?/);
  // Its first card names the CURRENT scope, so "All parks" is only shown when it really is all.
  assert.match(source, /\{selectedParkName \|\| copy\(pageContract, "kpi\.park_gain\.all"\)\}/);
  // Never the raw park id: that would put an internal identifier in front of a CEO.
  assert.match(source, /parks\.find\(\(park\) => park\.park_id === parkFilter\)\?\.name \?\? ""/);
});

test("shed lists and gain chart only show sheds weighed in the selected window", () => {
  assert.match(source, /const weighedRows = rows\.filter\(\(row\) => row\.animals_weighed > 0\);/);
  assert.match(source, /modeFilter === "all" \? weighedRows : weighedRows\.filter/);
  assert.match(source, /const weighedRowKeys = new Set\(weighedRows\.map\(\(row\) => shedKey\(row\.location_id, row\.partition_label\)\)\);/);
  assert.match(source, /const visibleRowKeys = new Set\(visibleRows\.map\(\(row\) => shedKey\(row\.location_id, row\.partition_label\)\)\);/);
  assert.match(source, /shed\.adg_pair_count > 0 && visibleRowKeys\.has\(shedKey\(shed\.location_id, shed\.partition_label\)\)/);
});

test("weighed shed rows render breed and sex composition chips from the backend contract", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(source, /demo\?\.shed_composition \?\? \[\]/);
  assert.match(source, /shedLabelWithComposition/);
  assert.match(source, /replace\(" · ", " - "\)/);
  assert.doesNotMatch(source, /shed avg/);
  assert.match(source, /className="wcomp-chips"/);
  assert.match(source, /className="wcomp-chip"/);
  assert.match(source, /compositionLabel\(chip, pageContract\)/);
  assert.match(source, /copy\(pageContract, "composition\.unknown_breed"\)/);
  assert.match(source, /copy\(pageContract, "composition\.unknown_sex"\)/);
  assert.match(contract, /"composition\.unknown_breed":/);
  assert.match(contract, /"composition\.unknown_sex":/);
  assert.match(source, /composition\.source === "scanned_tags"/);
  assert.match(css, /\.wcomp-chips\{/);
  assert.match(css, /\.wcomp-chip\{/);
});

test("small shed charts do not reserve the tall empty panel height", () => {
  assert.match(source, /const shedChartSize = shedChartBars\.length <= 8 \? "short" : "tall";/);
  assert.match(source, /size=\{shedChartSize\}/);
});

test("full-width shed chart labels wrap instead of truncating composition", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(css, /\.wcols \.wbar \.wbl\{white-space:normal;/);
  assert.match(css, /\.wcols \.wbar \.wbl\{[^}]*text-overflow:clip/);
  assert.match(css, /\.wcols \.wbar \.wbl\{[^}]*overflow-wrap:anywhere/);
  assert.doesNotMatch(css, /\.wcols \.wbar \.wbl\{[^}]*text-overflow:ellipsis/);
  assert.match(css, /\.wbar\{[^}]*min-height:24px/);
});

test("the two table cards are inset without losing their full-bleed tables", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(source, /className="card wtable" aria-label=\{copy\(pageContract, "section\.sheds\.aria"\)\}/);
  assert.match(source, /className="card wtable" aria-label=\{copy\(pageContract, "section\.losing\.aria"\)\}/);
  // Vertical padding on the CARD, horizontal on its children — never on the card itself, which
  // would inset the table away from its own header rule and row separators.
  assert.match(css, /\.wtable\{padding:14px 0\}/);
  assert.match(css, /\.wtable > :not\(\.tablewrap\)\{padding-left:16px;padding-right:16px\}/);
  // The outer columns match the card's own 17px text edge; a cell padding, so the rules still reach
  // the frame.
  assert.match(css, /\.wtable table\.tbl th:first-child,\s*\n\.wtable table\.tbl td:first-child\{padding-left:16px\}/);
});

test("every visible string on the weighing calendar is backend-contract copy", () => {
  for (const key of [
    "filter.period.label",
    "filter.period.today",
    "filter.period.single",
    "filter.period.range",
    "filter.period.aria",
    "filter.period.previous_month",
    "filter.period.next_month",
    "filter.period.range_start_hint",
    "filter.period.range_end_hint",
    "filter.period.range_separator",
  ]) {
    const escaped = key.replace(/\./g, "\\.");
    assert.match(source, new RegExp(`copy\\(pageContract, "${escaped}"\\)`), `page: ${key}`);
    assert.match(contract, new RegExp(`"${escaped}":`), `contract: ${key}`);
  }
});
