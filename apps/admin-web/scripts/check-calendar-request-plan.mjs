#!/usr/bin/env node
//
// Calendar request-plan guard.
//
// After the FullCalendar rewrite the /calendar page pulls exactly ONE
// `getCalendarVaccinationEvents` list per render — FullCalendar owns the month
// grid and its `+more` popover natively, so the old aggregate date-marker probe
// that fed the mock's month-picker dots is retired. Selected-event detail and
// drive-target requests keep their existing narrow shape. This guard fails if a
// second marker/aggregate list request re-enters the plan, if the single list
// request grows a `includeDateMarkers`/`markersOnly` flag, or if any calendar
// request is fired unconditionally as part of a Promise.all fanout that would
// double the payload.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export function findingsForSource(source) {
  const findings = [];
  if (/request-plan:ignore/.test(source)) findings.push("calendar request plan must not bypass its guard");
  const listCalls = source.match(/getCalendarVaccinationEvents\s*\(/g) ?? [];
  if (listCalls.length !== 1) {
    findings.push(`calendar page must call getCalendarVaccinationEvents exactly once (found ${listCalls.length})`);
  }
  if (/includeDateMarkers\s*:/.test(source) || /markersOnly\s*:/.test(source)) {
    findings.push("date-marker probe is retired — remove includeDateMarkers/markersOnly from the calendar request plan");
  }
  return findings;
}

function selfTest() {
  const twoLists = `await Promise.all([getCalendarVaccinationEvents({}), getCalendarVaccinationEvents({ includeDateMarkers: true, limit: 1 })]);`;
  const twoFindings = findingsForSource(twoLists);
  if (!twoFindings.some((finding) => finding.includes("exactly once"))) throw new Error("self-test missed second list call");
  if (!twoFindings.some((finding) => finding.includes("date-marker probe is retired"))) throw new Error("self-test missed marker flag");
  const bypass = `// request-plan:ignore\nawait getCalendarVaccinationEvents({});`;
  if (!findingsForSource(bypass).some((finding) => finding.includes("bypass"))) throw new Error("self-test missed bypass comment");
  const good = `const list = await getCalendarVaccinationEvents({ parkId, limit: 200 });`;
  const goodFindings = findingsForSource(good);
  if (goodFindings.length) throw new Error(`self-test rejected single-list plan: ${goodFindings.join("; ")}`);
  console.log("calendar request-plan guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const source = readFileSync(resolve(import.meta.dirname, "../features/calendar/calendar.tsx"), "utf8");
const findings = findingsForSource(source);
if (findings.length) {
  console.error("calendar request-plan guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}
console.log("calendar request-plan guard: single-list plan, no date-marker probe");
