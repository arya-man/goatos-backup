#!/usr/bin/env node
// Keeps the OCI smoke's per-commit feature coverage SELF-UPDATING.
//
// Two ways coverage silently rots:
//   1. new user-visible admin-web commits land on origin/main and nobody writes an assertion
//   2. an existing assertion's selector/copy is renamed away, so the assertion quietly
//      asserts something that no longer exists (or starts passing for the wrong reason)
//
// This tool detects both against origin/main and either reports (--check, CI/guard) or
// applies (--write) the bookkeeping. It NEVER invents a selector: new commits land as
// status "needs-assertion" and drifted ones as "needs-review", and neither status is
// executed by apps/admin-web/scripts/lib/feature-assertions.mjs.
//
//   node tools/dashboard-automation/sync-coverage.mjs --check
//   node tools/dashboard-automation/sync-coverage.mjs --write
//   node tools/dashboard-automation/sync-coverage.mjs --self-test
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, realpathSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
export const statePath = join(repoRoot, "tools/dashboard-automation/coverage-state.json");
export const assertionsPath = join(repoRoot, "tools/dashboard-automation/feature-assertions.json");
export const ledgerDir = join(repoRoot, "tools/dashboard-automation/commit-classification");

// Web work that a human can see. Tests/stories are not user-visible surface.
const WEB_DIRS = /^apps\/admin-web\/(app|features|components|lib)\//;
const WEB_EXT = /\.(tsx|ts|css)$/;
const TEST_FILE = /(\.test\.|\.spec\.|__tests__\/|\.stories\.)/;
// Sources an assertion's selector or copy may legitimately live in.
const SOURCE_GLOBS = [
  { prefix: "apps/admin-web/", match: (p) => WEB_DIRS.test(p) && WEB_EXT.test(p) && !TEST_FILE.test(p) },
  { prefix: "backend/internal/adminui/app/service.go", match: (p) => p === "backend/internal/adminui/app/service.go" },
];
// Statuses the runner actually executes (must mirror feature-assertions.mjs loadFeatureAssertions).
export const EXECUTED_STATUSES = new Set(["assert", "data-dependent", "mobile-only"]);
export const PARKED_STATUSES = new Set(["needs-assertion", "needs-review"]);

export function isWebSurfaceFile(file) {
  return WEB_DIRS.test(file) && WEB_EXT.test(file) && !TEST_FILE.test(file);
}

export function guessKind(subject) {
  const s = String(subject ?? "");
  if (/^fix(\(|:|!)/i.test(s)) return "bugfix";
  if (/^feat(\(|:|!)/i.test(s)) return "feature";
  if (/^(style|ui|polish)(\(|:|!)/i.test(s)) return "ui-polish";
  if (/^(refactor|perf)(\(|:|!)/i.test(s)) return "refactor";
  if (/^(chore|docs|test|ci|build|revert)(\(|:|!)/i.test(s)) return "chore";
  return "unknown";
}

// Commit subjects are repo history verbatim, and history occasionally names a symbol whose spelling
// collides with a cross-organization/project name that tools/agent-hooks/check-org-boundary.mjs
// blocks on sight. That guard reads ADDED LINES, so an unscrubbed subject landing in this ledger
// fails the next ci-local run for a commit nobody is editing. Terms are base64 here for the same
// reason they are base64 in the guard: writing them plainly would trip it on this file. Keep this
// list in step with the guard's.
const BLOCKED_ORG_TERMS = [
  { value: "SGV2YQ==", caseSensitive: false },
  { value: "U2xpY2U=", caseSensitive: true },
  { value: "aGV2YXBsYXRmb3Jt", caseSensitive: false },
].map((entry) => ({
  term: Buffer.from(entry.value, "base64").toString("utf8"),
  caseSensitive: entry.caseSensitive,
}));

// Redacts rather than rewrites: "[redacted]" reads as a deliberate elision, where substituting a
// near-synonym would leave a subject that looks like real history and is not.
export function scrubOrgNames(text) {
  let out = String(text ?? "");
  for (const { term, caseSensitive } of BLOCKED_ORG_TERMS) {
    out = out.replace(new RegExp(term.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), caseSensitive ? "g" : "gi"), "[redacted]");
  }
  return out;
}

// Commit subjects carry a conventional-commit prefix and often a trailing issue ref.
export function cleanTitle(subject) {
  return String(subject ?? "")
    .replace(/^[a-z]+(\([^)]*\))?!?:\s*/i, "")
    .replace(/\s*\(#\d+\)\s*$/, "")
    .trim();
}

// Best-guess route from a changed page path; null rather than a wrong guess.
export function guessRoute(files, knownRoutes = new Set()) {
  for (const file of files) {
    const page = file.match(/^apps\/admin-web\/app\/(?:\([^)]*\)\/)?(.+)\/page\.tsx$/);
    if (!page) continue;
    const segs = page[1].split("/").filter((seg) => !seg.startsWith("(") && !seg.startsWith("["));
    const name = segs.join("-");
    if (name) return name;
  }
  for (const file of files) {
    const feature = file.match(/^apps\/admin-web\/features\/[^/]+\/([^/]+)\.tsx$/);
    if (feature && knownRoutes.has(feature[1])) return feature[1];
  }
  return null;
}

// Selectors an assertion depends on: class names / data-testids / copy strings.
// Copy strings only count as source-backed when the entry's own evidence quotes them.
// Anything else in a `text` target is a data value (a feed name, an animal tag) that is
// rendered from the database and correctly absent from source — flagging those would be
// exactly the kind of false alarm this automation has been burned by before.
export function evidenceQuotes(entry) {
  return new Set([...String(entry.evidence ?? "").matchAll(/"([^"]{1,120})"/g)].map((m) => m[1].replace(/\s+/g, " ").trim().toLowerCase()));
}

export function selectorsFor(entry) {
  const out = [];
  const addTarget = (target) => {
    if (!target) return;
    if (target.css) out.push(...cssTokens(target.css));
    if (target.text) out.push({ kind: "text", value: String(target.text) });
  };
  for (const step of entry.steps ?? []) addTarget(step.click);
  for (const expect of entry.expect ?? []) {
    // `absent` asserts something must NOT be there: its disappearance from source is the point.
    addTarget(expect.visible);
    if (expect.count?.css) out.push(...cssTokens(expect.count.css));
  }
  return out;
}

export function cssTokens(css) {
  const out = [];
  for (const m of String(css).matchAll(/\[data-testid=["']([^"']+)["']\]/g)) out.push({ kind: "testid", value: m[1] });
  for (const m of String(css).matchAll(/\.([A-Za-z_][\w-]{2,})/g)) out.push({ kind: "class", value: m[1] });
  for (const m of String(css).matchAll(/\[(data-[\w-]+)(?:[~^$*|]?=|\])/g)) {
    if (m[1] !== "data-testid") out.push({ kind: "attr", value: m[1] });
  }
  return out;
}

// Conservative: a token is only "gone" when it cannot be found anywhere in the UI sources.
// Copy strings are checked case-insensitively with whitespace collapsed, and dynamic-looking
// or very short strings are skipped so this never becomes a false-alarm generator.
export function tokenIsStale(token, haystack, haystackLower) {
  if (token.kind === "text") {
    const value = token.value.replace(/\s+/g, " ").trim();
    if (value.length < 4) return false;
    if (/\d{2,}/.test(value)) return false; // counts, dates, ids: rendered, never literal in source
    return !haystackLower.includes(value.toLowerCase());
  }
  return !haystack.includes(token.value);
}

// Lane 1's own ledger, by name. This used to be "every .jsonl in the directory", which quietly
// swallowed the lane 2-5 ledgers added later and made lane 1 believe it covered an Android commit.
export const LANE1_LEDGER_FILES = ["p1.jsonl", "p2.jsonl", "p3.jsonl", "p4.jsonl", "sync.jsonl", "web-A.jsonl", "web-B.jsonl", "web-C.jsonl"];

export function readLedger(dir = ledgerDir, files = LANE1_LEDGER_FILES) {
  const rows = [];
  if (!existsSync(dir)) return rows;
  const present = new Set(readdirSync(dir).filter((n) => n.endsWith(".jsonl")));
  for (const file of files.filter((n) => present.has(n)).sort()) {
    for (const [i, line] of readFileSync(join(dir, file), "utf8").split("\n").entries()) {
      if (!line.trim()) continue;
      try {
        rows.push({ ...JSON.parse(line), _file: file });
      } catch (error) {
        throw new Error(`${file}:${i + 1}: invalid JSON (${error.message})`);
      }
    }
  }
  return rows;
}

export function ledgerFileForAppend(dir = ledgerDir) {
  const own = join(dir, "sync.jsonl");
  return own;
}

// ---------------------------------------------------------------- git access

function git(args, { cwd = repoRoot } = {}) {
  const run = spawnSync("git", ["-C", cwd, ...args], { encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });
  if (run.status !== 0) throw new Error(`git ${args.slice(0, 3).join(" ")} failed: ${String(run.stderr ?? "").trim().slice(0, 200)}`);
  return run.stdout;
}

export function defaultGitPort(ref = "origin/main") {
  return {
    ref,
    revListOrder: () => git(["rev-list", "--abbrev=9", "--abbrev-commit", ref]).split("\n").filter(Boolean),
    commitsSince: (sha) => parseLog(git(["log", "--no-merges", "--abbrev=9", "--name-only", "--date=short", "--format=%x00%h%x1f%cd%x1f%s", `${sha}..${ref}`])),
    readSources: () => {
      const listed = git(["ls-tree", "-r", "--name-only", ref]).split("\n").filter(Boolean);
      const wanted = listed.filter((p) => SOURCE_GLOBS.some((g) => g.match(p)));
      const chunks = [];
      for (let i = 0; i < wanted.length; i += 200) {
        // `git cat-file --batch` in one pass beats 700 spawns.
        const batch = wanted.slice(i, i + 200).map((p) => `${ref}:${p}`).join("\n");
        const run = spawnSync("git", ["-C", repoRoot, "cat-file", "--batch"], { input: batch, encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });
        chunks.push(run.stdout ?? "");
      }
      return chunks.join("\n");
    },
    lastTouchedBy: (file) => git(["log", "-1", "--abbrev=9", "--format=%h", ref, "--", file]).trim() || null,
    // Shas only. The lane half asks "is this commit accounted for", not "what did it touch",
    // so one cheap log over the whole window is enough for the common case.
    commitsInWindow: () => parseLog(git(["log", "--no-merges", "--abbrev=9", "--date=short", `--since=${COVERAGE_WINDOW}`, "--format=%x00%h%x1f%cd%x1f%s", ref])),
    // File names for a handful of shas only. Asking for --name-only across the whole window
    // would be thousands of commits of output on every guard run, to answer a question about
    // the few that are not yet in a ledger.
    filesFor: (shas) => {
      const out = [];
      for (let i = 0; i < shas.length; i += 400) {
        out.push(...parseLog(git(["log", "--no-walk=unsorted", "--no-merges", "--abbrev=9", "--name-only", "--date=short", "--format=%x00%h%x1f%cd%x1f%s", ...shas.slice(i, i + 400)])));
      }
      return out;
    },
  };
}

export function parseLog(stdout) {
  const commits = [];
  for (const block of stdout.split("\0")) {
    if (!block.trim()) continue;
    const [header, ...rest] = block.split("\n");
    const [sha, date, ...subjectParts] = header.split("");
    commits.push({ sha, date, subject: subjectParts.join(""), files: rest.filter(Boolean) });
  }
  return commits;
}

// ---------------------------------------------------------------- planning

export function seedState(rows, revListOrder) {
  const known = new Set(rows.map((r) => String(r.sha).slice(0, 9)));
  for (const sha of revListOrder) if (known.has(sha)) return sha;
  return null;
}

export function planSync({ state, ledgerRows, assertions, git: port }) {
  const knownShas = new Set(ledgerRows.map((r) => String(r.sha).slice(0, 9)));
  const assertedShas = new Set(assertions.map((e) => String(e.sha).slice(0, 9)));
  const knownRoutes = new Set(assertions.map((e) => e.route).filter(Boolean));
  const evidenceFiles = new Map(); // file -> [entry index]
  assertions.forEach((entry, index) => {
    for (const m of String(entry.evidence ?? "").matchAll(/\b((?:apps|backend)\/[\w./-]+\.(?:tsx|ts|css|go))\b/g)) {
      evidenceFiles.set(m[1], [...(evidenceFiles.get(m[1]) ?? []), index]);
    }
  });

  const since = state?.lastSyncedSha;
  const commits = since ? port.commitsSince(since) : [];
  const newCommits = [];
  for (const commit of commits) {
    const sha = String(commit.sha).slice(0, 9);
    if (knownShas.has(sha) && assertedShas.has(sha)) continue;
    const webFiles = commit.files.filter(isWebSurfaceFile);
    const touchesAsserted = commit.files.some((f) => evidenceFiles.has(f));
    if (webFiles.length === 0 && !touchesAsserted) continue;
    newCommits.push({
      sha,
      date: commit.date,
      subject: commit.subject,
      files: commit.files,
      webFiles,
      kind: guessKind(commit.subject),
      route: guessRoute(webFiles.length ? webFiles : commit.files, knownRoutes),
      reason: webFiles.length ? "new user-visible admin-web work" : "modifies a file an existing assertion relies on",
      inLedger: knownShas.has(sha),
      hasAssertion: assertedShas.has(sha),
    });
  }

  const haystack = port.readSources();
  const haystackLower = haystack.toLowerCase();
  const staleCache = new Map();
  const stale = [];
  for (const [index, entry] of assertions.entries()) {
    if (!EXECUTED_STATUSES.has(entry.status)) continue;
    const quoted = evidenceQuotes(entry);
    const tokens = selectorsFor(entry).filter((t) => t.kind !== "text" || quoted.has(t.value.replace(/\s+/g, " ").trim().toLowerCase()));
    const gone = tokens.filter((token) => {
      const key = `${token.kind}:${token.value}`;
      if (!staleCache.has(key)) staleCache.set(key, tokenIsStale(token, haystack, haystackLower));
      return staleCache.get(key);
    });
    if (gone.length === 0) continue;
    const files = [...evidenceFiles.entries()].filter(([, idx]) => idx.includes(index)).map(([file]) => file);
    const culprit = files.map((file) => port.lastTouchedBy(file)).find(Boolean) ?? "unknown";
    stale.push({
      index,
      sha: entry.sha,
      title: entry.title,
      route: entry.route,
      token: gone[0],
      reason: `selector ${gone[0].value} no longer in source (changed by ${culprit})`,
    });
  }
  return { newCommits, stale, since };
}

export function newLedgerRow(commit) {
  return {
    sha: commit.sha,
    date: commit.date,
    subject: scrubOrgNames(commit.subject),
    files: commit.files.slice(0, 40),
    kind: commit.kind,
    userVisible: commit.webFiles.length > 0,
    source: "sync-coverage",
  };
}

export function newAssertionEntry(commit) {
  return {
    sha: commit.sha,
    title: scrubOrgNames(cleanTitle(commit.subject)),
    route: commit.route,
    viewports: ["laptop", "mobile"],
    steps: [],
    expect: [],
    status: "needs-assertion",
    evidence: commit.webFiles.slice(0, 6).join("; "),
    reason: `${commit.reason}; needs a human-written read-only assertion (run sync-coverage.mjs --write, then fill steps/expect)`,
  };
}

export function applyPlan({ plan, assertions, ledgerRows }) {
  const appended = plan.newCommits.filter((c) => !c.inLedger).map(newLedgerRow);
  const updated = assertions.map((e) => ({ ...e }));
  for (const item of plan.stale) {
    updated[item.index].status = "needs-review";
    updated[item.index].reason = item.reason;
  }
  for (const commit of plan.newCommits) {
    if (commit.hasAssertion) continue;
    updated.push(newAssertionEntry(commit));
  }
  const lastSyncedSha = plan.newCommits[0]?.sha ?? plan.since;
  return { appended, assertions: updated, lastSyncedSha, ledgerRows };
}

export function describePlan(plan) {
  const lines = [];
  if (plan.newCommits.length) {
    lines.push(`${plan.newCommits.length} new commit(s) need smoke coverage:`);
    for (const c of plan.newCommits.slice(0, 20)) lines.push(`  ${c.sha} ${c.date} ${cleanTitle(c.subject).slice(0, 80)} (${c.reason})`);
    if (plan.newCommits.length > 20) lines.push(`  +${plan.newCommits.length - 20} more`);
  }
  if (plan.stale.length) {
    lines.push(`${plan.stale.length} existing assertion(s) reference source that no longer exists:`);
    for (const s of plan.stale.slice(0, 20)) lines.push(`  ${s.sha} ${String(s.title).slice(0, 60)} — ${s.reason}`);
    if (plan.stale.length > 20) lines.push(`  +${plan.stale.length - 20} more`);
  }
  return lines.join("\n");
}

export function summarizePlan(plan) {
  return `coverage-sync: ${plan.newCommits.length} new commit(s) need smoke coverage, ${plan.stale.length} assertion(s) need review (since ${plan.since ?? "<unseeded>"})`;
}

// ---------------------------------------------------------------- lanes 2-5 coverage
//
// Lane 1 keeps its own assertion-level bookkeeping above. This half guards the DIFFERENT
// promise made by tools/dashboard-automation/lane-checks.json: that EVERY commit on
// origin/main since 2026-08-01 is accounted for exactly once across lane 1, lanes 2-5 and
// the parked list. A commit landing on main that no lane covers fails the guard here, so
// "every commit since Aug 1" cannot quietly decay into "every commit up to the day someone
// last looked".

// Pinned on purpose. A bare `--since=2026-08-01` is a git approxidate: with no time given git
// fills in the CURRENT time of day, so the same command returns a different commit set at 03:00
// and at 12:00, and the coverage claim stops being reproducible. Farm-local midnight, always.
export const COVERAGE_WINDOW = "2026-08-01T00:00:00+05:30";

export const LANE_LEDGER_FILES = {
  lane2: "lane2.jsonl",
  lane3: "lane3.jsonl",
  lane4: "lane4.jsonl",
  "lane5-android": "lane5-android.jsonl",
  "not-automatable": "not-automatable.jsonl",
  // Where --write parks a commit that has landed but that no human has routed to a lane yet.
  // It is COVERED for the exactly-once count and NOT covered by any check: never faked green.
  "needs-lane": "needs-lane.jsonl",
};
export const LANE_NEEDS_ROUTING = "needs-lane";
export const laneChecksPath = join(repoRoot, "tools/dashboard-automation/lane-checks.json");

// The automation's OWN bookkeeping. A commit that touches nothing else has no farm-facing
// surface, and if it were treated as uncovered the guard could never go green: writing the
// ledger produces a commit, which would itself be uncovered, for ever. It is auto-parked with
// a reason and --write writes that reason into the parked ledger, so the count stays checkable.
export const COVERAGE_BOOKKEEPING = /^tools\/dashboard-automation\/(commit-classification\/|lane-checks\.json|coverage-state\.json|feature-assertions\.json|coverage-since-aug1\.json|LANE-COVERAGE-REPORT\.md|bug-pattern-coverage\.json)/;
export const AUTO_PARK_REASON =
  "Touches only the dashboard automation's own coverage ledger, so there is nothing on a page or " +
  "a phone screen for any lane to look at.";

export function isCoverageBookkeeping(commit) {
  const files = commit?.files ?? [];
  return files.length > 0 && files.every((f) => COVERAGE_BOOKKEEPING.test(f));
}

// Mirrors the generator's validator. Re-checked on every guard run so a hand-edit to
// lane-checks.json can never slip a write past the read-only contract.
const SQL_WRITE_KEYWORD = /\b(insert|update|delete|drop|alter|create|truncate|grant|revoke|copy|merge|vacuum|analyze|refresh|call|do|lock|reindex|nextval|setval|dblink|pg_terminate|pg_cancel)\b/i;

export function sqlReadOnlyFindings(sql, id) {
  const out = [];
  const text = String(sql ?? "").trim();
  if (!text) return [`${id}: lane 2 check has no sql`];
  if (!/^select\b/i.test(text)) out.push(`${id}: lane 2 sql must start with SELECT`);
  if (text.includes(";")) out.push(`${id}: lane 2 sql must be a single statement (no ';')`);
  if (!/\blimit\s+\d+/i.test(text)) out.push(`${id}: lane 2 sql must carry a LIMIT`);
  const bare = text.replace(/'[^']*'/g, "''");
  const bad = bare.match(SQL_WRITE_KEYWORD);
  if (bad) out.push(`${id}: lane 2 sql contains a non-read keyword '${bad[0]}' — the STG replica is read-only`);
  return out;
}

// A failure sentence is read by a farm manager in Slack. No check codes, no selectors, no SQL,
// no stack traces, no field names.
const JARGON = [
  [/\bSELECT\b|\bFROM\s+public\./, "SQL"],
  // `.wchart svg`, `div.card`, `[data-testid=...]`: a CSS selector standing on its own.
  [/data-testid|\bcss\b|\.[a-z][\w-]*\s*\{|(?:^|\s)\.[a-z][\w-]{2,}/, "a selector"],
  [/\blane[0-9]\.|\bP-[a-z-]+\b/, "a check code"],
  [/\b[a-z_]+_id\b|\b[a-z_]+\.[a-z_]+\(/, "a field or function name"],
  [/\b(5xx|4xx|p9[059]|NaN|null|undefined|HTTP \d{3})\b/, "an engineering term"],
  [/\bat [\w./]+:\d+\b|\bstack trace\b/i, "a stack trace"],
];

export function failureSentenceFindings(sentence, id) {
  const text = String(sentence ?? "").trim();
  if (!text) return [`${id}: check has no failureSentence`];
  const out = [];
  for (const [rx, what] of JARGON) {
    if (rx.test(text)) out.push(`${id}: failureSentence contains ${what} — it is read by a farm manager, not an engineer: "${text.slice(0, 90)}"`);
  }
  return out;
}

export function readLaneLedgers(dir = ledgerDir) {
  const byLane = {};
  const rows = [];
  for (const [lane, file] of Object.entries(LANE_LEDGER_FILES)) {
    byLane[lane] = [];
    const path = join(dir, file);
    if (!existsSync(path)) continue;
    for (const [i, line] of readFileSync(path, "utf8").split("\n").entries()) {
      if (!line.trim()) continue;
      let parsed;
      try {
        parsed = JSON.parse(line);
      } catch (error) {
        throw new Error(`${file}:${i + 1}: invalid JSON (${error.message})`);
      }
      const row = { ...parsed, _file: file, _lane: lane, _line: i + 1 };
      byLane[lane].push(row);
      rows.push(row);
    }
  }
  return { byLane, rows };
}

export function readLaneChecks(path = laneChecksPath) {
  if (!existsSync(path)) return null;
  return JSON.parse(readFileSync(path, "utf8"));
}

export function planLaneCoverage({ windowShas, windowCommits, lane1Rows, laneRows, laneChecks }) {
  const short = (sha) => String(sha).slice(0, 9);
  const commits = windowCommits ?? (windowShas ?? []).map((sha) => ({ sha, files: [] }));
  const window = new Set(commits.map((c) => short(c.sha)));
  const bookkeepingOnly = new Set(commits.filter(isCoverageBookkeeping).map((c) => short(c.sha)));
  const lane1 = new Set(lane1Rows.map((r) => short(r.sha)));

  // exactly-once: a sha may appear in at most one lane ledger
  const seen = new Map();
  const duplicated = [];
  for (const row of laneRows) {
    const sha = short(row.sha);
    if (!sha) continue;
    if (seen.has(sha)) {
      duplicated.push({ sha, files: [seen.get(sha)._file, row._file] });
      continue;
    }
    seen.set(sha, row);
  }

  const uncovered = [];
  const autoParked = [];
  for (const sha of window) {
    if (lane1.has(sha) || seen.has(sha)) continue;
    (bookkeepingOnly.has(sha) ? autoParked : uncovered).push(sha);
  }

  const outsideWindow = [...seen.keys()].filter((sha) => !window.has(sha));

  const checkIds = new Set();
  const contract = [];
  const weakTies = {};
  let checkCount = 0;
  // A check routed to no commit covers nothing. 37 of the 194 are in that state — good
  // invariants the roadmap asked for, which no commit since 2026-08-01 maps to — and counting
  // them in the headline overstated the catalogue by 24%. They are kept, because deleting a
  // real invariant trades noise for blindness, and they are counted separately so the number
  // that gets quoted as coverage is only ever the checks that cover something.
  let commitCoveringCheckCount = 0;
  const checksCoveringNoCommit = [];
  if (laneChecks?.lanes) {
    for (const [lane, spec] of Object.entries(laneChecks.lanes)) {
      for (const check of spec.checks ?? []) {
        checkCount += 1;
        const routed = Number(check.sourceCommitCount ?? 0) > 0 || (check.sourceShas ?? []).length > 0;
        if (routed) commitCoveringCheckCount += 1;
        else checksCoveringNoCommit.push({ lane, id: check.id, guards: check.derivedFrom ? "a rule" : "nothing stated" });
        checkIds.add(check.id);
        contract.push(...failureSentenceFindings(check.failureSentence, check.id));
        if (lane === "lane2") contract.push(...sqlReadOnlyFindings(check.sql, check.id));
        if (lane === "lane3" && check.method && check.method !== "GET") {
          contract.push(`${check.id}: lane 3 is read-only, so only GET is allowed (found ${check.method})`);
        }
        if (lane === "lane4" && !(check.writesTables ?? []).length) {
          contract.push(`${check.id}: lane 4 writes, so it must name the tables it touches for the restore to be provable`);
        }
      }
    }
  }

  const dangling = [];
  const misfiled = [];
  const parkedWithoutReason = [];
  for (const row of laneRows) {
    if (row._lane !== row.lane && row.lane) misfiled.push({ sha: short(row.sha), file: row._file, lane: row.lane });
    if (row._lane === "not-automatable" || row._lane === LANE_NEEDS_ROUTING) {
      if (!String(row.reason ?? "").trim()) parkedWithoutReason.push({ sha: short(row.sha), file: row._file });
      continue;
    }
    if (!row.checkId) {
      dangling.push({ sha: short(row.sha), file: row._file, checkId: null });
    } else if (checkIds.size && !checkIds.has(row.checkId)) {
      dangling.push({ sha: short(row.sha), file: row._file, checkId: row.checkId });
    }
    const lane = row._lane;
    weakTies[lane] = weakTies[lane] ?? { "subject+path": 0, subject: 0, path: 0 };
    if (row.matchStrength in weakTies[lane]) weakTies[lane][row.matchStrength] += 1;
  }

  const needsRouting = (laneRows.filter((r) => r._lane === LANE_NEEDS_ROUTING) ?? []).length;

  // A sha can sit in BOTH lane 1's classification ledger and one of these ledgers - every Android
  // commit does, because lane 1 classified it but explicitly does not cover the app. So the
  // accounted-for total is the union, never the sum of the two.
  const inLane1Only = [...window].filter((sha) => lane1.has(sha) && !seen.has(sha)).length;
  const inLanes = [...window].filter((sha) => seen.has(sha)).length;
  const autoParkedCount = autoParked.length;
  return {
    windowSize: window.size,
    lane1Covered: inLane1Only,
    laneCovered: inLanes,
    bothLedgers: [...window].filter((sha) => lane1.has(sha) && seen.has(sha)).length,
    accountedFor: inLane1Only + inLanes + autoParkedCount,
    autoParked,
    uncovered,
    duplicated,
    outsideWindow,
    dangling,
    misfiled,
    parkedWithoutReason,
    contract,
    weakTies,
    needsRouting,
    checkCount,
    commitCoveringCheckCount,
    checksCoveringNoCommit,
  };
}

export function laneFindings(plan) {
  const out = [];
  if (plan.uncovered.length) {
    out.push(`lane-coverage: ${plan.uncovered.length} commit(s) on origin/main since ${COVERAGE_WINDOW} are covered by NO lane: ` +
      `${plan.uncovered.slice(0, 15).join(", ")}${plan.uncovered.length > 15 ? ` +${plan.uncovered.length - 15} more` : ""} — ${LANE_OPERATOR_HINT}`);
  }
  for (const d of plan.duplicated.slice(0, 15)) {
    out.push(`lane-coverage: ${d.sha} appears in more than one lane ledger (${d.files.join(" and ")}) — the exactly-once count is broken`);
  }
  for (const d of plan.dangling.slice(0, 15)) {
    out.push(`lane-coverage: ${d.sha} in ${d.file} points at check ${d.checkId ?? "<none>"}, which lane-checks.json does not define`);
  }
  for (const m of plan.misfiled.slice(0, 15)) {
    out.push(`lane-coverage: ${m.sha} sits in ${m.file} but its row says lane ${m.lane}`);
  }
  for (const p of plan.parkedWithoutReason.slice(0, 15)) {
    out.push(`lane-coverage: ${p.sha} in ${p.file} is parked with no reason — parked work is never reported green, but it must say why`);
  }
  if (plan.outsideWindow.length) {
    out.push(`lane-coverage: ${plan.outsideWindow.length} ledger row(s) name a commit that is not on origin/main in the window ` +
      `(${plan.outsideWindow.slice(0, 10).join(", ")}) — history was rewritten, or the row is a typo`);
  }
  out.push(...plan.contract);
  return out;
}

export function summarizeLanePlan(plan) {
  const weak = Object.values(plan.weakTies).reduce((n, w) => n + w.path, 0);
  const verdict = plan.uncovered.length ? `${plan.uncovered.length} UNCOVERED` : "none uncovered";
  const auto = plan.autoParked.length ? `, ${plan.autoParked.length} auto-parked as this automation's own bookkeeping` : "";
  return `lane-coverage: ${plan.accountedFor}/${plan.windowSize} commit(s) since ${COVERAGE_WINDOW} accounted for exactly once ` +
    `(${plan.lane1Covered} lane 1 only, ${plan.laneCovered} lanes 2-5 and parked, of which ${plan.bothLedgers} are also in lane 1's ledger${auto}), ` +
    `${verdict}, ${plan.commitCoveringCheckCount} of ${plan.checkCount} check(s) cover a commit, ${plan.needsRouting} awaiting routing, ` +
    `${weak} tied to their check by file path alone`;
}

export function describeLanePlan(plan) {
  const lines = [];
  for (const [lane, w] of Object.entries(plan.weakTies)) {
    if (!w.path) continue;
    lines.push(`  ${lane}: ${w["subject+path"]} subject+path, ${w.subject} subject-only, ${w.path} path-only (weakest tie)`);
  }
  return lines.join("\n");
}

export function newNeedsLaneRow(commit) {
  return {
    sha: String(commit.sha).slice(0, 9),
    date: commit.date,
    subject: commit.subject,
    lane: LANE_NEEDS_ROUTING,
    checkId: null,
    matchStrength: null,
    reason: "Landed on origin/main after the last classification pass and has not been routed to a lane yet. " +
      "Route it to lane 2, 3, 4 or 5 with a concrete check, or park it in not-automatable.jsonl with a reason. " +
      "It is counted as accounted-for and is NOT counted as covered by any check.",
    source: "sync-coverage",
  };
}

export const LANE_OPERATOR_HINT = "run `node tools/dashboard-automation/sync-coverage.mjs --write`, " +
  "then route each new needs-lane row in tools/dashboard-automation/commit-classification/needs-lane.jsonl " +
  "to a check in tools/dashboard-automation/lane-checks.json (or park it with a reason)";

export function laneCoverageSelfTest() {
  // the read-only contract is the thing that must never regress
  assert(sqlReadOnlyFindings("SELECT 1 FROM public.goats LIMIT 5", "t").length === 0, "a plain SELECT with a LIMIT must pass");
  assert(sqlReadOnlyFindings("DELETE FROM public.goats LIMIT 5", "t").length > 0, "a DELETE must be rejected");
  assert(sqlReadOnlyFindings("SELECT 1 LIMIT 1; DROP TABLE goats", "t").length > 0, "a second statement must be rejected");
  assert(sqlReadOnlyFindings("SELECT 1 FROM public.goats", "t").length > 0, "a missing LIMIT must be rejected");
  assert(sqlReadOnlyFindings("SELECT 'delete me' AS note FROM public.goats LIMIT 1", "t").length === 0,
    "a write word inside a string literal is not a write");
  assert(failureSentenceFindings("The herd total on the Counts page does not add up.", "t").length === 0,
    "plain English must pass");
  assert(failureSentenceFindings("SELECT * FROM public.goats returned rows", "t").length > 0, "SQL must be rejected");
  assert(failureSentenceFindings("lane2.herd-total-reconciles failed", "t").length > 0, "a check code must be rejected");

  // exactly-once, uncovered, dangling
  const plan = planLaneCoverage({
    windowShas: ["aaaaaaaa1", "bbbbbbbb2", "ccccccnew"],
    lane1Rows: [{ sha: "aaaaaaaa1" }],
    laneRows: [
      { sha: "bbbbbbbb2", lane: "lane2", checkId: "lane2.known", matchStrength: "path", _file: "lane2.jsonl", _lane: "lane2" },
      { sha: "bbbbbbbb2", lane: "lane3", checkId: "lane2.known", matchStrength: "subject", _file: "lane3.jsonl", _lane: "lane3" },
      { sha: "ddddddddd", lane: "lane4", checkId: "lane4.gone", matchStrength: "subject", _file: "lane4.jsonl", _lane: "lane4" },
    ],
    laneChecks: { lanes: { lane2: { checks: [{ id: "lane2.known", failureSentence: "The pen list is wrong.", sql: "SELECT 1 FROM public.goats LIMIT 1" }] } } },
  });
  assert.deepEqual(plan.uncovered, ["ccccccnew"], "a commit no lane names must be reported uncovered");
  assert.equal(plan.duplicated.length, 1, "a sha in two lane ledgers must break the exactly-once count");
  assert.equal(plan.dangling.length, 1, "a checkId with no check behind it must be reported");
  assert.equal(plan.outsideWindow.length, 1, "a ledger row naming a commit outside the window must be reported");
  assert.equal(plan.weakTies.lane2.path, 1, "path-only ties must stay visible to builders");
  assert(laneFindings(plan).length >= 4, "each of those must produce a finding");

  // a clean slate produces no findings
  const clean = planLaneCoverage({
    windowShas: ["aaaaaaaa1", "bbbbbbbb2"],
    lane1Rows: [{ sha: "aaaaaaaa1" }],
    laneRows: [{ sha: "bbbbbbbb2", lane: "not-automatable", reason: "Documentation only.", _file: "not-automatable.jsonl", _lane: "not-automatable" }],
    laneChecks: { lanes: {} },
  });
  assert.equal(laneFindings(clean).length, 0, "a fully accounted-for window must pass");
  assert.equal(clean.accountedFor, clean.windowSize, "accounted-for must be the union, never the sum");
  assert.equal(clean.accountedFor + clean.uncovered.length, clean.windowSize, "the arithmetic must close");
  return true;
}

// ---------------------------------------------------------------- entrypoint

export function loadState(path = statePath) {
  if (!existsSync(path)) return null;
  return JSON.parse(readFileSync(path, "utf8"));
}

export async function runSync({ mode = "check", ref = "origin/main", port = null } = {}) {
  const ledgerRows = readLedger();
  const assertions = JSON.parse(readFileSync(assertionsPath, "utf8"));
  const gitPort = port ?? defaultGitPort(ref);
  let state = loadState();
  if (!state) {
    const seeded = seedState(ledgerRows, gitPort.revListOrder());
    if (!seeded) throw new Error("cannot seed coverage-state.json: no ledger sha is reachable from " + ref);
    state = { lastSyncedSha: seeded, lastSyncedAt: null, ref, note: "seeded from newest sha in tools/dashboard-automation/commit-classification" };
    if (mode === "write") writeFileSync(statePath, `${JSON.stringify(state, null, 2)}\n`);
  }
  const plan = planSync({ state, ledgerRows, assertions, git: gitPort });

  // The lane half is independent of lane 1's incremental state: it re-derives the whole window
  // every run, because its promise is about the whole window.
  let windowCommits = gitPort.commitsInWindow ? gitPort.commitsInWindow() : [];
  const { rows: laneRows } = readLaneLedgers();
  const laneChecks = readLaneChecks();
  let lanePlan = planLaneCoverage({ windowCommits, lane1Rows: ledgerRows, laneRows, laneChecks });

  // Second pass, only for the commits nothing has claimed yet: fetch what they touched so a
  // commit that edits nothing but this automation's own ledger can be auto-parked instead of
  // holding the guard red for ever.
  if (lanePlan.uncovered.length && gitPort.filesFor) {
    const detailed = new Map(gitPort.filesFor(lanePlan.uncovered).map((c) => [String(c.sha).slice(0, 9), c]));
    const enriched = windowCommits.map((c) => detailed.get(String(c.sha).slice(0, 9)) ?? c);
    lanePlan = planLaneCoverage({ windowCommits: enriched, lane1Rows: ledgerRows, laneRows, laneChecks });
    windowCommits = enriched;
  }

  if (mode !== "write") return { plan, lanePlan, state, wrote: false };

  const applied = applyPlan({ plan, assertions, ledgerRows });
  if (applied.appended.length) {
    const file = ledgerFileForAppend();
    const prior = existsSync(file) ? readFileSync(file, "utf8").replace(/\n*$/, "\n") : "";
    writeFileSync(file, prior + applied.appended.map((r) => JSON.stringify(r)).join("\n") + "\n");
  }
  if (plan.newCommits.length || plan.stale.length) {
    writeFileSync(assertionsPath, `${JSON.stringify(applied.assertions, null, 2)}\n`);
  }
  writeFileSync(statePath, `${JSON.stringify({ ...state, lastSyncedSha: applied.lastSyncedSha, lastSyncedAt: new Date().toISOString(), ref }, null, 2)}\n`);

  const bySha = new Map(windowCommits.map((c) => [String(c.sha).slice(0, 9), c]));
  const appendRows = (lane, rows) => {
    if (!rows.length) return;
    const file = join(ledgerDir, LANE_LEDGER_FILES[lane]);
    const prior = existsSync(file) ? readFileSync(file, "utf8").replace(/\n*$/, "\n") : "";
    writeFileSync(file, prior + rows.map((r) => JSON.stringify(r)).join("\n") + "\n");
  };
  const needsRouting = lanePlan.uncovered.map((sha) => newNeedsLaneRow(bySha.get(sha) ?? { sha, date: "", subject: "" }));
  const autoParked = lanePlan.autoParked.map((sha) => {
    const c = bySha.get(sha) ?? { sha, date: "", subject: "" };
    return { sha: String(c.sha).slice(0, 9), date: c.date, subject: c.subject, lane: "not-automatable", reason: AUTO_PARK_REASON, source: "sync-coverage" };
  });
  appendRows(LANE_NEEDS_ROUTING, needsRouting);
  appendRows("not-automatable", autoParked);
  lanePlan.parkedNow = needsRouting.length + autoParked.length;
  return { plan, lanePlan, state, wrote: true };
}

export const OPERATOR_HINT = "run `node tools/dashboard-automation/sync-coverage.mjs --write`, then fill in the new needs-assertion/needs-review entries in tools/dashboard-automation/feature-assertions.json";

// Compare real paths: on macOS a temp dir is a symlink, and argv[1] would not match otherwise.
const realOrNull = (p) => { try { return realpathSync(p); } catch { return p; } };
if (process.argv[1] && realOrNull(resolve(process.argv[1])) === realOrNull(fileURLToPath(import.meta.url))) {
  const argv = process.argv.slice(2);
  if (argv.includes("--self-test")) {
    laneCoverageSelfTest();
    console.log("coverage-sync self-test: PASS (read-only SQL contract, plain-English failure sentences, exactly-once lane accounting)");
    process.exit(0);
  }
  const mode = argv.includes("--write") ? "write" : "check";
  const refArg = argv.indexOf("--ref");
  const ref = refArg > -1 ? argv[refArg + 1] : (process.env.GOATOS_SYNC_REF || "origin/main");
  const { plan, lanePlan, wrote } = await runSync({ mode, ref });
  console.log(summarizePlan(plan));
  console.log(summarizeLanePlan(lanePlan));
  const laneProblems = laneFindings(lanePlan);
  const weak = describeLanePlan(lanePlan);
  if (weak) console.log(`coverage-sync: how strongly each commit is tied to its check\n${weak}`);
  if (plan.newCommits.length || plan.stale.length || laneProblems.length) {
    if (plan.newCommits.length || plan.stale.length) console.log(describePlan(plan));
    if (laneProblems.length) console.log(laneProblems.join("\n"));
    if (wrote) {
      console.log(`coverage-sync: wrote ${plan.newCommits.length} entr(ies), flipped ${plan.stale.length} to needs-review` +
        (lanePlan.parkedNow ? `, parked ${lanePlan.parkedNow} commit(s) as needs-lane` : ""));
      process.exit(0);
    }
    console.error(`FAIL coverage-sync is stale — ${OPERATOR_HINT}`);
    if (laneProblems.length) console.error(`FAIL lane-coverage — ${LANE_OPERATOR_HINT}`);
    process.exit(1);
  }
  console.log("coverage-sync: PASS (every commit on origin/main since " + COVERAGE_WINDOW +
    " is accounted for by a lane or parked with a reason, and no assertion has drifted)");
}
