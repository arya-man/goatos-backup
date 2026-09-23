// Where each lane's derived checks live inside lane-checks.json, in ONE place.
//
// This file exists because of a silent bug: lane 2 looked for its rows at the top level of
// lane-checks.json while the miner writes them at lanes.lane2.checks. All of its fallbacks
// missed, the loader took the miss for "there is no derived file", and 50 checks became one
// parked line reading "the derived check file could not be read" — which looks like a handled
// edge case and is not. The ledger went on counting 170 commits as routed through them.
//
// Two rules follow from that, and both are enforced here rather than left to each caller:
//   1. A lane asks this module where its rows are. Nobody re-guesses the shape locally, so a
//      future move is one edit here instead of four silent misses.
//   2. "I could not find the rows" is never the same answer as "there are no rows". When no
//      known shape matches, this module counts the check-shaped rows the file holds anyway and
//      hands back that number, so the caller can say how many it failed to load.

// Most specific first. The flat entries are older shapes, kept so an older file still loads.
export const LANE_SHAPES = {
  lane2: [
    { at: "lanes.lane2.checks", read: (d) => d?.lanes?.lane2?.checks },
    { at: "lane2.checks", read: (d) => d?.lane2?.checks },
    { at: "dataSanityChecks", read: (d) => d?.dataSanityChecks },
    { at: "lane2", read: (d) => d?.lane2 },
    { at: "checks", read: (d) => d?.checks }
  ],
  lane3: [
    { at: "lanes.lane3.checks", read: (d) => d?.lanes?.lane3?.checks },
    { at: "lane3.checks", read: (d) => d?.lane3?.checks },
    { at: "lane3", read: (d) => d?.lane3 },
    { at: "checks", read: (d) => d?.checks },
    { at: "endpoints", read: (d) => d?.endpoints }
  ],
  lane4: [
    { at: "lanes.lane4.checks", read: (d) => d?.lanes?.lane4?.checks },
    { at: "lane4.checks", read: (d) => d?.lane4?.checks },
    { at: "lane4", read: (d) => d?.lane4 }
  ],
  "lane5-android": [
    { at: "lanes.lane5-android.checks", read: (d) => d?.lanes?.["lane5-android"]?.checks },
    { at: "lane5-android.checks", read: (d) => d?.["lane5-android"]?.checks },
    { at: "lanes.lane5.checks", read: (d) => d?.lanes?.lane5?.checks },
    { at: "journeys", read: (d) => d?.journeys }
  ]
};

// How many check-shaped rows the file holds, wherever they sit. Only ever used to answer "how
// many did this lane fail to load", which is the number that makes a shape mismatch impossible
// to mistake for an empty file. A row counts if it carries an identity and something runnable.
export function countCheckShapedRows(value, depth = 0, seen = new Set()) {
  if (depth > 6 || value === null || typeof value !== "object") return 0;
  if (seen.has(value)) return 0;
  seen.add(value);
  if (Array.isArray(value)) {
    const runnable = value.filter((row) => row && typeof row === "object"
      && (typeof row.sql === "string" || typeof row.path === "string" || typeof row.endpoint === "string")
      && (typeof row.id === "string" || typeof row.name === "string"));
    if (runnable.length) return runnable.length;
    return value.reduce((sum, item) => sum + countCheckShapedRows(item, depth + 1, seen), 0);
  }
  return Object.values(value).reduce((sum, item) => sum + countCheckShapedRows(item, depth + 1, seen), 0);
}

/**
 * Finds one lane's rows in the miner's parsed file.
 *
 * Returns `{ rows, at, rowsNotLoaded: 0 }` when a known shape matched, and
 * `{ rows: null, at: null, rowsNotLoaded: n, shapesTried }` when none did — where `n` is how many
 * check-shaped rows the file holds regardless. `rows: null` with `rowsNotLoaded > 0` is a shape
 * mismatch: a broken lane, not an empty one, and the caller must say so out loud.
 */
export function locateLaneRows(parsed, lane) {
  const shapes = Array.isArray(lane) ? lane : (LANE_SHAPES[lane] ?? []);
  if (Array.isArray(parsed)) return { rows: parsed, at: "the whole file", rowsNotLoaded: 0 };
  for (const shape of shapes) {
    let candidate;
    try {
      candidate = shape.read(parsed);
    } catch {
      continue;
    }
    if (Array.isArray(candidate)) return { rows: candidate, at: shape.at, rowsNotLoaded: 0 };
  }
  return { rows: null, at: null, rowsNotLoaded: countCheckShapedRows(parsed), shapesTried: shapes.map((shape) => shape.at) };
}

/** Plain English for a mismatch, carrying the number that was not loaded. */
export function mismatchSentence(rowsNotLoaded) {
  const n = Number(rowsNotLoaded) || 0;
  return `the derived check file holds ${n} check${n === 1 ? "" : "s"} but none of them are where this lane looks for them, so 0 of ${n} were loaded`;
}
