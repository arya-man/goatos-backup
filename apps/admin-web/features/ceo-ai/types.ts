import type { CeoAiChart, CeoAiCitation, CeoAiFinal } from "@/lib/ceo-ai-stream";

export type { CeoAiChart, CeoAiCitation, CeoAiFinal };

// One rendered chat turn. Assistant turns accumulate streamed tokens into
// `text` and gain terminal metadata (source/mode/citations/message_id) on
// completion. `state` drives the per-message UI (streaming skeleton, error).
export type ChatMessageState = "streaming" | "complete" | "error";

export type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
  state: ChatMessageState;
  source?: string;
  mode?: string;
  requestId?: string;
  messageId?: string;
  citations?: CeoAiCitation[];
  chart?: CeoAiChart;
  feedback?: "up" | "down";
  // Coarse live progress status shown under the streaming placeholder before the
  // first answer token (planning / querying / synthesizing). Cleared once answer
  // text arrives. Never carries chain-of-thought — only a coarse route label.
  progress?: string;
};

// A conversation thread summary in the sidebar (backend-owned list).
export type ConversationSummary = {
  id: string;
  title: string;
  updated_at?: string;
};

// Backend messages payload row (for resuming a thread).
export type StoredMessage = {
  id?: string;
  message_id?: string;
  role?: string;
  content?: string;
  source?: string;
  mode?: string;
  request_id?: string;
  citations?: CeoAiCitation[];
};

// All user-visible copy for the assistant surface. The subset shared with the
// backend admin-web contract (title/subtitle/starters/etc.) is passed in from
// `mesha-shell`; the thread/feedback/state strings are local literals for this
// leadership-only surface, documented as an exception in
// context/frontend/admin-web-backend-ui-contract.md (Verification-style: no
// backend page contract for the assistant chrome exists yet).
export type AssistantCopy = {
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
