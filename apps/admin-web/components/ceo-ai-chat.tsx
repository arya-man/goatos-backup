"use client";

import { Bot, Send, Sparkles, X } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";

type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
  meta?: string;
};

type AskResponse = {
  answer: string;
  source: string;
  mode: string;
};

const starterQuestions = [
  "today vaccination due by shed",
  "which sheds are overdue?",
  "show current animal count summary",
  "what can you answer right now?",
];

const PANEL_MARGIN = 14;
const PANEL_WIDTH = 420;

function canSeeCEOChat(displayName: string, subtitle: string): boolean {
  const text = `${displayName} ${subtitle}`.toLowerCase();
  return /\b(ceo|cxo|coo|founder|superadmin)\b/.test(text);
}

export function CEOAIChat({
  displayName,
  subtitle,
}: {
  displayName: string;
  subtitle: string;
}) {
  const allowed = canSeeCEOChat(displayName, subtitle);
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "hello",
      role: "assistant",
      text: "Ask about Mesha operational data. This first version is read-only and leadership-only.",
      meta: "CEO/CXO analyst",
    },
  ]);
  const [input, setInput] = useState("");
  const [pending, setPending] = useState(false);
  const scrollRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages, open]);

  if (!allowed) return null;

  const rootStyle = open
    ? {
        right: PANEL_MARGIN,
        bottom: PANEL_MARGIN,
        width: `min(${PANEL_WIDTH}px, calc(100vw - ${PANEL_MARGIN * 2}px))`,
        height: `min(610px, calc(100vh - ${PANEL_MARGIN * 2}px))`,
      }
    : { right: 24, bottom: 24 };

  async function ask(question: string) {
    const trimmed = question.trim();
    if (!trimmed || pending) return;
    setInput("");
    setPending(true);
    setMessages((prev) => [...prev, { id: crypto.randomUUID(), role: "user", text: trimmed }]);
    try {
      const response = await fetch("/api/ceo-ai/ask", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ question: trimmed }),
      });
      const payload = (await response.json()) as Partial<AskResponse> & { error?: string };
      if (!response.ok) throw new Error(payload.error || "assistant_unavailable");
      setMessages((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          role: "assistant",
          text: payload.answer || "No answer returned.",
          meta: `${payload.source || "Mesha"} · ${payload.mode || "read-only"}`,
        },
      ]);
    } catch (error) {
      setMessages((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          role: "assistant",
          text: error instanceof Error ? error.message : "The assistant could not answer right now.",
          meta: "unavailable",
        },
      ]);
    } finally {
      setPending(false);
    }
  }

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void ask(input);
  }

  return (
    <div className="ceo-ai" style={rootStyle}>
      {open ? (
        <section className="ceo-ai-panel" aria-label="Ask Mesha">
          <div className="ceo-ai-head">
            <div className="ceo-ai-title">
              <span className="ceo-ai-mark"><Sparkles className="ic" aria-hidden="true" /></span>
              <span>
                <b>Ask Mesha</b>
                <small>CEO/CXO · read-only</small>
              </span>
            </div>
            <button type="button" className="ceo-ai-icon" onClick={() => setOpen(false)} aria-label="Close Ask Mesha">
              <X className="ic" />
            </button>
          </div>
          <div ref={scrollRef} className="ceo-ai-log">
            {messages.map((message) => (
              <div key={message.id} className={`ceo-ai-msg ${message.role}`}>
                <div>{message.text}</div>
                {message.meta ? <small>{message.meta}</small> : null}
              </div>
            ))}
            {pending ? <div className="ceo-ai-msg assistant"><div>Checking Mesha data...</div></div> : null}
          </div>
          <div className="ceo-ai-starters">
            {starterQuestions.map((question) => (
              <button key={question} type="button" onClick={() => void ask(question)} disabled={pending}>
                {question}
              </button>
            ))}
          </div>
          <form className="ceo-ai-form" onSubmit={onSubmit}>
            <textarea
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter" && !event.shiftKey) {
                  event.preventDefault();
                  event.currentTarget.form?.requestSubmit();
                }
              }}
              rows={2}
              placeholder="Ask about counts, vaccination, feed, shifting..."
              disabled={pending}
            />
            <button type="submit" aria-label="Send question" disabled={pending || !input.trim()}>
              <Send className="ic" />
            </button>
          </form>
        </section>
      ) : (
        <button
          type="button"
          className="ceo-ai-bubble"
          onClick={() => setOpen(true)}
          aria-label="Open Ask Mesha"
          title="Ask Mesha"
        >
          <Bot className="ic" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
