#!/usr/bin/env node

// check-android-compose-lists.mjs — Jetpack Compose lazy-list correctness guard.
// Blocks the LazyColumn/LazyRow/LazyVerticalGrid/LazyHorizontalGrid key crash class
// and the index-key state-loss class. See docs/decisions/mobile-data-fetch-anti-patterns.md
// ("Compose lazy-list key correctness") + docs/mobile/android-ui-quality.md.
//
// Rules (all diff-scoped by default; a genuinely-bounded case appends
// `compose-guard:ignore: <reason>` on the offending line):
//
//   [lazy-list-missing-key]
//     items(<collection>) / itemsIndexed(<collection>) with NO `key =`. Without a
//     stable key Compose uses positional identity, so an insert/remove/reorder reuses
//     an item's remembered state for the WRONG row (checkbox/expand/scroll jumps) and
//     some list mutations crash. The count overload `items(<Int>)` is exempt (no key
//     param exists). Fix: items(list, key = { it.<uniqueRowId> }).
//
//   [lazy-list-entity-id-key]
//     A key whose primary selector is a bare per-ENTITY id (goatId/animalId/...), used
//     on a list that renders one row per OBLIGATION/record. A goat with two due vaccines
//     (ET+TT · PPR) then produces two rows with the SAME key ->
//     "IllegalArgumentException: Key <x> was already used" in the LazyList measure pass,
//     which pops the whole screen (shipped crash, Crashlytics 0.1.6-stg, fixed a9c35a1d).
//     Fix: key on the unique per-row id (obligationId) or a composite that separates the
//     two rows: key = { "${it.goatId}|${it.vaccineLabel}" }.
//
// Modes:
//   (default)     diff-scoped: only .kt changed vs $ANDROID_GUARD_BASE (or origin/main).
//   --all         audit the whole apps/goatos-android tree.
//   --self-test   run built-in fixtures and exit.

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

// Bare per-entity id selectors that are NOT safe as a lazy key when a list can
// hold >1 row per entity. Row-unique ids (id, obligationId, rowId, uuid, key)
// are intentionally excluded.
const ENTITY_ID = /\b(?:it|row|item|entry|[a-z]\w*)\.(goatId|animalId|goatUuid|animalUuid|herdAnimalId|tagId)\b/;

const lineOf = (source, index) => source.slice(0, index).split("\n").length;

// Extract the parenthesised argument text of the call starting at `open`
// (the index of the '(' ). Returns { args, endIndex } with balanced parens,
// ignoring parens inside strings.
function matchCall(source, open) {
  let depth = 0;
  let inStr = null;
  for (let i = open; i < source.length; i++) {
    const c = source[i];
    if (inStr) {
      if (c === "\\") { i++; continue; }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === '"' || c === "'") { inStr = c; continue; }
    if (c === "(") depth++;
    else if (c === ")") {
      depth--;
      if (depth === 0) return { args: source.slice(open + 1, i), endIndex: i };
    }
  }
  return null;
}

// True when the first positional arg is a count (the `items(count: Int)` overload),
// which has no `key` parameter to require.
function isCountOverload(args) {
  const first = args.split(",")[0].trim();
  return (
    /^\d+$/.test(first) ||
    /^count\b/i.test(first) ||
    /\.(size|count|length)\b/.test(first) ||
    /^[A-Za-z_]\w*Count$/.test(first)
  );
}

// Only Compose lazy scopes define the item DSL. Gate on lazy usage so a stdlib
// `items(` in a repository/util (e.g. a builder or a non-Compose extension) is
// never mistaken for a lazy-list item call.
const usesComposeLazy = (source) =>
  /androidx\.compose\.foundation\.lazy/.test(source) ||
  /\bLazy(Column|Row|VerticalGrid|HorizontalGrid|VerticalStaggeredGrid|HorizontalStaggeredGrid)\b/.test(source);

export function findingsForSource(source) {
  const findings = [];
  const seen = new Set();
  if (!usesComposeLazy(source)) return findings;

  const callRe = /\b(items|itemsIndexed)\s*\(/g;
  let m;
  while ((m = callRe.exec(source)) !== null) {
    const open = m.index + m[0].length - 1;
    const call = matchCall(source, open);
    if (!call) continue;
    const startLine = lineOf(source, m.index);
    const lineText = source.split("\n")[startLine - 1] || "";
    if (/compose-guard:ignore/.test(lineText)) continue;
    if (seen.has(startLine)) continue;
    seen.add(startLine);

    const args = call.args;
    const hasKey = /\bkey\s*=/.test(args);

    if (!hasKey) {
      if (isCountOverload(args)) continue; // count overload: no key param
      findings.push({
        line: startLine,
        rule: "lazy-list-missing-key",
        message:
          "items()/itemsIndexed() over a collection needs key = { it.<uniqueRowId> }; " +
          "positional keys reuse remembered state for the wrong row on insert/reorder.",
      });
      continue;
    }

    // has a key -> check it is not a bare per-entity id on a per-row list.
    const keyMatch = /\bkey\s*=\s*\{([\s\S]*?)\}/.exec(args);
    if (!keyMatch) continue;
    const keyBody = keyMatch[1];
    const composite = keyBody.includes('"') || keyBody.includes("`") || / to \b/.test(keyBody) || /Pair\s*\(/.test(keyBody);
    if (!composite && ENTITY_ID.test(keyBody)) {
      findings.push({
        line: startLine,
        rule: "lazy-list-entity-id-key",
        message:
          "lazy key selects a per-entity id (goatId/animalId/...): a list with >1 row per " +
          "entity (e.g. a goat with two due vaccines) yields duplicate keys -> " +
          "'Key was already used' crash. Key the unique per-row id (obligationId) or a composite.",
      });
    }
  }
  return findings;
}

function walkTree(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules"].includes(entry.name)) continue;
      out.push(...walkTree(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isAndroidKt(rel)) out.push(rel);
    }
  }
  return out;
}

function changedFiles() {
  const base = process.env.ANDROID_GUARD_BASE || "origin/main";
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
  // All fixtures are wrapped in a LazyColumn so the compose-lazy gate applies.
  const wrap = (inner) => `LazyColumn {\n${inner}\n}`;
  const bad = [
    ["items(rows) { r -> Row(r) }", "lazy-list-missing-key"],
    ["itemsIndexed(rows) { i, r -> Row(r) }", "lazy-list-missing-key"],
    ["items(filtered, key = { row -> row.goatId.takeIf { it.isNotBlank() } ?: row.primaryTag }) { }", "lazy-list-entity-id-key"],
    ["items(list, key = { it.animalId }) { }", "lazy-list-entity-id-key"],
  ];
  for (const [inner, rule] of bad) {
    const f = findingsForSource(wrap(inner));
    if (!f.some((x) => x.rule === rule)) throw new Error(`self-test: '${rule}' not flagged for: ${inner.slice(0, 70)}`);
  }
  const good = [
    "items(rows, key = { it.id }) { r -> Row(r) }",
    "items(rows, key = { it.obligationId }) { r -> Row(r) }",
    'items(filtered, key = { row -> row.obligationId.takeIf { it.isNotBlank() } ?: "${row.goatId}|${row.vaccineLabel}" }) { }',
    'items(matches, key = { "match-${it.goatId}" }) { }',
    "items(3) { Dot() }",
    "items(pageCount) { i -> Page(i) }",
    "items(rows.size) { i -> Row(rows[i]) }",
    "items(rows, key = { it.id }, contentType = { it.goatId }) { }",
    "items(rows) { r -> Row(r) } // compose-guard:ignore: static",
  ];
  for (const inner of good) {
    const f = findingsForSource(wrap(inner));
    if (f.length) throw new Error(`self-test: false positive: ${inner.slice(0, 70)} -> ${JSON.stringify(f)}`);
  }
  // Gate: a non-Compose file with a stdlib items( call must never be scanned.
  if (findingsForSource("fun build() { builder.items(rows) }").length) {
    throw new Error("self-test: non-Compose items( should be ignored");
  }
  console.log("check-android-compose-lists self-test: ok");
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
    console.log("check-android-compose-lists: skipped (no git diff base; run with --all to audit)");
    process.exit(0);
  }
  if (files.length === 0) files = walkTree(join(repo, "apps/goatos-android"));
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
  console.error(`check-android-compose-lists: ${findings.length} violation(s) (see docs/decisions/mobile-data-fetch-anti-patterns.md)`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded, append `compose-guard:ignore: <reason>` on the line.");
  process.exit(1);
}
console.log(`check-android-compose-lists: ok (${files.length} kotlin file(s) scanned)`);
