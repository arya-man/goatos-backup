// Can this defect detector actually FIRE, and can it stay quiet?
//
// THE FINDING THIS EXISTS FOR. All seven surviving `covered` entries passed
// against a blank page. They reached `covered` through exactly one line —
// `smokeReason` returned null for a `check` interaction whenever the entry's
// kind was "pattern" — and nothing anywhere verified the detector could fire.
//
// The reason is structural. These are defect FINDERS: colliding chart labels,
// overpainting cells, leaked raw text. A page with NOTHING on it has no defects
// to find, so every finder is silent, and silence was read as "clean". That is
// the coverage ledger's own failure rebuilt one level over.
//
// A detector earns its entry's `covered` only with BOTH halves recorded:
//
//   it FIRES on a page carrying the defect      — otherwise its silence is worthless
//   it stays QUIET on a page that is correct    — otherwise it is a false positive
//
// Both already exist for some detectors, in the real-DOM tests, which pin each
// family "in BOTH directions: the shape that must stay quiet, and the genuine
// bug of the same family that must still report". So this DERIVES the answer
// from those tests rather than asking an entry to vouch for itself — an entry
// cannot talk itself into being covered, which is the same rule the rest of
// this file already follows.
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
export const DOM_TEST_FILE = path.resolve(here, "../../apps/admin-web/scripts/lib/regression-checks.dom.test.mjs");

/**
 * Read both directions for every detector named in a real-DOM test file.
 *
 * `patterns.includes("x")` is a FIRES case; `!patterns.includes("x")` is a
 * QUIET case. Order matters: the negated form is checked first, because the
 * positive pattern is a substring of it.
 *
 * @returns {Map<string, {fires: boolean, quiet: boolean}>}
 */
export function scanDiscrimination(source) {
  const seen = new Map();
  const note = (id, key) => {
    if (!seen.has(id)) seen.set(id, { fires: false, quiet: false });
    seen.get(id)[key] = true;
  };
  const re = /(!?)\s*patterns\.includes\(\s*["']([^"']+)["']\s*\)/g;
  for (let m = re.exec(source); m; m = re.exec(source)) {
    note(m[2], m[1] === "!" ? "quiet" : "fires");
  }
  return seen;
}

let cached = null;
export function discrimination(file = DOM_TEST_FILE) {
  if (cached && cached.file === file) return cached.map;
  let map;
  try {
    map = scanDiscrimination(readFileSync(file, "utf8"));
  } catch {
    // No evidence file means NO detector is proven. Failing open here would
    // restore the exact state this exists to prevent.
    map = new Map();
  }
  cached = { file, map };
  return map;
}

/**
 * May a `check` interaction on this detector make its pattern entry `covered`?
 *
 * @returns {{proven: boolean, why: string}} — `why` is empty when it may.
 */
export function detectorProven(ref, map = discrimination()) {
  const found = map.get(ref);
  if (found?.fires && found?.quiet) return { proven: true, why: "" };
  if (!found) {
    return { proven: false, why: "nothing has ever shown this check spotting the fault it is named after, so a page where it says nothing proves nothing" };
  }
  if (!found.fires) {
    return { proven: false, why: "this check has only ever been shown staying quiet, never spotting the fault it is named after, so its silence proves nothing" };
  }
  return { proven: false, why: "this check has only ever been shown spotting the fault, never staying quiet on a page that is fine, so it is not yet safe to trust when it fires" };
}
