"use client";

// LOCAL-ONLY visual harness for the Ask Mesha panel. Mocks /api/ceo-ai/* so the
// real panel renders without login. Never commit this.
import { useEffect, useState } from "react";
import { CeoAiPanel, type AssistantCopy } from "@/features/ceo-ai";

const ANSWER = `**129 animals were sold this month** — Coimbatore 125, Channapatna 4.

| Park | Sold | Avg weight (kg) | Revenue proxy | Pens | Last sale | Buyer notes |
|---|---|---|---|---|---|---|
| Coimbatore | 125 | 31.8 | n/a | CBE Castro 1, CBE Castro 2, CBE Yashoda 3 | 22/09/2026 | Mostly Bakrid trade buyers, paid on pickup |
| Channapatna | 4 | 29.4 | n/a | CPT Castro 1 | 18/09/2026 | Local |

- Counted by exit reason "sold", by exit date.
- Source: https://admin.stg.goatos.example/reports/sales/exits?park=all&from=2026-09-01&to=2026-09-30&reason=sold&view=detailed
- No price view exists yet.

\`\`\`sql
select park_label, shed_label, count(*) as sold_count, avg(weight_kg) as avg_weight from ceo_ai.animals_base where exit_reason = 'sold' and exit_date >= date_trunc('month', now()) group by 1, 2 order by 3 desc;
\`\`\``;

const BAR = {
  type: "bar",
  title: "Feed issued by feed type this month (kg)",
  x: ["Mesha Kids Concentrate – CBE", "Mesha Kids Concentrate – CPT", "Grower Pellet – CBE", "Dry Fodder"],
  series: [{ name: "kg", data: [1840, 912, 1320, 260] }],
};
const LINE = {
  type: "line",
  title: "Weekly ADG (g/day), last 12 weeks",
  x: Array.from({ length: 12 }, (_, i) => `W${27 + i} (${String(1 + i * 7).padStart(2, "0")}/07)`),
  series: [{ name: "ADG", data: [88, 92, 95, 90, 101, 106, 99, 110, 114, 108, 117, 121] }],
};

const STEPS = [
  "Looking at your screenshot",
  "Using the Mesha data map",
  "Looking up how feed is worked out",
  "Checking feed records",
  "Checking sales and exits records",
  "Double-checking the numbers across Coimbatore and Channapatna parks",
  "Drafting the answer",
];

function sse(events: object[], delay = 500, signal?: AbortSignal | null): Response {
  const body = new ReadableStream({
    async start(c) {
      const enc = new TextEncoder();
      // Like a real fetch: aborting errors the body so reader.read() rejects.
      signal?.addEventListener("abort", () => c.error(new DOMException("Aborted", "AbortError")));
      for (const e of events) {
        if (signal?.aborted) return;
        c.enqueue(enc.encode(`data: ${JSON.stringify(e)}\n\n`));
        await new Promise((r) => setTimeout(r, delay));
      }
      c.close();
    },
  });
  return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
}

const tokens = (t: string) => t.match(/[\s\S]{1,120}/g)!.map((x) => ({ type: "token", text: x }));
const progress = (n: number) => [
  { type: "progress", phase: "planning", label: "Starting agent", request_id: "req-harness-1" },
  ...STEPS.slice(0, n).map((label) => ({ type: "progress", phase: "querying", label })),
];

let currentSignal: AbortSignal | null | undefined;
function scenario(q: string): Response {
  if (/line/i.test(q))
    return sse([...progress(3), ...tokens("ADG is **up 38%** over 12 weeks."), { type: "final", answer: "ADG is **up 38%** over 12 weeks.", conversation_id: "c2", message_id: "m2", chart: LINE }]);
  if (/reset/i.test(q))
    return sse([...progress(2), ...tokens("Let me check the feed table first…"), { type: "reset" }, ...progress(4).slice(3), ...tokens("Feed issued: **4,332 kg**."), { type: "final", answer: "Feed issued: **4,332 kg**.", conversation_id: "c2", message_id: "m3" }]);
  if (/error/i.test(q)) return sse([...progress(3), { type: "error", message: "Ask Mesha hit a problem reading the data. Please try again." }]);
  if (/nofinal/i.test(q)) return sse([...progress(2), ...tokens("Partial answer that never gets a final event…")]);
  if (/slow/i.test(q)) return sse([...progress(7), ...tokens(ANSWER)], 1500, currentSignal);
  return sse([...progress(7), ...tokens(ANSWER), { type: "final", answer: ANSWER, mode: "agent", conversation_id: "c2", message_id: "m1", chart: BAR }]);
}

function installMocks() {
  const real = window.fetch.bind(window);
  (window as unknown as { __stops: unknown[] }).__stops = [];
  window.fetch = async (input, init) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
    if (url.includes("/api/ceo-ai/starters"))
      return Response.json({
        starters: [
          "How many animals were weighed this month, by park?",
          "Show the weekly ADG trend for the last 8 weeks as a chart",
          "How many animals did we sell this month, by park?",
          "Which pens are behind on weighing verification?",
        ],
      });
    if (url.includes("/api/ceo-ai/events")) {
      (window as unknown as { __stops: unknown[] }).__stops.push(JSON.parse(String(init?.body)));
      return new Response(null, { status: 204 });
    }
    if (url.includes("/api/ceo-ai/conversations") && (!init || init.method === undefined || init.method === "GET"))
      return Response.json({ conversations: [{ id: "c1", title: "when did we do weighing?" }] });
    if (url.includes("/api/ceo-ai/conversations")) return Response.json({ id: "c2", title: "New chat" });
    currentSignal = init?.signal;
    if (url.includes("/api/ceo-ai/ask")) return scenario(String(JSON.parse(String(init?.body)).question));
    return real(input, init);
  };
}

const COPY: AssistantCopy = {
  title: "Ask Mesha",
  subtitle: "Ask about your operations",
  hello: "",
  helloMeta: "",
  checking: "Checking…",
  noAnswer: "No answer",
  unavailable: "Assistant temporarily unavailable",
  unavailableMeta: "",
  sourceFallback: "",
  modeFallback: "",
  placeholder: "Ask about sales, weighing, counts, feed...",
  open: "Open",
  close: "Close",
  send: "Send",
  starters: [],
};

export default function AskHarness() {
  const [ready, setReady] = useState(false);
  useEffect(() => {
    installMocks();
    setReady(true);
  }, []);
  return <div style={{ minHeight: "100dvh", background: "#131a15" }}>{ready ? <CeoAiPanel copy={COPY} /> : null}</div>;
}
