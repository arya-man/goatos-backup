import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  REFERENCE_DIR, REFERENCE_FILES, referencePath, referenceParams, paramLiteral, stripSqlComments,
  buildReferenceSql, validateReadSql, hasPsqlBackslash, toolLabel,
} from "../lib.mjs";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const read = (n) => fs.readFileSync(path.join(ROOT, REFERENCE_DIR, n), "utf8");

test("every allow-listed reference file builds into ONE read-only SELECT", () => {
  for (const n of REFERENCE_FILES) {
    const r = buildReferenceSql(read(n), { name: n });
    assert.equal(r.ok, true, `${n}: ${r.out}`);
    assert.match(r.sql, /^SELECT \* FROM \(\n(WITH|SELECT)\b/i, n);
    assert.ok(!r.sql.includes(";"), n);
    assert.ok(!/--|\/\*/.test(r.sql.replace(/'(?:[^']|'')*'/g, "''")), `${n}: comments stripped`);
  }
});

test("allow-list: only bare file names from the list, no paths", () => {
  for (const bad of ["../../../etc/passwd", "references/pens.sql", "/abs/pens.sql", "pens-selftest.sql", "views.generated.md", "", "pens.sql/.."]) {
    assert.equal(referencePath("/repo", bad).ok, false, bad);
  }
  assert.equal(referencePath("/repo", "pens.sql").file, path.join("/repo", REFERENCE_DIR, "pens.sql"));
});

test("where / order_by / limit wrap the file as a subquery", () => {
  const r = buildReferenceSql(read("pen-weighing-latest.sql"), { name: "p", where: "pen_code='G1P3' AND park_code='CBE'", order_by: "park", limit: 5 });
  assert.equal(r.ok, true, r.out);
  assert.match(r.sql, /\n\) q\nWHERE \(pen_code='G1P3' AND park_code='CBE'\)\nORDER BY park\nLIMIT 5$/);
});

test("free-text clauses cannot add statements, comments or tx commands", () => {
  const t = read("pens.sql");
  for (const where of ["true; commit", "true) q; set x=1", "true -- tail", "true /* x */", "\\! ls"]) {
    assert.equal(buildReferenceSql(t, { where }).ok, false, where);
  }
  assert.equal(buildReferenceSql(t, { order_by: "1; rollback" }).ok, false);
  for (const limit of [0, -1, 501, 1.5, "x"]) assert.equal(buildReferenceSql(t, { limit }).ok, false, String(limit));
});

test("params: declared + typed only, defaults kept when omitted", () => {
  const adg = read("adg-by-park.sql");
  assert.deepEqual(Object.keys(referenceParams(adg)).sort(), ["from_date", "to_date"]);
  const def = buildReferenceSql(adg, {});
  assert.match(def.sql, /date_trunc\('month'/);
  const r = buildReferenceSql(adg, { params: { from_date: "2026-08-01", to_date: "2026-08-31" } });
  assert.equal(r.ok, true, r.out);
  assert.match(r.sql, /SELECT '2026-08-01'::date AS f,\s+'2026-08-31'::date AS t/);
  assert.equal(buildReferenceSql(adg, { params: { from_date: "2026-02-30" } }).ok, false);
  assert.equal(buildReferenceSql(adg, { params: { from_date: "2026-08-01'::date); drop" } }).ok, false);
  assert.equal(buildReferenceSql(adg, { params: { park: "CBE" } }).ok, false);
  assert.equal(buildReferenceSql(read("pens.sql"), { params: { x: 1 } }).ok, false);
  const c = buildReferenceSql(read("cost-per-kg-gain.sql"), { params: { days: 60 } });
  assert.match(c.sql, /::date - 60 AS s/);
  assert.equal(buildReferenceSql(read("cost-per-kg-gain.sql"), { params: { days: "30 OR 1=1" } }).ok, false);
});

test("paramLiteral validates date / uuid / int / number", () => {
  assert.equal(paramLiteral("date", "2026-09-01"), "'2026-09-01'::date");
  assert.equal(paramLiteral("uuid", "00000000-0000-4000-8000-000000000001"), "'00000000-0000-4000-8000-000000000001'::uuid");
  assert.equal(paramLiteral("uuid", "x' or '1"), null);
  assert.equal(paramLiteral("int", 7), "7");
  assert.equal(paramLiteral("int", "7.5"), null);
  assert.equal(paramLiteral("number", "7.5"), "7.5");
  assert.equal(paramLiteral("number", "1e9"), null);
});

test("undeclared inline param markers are refused", () => {
  assert.equal(buildReferenceSql("SELECT /*param:x*/1/*end*/", {}).ok, false);
  assert.equal(buildReferenceSql("-- param: x int\nSELECT /*param:x*/1/*end*/ n;", { params: { x: 4 } }).sql, "SELECT * FROM (\nSELECT 4 n\n) q");
});

test("comment stripping keeps string literals intact", () => {
  assert.equal(stripSqlComments("select '--x' a -- c; d\n, '/*y*/' b /* z; */ from t"), "select '--x' a \n, '/*y*/' b   from t");
  assert.equal(stripSqlComments("select 'it''s -- ok' x"), "select 'it''s -- ok' x");
});

test("backslashes: allowed inside plain strings only", () => {
  assert.equal(hasPsqlBackslash("select regexp_replace(x, '([A-Z])[a-z]*', '\\1', 'g')"), false);
  for (const bad of ["select 1 \\! ls", "\\copy t to '/tmp/x'", "select 'a' \\gexec", "select E'\\''", "select U&'\\0041'", "select $$\\$$", "select 'it''s' \\g"]) {
    assert.equal(hasPsqlBackslash(bad), true, bad);
    assert.equal(validateReadSql(bad).ok, false, bad);
  }
});

test("run_reference has a plain progress label", () => {
  assert.equal(toolLabel("mcp__mesha__run_reference", { name: "pens.sql" }), "Checking the pen records");
  assert.equal(toolLabel("mcp__mesha__run_reference", { name: "nope" }), "Checking the records");
  for (const n of REFERENCE_FILES) assert.doesNotMatch(toolLabel("mcp__mesha__run_reference", { name: n }), /sql|query|file/i);
});

test("server registers run_reference read-only and allows it", () => {
  const src = fs.readFileSync(path.join(ROOT, "tools/ask-mesha-agent/server.mjs"), "utf8");
  assert.match(src, /"run_reference",[\s\S]*?buildReferenceSql[\s\S]*?runSql\(built\.sql\)[\s\S]*?\n\s+RO,\n/);
  assert.match(src, /allowedTools: \[[^\]]*"mcp__mesha__run_reference"/);
  const block = src.slice(src.indexOf('"run_reference",'), src.indexOf('"describe_table",'));
  assert.doesNotMatch(block, /z\.record\(/, "z.record hides the tool from the model");
});
