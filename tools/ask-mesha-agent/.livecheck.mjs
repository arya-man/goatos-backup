import { query } from "@anthropic-ai/claude-agent-sdk";
import os from "node:os";
import { checkAnswer } from "/Users/raviteja/airnd/goatos-checker/tools/ask-mesha-agent/checker.mjs";
const answer = `| Week of | Castro 1 | Castro 2 | Castro 3 |
|---|---|---|---|
| 03/08 | 30.79 (63) | 26.08 (74) | 23.91 (64) |
| 10/08 (weighed 11/08) | 31.27 (63) | 27.50 (72) | 25.08 (63) |
| 31/08 (weighed 01/09) | 34.92 (63) | 30.27 (74) | 27.94 (63) |
| 21/09 | 39.07 (54) | 34.25 (73) | 31.86 (59) |
- Why the chart has two low weeks: In the weeks of 10/08 and 31/08 the pens were weighed twice. The chart uses the later weighing (11/08 and 01/09), which falls only a day after the previous week's. That's why those weeks show only 40-60 g/day.
- Weights on 10/08 and 31/08: Castro 1 was 31.75 kg on 10/08 and 34.76 kg on 31/08. Castro 2 was 27.40 and 30.54, Castro 3 25.16 and 27.78.
\`\`\`chart
{"type":"line","title":"Average weight per sheep, Castro pens, Coimbatore (kg)","x":["03/08","10/08","31/08","21/09"],"series":[{"name":"kg","data":[30.79,31.27,34.92,39.07]}]}
\`\`\``;
const evidence = `pen|date|n|avg_kg
Castro 1|2026-08-03|63|30.79
Castro 1|2026-08-10|63|31.75
Castro 1|2026-08-11|63|31.27
Castro 1|2026-08-31|63|34.76
Castro 1|2026-09-01|63|34.92
Castro 1|2026-09-21|54|39.07
Castro 2|2026-08-03|74|26.08
Castro 2|2026-08-10|73|27.40
Castro 2|2026-08-11|72|27.50
Castro 2|2026-08-31|74|30.54
Castro 2|2026-09-01|74|30.27
Castro 2|2026-09-21|73|34.25
Castro 3|2026-08-03|64|23.91
Castro 3|2026-08-10|64|25.16
Castro 3|2026-08-11|63|25.08
Castro 3|2026-08-31|63|27.78
Castro 3|2026-09-01|63|27.94
Castro 3|2026-09-21|59|31.86
weekly gain g/day (dashboard): week 10/08: Castro 1 60, Castro 2 177, Castro 3 147; week 31/08: Castro 1 60, Castro 2 51, Castro 3 40`;
const run = async (prompt, signal) => { const ac=new AbortController(); signal.addEventListener("abort",()=>ac.abort()); let text="",costUsd=0;
  for await (const m of query({prompt, options:{cwd:os.tmpdir(), model:"claude-haiku-4-5-20251001", maxTurns:1, tools:[], settingSources:[], systemPrompt:"You check answers against query results. Reply with JSON only.", abortController:ac, env:{...process.env, CLAUDE_CODE_DISABLE_CLAUDE_MDS:"1"}}})) {
    if (m.type==="assistant") { for (const b of m.message?.content||[]) if (b.type==="text") text+=b.text; } else if (m.type==="result") costUsd=m.total_cost_usd||0; }
  return {text,costUsd}; };
const t=Date.now(); const r = await checkAnswer({question:"Castro 1,2,3 CBE average weight per sheep per week", answer, evidence, run, timeoutMs:90000});
console.log(JSON.stringify({ms:Date.now()-t, ok:r.ok, error:r.error, cost:r.costUsd, issues:r.issues},null,1)); console.log("---REVISED---\n"+r.revised);
