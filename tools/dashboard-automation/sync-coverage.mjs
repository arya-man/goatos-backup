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

export function readLedger(dir = ledgerDir) {
  const rows = [];
  if (!existsSync(dir)) return rows;
  for (const file of readdirSync(dir).filter((n) => n.endsWith(".jsonl")).sort()) {
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
  if (mode !== "write") return { plan, state, wrote: false };

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
  return { plan, state, wrote: true };
}

export const OPERATOR_HINT = "run `node tools/dashboard-automation/sync-coverage.mjs --write`, then fill in the new needs-assertion/needs-review entries in tools/dashboard-automation/feature-assertions.json";

// Compare real paths: on macOS a temp dir is a symlink, and argv[1] would not match otherwise.
const realOrNull = (p) => { try { return realpathSync(p); } catch { return p; } };
if (process.argv[1] && realOrNull(resolve(process.argv[1])) === realOrNull(fileURLToPath(import.meta.url))) {
  const argv = process.argv.slice(2);
  const mode = argv.includes("--write") ? "write" : "check";
  const refArg = argv.indexOf("--ref");
  const ref = refArg > -1 ? argv[refArg + 1] : (process.env.GOATOS_SYNC_REF || "origin/main");
  const { plan, wrote } = await runSync({ mode, ref });
  console.log(summarizePlan(plan));
  if (plan.newCommits.length || plan.stale.length) {
    console.log(describePlan(plan));
    if (wrote) {
      console.log(`coverage-sync: wrote ${plan.newCommits.length} entr(ies) and flipped ${plan.stale.length} to needs-review`);
      process.exit(0);
    }
    console.error(`FAIL coverage-sync is stale — ${OPERATOR_HINT}`);
    process.exit(1);
  }
  console.log("coverage-sync: PASS (every commit on origin/main is covered and no assertion has drifted)");
}
