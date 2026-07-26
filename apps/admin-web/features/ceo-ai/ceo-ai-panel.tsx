"use client";

import {
  MessageSquarePlus,
  PanelLeft,
  Pencil,
  Send,
  Sparkles,
  Trash2,
  X,
} from "lucide-react";
import { type FormEvent, type ReactElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { readCeoAiStream } from "@/lib/ceo-ai-stream";
import {
  createConversation,
  deleteConversation,
  listConversations,
  loadConversationMessages,
  probeCapability,
  renameConversation,
} from "./ceo-ai-client";
import { CeoAiChart } from "./ceo-ai-chart";
import { CeoAiStyles, GoatAvatar, GoatWalking, MeshaLogo } from "./ceo-ai-styles";
import { CeoAiEvents, trackCeoAiError, trackCeoAiEvent } from "./telemetry";
import type { AssistantCopy, ChatMessage, ConversationSummary } from "./types";

// Local-literal chrome copy for the leadership-only assistant. No backend page
// contract exists for the assistant sidebar yet — documented exception
// (Verification-screen precedent) in
// context/frontend/admin-web-backend-ui-contract.md.
const CHROME = {
  threads: "Chats",
  newChat: "New chat",
  toggleThreads: "Toggle chat history",
  noThreads: "Your chats will appear here.",
  rename: "Rename",
  delete: "Delete",
  save: "Save",
  stop: "Stop generating",
  degraded: "Assistant temporarily unavailable",
  timedOut: "That took too long to answer. The assistant may be busy — please try again.",
  rateLimited: "You're asking a lot right now — please wait a moment and try again.",
  emptyReason: "No records found for the requested scope.",
  liveData: "Live data",
  planning: "Planning your answer…",
  querying: "Consulting Mesha data…",
  synthesizing: "Composing the answer…",
} as const;

// progressStatusLabel maps a coarse backend progress frame to a friendly status
// line. It leads with the phase copy and, for the querying phase, appends the
// coarse route label the backend supplied (e.g. "Consulting Cube ·
// vaccination_overdue"). It never renders reasoning/chain-of-thought — the
// backend frame carries only phase + a route tag.
function progressStatusLabel(progress: { phase: string; label?: string }): string {
  switch (progress.phase) {
    case "planning":
      return CHROME.planning;
    case "querying":
      return progress.label && progress.label.trim() ? progress.label : CHROME.querying;
    case "synthesizing":
      return CHROME.synthesizing;
    default:
      return CHROME.querying;
  }
}

const PANEL_MARGIN = 14;
const PANEL_WIDTH = 640;

// modeLabel is the small footer provenance tag. It is CEO-facing, so it never
// leaks the planner/route internals ("Planned by Gemini via Vertex AI", "Cube",
// "MCP Toolbox", "read-only SQL") — those stay in the admin trace + audit only.
// Every healthy grounded answer reads as a neutral "Live data" freshness tag;
// only the degraded state carries its own message.
function modeLabel(mode: string | undefined, copy: AssistantCopy): string {
  if (!mode) return copy.modeFallback;
  switch (mode) {
    case "degraded":
      return CHROME.degraded;
    case "refused":
      return copy.modeFallback;
    default:
      return CHROME.liveData;
  }
}

// A raw metric id / plumbing-prefixed surface must never reach the CEO chip. The
// backend now sends clean business labels, but stored conversations from before
// that change (and any future adapter that forgets) may still carry
// "Cube · active_animals" or a snake_case id — defensively strip the plumbing
// prefix and humanize a residual snake_case token so the chip stays clean.
const PLUMBING_PREFIX = /^(cube|mesha mcp toolbox|mesha read-only sql fallback|mesha read model|toolbox|sql)\s*·?\s*/i;
function formatCitationSurface(surface: string | undefined): string {
  const raw = (surface ?? "").trim();
  if (!raw) return "Mesha operational data";
  const stripped = raw.replace(PLUMBING_PREFIX, "").trim();
  const base = stripped || raw;
  if (/^[a-z0-9]+(_[a-z0-9]+)+$/.test(base)) {
    return base
      .split("_")
      .map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w))
      .join(" ");
  }
  return base;
}

// formatSource sanitizes the footer source string for the CEO. The backend now
// emits clean business labels, but a stored history turn may still carry
// "Cube · <id>" plumbing joined by " · "; strip the route tokens and humanize any
// residual snake_case segment so the footer never leaks Cube/Toolbox/SQL/metric
// ids. Returns "" when nothing business-meaningful survives.
const PLUMBING_TOKENS = new Set([
  "cube",
  "mesha mcp toolbox",
  "mesha read-only sql fallback",
  "mesha read model",
  "toolbox",
  "sql",
  "api",
]);
function humanizeToken(s: string): string {
  if (/^[a-z0-9]+(_[a-z0-9]+)+$/.test(s)) {
    return s
      .split("_")
      .map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w))
      .join(" ");
  }
  return s;
}
function formatSource(source: string | undefined): string {
  const raw = (source ?? "").trim();
  if (!raw) return "";
  const seen = new Set<string>();
  const parts = raw
    .split("·")
    .map((s) => s.trim())
    .filter(Boolean)
    .filter((s) => !PLUMBING_TOKENS.has(s.toLowerCase())) // drop bare route tokens (old history)
    .map(humanizeToken)
    .filter((s) => {
      const key = s.toLowerCase();
      if (!s || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
  return parts.join(" · ");
}

// formatFreshness turns the raw ISO/microsecond as_of into a friendly short IST
// phrase — "just now", "N min ago", "as of 3:10 PM" (today), or a short date —
// so the CEO never sees an ISO timestamp, microseconds, or a +05:30 offset.
function formatFreshness(asOf: string | undefined): string {
  const raw = (asOf ?? "").trim();
  if (!raw) return "";
  const then = new Date(raw);
  if (Number.isNaN(then.getTime())) return "";
  const now = Date.now();
  const diffMs = now - then.getTime();
  const diffMin = Math.floor(diffMs / 60_000);
  if (diffMs >= 0 && diffMin < 1) return "just now";
  if (diffMs >= 0 && diffMin < 60) return `${diffMin} min ago`;
  const ist = "Asia/Kolkata";
  const sameDay =
    new Intl.DateTimeFormat("en-CA", { timeZone: ist, year: "numeric", month: "2-digit", day: "2-digit" }).format(then) ===
    new Intl.DateTimeFormat("en-CA", { timeZone: ist, year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(now));
  if (sameDay) {
    const t = new Intl.DateTimeFormat("en-US", { timeZone: ist, hour: "numeric", minute: "2-digit", hour12: true }).format(then);
    return `as of ${t}`;
  }
  return new Intl.DateTimeFormat("en-US", { timeZone: ist, month: "short", day: "numeric" }).format(then);
}

function newId(): string {
  return typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : `id-${Date.now()}-${Math.random()}`;
}

export function CeoAiPanel({ copy }: { copy: AssistantCopy }): ReactElement | null {
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [starters, setStarters] = useState<string[]>(copy.starters);
  const [open, setOpen] = useState(false);
  const [showThreads, setShowThreads] = useState(true);
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [conversationId, setConversationId] = useState<string | undefined>(undefined);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [pending, setPending] = useState(false);
  const [showStarters, setShowStarters] = useState(true);
  const [banner, setBanner] = useState<{ kind: "err" | "warn"; text: string } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameText, setRenameText] = useState("");

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Leadership capability probe (server-authoritative; replaces client regex).
  useEffect(() => {
    const controller = new AbortController();
    probeCapability(controller.signal).then((result) => {
      setAllowed(result.allowed);
      if (result.starters.length) setStarters(result.starters);
    });
    return () => controller.abort();
  }, []);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages, open, pending]);

  const refreshThreads = useCallback(() => {
    listConversations().then((res) => setConversations(res.conversations)).catch(() => undefined);
  }, []);

  useEffect(() => {
    if (open) refreshThreads();
  }, [open, refreshThreads]);

  useEffect(() => {
    if (!open) return undefined;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = previous;
    };
  }, [open]);

  const helloMessage = useMemo<ChatMessage>(
    () => ({ id: "hello", role: "assistant", text: copy.hello, state: "complete", source: copy.helloMeta }),
    [copy.hello, copy.helloMeta],
  );

  const shown = messages.length ? messages : [helloMessage];

  const stopGenerating = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setPending(false);
    trackCeoAiEvent(CeoAiEvents.StopGenerating);
  }, []);

  const ask = useCallback(
    async (raw: string) => {
      const question = raw.trim();
      if (!question || pending) return;
      setInput("");
      setBanner(null);
      setPending(true);
      setShowStarters(false);
      trackCeoAiEvent(CeoAiEvents.Ask, { streaming: "true" });

      const assistantId = newId();
      setMessages((prev) => [
        ...prev,
        { id: newId(), role: "user", text: question, state: "complete" },
        { id: assistantId, role: "assistant", text: "", state: "streaming" },
      ]);

      const controller = new AbortController();
      abortRef.current = controller;

      const patch = (fields: Partial<ChatMessage>) =>
        setMessages((prev) => prev.map((m) => (m.id === assistantId ? { ...m, ...fields } : m)));

      try {
        const final = await readCeoAiStream(
          { question, conversationId, signal: controller.signal },
          {
            onToken: (text) =>
              setMessages((prev) =>
                prev.map((m) =>
                  // First token clears the coarse progress status; the answer body takes over.
                  m.id === assistantId ? { ...m, text: m.text + text, progress: undefined } : m,
                ),
              ),
            onProgress: (progress) =>
              setMessages((prev) =>
                prev.map((m) =>
                  m.id === assistantId && !m.text
                    ? { ...m, progress: progressStatusLabel(progress) }
                    : m,
                ),
              ),
            onError: (message, status) => {
              if (status === 429) {
                setBanner({ kind: "warn", text: CHROME.rateLimited });
                patch({ text: CHROME.rateLimited, state: "error" });
              } else if (status === 504) {
                setBanner({ kind: "warn", text: CHROME.timedOut });
                patch({ text: CHROME.timedOut, state: "error", mode: "degraded" });
              } else if (status === 401) {
                setBanner({ kind: "err", text: copy.unavailable });
                patch({ text: copy.unavailable, state: "error" });
              } else {
                setBanner({ kind: "err", text: CHROME.degraded });
                patch({ text: message || CHROME.degraded, state: "error", mode: "degraded" });
              }
              trackCeoAiError("ask", message, status);
            },
          },
        );

        if (final) {
          if (final.conversation_id && final.conversation_id !== conversationId) {
            setConversationId(final.conversation_id);
            refreshThreads();
          }
          patch({
            text: final.answer || copy.noAnswer,
            state: "complete",
            source: final.source ?? copy.sourceFallback,
            mode: final.mode,
            requestId: final.request_id,
            messageId: final.message_id,
            citations: final.citations,
            chart: final.chart,
          });
          trackCeoAiEvent(CeoAiEvents.Answer, {
            mode: final.mode ?? "unknown",
            grounded: final.citations?.length ? "true" : "false",
          });
        }
      } catch (error: unknown) {
        if (controller.signal.aborted) {
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? { ...m, text: m.text || "…", state: "complete" }
                : m,
            ),
          );
        } else {
          const message = error instanceof Error ? error.message : "assistant_error";
          setBanner({ kind: "err", text: CHROME.degraded });
          patch({ text: CHROME.degraded, state: "error", mode: "degraded" });
          trackCeoAiError("ask_throw", message);
        }
      } finally {
        abortRef.current = null;
        setPending(false);
      }
    },
    [conversationId, copy, pending, refreshThreads],
  );

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void ask(input);
  };

  const startNewChat = useCallback(async () => {
    stopGenerating();
    setMessages([]);
    setConversationId(undefined);
    setBanner(null);
    setShowStarters(true);
    trackCeoAiEvent(CeoAiEvents.NewChat);
    const created = await createConversation().catch(() => null);
    if (created?.id) {
      setConversationId(created.id);
      refreshThreads();
    }
  }, [refreshThreads, stopGenerating]);

  const resumeThread = useCallback(
    async (id: string) => {
      if (id === conversationId) return;
      stopGenerating();
      setConversationId(id);
      setBanner(null);
      setShowStarters(false);
      trackCeoAiEvent(CeoAiEvents.ResumeChat);
      const stored = await loadConversationMessages(id).catch(() => []);
      setMessages(
        stored.map((m) => ({
          id: m.id ?? m.message_id ?? newId(),
          role: m.role === "user" ? "user" : "assistant",
          text: m.content ?? "",
          state: "complete",
          source: m.source,
          mode: m.mode,
          requestId: m.request_id,
          messageId: m.message_id ?? m.id,
          citations: m.citations,
        })),
      );
    },
    [conversationId, stopGenerating],
  );

  const removeThread = useCallback(
    async (id: string) => {
      const ok = await deleteConversation(id).catch(() => false);
      if (ok) {
        trackCeoAiEvent(CeoAiEvents.DeleteChat);
        if (id === conversationId) {
          setConversationId(undefined);
          setMessages([]);
          setShowStarters(true);
        }
        refreshThreads();
      }
    },
    [conversationId, refreshThreads],
  );

  const commitRename = useCallback(
    async (id: string) => {
      const title = renameText.trim();
      setRenaming(null);
      if (title) {
        await renameConversation(id, title).catch(() => false);
        refreshThreads();
      }
    },
    [renameText, refreshThreads],
  );

  if (allowed !== true) return null;

  const startersVisible = showStarters || messages.length === 0;

  const rootStyle = open
    ? {
        right: PANEL_MARGIN,
        bottom: PANEL_MARGIN,
        width: `min(${PANEL_WIDTH}px, calc(100vw - ${PANEL_MARGIN * 2}px))`,
        height: `min(640px, calc(100vh - ${PANEL_MARGIN * 2}px))`,
      }
    : { right: 24, bottom: 24 };

  return (
    <div className="mzai-root" style={rootStyle}>
      <CeoAiStyles />
      {open ? (
        <section className="mzai-panel" aria-label={copy.title}>
          <div className="mzai-head">
            <button
              type="button"
              className="mzai-icon"
              aria-pressed={showThreads}
              onClick={() => setShowThreads((v) => !v)}
              aria-label={CHROME.toggleThreads}
              title={CHROME.toggleThreads}
            >
              <PanelLeft className="ic" />
            </button>
            <span className="mzai-mark">
              <MeshaLogo width={20} height={20} />
            </span>
            <span className="mzai-htext">
              <b>{copy.title}</b>
              <small>{copy.subtitle}</small>
            </span>
            <div className="mzai-hbtns">
              <button type="button" className="mzai-icon" onClick={() => setOpen(false)} aria-label={copy.close}>
                <X className="ic" />
              </button>
            </div>
          </div>

          <div className="mzai-body">
            <aside className={`mzai-side${showThreads ? "" : " mzai-hide"}`}>
              <div className="mzai-side-head">
                <span>{CHROME.threads}</span>
                <button type="button" className="mzai-newbtn" onClick={() => void startNewChat()}>
                  <MessageSquarePlus className="ic" /> {CHROME.newChat}
                </button>
              </div>
              <div className="mzai-threads">
                {conversations.length === 0 ? (
                  <div className="mzai-side-empty">{CHROME.noThreads}</div>
                ) : (
                  conversations.map((thread) => (
                    <div
                      key={thread.id}
                      className={`mzai-thread${thread.id === conversationId ? " mzai-on" : ""}`}
                      onClick={() => void resumeThread(thread.id)}
                    >
                      {renaming === thread.id ? (
                        <input
                          autoFocus
                          value={renameText}
                          onClick={(e) => e.stopPropagation()}
                          onChange={(e) => setRenameText(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") void commitRename(thread.id);
                            if (e.key === "Escape") setRenaming(null);
                          }}
                          onBlur={() => void commitRename(thread.id)}
                        />
                      ) : (
                        <span className="mzai-tt" title={thread.title}>
                          {thread.title || CHROME.newChat}
                        </span>
                      )}
                      <button
                        type="button"
                        className="mzai-thread-act"
                        aria-label={CHROME.rename}
                        title={CHROME.rename}
                        onClick={(e) => {
                          e.stopPropagation();
                          setRenaming(thread.id);
                          setRenameText(thread.title);
                        }}
                      >
                        <Pencil className="ic" />
                      </button>
                      <button
                        type="button"
                        className="mzai-thread-act"
                        aria-label={CHROME.delete}
                        title={CHROME.delete}
                        onClick={(e) => {
                          e.stopPropagation();
                          void removeThread(thread.id);
                        }}
                      >
                        <Trash2 className="ic" />
                      </button>
                    </div>
                  ))
                )}
              </div>
            </aside>

            <div className="mzai-main">
              <div ref={scrollRef} className="mzai-log">
                {shown.map((message) => (
                  <div key={message.id} className={`mzai-msg-wrap ${message.role}`}>
                    {message.role === "assistant" && (
                      <div className="mzai-avatar">
                        <MeshaLogo width={32} height={32} />
                      </div>
                    )}
                    <div className={`mzai-msg ${message.role} ${message.state}`}>
                      <div className="mzai-bub">
                        {message.text}
                        {message.state === "streaming" && message.text ? <span className="mzai-caret" /> : null}
                      </div>
                    {message.role === "assistant" && message.state === "complete" && message.chart ? (
                      <CeoAiChart chart={message.chart} />
                    ) : null}
                    {message.state === "streaming" && !message.text ? (
                      <div className="mzai-progress">
                        <div className="mzai-skel" aria-label={copy.checking}>
                          <span />
                          <span />
                          <span />
                        </div>
                        {message.progress ? (
                          <span className="mzai-progress-label" aria-live="polite">
                            {message.progress}
                          </span>
                        ) : null}
                      </div>
                    ) : null}
                    {message.role === "assistant" && message.state === "complete" && message.citations?.length ? (
                      <div className="mzai-cites">
                        {message.citations.map((cite, i) => {
                          const freshness = formatFreshness(cite.as_of);
                          return (
                            <span key={`${message.id}-c${i}`} className={`mzai-cite tier-${cite.tier ?? "api"}`}>
                              <b>{formatCitationSurface(cite.surface)}</b>
                              {freshness ? ` · ${freshness}` : ""}
                            </span>
                          );
                        })}
                      </div>
                    ) : null}
                    {message.role === "assistant" && message.state === "complete" && message.id !== "hello" ? (
                      <div className="mzai-foot">
                        <span className={`mzai-mode${message.mode === "degraded" ? " degraded" : ""}`}>
                          {formatSource(message.source) ? `${formatSource(message.source)} · ` : ""}
                          {modeLabel(message.mode, copy)}
                        </span>
                      </div>
                    ) : null}
                    </div>
                  </div>
                ))}
              </div>

              {banner ? <div className={`mzai-banner ${banner.kind}`}>{banner.text}</div> : null}

              {messages.length > 0 ? (
                <div className="mzai-suggestbar">
                  <button type="button" onClick={() => setShowStarters((v) => !v)} aria-expanded={startersVisible}>
                    <Sparkles className="ic" />
                    Suggestions
                  </button>
                </div>
              ) : null}

              {startersVisible ? (
                <div className="mzai-starters">
                  {starters.map((question) => (
                    <button
                      key={question}
                      type="button"
                      disabled={pending}
                      onClick={() => {
                        trackCeoAiEvent(CeoAiEvents.StarterClick);
                        void ask(question);
                      }}
                    >
                      {question}
                    </button>
                  ))}
                </div>
              ) : null}

              <form className="mzai-form" onSubmit={onSubmit}>
                <textarea
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !e.shiftKey) {
                      e.preventDefault();
                      e.currentTarget.form?.requestSubmit();
                    }
                  }}
                  rows={1}
                  placeholder={copy.placeholder}
                />
                {pending ? (
                  <button type="button" className="mzai-send stop" onClick={stopGenerating} aria-label={CHROME.stop} title={CHROME.stop}>
                    <GoatWalking />
                  </button>
                ) : (
                  <button type="submit" className="mzai-send" aria-label={copy.send} disabled={!input.trim()}>
                    <Send className="ic" />
                  </button>
                )}
              </form>
            </div>
          </div>
        </section>
      ) : (
        <>
          <CeoAiStyles />
          <button
            type="button"
            className="mzai-bubble"
            onClick={() => {
              setOpen(true);
              trackCeoAiEvent(CeoAiEvents.Open);
            }}
            aria-label={copy.open}
            title={copy.title}
          >
            <GoatAvatar />
          </button>
        </>
      )}
    </div>
  );
}
