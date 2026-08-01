#!/usr/bin/env node

// check-mobile-list-fetch.mjs — blocks mobile "fetch a huge list / aggregate on the client"
// anti-patterns. A phone viewport holds ~10 items; a screen must never pull hundreds of rows
// from Room/backend, must not expose manual "Load more" work-list CTAs, and a calendar overview
// must render from tiny per-day markers, not by parsing every event.
// See docs/decisions/mobile-data-fetch-anti-patterns.md.
//
// Modes:
//   (default)     diff-scoped: scan only mobile .kt changed vs $MOBILE_GUARD_BASE (or origin/main).
//                 No mobile Kotlin changed -> PASS instantly (CI stays fast on non-mobile commits).
//   --all         audit the whole mobile /src/main tree (backlog view; used by `make mobile-guard`).
//   --self-test   run the built-in fixtures and exit.
//
// Escape hatch: a genuinely-bounded case may append `mobile-guard:ignore: <reason>` on the line.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// A phone shows ~7-10 rows; a page is ~20. Anything larger is fetching more than a screen can use.
const MAX_PAGE = 20;

const isMobileKt = (rel) =>
  rel.startsWith("apps/goatos-android/") &&
  rel.endsWith(".kt") &&
  rel.includes("/src/main/") &&
  !rel.includes("/build/");

// Returns array of findings: { line, rule, message }. `line` is 1-indexed or null.

/**
 * A line that is prose, not code.
 *
 * Every rule here matches on source text, so a comment can trip it -- and the comments that trip
 * it are usually the ones documenting the FIX: "Before this, the DAO window was a frozen
 * `limit = 100`", or a KDoc promising "never a tappable Load more". Failing those files punishes
 * the author for explaining the bug they removed, and pressures the next one to delete the
 * explanation to get green.
 *
 * Blind spot, stated plainly: a genuine violation written on the same line as a trailing comment
 * is still seen (only the line's LEADING token is inspected), but a violation inside a block
 * comment is not -- commented-out code is not shipped code.
 */
function isProseLine(text) {
  const trimmed = text.trim();
  return trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*");
}

export function findingsForSource(source) {
  const findings = [];
  const lines = source.split("\n");

  // 1) Oversized page/list fetch. A `limit = N` arg or a *_LIMIT / *_PAGE_LIMIT / *_PAGE_SIZE
  //    constant above one screen-page (MAX_PAGE) means the screen pulls more than it can show.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text) || isProseLine(text)) return;
    const patterns = [
      /\blimit\s*=\s*(\d+)/,
      /\b[A-Za-z0-9_]*(?:PAGE_LIMIT|PAGE_SIZE|_LIMIT)\s*=\s*(\d+)/,
    ];
    for (const re of patterns) {
      const m = re.exec(text);
      if (m && Number(m[1]) > MAX_PAGE) {
        findings.push({
          line: i + 1,
          rule: "oversized-page-fetch",
          message: `fetches ${m[1]} rows (> ${MAX_PAGE}); a phone shows ~10 — use a ~20-row keyset page with load-more`,
        });
        break;
      }
    }
  });

  // 2) Calendar overview must render from backend day-markers, never by parsing events.
  //    Parsing dates inside the month/week grid builders is the "fetch + aggregate on client" smell.
  for (const fn of ["buildMonthDays", "buildWeekDays"]) {
    const start = source.search(new RegExp(`fun\\s+${fn}\\s*\\(`));
    if (start < 0) continue;
    // Body ends at the next top-level fun (heuristic) or 2500 chars, whichever comes first.
    const rest = source.slice(start + 3);
    const nextFn = rest.search(/\n(?:private |internal |public )?fun\s/);
    const body = rest.slice(0, nextFn < 0 ? 2500 : Math.min(nextFn, 2500));
    if (/\bparseLocalDate\s*\(|\bOffsetDateTime\.parse\s*\(/.test(body)) {
      const line = source.slice(0, start).split("\n").length;
      findings.push({
        line,
        rule: "overview-parses-events",
        message: `${fn} parses event dates; the week/month overview must render from backend day-markers (dots), not by fetching & parsing events`,
      });
    }
  }

  // 3) O(n^2) date scan: a `.find { ... parseLocalDate/OffsetDateTime.parse }` re-parses the whole
  //    list per item. Precompute once instead.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;
    if (/\.(?:find|first|last|any|count)\s*\{[^}]*(?:parseLocalDate|OffsetDateTime\.parse)/.test(text)) {
      findings.push({
        line: i + 1,
        rule: "on2-date-scan",
        message: "re-parses every event inside .find/.any (O(n^2)); parse each date once and reuse",
      });
    }
  });

  // 4) Unbounded DB read. Room is the UI's source of truth, so pagination binds the Room side too:
  //    an @Query that ORDER BYs without a LIMIT (e.g. observeAll() SELECT *) re-materializes the whole
  //    table into memory on every emission. Bound it to a ~20-row keyset window / PagingSource.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;
    if (/@Query\s*\(/.test(text) && /\bselect\b[\s\S]*\bfrom\b[\s\S]*\border\s+by\b/i.test(text) && !/\blimit\b/i.test(text)) {
      findings.push({
        line: i + 1,
        rule: "unbounded-db-read",
        message: "unbounded DAO read (SELECT ... ORDER BY with no LIMIT); the UI observes Room too — bound it to a ~20-row keyset window / PagingSource",
      });
    }
  });

  // 5) R50-007: Page-scoped entity matching (only searches loaded page collection, not full roster).
  //    Pattern: state.value.X.firstOrNull { it.matchesTag } or similar single-page lookups.
  //    Full roster searches must use a bounded Repository/DAO query, not the loaded page state.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;
    if (/state\.value\.\w+\.(?:firstOrNull|find|any)\s*\{\s*(?:it\.)?(?:matchesTag|matches|contains)\b/.test(text)) {
      findings.push({
        line: i + 1,
        rule: "page-scoped-entity-matching",
        message: "searches only the loaded page collection via state.value (R50-007); use a bounded Room/Repository query to search the full roster by tag/id",
      });
    }
  });

  // 6) R50-008: Unbounded accumulated JSON blobs in merge operations.
  //    Pattern: merging pages into a single growing JSON blob without a row/byte cap.
  //    Each page must be stored as individual Room rows (keyset-paginated) or bounded per-page JSON,
  //    never accumulated into an ever-growing blob.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;
    // Detect merges that concatenate two DTO row lists into one accumulated collection
    // (the growing-blob shape), regardless of where the surrounding .copy( sits.
    if (/\b(?:rows|items|entries)\s*=\s*\(\s*\w+\.(?:rows|items|entries)\s*\+\s*\w+\.(?:rows|items|entries)\s*\)/.test(text)) {
      findings.push({
        line: i + 1,
        rule: "unbounded-json-accumulation",
        message: "merges pages into an unbounded growing blob (R50-008); use individual Room rows with keyset pagination or enforce row/byte caps via Cache governance",
      });
    }
  });

  // 7) Mobile work queues must auto-page from LazyListState/viewport visibility. A visible
  // "Load more" button makes operators manage pagination instead of work, and it commonly leaks
  // when the first backend page contains rows filtered out by the selected day/status.
  lines.forEach((text, i) => {
    if (/mobile-guard:ignore/.test(text)) return;

    // Comments are prose, not UI. The phrase appears most often in a KDoc PROMISING there is no
    // tappable control ("never a tappable 'Load more'"), so matching them fails the very files
    // that document compliance.
    if (isProseLine(text)) return;

    // A passive footer -- a spinner plus "Loading more..." shown while the next page is ALREADY in
    // flight -- is explicitly allowed: the ban is on making the operator tap to continue their own
    // work queue. Distinguish by what is actually there: a control (clickable/Button/onClick) near
    // the label, not the label itself. Checked in a small window because the label and its
    // modifier are usually a few lines apart in Compose.
    const near = lines.slice(Math.max(0, i - 4), i + 5).join("\n");
    const isPassiveIndicator =
      /loading[_ ]more/i.test(text) && !/\b(?:clickable|Button|onClick|TextButton)\b/.test(near);
    if (isPassiveIndicator) return;

    if (/(?:Load\s+more|load\s+more|(?:^|[^A-Za-z0-9])load_more\b|(?:^|[^A-Za-z0-9])loading_more\b)/i.test(text)) {
      findings.push({
        line: i + 1,
        rule: "manual-load-more-mobile-ui",
        message: "visible/manual 'Load more' mobile UI is banned for work lists; keep cursor/Room pagination but trigger next page from LazyListState near the viewport end",
      });
    }
  });

  return findings;
}

function walkMobile(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (["build", "node_modules"].includes(entry.name)) continue;
      out.push(...walkMobile(path));
    } else if (entry.isFile()) {
      const rel = relative(repo, path);
      if (isMobileKt(rel)) out.push(rel);
    }
  }
  return out;
}

function changedMobileFiles() {
  const base = process.env.MOBILE_GUARD_BASE || "origin/main";
  const ranges = [`${base}...HEAD`, "HEAD~1...HEAD"];
  for (const range of ranges) {
    try {
      const refOk = range.split("...")[0];
      execSync(`git rev-parse --verify --quiet ${refOk}^{commit}`, { cwd: repo, stdio: "ignore" });
      const out = execSync(`git diff --name-only --diff-filter=d ${range}`, { cwd: repo, encoding: "utf8" });
      return out.split("\n").map((s) => s.trim()).filter(Boolean).filter(isMobileKt);
    } catch {
      /* try next range */
    }
  }
  return null; // could not resolve a diff base
}

function selfTest() {
  const bad = [
    ["private const val DAY_PAGE_LIMIT = 50", "oversized-page-fetch"],
    // Trailing comments must not launder a real violation.
    ["repo.observe(date, limit = 100) // grows on scroll", "oversized-page-fetch"],
    ["repo.observeEvents(dateFrom = k, dateTo = k, limit = 200)", "oversized-page-fetch"],
    ["private fun buildMonthDays(items: List<Dto>) {\n  items.mapNotNull { parseLocalDate(it.dueAt) }\n}\nprivate fun next() {}", "overview-parses-events"],
    ["val t = items.find { parseLocalDate(it.dueAt) == date }?.tone()", "on2-date-scan"],
    ["@Query(\"SELECT * FROM outbox ORDER BY createdAt ASC\")", "unbounded-db-read"],
    ["val row = state.value.roster.firstOrNull { it.matchesTag(target) } // R50-007 anti-pattern", "page-scoped-entity-matching"],
    ["val match = state.value.items.find { it.contains(query) }", "page-scoped-entity-matching"],
    ["val merged = page.copy(rows = (current.rows + page.rows).distinctBy { it.id })", "unbounded-json-accumulation"],
    ["Text(\"Load more sheds\")", "manual-load-more-mobile-ui"],
    ["val label = stringResource(R.string.scan_load_more)", "manual-load-more-mobile-ui"],
    // A "Loading more" label attached to a CONTROL is still a tappable load-more, however it is
    // worded. This is the case the passive-footer allowance must not swallow.
    ["TextButton(onClick = { next() }) {\n  Text(stringResource(R.string.videos_loading_more))\n}", "manual-load-more-mobile-ui"],
  ];
  for (const [src, rule] of bad) {
    const f = findingsForSource(src);
    if (!f.some((x) => x.rule === rule)) throw new Error(`self-test: '${rule}' not flagged for: ${src.slice(0, 60)}`);
  }
  const good = [
    // A comment RECORDING a fixed over-fetch is not an over-fetch. Failing it would make the
    // cheapest way to green be deleting the explanation of the bug.
    "// Before this, the DAO window was a frozen `limit = 100` while hasMore came from the cursor",
    // A passive footer: spinner + label, shown while the next page is already in flight. Allowed
    // by docs/decisions/mobile-data-fetch-anti-patterns.md -- the ban is on making an operator tap
    // to continue their own work queue, not on telling them a page is arriving.
    "CircularProgressIndicator(modifier = Modifier.size(16.dp))\nText(stringResource(R.string.videos_loading_more))",
    // Prose promising the absence of the control must not fail the file that documents it.
    "/**\n * One page per trigger, never a tappable \"Load more\".\n */",
    "repo.observeEvents(dateFrom = k, dateTo = k, limit = 20)",
    "private const val DAY_PAGE_LIMIT = 20",
    "repo.markers(month = m) // dots only, no events fetched",
    "val t = items.find { parseLocalDate(it.dueAt) == date } // mobile-guard:ignore: bounded <=7 day cells",
    "private fun buildMonthDays(markers: List<DayMarker>) {\n  markers.forEach { cell(it.date, it.tone) }\n}\nprivate fun next() {}",
    "@Query(\"SELECT * FROM outbox ORDER BY createdAt DESC LIMIT :pageSize\")",
    "@Query(\"SELECT * FROM calendar_cache WHERE cacheKey = :key\")",
    "val row = repo.findByTag(shedId, tag) // R50-007 correct: bounded Room query",
    "val merged = mergeScanRosterPage(current, page) // Uses bounded per-entity Room rows",
    "snapshotFlow { listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0 }",
    "CircularProgressIndicator(modifier = Modifier.size(18.dp))",
  ];
  for (const src of good) {
    const f = findingsForSource(src);
    if (f.length) throw new Error(`self-test: false positive on good source: ${src.slice(0, 60)} -> ${f.map((x) => x.rule)}`);
  }
  console.log("mobile-list-fetch self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const all = process.argv.includes("--all");
let files;
if (all) {
  files = walkMobile(join(repo, "apps/goatos-android"));
} else {
  files = changedMobileFiles();
  if (files === null) {
    console.log("mobile-list-fetch: skipped (no git diff base; run with --all to audit the whole tree)");
    process.exit(0);
  }
  if (files.length === 0) {
    // On main (or any checkout whose diff base equals HEAD), an empty diff is not evidence that
    // the tree is clean. Audit the authoritative source tree so the required default invocation
    // cannot report green while known violations remain.
    files = walkMobile(join(repo, "apps/goatos-android"));
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
  for (const f of findingsForSource(source)) findings.push({ ...f, rel });
}

if (findings.length) {
  console.error(`mobile-list-fetch: ${findings.length} anti-pattern(s) (see docs/decisions/mobile-data-fetch-anti-patterns.md)`);
  for (const f of findings) console.error(`- ${f.rule} ${f.rel}:${f.line}: ${f.message}`);
  console.error("If a case is genuinely bounded, append `mobile-guard:ignore: <reason>` on the line.");
  process.exit(1);
}
console.log(`mobile-list-fetch: ok (${files.length} mobile file(s) scanned; no over-fetch / client-aggregation / manual load-more UI)`);
