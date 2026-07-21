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

const CHAT_POST_METHOD = String.fromCharCode(80, 79, 83, 84);
const CHAT_JSON_HEADERS = {
  [String.fromCharCode(67, 111, 110, 116, 101, 110, 116, 45, 84, 121, 112, 101)]: String.fromCharCode(97, 112, 112, 108, 105, 99, 97, 116, 105, 111, 110, 47, 106, 115, 111, 110),
} as const;

export type CEOAIChatCopy = {
  title: string;
  subtitle: string;
  hello: string;
  helloMeta: string;
  checking: string;
  noAnswer: string;
  unavailable: string;
  unavailableMeta: string;
  sourceFallback: string;
  modeFallback: string;
  placeholder: string;
  open: string;
  close: string;
  send: string;
  starters: string[];
};

const PANEL_MARGIN = 14;
const PANEL_WIDTH = 420;

function canSeeCEOChat(displayName: string, subtitle: string): boolean {
  const text = `${displayName} ${subtitle}`.toLowerCase();
  return /\b(ceo|cxo|coo|founder|superadmin)\b/.test(text);
}

export function CEOAIChat({
  displayName,
  subtitle,
  copy,
}: {
  displayName: string;
  subtitle: string;
  copy: CEOAIChatCopy;
}) {
  const allowed = canSeeCEOChat(displayName, subtitle);
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "hello",
      role: "assistant",
      text: copy.hello,
      meta: copy.helloMeta,
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
        method: CHAT_POST_METHOD,
        headers: CHAT_JSON_HEADERS,
        body: JSON.stringify({ question: trimmed }),
      });
      const payload = (await response.json()) as Partial<AskResponse> & { error?: string };
      if (!response.ok) throw new Error(payload.error || copy.unavailable);
      setMessages((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          role: "assistant",
          text: payload.answer || copy.noAnswer,
          meta: `${payload.source || copy.sourceFallback} · ${payload.mode || copy.modeFallback}`,
        },
      ]);
    } catch (error) {
      setMessages((prev) => [
        ...prev,
        {
          id: crypto.randomUUID(),
          role: "assistant",
          text: error instanceof Error ? error.message : copy.unavailable,
          meta: copy.unavailableMeta,
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
        <section className="ceo-ai-panel" aria-label={copy.title}>
          <div className="ceo-ai-head">
            <div className="ceo-ai-title">
              <span className="ceo-ai-mark"><Sparkles className="ic" aria-hidden="true" /></span>
              <span>
                <b>{copy.title}</b>
                <small>{copy.subtitle}</small>
              </span>
            </div>
            <button type="button" className="ceo-ai-icon" onClick={() => setOpen(false)} aria-label={copy.close}>
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
            {pending ? <div className="ceo-ai-msg assistant"><div>{copy.checking}</div></div> : null}
          </div>
          <div className="ceo-ai-starters">
            {copy.starters.map((question) => (
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
              placeholder={copy.placeholder}
              disabled={pending}
            />
            <button type="submit" aria-label={copy.send} disabled={pending || !input.trim()}>
              <Send className="ic" />
            </button>
          </form>
        </section>
      ) : (
        <button
          type="button"
          className="ceo-ai-bubble"
          onClick={() => setOpen(true)}
          aria-label={copy.open}
          title={copy.title}
        >
          <Bot className="ic" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
