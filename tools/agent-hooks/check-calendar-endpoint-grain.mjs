#!/usr/bin/env node

// check-calendar-endpoint-grain.mjs -- blocks wiring narrow vaccination schedule/list
// surfaces to the broad Calendar events endpoint. Calendar may use
// /calendar/vaccination/events; Full Schedule, vaccination schedule contracts, mobile
// schedule APIs, and DB read models must use a grain-matched endpoint/read model.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const BROAD_ENDPOINT_RE = /(?:^|["'`\s(])\/?calendar\/vaccination\/events|getCalendarVaccinationEvents|calendarVaccinationEvents/i;
const NARROW_SURFACE_RE =
  /Full\s*Schedule|full[_-]?schedule|VaccinationFullSchedule|vaccination[\s_-]+schedule|getVaccinationSchedule|\/vaccination\/schedule|calendar_schedule_items|CalendarScheduleRemoteKey/i;
const ALLOW_RE = /calendar-endpoint-grain:ignore/;

const scanRoots = [
  "apps/admin-web",
  "apps/goatos-android",
  "contracts",
  "backend/migrations",
  "backend/db",
];

const scanExts = new Set([".ts", ".tsx", ".mjs", ".kt", ".yaml", ".yml", ".json", ".sql"]);

function extname(rel) {
  const dot = rel.lastIndexOf(".");
  return dot < 0 ? "" : rel.slice(dot);
}

function keep(rel) {
  if (!scanRoots.some((root) => rel.startsWith(`${root}/`))) return false;
  if (!scanExts.has(extname(rel))) return false;
  if (/\/(node_modules|\.next|build|\.turbo)\//.test(rel)) return false;
  if (/\.(test|spec|mock|stories)\.(ts|tsx|mjs|kt)$/.test(rel)) return false;
  if (/\/src\/test\//.test(rel)) return false;
  return true;
}

function lineOf(source, index) {
  return source.slice(0, index).split("\n").length;
}

function ignored(source, index) {
  const lines = source.split("\n");
  const lineIdx = lineOf(source, index) - 1;
  return ALLOW_RE.test(lines[lineIdx] || "") || ALLOW_RE.test(lines[lineIdx - 1] || "");
}

function openApiSection(source, path) {
  const start = source.indexOf(`  ${path}:`);
  if (start < 0) return null;
  const rest = source.slice(start + 1);
  const next = rest.search(/\n  \/[A-Za-z0-9_/{.-]+:/);
  return {
    start,
    text: source.slice(start, next < 0 ? source.length : start + 1 + next),
  };
}

export function findingsForSource(source, rel = "fixture") {
  const findings = [];

  if (/contracts\/.*openapi.*\.ya?ml$/.test(rel) || rel.endsWith("app-api.yaml")) {
    const section = openApiSection(source, "/vaccination/schedule");
    if (section && BROAD_ENDPOINT_RE.test(section.text) && !ignored(source, section.start)) {
      findings.push({
        line: lineOf(source, section.start),
        rule: "schedule-contract-aliases-calendar-events",
        message:
          "the /vaccination/schedule contract must not alias the broad Calendar events response/path; keep the schedule contract grain-owned",
      });
    }
    return findings;
  }

  const broad = BROAD_ENDPOINT_RE.exec(source);
  if (!broad || ignored(source, broad.index)) return findings;

  const localWindow = source.slice(Math.max(0, broad.index - 1200), broad.index + 1200);
  if (NARROW_SURFACE_RE.test(localWindow)) {
    findings.push({
      line: lineOf(source, broad.index),
      rule: "narrow-surface-uses-broad-calendar-events",
      message:
        "a narrow vaccination schedule/full-schedule surface references the broad Calendar events endpoint; call/build the endpoint that owns the screen grain/window",
    });
  }

  return findings;
}

function walk(dir) {
  if (!existsSync(dir)) return [];
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["node_modules", ".next", "build", ".turbo"].includes(entry.name)) continue;
      out.push(...walk(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (keep(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const base = process.env.CALENDAR_ENDPOINT_GRAIN_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const ref = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${ref}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(keep);
    } catch {
      // Try the next diff range.
    }
  }
  return null;
}

function selfTest() {
  const bad = [
    [
      "narrow-surface-uses-broad-calendar-events",
      "apps/admin-web/features/preventive-care-vaccination/full-vaccine-schedule.tsx",
      `export async function VaccinationFullSchedule() {
  return getCalendarVaccinationEvents({ month: 7 });
}`,
    ],
    [
      "narrow-surface-uses-broad-calendar-events",
      "apps/goatos-android/core/core-network/src/main/kotlin/AppApi.kt",
      `interface AppApi {
  // Monthly vaccination schedule
  @GET("calendar/vaccination/events")
  suspend fun getVaccinationSchedule(): CalendarEventListResponseDto
}`,
    ],
    [
      "schedule-contract-aliases-calendar-events",
      "contracts/openapi/app-api.yaml",
      `paths:
  /vaccination/schedule:
    get:
      responses:
        "200":
          description: CalendarEventListResponse from /calendar/vaccination/events
  /calendar/vaccination/events:
    get:
      responses: {}
`,
    ],
    [
      "narrow-surface-uses-broad-calendar-events",
      "backend/migrations/postgres/000999_bad.sql",
      `CREATE VIEW vaccination_schedule AS
SELECT * FROM calendar_event_projections; -- /calendar/vaccination/events
`,
    ],
  ];
  for (const [rule, rel, src] of bad) {
    const f = findingsForSource(src, rel);
    if (!f.some((x) => x.rule === rule)) {
      throw new Error(`self-test: '${rule}' not flagged for ${rel}`);
    }
  }

  const good = [
    [
      "apps/admin-web/features/preventive-care-vaccination/full-vaccine-schedule.tsx",
      `export async function VaccinationFullSchedule() {
  return getVaccinationSchedule({ year: 2026, month: 7 });
}`,
    ],
    [
      "apps/admin-web/lib/api/server.ts",
      `export async function getVaccinationSchedule() {
  return client.request("/vaccination/schedule");
}`,
    ],
    [
      "apps/goatos-android/core/core-network/src/main/kotlin/AppApi.kt",
      `interface AppApi {
  /** GET /calendar/vaccination/events - Calendar presentation only. */
  @GET("calendar/vaccination/events")
  suspend fun calendarEvents(): CalendarEventListResponseDto
}`,
    ],
    [
      "contracts/openapi/app-api.yaml",
      `paths:
  /vaccination/schedule:
    get:
      responses:
        "200":
          description: Vaccination schedule matrix.
  /calendar/vaccination/events:
    get:
      responses: {}
`,
    ],
    [
      "backend/migrations/postgres/000999_bounded.sql",
      `CREATE VIEW vaccination_schedule AS SELECT 1; -- calendar-endpoint-grain:ignore: generated fixture only`,
    ],
  ];
  for (const [rel, src] of good) {
    const f = findingsForSource(src, rel);
    if (f.length) {
      throw new Error(`self-test: false positive for ${rel}: ${f.map((x) => x.rule).join(", ")}`);
    }
  }

  console.log("calendar-endpoint-grain self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
if (all) {
  files = scanRoots.flatMap((root) => walk(join(repo, root)));
} else {
  files = changedFiles();
  if (files === null || files.length === 0) files = scanRoots.flatMap((root) => walk(join(repo, root)));
}

const findings = [];
for (const rel of files) {
  let source;
  try {
    source = readFileSync(join(repo, rel), "utf8");
  } catch {
    continue;
  }
  for (const f of findingsForSource(source, rel)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(`calendar-endpoint-grain: ${findings.length} endpoint-grain anti-pattern(s)`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded/calendar-owned, add `calendar-endpoint-grain:ignore: <reason>`.");
  process.exit(1);
}

console.log(`calendar-endpoint-grain: ok (${files.length} file(s) scanned)`);
