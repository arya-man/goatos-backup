import test from "node:test";
import assert from "node:assert/strict";
import { detectorProven, discrimination, discriminationCensus, scanDiscrimination } from "./pattern-discrimination.mjs";
import { smokeReason } from "./check-coverage-since-aug1.mjs";

test("both directions are read out of the real DOM tests", () => {
  const map = scanDiscrimination(`
    assert.ok(!patterns.includes("text-overlap"), "expected quiet");
    assert.ok(patterns.includes("text-overlap"));
    assert.ok(patterns.includes("only-fires"));
    assert.ok(!patterns.includes("only-quiet"), "expected quiet");
  `);
  assert.deepEqual(map.get("text-overlap"), { fires: true, quiet: true });
  assert.deepEqual(map.get("only-fires"), { fires: true, quiet: false });
  assert.deepEqual(map.get("only-quiet"), { fires: false, quiet: true });
});

test("a detector is proven only when it can both fire and stay quiet", () => {
  const map = scanDiscrimination(`
    assert.ok(patterns.includes("both")); assert.ok(!patterns.includes("both"));
    assert.ok(patterns.includes("fires-only"));
    assert.ok(!patterns.includes("quiet-only"));
  `);
  assert.equal(detectorProven("both", map).proven, true);
  const fires = detectorProven("fires-only", map);
  assert.equal(fires.proven, false);
  assert.match(fires.why, /never staying quiet on a page that is fine/);
  const quiet = detectorProven("quiet-only", map);
  assert.equal(quiet.proven, false);
  assert.match(quiet.why, /only ever been shown staying quiet/);
  const missing = detectorProven("never-heard-of", map);
  assert.equal(missing.proven, false);
  assert.match(missing.why, /a page where it says nothing proves nothing/);
});

test("being a pattern entry is no longer enough on its own", () => {
  // THE DEFECT: this single line made an entry `covered`, and nothing verified
  // the detector could fire. All seven survivors passed against a blank page.
  const map = scanDiscrimination(`assert.ok(patterns.includes("real")); assert.ok(!patterns.includes("real"));`);
  const ctx = { discrimination: map };
  assert.equal(smokeReason({ kind: "pattern" }, { type: "check", ref: "real" }, ctx), null,
    "a detector shown both firing and staying quiet still counts");
  assert.ok(smokeReason({ kind: "pattern" }, { type: "check", ref: "unproven" }, ctx),
    "a detector nobody has seen fire does not, however much the entry claims");
  assert.ok(smokeReason({ kind: "feature" }, { type: "check", ref: "real" }, ctx),
    "and a feature entry citing the background sweep is unchanged");
});

test("no evidence file means no detector is proven, never every detector", () => {
  const map = discrimination("/nonexistent/regression-checks.dom.test.mjs");
  assert.equal(map.size, 0, "failing open would restore exactly what this prevents");
  assert.equal(detectorProven("A-chart-empty-frame", map).proven, false);
});

test("the detectors that survive are the ones with both halves recorded", () => {
  const map = discrimination();
  assert.ok(map.size > 0, "the real DOM tests carry evidence");
  const proven = [...map.entries()].filter(([, v]) => v.fires && v.quiet).map(([k]) => k);
  // Measured on 2026-09-23: 5 of the 12 detectors the pattern entries cite.
  assert.ok(proven.includes("A-chart-empty-frame"));
  assert.ok(proven.includes("J-raw-text"));
  assert.ok(!proven.includes("B-container-overflow"), "never shown firing");
  assert.ok(!proven.includes("D-page-overflow"), "never shown firing");
});

test("the detector census says what it read, not just what it found", () => {
  // B found this class in its own scanner: a file listed but never read
  // contributed zero silently. An empty map that means "no evidence file" and
  // an empty map that means "a file with no detectors in it" were the same
  // value, and the headline counted from it either way.
  const real = discriminationCensus().census;
  assert.ok(real.characters > 1000, `the evidence file must actually be read, got ${real.characters} characters`);
  assert.ok(real.detectors > 0, "and name detectors");
  assert.equal(real.error, "");

  const absent = discriminationCensus("/nonexistent/evidence.mjs").census;
  assert.equal(absent.characters, 0);
  assert.equal(absent.detectors, 0);
  assert.match(absent.error, /could not be read/, "a silent zero and a real zero must not look the same");
});

test("an evidence file that is read but names nothing is reported, not counted", () => {
  const { census } = discriminationCensus(new URL("./flicker-calibration.json", import.meta.url).pathname);
  assert.ok(census.characters > 0, "it was read");
  assert.equal(census.detectors, 0);
  assert.match(census.error, /names no detector at all/);
});
