import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { dateMacros, datePresent, extractNumbers, extractPath, fillMacros, gradeAnswer, numberPresent, threeWay } from "../eval/grade.mjs";
import { selectItems, truthSql } from "../eval/run.mjs";
import { evalHistory, READ_EVENTS } from "../events.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const golden = JSON.parse(fs.readFileSync(path.join(HERE, "../eval/golden.json"), "utf8"));

test("numbers: Indian commas, lakh/crore/k scaling", () => {
  assert.ok(numberPresent("owed Rs 4,50,175 on 36 deals", 450175));
  assert.ok(numberPresent("about Rs 4.5 lakh", 450175, { tol_pct: 1.5 }));
  assert.ok(numberPresent("Rs 1.2 crore", 12_000_000));
  assert.ok(!numberPresent("1,562 animals", 1561));
  assert.ok(numberPresent("150 g/day", 151, { tol: 2 }));
  assert.deepEqual(extractNumbers("G1P3 has 4"), [4]);
});

test("dates: DD/MM/YYYY, ISO and '22 Sep'", () => {
  assert.ok(datePresent("last weighed 22/09/2026", "22/09/2026"));
  assert.ok(datePresent("on 22 Sep", "2026-09-22"));
  assert.ok(!datePresent("on 21/09/2026", "22/09/2026"));
});

test("date macros are IST and month/week aligned", () => {
  const m = dateMacros(new Date("2026-09-24T20:00:00Z")); // 01:30 IST on the 25th
  assert.equal(m.today, "2026-09-25");
  assert.equal(m.month_start, "2026-09-01");
  assert.equal(m.prev_month_start, "2026-08-01");
  assert.equal(m.prev_month_end, "2026-08-31");
  assert.equal(m.week_start, "2026-09-21");
  assert.equal(m.week_end, "2026-09-27");
  assert.deepEqual(fillMacros({ q: { from: "{{month_start}}" } }, m), { q: { from: "2026-09-01" } });
});

test("grade: values, zero words, code talk, conditional flag, slowness", () => {
  const item = { expect: [{ label: "total", col: "total" }, { label: "dead", col: "dead", zero_words: true }], must_mention: ["sheep"] };
  const rows = [{ total: 1562, dead: 0 }];
  assert.equal(gradeAnswer(item, "1,562 animals (693 goats, 869 sheep). No deaths.", rows).pass, true);
  const bad = gradeAnswer(item, "1,561 goats per the ceo_ai.animal_current_scope view", rows);
  assert.equal(bad.pass, false);
  assert.ok(bad.reasons.some((r) => r.startsWith("missing total")));
  assert.ok(bad.reasons.some((r) => r.includes("must not say")));
  const flag = { expect: [], mention_if: [{ col: "double_count", regex: "double.?count" }] };
  assert.equal(gradeAnswer(flag, "Owes Rs 4,29,875.", [{ double_count: true }]).pass, false);
  assert.equal(gradeAnswer(flag, "Owes Rs 4,29,875.", [{ double_count: false }]).pass, true);
  assert.equal(gradeAnswer({ expect: [] }, "ok", [], { seconds: 100, maxSeconds: 60 }).pass, false);
  assert.equal(gradeAnswer({ expect: [] }, "", []).pass, false);
});

test("each_row expands per truth row", () => {
  const item = { expect: [{ label: "alive", col: "alive", each_row: "park_code" }] };
  const r = gradeAnswer(item, "Yashoda 3: 19 in Coimbatore, 5 in Channapatna", [{ park_code: "CBE", alive: 19 }, { park_code: "CPT", alive: 5 }]);
  assert.equal(r.pass, true);
  assert.equal(r.checks.length, 2);
});

test("extractPath: filters, ~ contains, aggregates", () => {
  const body = { headline: { average_adg_g_per_day: 150 }, by_park: [{ park_name: "Coimbatore", average_adg_g_per_day: 152 }], items: [{ activeCount: 2, days_left: 5 }, { activeCount: 3, days_left: 9 }] };
  assert.equal(extractPath(body, "headline.average_adg_g_per_day"), 150);
  assert.equal(extractPath(body, "by_park[park_name~coimb].average_adg_g_per_day"), 152);
  assert.equal(extractPath(body, "sum:items[].activeCount"), 5);
  assert.equal(extractPath(body, "min:items[].days_left"), 5);
  assert.equal(extractPath(body, "by_park[park_name=Nowhere].x"), undefined);
});

test("threeWay labels UI vs chat mismatches", () => {
  assert.equal(threeWay({ label: "ALL", truth: 150, ui: 150, chatOk: true }).verdict, "ok");
  assert.equal(threeWay({ label: "ALL", truth: 150, ui: 151, chatOk: false, tol: 2 }).verdict, "chat differs (chat bug)");
  assert.match(threeWay({ label: "ALL", truth: 150, ui: 170, chatOk: true, answer: "150 g" }).verdict, /UI differs from SQL/);
});

test("golden.json: shape, unique ids, >= 40 items, read-only truth, 5+ traps", () => {
  const ids = new Set();
  for (const it of golden.items) {
    assert.ok(it.id && !ids.has(it.id), `dup/missing id ${it.id}`);
    ids.add(it.id);
    assert.ok(it.question && Array.isArray(it.expect), it.id);
    if (it.truth?.sql) assert.doesNotMatch(it.truth.sql, /\b(insert|update|delete|drop|alter|create|truncate|grant)\b/i, it.id);
    for (const e of it.expect) assert.ok(e.col && e.label, `${it.id} expect needs col + label`);
    if (it.expect.length) assert.ok(it.truth, `${it.id}: expected values need truth (never hard-coded numbers)`);
    for (const x of it.ui_api?.extract || []) assert.ok(it.expect.some((e) => e.label === x.label), `${it.id}: ui label ${x.label} has no expect`);
  }
  assert.ok(golden.items.length >= 40);
  assert.ok(golden.items.filter((i) => i.tags.includes("trap")).length >= 5);
});

test("truthSql builds references with macros; selectItems by n/tag/id", () => {
  const m = dateMacros(new Date("2026-09-24T06:00:00Z"));
  const adg = golden.items.find((i) => i.id === "adg-last-month");
  const sql = truthSql(adg.truth, m);
  assert.match(sql, /'2026-08-01'::date/);
  assert.match(sql, /WHERE \(park='ALL'\)/);
  assert.match(truthSql(golden.items.find((i) => i.id === "pen-weighing-g1p3").truth, m), /FROM \(SELECT \* FROM \(/);
  assert.equal(selectItems(golden.items, "3").length, 3);
  assert.ok(selectItems(golden.items, "trap").every((i) => i.tags.includes("trap")));
  assert.deepEqual(selectItems(golden.items, "adg-by-park,sick-now").map((i) => i.id), ["adg-by-park", "sick-now"]);
});

test("evalHistory: eval_run events newest first; read side includes them", () => {
  assert.ok(READ_EVENTS.includes("eval_run"));
  const h = evalHistory([
    { event_name: "eval_run", ts: "2026-09-20T00:00:00Z", pass: 30, fail: 10, accuracy: 0.75 },
    { event_name: "ask_completed", ts: "2026-09-21T00:00:00Z" },
    { event_name: "eval_run", ts: "2026-09-24T00:00:00Z", pass: 38, fail: 2, accuracy: 0.95, cost_usd: 6.1 },
  ]);
  assert.equal(h.runs.length, 2);
  assert.equal(h.last.accuracy, 0.95);
  assert.equal(h.runs[1].pass, 30);
});
