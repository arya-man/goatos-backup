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
  derivedChecksRequested,
  INCLUDE_DERIVED_ENV,
  layerSentence,
  loadCatalogue,
  tablesReadByCatalogue,
  readOnlyProofSql,
  cappedSql,
  readOnlySql,
  stripSqlNoise,
  summariseParked
} from "./check-data-sanity.mjs";
import { countCheckShapedRows, LANE_SHAPES, locateLaneRows } from "./lib/lane-rows.mjs";

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
    // A check must be traceable. Almost always that means the commits it came from. A handful
    // guard an invariant the roadmap names directly, which no commit introduced — those must say
    // so in words instead. What is NOT allowed is an entry that cites neither.
    const citesCommits = Array.isArray(check.sourceCommits) && check.sourceCommits.length > 0;
    const saysWhyNot = typeof check.notFromCommits === "string" && check.notFromCommits.trim().length > 20;
    assert.ok(citesCommits || saysWhyNot,
      `${check.name} must cite the commits it comes from, or say in words why no commit maps to it`);
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


// ---------------------------------------------------------------------------
// The derived-check loader.
//
// These exist because of a bug that was silent by construction: this lane looked for the miner's
// rows at the top level of lane-checks.json while the miner writes them at lanes.lane2.checks.
// Every fallback missed, the miss was indistinguishable from "there is no derived file", and 50
// checks collapsed into one parked line reading "the derived check file could not be read" — a
// sentence that reads like a handled edge case. The silence was as much the bug as the path.
// ---------------------------------------------------------------------------

const laneChecksFile = path.join(here, "lane-checks.json");
const shippedLaneChecks = JSON.parse(readFileSync(laneChecksFile, "utf8"));

function withLaneFile(contents, run) {
  const dir = mkdtempSync(path.join(tmpdir(), "lane-shape-"));
  try {
    const file = path.join(dir, "lane-checks.json");
    writeFileSync(file, typeof contents === "string" ? contents : JSON.stringify(contents));
    return run(file, dir);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test("the loader reads the derived checks from where the miner actually writes them", () => {
  const onDisk = shippedLaneChecks.lanes?.lane2?.checks;
  assert.ok(Array.isArray(onDisk) && onDisk.length > 0, "the shipped file must still carry lane 2 rows at lanes.lane2.checks");
  const loaded = loadCatalogue();
  assert.equal(loaded.laneChecks.present, true);
  assert.equal(loaded.laneChecks.shape, "lanes.lane2.checks", "the loader must find the rows at the shape the miner writes");
  assert.equal(
    loaded.laneChecks.found,
    onDisk.length,
    `every derived row must be accounted for: the file holds ${onDisk.length}`
  );
  assert.equal(loaded.laneChecks.notLoaded, 0, "no derived row may go unloaded without being counted");
  assert.equal(loaded.laneChecks.unreadable, null);
});

test("a shape mismatch is reported loudly with the number of rows it could not load", () => {
  // The exact regression: the rows move one level deeper and nothing this lane knows matches.
  const moved = { lanes: { lane2: { buckets: { checks: shippedLaneChecks.lanes.lane2.checks } } } };
  withLaneFile(moved, (file) => {
    const loaded = loadCatalogue({ lanePath: file });
    assert.equal(loaded.laneChecks.shape, null, "no known shape may claim to have matched");
    assert.equal(loaded.laneChecks.notLoaded, moved.lanes.lane2.buckets.checks.length,
      "a mismatch must say how many rows it failed to load");
    assert.ok(loaded.laneChecks.unreadable.includes(String(moved.lanes.lane2.buckets.checks.length)),
      "the number must be in the sentence a person reads, not only in a field");
    assert.ok(/0 of \d+ were loaded/.test(loaded.laneChecks.unreadable),
      "the sentence must state the shortfall as a count");
    assert.ok(Array.isArray(loaded.laneChecks.shapesTried) && loaded.laneChecks.shapesTried.length > 0,
      "the mismatch must record where it looked, so the next person can fix it in one edit");
    // The old sentence is the thing being outlawed: it made 50 dead checks read as routine.
    const parked = JSON.stringify(loaded.laneChecksParkedSummary);
    assert.equal(/could not be read, so only this lane's own checks were run/.test(parked), false,
      "a shape mismatch must never be reported with the vague sentence that hid this bug");
  });
});

test("a shape mismatch takes the layer down instead of passing as one parked line", () => {
  const moved = { lanes: { lane2: { buckets: { checks: shippedLaneChecks.lanes.lane2.checks } } } };
  withLaneFile(moved, (file, dir) => {
    const out = path.join(dir, "report.json");
    // A live connection would make this pass for the wrong reason, so it is removed: without the
    // mismatch this run would park on the missing connection, which is NOT a failure.
    const env = { ...process.env };
    delete env.GOATOS_STG_READONLY_DATABASE_URL;
    const result = spawnSync(process.execPath, [runner, "--out", out, "--lane-checks", file], { cwd: repo, encoding: "utf8", env });
    assert.notEqual(result.status, 0, "an unreadable derived file must not exit green");
    const report = JSON.parse(readFileSync(out, "utf8"));
    assert.equal(report.status, "fail", "a derived file this lane cannot read is a failed layer, not a parked one");
    assert.ok(/0 of \d+ were loaded/.test(report.slackLayerMessage),
      `the layer must say how many rows went unloaded, got: ${report.slackLayerMessage}`);
    assert.equal(report.checksRun, 0, "nothing may be claimed as run once the derived file is broken");
    assert.equal(report.findings.length, 0, "a broken loader must never render a verdict");

    // The control: the same run with the rows where they belong is NOT a failure for this reason.
    const ok = path.join(dir, "ok.json");
    const good = spawnSync(process.execPath, [runner, "--out", ok, "--lane-checks", laneChecksFile], { cwd: repo, encoding: "utf8", env });
    const okReport = JSON.parse(readFileSync(ok, "utf8"));
    assert.equal(okReport.status, "parked", "with a readable derived file the run parks on the connection, not on the shape");
    assert.notEqual(good.status, 0);
    assert.equal(/were loaded/.test(okReport.slackLayerMessage ?? ""), false);
  });
});

test("an absent derived file and an unreadable one are never the same answer", () => {
  const absent = loadCatalogue({ lanePath: path.join(here, "no-such-lane-checks.json") });
  assert.equal(absent.laneChecks, null, "no file at all must say so, with no invented count");
  assert.equal(absent.laneChecksParkedSummary.length, 0, "an absent file parks nothing");
  withLaneFile("{ not json at all", (file) => {
    const broken = loadCatalogue({ lanePath: file });
    assert.ok(broken.laneChecks.unreadable, "a file that will not parse is unreadable, not absent");
    assert.ok(/not valid JSON/.test(broken.laneChecks.unreadable));
  });
  withLaneFile({ lanes: { lane2: { checks: [] } } }, (file) => {
    const empty = loadCatalogue({ lanePath: file });
    assert.equal(empty.laneChecks.unreadable, null, "a file with no rows is readable and empty, not broken");
    assert.equal(empty.laneChecks.found, 0);
  });
});

test("derived checks stay off unless they are explicitly asked for", () => {
  assert.equal(derivedChecksRequested({}), false, "the default must be off");
  assert.equal(derivedChecksRequested({ [INCLUDE_DERIVED_ENV]: "" }), false);
  assert.equal(derivedChecksRequested({ [INCLUDE_DERIVED_ENV]: "0" }), false);
  assert.equal(derivedChecksRequested({ [INCLUDE_DERIVED_ENV]: "1" }), true);
  assert.equal(derivedChecksRequested({ [INCLUDE_DERIVED_ENV]: "true" }), true);

  // A derived row that is complete enough to run: it must still not run by default.
  const runnable = {
    lanes: { lane2: { checks: [{
      id: "lane2.probe",
      name: "a derived check that could run",
      question: "Does anything look wrong?",
      sql: "select id from goats limit 5",
      failureSentence: "Something on the screen is wrong.",
      countUnit: "animals",
      page: { title: "Herd register", path: "/counts/herd" }
    }] } }
  };
  withLaneFile(runnable, (file) => {
    const off = loadCatalogue({ lanePath: file, includeDerived: false });
    assert.equal(off.laneChecksAdded.length, 0, "an unmeasured derived check must not run by default");
    assert.equal(off.laneChecksHeldBack.length, 1, "it must be reported as held back, not silently dropped");
    assert.ok(off.laneChecksHeldBack[0].reason.includes(INCLUDE_DERIVED_ENV),
      "the held-back reason must name the switch that would run it");
    assert.equal(off.checks.some((check) => check.name === "a derived check that could run"), false);

    const on = loadCatalogue({ lanePath: file, includeDerived: true });
    assert.equal(on.laneChecksAdded.length, 1, "the switch must actually turn it on");
    assert.equal(on.laneChecksHeldBack.length, 0);
    assert.ok(on.checks.some((check) => check.name === "a derived check that could run"));
  });
});

test("fixing the path does not turn one line in tonight's post into fifty", () => {
  // Fifty rows that all park for the same reason must reach the layer sentence as ONE line.
  const many = { lanes: { lane2: { checks: Array.from({ length: 50 }, (_, index) => ({
    id: `lane2.no-screen-${index}`,
    name: `derived check ${index}`,
    question: "Does anything look wrong?",
    sql: "select id from goats limit 5",
    failureSentence: "Something on the screen is wrong."
  })) } } };
  withLaneFile(many, (file) => {
    const loaded = loadCatalogue({ lanePath: file });
    assert.equal(loaded.laneChecksParked.length, 50, "the report must keep every one of them, by name");
    assert.equal(loaded.laneChecksParkedSummary.length, 1, "the layer must see one line, not fifty");
    assert.ok(loaded.laneChecksParkedSummary[0].reason.includes("50"),
      "the one line must carry the number, so the count is never lost");
    const sentence = layerSentence([], loaded.laneChecksParkedSummary);
    assert.ok(/1 check could not be run/.test(sentence), `the layer sentence must stay one line, got: ${sentence}`);
  });
});

test("a derived check parks for the reason that is actually true of it", () => {
  const rows = [
    { id: "lane2.no-sentence", name: "no sentence", question: "q?", sql: "select id from goats limit 5" },
    { id: "lane2.no-screen", name: "no screen", question: "q?", sql: "select id from goats limit 5", failureSentence: "Something is wrong." },
    { id: "lane2.not-a-read", name: "not a read", question: "q?", failureSentence: "Something is wrong.", page: { title: "Herd register", path: "/counts/herd" }, sql: "delete from goats limit 1" }
  ];
  withLaneFile({ lanes: { lane2: { checks: rows } } }, (file) => {
    const loaded = loadCatalogue({ lanePath: file, includeDerived: true });
    const reasonFor = (name) => loaded.laneChecksParked.find((item) => item.name === name)?.reason ?? "";
    assert.ok(/sentence a person would read/.test(reasonFor("no sentence")),
      "a row missing its sentence must say so, not give the same answer as a row missing its screen");
    assert.ok(/does not name the screen/.test(reasonFor("no screen")));
    assert.ok(/not a single capped read/.test(reasonFor("not a read")));
  });
});

test("the miner's own field names are understood, so nothing parks for a name mismatch", () => {
  // The miner writes `failureSentence`; this lane calls it `humanFailure`. A row must not be
  // held back over that, or a real shape fix looks like a wall of broken checks.
  const shipped = shippedLaneChecks.lanes.lane2.checks;
  assert.ok(shipped.every((row) => typeof row.failureSentence === "string"),
    "precondition: the shipped rows carry the miner's field name");
  const loaded = loadCatalogue({ includeDerived: true });
  const reasons = new Set(loaded.laneChecksParked.map((item) => item.reason));
  for (const reason of reasons) {
    assert.equal(/sentence a person would read/.test(reason), false,
      `no shipped derived row may park for a missing sentence it actually has: ${reason}`);
  }
});

test("every lane's rows are found where that lane's file actually puts them", () => {
  // Lane 2's bug was one lane looking in the wrong place. The same question is asked of all four,
  // against the shipped file, so the next lane to drift is caught here rather than in Slack.
  for (const lane of Object.keys(shippedLaneChecks.lanes ?? {})) {
    const located = locateLaneRows(shippedLaneChecks, LANE_SHAPES[lane] ? lane : []);
    assert.ok(Array.isArray(located.rows), `lane ${lane}: its rows must be findable by a known shape`);
    assert.equal(located.rows.length, shippedLaneChecks.lanes[lane].checks.length,
      `lane ${lane}: every row in the file must be located`);
    assert.equal(located.rowsNotLoaded, 0);
  }
});

test("the row counter can tell an empty file from one this lane cannot read", () => {
  assert.equal(countCheckShapedRows({}), 0);
  assert.equal(countCheckShapedRows({ lanes: { lane2: { checks: [] } } }), 0);
  assert.equal(countCheckShapedRows({ buried: { deeper: { rows: [{ id: "a", sql: "select 1" }, { id: "b", sql: "select 2" }] } } }), 2,
    "rows that are somewhere unexpected must still be counted, or a mismatch reports zero");
  assert.equal(countCheckShapedRows({ notes: ["a", "b", "c"] }), 0, "plain strings are not checks");
});

test("summarising parked reasons never loses a check or invents one", () => {
  const parked = [
    { name: "a", reason: "this derived check does not name the screen it would show up on, so it was not run" },
    { name: "b", reason: "this derived check does not name the screen it would show up on, so it was not run" },
    { name: "c", reason: "this derived check is not a single capped read, so it was not run" }
  ];
  const summary = summariseParked(parked);
  assert.equal(summary.length, 2, "one line per distinct reason");
  const grouped = summary.find((item) => item.checkCount === 2);
  assert.ok(grouped, "the repeated reason must carry its count");
  assert.ok(grouped.reason.includes("2"));
  const single = summary.find((item) => item.name === "c");
  assert.ok(single, "a reason that applies to one check keeps that check's own name");
  assert.equal(summariseParked([]).length, 0);
});

test("the role-grant proof sees the tables of a schema-qualified check", () => {
  // The derived rows are written as public.<table> to the last one. The extractor read a dot as
  // "this is an alias", so every one of them contributed NO tables — the read-only grant proof
  // would have covered nothing at all for exactly the checks it most needed to cover. Unnoticed
  // only because those checks never loaded.
  assert.deepEqual(
    tablesReadByCatalogue([{ sql: "SELECT a FROM public.vaccination_completions LIMIT 1" }]),
    ["vaccination_completions"],
    "a schema-qualified table is a table, not an alias"
  );
  // ...and the thing the dot rule was protecting still holds: a column on an alias is not a table.
  assert.deepEqual(
    tablesReadByCatalogue([{ sql: "SELECT d.id FROM public.sales_deals d JOIN public.sales_deal_lines l ON l.deal_id = d.id WHERE d.animal_count IS DISTINCT FROM l.line_animals LIMIT 1" }]),
    ["sales_deal_lines", "sales_deals"],
    "\"is distinct from l.line_animals\" names a column, and must never be read as a table"
  );
  assert.deepEqual(tablesReadByCatalogue([{ sql: "select 1 from count(x) limit 1" }]), [], "a function is not a table");
  assert.deepEqual(tablesReadByCatalogue([{ sql: "select 1 from goats limit 1" }]), ["goats"], "the unqualified form must keep working");

  // Every shipped derived row must now yield at least one real table, or the proof is empty again.
  const derived = JSON.parse(readFileSync(laneChecksFile, "utf8")).lanes.lane2.checks;
  const withNoTables = derived
    .filter((row) => typeof row.sql === "string" && !/\\/.test(row.sql))
    .filter((row) => tablesReadByCatalogue([row]).length === 0)
    .map((row) => row.id);
  assert.deepEqual(withNoTables, [], "no derived check may contribute an empty table list to the grant proof");
});
