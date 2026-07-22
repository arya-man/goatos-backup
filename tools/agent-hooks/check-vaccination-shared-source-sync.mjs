#!/usr/bin/env node

// check-vaccination-shared-source-sync.mjs -- blocks the recurrence where
// Calendar / Protocol Adherence / Action Center / Workflows drift away from the
// vaccination operator-day source of truth, or where admin-web page switches fan
// out into per-row/per-page fetches.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const scanRoots = [
  "backend/internal/calendar",
  "backend/internal/processintegrity",
  "backend/internal/vaccinationexecution",
  "apps/admin-web/app",
  "apps/admin-web/features",
  "apps/admin-web/lib/api",
];

const scanExts = new Set([".go", ".sql", ".ts", ".tsx", ".mjs"]);
const allowRe = /vaccination-shared-source-sync:ignore/;

function extname(rel) {
  const dot = rel.lastIndexOf(".");
  return dot < 0 ? "" : rel.slice(dot);
}

function keep(rel) {
  if (!scanRoots.some((root) => rel.startsWith(`${root}/`))) return false;
  if (!scanExts.has(extname(rel))) return false;
  if (/\/(node_modules|\.next|build|\.turbo)\//.test(rel)) return false;
  if (/\.(test|spec|mock|stories)\.(ts|tsx|mjs|go)$/.test(rel)) return false;
  return true;
}

function lineOf(source, index) {
  return source.slice(0, index).split("\n").length;
}

function ignored(source, index) {
  const lines = source.split("\n");
  const lineIdx = lineOf(source, index) - 1;
  return allowRe.test(lines[lineIdx] || "") || allowRe.test(lines[lineIdx - 1] || "");
}

function backendSurface(rel, source) {
  if (!rel.startsWith("backend/internal/")) return false;
  return /\/(calendar|processintegrity|vaccinationexecution)\//.test(rel) ||
    /vaccination|Calendar|Action Center|Protocol Adherence|Workflows|process[-_ ]integrity/i.test(source);
}

function hasOperatorDayShape(source) {
  return /\boperator(?:_id|Name|Scope| assignment| day)?\b/i.test(source) &&
    /\b(planned_date|scheduled_date|due_date|drive_due_date|business_date)\b/i.test(source);
}

function hasStaleBatchDateShape(source) {
  return /\bobligation_batches\b/i.test(source) &&
    /\b(ob|batch|target_batch)\.(planned_date|window_start|window_end|due_at|scheduled_date)\b/i.test(source);
}

function findingsForBackend(source, rel) {
  if (!backendSurface(rel, source)) return [];
  if (!hasOperatorDayShape(source) || !hasStaleBatchDateShape(source)) return [];
  if (/\bvaccination_drive_assignments\b/.test(source)) return [];

  const index = Math.max(
    source.search(/\bobligation_batches\b/i),
    source.search(/\b(planned_date|scheduled_date|due_date|drive_due_date)\b/i)
  );
  if (index >= 0 && ignored(source, index)) return [];
  return [{
    line: index >= 0 ? lineOf(source, index) : 1,
    rule: "operator-day-without-drive-assignments",
    message:
      "vaccination operator-day/date read uses batch/obligation date state without vaccination_drive_assignments; " +
      "Calendar, Protocol Adherence, Action Center, and Workflows must share vaccination_drive_assignments as the canonical operator-day source",
  }];
}

function findingsForFrontend(source, rel) {
  if (!rel.startsWith("apps/admin-web/")) return [];
  if (/^\s*['"]use client['"]/m.test(source)) return [];
  const findings = [];

  const promiseAllRe = /Promise\.all[\s\S]{0,500}\.(?:map|flatMap)[\s\S]{0,500}(?:get|fetch)[A-Za-z0-9_]*(?:Vaccination|Calendar|Adherence|ActionCenter|Workflow|Execution)/gi;
  let match;
  while ((match = promiseAllRe.exec(source)) !== null) {
    if (ignored(source, match.index)) continue;
    findings.push({
      line: lineOf(source, match.index),
      rule: "page-switch-promise-all-fetch-map",
      message:
        "server page code fans out vaccination/calendar/process-integrity fetches in Promise.all(map); " +
        "page switching must call one grain-owned endpoint/read model, not N per row/filter/page",
    });
  }

  const loopAwaitRe = /\b(?:for\s*\([^)]*\)|for\s+await\s*\([^)]*\)|while\s*\([^)]*\))[\s\S]{0,700}\bawait\s+(?:get|fetch)[A-Za-z0-9_]*(?:Vaccination|Calendar|Adherence|ActionCenter|Workflow|Execution)/g;
  while ((match = loopAwaitRe.exec(source)) !== null) {
    if (ignored(source, match.index)) continue;
    findings.push({
      line: lineOf(source, match.index),
      rule: "page-switch-loop-await-fetch",
      message:
        "server page code awaits vaccination/calendar/process-integrity reads inside a loop; " +
        "batch it behind one backend endpoint/read model before rendering",
    });
  }

  return findings;
}

export function findingsForSource(source, rel = "fixture") {
  return [...findingsForBackend(source, rel), ...findingsForFrontend(source, rel)];
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
  const base = process.env.VACCINATION_SHARED_SOURCE_SYNC_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const ref = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${ref}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(keep);
    } catch {
      // Try the next range.
    }
  }
  return null;
}

function selfTest() {
  const bad = [
    [
      "operator-day-without-drive-assignments",
      "backend/internal/calendar/adapters/postgres/bad.go",
      `const sql = \`
SELECT ob.planned_date, ob.conducted_by AS operator_id
FROM obligation_batches ob
WHERE ob.planned_date = $1::date
\``,
    ],
    [
      "page-switch-promise-all-fetch-map",
      "apps/admin-web/app/vaccination/page.tsx",
      `export default async function Page({ days }) {
  const pages = await Promise.all(days.map((day) => getVaccinationAdherence({ asOf: day })));
  return pages.length;
}`,
    ],
    [
      "page-switch-loop-await-fetch",
      "apps/admin-web/features/process-integrity/bad.tsx",
      `export async function load(rows) {
  for (const row of rows) {
    await getCalendarVaccinationEvents({ date: row.date });
  }
}`,
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
      "backend/internal/calendar/adapters/postgres/targets.go",
      `const sql = \`
SELECT vda.planned_date, vda.operator_id
FROM vaccination_drive_assignments vda
JOIN obligation_batches ob ON ob.batch_id = vda.batch_id
WHERE vda.planned_date = $1::date
\``,
    ],
    [
      "apps/admin-web/app/vaccination/page.tsx",
      `export default async function Page() {
  const page = await getVaccinationAdherence({ limit: 50 });
  return page.data.rows.length;
}`,
    ],
    [
      "apps/admin-web/features/process-integrity/fixture.tsx",
      `// vaccination-shared-source-sync:ignore: bounded static story fixture
for (const row of rows) await getVaccinationExecution({ row });`,
    ],
  ];
  for (const [rel, src] of good) {
    const f = findingsForSource(src, rel);
    if (f.length) {
      throw new Error(`self-test: false positive for ${rel}: ${f.map((x) => x.rule).join(", ")}`);
    }
  }

  console.log("vaccination-shared-source-sync self-test: ok");
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
  if (files === null || files.length === 0) {
    console.log("vaccination-shared-source-sync: ok (no relevant changed files)");
    process.exit(0);
  }
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
  console.error(`vaccination-shared-source-sync: ${findings.length} shared-source/N+1 anti-pattern(s)`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded/test-only, add `vaccination-shared-source-sync:ignore: <reason>`.");
  process.exit(1);
}

console.log(`vaccination-shared-source-sync: ok (${files.length} file(s) scanned)`);
