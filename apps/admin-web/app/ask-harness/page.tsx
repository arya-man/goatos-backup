"use client";

// LOCAL-ONLY visual harness for the Ask Mesha panel. Mocks /api/ceo-ai/* so the
// real panel renders without login. Never commit this.
import { useEffect, useState } from "react";
import { CeoAiPanel, type AssistantCopy } from "@/features/ceo-ai";

const ANSWER = `**129 animals were sold this month** — Coimbatore 125, Channapatna 4.

| Park | Sold | Avg weight (kg) | Revenue proxy | Pens | Last sale |
|---|---|---|---|---|---|
| Coimbatore | 125 | 31.8 | n/a | CBE Castro 1, CBE Castro 2, CBE Yashoda 3 | 22/09/2026 |
| Channapatna | 4 | 29.4 | n/a | CPT Castro 1 | 18/09/2026 |

- Counted by exit reason "sold", by exit date.
- No price view exists yet.

\`\`\`sql
select park_label, count(*) from ceo_ai.animals_base where exit_reason = 'sold' group by 1;
\`\`\`

\`\`\`chart
{"type":"bar","title":"Sold by park","x":["Coimbatore","Channapatna"],"series":[{"name":"Sold","data":[125,4]}]}
\`\`\``;

function sse(events: object[]): Response {
  const body = new ReadableStream({
    async start(c) {
      const enc = new TextEncoder();
      for (const e of events) {
        c.enqueue(enc.encode(`data: ${JSON.stringify(e)}\n\n`));
        await new Promise((r) => setTimeout(r, 250));
      }
      c.close();
    },
  });
  return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
}

function installMocks() {
  const real = window.fetch.bind(window);
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
    if (url.includes("/api/ceo-ai/conversations") && (!init || init.method === undefined || init.method === "GET"))
      return Response.json({ conversations: [{ id: "c1", title: "when did we do weighing?" }] });
    if (url.includes("/api/ceo-ai/conversations")) return Response.json({ id: "c2", title: "New chat" });
    if (url.includes("/api/ceo-ai/ask")) {
      const { chart, clean } = (() => {
        const m = ANSWER.match(/```chart\s*([\s\S]*?)```/);
        return { chart: m ? JSON.parse(m[1]) : undefined, clean: ANSWER.replace(m?.[0] ?? "", "").trim() };
      })();
      return sse([
        { type: "token", text: "Let me check the sales view first. " },
        { type: "reset" },
        { type: "progress", phase: "querying", label: "Querying" },
        ...clean.match(/[\s\S]{1,120}/g)!.map((t) => ({ type: "token", text: t })),
        { type: "final", answer: clean, mode: "agent", conversation_id: "c2", message_id: "m1", chart },
      ]);
    }
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
