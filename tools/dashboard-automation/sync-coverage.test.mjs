// node --test tools/dashboard-automation/sync-coverage.test.mjs
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import {
  EXECUTED_STATUSES, PARKED_STATUSES, applyPlan, cleanTitle, cssTokens, describePlan, evidenceQuotes,
  guessKind, guessRoute, isWebSurfaceFile, newAssertionEntry, newLedgerRow, parseLog, planSync,
  seedState, selectorsFor, summarizePlan, tokenIsStale,
} from "./sync-coverage.mjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

// A fake origin/main: whatever the test says has landed, and whatever source still exists.
function fakeGit({ commits = [], source = "", lastTouchedBy = () => "deadbeef1", order = [] } = {}) {
  return {
    ref: "origin/main",
    revListOrder: () => order,
    commitsSince: () => commits,
    readSources: () => source,
    lastTouchedBy,
  };
}

const baseAssertion = {
  sha: "aaaaaaaa1",
  title: "Feed Analytics shows the daily chart",
  route: "feed-analytics",
  viewports: ["laptop", "mobile"],
  steps: [],
  expect: [{ visible: { text: "Consumption" } }, { count: { css: ".wchart svg", min: 1 } }],
  status: "assert",
  evidence: 'apps/admin-web/features/feed/feed-analytics.tsx:77 "Consumption"; apps/admin-web/features/counts/herd-analytics.tsx:315 wchart',
};

test("classifies admin-web surface files and ignores tests", () => {
  assert.equal(isWebSurfaceFile("apps/admin-web/features/feed/feed-analytics.tsx"), true);
  assert.equal(isWebSurfaceFile("apps/admin-web/app/(admin)/weighing/page.tsx"), true);
  assert.equal(isWebSurfaceFile("apps/admin-web/components/a.css"), true);
  assert.equal(isWebSurfaceFile("apps/admin-web/features/feed/feed-analytics.test.mjs"), false);
  assert.equal(isWebSurfaceFile("apps/admin-web/features/feed/feed.test.tsx"), false);
  assert.equal(isWebSurfaceFile("apps/admin-web/scripts/lib/x.mjs"), false);
  assert.equal(isWebSurfaceFile("backend/internal/adminui/app/service.go"), false);
});

test("guesses kind, title and route without inventing one", () => {
  assert.equal(guessKind("fix(feed): thing"), "bugfix");
  assert.equal(guessKind("feat: thing"), "feature");
  assert.equal(guessKind("chore(deps): bump"), "chore");
  assert.equal(guessKind("random subject"), "unknown");
  assert.equal(cleanTitle("fix(feed): the loads table agrees with the card (#812)"), "the loads table agrees with the card");
  assert.equal(guessRoute(["apps/admin-web/app/(admin)/weighing/page.tsx"]), "weighing");
  assert.equal(guessRoute(["apps/admin-web/app/(admin)/feed/analytics/page.tsx"]), "feed-analytics");
  assert.equal(guessRoute(["apps/admin-web/features/feed/feed-analytics.tsx"], new Set(["feed-analytics"])), "feed-analytics");
  assert.equal(guessRoute(["apps/admin-web/features/feed/unknown-thing.tsx"], new Set(["feed-analytics"])), null);
});

test("extracts only source-backed selectors", () => {
  assert.deepEqual(cssTokens('.wchart svg'), [{ kind: "class", value: "wchart" }]);
  assert.deepEqual(cssTokens('[data-testid="feed-tabbar"]'), [{ kind: "testid", value: "feed-tabbar" }]);
  assert.deepEqual(cssTokens("[data-tip]"), [{ kind: "attr", value: "data-tip" }]);
  const tokens = selectorsFor(baseAssertion);
  assert.ok(tokens.some((t) => t.kind === "class" && t.value === "wchart"));
  assert.ok(tokens.some((t) => t.kind === "text" && t.value === "Consumption"));
  assert.ok(evidenceQuotes(baseAssertion).has("consumption"));
});

test("copy strings that are data values are never called stale", () => {
  const hay = "const label = 'Consumption'";
  const low = hay.toLowerCase();
  assert.equal(tokenIsStale({ kind: "text", value: "Consumption" }, hay, low), false);
  assert.equal(tokenIsStale({ kind: "text", value: "Gone" }, hay, low), true);
  // Short and numeric-looking copy is skipped: those are rendered, never literal in source.
  assert.equal(tokenIsStale({ kind: "text", value: "ADG" }, hay, low), false);
  assert.equal(tokenIsStale({ kind: "text", value: "12 pending" }, hay, low), false);
});

test("a new user-visible commit becomes a needs-assertion entry, never a guessed selector", () => {
  const plan = planSync({
    state: { lastSyncedSha: "f30769625" },
    ledgerRows: [{ sha: "f30769625" }],
    assertions: [baseAssertion],
    git: fakeGit({
      source: 'Consumption wchart apps/admin-web/features/feed/feed-analytics.tsx herd-analytics.tsx',
      commits: [{ sha: "bbbbbbbb2", date: "2026-09-23", subject: "feat(weighing): shed weights table paginates", files: ["apps/admin-web/app/(admin)/weighing/page.tsx"] }],
    }),
  });
  assert.equal(plan.newCommits.length, 1);
  assert.equal(plan.stale.length, 0);
  const entry = newAssertionEntry(plan.newCommits[0]);
  assert.equal(entry.status, "needs-assertion");
  assert.equal(entry.route, "weighing");
  assert.equal(entry.title, "shed weights table paginates");
  assert.deepEqual(entry.steps, []);
  assert.deepEqual(entry.expect, []);
  assert.ok(!EXECUTED_STATUSES.has(entry.status), "needs-assertion must not be executed by the runner");
  assert.ok(PARKED_STATUSES.has(entry.status));
  const row = newLedgerRow(plan.newCommits[0]);
  assert.equal(row.sha, "bbbbbbbb2");
  assert.equal(row.userVisible, true);
  assert.equal(row.kind, "feature");
});

test("a commit touching only an asserted file is picked up even without new surface files", () => {
  const plan = planSync({
    state: { lastSyncedSha: "f30769625" },
    ledgerRows: [],
    assertions: [baseAssertion],
    git: fakeGit({
      source: "Consumption wchart",
      commits: [{ sha: "cccccccc3", date: "2026-09-23", subject: "refactor: move helper", files: ["apps/admin-web/features/feed/feed-analytics.test.mjs", "apps/admin-web/features/feed/feed-analytics.tsx"] }],
    }),
  });
  assert.equal(plan.newCommits.length, 1);
  // Non-surface-only commits are still recorded, but not marked user-visible in the ledger.
  const onlyEvidence = planSync({
    state: { lastSyncedSha: "x" },
    ledgerRows: [],
    assertions: [{ ...baseAssertion, evidence: 'backend/internal/adminui/app/service.go:1 "Consumption"' }],
    git: fakeGit({ source: "Consumption", commits: [{ sha: "dddddddd4", date: "2026-09-23", subject: "fix(api): copy", files: ["backend/internal/adminui/app/service.go"] }] }),
  });
  assert.equal(onlyEvidence.newCommits.length, 1);
  assert.equal(onlyEvidence.newCommits[0].reason, "modifies a file an existing assertion relies on");
  assert.equal(newLedgerRow(onlyEvidence.newCommits[0]).userVisible, false);
});

test("a removed selector flips the assertion to needs-review and names the culprit", () => {
  const plan = planSync({
    state: { lastSyncedSha: "f30769625" },
    ledgerRows: [],
    assertions: [baseAssertion],
    git: fakeGit({ source: 'Consumption only, the chart class is gone', lastTouchedBy: () => "9999999aa" }),
  });
  assert.equal(plan.stale.length, 1);
  assert.match(plan.stale[0].reason, /selector wchart no longer in source \(changed by 9999999aa\)/);
  const applied = applyPlan({ plan, assertions: [baseAssertion], ledgerRows: [] });
  assert.equal(applied.assertions[0].status, "needs-review");
  assert.match(applied.assertions[0].reason, /no longer in source/);
  assert.ok(!EXECUTED_STATUSES.has(applied.assertions[0].status), "needs-review must not be executed by the runner");
});

test("assertions the runner does not execute are never checked for drift", () => {
  for (const status of ["superseded", "not-read-only", "screenshot-only", "rejected", "needs-assertion", "needs-review"]) {
    const plan = planSync({
      state: { lastSyncedSha: "x" },
      ledgerRows: [],
      assertions: [{ ...baseAssertion, status }],
      git: fakeGit({ source: "" }),
    });
    assert.equal(plan.stale.length, 0, `${status} should not be drift-checked`);
  }
});

test("applyPlan bumps state to the newest commit and appends ledger rows", () => {
  const commits = [
    { sha: "eeeeeeee5", date: "2026-09-23", subject: "feat(a): newest", files: ["apps/admin-web/features/a/a.tsx"] },
    { sha: "fffffff66", date: "2026-09-22", subject: "feat(b): older", files: ["apps/admin-web/features/b/b.tsx"] },
  ];
  const plan = planSync({ state: { lastSyncedSha: "old" }, ledgerRows: [], assertions: [], git: fakeGit({ commits, source: "" }) });
  const applied = applyPlan({ plan, assertions: [], ledgerRows: [] });
  assert.equal(applied.lastSyncedSha, "eeeeeeee5", "git log is newest-first; state must follow the tip");
  assert.equal(applied.appended.length, 2);
  assert.equal(applied.assertions.length, 2);
});

test("already-covered commits are not re-added", () => {
  const commits = [{ sha: "aaaaaaaa1", date: "2026-09-23", subject: "feat: already known", files: ["apps/admin-web/features/feed/feed-analytics.tsx"] }];
  const plan = planSync({
    state: { lastSyncedSha: "old" },
    ledgerRows: [{ sha: "aaaaaaaa1" }],
    assertions: [baseAssertion],
    git: fakeGit({ commits, source: "Consumption wchart" }),
  });
  assert.equal(plan.newCommits.length, 0);
});

test("seedState picks the newest ledger sha reachable from origin/main", () => {
  assert.equal(seedState([{ sha: "bbb" }, { sha: "ccc" }], ["aaa", "ccc", "bbb"]), "ccc");
  assert.equal(seedState([{ sha: "zzz" }], ["aaa", "bbb"]), null);
});

test("parseLog reads sha/date/subject/files out of one git log pass", () => {
  const out = " abc1234562026-09-23feat: thing\napps/admin-web/a.tsx\napps/admin-web/b.tsx\n def1234562026-09-22fix: other\nx.ts\n";
  const parsed = parseLog(out);
  assert.equal(parsed.length, 2);
  assert.deepEqual(parsed[0], { sha: "abc123456", date: "2026-09-23", subject: "feat: thing", files: ["apps/admin-web/a.tsx", "apps/admin-web/b.tsx"] });
});

test("summary and description are readable plain English", () => {
  const plan = { newCommits: [{ sha: "a", date: "d", subject: "feat: x", reason: "new user-visible admin-web work" }], stale: [], since: "f30769625" };
  assert.match(summarizePlan(plan), /1 new commit\(s\) need smoke coverage/);
  assert.match(describePlan(plan), /new commit\(s\) need smoke coverage/);
});

test("--check exits non-zero while stale, and --write then makes --check clean", () => {
  const dir = mkdtempSync(join(tmpdir(), "sync-coverage-"));
  try {
    // A throwaway git repo standing in for origin/main.
    const run = (...args) => {
      const r = spawnSync(args[0], args.slice(1), { cwd: dir, encoding: "utf8" });
      if (r.status !== 0) throw new Error(`${args.join(" ")}: ${r.stderr}`);
      return r.stdout;
    };
    const tool = join(dir, "tools/dashboard-automation");
    mkdirSync(join(tool, "commit-classification"), { recursive: true });
    mkdirSync(join(dir, "apps/admin-web/features/demo"), { recursive: true });
    writeFileSync(join(dir, "apps/admin-web/features/demo/demo.tsx"), 'export const x = <div className="demo">Hello Demo</div>;\n');
    writeFileSync(join(tool, "feature-assertions.json"), "[]\n");
    writeFileSync(join(tool, "commit-classification/seed.jsonl"), "");
    // Reuse the real tool from the real repo, pointed at this fixture via cwd-independent copy.
    writeFileSync(join(tool, "sync-coverage.mjs"), readFileSync(join(repoRoot, "tools/dashboard-automation/sync-coverage.mjs"), "utf8"));
    run("git", "init", "-q", "-b", "main");
    run("git", "config", "user.email", "t@example.com");
    run("git", "config", "user.name", "t");
    run("git", "add", "-A");
    run("git", "commit", "-qm", "chore: seed");
    const seedSha = run("git", "rev-parse", "--short=9", "HEAD").trim();
    writeFileSync(join(tool, "commit-classification/seed.jsonl"), `${JSON.stringify({ sha: seedSha, date: "2026-09-23", subject: "chore: seed", kind: "chore", userVisible: false })}\n`);
    writeFileSync(join(tool, "coverage-state.json"), `${JSON.stringify({ lastSyncedSha: seedSha, ref: "main" }, null, 2)}\n`);
    run("git", "add", "-A");
    run("git", "commit", "-qm", "chore: state");
    // Now a new user-visible commit lands.
    writeFileSync(join(dir, "apps/admin-web/features/demo/demo.tsx"), 'export const x = <div className="demo">Hello Demo</div>;\nexport const y = 1;\n');
    run("git", "add", "-A");
    run("git", "commit", "-qm", "feat(demo): a brand new panel");
    const node = (...argv) => spawnSync(process.execPath, [join(tool, "sync-coverage.mjs"), ...argv], { cwd: dir, encoding: "utf8", env: { ...process.env, GOATOS_SYNC_REF: "main" } });

    const stale = node("--check");
    assert.equal(stale.status, 1, stale.stdout + stale.stderr);
    assert.match(stale.stdout + stale.stderr, /new commit\(s\) need smoke coverage/);
    assert.match(stale.stderr, /--write/);

    const wrote = node("--write");
    assert.equal(wrote.status, 0, wrote.stdout + wrote.stderr);
    const entries = JSON.parse(readFileSync(join(tool, "feature-assertions.json"), "utf8"));
    assert.ok(entries.some((e) => e.status === "needs-assertion" && /brand new panel/.test(e.title)));
    assert.ok(existsSync(join(tool, "commit-classification/sync.jsonl")));
    const state = JSON.parse(readFileSync(join(tool, "coverage-state.json"), "utf8"));
    assert.notEqual(state.lastSyncedSha, seedSha, "state file must be bumped past the synced commit");
    assert.ok(state.lastSyncedAt, "state file must record when it was synced");

    run("git", "add", "-A");
    run("git", "commit", "-qm", "chore: sync coverage");
    const clean = node("--check");
    assert.equal(clean.status, 0, clean.stdout + clean.stderr);
    assert.match(clean.stdout, /coverage-sync: PASS/);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
