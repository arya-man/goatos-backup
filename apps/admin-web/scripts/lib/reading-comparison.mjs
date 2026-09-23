// The shared primitive: ONE comparison of two readings, two verdict vocabularies.
//
// Agreed between the feature-assertion engine (builder A) and the interactive-surface ledger
// (builder B). Both sides ask the same question -- did these two readings of one target agree? --
// and differ only in the CONDITIONS under which the pair was taken, and therefore in what a
// disagreement means:
//
//   conditions: "same"              nothing was done between the readings. A disagreement means
//                                   the page varies here, so this is not something it OWES and
//                                   must not become an expectation. Refuse promotion.
//
//   conditions: "deliberate-action" something was done between them that must not change this
//                                   figure -- paging, filtering, resizing. A disagreement means
//                                   it MOVED WHEN IT SHOULD NOT. That is a finding about the
//                                   product, not about the check.
//
// Neither side owns the other's vocabulary, and neither re-implements the comparison. A's
// `expect.stable` branch in feature-assertions.mjs is the other caller; it is deliberately not
// edited from here while that file is being worked in.
//
// ---------------------------------------------------------------------------------------------
// WHAT A CONTRACT REVISION IS, stated ONCE so two receipts cannot define it differently.
//
// A reading is only comparable to another reading taken against the same compiled contract. The
// revision recorded for that purpose is the BACKEND BUILD SHA (`api_build_sha`), read from
// /version, which every sweep already obtains and already refuses to run without -- a missing
// sha, or the literal "unknown" or "dev", is a refusal, not a default.
//
// It is COARSER than the thing it stands for: one backend build, not a hash of the individual
// page contract. Two contracts that did not change between builds share a revision, and a build
// that changed nothing a screen renders still reads as a new revision. It is used anyway because
// it is REAL and already collected on every run, which makes receipts comparable across contract
// changes today rather than after someone builds per-contract hashing. When a per-contract hash
// exists, it replaces this in one place.

export const ABSENT_BUILD_SHAS = new Set(["", "unknown", "dev", "none", "null", "undefined"]);

/** A revision that is not a real build identity is refused here, never defaulted. */
export function contractRevisionRefusal(sha) {
  const value = String(sha ?? "").trim().toLowerCase();
  if (ABSENT_BUILD_SHAS.has(value)) {
    return (
      `refusing a contract revision of ${JSON.stringify(sha ?? null)}: readings are only comparable ` +
      `against a known build, and a placeholder makes two different builds look like one`
    );
  }
  return null;
}

/** sum / count reducers, so a list reading and a number reading compare through one path. */
function reduceReading(reading, all) {
  if (all === "count") return Array.isArray(reading) ? reading.length : reading === null || reading === undefined ? 0 : 1;
  if (all === "sum") return (Array.isArray(reading) ? reading : [reading]).reduce((n, v) => n + Number(v || 0), 0);
  return reading;
}

/**
 * Compare two readings of one target.
 *
 * @param {unknown} before
 * @param {unknown} after
 * @param {{conditions: "same"|"deliberate-action", label?: string, all?: "sum"|"count"}} options
 * @returns {{agreed: boolean, verdict: string, value?: unknown}}
 */
export function compareReadings(before, after, { conditions, label = "this reading", all } = {}) {
  if (conditions !== "same" && conditions !== "deliberate-action") {
    throw new Error(`compareReadings needs to know the conditions the pair was taken under, got ${JSON.stringify(conditions ?? null)}`);
  }
  // A reading that never arrived is not a reading. Both sides must refuse it rather than let two
  // absences agree with each other -- see the note on direction below.
  for (const [which, reading] of [["first", before], ["second", after]]) {
    if (reading === null || reading === undefined) {
      return { agreed: false, verdict: `the ${which} reading of ${label} never arrived, so there is nothing to compare` };
    }
  }
  const a = reduceReading(before, all);
  const b = reduceReading(after, all);
  if (JSON.stringify(a) === JSON.stringify(b)) return { agreed: true, verdict: "", value: a };
  return {
    agreed: false,
    verdict:
      conditions === "same"
        ? `${label} reads ${JSON.stringify(a)}, then ${JSON.stringify(b)} with nothing done in between — ` +
          `that is something the page varies, not something it owes`
        : `${label} reads ${JSON.stringify(a)}, then ${JSON.stringify(b)} after the page changed — ` +
          `it moved when it should not`,
  };
}
