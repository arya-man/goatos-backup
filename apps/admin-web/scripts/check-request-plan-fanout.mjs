#!/usr/bin/env node

// check-request-plan-fanout.mjs — Detects high-cardinality Promise.all / mapped fetch fanout
// in admin-web page components. A page must never fetch N rows then fetch-per-row to load detail.
//
// Modes:
//   (default)     audit changed .tsx files vs $GITBASE or origin/main
//   --all         audit entire apps/admin-web
//   --self-test   run built-in fixtures and exit
//
// Escape hatch (time-bounded debt only): append
// request-plan:ignore owner=X issue=Y expires=YYYY-MM-DD reason=...

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../../..");

const isAdminWebTsx = (rel) => rel.startsWith("apps/admin-web/") && rel.endsWith(".tsx") && !rel.includes("/node_modules");

// Returns array of findings: { line, rule, message }. `line` is 1-indexed or null.
export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");

  // 1) Promise.all with unmapped single fetch (OK if it's a bounded set like 2-4 fixed calls).
  //    Flag only if it looks like a fan-out: .map( ... => fetch ) or unfiltered list.map
  lines.forEach((text, i) => {
    const ignoreLine = [text, ...lines.slice(Math.max(0, i - 3), i).reverse()].find((line) => /request-plan:ignore/.test(line)) ?? "";
    if (ignoreLine) {
      if (!validIgnore(ignoreLine)) {
        findings.push({ line: i + 1, rule: "invalid-ignore", message: "request-plan ignore requires owner, issue, unexpired YYYY-MM-DD, and reason" });
      }
      return;
    }

    // Pattern: Promise.all(items.map(...fetch...)) or Promise.all(list.map(...getSomething...))
    // This is a high-cardinality fan-out unless items/list is clearly bounded (e.g. 2-4 items).
    if (/Promise\.all\s*\(\s*\w+\s*\.map\s*\(/.test(text)) {
      findings.push({
        line: i + 1,
        rule: "promise-all-mapped-fanout",
        message: `Promise.all with .map() fetch fan-out — no cardinality bound visible. If items/list is unbounded (comes from backend pagination or filter), use a bounded batch/bulk API instead of per-row fetches.`,
      });
      return;
    }

  });

  // 2) Double fetch pattern: list fetch then detail fetch in Promise.all for same/overlapping resource.
  //    E.g., getCalendarVaccinationEvents + getCalendarVaccinationEvents (with different params)
  //          or getSop + getSop in the same Promise.all.
  const promiseAllBlocks = source.match(/Promise\.all\s*\(\s*\[([\s\S]*?)\]\s*\)/g) || [];
  for (const block of promiseAllBlocks) {
    // Check if this block has an ignore comment
    const blockStart = source.indexOf(block);
    const preceding = source.slice(0, blockStart).split("\n").slice(-3);
    if (block.split("\n").some(validIgnore) || preceding.some(validIgnore)) continue;

    const calls = block.match(/\b(?:get|fetch|list)[A-Za-z0-9]*\s*\(/g) || [];
    const callNames = calls.map((c) => c.replace(/\s*\(/, "").toLowerCase());
    // Check if same function is called twice with different params (likely overlapping request).
    const seen = new Set();
    for (const name of callNames) {
      if (seen.has(name)) {
        const line = source.slice(0, source.indexOf(block)).split("\n").length;
        findings.push({
          line,
          rule: "duplicate-fetch-in-promise-all",
          message: `${name}() called twice in Promise.all — likely overlapping requests. Combine into one bounded request with all needed params (e.g., includeDateMarkers, limit).`,
        });
        break;
      }
      seen.add(name);
    }
  }

  return findings;
}

function validIgnore(line) {
  if (!/request-plan:ignore/.test(line)) return false;
  const owner = line.match(/\bowner=([^\s]+)/)?.[1];
  const issue = line.match(/\bissue=([^\s]+)/)?.[1];
  const expires = line.match(/\bexpires=(\d{4}-\d{2}-\d{2})/)?.[1];
  const reason = line.match(/\breason=(.+)$/)?.[1]?.trim();
  if (!owner || !issue || !expires || !reason) return false;
  const expiry = new Date(`${expires}T23:59:59Z`);
  return !Number.isNaN(expiry.getTime()) && expiry.getTime() >= Date.now();
}

function walkAdminWeb(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules", ".next"].includes(entry.name)) continue;
      out.push(...walkAdminWeb(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isAdminWebTsx(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const base = process.env.GITBASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const refOk = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${refOk}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out
        .split("\n")
        .map((s) => s.trim())
        .filter(Boolean)
        .filter(isAdminWebTsx);
    } catch {
      /* try next range */
    }
  }
  return null;
}

function selfTest() {
  const bad = [
    ["Promise.all(sops.map((sop) => getSop(sop.id)))", "promise-all-mapped-fanout"],
    ["Promise.all(defs.map((def) => getSop(def.sop_id)))", "promise-all-mapped-fanout"],
    ["Promise.all([\n  getCalendarVaccinationEvents({...}),\n  getCalendarVaccinationEvents({...}),\n])", "duplicate-fetch-in-promise-all"],
    ["// request-plan:ignore reason=missing-metadata\nPromise.all(items.map((item) => getItem(item.id)))", "invalid-ignore"],
  ];
  for (const [src, rule] of bad) {
    const f = findingsForSource(src);
    if (!f.some((x) => x.rule === rule)) throw new Error(`self-test: '${rule}' not flagged for: ${src.slice(0, 80)}`);
  }
  const good = [
    "Promise.all([\n  getList(),\n  getDetail(selectedId),\n])",
    "const [a, b, c] = await Promise.all([\n  getA(),\n  getB(),\n  getC(),\n]);",
    "const items = await getItems(); const details = await Promise.all(items.map(i => getDetail(i.id))); // request-plan:ignore owner=test issue=TEST-1 expires=2099-12-31 reason=bounded fixture",
  ];
  for (const src of good) {
    const f = findingsForSource(src);
    if (f.length) throw new Error(`self-test: false positive on good source: ${src.slice(0, 80)} -> ${f.map((x) => x.rule)}`);
  }
  console.log("request-plan-fanout self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
if (all) {
  const adminWebDir = join(repo, "apps", "admin-web");
  files = walkAdminWeb(adminWebDir);
} else {
  files = changedFiles();
  if (files === null) {
    console.log("request-plan-fanout: skipped (no git diff base; run with --all to audit the whole tree)");
    process.exit(0);
  }
  if (files.length === 0) {
    console.log("request-plan-fanout: ok (no admin-web .tsx changed)");
    process.exit(0);
  }
}

const findings = [];
for (const rel of files) {
  const abs = join(repo, rel);
  let source;
  try {
    source = readFileSync(abs, "utf8");
  } catch {
    continue;
  }
  for (const f of findingsForSource(source, rel)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(`request-plan-fanout: ${findings.length} anti-pattern(s)`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If temporary debt is genuinely bounded, append `request-plan:ignore owner=X issue=Y expires=YYYY-MM-DD reason=...` on the line.");
  process.exit(1);
}
console.log(`request-plan-fanout: ok (${files.length} admin-web file(s) scanned; no unbounded Promise.all fanout)`);
