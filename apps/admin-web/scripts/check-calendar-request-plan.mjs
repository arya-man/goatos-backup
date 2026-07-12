#!/usr/bin/env node

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export function findingsForSource(source) {
  const findings = [];
  if (/request-plan:ignore/.test(source)) findings.push("calendar request plan must not bypass its guard");
  if (!/const\s+markerRequest\s*=\s*datePickerOpen\s*\?\s*getCalendarVaccinationEvents\s*\(/s.test(source)) {
    findings.push("date-marker request must be conditional on datePickerOpen");
  }
  if (!/:\s*Promise\.resolve\(null\)/s.test(source)) {
    findings.push("closed picker must resolve without a marker API request");
  }
  if (!/includeDateMarkers:\s*true/.test(source)) findings.push("marker request must use aggregate date markers");
  if (!/limit:\s*1/.test(source)) findings.push("marker request item payload must remain bounded to one row");
  return findings;
}

function selfTest() {
  const bad = `const [list, markers] = await Promise.all([getCalendarVaccinationEvents({}), getCalendarVaccinationEvents({ includeDateMarkers: true, limit: 1 })]);`;
  if (findingsForSource(bad).length < 2) throw new Error("self-test missed unconditional marker fetch");
  const bypass = `// request-plan:ignore\nconst markerRequest = datePickerOpen ? getCalendarVaccinationEvents({ includeDateMarkers: true, limit: 1 }) : Promise.resolve(null);`;
  if (!findingsForSource(bypass).some((finding) => finding.includes("bypass"))) throw new Error("self-test missed bypass comment");
  const good = `const markerRequest = datePickerOpen ? getCalendarVaccinationEvents({ includeDateMarkers: true, limit: 1 }) : Promise.resolve(null);`;
  if (findingsForSource(good).length) throw new Error(`self-test rejected bounded conditional plan: ${findingsForSource(good).join("; ")}`);
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
console.log("calendar request-plan guard: marker fetch runs only while picker is open");
