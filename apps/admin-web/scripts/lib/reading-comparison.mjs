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
// WHY AGREEMENT IS NOT ENOUGH, which is the reason every rule below exists:
//
//     CONSISTENCY IS NOT CORRECTNESS WHEN THE TWO THINGS BEING COMPARED ARE NOT THE SAME THING.
//
// Two empty readings agree. Two readings of a sign-in page a stale token redirected to agree.
// Two readings taken either side of a source edit agree. Two runs that both typed the same
// principal label agree. Every one of those would have been promoted into an expectation, and
// every one describes something other than what it claims to. So a pair must be shown to be a
// pair -- same source, same contract, same grants, something actually read -- before agreement
// between its halves means anything at all.
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

/**
 * A reading that holds NOTHING is an absence of evidence, not evidence.
 *
 * Found by builder A after adopting this primitive, one level below where both callers had
 * already fixed the same shape privately: B refused absent readings in stableReading, A
 * normalised empty to absent before calling, and the thing underneath them both still agreed
 * that nothing equals nothing. Measured in A's lane: with the page drawing nothing at both
 * readings, `sum` reported and a single cell reported, but `count` PASSED.
 *
 * So the refusal lives HERE and the callers stop compensating -- otherwise every future caller
 * has to remember, which is exactly what gets forgotten.
 *
 * ZERO IS NOT NOTHING. A total that reads 0, a count of 0 rows, `false` -- those are real
 * readings and must keep comparing. Only a container that holds nothing, and null/undefined, are
 * absences.
 */
export function isAbsentReading(value) {
  if (value === null || value === undefined) return true;
  if (typeof value === "string") return value.trim() === "";
  if (Array.isArray(value)) return value.length === 0;
  if (typeof value === "object") return Object.keys(value).length === 0;
  return false;
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
  // Checked on the RAW readings, before any reducer: `count` over an empty list is 0, and two
  // zeroes agree, which is how an empty page passed.
  for (const [which, reading] of [["first", before], ["second", after]]) {
    if (!isAbsentReading(reading)) continue;
    const nothingThere = reading !== null && reading !== undefined;
    return {
      agreed: false,
      verdict: nothingThere
        ? `the ${which} reading of ${label} found nothing on the screen; two absences agree with each ` +
          `other, and agreeing is how a value is trusted, so nothing is never an answer here`
        : `the ${which} reading of ${label} never arrived, so there is nothing to compare`,
    };
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
