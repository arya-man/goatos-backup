// Source-shape contract tests for the Time-wise tab's two controls (maintainer request
// 2026-09-21: "under time wise ... I should be able to select shed ... either weekly or monthly").
// They pin the three rules that keep the tab honest: the bucket reaches BOTH reads so every
// section sits on one set of columns, the pen picker narrows the pen grid and nothing else, and
// every heading has a matching 30-day wording so a title can never describe columns the table is
// not showing.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pageSource = readFileSync(join(here, "weights-analytics.tsx"), "utf8");

test("the bucket rides both Time-wise reads, so the tab cannot mix week and 30-day columns", () => {
  // BOTH reads, and nothing else: the growth read feeds the overall chart, the demographics read
  // feeds the breed rows and the two grids, and a bucket reaching one but not the other would put
  // week columns beside 30-day columns on the same screen.
  const timeScopedReads = pageSource.match(/\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\)/g) ?? [];
  assert.equal(timeScopedReads.length, 2, "the bucket and pen ride exactly the two Time-wise reads");
  assert.match(pageSource, /getWeighingGrowth\(\{[\s\S]{0,240}?bucket: gainBucket, \.\.\.penScope/, "the growth read takes them");
  assert.match(pageSource, /getWeightDemographics\(\{[\s\S]{0,240}?bucket: gainBucket, \.\.\.penScope/, "so does the demographics read");
  // Only on that tab: General and Shed-wise read the same growth response and have no bucket
  // control, so sending one there would change a chart nobody asked about.
  assert.doesNotMatch(pageSource, /bucket: gainBucket \}(?!\s*:)/, "the bucket must never be sent unconditionally");
});

test("an unrecognised bucket falls back to the week rather than taking the tab down", () => {
  assert.match(
    pageSource,
    /one\(params, GAIN_BUCKET_PARAM\) === "month" \? "month" : "week"/,
    "a hand-typed URL must resolve to the default, not to a value the backend refuses",
  );
});

test("the pen is a SCOPE on both reads, not a filter applied to their answers", () => {
  // Maintainer correction 2026-09-21: selecting a pen must show that pen's growth, its breeds and
  // the load it sits in -- and three of those four sections are server-side aggregates that carry
  // no pen once they are grouped, so the narrowing has to reach the query.
  assert.match(pageSource, /bucket: gainBucket, \.\.\.penScope/, "the pen rides both Time-wise reads");
  assert.match(pageSource, /function penScopeFrom\(penKey: string\)/, "the picker's key splits into the two query parameters");
  assert.match(pageSource, /pen_location_id: penKey\.slice\(0, separator\)/, "a pen is (location, partition), never a name");
  assert.doesNotMatch(pageSource, /shownPenPoints/, "no client-side pen filter beside the server scope: two narrowings would disagree");
});

test("the pen picker's vocabulary comes from the read the pen scope does not narrow", () => {
  assert.match(pageSource, /pens=\{rows\.map\(/, "options come from the shed read every tab makes");
  assert.match(pageSource, /key: `\$\{row\.location_id\}::\$\{row\.partition_label \?\? ""\}`/, "keyed on identity, because a pen name repeats across parks");
  assert.match(pageSource, /seenPens\.has\(penKey\) \? penKey : ""/, "a selection the vocabulary lacks shows as unselected");
});

test("every heading on the tab has a week wording and a 30-day wording, both backend-owned", () => {
  assert.match(
    pageSource,
    /copy\(pageContract, gainBucket === "month" \? `\$\{key\}\.month` : key\)/,
    "the page picks one authored string or the other; it never edits either",
  );
  for (const key of [
    "section.time.title",
    "section.time.caption",
    "section.time.breed.title",
    "section.time.pen.title",
    "section.time.load.title",
    "note.time.gaps",
  ]) {
    assert.ok(pageSource.includes(`bucketCopy("${key}")`), `${key} must be read through the bucket-aware pair`);
  }
  // The two controls' own labels are backend copy too.
  for (const key of ["filter.gain_bucket.label", "filter.gain_bucket.note", "filter.time_pen.label", "filter.time_pen.note"]) {
    assert.ok(pageSource.includes(`"${key}"`), `page must read backend copy key ${key}`);
  }
  assert.match(pageSource, /optionGroup\(pageContract, "gain_bucket"\)/, "Weekly/Monthly are a backend option group, never literals");
});
