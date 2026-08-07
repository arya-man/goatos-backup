// Client-side SSE reader for the leadership assistant answer stream.
//
// The admin-web proxy (/api/ceo-ai/ask) pipes the backend's text/event-stream
// straight through. Each SSE `data:` line is one JSON event:
//
//   { "type": "token",  "text": "Cas" }                       progressive token
//   { "type": "final",  "answer", "source", "mode",           terminal metadata
//                       "request_id", "conversation_id",
//                       "message_id", "citations": [...] }
//   { "type": "error",  "message": "..." }                    honest failure
//
// The reader never renders step traces / chain-of-thought — the backend only
// emits answer tokens + terminal metadata, and this reader forwards exactly
// those. If the backend answers with JSON instead of a stream (non-streaming
// fallback or an error envelope), `readCeoAiStream` detects it and emits a
// single final/error event so the caller has one code path.

export type CeoAiCitation = {
  surface: string;
  as_of?: string;
  tier?: "cube" | "api" | "toolbox" | "sql" | string;
  planned_by_model?: boolean;
};

// Optional, additive inline chart. Present only when the backend judged the
// answer plot-worthy and grounded it in a dimensioned series; points come
// verbatim from real Cube/tool rows. Absent for plain scalar answers.
export type CeoAiChartSeries = { name: string; data: number[] };
export type CeoAiChart = {
  type: "bar" | "line";
  title: string;
  x: string[];
  series: CeoAiChartSeries[];
};

export type CeoAiFinal = {
  answer: string;
  source?: string;
  mode?: string;
  request_id?: string;
  conversation_id?: string;
  message_id?: string;
  citations?: CeoAiCitation[];
  chart?: CeoAiChart;
};

// parseChart narrows an untrusted JSON value into a CeoAiChart, or undefined.
// Used on the non-streaming JSON fallback path (the SSE path spreads the parsed
// object, which already carries chart when present).
export function parseChart(raw: unknown): CeoAiChart | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const c = raw as Record<string, unknown>;
  if (c.type !== "bar" && c.type !== "line") return undefined;
  if (!Array.isArray(c.x) || !Array.isArray(c.series)) return undefined;
  const x = c.x.filter((v): v is string => typeof v === "string");
  const series = c.series
    .filter((s): s is Record<string, unknown> => !!s && typeof s === "object")
    .map((s) => ({
      name: typeof s.name === "string" ? s.name : "",
      data: Array.isArray(s.data) ? s.data.filter((n): n is number => typeof n === "number") : [],
    }));
  if (x.length < 2 || series.length === 0) return undefined;
  return {
    type: c.type,
    title: typeof c.title === "string" ? c.title : "",
    x,
    series,
  };
}

// Coarse pre-answer progress frame (planning / querying / synthesizing). It
// carries only a stable phase enum + a coarse route label — never step traces or
// chain-of-thought — so the UI can show progressive status while the grounded
// pipeline runs, instead of a frozen blank placeholder.
export type CeoAiProgress = { phase: string; label?: string };

export type CeoAiStreamEvent =
  | { type: "token"; text: string }
  | ({ type: "final" } & CeoAiFinal)
  | { type: "progress"; phase: string; label?: string }
  | { type: "error"; message: string; status?: number };

export type CeoAiStreamHandlers = {
  onToken?: (text: string) => void;
  onFinal?: (final: CeoAiFinal) => void;
  onProgress?: (progress: CeoAiProgress) => void;
  onError?: (message: string, status?: number) => void;
};

type AskPageScope = { park_id?: string; shed_id?: string };

type AskArgs = {
  question: string;
  conversationId?: string;
  locale?: string;
  pageScope?: AskPageScope;
  signal?: AbortSignal;
  // No-progress window (ms) after which a slow/unavailable Vertex is degraded
  // instead of hanging forever. The timer is armed before connect and re-armed
  // on every chunk, so a healthy progressive stream never trips it; only a real
  // stall (no headers, no token, no final for this long) does. Default 40s.
  idleTimeoutMs?: number;
};

// Default no-progress window. Full answers take ~20s and tokens then arrive
// continuously, so 40s of total silence is an unambiguous stall, not slowness.
const DEFAULT_IDLE_TIMEOUT_MS = 40_000;
const TIMEOUT_ERROR = "assistant_timeout";

function parseEvent(raw: string): CeoAiStreamEvent | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  const dataLines = trimmed
    .split("\n")
    .filter((line) => line.startsWith("data:"))
    .map((line) => line.slice(5).trim());
  if (dataLines.length === 0) return null;
  const payload = dataLines.join("\n");
  if (payload === "[DONE]") return null;
  try {
    const obj = JSON.parse(payload) as Record<string, unknown>;
    const type = obj.type;
    if (type === "token" && typeof obj.text === "string") {
      return { type: "token", text: obj.text };
    }
    if (type === "progress" && typeof obj.phase === "string") {
      return { type: "progress", phase: obj.phase, label: typeof obj.label === "string" ? obj.label : undefined };
    }
    if (type === "error") {
      return { type: "error", message: typeof obj.message === "string" ? obj.message : "assistant_error" };
    }
    if (typeof obj.answer === "string" || type === "final") {
      return { type: "final", ...(obj as unknown as CeoAiFinal) };
    }
    return null;
  } catch {
    return null;
  }
}

// readCeoAiStream POSTs the question through the admin-web proxy and drives the
// handlers as the answer streams in. Resolves to the terminal metadata. It is
// abortable via `signal` (stop-generating).
export async function readCeoAiStream(
  args: AskArgs,
  handlers: CeoAiStreamHandlers,
): Promise<CeoAiFinal | null> {
  // Compose the caller's stop signal with an internal no-progress timeout. Any
  // trip aborts the same fetch; `timedOut` tells the two apart so a stall
  // degrades honestly while a user Stop stays a user abort.
  const idleMs = args.idleTimeoutMs ?? DEFAULT_IDLE_TIMEOUT_MS;
  const internal = new AbortController();
  let timedOut = false;
  let idleTimer: ReturnType<typeof setTimeout> | undefined;

  const armIdle = () => {
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = setTimeout(() => {
      timedOut = true;
      internal.abort();
    }, idleMs);
  };
  const clearIdle = () => {
    if (idleTimer) clearTimeout(idleTimer);
    idleTimer = undefined;
  };

  const onCallerAbort = () => internal.abort();
  if (args.signal) {
    if (args.signal.aborted) internal.abort();
    else args.signal.addEventListener("abort", onCallerAbort, { once: true });
  }

  try {
    armIdle();
    return await readComposed(args, handlers, internal.signal, armIdle, clearIdle);
  } catch (error: unknown) {
    // A no-progress timeout is an honest degraded state, not a thrown failure —
    // surface it through onError so the caller renders it like any backend
    // error. A genuine user Stop (caller signal) is rethrown for the caller.
    if (timedOut && !(args.signal?.aborted ?? false)) {
      handlers.onError?.(TIMEOUT_ERROR, 504);
      return null;
    }
    throw error;
  } finally {
    clearIdle();
    args.signal?.removeEventListener("abort", onCallerAbort);
  }
}

async function readComposed(
  args: AskArgs,
  handlers: CeoAiStreamHandlers,
  signal: AbortSignal,
  armIdle: () => void,
  clearIdle: () => void,
): Promise<CeoAiFinal | null> {
  const response = await fetch("/api/ceo-ai/ask", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      question: args.question,
      conversation_id: args.conversationId,
      locale: args.locale,
      page_scope: args.pageScope,
      stream: true,
    }),
    signal,
  });
  // Headers arrived: connect succeeded, re-arm for the first token/body.
  armIdle();

  const contentType = response.headers.get("Content-Type") ?? "";

  // JSON path: non-streaming fallback or an error envelope.
  if (!contentType.includes("text/event-stream")) {
    let body: Record<string, unknown> = {};
    try {
      body = (await response.json()) as Record<string, unknown>;
    } catch {
      body = {};
    }
    if (!response.ok) {
      const message =
        typeof body.message === "string"
          ? body.message
          : typeof body.error === "string"
            ? body.error
            : `assistant_http_${response.status}`;
      handlers.onError?.(message, response.status);
      return null;
    }
    const final: CeoAiFinal = {
      answer: typeof body.answer === "string" ? body.answer : "",
      source: typeof body.source === "string" ? body.source : undefined,
      mode: typeof body.mode === "string" ? body.mode : undefined,
      request_id: typeof body.request_id === "string" ? body.request_id : undefined,
      conversation_id: typeof body.conversation_id === "string" ? body.conversation_id : undefined,
      message_id: typeof body.message_id === "string" ? body.message_id : undefined,
      citations: Array.isArray(body.citations) ? (body.citations as CeoAiCitation[]) : undefined,
      chart: parseChart(body.chart),
    };
    if (final.answer) handlers.onToken?.(final.answer);
    handlers.onFinal?.(final);
    return final;
  }

  if (!response.body) {
    handlers.onError?.("assistant_empty_stream", response.status);
    return null;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let final: CeoAiFinal | null = null;

  const flush = (chunk: string) => {
    buffer += chunk;
    let idx = buffer.indexOf("\n\n");
    while (idx !== -1) {
      const rawEvent = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 2);
      const event = parseEvent(rawEvent);
      if (event) {
        if (event.type === "token") {
          handlers.onToken?.(event.text);
        } else if (event.type === "progress") {
          handlers.onProgress?.({ phase: event.phase, label: event.label });
        } else if (event.type === "final") {
          const { type: _t, ...rest } = event;
          void _t;
          final = rest;
          handlers.onFinal?.(rest);
        } else if (event.type === "error") {
          handlers.onError?.(event.message, event.status);
        }
      }
      idx = buffer.indexOf("\n\n");
    }
  };

  for (;;) {
    // serial-await: allow sequential SSE chunk reads must be consumed in order as they stream in; there is no batch to parallelize
    const { done, value } = await reader.read();
    if (done) break;
    // Progress: re-arm the no-progress timeout on every chunk (tokens AND
    // heartbeat comments), so only real silence trips the timeout.
    armIdle();
    flush(decoder.decode(value, { stream: true }));
  }
  if (buffer.trim()) flush("\n\n");
  clearIdle();

  return final;
}
