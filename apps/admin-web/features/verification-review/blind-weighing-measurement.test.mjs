// BLIND WEIGHING VERIFICATION (maintainer decision 2026-09-21). Weighing now requires the
// verifier's own positive scale reading; feed wastage still allows 0 as a real "empty trough"
// reading. Admin-web must therefore make the same distinction Android makes instead of merely
// checking that the input is nonblank.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");
const actionsSource = readFileSync(new URL("./actions.ts", import.meta.url), "utf8");

test("the drawer holds Accept for a non-positive weighing value but keeps wastage zero valid", () => {
  assert.match(drawerSource, /const FEED_WASTAGE_REF_TYPE = "feed_wastage_completion";/);
  assert.match(drawerSource, /function singleMeasurementValueUsable\(raw: string, refType: string \| null \| undefined\): boolean/);
  assert.match(
    drawerSource,
    /return refType === FEED_WASTAGE_REF_TYPE \? value >= 0 : value > 0;/,
    "only feed wastage may enable Accept for 0; weighing must be positive",
  );
  assert.match(
    drawerSource,
    /const measurementEntered = singleMeasurementValueUsable\(measurementValue, correction\?\.ref_type\);/,
    "the required-measurement gate must use the numeric predicate, not a nonblank check",
  );
  assert.doesNotMatch(
    drawerSource,
    /const measurementEntered = measurementValue\.trim\(\) !== "";/,
    "nonblank alone enabled Accept for weighing value 0",
  );
});

test("the verdict action refuses non-wastage zero before posting the approve", () => {
  assert.match(drawerSource, /name="measurement_ref_type" value=\{correction\.ref_type\}/);
  assert.match(actionsSource, /const FEED_WASTAGE_REF_TYPE = "feed_wastage_completion";/);
  assert.match(actionsSource, /const refType = String\(formData\.get\("measurement_ref_type"\) \?\? ""\)\.trim\(\);/);
  assert.match(
    actionsSource,
    /if \(refType !== FEED_WASTAGE_REF_TYPE && value <= 0\) return \{ ok: false, code: "invalid_measurement" \};/,
    "direct/stale form posts must not send a weighing value of 0 to the backend",
  );
});
