#!/usr/bin/env node

// check-android-bounded-memory.mjs — blocks "unbounded in-memory growth" anti-patterns in the
// Android data layer. Offline-first keeps data on device; a cache or accumulator that grows with
// no cap / TTL / eviction is a slow memory leak that OOMs the phone at scale. Commit 7058fff2
// added TTL + row/byte caps + LRU eviction (JsonBlobCacheSupport: readCachedJson /
// enforceCacheBounds / CacheGovernance); commit d58acac2 bounded the outbox by observing only
// ACTIVE rows and pruning SUCCEEDED instead of holding the whole table in memory forever.
//
// This guard is the MEMORY-retention lens; it is deliberately distinct from
// check-mobile-list-fetch.mjs (which owns fetch/page SIZE and `SELECT ... ORDER BY` with no LIMIT).
// Two rules:
//   unbounded-inmemory-collection  a class-field mutableMapOf/mutableListOf/mutableSetOf (or
//                                  HashMap/ArrayList/LinkedHashMap) that the file never evicts
//                                  from (no .clear/.remove/removeAt/removeFirst/poll/evict, and
//                                  not an LruCache). The in-heap cache/accumulator that grows.
//   whole-table-read               a DAO `@Query("SELECT * FROM <t>")` with NO where and NO limit
//                                  — observing/loading an entire table into memory (the outbox
//                                  observeAll shape). Filter by status or bound with LIMIT.
//
// Modes:
//   (default)     diff-scoped: scan only android .kt changed vs $MOBILE_GUARD_BASE (or origin/main).
//                 No android Kotlin changed -> PASS instantly (CI stays fast on other commits).
//   --all         audit the whole apps/goatos-android /src/main tree (backlog view).
//   --self-test   run the built-in fixtures and exit.
//
// Escape hatch: a genuinely-bounded case may append `mobile-guard:ignore: <reason>` on the line.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const isAndroidKt = (rel) =>
  rel.startsWith("apps/goatos-android/") &&
  rel.endsWith(".kt") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/") &&
  !/(?:Test|Fake|Preview)\w*\.kt$/.test(rel) &&
  !rel.endsWith("Entity.kt") &&
  !rel.endsWith("Dto.kt");

const MUTABLE_FIELD_RE =
  /^\s*(?:private\s+|internal\s+|protected\s+)?val\s+(\w+)\s*[:=][^\n]*\b(?:mutableMapOf|mutableListOf|mutableSetOf|hashMapOf|linkedMapOf|arrayListOf)\s*</;
const CTOR_FIELD_RE =
  /^\s*(?:private\s+|internal\s+|protected\s+)?val\s+(\w+)\s*=\s*(?:HashMap|LinkedHashMap|ArrayList|HashSet|LinkedHashSet)\s*</;

// Returns array of findings: { line, rule, message }.
export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");

  // Rule 1: unbounded in-memory collection field.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;
    const m = MUTABLE_FIELD_RE.exec(text) || CTOR_FIELD_RE.exec(text);
    if (!m) return;
    const name = m[1];
    // An LruCache / bounded wrapper on the same line is fine.
    if (/\bLruCache\b/.test(text)) return;
    // Evicts somewhere in the file? Then it is bounded — do not flag.
    const evictRe = new RegExp(
      `\\b${name}\\s*\\.\\s*(?:clear|remove|removeAt|removeFirst|removeLast|poll|evict|trimToSize)\\b`
    );
    if (evictRe.test(source)) return;
    findings.push({
      line: i + 1,
      rule: "unbounded-inmemory-collection",
      message:
        `in-memory collection '${name}' is written but never evicted (no cap/TTL/eviction) — it grows ` +
        "for the life of the process; use androidx LruCache, or a Room JsonBlobCacheDao with " +
        "readCachedJson (TTL) + enforceCacheBounds (row/byte cap), or clear/prune it explicitly",
    });
  });

  // Rule 2: whole-table DAO read — SELECT * with no WHERE and no LIMIT.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;
    const q = /"(\s*select\s+\*\s+from\s+[\s\S]*?)"/i.exec(text);
    if (!q) return;
    const sql = q[1].toLowerCase();
    if (/\bwhere\b/.test(sql) || /\blimit\b/.test(sql)) return; // bounded — fine
    findings.push({
      line: i + 1,
      rule: "whole-table-read",
      message:
        "DAO reads a whole table (SELECT * with no WHERE and no LIMIT) into memory (the outbox " +
        "observeAll shape); filter to active rows (WHERE status IN (...)) or bound with LIMIT and prune terminals",
    });
  });

  return findings;
}

function walkTree(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules", ".gradle"].includes(entry.name)) continue;
      out.push(...walkTree(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isAndroidKt(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const base = process.env.MOBILE_GUARD_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const refOk = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${refOk}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(isAndroidKt);
    } catch {
      /* try next range */
    }
  }
  return null;
}

function selfTest() {
  const bad = [
    ["unbounded-inmemory-collection", "private val cache = mutableMapOf<String, Dto>()"],
    ["unbounded-inmemory-collection", "  val rows = ArrayList<Entity>()"],
    ['whole-table-read', '@Query("SELECT * FROM outbox")\nfun observeAll(): Flow<List<OutboxEntity>>'],
  ];
  for (const [rule, src] of bad) {
    const f = findingsForSource(src);
    if (!f.some((x) => x.rule === rule)) throw new Error(`self-test: '${rule}' not flagged for: ${src.slice(0, 60)}`);
  }
  const good = [
    // Bounded: LruCache.
    "private val cache = LruCache<String, Dto>(100)",
    // Bounded: evicted elsewhere in the file.
    "private val cache = mutableMapOf<String, Dto>()\nfun trim() { cache.remove(oldestKey) }",
    // Single-object state, not a growing collection.
    "private val _state = MutableStateFlow(initial)",
    // Active-only observe (WHERE) — bounded.
    "@Query(\"SELECT * FROM outbox WHERE status IN ('QUEUED','IN_FLIGHT')\")\nfun observeActive(): Flow<List<OutboxEntity>>",
    // Bounded recent window (LIMIT).
    '@Query("SELECT * FROM outbox WHERE status = \'SUCCEEDED\' ORDER BY updatedAt DESC LIMIT :n")',
    // Single-row cache lookup by key (WHERE) — bounded.
    '@Query("SELECT * FROM calendar_cache WHERE cacheKey = :key")',
    // Escape hatch.
    "val cache = mutableMapOf<String, Dto>() // mobile-guard:ignore: bounded, sized to <=4 role tabs",
  ];
  for (const src of good) {
    const f = findingsForSource(src);
    if (f.length) throw new Error(`self-test: false positive on good source: ${src.slice(0, 60)} -> ${f.map((x) => x.rule)}`);
  }
  console.log("android-bounded-memory self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
if (all) {
  files = walkTree(join(repo, "apps/goatos-android"));
} else {
  files = changedFiles();
  if (files === null) {
    console.log("android-bounded-memory: skipped (no git diff base; run with --all to audit the whole tree)");
    process.exit(0);
  }
  if (files.length === 0) {
    console.log("android-bounded-memory: ok (no android Kotlin changed)");
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
  for (const f of findingsForSource(source)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(
    `android-bounded-memory: ${findings.length} unbounded-memory anti-pattern(s) ` +
      "(see docs/decisions/mobile-data-fetch-anti-patterns.md)"
  );
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded, append `mobile-guard:ignore: <reason>` on the line.");
  process.exit(1);
}
console.log(`android-bounded-memory: ok (${files.length} android file(s) scanned; no unbounded caches/reads)`);
