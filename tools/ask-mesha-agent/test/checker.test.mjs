import test from "node:test";
import assert from "node:assert/strict";
import { buildCheckPrompt, checkAnswer, EVIDENCE_MAX_CHARS, makeEvidence, parseCheck } from "../checker.mjs";

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
  assert.ok(ev.text().replace(/--- result \d+ ---\n/g, "").replace(/\n/g, "").length <= EVIDENCE_MAX_CHARS);
});

test("prompt carries question, draft and results", () => {
  const p = buildCheckPrompt({ question: "Q?", answer: "DRAFT", evidence: "ROWS" });
  assert.match(p, /QUESTION:\nQ\?/);
  assert.match(p, /DRAFT ANSWER:\nDRAFT/);
  assert.match(p, /QUERY RESULTS:\nROWS$/);
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
    question: "q", answer: "The two low weeks...", evidence: "rows",
    run: async (prompt) => {
      assert.match(prompt, /The two low weeks/);
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
  let aborted = false;
  const slow = await checkAnswer({
    question: "q", answer: "a", evidence: "e", timeoutMs: 20,
    run: (_p, signal) => new Promise((resolve) => { signal.addEventListener("abort", () => { aborted = true; resolve({ text: "" }); }); }),
  });
  assert.equal(slow.ok, true);
  assert.equal(slow.error, "checker_timeout");
  assert.equal(aborted, true);
});
