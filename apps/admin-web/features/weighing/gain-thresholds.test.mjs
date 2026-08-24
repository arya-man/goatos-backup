import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./weights.tsx", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);
const openapi = readFileSync(
  new URL("../../../../contracts/openapi/app-api.yaml", import.meta.url),
  "utf8",
);

/** Source with block and line comments removed, for scans that must not read documentation. */
const stripComments = (text) => text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");

test("the marks are rendered as three independent columns, never summed or stacked", () => {
  // The three counts OVERLAP (a kid at 260 g/day is in all three), so any arithmetic
  // ACROSS them — a total, a stack, or a subtraction to fake a band — states a
  // distribution nobody measured. Each cell prints its own backend count.
  assert.match(source, /const gainThresholdRows: GainThresholdRow\[\] =/);
  assert.doesNotMatch(source, /above_250_g_per_day\s*\+\s*above_200_g_per_day/);
  assert.doesNotMatch(source, /above_180_g_per_day\s*-\s*above_200_g_per_day/);
  assert.doesNotMatch(source, /above_200_g_per_day\s*-\s*above_250_g_per_day/);
});

test("the share is taken against the row's own backend denominator", () => {
  // `row.animals` is kids of THIS breed weighed twice — the exact key set the backend
  // filtered. Dividing by the page's animal total (which counts kids weighed once)
  // would understate every breed while still rendering a plausible percentage.
  assert.match(source, /pct: \(count \/ row\.animals\) \* 100/);
  assert.doesNotMatch(source, /\/ summary\.animals_weighed/);
});

test("the card opens on the chart and the toggle only changes the view", () => {
  // Chart is the default because the card exists to answer "is this breed growing", and
  // an ABSENT param means chart — so a link shared from the default view keeps meaning
  // chart rather than freezing whichever view it was copied in.
  assert.match(source, /one\(params, GAIN_VIEW_PARAM\) === "table" \? "table" : "chart"/);
  assert.match(source, /GAIN_VIEW_PARAM\]: null/);
  // Both views render the SAME derived rows. A second source for one fact is how two
  // views of one card start disagreeing.
  assert.equal(source.match(/gainThresholdRows/g).length >= 4, true);
  assert.doesNotMatch(source, /gain_thresholds_by_breed[\s\S]{0,400}gain_thresholds_by_breed/);
});

test("every bar states its share, its count, and the animals behind it on hover", () => {
  const bars = readFileSync(new URL("./gain-threshold-bars.tsx", import.meta.url), "utf8");
  // Share then count in the row's own cell — the percentage is what the bar encodes, the
  // count is the evidence under it, so a share standing on two kids can never read as a
  // share standing on a hundred.
  assert.match(bars, /<span className="gmv">\{mark\.pct\.toFixed\(1\)\}%<\/span>/);
  assert.match(bars, /<span className="gmn">\(\{mark\.count\.toLocaleString\("en-IN"\)\}\)<\/span>/);
  // Head count under the breed name.
  assert.match(bars, /className="gml-sub"[\s\S]{0,120}row\.animals\.toLocaleString/);
  // Count on hover, against the breed's own denominator — never a page-wide total.
  assert.match(bars, /title=\{`\$\{mark\.count\.toLocaleString\("en-IN"\)\} \$\{ofLabel\} \$\{row\.animals/);
  // The marks are cumulative, so the bars must never be summed into one stacked track.
  assert.doesNotMatch(bars, /reduce\(/);
  assert.doesNotMatch(bars, /cumulativeWidth|stackOffset/);
});

test("the chart renders no copy of its own", () => {
  // Comments are stripped first: a doc comment may NAME a contract label as an example
  // ("e.g. \"Above 250 g/day\"") without that label being shipped copy. Scanning the raw
  // file would fail on documentation and teach the next author to delete the docs.
  const bars = stripComments(readFileSync(new URL("./gain-threshold-bars.tsx", import.meta.url), "utf8"));
  // Every string arrives resolved from the page contract; a literal here would be a label
  // the backend cannot govern.
  assert.doesNotMatch(bars, /"Above \d+ g\/day"/);
  assert.doesNotMatch(bars, /"kids"/);
  // Colours are theme tokens, so both themes stay correct by construction.
  assert.doesNotMatch(bars, /#[0-9a-fA-F]{3,8}\b/);
});

test("every visible string on the table is backend-contract copy", () => {
  for (const key of [
    "section.gain_thresholds.title",
    "view.chart",
    "view.table",
    "section.gain_thresholds.aria",
    "section.gain_thresholds.caption",
    "empty.gain_thresholds.body",
  ]) {
    assert.match(source, new RegExp(`copy\\(pageContract, "${key.replace(/\./g, "\\.")}"\\)`));
    assert.ok(contract.includes(`"${key}":`), `contract is missing copy key ${key}`);
  }
  // Column headers come from the table contract, not literals in the page.
  assert.match(source, /tableLabels\(pageContract, "gain-thresholds"\)/);
  assert.match(contract, /tableP\("gain-thresholds"/);
  assert.doesNotMatch(source, /Above 250 g\/day/);
});

test("the table sits directly above the daily gain by load chart", () => {
  const table = source.indexOf('aria-label={copy(pageContract, "section.gain_thresholds.aria")}');
  const loadChart = source.indexOf('copy(pageContract, "chart.load.title")');
  assert.ok(table > 0 && loadChart > 0);
  assert.ok(table < loadChart, "the gain-mark table must render before the load chart");
});

test("the contract states the marks overlap, in both the caption and the schema", () => {
  // A reader who adds the columns gets a number larger than the herd. The caption is the
  // only thing standing between them and that mistake, so it is pinned.
  assert.match(contract, /counted under all three marks, so the columns overlap and do not add up/);
  assert.match(openapi, /must never be summed or stacked/);
});
