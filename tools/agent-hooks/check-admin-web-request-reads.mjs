#!/usr/bin/env node

// check-admin-web-request-reads.mjs — blocks the "Next.js SSR request-path full-table read"
// anti-pattern in apps/admin-web/**. The backend-side twin of the compute-on-read scale
// anti-patterns (docs/decisions/scale-anti-patterns.md): an admin-web server data helper must
// never drain a paginated backend endpoint cursor-by-cursor into one big array to compute a KPI
// on the request path. That is fast at ~1k goats and fatal at 1M. Read the projection/summary
// endpoint instead (e.g. `/herd-register/summary` returning pre-aggregated counts), as commit
// 810bc1b3 did when it dropped the `searchAllGoats` full-herd SSR walk.
//
// One high-confidence rule (no AST — plain source scan like the sibling guards):
//   cursor-drain-loop        a for/while whose body BOTH accumulates (`.push(...)`/`.concat(`)
//                            AND advances a cursor from `next_cursor` — the searchAllGoats shape.
//
// NOTE: the `Omit<Params, "limit" | "cursor">` signature is intentionally NOT flagged — it is also
// the correct shape of a projection/summary reader (e.g. getOperationsAuditSummary hits
// `/operations/audit/summary` and legitimately takes no page bound). Only the drain LOOP that
// actually materializes every page into one array is the anti-pattern.
//
// Modes:
//   (default)     diff-scoped: scan only admin-web .ts/.tsx changed vs $ADMIN_WEB_GUARD_BASE
//                 (or origin/main). No admin-web TS changed -> PASS instantly (CI stays fast).
//   --all         audit the whole apps/admin-web tree (backlog view).
//   --self-test   run the built-in fixtures and exit.
//
// Escape hatch: a genuinely-bounded case (fixed small cardinality) may append
// `scale-guard:ignore: <reason>` on the offending line (or the line above it).

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const isAdminWebTs = (rel) =>
  rel.startsWith("apps/admin-web/") &&
  (rel.endsWith(".ts") || rel.endsWith(".tsx")) &&
  !rel.includes("/node_modules/") &&
  !rel.includes("/.next/") &&
  !/\.(test|spec|mock|seed|stories)\.(ts|tsx)$/.test(rel) &&
  !rel.includes("/__tests__/") &&
  !rel.includes("/__mocks__/");

// Returns array of findings: { line, rule, message }.
export function findingsForSource(source, rel = "") {
  // A `'use client'` module is a browser component, not a request-path server read — skip it.
  if (/^\s*['"]use client['"]/m.test(source)) return [];

  const findings = [];
  const lines = source.split("\n");
  const ignored = (lineIdx) => {
    const here = lines[lineIdx] || "";
    const above = lines[lineIdx - 1] || "";
    return /scale-guard:ignore/.test(here) || /scale-guard:ignore/.test(above);
  };

  // Rule 1: cursor-drain loop. For each for/while, look at a bounded window of its body and flag
  // when it BOTH advances a cursor from `next_cursor` AND accumulates into an array. The two
  // together are unambiguously "drain every page into memory"; neither alone trips.
  const loopRe = /\b(?:for|while)\s*\(/g;
  let m;
  while ((m = loopRe.exec(source)) !== null) {
    const window = source.slice(m.index, m.index + 900);
    const drainsCursor = /next_cursor/.test(window) && /\bcursor\s*=/.test(window);
    const accumulates = /\.push\s*\(\s*\.\.\./.test(window) || /\.concat\s*\(/.test(window);
    if (drainsCursor && accumulates) {
      const lineIdx = source.slice(0, m.index).split("\n").length - 1;
      if (ignored(lineIdx)) continue;
      findings.push({
        line: lineIdx + 1,
        rule: "cursor-drain-loop",
        message:
          "drains a paginated endpoint cursor-by-cursor into one array on the request path " +
          "(the searchAllGoats full-herd shape); call a projection/summary endpoint that returns " +
          "pre-aggregated counts, or return one keyset page for the component to paginate",
      });
    }
  }

  return findings;
}

function walkTree(dir, keep) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["node_modules", ".next", "build", ".turbo"].includes(entry.name)) continue;
      out.push(...walkTree(path, keep));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (keep(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const base = process.env.ADMIN_WEB_GUARD_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const refOk = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${refOk}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(isAdminWebTs);
    } catch {
      /* try next range */
    }
  }
  return null;
}

function selfTest() {
  const bad = [
    [
      "cursor-drain-loop",
      `export async function searchAllGoats(params) {
  const items = [];
  let cursor;
  for (;;) {
    const page = await searchGoats({ ...params, limit: 100, cursor });
    if (!page.ok) return page;
    items.push(...page.data.items);
    if (!page.data.next_cursor) break;
    cursor = page.data.next_cursor;
  }
  return { ok: true, data: items };
}`,
    ],
  ];
  for (const [rule, src] of bad) {
    const f = findingsForSource(src);
    if (!f.some((x) => x.rule === rule)) {
      throw new Error(`self-test: '${rule}' not flagged for: ${src.slice(0, 60)}`);
    }
  }
  const good = [
    // Correct keyset read: single request, cursor handed back to the component.
    `export async function searchGoats(params: { limit: number; cursor?: string }) {
  return request(() => client.request("/goats/search", { query: params }));
}`,
    // Correct projection read: one request, pre-aggregated counts.
    `export async function getHerdRegisterSummary(params: HerdRegisterSummaryParams) {
  return request(() => client.request("/herd-register/summary", { query: params }));
}`,
    // Correct summary reader that legitimately omits limit/cursor (no drain loop) — must NOT flag.
    `export async function getOperationsAuditSummary(params: Omit<OperationsAuditListParams, "limit" | "cursor"> = {}) {
  return request(() => client.request("/operations/audit/summary", { query: params }));
}`,
    // A loop that advances a cursor but does NOT accumulate (e.g. processes each page) is not a drain.
    `for (;;) {
  const page = await next({ cursor });
  await handle(page.data.items);
  if (!page.data.next_cursor) break;
  cursor = page.data.next_cursor;
}`,
    // Client component is out of scope even if it looks drain-y.
    `'use client';
for (;;) { items.push(...page.data.items); if (!page.data.next_cursor) break; cursor = page.data.next_cursor; }`,
    // Escape hatch on a genuinely bounded case.
    `export async function listParks(p: Omit<Params, "limit" | "cursor">) {} // scale-guard:ignore: parks are <1000 fixed cardinality`,
  ];
  for (const src of good) {
    const f = findingsForSource(src);
    if (f.length) {
      throw new Error(`self-test: false positive on good source: ${src.slice(0, 60)} -> ${f.map((x) => x.rule)}`);
    }
  }
  console.log("admin-web-request-reads self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
if (all) {
  files = walkTree(join(repo, "apps/admin-web"), isAdminWebTs);
} else {
  files = changedFiles();
  if (files === null) {
    console.log("admin-web-request-reads: skipped (no git diff base; run with --all to audit the whole tree)");
    process.exit(0);
  }
  if (files.length === 0) {
    console.log("admin-web-request-reads: ok (no admin-web TS changed)");
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
  console.error(
    `admin-web-request-reads: ${findings.length} full-table request-read anti-pattern(s) ` +
      "(see docs/decisions/scale-anti-patterns.md)"
  );
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded, append `scale-guard:ignore: <reason>` on the line.");
  process.exit(1);
}
console.log(`admin-web-request-reads: ok (${files.length} admin-web file(s) scanned; no full-table request reads)`);
