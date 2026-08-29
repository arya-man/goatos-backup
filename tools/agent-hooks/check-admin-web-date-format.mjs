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

const DATE_FIELD = String.raw`[A-Za-z_$][\w$]*(?:\.[\w$]+)*\.(?:[\w$]*_date|[\w$]*_day|[\w$]*_at|feed_day)`;

// JSX text interpolations that ship a date-ish member access directly to the
// screen: `{row.first_purchase_date}`, `{row.last_weighed_date ?? (...)}`, or a
// template/range literal like `` `${coverage.start_date} to ${coverage.end_date}` ``.
const BARE_DATE_PATTERNS = [
  new RegExp(String.raw`>\{\s*(${DATE_FIELD})\s*\}\s*<`, "g"),
  new RegExp(String.raw`>\{\s*(${DATE_FIELD})\s*\?\?`, "g"),
  new RegExp(String.raw`>\{\s*` + "`" + String.raw`[^` + "`" + String.raw`]*\$\{\s*${DATE_FIELD}\s*\}[^` + "`" + String.raw`]*` + "`" + String.raw`\s*\}<`, "g"),
  new RegExp(String.raw`>[^<{}]*\{\s*(${DATE_FIELD})\s*\}[^<{}]*<`, "g"),
  new RegExp(String.raw`>\{\s*(${DATE_FIELD})\s*\}\s*[^<{}]+`, "g"),
];

function scanSource(source) {
  const hits = [];
  const seen = new Set();
  for (const pattern of BARE_DATE_PATTERNS) {
    pattern.lastIndex = 0;
    let match;
    while ((match = pattern.exec(source)) !== null) {
      const line = source.slice(0, match.index).split("\n").length;
      const text = match[0].trim();
      const key = `${line}:${text}`;
      if (seen.has(key)) continue;
      seen.add(key);
      hits.push({ line, text });
    }
  }
  return hits;
}

function canaryFailures(formatSource) {
  const failures = [];
  const fmtDateBody = formatSource.match(/export function fmtDate\([^]*?\n}/)?.[0] ?? "";
  const fmtDateTimeBody = formatSource.match(/export function fmtDateTime\([^]*?\n}/)?.[0] ?? "";
  if (!fmtDateBody.includes("${parts.day}-${parts.month}-${parts.year}")) {
    failures.push("lib/format.ts fmtDate no longer composes DD-MM-YYYY (maintainer decision 2026-08-21)");
  }
  if (!fmtDateTimeBody.includes("${parts.day}-${parts.month}-${parts.year} ${parts.hour}:${parts.minute}")) {
    failures.push("lib/format.ts fmtDateTime no longer composes DD-MM-YYYY HH:MM (maintainer decision 2026-08-21)");
  }
  return failures;
}

function selfTest() {
  const bad = [
    "<td>{row.first_purchase_date}</td>",
    "<td>{d.feed_day}</td>",
    "<td>{row.last_weighed_date ?? <span>never</span>}</td>",
    "<p>Recorded {trace.created_at}</p>",
    "<span>{`${coverage.start_date} to ${coverage.end_date}`}</span>",
  ].join("\n");
  const good = [
    "<td>{fmtDate(row.first_purchase_date)}</td>",
    "<td key={d.feed_day}>{fmtDate(d.feed_day)}</td>",
    "<td>{row.last_weighed_date ? fmtDate(row.last_weighed_date) : <span>never</span>}</td>",
    "<p>Recorded {dateTime(trace.created_at)}</p>",
    "<span>{coverage.start_date && coverage.end_date ? `${fmtDate(coverage.start_date)} to ${fmtDate(coverage.end_date)}` : '—'}</span>",
  ].join("\n");
  if (scanSource(bad).length !== 5) {
    console.error("self-test FAIL: bare date text nodes not flagged");
    process.exit(1);
  }
  if (scanSource(good).length !== 0) {
    console.error("self-test FAIL: wrapped/attribute dates wrongly flagged");
    process.exit(1);
  }
  const isoFmtDate = "export function fmtDate(iso) {\nreturn `${parts.year}-${parts.month}-${parts.day}`;\n}\nexport function fmtDateTime(iso) {\nreturn `${parts.day}-${parts.month}-${parts.year} ${parts.hour}:${parts.minute}`;\n}";
  const isoFmtDateTime = "export function fmtDate(iso) {\nreturn `${parts.day}-${parts.month}-${parts.year}`;\n}\nexport function fmtDateTime(iso) {\nreturn `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;\n}";
  const ddmmyyyy = "export function fmtDate(iso) {\nreturn `${parts.day}-${parts.month}-${parts.year}`;\n}\nexport function fmtDateTime(iso) {\nreturn `${parts.day}-${parts.month}-${parts.year} ${parts.hour}:${parts.minute}`;\n}";
  if (canaryFailures(isoFmtDate).length !== 1) {
    console.error("self-test FAIL: ISO-shaped fmtDate canary not caught");
    process.exit(1);
  }
  if (canaryFailures(isoFmtDateTime).length !== 1) {
    console.error("self-test FAIL: ISO-shaped fmtDateTime canary not caught");
    process.exit(1);
  }
  if (canaryFailures(ddmmyyyy).length !== 0) {
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
