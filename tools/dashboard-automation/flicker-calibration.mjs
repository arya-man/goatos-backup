// Which pages may REPORT flicker, and which are only filmed.
//
// The flicker judgement was calibrated on four screens, chosen because they
// carry a pinned, blurred element. Widening the filming to the whole route
// table pointed that judgement at ~135 pages it was never calibrated against —
// and this repo MANDATES a continuously rotating refresh icon on every read
// screen. That is a correct, required animation on nearly every page now being
// filmed for unwanted motion.
//
// §2 ranks no-false-positives above finding things, so until calibration
// exists the honest state of those pages is NOT CHECKED. Not clean, and not
// flickering. Same discipline as a blind sweep returning `not-checked` rather
// than `pass`.
//
// MEMBERSHIP IS NOT AN EDIT. A route is eligible only with a positive control
// recorded beside it — evidence that the detector actually fired on a real
// flicker there. Adding a bare route name changes nothing and the loader says
// why, so the list cannot be widened by someone who simply wants a green page.
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
export const CALIBRATION_FILE = path.join(here, "flicker-calibration.json");

/** @returns {{calibrated: Map<string,object>, ignored: Array<{route,why}>}} */
/** The judging half, split out so a test can hand it an object instead of a file. */
export function loadCalibrationObject(parsed) {
  const calibrated = new Map();
  const ignored = [];
  for (const entry of parsed?.routes ?? []) {
    const control = entry?.positiveControl;
    if (!entry?.route) continue;
    if (!control || !control.what || !control.result) {
      ignored.push({
        route: entry.route,
        why: "this page is listed as calibrated but records no evidence that the check ever fired on a real flicker there, so listing it changes nothing",
      });
      continue;
    }
    calibrated.set(entry.route, entry);
  }
  return { calibrated, ignored };
}

export function loadCalibration(file = CALIBRATION_FILE) {
  let parsed;
  try {
    parsed = JSON.parse(readFileSync(file, "utf8"));
  } catch (error) {
    // A calibration file that cannot be read means NOTHING is calibrated. It
    // must never mean everything is: failing open here would silently restore
    // the exact state this exists to prevent.
    return { calibrated: new Map(), ignored: [], error: String(error?.message ?? error) };
  }
  return loadCalibrationObject(parsed);
}

/**
 * May this page report a flicker finding?
 *
 * @returns {{report: boolean, why: string}} — `why` is empty when it may.
 */
export function flickerReportable(routeName, calibration = loadCalibration()) {
  if (calibration.calibrated.has(routeName)) return { report: true, why: "" };
  return {
    report: false,
    why: "the way this check tells flicker from ordinary movement has not been checked against this page, and every page here shows a spinning refresh mark on purpose, so anything found on it would not be trusted yet",
  };
}

/**
 * Split a sweep's findings into the ones a person may be shown and the ones
 * that are only "not checked yet".
 *
 * Nothing is thrown away: an uncalibrated page's findings are kept, labelled,
 * and reported as not-calibrated so the supervised run that calibrates these
 * pages has somewhere to start.
 */
export function partitionFindings(rows, calibration = loadCalibration()) {
  const reportable = [];
  const notCalibrated = [];
  for (const row of rows) {
    if (!(row.findings ?? []).length) continue;
    const verdict = flickerReportable(row.route, calibration);
    (verdict.report ? reportable : notCalibrated).push({
      route: row.route,
      viewport: row.viewport,
      findings: row.findings,
      ...(verdict.report ? {} : { why: verdict.why }),
    });
  }
  return { reportable, notCalibrated };
}

/** Pages that were filmed but whose verdict cannot be trusted yet. */
export function uncalibratedPages(rows, calibration = loadCalibration()) {
  return rows
    .filter((row) => !row.parked && !row.error && !flickerReportable(row.route, calibration).report)
    .map((row) => ({ route: row.route, viewport: row.viewport, why: flickerReportable(row.route, calibration).why }));
}
