"use client";

import { CopyButton } from "./ceo-ai-copy-button";
import { CeoAiMarkdown, preloadCeoAiMarkdown } from "./ceo-ai-markdown-lazy";
import {
  Lightbox,
  type PreviewFile,
  Thumb,
  revokePreviews,
  shrinkImage,
  toPreview,
} from "./ceo-ai-attachments";
import {
  Check,
  ChevronUp,
  Maximize2,
  MessageSquarePlus,
  Mic,
  MicOff,
  Paperclip,
  Minimize2,
  Minus,
  PanelLeft,
  Pencil,
  Send,
  Sparkles,
  Square,
  Trash2,
  X,
} from "lucide-react";
import {
  type FormEvent,
  type ReactElement,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  readCeoAiStream,
  sendCeoAiStopSignal,
  sendCeoAiWatchStop,
} from "@/lib/ceo-ai-stream";
import { CeoAiWatchCard, mergeWatch } from "./ceo-ai-watch";
import { createPortal } from "react-dom";
import {
  createConversation,
  deleteConversation,
  listConversations,
  loadConversationMessages,
  probeCapability,
  renameConversation,
} from "./ceo-ai-client";
import { CeoAiChart } from "./ceo-ai-chart";
import { CeoAiStyles, GoatAvatar, MeshaLogo } from "./ceo-ai-styles";
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
  stoppedEmpty: "_Stopped before an answer._",
  cutOff:
    "The answer was cut off before it finished. Ask again to get the full answer.",
  chatGone: "This chat no longer exists — starting a new one.",
  tooManyFiles: "Up to 5 files per question — the extra files were not added.",
  unsupportedFile: (names: string) =>
    `${names} can't be read here. Attach a PDF, a PNG/JPEG screenshot or a CSV instead.`,
  filesTooLarge:
    "Those attachments are too large to send together (about 7 MB in total). Please attach fewer or smaller files.",
  timedOut:
    "That took too long to answer. The assistant may be busy — please try again.",
  rateLimited:
    "You're asking a lot right now — please wait a moment and try again.",
  emptyReason: "No records found for the requested scope.",
  liveData: "Live data",
  planning: "Planning your answer…",
  querying: "Checking live Mesha data…",
  synthesizing: "Composing the answer…",
  staleToolFailure:
    "That old answer came from a broken local data route. Ask again and I’ll use the live Mesha read API.",
} as const;

function progressStatusLabel(progress: {
  phase: string;
  label?: string;
}): string {
  // The coding-agent backend sends a specific step label; prefer it.
  if (progress.label && progress.label !== "Starting agent")
    return progress.label;
  switch (progress.phase) {
    case "planning":
      return CHROME.planning;
    case "querying":
      return CHROME.querying;
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
const PLUMBING_PREFIX =
  /^(cube|mesha mcp toolbox|mesha read-only sql fallback|mesha read model|toolbox|sql)\s*·?\s*/i;
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

function cleanAssistantText(text: string | undefined): string {
  const raw = (text ?? "").trim();
  if (!raw) return "";
  if (/cube:\s*could not be retrieved/i.test(raw))
    return CHROME.staleToolFailure;
  return raw
    .replace(/\bHere is what I found from the live read models:\s*/gi, "")
    .replace(/\bcube:\s*/gi, "")
    .trim();
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
    new Intl.DateTimeFormat("en-CA", {
      timeZone: ist,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(then) ===
    new Intl.DateTimeFormat("en-CA", {
      timeZone: ist,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date(now));
  if (sameDay) {
    const t = new Intl.DateTimeFormat("en-US", {
      timeZone: ist,
      hour: "numeric",
      minute: "2-digit",
      hour12: true,
    }).format(then);
    return `as of ${t}`;
  }
  return new Intl.DateTimeFormat("en-GB", {
    timeZone: ist,
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(then);
}

function newId(): string {
  return typeof crypto !== "undefined" && crypto.randomUUID
    ? crypto.randomUUID()
    : `id-${Date.now()}-${Math.random()}`;
}

// Browser speech-to-text (Chrome/Edge/Safari). Hidden when unsupported (most
// Android WebViews), so the mic never appears as a dead button.
type SpeechRecognitionLike = {
  lang: string;
  interimResults: boolean;
  continuous: boolean;
  onresult:
    | ((event: {
        results: ArrayLike<ArrayLike<{ transcript: string }>>;
      }) => void)
    | null;
  onend: (() => void) | null;
  onerror: (() => void) | null;
  start: () => void;
  stop: () => void;
};
function speechRecognitionCtor():
  (new () => SpeechRecognitionLike) | undefined {
  if (typeof window === "undefined") return undefined;
  const w = window as unknown as Record<string, unknown>;
  return (w.SpeechRecognition ?? w.webkitSpeechRecognition) as
    (new () => SpeechRecognitionLike) | undefined;
}

const MAX_FILES = 5;
// base64 grows ~4/3; the proxy/server cap is 10 MB of request, so ~7 MB of raw files.
const MAX_ATTACH_TOTAL_BYTES = 7 * 1024 * 1024;
// What the assistant can open: images, PDFs and plain text/CSV. Excel/Word/HEIC are refused
// up front with a plain message instead of failing mid-answer.
const READABLE_EXT = /\.(png|jpe?g|gif|webp|pdf|csv|tsv|txt|md|json)$/i;
function isReadableAttachment(file: File): boolean {
  const t = file.type.toLowerCase();
  if (/^image\/(png|jpe?g|gif|webp)$/.test(t) || t === "application/pdf")
    return true;
  if (t.startsWith("text/") || t === "application/json") return true;
  return !t && READABLE_EXT.test(file.name);
}

function fileToAttachment(
  file: File,
): Promise<{ name: string; type: string; data: string }> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const url = String(reader.result);
      resolve({
        name: file.name,
        type: file.type,
        data: url.slice(url.indexOf(",") + 1),
      });
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

// Smooth, ChatGPT-style reveal: tokens arrive in bursts; show them a few characters
// per animation frame. The step grows with the backlog so it never lags far behind.
function createTypewriter(render: (shown: string) => void) {
  let shown = "";
  let pending = "";
  let raf = 0;
  let waiters: (() => void)[] = [];
  let guard: ReturnType<typeof setTimeout> | undefined;
  const settle = () => {
    if (guard) clearTimeout(guard);
    guard = undefined;
    const w = waiters;
    waiters = [];
    w.forEach((fn) => fn());
  };
  const tick = () => {
    raf = 0;
    if (!pending) return settle();
    const step = Math.max(2, Math.ceil(pending.length / 18));
    shown += pending.slice(0, step);
    pending = pending.slice(step);
    render(shown);
    raf = requestAnimationFrame(tick);
  };
  const kick = () => {
    if (!raf) raf = requestAnimationFrame(tick);
  };
  const api = {
    push(text: string) {
      pending += text;
      kick();
    },
    reset() {
      if (raf) cancelAnimationFrame(raf);
      raf = 0;
      shown = "";
      pending = "";
      render("");
    },
    // cancel stops the reveal where it is (Stop keeps what the user saw) and
    // releases anyone awaiting drain().
    cancel() {
      if (raf) cancelAnimationFrame(raf);
      raf = 0;
      pending = "";
      settle();
    },
    // replace swaps the whole answer in one render (checker correction): no wipe-and-retype.
    replace(text: string) {
      if (raf) cancelAnimationFrame(raf);
      raf = 0;
      shown = text;
      pending = "";
      render(shown);
      settle();
    },
    flush() {
      if (raf) cancelAnimationFrame(raf);
      raf = 0;
      shown += pending;
      pending = "";
      render(shown);
      settle();
    },
    shown: () => shown.trim(),
    drain() {
      if (!pending && !raf) return Promise.resolve();
      // Background tabs don't run rAF: never wait more than 1.5s.
      return new Promise<void>((resolve) => {
        waiters.push(resolve);
        if (!guard) guard = setTimeout(() => api.flush(), 1500);
      });
    },
  };
  return api;
}

// Codex-style activity trail: live step list while the agent works, then a
// collapsed "Worked for 1m 12s · 6 steps" summary the user can expand.
function AgentSteps(props: {
  steps: string[];
  live: boolean;
  startedAt?: number;
  workedMs?: number;
}) {
  const { steps, live, startedAt, workedMs } = props;
  const [open, setOpen] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!live) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [live]);
  const ms = live ? now - (startedAt ?? now) : (workedMs ?? 0);
  const secs = Math.max(0, Math.round(ms / 1000));
  const took =
    secs >= 60 ? `${Math.floor(secs / 60)}m ${secs % 60}s` : `${secs}s`;
  const expanded = live || open;
  return (
    <div className={`mzai-steps${live ? " live" : ""}`}>
      <button
        type="button"
        className="mzai-steps-head"
        onClick={() => !live && setOpen((v) => !v)}
        aria-expanded={expanded}
      >
        {live
          ? `Working… ${took}`
          : `Worked for ${took} · ${steps.length} step${steps.length === 1 ? "" : "s"}`}
        {!live ? (
          <span className="mzai-steps-chev">{open ? "▾" : "›"}</span>
        ) : null}
      </button>
      {expanded ? (
        <ol aria-live={live ? "polite" : undefined}>
          {live && steps.length > 4 ? (
            <li className="more">+{steps.length - 4} earlier</li>
          ) : null}
          {(live ? steps.slice(-4) : steps).map((s, i, shownSteps) => {
            const now = live && i === shownSteps.length - 1;
            return (
              <li key={`${i}-${s}`} className={now ? "now" : "done"}>
                <span className="mzai-step-ic" aria-hidden>
                  {now ? null : <Check size={11} strokeWidth={3} />}
                </span>
                <span className="mzai-step-tx">{s}</span>
              </li>
            );
          })}
        </ol>
      ) : null}
    </div>
  );
}

const BUBBLE_POS_KEY = "mzai-bubble-pos";
const clamp = (v: number, lo: number, hi: number) =>
  Math.max(lo, Math.min(hi, v));
// Per-device convenience only; storage may be unavailable (private mode).
function readBubblePos(): { x: number; y: number } | null {
  try {
    const raw =
      typeof window !== "undefined"
        ? window.localStorage.getItem(BUBBLE_POS_KEY)
        : null;
    const p = raw ? (JSON.parse(raw) as { x?: unknown; y?: unknown }) : null;
    if (!p || typeof p.x !== "number" || typeof p.y !== "number") return null;
    // Keep it on screen if the viewport shrank since it was saved.
    return {
      x: clamp(p.x, 4, window.innerWidth - 60),
      y: clamp(p.y, 4, window.innerHeight - 60),
    };
  } catch {
    return null;
  }
}
function writeBubblePos(p: { x: number; y: number }) {
  try {
    window.localStorage.setItem(BUBBLE_POS_KEY, JSON.stringify(p));
  } catch {
    /* storage unavailable: position just isn't remembered */
  }
}

function asksAllParks(question: string): boolean {
  return /\b(all parks|across all parks|company(?:-wide)?|overall|whole company|tenant-wide)\b/i.test(
    question,
  );
}

function currentPageScope(
  question: string,
): { park_id?: string; shed_id?: string } | undefined {
  if (typeof window === "undefined") return undefined;
  if (asksAllParks(question)) return undefined;
  const params = new URLSearchParams(window.location.search);
  const park = params.get("park") ?? undefined;
  const shed = params.get("shed") ?? undefined;
  if (!park && !shed) return undefined;
  return { park_id: park, shed_id: shed };
}

export function CeoAiPanel({
  copy,
}: {
  copy: AssistantCopy;
}): ReactElement | null {
  const [allowed, setAllowed] = useState<boolean | null>(null);
  const [starters, setStarters] = useState<string[]>(copy.starters);
  const [open, setOpen] = useState(false);
  // Window state: "normal" floating panel, "max" fills the viewport, "min" docks
  // to a header-only bar. Conversation state survives every transition.
  const [view, setView] = useState<"normal" | "max" | "min">("normal");
  // Answers render through the lazily loaded markdown chunk; fetch it as soon as the
  // panel opens (restored-open state included) so the first answer is formatted.
  useEffect(() => {
    if (open) preloadCeoAiMarkdown();
  }, [open]);
  // Draggable launcher (chat-head style): free position, snaps to the nearest side,
  // remembered per device. A tap (< 6px of movement) still opens the chat.
  const [bubblePos, setBubblePos] = useState<{ x: number; y: number } | null>(
    readBubblePos,
  );
  const dragRef = useRef<{
    id: number;
    dx: number;
    dy: number;
    sx: number;
    sy: number;
    moved: boolean;
  } | null>(null);
  const suppressClickRef = useRef(false);
  // Thread list starts open on desktop, closed on phones (it overlays the chat there).
  // The panel renders only after the client-side capability probe, so reading the
  // viewport in the initializers is safe (no server render to mismatch).
  const isNarrow = () =>
    typeof window !== "undefined" &&
    window.matchMedia("(max-width:620px)").matches;
  const [showThreads, setShowThreads] = useState(() => !isNarrow());
  const [narrow, setNarrow] = useState(isNarrow);
  useEffect(() => {
    const mq = window.matchMedia("(max-width:620px)");
    const sync = () => setNarrow(mq.matches);
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);
  // The top-bar dock slot, looked up once on the client (SSR has no document).
  const [dockSlot, setDockSlot] = useState<HTMLElement | null>(null);
  useEffect(() => {
    const slot = document.getElementById("topbar-ai-slot");
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-time DOM lookup after mount
    if (slot) setDockSlot(slot);
  }, []);
  const [conversations, setConversations] = useState<ConversationSummary[]>([]);
  const [conversationId, setConversationId] = useState<string | undefined>(
    undefined,
  );
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const previews = useMemo(() => files.map(toPreview), [files]);
  // Composer-tray object URLs are per `files` snapshot: revoke the old set.
  useEffect(() => () => revokePreviews(previews), [previews]);
  const [lightbox, setLightbox] = useState<{
    files: PreviewFile[];
    start: number;
  } | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [listening, setListening] = useState(false);
  const recognitionRef = useRef<SpeechRecognitionLike | null>(null);
  const speechSupported = useMemo(
    () => typeof window !== "undefined" && Boolean(speechRecognitionCtor()),
    [],
  );

  // Esc restores a maximized/minimized panel to its normal size.
  useEffect(() => {
    if (!open || view === "normal") return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setView("normal");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, view]);

  const toggleVoice = useCallback(() => {
    if (listening) {
      recognitionRef.current?.stop();
      return;
    }
    const Ctor = speechRecognitionCtor();
    if (!Ctor) return;
    const rec = new Ctor();
    rec.lang = navigator.language || "en-IN";
    rec.interimResults = true;
    rec.continuous = true;
    const base = input ? input.replace(/\s*$/, " ") : "";
    rec.onresult = (event) => {
      let heard = "";
      for (let i = 0; i < event.results.length; i++)
        heard += event.results[i][0].transcript;
      setInput(base + heard);
    };
    rec.onend = () => {
      setListening(false);
      recognitionRef.current = null;
    };
    rec.onerror = () => setListening(false);
    recognitionRef.current = rec;
    setListening(true);
    rec.start();
  }, [input, listening]);

  const [banner, setBanner] = useState<{
    kind: "err" | "warn";
    text: string;
  } | null>(null);
  const addFiles = useCallback((list: FileList | null) => {
    if (!list) return;
    const all = Array.from(list);
    // Say why a file didn't make it instead of dropping it silently.
    const unsupported = all.filter((f) => !isReadableAttachment(f));
    const incoming = all.filter(isReadableAttachment).slice(0, MAX_FILES);
    const notes: string[] = [];
    if (unsupported.length)
      notes.push(
        CHROME.unsupportedFile(unsupported.map((f) => f.name).join(", ")),
      );
    // Bounded: incoming is sliced to MAX_FILES above, and shrinkImage is local (no network).
    const shrinking = incoming.map((f) => shrinkImage(f).catch(() => f));
    void Promise.all(shrinking).then((shrunk) =>
      setFiles((prev) => {
        const next = [...prev, ...shrunk];
        if (next.length > MAX_FILES) notes.push(CHROME.tooManyFiles);
        if (notes.length) setBanner({ kind: "warn", text: notes.join(" ") });
        return next.slice(0, MAX_FILES);
      }),
    );
  }, []);
  const [dragging, setDragging] = useState(false);
  const [pending, setPending] = useState(false);
  const [showStarters, setShowStarters] = useState(true);
  const [renaming, setRenaming] = useState<string | null>(null);
  const [renameText, setRenameText] = useState("");

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const resumeSeq = useRef(0);
  const renamingRef = useRef<string | null>(null);

  // Leadership capability probe (server-authoritative; replaces client regex).
  useEffect(() => {
    const controller = new AbortController();
    probeCapability(controller.signal).then((result) => {
      setAllowed(result.allowed);
      if (result.starters.length) setStarters(result.starters);
    });
    return () => controller.abort();
  }, []);

  // Follow the answer only while the reader is at the bottom; scrolling up to
  // re-read is not yanked back down by every streamed frame.
  const stickRef = useRef(true);
  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickRef.current) el.scrollTo({ top: el.scrollHeight });
  }, [messages, open, pending]);
  useEffect(() => {
    if (pending) stickRef.current = true;
  }, [pending]);

  // Release object URLs for sent-message thumbnails once those messages are
  // gone (new chat / resume / delete) and on unmount.
  const liveBlobUrls = useRef(new Set<string>());
  useEffect(() => {
    const now = new Set<string>();
    messages.forEach((m) =>
      m.files?.forEach((f) => {
        if (f.url.startsWith("blob:")) now.add(f.url);
      }),
    );
    liveBlobUrls.current.forEach((url) => {
      if (!now.has(url)) URL.revokeObjectURL(url);
    });
    liveBlobUrls.current = now;
  }, [messages]);
  useEffect(
    () => () => liveBlobUrls.current.forEach((url) => URL.revokeObjectURL(url)),
    [],
  );

  // Keep the dragged launcher on screen after resize / phone rotation.
  useEffect(() => {
    const onResize = () =>
      setBubblePos((p) =>
        p
          ? {
              x: clamp(p.x, 4, Math.max(4, window.innerWidth - 60)),
              y: clamp(p.y, 4, Math.max(4, window.innerHeight - 60)),
            }
          : p,
      );
    window.addEventListener("resize", onResize);
    window.addEventListener("orientationchange", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("orientationchange", onResize);
    };
  }, []);

  const refreshThreads = useCallback(() => {
    listConversations()
      .then((res) => setConversations(res.conversations))
      .catch(() => undefined);
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

  const shown = messages;

  const typerRef = useRef<ReturnType<typeof createTypewriter> | null>(null);
  // request_id of the running answer (first progress frame), for the Stop signal.
  const runRequestIdRef = useRef<string | undefined>(undefined);
  const stopGenerating = useCallback(() => {
    const running = abortRef.current;
    if (running && !running.signal.aborted)
      sendCeoAiStopSignal(runRequestIdRef.current);
    runRequestIdRef.current = undefined;
    typerRef.current?.cancel();
    typerRef.current = null;
    running?.abort();
    abortRef.current = null;
    setPending(false);
    if (running) trackCeoAiEvent(CeoAiEvents.StopGenerating);
  }, []);

  // Unmount: stop any in-flight answer and its reveal loop.
  useEffect(
    () => () => {
      typerRef.current?.cancel();
      abortRef.current?.abort();
    },
    [],
  );

  const ask = useCallback(
    async (raw: string, attached: File[] = []) => {
      // Returns true when the chat turned out to be gone (404), so the caller retries once
      // in a brand-new chat (fresh).
      const runOnce = async (fresh: boolean): Promise<boolean> => {
        const typed = raw.trim();
        if (!typed && !attached.length) return false;
        // Sending while an answer is running interrupts it, like ChatGPT/Claude.
        if (abortRef.current) stopGenerating();
        recognitionRef.current?.stop();
        const question = typed || "Please look at the attached file(s).";
        const boundedAttached = attached.slice(0, MAX_FILES);
        if (
          boundedAttached.reduce((n, f) => n + f.size, 0) >
          MAX_ATTACH_TOTAL_BYTES
        ) {
          setBanner({ kind: "warn", text: CHROME.filesTooLarge });
          return false;
        }
        const askConversationId = fresh ? undefined : conversationId;
        let attachments:
          Awaited<ReturnType<typeof fileToAttachment>>[] | undefined;
        try {
          attachments = boundedAttached.length
            ? await Promise.all(boundedAttached.map(fileToAttachment)) // request-plan:ignore owner=admin-web issue=CEO-AI-ATTACHMENT-CAP expires=2026-12-31 reason=boundedAttached is capped to the five visible attachment slots before fan-out
            : undefined;
        } catch {
          // Unreadable file (revoked/moved): keep the draft, tell the user.
          setBanner({
            kind: "err",
            text: "Couldn't read an attached file. Remove it and try again.",
          });
          return false;
        }
        setFiles([]);
        setInput("");
        if (!fresh) setBanner(null); // keep the "starting a new one" note on the automatic retry
        setPending(true);
        setShowStarters(false);
        trackCeoAiEvent(CeoAiEvents.Ask, { streaming: "true" });

        const assistantId = newId();
        setMessages((prev) => [
          ...prev,
          {
            id: newId(),
            role: "user",
            text: question,
            files: attached.length ? attached.map(toPreview) : undefined,
            state: "complete",
          },
          {
            id: assistantId,
            role: "assistant",
            text: "",
            state: "streaming",
            startedAt: Date.now(),
          },
        ]);

        const controller = new AbortController();
        abortRef.current = controller;
        runRequestIdRef.current = undefined;
        let errored = false;
        let gone = false; // 404: the chat was deleted/unknown; start a new one and ask again
        // Put the question (and files) back so a refused send isn't lost.
        const restoreDraft = () => {
          setInput((prev) => prev || raw);
          setFiles((prev) => (prev.length ? prev : attached));
        };

        const patch = (fields: Partial<ChatMessage>) =>
          setMessages((prev) =>
            prev.map((m) => (m.id === assistantId ? { ...m, ...fields } : m)),
          );

        try {
          const typer = createTypewriter((shown) =>
            setMessages((prev) =>
              prev.map((m) =>
                // First revealed text clears the coarse progress status.
                m.id === assistantId
                  ? {
                      ...m,
                      text: shown,
                      progress: shown ? undefined : m.progress,
                    }
                  : m,
              ),
            ),
          );
          typerRef.current = typer;
          const final = await readCeoAiStream(
            {
              question,
              attachments,
              conversationId: askConversationId,
              pageScope: currentPageScope(question),
              signal: controller.signal,
            },
            {
              onToken: (text) => typer.push(text),
              onReset: () => typer.reset(),
              onReplace: (text) => {
                typer.replace(text);
                patch({ checking: false });
              },
              // Live tag watch frames: keep updating even while the panel is minimized.
              onWatch: (frame) =>
                setMessages((prev) =>
                  prev.map((m) =>
                    m.id === assistantId
                      ? { ...m, watch: mergeWatch(m.watch, frame) }
                      : m,
                  ),
                ),
              onProgress: (progress) => {
                if (progress.requestId && abortRef.current === controller)
                  runRequestIdRef.current = progress.requestId;
                // Adopt the server's chat id right away: if the stream is cut short (server restart,
                // network), asking again continues this chat instead of starting another one.
                if (
                  progress.conversationId &&
                  abortRef.current === controller &&
                  progress.conversationId !== askConversationId
                ) {
                  setConversationId(progress.conversationId);
                  refreshThreads();
                }
                setMessages((prev) =>
                  prev.map((m) =>
                    m.id === assistantId
                      ? {
                          ...m,
                          progress: m.text
                            ? m.progress
                            : progressStatusLabel(progress),
                          // Answer shown, now being checked against the query results.
                          checking: progress.phase === "checking" ? true : m.checking,
                          steps:
                            progress.label &&
                            progress.label !== "Starting agent" &&
                            m.steps?.at(-1) !== progress.label
                              ? [...(m.steps ?? []), progress.label]
                              : m.steps,
                        }
                      : m,
                  ),
                );
              },
              onError: (message, status) => {
                errored = true;
                if (status === 404 && !fresh) {
                  gone = true;
                  return;
                }
                if (status === 409 || status === 413) {
                  // Busy (another tab is still answering in this chat) / too large: plain
                  // message, and the draft comes back so nothing typed is lost.
                  setBanner({ kind: "warn", text: message });
                  patch({ text: message, state: "error" });
                  restoreDraft();
                } else if (status === 410) {
                  // This chat was deleted (here or in another tab) while answering.
                  patch({ text: message, state: "error" });
                } else if (status === 429) {
                  setBanner({ kind: "warn", text: CHROME.rateLimited });
                  patch({ text: CHROME.rateLimited, state: "error" });
                } else if (status === 504) {
                  setBanner({ kind: "warn", text: CHROME.timedOut });
                  patch({
                    text: CHROME.timedOut,
                    state: "error",
                    mode: "degraded",
                  });
                } else if (status === 401) {
                  setBanner({ kind: "err", text: copy.unavailable });
                  patch({ text: copy.unavailable, state: "error" });
                } else {
                  setBanner({ kind: "err", text: CHROME.degraded });
                  patch({
                    text: message || CHROME.degraded,
                    state: "error",
                    mode: "degraded",
                  });
                }
                trackCeoAiError("ask", message, status);
              },
            },
          );

          // Let the typewriter finish revealing what already streamed before the
          // final metadata lands, so the answer never jumps.
          await typer.drain();
          // Stopped (or interrupted by a newer question) while draining: keep
          // what was shown; the catch-free path must not overwrite it.
          if (controller.signal.aborted) {
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantId
                  ? {
                      ...m,
                      text: m.text || CHROME.stoppedEmpty,
                      state: "complete",
                      workedMs: m.startedAt
                        ? Date.now() - m.startedAt
                        : m.workedMs,
                    }
                  : m,
              ),
            );
          } else if (!final && !errored) {
            // Stream closed cleanly without a final event: never leave a
            // perpetual caret/"Working…" behind.
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantId
                  ? m.text
                    ? {
                        ...m,
                        // Say it was cut off rather than passing a partial answer off as whole.
                        text: `${m.text}\n\n_${CHROME.cutOff}_`,
                        state: "complete",
                        workedMs: m.startedAt
                          ? Date.now() - m.startedAt
                          : m.workedMs,
                      }
                    : {
                        ...m,
                        text: CHROME.degraded,
                        state: "error",
                        mode: "degraded",
                      }
                  : m,
              ),
            );
          } else if (final) {
            if (
              final.conversation_id &&
              final.conversation_id !== askConversationId
            ) {
              setConversationId(final.conversation_id);
              refreshThreads();
            }
            const answer = final.answer || copy.noAnswer;
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantId && m.startedAt
                  ? { ...m, workedMs: Date.now() - m.startedAt }
                  : m,
              ),
            );
            patch({
              text: typer.shown() === answer.trim() ? typer.shown() : answer,
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
                  ? {
                      ...m,
                      text: m.text || CHROME.stoppedEmpty,
                      state: "complete",
                      workedMs: m.startedAt
                        ? Date.now() - m.startedAt
                        : m.workedMs,
                    }
                  : m,
              ),
            );
          } else {
            const message =
              error instanceof Error ? error.message : "assistant_error";
            setBanner({ kind: "err", text: CHROME.degraded });
            // Connection dropped mid-answer (server restart, network): keep what was shown
            // and say it was cut off; the chat id was adopted, so asking again continues it.
            const shown = typerRef.current?.shown() ?? "";
            typerRef.current?.cancel();
            patch(
              shown
                ? { text: `${shown}\n\n_${CHROME.cutOff}_`, state: "complete" }
                : { text: CHROME.degraded, state: "error", mode: "degraded" },
            );
            trackCeoAiError("ask_throw", message);
          }
        } finally {
          // A watch cut off with the stream (Stop, network) must not keep counting down.
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId && m.watch && !m.watch.ended
                ? {
                    ...m,
                    watch: {
                      ...m.watch,
                      ended: true,
                      reason: m.watch.reason ?? "stopped",
                    },
                  }
                : m,
            ),
          );
          // An interrupted run must not clear the NEWER run's controller/pending.
          if (abortRef.current === controller) {
            abortRef.current = null;
            typerRef.current = null;
            setPending(false);
          }
        }
        // The chat was deleted (another tab) or no longer exists: never leave the CEO stuck on
        // it. Say so, start a new chat and ask the same question there, once.
        if (gone && !controller.signal.aborted) {
          trackCeoAiError("ask", "conversation_not_found", 404);
          setMessages([]);
          setConversationId(undefined);
          setBanner({ kind: "warn", text: CHROME.chatGone });
          refreshThreads();
          return true;
        }
        return false;
      };
      if (await runOnce(false)) await runOnce(true);
    },
    [conversationId, copy, refreshThreads, stopGenerating],
  );

  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void ask(input, files);
  };

  const startNewChat = useCallback(async () => {
    resumeSeq.current += 1;
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
      if (isNarrow()) setShowThreads(false);
      if (id === conversationId) return;
      const seq = ++resumeSeq.current;
      stopGenerating();
      setConversationId(id);
      setBanner(null);
      setShowStarters(false);
      trackCeoAiEvent(CeoAiEvents.ResumeChat);
      const stored = await loadConversationMessages(id).catch(() => []);
      // A later click won: drop this stale load.
      if (seq !== resumeSeq.current) return;
      const restoredMessages: ChatMessage[] = stored.map((m) => {
        const role: ChatMessage["role"] =
          m.role === "user" ? "user" : "assistant";
        const message: ChatMessage = {
          id: m.id ?? m.message_id ?? newId(),
          role,
          text:
            role === "user" ? (m.content ?? "") : cleanAssistantText(m.content),
          state: "complete" as const,
          source: m.source,
          mode: m.mode,
          requestId: m.request_id,
          messageId: m.message_id ?? m.id,
          citations: m.citations,
          chart: m.chart,
          files: m.files?.map((f) => ({
            name: f.name,
            type: f.type ?? "",
            url: `/api/ceo-ai/conversations/${encodeURIComponent(id)}/files/${encodeURIComponent(f.id)}`,
          })),
        };
        return message;
      });
      setMessages(restoredMessages);
    },
    [conversationId, stopGenerating],
  );

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
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
      // Enter/Escape unmount the input and fire blur: commit only once, and
      // never after Escape cancelled.
      if (renamingRef.current !== id) return;
      renamingRef.current = null;
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
    ? view === "max"
      ? {
          right: PANEL_MARGIN,
          bottom: PANEL_MARGIN,
          top: PANEL_MARGIN,
          left: PANEL_MARGIN,
        }
      : view === "min"
        ? {
            right: PANEL_MARGIN,
            bottom: PANEL_MARGIN,
            width: `min(360px, calc(100vw - ${PANEL_MARGIN * 2}px))`,
          }
        : {
            right: PANEL_MARGIN,
            bottom: PANEL_MARGIN,
            width: `min(${PANEL_WIDTH}px, calc(100vw - ${PANEL_MARGIN * 2}px))`,
            // Leave room for the app's top bar so the panel's own close/min/max buttons are never covered.
            height: `min(640px, calc(100dvh - ${PANEL_MARGIN * 2}px - 72px))`,
          }
    : bubblePos
      ? { left: bubblePos.x, top: bubblePos.y, right: "auto", bottom: "auto" }
      : { right: 24, bottom: 24 };

  return (
    <div
      className={`mzai-root ${open ? "mzai-open" : "mzai-closed"} mzai-view-${view}${!open && bubblePos ? " mzai-free" : ""}`}
      style={rootStyle}
    >
      <CeoAiStyles />
      {open ? (
        <section
          className={`mzai-panel${dragging ? " mzai-dragging" : ""}`}
          aria-label={copy.title}
          onDragEnter={(e) => {
            if (e.dataTransfer.types.includes("Files")) {
              e.preventDefault();
              setDragging(true);
            }
          }}
          onDragOver={(e) => {
            if (e.dataTransfer.types.includes("Files")) e.preventDefault();
          }}
          onDragLeave={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null))
              setDragging(false);
          }}
          onDrop={(e) => {
            e.preventDefault();
            setDragging(false);
            addFiles(e.dataTransfer.files);
            if (view === "min") setView("normal");
          }}
        >
          {dragging ? (
            <div className="mzai-drop">Drop files to attach</div>
          ) : null}
          <div
            className="mzai-head"
            onClick={view === "min" ? () => setView("normal") : undefined}
            onDoubleClick={
              view === "min"
                ? undefined
                : () => setView(view === "max" ? "normal" : "max")
            }
            role={view === "min" ? "button" : undefined}
          >
            {/* The chats list can't show in the minimized bar; don't offer its toggle. */}
            {view === "min" ? null : (
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
            )}
            <span className="mzai-mark">
              <MeshaLogo width={32} height={32} />
            </span>
            <span className="mzai-htext">
              <b>{copy.title}</b>
              <small>{copy.subtitle}</small>
            </span>
            <div className="mzai-hbtns" onClick={(e) => e.stopPropagation()}>
              <button
                type="button"
                className="mzai-icon"
                onClick={() => setView(view === "min" ? "normal" : "min")}
                aria-label={view === "min" ? "Restore panel" : "Minimize"}
                title={view === "min" ? "Restore panel" : "Minimize"}
                aria-expanded={view !== "min"}
              >
                {view === "min" ? (
                  <ChevronUp className="ic" />
                ) : (
                  <Minus className="ic" />
                )}
              </button>
              <button
                type="button"
                className="mzai-icon mzai-hide-mobile"
                onClick={() => setView(view === "max" ? "normal" : "max")}
                aria-label={view === "max" ? "Restore size" : "Maximize"}
                title={view === "max" ? "Restore size" : "Maximize"}
              >
                {view === "max" ? (
                  <Minimize2 className="ic" />
                ) : (
                  <Maximize2 className="ic" />
                )}
              </button>
              <button
                type="button"
                className="mzai-icon"
                onClick={() => {
                  setOpen(false);
                  setView("normal");
                }}
                aria-label={copy.close}
              >
                <X className="ic" />
              </button>
            </div>
          </div>

          <div className="mzai-body">
            {showThreads ? (
              <button
                type="button"
                className="mzai-scrim"
                aria-label="Close chats"
                onClick={() => setShowThreads(false)}
              />
            ) : null}
            <aside className={`mzai-side${showThreads ? "" : " mzai-hide"}`}>
              <div className="mzai-side-head">
                <span>{CHROME.threads}</span>
                <button
                  type="button"
                  className="mzai-newbtn"
                  onClick={() => void startNewChat()}
                >
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
                      className={`mzai-thread${thread.id === conversationId ? " mzai-on" : ""}${confirmDelete === thread.id ? " mzai-confirming" : ""}`}
                      role="button"
                      tabIndex={0}
                      aria-current={
                        thread.id === conversationId ? "true" : undefined
                      }
                      onClick={() =>
                        confirmDelete !== thread.id &&
                        renaming !== thread.id &&
                        void resumeThread(thread.id)
                      }
                      onKeyDown={(e) => {
                        if (e.target !== e.currentTarget) return;
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault();
                          void resumeThread(thread.id);
                        }
                      }}
                    >
                      {confirmDelete === thread.id ? (
                        <span
                          className="mzai-confirm"
                          onClick={(e) => e.stopPropagation()}
                        >
                          <span>Delete this chat?</span>
                          <button
                            type="button"
                            className="mzai-confirm-yes"
                            autoFocus
                            onClick={() => {
                              setConfirmDelete(null);
                              void removeThread(thread.id);
                            }}
                          >
                            Delete
                          </button>
                          <button
                            type="button"
                            className="mzai-confirm-no"
                            onClick={() => setConfirmDelete(null)}
                          >
                            Cancel
                          </button>
                        </span>
                      ) : renaming === thread.id ? (
                        <input
                          autoFocus
                          value={renameText}
                          onClick={(e) => e.stopPropagation()}
                          onChange={(e) => setRenameText(e.target.value)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") void commitRename(thread.id);
                            if (e.key === "Escape") {
                              e.stopPropagation();
                              renamingRef.current = null;
                              setRenaming(null);
                            }
                          }}
                          onBlur={() => void commitRename(thread.id)}
                        />
                      ) : (
                        <span className="mzai-tt" title={thread.title}>
                          {thread.title || CHROME.newChat}
                        </span>
                      )}
                      {confirmDelete === thread.id ? null : (
                        <>
                          <button
                            type="button"
                            className="mzai-thread-act"
                            aria-label={CHROME.rename}
                            title={CHROME.rename}
                            onClick={(e) => {
                              e.stopPropagation();
                              renamingRef.current = thread.id;
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
                              setConfirmDelete(thread.id);
                            }}
                          >
                            <Trash2 className="ic" />
                          </button>
                        </>
                      )}
                    </div>
                  ))
                )}
              </div>
            </aside>

            <div className="mzai-main">
              <div
                ref={scrollRef}
                className="mzai-log"
                onScroll={(e) => {
                  const el = e.currentTarget;
                  stickRef.current =
                    el.scrollHeight - el.scrollTop - el.clientHeight < 48;
                }}
              >
                {shown.map((message) => (
                  <div
                    key={message.id}
                    className={`mzai-msg-wrap ${message.role}`}
                  >
                    {message.role === "assistant" && (
                      <div className="mzai-avatar">
                        <MeshaLogo width={32} height={32} />
                      </div>
                    )}
                    <div
                      className={`mzai-msg ${message.role} ${message.state}`}
                    >
                      {message.role === "assistant" && message.steps?.length ? (
                        <AgentSteps
                          steps={message.steps}
                          live={message.state === "streaming"}
                          startedAt={message.startedAt}
                          workedMs={message.workedMs}
                        />
                      ) : null}
                      {message.role === "assistant" && message.watch ? (
                        <CeoAiWatchCard
                          watch={message.watch}
                          onStop={
                            message.state === "streaming"
                              ? () =>
                                  sendCeoAiWatchStop(runRequestIdRef.current)
                              : undefined
                          }
                        />
                      ) : null}
                      {/* No empty assistant bubble while the agent works; the progress line shows instead. */}
                      {message.role === "user" || message.text ? (
                        <div className="mzai-bub">
                          {message.role === "assistant" ? (
                            <CeoAiMarkdown text={message.text} />
                          ) : (
                            message.text
                          )}
                          {message.files?.length ? (
                            <div className="mzai-msg-files">
                              {message.files.map((f, i) => (
                                <Thumb
                                  key={f.url}
                                  file={f}
                                  onOpen={() =>
                                    setLightbox({
                                      files: message.files ?? [],
                                      start: i,
                                    })
                                  }
                                />
                              ))}
                            </div>
                          ) : null}
                          {message.state === "streaming" && message.text ? (
                            <span className="mzai-caret" />
                          ) : null}
                        </div>
                      ) : null}
                      {/* Chart belongs to the answer: above its Copy action, not after it. */}
                      {message.role === "assistant" &&
                      message.state === "complete" &&
                      message.chart ? (
                        <CeoAiChart chart={message.chart} />
                      ) : null}
                      {message.role === "assistant" &&
                      message.state === "complete" &&
                      message.id !== "hello" &&
                      message.text &&
                      message.text !== CHROME.stoppedEmpty ? (
                        <div className="mzai-actions">
                          <CopyButton text={message.text} />
                        </div>
                      ) : null}
                      {message.state === "streaming" &&
                      message.text &&
                      message.checking ? (
                        <div className="mzai-progress">
                          <span
                            className="mzai-progress-label"
                            role="status"
                            aria-live="polite"
                          >
                            Checking the answer against the data…
                          </span>
                        </div>
                      ) : null}
                      {message.state === "streaming" &&
                      !message.text &&
                      !message.steps?.length ? (
                        <div className="mzai-progress">
                          <div
                            className="mzai-skel"
                            role="status"
                            aria-label={copy.checking}
                          >
                            <span />
                            <span />
                            <span />
                          </div>
                          {message.progress ? (
                            <span
                              className="mzai-progress-label"
                              aria-live="polite"
                            >
                              {message.progress}
                            </span>
                          ) : null}
                        </div>
                      ) : null}
                      {message.role === "assistant" &&
                      message.state === "complete" &&
                      message.citations?.length ? (
                        <div className="mzai-cites">
                          {message.citations.map((cite, i) => {
                            const freshness = formatFreshness(cite.as_of);
                            return (
                              <span
                                key={`${message.id}-c${i}`}
                                className={`mzai-cite tier-${cite.tier ?? "api"}`}
                              >
                                <b>{formatCitationSurface(cite.surface)}</b>
                                {freshness ? ` · ${freshness}` : ""}
                              </span>
                            );
                          })}
                        </div>
                      ) : null}
                      {message.role === "assistant" &&
                      message.state === "complete" &&
                      message.id !== "hello" &&
                      message.mode !== "agent" ? (
                        <div className="mzai-foot">
                          <span
                            className={`mzai-mode${message.mode === "degraded" ? " degraded" : ""}`}
                          >
                            {formatSource(message.source)
                              ? `${formatSource(message.source)} · `
                              : ""}
                            {modeLabel(message.mode, copy)}
                          </span>
                        </div>
                      ) : null}
                    </div>
                  </div>
                ))}
              </div>

              {banner ? (
                <div className={`mzai-banner ${banner.kind}`}>
                  {banner.text}
                </div>
              ) : null}

              {messages.length > 0 ? (
                <div className="mzai-suggestbar">
                  <button
                    type="button"
                    onClick={() => setShowStarters((v) => !v)}
                    aria-expanded={startersVisible}
                  >
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

              {previews.length ? (
                <div className="mzai-files">
                  {previews.map((f, i) => (
                    <Thumb
                      key={f.url}
                      file={f}
                      onOpen={() => setLightbox({ files: previews, start: i })}
                      onRemove={() =>
                        setFiles((prev) => prev.filter((_, j) => j !== i))
                      }
                    />
                  ))}
                </div>
              ) : null}
              <form className="mzai-form" onSubmit={onSubmit}>
                <input
                  ref={fileInputRef}
                  type="file"
                  multiple
                  hidden
                  accept="image/png,image/jpeg,image/gif,image/webp,application/pdf,.csv,.tsv,.txt,.md,.json"
                  onChange={(e) => {
                    addFiles(e.target.files);
                    e.target.value = "";
                  }}
                />
                <button
                  type="button"
                  className="mzai-tool"
                  onClick={() => fileInputRef.current?.click()}
                  aria-label="Attach files"
                  title="Attach files"
                >
                  <Paperclip size={18} />
                </button>
                <textarea
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Escape" && pending) {
                      e.preventDefault();
                      e.stopPropagation();
                      stopGenerating();
                      return;
                    }
                    if (e.key === "Enter" && !e.shiftKey) {
                      e.preventDefault();
                      e.currentTarget.form?.requestSubmit();
                    }
                  }}
                  onPaste={(e) => {
                    if (e.clipboardData.files.length) {
                      e.preventDefault();
                      addFiles(e.clipboardData.files);
                    }
                  }}
                  rows={1}
                  placeholder={
                    listening
                      ? "Listening…"
                      : narrow
                        ? "Ask Mesha…"
                        : copy.placeholder
                  }
                />
                {speechSupported ? (
                  <button
                    type="button"
                    className={`mzai-tool${listening ? " on" : ""}`}
                    onClick={toggleVoice}
                    aria-pressed={listening}
                    aria-label={listening ? "Stop voice input" : "Voice input"}
                    title={listening ? "Stop voice input" : "Voice input"}
                  >
                    {listening ? <MicOff size={18} /> : <Mic size={18} />}
                  </button>
                ) : null}
                {pending && !input.trim() && !files.length ? (
                  <button
                    type="button"
                    className="mzai-send stop"
                    onClick={stopGenerating}
                    aria-label={CHROME.stop}
                    title="Stop (Esc)"
                  >
                    <Square size={14} fill="currentColor" strokeWidth={0} />
                  </button>
                ) : (
                  <button
                    type="submit"
                    className="mzai-send"
                    aria-label={copy.send}
                    disabled={!input.trim() && !files.length}
                  >
                    <Send className="ic" />
                  </button>
                )}
              </form>
            </div>
          </div>
          {lightbox ? (
            <Lightbox
              files={lightbox.files}
              start={lightbox.start}
              onClose={() => setLightbox(null)}
            />
          ) : null}
        </section>
      ) : (
        <>
          <CeoAiStyles />
          {/* Closed: the launcher DOCKS into the top bar's slot when the shell offers one, so it
              never sits over a table's last column or a footer's pager at the bottom-right of the
              viewport. Without a slot (no shell) it stays the floating bubble. */}
          {/* At phone width the top bar hides its slot (frame.css, max-width:620px), so a docked
              launcher would be 0x0 and unreachable; the phone keeps the draggable bubble. */}
          {dockSlot && !narrow
            ? createPortal(
                <button
                  type="button"
                  className="mzai-bubble mzai-bubble-dock"
                  onClick={() => {
                    setOpen(true);
                    trackCeoAiEvent(CeoAiEvents.Open);
                  }}
                  aria-label={copy.open}
                  title={copy.title}
                >
                  <GoatAvatar />
                </button>,
                dockSlot,
              )
            : (
            <button
              type="button"
              className="mzai-bubble"
              onPointerDown={(e) => {
                const r = e.currentTarget.getBoundingClientRect();
                dragRef.current = {
                  id: e.pointerId,
                  dx: e.clientX - r.left,
                  dy: e.clientY - r.top,
                  sx: e.clientX,
                  sy: e.clientY,
                  moved: false,
                };
                e.currentTarget.setPointerCapture(e.pointerId);
              }}
              onPointerMove={(e) => {
                const d = dragRef.current;
                if (!d || d.id !== e.pointerId) return;
                if (
                  !d.moved &&
                  Math.hypot(e.clientX - d.sx, e.clientY - d.sy) < 6
                )
                  return;
                d.moved = true;
                const size = e.currentTarget.offsetWidth;
                setBubblePos({
                  x: clamp(e.clientX - d.dx, 4, window.innerWidth - size - 4),
                  y: clamp(e.clientY - d.dy, 4, window.innerHeight - size - 4),
                });
              }}
              onPointerUp={(e) => {
                const d = dragRef.current;
                dragRef.current = null;
                if (!d?.moved) return;
                suppressClickRef.current = true;
                const size = e.currentTarget.offsetWidth;
                const r = e.currentTarget.getBoundingClientRect();
                const snapped = {
                  x:
                    r.left + size / 2 < window.innerWidth / 2
                      ? 12
                      : window.innerWidth - size - 12,
                  y: clamp(r.top, 12, window.innerHeight - size - 12),
                };
                setBubblePos(snapped);
                writeBubblePos(snapped);
              }}
              onPointerCancel={() => {
                dragRef.current = null;
              }}
              onClick={() => {
                if (suppressClickRef.current) {
                  suppressClickRef.current = false;
                  return;
                }
                setOpen(true);
                trackCeoAiEvent(CeoAiEvents.Open);
              }}
              aria-label={copy.open}
              title={copy.title}
            >
              <GoatAvatar />
            </button>
              )}
        </>
      )}
    </div>
  );
}
