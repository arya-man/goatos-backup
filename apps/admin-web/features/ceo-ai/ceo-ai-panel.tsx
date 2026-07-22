"use client";

import {
  MessageSquarePlus,
  PanelLeft,
  Pencil,
  Send,
  Sparkles,
  Square,
  ThumbsDown,
  ThumbsUp,
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
  sendFeedback,
} from "./ceo-ai-client";
import { CeoAiChart } from "./ceo-ai-chart";
import { CeoAiStyles, GoatAvatar, GoatWalking, MeshaLogo } from "./ceo-ai-styles";
import { CeoAiEvents, trackCeoAiError, trackCeoAiEvent } from "./telemetry";
import type { AssistantCopy, ChatMessage, ConversationSummary } from "./types";

// Local-literal chrome copy for the leadership-only assistant. No backend page
// contract exists for the assistant sidebar/feedback yet — documented exception
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
  helpful: "Helpful",
  notHelpful: "Not helpful",
  reasonPlaceholder: "What was wrong? (optional)",
  stop: "Stop generating",
  degraded: "Assistant temporarily unavailable",
  timedOut: "That took too long to answer. The assistant may be busy — please try again.",
  rateLimited: "You're asking a lot right now — please wait a moment and try again.",
  emptyReason: "No records found for the requested scope.",
} as const;

const PANEL_MARGIN = 14;
const PANEL_WIDTH = 640;

function modeLabel(mode: string | undefined, copy: AssistantCopy): string {
  if (!mode) return copy.modeFallback;
  switch (mode) {
    case "vertex":
    case "gemini":
    case "planned":
      return "Planned by Gemini via Vertex AI";
    case "cube":
      return "Cube governed metric";
    case "mesha-read-api":
    case "api":
      return "Mesha read API";
    case "toolbox":
      return "Mesha MCP Toolbox";
    case "sql":
      return "Governed read-only SQL";
    case "degraded":
      return CHROME.degraded;
    default:
      return mode;
  }
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
  const [banner, setBanner] = useState<{ kind: "err" | "warn"; text: string } | null>(null);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameText, setRenameText] = useState("");
  const [reasonFor, setReasonFor] = useState<string | null>(null);
  const [reasonText, setReasonText] = useState("");

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
                prev.map((m) => (m.id === assistantId ? { ...m, text: m.text + text } : m)),
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

  const applyFeedback = useCallback(
    async (message: ChatMessage, rating: "up" | "down") => {
      if (!message.messageId) return;
      setMessages((prev) => prev.map((m) => (m.id === message.id ? { ...m, feedback: rating } : m)));
      trackCeoAiEvent(CeoAiEvents.Feedback, { rating });
      if (rating === "down") {
        setReasonFor(message.id);
        setReasonText("");
      }
      await sendFeedback(message.messageId, rating).catch(() => false);
    },
    [],
  );

  const submitReason = useCallback(
    async (message: ChatMessage) => {
      const reason = reasonText.trim();
      setReasonFor(null);
      if (message.messageId && reason) {
        await sendFeedback(message.messageId, "down", reason).catch(() => false);
      }
    },
    [reasonText],
  );

  if (allowed !== true) return null;

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
                      <div className="mzai-skel" aria-label={copy.checking}>
                        <span />
                        <span />
                        <span />
                      </div>
                    ) : null}
                    {message.role === "assistant" && message.state === "complete" && message.citations?.length ? (
                      <div className="mzai-cites">
                        {message.citations.map((cite, i) => (
                          <span key={`${message.id}-c${i}`} className={`mzai-cite tier-${cite.tier ?? "api"}`}>
                            <b>{cite.surface}</b>
                            {cite.as_of ? ` · ${cite.as_of}` : ""}
                          </span>
                        ))}
                      </div>
                    ) : null}
                    {message.role === "assistant" && message.state === "complete" && message.id !== "hello" ? (
                      <div className="mzai-foot">
                        <span className={`mzai-mode${message.mode === "degraded" ? " degraded" : ""}`}>
                          {message.source ? `${message.source} · ` : ""}
                          {modeLabel(message.mode, copy)}
                        </span>
                        {message.messageId ? (
                          <span className="mzai-fb">
                            <button
                              type="button"
                              className={message.feedback === "up" ? "on-up" : ""}
                              aria-label={CHROME.helpful}
                              title={CHROME.helpful}
                              onClick={() => void applyFeedback(message, "up")}
                            >
                              <ThumbsUp className="ic" />
                            </button>
                            <button
                              type="button"
                              className={message.feedback === "down" ? "on-down" : ""}
                              aria-label={CHROME.notHelpful}
                              title={CHROME.notHelpful}
                              onClick={() => void applyFeedback(message, "down")}
                            >
                              <ThumbsDown className="ic" />
                            </button>
                          </span>
                        ) : null}
                      </div>
                    ) : null}
                      {reasonFor === message.id ? (
                        <div className="mzai-reason">
                          <input
                            autoFocus
                            value={reasonText}
                            placeholder={CHROME.reasonPlaceholder}
                            onChange={(e) => setReasonText(e.target.value)}
                            onKeyDown={(e) => {
                              if (e.key === "Enter") void submitReason(message);
                              if (e.key === "Escape") setReasonFor(null);
                            }}
                          />
                          <button type="button" onClick={() => void submitReason(message)}>
                            {CHROME.save}
                          </button>
                        </div>
                      ) : null}
                    </div>
                  </div>
                ))}
              </div>

              {banner ? <div className={`mzai-banner ${banner.kind}`}>{banner.text}</div> : null}

              {messages.length === 0 ? (
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
