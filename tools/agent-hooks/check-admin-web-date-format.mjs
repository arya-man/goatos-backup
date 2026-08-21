#!/usr/bin/env node

// admin-web-date-format-guard — DATE DISPLAY RULE (maintainer decision 2026-08-21).
//
// Every VISIBLE date in an admin-web table/card/drawer renders DD-MM-YYYY
// through lib/format.ts `fmtDate` (timestamps through `fmtDateTime`/`dateTime`);
// chart axes render the compact dd-mm-yy via components/svg-series.tsx. Wire
// formats (query params, API payloads, React keys) stay ISO YYYY-MM-DD.
//
// Two checks:
//   1. CANARY — lib/format.ts must keep composing fmtDate/fmtDateTime as
//      day-month-year. A refactor that silently flips the helper back to ISO
//      fails here even though no feature file changed.
//   2. BARE-DATE SCAN — a JSX TEXT node that renders a date-named field
//      directly (`<td>{row.first_purchase_date}</td>`, `{d.feed_day}`) is
//      flagged: it ships the wire's ISO string to the operator's eyes. Wrap it
//      in fmtDate(...) / fmtDateTime(...), or keep it out of text position.
//
// BLIND SPOTS (stated per the guard-honesty rule; review owns these):
//   - a date laundered through an intermediate variable (`const d = row.x_date`)
//     or template literal before rendering;
//   - fields whose names do not end in _date/_day/_at (e.g. `captured`, `when`);
//   - non-JSX composition (`.join(...)` pipelines) and chart tooltip strings;
//   - Android/mobile surfaces — this guard is admin-web only.

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// A JSX text interpolation whose entire expression is a member access ending in
// a date-ish suffix: `>{row.first_purchase_date}<`, `>{d.feed_day}</`.
const BARE_DATE_TEXT = />\{\s*[A-Za-z_$][\w$]*(?:\.[\w$]+)*\.(?:[\w$]*_date|[\w$]*_day|feed_day)\s*\}\s*</g;

function scanSource(source) {
  const hits = [];
  let match;
  BARE_DATE_TEXT.lastIndex = 0;
  while ((match = BARE_DATE_TEXT.exec(source)) !== null) {
    const line = source.slice(0, match.index).split("\n").length;
    hits.push({ line, text: match[0].trim() });
  }
  return hits;
}

function canaryFailures(formatSource) {
  const failures = [];
  if (!formatSource.includes("${parts.day}-${parts.month}-${parts.year}")) {
    failures.push("lib/format.ts fmtDate no longer composes DD-MM-YYYY (maintainer decision 2026-08-21)");
  }
  return failures;
}

function selfTest() {
  const bad = "<td>{row.first_purchase_date}</td>\n<td>{d.feed_day}</td>";
  const good = "<td>{fmtDate(row.first_purchase_date)}</td>\n<td key={d.feed_day}>{fmtDate(d.feed_day)}</td>";
  if (scanSource(bad).length !== 2) {
    console.error("self-test FAIL: bare date text nodes not flagged");
    process.exit(1);
  }
  if (scanSource(good).length !== 0) {
    console.error("self-test FAIL: wrapped/attribute dates wrongly flagged");
    process.exit(1);
  }
  if (canaryFailures("return `${parts.year}-${parts.month}-${parts.day}`;").length !== 1) {
    console.error("self-test FAIL: ISO-shaped fmtDate canary not caught");
    process.exit(1);
  }
  if (canaryFailures("return `${parts.day}-${parts.month}-${parts.year}`;").length !== 0) {
    console.error("self-test FAIL: DD-MM-YYYY canary wrongly flagged");
    process.exit(1);
  }
  console.log("admin-web-date-format-guard self-test: PASS");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }
  const failures = [];

  const formatPath = resolve(repo, "apps/admin-web/lib/format.ts");
  failures.push(...canaryFailures(readFileSync(formatPath, "utf8")));

  const files = execFileSync(
    "git",
    ["ls-files", "apps/admin-web/features", "apps/admin-web/components", "apps/admin-web/app"],
    { cwd: repo, encoding: "utf8" },
  )
    .split("\n")
    .filter((file) => file.endsWith(".tsx") && !file.includes(".test."));
  for (const file of files) {
    const hits = scanSource(readFileSync(resolve(repo, file), "utf8"));
    for (const hit of hits) {
      failures.push(`${file}:${hit.line}: bare ISO date in JSX text — wrap in fmtDate()/fmtDateTime(): ${hit.text}`);
    }
  }

  if (failures.length > 0) {
    console.error("admin-web-date-format-guard: FAIL");
    for (const failure of failures) console.error(`  - ${failure}`);
    process.exit(1);
  }
  console.log("admin-web-date-format-guard: PASS");
}

main();
