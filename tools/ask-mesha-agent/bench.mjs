// Benchmark: send questions through the real /ceo-ai/ask path and print timings.
// usage: ASK_MESHA_BENCH_TOKEN=... node bench.mjs [--same-chat] "question" ["question" ...]
//   --same-chat: later questions are follow-ups in the first question's chat (resume path).
const url = process.env.ASK_MESHA_URL || "http://127.0.0.1:8787";
const args = process.argv.slice(2);
const sameChat = args.includes("--same-chat");
const qs = args.filter((a) => a !== "--same-chat");
// Words a CEO-facing answer must not contain (docs/agent-rules/ask-mesha.md).
const CODE_TALK = /\b(sql|query|queries|database|schema|column|ceo_ai\.|public\.\w|codebase|the code|function|tool|budget|token)\b/i;
let conversationId;
for (const question of qs) {
  const t0 = Date.now();
  let first = null, final = null, err = null, resets = 0;
  const steps = [];
  const res = await fetch(`${url}/ceo-ai/ask`, {
    method: "POST",
    headers: { Authorization: `Bearer ${process.env.ASK_MESHA_BENCH_TOKEN}`, "Content-Type": "application/json" },
    body: JSON.stringify({ question, stream: true, ...(sameChat && conversationId ? { conversation_id: conversationId } : {}) }),
  });
  if (!(res.headers.get("content-type") || "").includes("text/event-stream")) {
    console.log(JSON.stringify({ question, status: res.status, body: await res.text() }));
    continue;
  }
  const dec = new TextDecoder(); let buf = "";
  for await (const chunk of res.body) {
    buf += dec.decode(chunk, { stream: true });
    let i;
    while ((i = buf.indexOf("\n\n")) >= 0) {
      const ev = buf.slice(0, i); buf = buf.slice(i + 2);
      const line = ev.split("\n").find((l) => l.startsWith("data:"));
      if (!line) continue;
      const o = JSON.parse(line.slice(5));
      if (o.type === "token" && first === null) first = Date.now() - t0;
      if (o.type === "reset") resets += 1;
      if (o.type === "progress" && o.label) steps.push(o.label);
      if (o.type === "final") final = o;
      if (o.type === "error") err = o.message;
    }
  }
  if (final?.conversation_id) conversationId ??= final.conversation_id;
  const total = Date.now() - t0;
  const codeTalk = final?.answer?.match(CODE_TALK)?.[0] ?? null;
  console.log(JSON.stringify({
    question, total_s: +(total / 1000).toFixed(1), first_token_s: first && +(first / 1000).toFixed(1),
    db_queries: final?.timing?.db_queries, resets, steps: [...new Set(steps)], code_talk: codeTalk,
    conversation_id: final?.conversation_id, error: err,
  }));
  if (final) console.log("  →", final.answer.slice(0, 400).replace(/\n/g, " "), "\n");
}
