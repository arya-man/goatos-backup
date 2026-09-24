// Answer checker: one extra model call after the agent drafts its answer. It sees
// the question, the draft and the query results the agent actually got, and flags
// sentences the rows don't support (a "why" with no evidence, "all pens / both weeks"
// when some rows say otherwise, a chart whose title doesn't match its lines). When it
// finds any, it returns a corrected answer; otherwise the draft stands. Fail-open:
// any error or timeout keeps the draft.

export const EVIDENCE_MAX_CHARS = 40000;
const PER_RESULT_MAX_CHARS = 6000;

// Collects tool results (query rows) from the SDK message stream, capped.
export function makeEvidence() {
  const parts = [];
  let size = 0;
  return {
    add(text) {
      if (!text || size >= EVIDENCE_MAX_CHARS) return;
      const piece = String(text).slice(0, Math.min(PER_RESULT_MAX_CHARS, EVIDENCE_MAX_CHARS - size));
      parts.push(piece);
      size += piece.length;
    },
    // SDK "user" messages carry tool_result blocks (content: string | [{type:"text",text}]).
    addMessage(msg) {
      for (const block of msg?.message?.content || []) {
        if (block?.type !== "tool_result") continue;
        const c = block.content;
        if (typeof c === "string") this.add(c);
        else if (Array.isArray(c)) for (const x of c) if (x?.type === "text") this.add(x.text);
      }
    },
    text: () => parts.map((p, i) => `--- result ${i + 1} ---\n${p}`).join("\n"),
    empty: () => parts.length === 0,
  };
}

export function buildCheckPrompt({ question, answer, evidence }) {
  return `You check a farm-data answer before a CEO sees it. You get the question, the draft answer and
the query results the answer was built from. Numbers were computed carefully; your job is the WORDS around them.

Flag a sentence only when the results show it is wrong or unsupported:
1. A reason / "why" / "because" that the rows don't show, or that the rows contradict (e.g. a low week blamed on
   timing when the rows show the second weighing came out lower).
2. A general claim ("all", "every", "both", "the two weeks", "each pen") that is false for at least one item in
   the results. Name exactly which items it holds for instead.
3. Figures the answer presents as the screen's numbers but that are raw/superseded rows the screen does not use.
4. A chart whose title or series names don't match what is plotted (e.g. titled "Castro pens" but one line).
Do NOT flag style, length, rounding, or numbers you cannot check from the results. Do not add new facts.

Reply with ONLY a JSON object, no prose:
{"ok": true} when nothing needs fixing, or
{"ok": false, "issues": ["short reason", ...], "revised": "<the full corrected answer>"}
The revised answer keeps everything that was right (same tables, numbers, tone, language, any \`\`\`chart block
fixed or removed), and only rewrites or drops the flagged sentences.

QUESTION:
${question}

DRAFT ANSWER:
${answer}

QUERY RESULTS:
${evidence}`;
}

// Parses the checker's reply. Anything unusable => ok (keep the draft).
export function parseCheck(text) {
  const s = String(text || "");
  const start = s.indexOf("{");
  const end = s.lastIndexOf("}");
  if (start < 0 || end <= start) return { ok: true, issues: [], revised: null, parsed: false };
  try {
    const o = JSON.parse(s.slice(start, end + 1));
    if (o.ok === false && typeof o.revised === "string" && o.revised.trim()) {
      return { ok: false, issues: Array.isArray(o.issues) ? o.issues.map(String).slice(0, 8) : [], revised: o.revised.trim(), parsed: true };
    }
    return { ok: true, issues: [], revised: null, parsed: true };
  } catch {
    return { ok: true, issues: [], revised: null, parsed: false };
  }
}

// run(prompt, signal) -> { text, costUsd }. Injected so tests need no model; aborted on timeout
// so a stuck check stops spending.
export async function checkAnswer({ question, answer, evidence, run, timeoutMs = 45000 }) {
  if (!answer || !evidence) return { ok: true, issues: [], revised: null, costUsd: 0, skipped: true };
  let timer;
  const ac = new AbortController();
  try {
    const out = await Promise.race([
      run(buildCheckPrompt({ question, answer, evidence }), ac.signal),
      new Promise((_, rej) => { timer = setTimeout(() => { rej(new Error("checker_timeout")); ac.abort(); }, timeoutMs); }),
    ]);
    return { ...parseCheck(out?.text), costUsd: Number(out?.costUsd) || 0 };
  } catch (err) {
    return { ok: true, issues: [], revised: null, costUsd: 0, error: String(err?.message || err).slice(0, 120) };
  } finally {
    clearTimeout(timer);
  }
}
