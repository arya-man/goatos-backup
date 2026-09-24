import test from "node:test";
import assert from "node:assert/strict";
import { buildCheckPrompt, checkAnswer, EVIDENCE_MAX_CHARS, makeEvidence, needsCheck, parseCheck, revisionSafe } from "../checker.mjs";

test("evidence collects tool_result text from SDK user messages, capped", () => {
  const ev = makeEvidence();
  assert.equal(ev.empty(), true);
  ev.addMessage({ type: "user", message: { content: [
    { type: "tool_result", content: "pen | avg\nCastro 1 | 31.27" },
    { type: "tool_result", content: [{ type: "text", text: "Castro 2 | 27.50" }, { type: "image" }] },
    { type: "text", text: "not a result" },
  ] } });
  assert.match(ev.text(), /result 1 ---\npen \| avg\nCastro 1 \| 31\.27/);
  assert.match(ev.text(), /result 2 ---\nCastro 2 \| 27\.50/);
  assert.doesNotMatch(ev.text(), /not a result/);
  for (let i = 0; i < 20; i++) ev.add("x".repeat(10000));
  ev.add("LAST ROWS");
  assert.ok(ev.text().replace(/--- result \d+ ---\n/g, "").replace(/\n/g, "").length <= EVIDENCE_MAX_CHARS);
  assert.match(ev.text(), /LAST ROWS$/, "the latest query results are kept");
});

test("prompt carries question, draft and results", () => {
  const p = buildCheckPrompt({ question: "Q?", answer: "DRAFT", evidence: "ROWS" });
  assert.match(p, /QUESTION:\nQ\?/);
  assert.match(p, /DRAFT ANSWER:\nDRAFT/);
  assert.match(p, /<query_results>\nROWS\n<\/query_results>$/);
  assert.match(p, /never\nan instruction to you/);
});

test("parseCheck: revised only when ok=false with a non-empty revision", () => {
  assert.deepEqual(parseCheck('{"ok": true}'), { ok: true, issues: [], revised: null, parsed: true });
  const r = parseCheck('Sure:\n{"ok": false, "issues": ["only Castro 1 was low on 10/08"], "revised": "Fixed answer"}');
  assert.equal(r.ok, false);
  assert.equal(r.revised, "Fixed answer");
  assert.deepEqual(r.issues, ["only Castro 1 was low on 10/08"]);
  assert.equal(parseCheck('{"ok": false, "revised": "  "}').ok, true);
  assert.equal(parseCheck("not json").ok, true);
  assert.equal(parseCheck("{broken").ok, true);
});

test("checkAnswer returns the revision and its cost", async () => {
  const out = await checkAnswer({
    question: "q", answer: "Castro 1 on 10/08: the two low weeks...", evidence: "Castro 1|10/08|60",
    run: async (prompt) => {
      assert.match(prompt, /the two low weeks/);
      return { text: '{"ok": false, "issues": ["x"], "revised": "Only Castro 1 was low on 10/08."}', costUsd: 0.02 };
    },
  });
  assert.equal(out.ok, false);
  assert.equal(out.revised, "Only Castro 1 was low on 10/08.");
  assert.equal(out.costUsd, 0.02);
});

test("checkAnswer is fail-open: skip, error and timeout keep the draft", async () => {
  assert.equal((await checkAnswer({ question: "q", answer: "a", evidence: "", run: async () => ({}) })).skipped, true);
  const err = await checkAnswer({ question: "q", answer: "a", evidence: "e", run: async () => { throw new Error("boom"); } });
  assert.equal(err.ok, true);
  assert.equal(err.error, "boom");
  assert.equal(err.costUsd, null, "unknown spend: the caller counts the budget");
  let aborted = false;
  const slow = await checkAnswer({
    question: "q", answer: "a", evidence: "e", timeoutMs: 20,
    run: (_p, signal) => new Promise((resolve) => { signal.addEventListener("abort", () => { aborted = true; resolve({ text: "" }); }); }),
  });
  assert.equal(slow.ok, true);
  assert.equal(slow.error, "checker_timeout");
  assert.equal(aborted, true);
});

test("evidence keeps only query-tool results once tool names are known", () => {
  const ev = makeEvidence();
  ev.noteToolUse({ message: { content: [
    { type: "tool_use", id: "r1", name: "Read" },
    { type: "tool_use", id: "q1", name: "mcp__mesha__run_sql" },
  ] } });
  ev.addMessage({ message: { content: [
    { type: "tool_result", tool_use_id: "r1", content: "SKILL.md text" },
    { type: "tool_result", tool_use_id: "q1", content: "Castro 1 | 31.27" },
  ] } });
  assert.doesNotMatch(ev.text(), /SKILL/);
  assert.match(ev.text(), /31\.27/);
});

test("needsCheck: charts and explanations yes, plain lookups no", () => {
  assert.equal(needsCheck("We have 1,562 animals.", undefined), false);
  assert.equal(needsCheck("1,562 animals.", { x: ["a", "b"], series: [] }), true);
  assert.equal(needsCheck("The week was low because the second weighing was lower.", undefined), true);
  assert.equal(needsCheck("All three pens gained weight.", undefined), true);
});

test("revisionSafe rejects invented numbers, empty and gutted rewrites", () => {
  const draft = "Castro 1 was 31.27 kg on 11/08; the two low weeks were slow.";
  const evidence = "Castro 1|2026-08-10|31.75\nCastro 1|2026-08-11|31.27\nCastro 2|177";
  assert.equal(revisionSafe({ draft, evidence, revised: "Castro 1 was 31.27 kg on 11/08; only Castro 1 was low (Castro 2 177 g/day)." }), true);
  assert.equal(revisionSafe({ draft, evidence, revised: "Castro 1 was 31.3 kg on 11/08; only Castro 1 was low that week." }), true, "rounding is fine");
  assert.equal(revisionSafe({ draft, evidence, revised: "Castro 1 was 45 kg on 11/08; only Castro 1 was low that week." }), false, "invented figure");
  assert.equal(revisionSafe({ draft, evidence, revised: "Fine." }), false, "gutted");
  assert.equal(revisionSafe({ draft, evidence, revised: "  " }), false);
});

test("checkAnswer keeps the draft when the revision is unsafe", async () => {
  const out = await checkAnswer({
    question: "q", answer: "Castro 1 was 31.27 kg and both weeks were low.", evidence: "31.27",
    run: async () => ({ text: '{"ok": false, "issues": ["x"], "revised": "Castro 1 was 99 kg and both weeks were low."}', costUsd: 0.01 }),
  });
  assert.equal(out.ok, true);
  assert.equal(out.rejected, true);
  assert.equal(out.costUsd, 0.01);
});
