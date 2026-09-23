// Benchmark: send questions through the real /ceo-ai/ask path and print timings.
// usage: ASK_MESHA_BENCH_TOKEN=... node bench.mjs "question" ["question" ...]
const url = process.env.ASK_MESHA_URL || "http://127.0.0.1:8787";
const qs = process.argv.slice(2);
for (const question of qs) {
  const t0 = Date.now();
  let first = null, final = null, err = null;
  const res = await fetch(`${url}/ceo-ai/ask`, {
    method: "POST",
    headers: { Authorization: `Bearer ${process.env.ASK_MESHA_BENCH_TOKEN}`, "Content-Type": "application/json" },
    body: JSON.stringify({ question, stream: true }),
  });
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
      if (o.type === "final") final = o;
      if (o.type === "error") err = o.message;
    }
  }
  const total = Date.now() - t0;
  console.log(JSON.stringify({ question, total_s: +(total / 1000).toFixed(1), first_token_s: first && +(first / 1000).toFixed(1), db_queries: final?.timing?.db_queries, error: err }));
  if (final) console.log("  →", final.answer.slice(0, 300).replace(/\n/g, " "), "\n");
}
