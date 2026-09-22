#!/usr/bin/env node
// node --test tools/dashboard-automation/check-data-sanity.test.mjs
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  assertSelectOnly,
  capAndRedactRows,
  layerSentence,
  loadCatalogue,
  readOnlyProofSql,
  cappedSql,
  readOnlySql,
  stripSqlNoise
} from "./check-data-sanity.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const runner = path.join(here, "check-data-sanity.mjs");
const catalogue = loadCatalogue();

test("the SELECT-only guard refuses every write verb", () => {
  const forbidden = {
    insert: "insert into goats (a) values (1) limit 1",
    update: "update goats set sex = 'f' limit 1",
    delete: "delete from goats limit 1",
    drop: "drop table goats limit 1",
    alter: "alter table goats add column x int limit 1",
    truncate: "truncate goats limit 1",
    grant: "grant select on goats to public limit 1",
    revoke: "revoke select on goats from public limit 1",
    create: "create table t as select 1 limit 1",
    copy: "copy goats to stdout limit 1",
    call: "call rebuild_projection() limit 1",
    do: "do $$ begin end $$ limit 1",
    merge: "merge into goats using x on true limit 1",
    into: "select 1 into scratch from goats limit 1",
    set: "set default_transaction_read_only = off limit 1",
    vacuum: "vacuum goats limit 1"
  };
  for (const [verb, sql] of Object.entries(forbidden)) {
    assert.throws(() => assertSelectOnly(sql, verb), new RegExp(verb, "i"), `guard accepted ${verb}`);
  }
});

test("the SELECT-only guard refuses chained statements, locks, meta-commands and uncapped reads", () => {
  assert.throws(() => assertSelectOnly("select 1 limit 1; drop table goats", "chain"), /single statement/);
  assert.throws(() => assertSelectOnly("select * from goats for share limit 1", "lock"), /row locks/);
  assert.throws(() => assertSelectOnly("select * from goats for update limit 1", "lock"), /read-only|row locks/);
  assert.throws(() => assertSelectOnly("\\copy goats to 'out.csv' limit 1", "meta"), /meta-command/);
  assert.throws(() => assertSelectOnly("select 1", "uncapped"), /LIMIT/);
  assert.throws(() => assertSelectOnly("", "empty"), /empty/);
  assert.throws(() => assertSelectOnly("explain select 1 limit 1", "explain"), /SELECT or WITH/);
});

test("the SELECT-only guard accepts real reads and is not fooled by literals or column names", () => {
  assert.ok(assertSelectOnly("select 1 as ok limit 1", "plain"));
  assert.ok(assertSelectOnly("with t as (select 1 as n) select n from t limit 1", "cte"));
  // 'created_at'/'updated_at'/'merged_into_goat_id' must not read as CREATE/UPDATE/MERGE/INTO.
  assert.ok(assertSelectOnly("select created_at, updated_at from goats where merged_into_goat_id is null limit 1", "columns"));
  // A write verb inside a string literal is data, not a statement.
  assert.ok(assertSelectOnly("select status from goats where status = 'do not delete' limit 1", "literal"));
  assert.equal(stripSqlNoise("select 'drop table x' from t").includes("drop"), false);
});

test("the row cap is applied by the runner, so a LIMIT inside a CTE cannot evade it", () => {
  // The bypass, proved against the replica: this returns every animal even though it contains
  // a LIMIT, because the LIMIT only caps the CTE. The text guard accepts it; the runner caps it.
  const cteBypass = "with c as (select 1 limit 1) select g.goat_id from goats g, c";
  assert.ok(assertSelectOnly(cteBypass, "cte"), "the text guard sees a LIMIT here, which is exactly the problem");
  const capped = cappedSql(cteBypass, 500);
  assert.match(capped, /^select \* from \(/, "the runner must wrap the check, not read it");
  assert.match(capped, /\)\s*_capped\s+limit\s+500$/, "the runner's cap must be the outermost limit");
  assert.ok(capped.includes(cteBypass), "the check's own text must be preserved inside the wrapper");
  // A check with no LIMIT at all is still capped, so the cap never depends on the text guard.
  assert.match(cappedSql("select * from goats", 25), /\)\s*_capped\s+limit\s+25$/);
  assert.match(cappedSql("select 1", 0), /limit 1$/, "a nonsense cap must still be a cap");
  // The wrapper is string interpolation, so a check must not be able to close it early or
  // comment it out. Each of these renders valid PostgreSQL in which the runner's cap is either
  // inside a comment or outside the surviving statement, so the guard has to refuse them.
  const escapes = {
    "closes the wrapper and comments the cap out": "select goat_id from goats) _capped limit 999999 --",
    "closes the wrapper and block-comments the cap out": "select goat_id from goats) _capped limit 999999 /*",
    "leaves a parenthesis open so the suffix is swallowed": "select goat_id from goats where (1=1 limit 5",
    "trails a line comment over the cap": "select goat_id from goats limit 5 -- trailing",
    "trails a block comment over the cap": "select goat_id from goats limit 5 /* trailing */"
  };
  for (const [why, sql] of Object.entries(escapes)) {
    assert.throws(() => assertSelectOnly(sql, "escape"), /comment|parenthesis/, `the guard accepted SQL that ${why}`);
  }
  // A ")" or a "--" inside a value is data, not structure, and must not be refused.
  assert.ok(assertSelectOnly("select name from locations where name = 'Castro 3 (north)' limit 1", "literal-paren"));
  // Nested CTEs and unions keep the outer cap.
  assert.match(cappedSql("with a as (select 1 n), b as (select 2 n) select n from a union all select n from b limit 9", 500), /\n\) _capped limit 500$/);
  // The runner is what calls it: the cap is not something a check can opt out of.
  const source = readFileSync(runner, "utf8");
  assert.ok(/psqlRows\(databaseUrl, readOnlySql\(cappedSql\(check\.sql/.test(source), "every check must go through the runner's cap");
});

test("every statement is wrapped in a read-only transaction that is rolled back", () => {
  const wrapped = readOnlySql("select 1 limit 1", 12345);
  for (const fragment of ["begin read only", "set local default_transaction_read_only = on", "set local statement_timeout = 12345", "rollback"]) {
    assert.ok(wrapped.includes(fragment), `read-only wrapper missing ${fragment}`);
  }
  assert.ok(wrapped.trimEnd().endsWith("rollback"), "the transaction must always be rolled back");
  const proof = readOnlyProofSql();
  assert.ok(proof.includes("transaction_read_only"), "the runner must ask the session to prove it is read-only");
  assert.ok(proof.includes("begin read only"));
});

test("the runner refuses to report anything when the session cannot be proved read-only", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "data-sanity-"));
  try {
    const out = path.join(dir, "report.json");
    // 127.0.0.1:1 is closed, so the read-only proof cannot succeed.
    const result = spawnSync(process.execPath, [runner, "--out", out], {
      cwd: repo,
      encoding: "utf8",
      env: { ...process.env, GOATOS_STG_READONLY_DATABASE_URL: "postgres://nobody:nothing@127.0.0.1:1/goatos?sslmode=disable" }
    });
    assert.notEqual(result.status, 0, "an unproven read-only session must not exit green");
    const report = JSON.parse(readFileSync(out, "utf8"));
    assert.equal(report.readOnly.proven, false);
    assert.equal(report.status, "fail");
    assert.equal(report.findings.length, 0, "nothing may be reported green or red without a proven read-only session");
    assert.ok(report.parked.some((item) => /read-only/i.test(item.reason)), "the refusal must be parked with its reason");
    const text = JSON.stringify(report) + result.stdout + result.stderr;
    assert.equal(/nothing@|:nothing/.test(text), false, "the connection password must never reach the report or the logs");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a missing connection is parked with a reason, never faked green", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "data-sanity-"));
  try {
    const out = path.join(dir, "report.json");
    const env = { ...process.env };
    delete env.GOATOS_STG_READONLY_DATABASE_URL;
    const result = spawnSync(process.execPath, [runner, "--out", out], { cwd: repo, encoding: "utf8", env });
    assert.notEqual(result.status, 0);
    const report = JSON.parse(readFileSync(out, "utf8"));
    assert.equal(report.status, "parked");
    assert.ok(report.parked.length > 0);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("sample rows are capped and every value is redacted", () => {
  const rows = [
    ["alpha", "postgres://user:hunter2@db.internal/goatos"],
    ["bravo", "Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature"],
    ["charlie", "x"],
    ["delta", "y"]
  ];
  const capped = capAndRedactRows(rows, ["animal", "note"], 2, 80);
  assert.equal(capped.length, 2, "rows must be capped to the catalogue's sample size");
  assert.deepEqual(Object.keys(capped[0]), ["animal", "note"], "columns must be named, not positional");
  const text = JSON.stringify(capped);
  assert.equal(text.includes("hunter2"), false, "a password must never survive into a sample row");
  assert.ok(text.includes("[REDACTED]"));
  const long = capAndRedactRows([["k", "z".repeat(500)]], ["k", "v"], 1, 40);
  assert.ok(long[0].v.length <= 40, "a long value must be truncated before it reaches a receipt");
  assert.deepEqual(capAndRedactRows([["a"]], ["k"], 0, 80), [], "a zero cap must return no rows");
});

test("the catalogue covers the roadmap and every entry is a capped, read-only SELECT", () => {
  assert.ok(catalogue.checks.length >= 20, "the roadmap's check list must be covered");
  const names = catalogue.checks.map((check) => check.name);
  assert.equal(new Set(names).size, names.length, "check names must be unique");
  for (const required of [
    "herd_total_vs_status_breakdown", "park_total_vs_pen_sum", "animal_in_two_pens",
    "non_positive_animal_weight", "implausible_weight_jump", "feed_issued_exceeds_purchased",
    "drive_doses_exceed_pen_animals", "future_dated_animal_record", "death_before_birth",
    "capture_after_submission", "orphan_weighing_record", "orphan_sale_record", "orphan_load_record",
    "weighing_run_stuck_over_2_days", "verification_pending_over_7_days", "sales_sold_count_vs_herd_sold"
  ]) {
    assert.ok(names.includes(required), `roadmap check ${required} is missing`);
  }
  for (const check of catalogue.checks) {
    assert.ok(assertSelectOnly(check.sql, check.name));
    assert.equal(Number(check.expectRows), 0, `${check.name} must expect zero offending rows`);
    assert.ok(["high", "medium", "low"].includes(check.severity), `${check.name} has no usable severity`);
    assert.ok(check.page?.path?.startsWith("/"), `${check.name} must name the screen it shows up on`);
    assert.ok(check.page?.title, `${check.name} must name the screen in words`);
    assert.ok(Array.isArray(check.sourceCommits) && check.sourceCommits.length > 0, `${check.name} must cite the commits it comes from`);
  }
});

const SQL_KEYWORDS = new Set([
  "count", "sum", "min", "max", "round", "floor", "extract", "coalesce", "greatest", "least",
  "now", "select", "distinct", "lateral", "only", "unnest", "generate_series"
]);

test("the sentence a farm manager reads carries no SQL, no table or column name, and no check code", () => {
  // Every identifier the SQL touches, so the assertion cannot drift when a check is added.
  const identifiers = new Set();
  for (const check of catalogue.checks) {
    // Names a check invents for itself are not schema, so they are not a leak.
    const cteNames = new Set([...check.sql.matchAll(/\b([a-z_][a-z0-9_]*)\s+as\s*\(/gi)].map((m) => m[1].toLowerCase()));
    for (const match of check.sql.matchAll(/\b(?:from|join)\s+([a-z_][a-z0-9_]*)(?!\s*\()/gi)) {
      const name = match[1].toLowerCase();
      if (!SQL_KEYWORDS.has(name) && !cteNames.has(name)) identifiers.add(name);
    }
    for (const match of check.sql.matchAll(/\b([a-z][a-z0-9]*_[a-z0-9_]+)\b/gi)) {
      const name = match[1].toLowerCase();
      if (!cteNames.has(name)) identifiers.add(name);
    }
  }
  assert.ok(identifiers.has("goats"), "the identifier sweep must actually see the schema");
  assert.ok(identifiers.has("lifecycle_status"));

  // Words that only ever come from SQL. "from"/"where"/"limit" are ordinary English and are
  // covered instead by the syntax and identifier assertions below.
  const sqlWords = ["select", "group by", "order by", "is distinct from", "coalesce", "is null", "inner join", "left join"];
  for (const check of catalogue.checks) {
    for (const [field, sentence] of [["humanFailure", check.humanFailure], ["countUnit", check.countUnit], ["question", check.question]]) {
      assert.ok(sentence && sentence.length > 0, `${check.name}: ${field} is empty`);
      const lower = ` ${sentence.toLowerCase()} `;
      assert.equal(lower.includes(check.name.toLowerCase()), false, `${check.name}: ${field} leaks the check code`);
      assert.equal(/[a-z0-9]_[a-z0-9]/i.test(sentence), false, `${check.name}: ${field} leaks a snake_case identifier`);
      for (const word of sqlWords) {
        assert.equal(lower.includes(` ${word} `), false, `${check.name}: ${field} leaks the SQL word "${word}"`);
      }
      // No SQL syntax at all: no casts, no function calls, no comparisons, no wildcards.
      for (const syntax of ["::", "(*", "count(", "sum(", "select ", "--", "/*", " = ", " <> ", "*/"]) {
        assert.equal(sentence.includes(syntax), false, `${check.name}: ${field} leaks SQL syntax "${syntax}"`);
      }
      for (const identifier of identifiers) {
        if (identifier.length < 5) continue;
        assert.equal(
          new RegExp(`\\b${identifier}\\b`).test(lower),
          false,
          `${check.name}: ${field} leaks the schema identifier "${identifier}"`
        );
      }
    }
    assert.ok(/[.?]$/.test(check.humanFailure.trim()), `${check.name}: the sentence must read as a sentence`);
    assert.ok(check.humanFailure.length > 40, `${check.name}: the sentence must explain what a person will see`);
  }
});

test("the layer sentence stays plain English in every state", () => {
  assert.match(layerSentence([], []), /adds up/);
  assert.match(layerSentence([{ rowCount: 3 }], []), /does not add up/);
  assert.match(layerSentence([{ rowCount: 3 }, { rowCount: 2 }], []), /do not add up/);
  assert.match(layerSentence([], [{ name: "x" }]), /parked/);
  for (const sentence of [layerSentence([], []), layerSentence([{ rowCount: 1 }], [{ name: "x" }])]) {
    assert.equal(/[a-z0-9]_[a-z0-9]/i.test(sentence), false, "the layer sentence must not leak an identifier");
    assert.equal(/select|from |where /i.test(sentence), false, "the layer sentence must not leak SQL");
  }
});

test("--only runs one named check and an unknown name is refused", () => {
  const env = { ...process.env };
  delete env.GOATOS_STG_READONLY_DATABASE_URL;
  const dir = mkdtempSync(path.join(tmpdir(), "data-sanity-"));
  try {
    const unknown = spawnSync(process.execPath, [runner, "--only", "no_such_check", "--out", path.join(dir, "r.json")], { cwd: repo, encoding: "utf8", env });
    assert.equal(unknown.status, 2, "an unknown check name must be refused, not silently skipped");
    const bad = spawnSync(process.execPath, [runner, "--nonsense"], { cwd: repo, encoding: "utf8", env });
    assert.notEqual(bad.status, 0, "an unknown argument must be refused");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("the runner's own self-test passes", () => {
  const result = spawnSync(process.execPath, [runner, "--self-test"], { cwd: repo, encoding: "utf8" });
  assert.equal(result.status, 0, result.stderr);
});

test("every check links to a screen that actually exists in admin-web", () => {
  // A finding that links to a dead page is worse than no finding: the person clicks and gets a 404.
  const root = path.join(repo, "apps/admin-web/app/(admin)");
  const routes = new Set();
  const walk = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) walk(full);
      else if (entry.name === "page.tsx") {
        const segments = path.relative(root, dir).split(path.sep).filter(Boolean).filter((part) => !/^\(.*\)$/.test(part));
        routes.add(`/${segments.join("/")}`);
      }
    }
  };
  walk(root);
  assert.ok(routes.size > 40, "the route sweep must actually see admin-web");
  for (const check of catalogue.checks) {
    const withoutParams = check.page.path.replace(/\/\[[^\]]+\]/g, "/placeholder");
    assert.ok(
      routes.has(check.page.path) || [...routes].some((route) => route.replace(/\/\[[^\]]+\]/g, "/placeholder") === withoutParams),
      `${check.name} links to ${check.page.path}, which is not a screen in admin-web`
    );
  }
});

test("the lane's Slack rendering keeps rows out of Slack and redacts them in its own report", async () => {
  const kind = (await import("./lib/finding-kinds/data-sanity.mjs")).default;
  const dir = mkdtempSync(path.join(tmpdir(), "data-sanity-slack-"));
  try {
    writeFileSync(path.join(dir, "data-sanity.json"), JSON.stringify({
      productionUrl: "https://dashboard.mesha.sg",
      findings: [{
        name: "feed_issued_exceeds_purchased",
        question: "Has more feed been issued to the pens than was ever bought?",
        humanFailure: "More of some feeds has been sent out to the pens than was ever bought.",
        countUnit: "feeds sent out in greater quantity than was bought",
        severity: "high",
        page: { title: "Feed analytics", path: "/feed/analytics" },
        rowCount: 4,
        sampleRows: [{ feed: "Maize", note: "password=hunter2supersecret" }]
      }]
    }));
    const findings = kind.toFindings({}, dir);
    assert.equal(findings.length, 1);
    const slack = JSON.stringify(kind.renderSection(findings)) + kind.summaryText(findings) + kind.headline(findings);
    assert.ok(slack.includes("Data does not add up"), "the approved label must be the one used");
    assert.ok(slack.includes("4 feeds sent out in greater quantity than was bought"));
    assert.ok(slack.includes("https://dashboard.mesha.sg/feed/analytics"));
    for (const leak of ["Maize", "hunter2supersecret", "sampleRows", "feed_issued_exceeds_purchased", "select "]) {
      assert.equal(slack.includes(leak), false, `Slack leaked ${leak}`);
    }
    // The scrub is enforced, not advisory: a sentence carrying a table name is refused outright.
    assert.throws(() => kind.assertPlainEnglish("the goat_shed_partitions rows disagree", "t"), /table or column name/);
    assert.throws(() => kind.assertPlainEnglish("select display_id from goats limit 1", "t"), /SQL/);
    const replies = kind.renderReplies(findings);
    assert.equal(replies.length, 1, "the rows must be carried by this lane's own report, as one reply");
    const html = readFileSync(replies[0].file, "utf8");
    assert.ok(html.includes("Maize"), "the rows belong in the report");
    assert.equal(html.includes("hunter2supersecret"), false, "the report must redact a credential in a row");
    assert.deepEqual(kind.issueRules(), [], "this lane adds no route-error rules");
    // No report next to the receipt at all: nothing is contributed, so lane 1 renders untouched.
    assert.deepEqual(kind.toFindings({}, path.join(dir, "elsewhere")), []);
    assert.deepEqual(kind.renderSection([]), []);
    assert.equal(kind.summaryText([]), "");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

// Renders a real Slack message the way the runner does, in dry-run, and returns the blocks.
function renderSlack(dir, { withDataReport }) {
  const receiptFile = path.join(dir, "receipt.json");
  const receipt = {
    runId: "test", mode: "production-smoke", status: "fail", repoSha: "abc123456789",
    productionUrl: "https://dashboard.mesha.sg",
    runtimePolicy: { browserSmoke: "ran_certified", dataTrust: "not_checked_read_only_smoke" },
    layers: [{ name: "production-module-journeys", authority: "deterministic", status: "pass" }],
    artifacts: withDataReport ? [{ kind: "data-sanity-report", path: path.join(dir, "data-sanity.json") }] : [],
    blockers: [{ layer: "production-module-journeys", message: "calendar laptop C-cell-mid-token-wrap: Warmup split over 2 lines" }]
  };
  writeFileSync(receiptFile, JSON.stringify(receipt, null, 2));
  mkdirSync(path.join(dir, "module-journeys"), { recursive: true });
  writeFileSync(path.join(dir, "module-journeys", "module-journeys-receipt.json"), JSON.stringify({
    modules: [{ id: "calendar", failures: [{ route: "laptop:calendar", url: "https://dashboard.mesha.sg/calendar", error: 'calendar laptop C-cell-mid-word-wrap: "Warmup" split over 2 lines', screenshots: [] }] }]
  }));
  const result = spawnSync(process.execPath, [path.join(repo, "tools/dashboard-automation/notify-slack.mjs"), "--receipt", receiptFile], {
    cwd: repo,
    encoding: "utf8",
    env: { ...process.env, GOATOS_DASHBOARD_SLACK_DRY_RUN: "1", GOATOS_DASHBOARD_SLACK_STATE_FILE: path.join(dir, `state-${withDataReport ? "with" : "without"}.json`) }
  });
  assert.equal(result.status, 0, result.stderr);
  const start = result.stdout.indexOf("[");
  const end = result.stdout.lastIndexOf("]");
  return { text: result.stdout.slice(0, start).trim(), blocks: JSON.parse(result.stdout.slice(start, end + 1)), stdout: result.stdout };
}

test("a lane-1-only receipt renders byte-identically once this lane is registered", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "data-sanity-e2e-"));
  try {
    // No report from this lane next to the receipt, so this is exactly a lane-1 run.
    const a = renderSlack(dir, { withDataReport: false });
    const b = renderSlack(dir, { withDataReport: false });
    assert.equal(JSON.stringify(a.blocks), JSON.stringify(b.blocks));
    assert.ok(a.text.includes("visible issue"), "the lane-1 message must still be the visual-issues message");
    assert.equal(JSON.stringify(a.blocks).includes("Data does not add up"), false, "this lane must contribute nothing when it found nothing");
    assert.equal(a.blocks[0].type, "header");
    assert.ok(/What is wrong/.test(JSON.stringify(a.blocks)), "lane 1's own section must be untouched");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("a mixed receipt carries both lanes, and this lane leaks nothing into the rendered message", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "data-sanity-e2e-"));
  try {
    const lane1Only = renderSlack(dir, { withDataReport: false });
    writeFileSync(path.join(dir, "data-sanity.json"), JSON.stringify({
      productionUrl: "https://dashboard.mesha.sg",
      findings: [{
        name: "drive_scheduled_for_exited_animal",
        question: "Is any upcoming vaccination planned for an animal that has already left?",
        humanFailure: "Upcoming vaccination rounds still list animals that have already been sold or have died.",
        countUnit: "upcoming vaccinations planned for animals that have left",
        severity: "high",
        page: { title: "Vaccination live tracker", path: "/vaccination/live-tracker" },
        rowCount: 139,
        sql: "select goat_id from vaccination_drive_assignment_members where canceled_at is null limit 500",
        sampleRows: [{ animal: "SF-0481", note: "password=hunter2supersecret" }]
      }]
    }));
    const mixed = renderSlack(dir, { withDataReport: true });
    const rendered = JSON.stringify(mixed.blocks);
    for (const expected of ["Data does not add up", "Vaccination live tracker", "139 upcoming vaccinations", "already been sold or have died", "/vaccination/live-tracker"]) {
      assert.ok(rendered.includes(expected), `the data section is missing ${expected}`);
    }
    for (const leak of ["select ", "vaccination_drive_assignment_members", "canceled_at", "drive_scheduled_for_exited_animal", "hunter2supersecret", "SF-0481", "sampleRows", "data-sanity.json"]) {
      assert.equal(rendered.includes(leak), false, `the rendered message leaked ${leak}`);
    }
    // Lane 1's own blocks survive the mixed render unchanged.
    const lane1Blocks = lane1Only.blocks.map((block) => JSON.stringify(block));
    const mixedBlocks = mixed.blocks.map((block) => JSON.stringify(block));
    for (const block of lane1Blocks) {
      assert.ok(mixedBlocks.includes(block), "a lane-1 block was rewritten by this lane");
    }
    assert.equal(mixed.blocks.length, lane1Only.blocks.length + 1, "this lane must add exactly one section");
    assert.equal(mixed.blocks[0].type, "header", "the header must stay first");
    assert.equal(mixed.blocks[0].text.text, lane1Only.blocks[0].text.text, "lane 1 keeps the headline when it found issues of its own");
    // The rows are carried by this lane's own report, attached in the thread.
    assert.ok(mixed.stdout.includes("data-sanity-report.html"), "the rows must be attached as this lane's own report");
    const html = readFileSync(path.join(dir, "data-sanity-report.html"), "utf8");
    assert.ok(html.includes("SF-0481"));
    assert.equal(html.includes("hunter2supersecret"), false);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
