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
import Box from "@mui/material/Box";
import Fab from "@mui/material/Fab";
import Chip from "@mui/material/Chip";
import Alert from "@mui/material/Alert";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import Collapse from "@mui/material/Collapse";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import IconButton from "@mui/material/IconButton";
import DialogTitle from "@mui/material/DialogTitle";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import CircularProgress from "@mui/material/CircularProgress";
import type { SxProps, Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { EmptyContent } from "@/components/minimal/empty-content";
import { ConfirmDialog } from "@/components/minimal/custom-dialog";
import { ChatLayout } from "@/components/minimal/sections/chat/layout";
import { useCollapseNav } from "@/components/minimal/sections/chat/hooks/use-collapse-nav";
import { ChatNav } from "@/components/app/sections/chat/chat-nav";
import { ChatNavItem } from "@/components/app/sections/chat/chat-nav-item";
import { ChatHeaderDetails } from "@/components/app/sections/chat/chat-header-details";
import { ChatMessageList } from "@/components/app/sections/chat/chat-message-list";
import { ChatMessageItem } from "@/components/app/sections/chat/chat-message-item";
import { ChatMessageInput } from "@/components/app/sections/chat/chat-message-input";
import { fmtDate } from "@/lib/format";
import {
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
import { GoatAvatar, MeshaLogo } from "./ceo-ai-marks";
import { CeoAiEvents, trackCeoAiError, trackCeoAiEvent } from "./telemetry";
import type { AssistantCopy, ChatMessage, ConversationSummary } from "./types";

// Ask Mesha is the template chat app (Minimal v7.7.0 sections/chat) in a floating window:
// ChatLayout (verbatim) with the template-derived ChatNav + ChatNavItem (chat history),
// ChatHeaderDetails (assistant identity + window controls + Rename / Delete menu),
// ChatMessageList + ChatMessageItem (turns; an answer carries its steps, live watch card, markdown,
// chart, sources and Copy) and ChatMessageInput (attach, voice, send / stop). Behaviour (streaming,
// stop, history, attachments, charts, errors, phone sheet, header launcher) is unchanged.
// guard: ask-mesha-template-chat (features/ceo-ai/ceo-ai-template-chat.test.mjs)

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
  sender: "Mesha",
  moreActions: "Chat actions",
  renameTitle: "Rename chat",
  deleteTitle: "Delete this chat?",
  cancel: "Cancel",
  suggestions: "Suggestions",
  drop: "Drop files to attach",
  attach: "Attach files",
  inputLabel: "Message Ask Mesha",
  checkingAnswer: "Checking the answer against the data…",
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

type View = "normal" | "min" | "max";
// The template chat header is 72px tall (ChatLayout LayoutHeader); the minimized window is that bar.
const HEADER_HEIGHT = 72;
const PANEL_MARGIN = 14;
// Window sizes: the normal window holds the template chat nav (320) beside a 640 thread; the app's
// top bar stays visible above it on laptops. The phone sheet fills the screen, so it sits above the
// shell header (appBar 1100/1101) and below MUI drawers (1200: the phone chats drawer) and modals /
// popovers (1300).
const PANEL_Z = 1150;

/** The open window's fixed frame (placement per view; the phone sheet fills the screen). */
function frameSx(view: View): SxProps<Theme> {
  return (theme: Theme) => ({
    position: "fixed",
    zIndex: PANEL_Z,
    display: "flex",
    alignItems: "flex-end",
    justifyContent: "flex-end",
    pointerEvents: "none",
    overscrollBehavior: "contain",
    right: PANEL_MARGIN,
    bottom: PANEL_MARGIN,
    left: PANEL_MARGIN,
    // Every size stays below the app top bar (docs/design/redesign-regression-guard.md).
    top: PANEL_MARGIN + HEADER_HEIGHT,
    [theme.breakpoints.down("sm")]: { inset: 0 },
  });
}

/** ChatLayout size per view (template root card: paper, radius, shadow). */
function windowSx(view: View): SxProps<Theme> {
  return (theme: Theme) => ({
    pointerEvents: "auto",
    flex: "none",
    overflow: "hidden",
    boxShadow: theme.vars.customShadows.dialog,
    width: view === "max" ? 1 : view === "min" ? 420 : 960,
    height: view === "max" ? 1 : view === "min" ? HEADER_HEIGHT : 720,
    maxWidth: 1,
    maxHeight: 1,
    [theme.breakpoints.down("sm")]: { width: 1, height: view === "min" ? HEADER_HEIGHT : 1, borderRadius: 0 },
  });
}

// An answer fills the thread; the reader's own turn keeps the template 320 bubble.
const ANSWER_SLOTS = {
  column: { flex: "1 1 auto", minWidth: 0, alignItems: "stretch" },
  body: { maxWidth: 1, flex: "1 1 auto", minWidth: 0 },
  actions: { "@media (hover: none)": { opacity: 1 } },
};
const ERROR_BODY = { maxWidth: 1, flex: "1 1 auto", minWidth: 0, color: "error.darker", bgcolor: "error.lighter" };
// A suggested prompt is a whole question: the Chip label wraps; 44px tap floor on phones.
const STARTER_SX = { height: "auto", minHeight: { xs: 44, sm: 32 }, maxWidth: 1, "& .MuiChip-label": { whiteSpace: "normal", py: 0.75 } };
// File drag over the window: the template upload drop-zone look (dashed primary over a soft tint).
const DROP_SX: SxProps<Theme> = (theme: Theme) => ({
  position: "absolute",
  inset: 0,
  zIndex: 10,
  display: "grid",
  placeItems: "center",
  borderRadius: "inherit",
  color: "primary.main",
  pointerEvents: "none",
  border: `dashed 2px ${theme.vars.palette.primary.main}`,
  bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.08),
});

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
  const shownSteps = live ? steps.slice(-4) : steps;
  return (
    <Box sx={{ mb: 1 }}>
      <Button
        size="small"
        color="inherit"
        onClick={() => !live && setOpen((v) => !v)}
        aria-expanded={expanded}
        endIcon={live ? <CircularProgress size={12} color="inherit" /> : <Iconify icon={open ? "eva:arrow-ios-downward-fill" : "eva:arrow-ios-forward-fill"} width={16} />}
        sx={{ color: "text.secondary", px: 0.5 }}
      >
        {live
          ? `Working… ${took}`
          : `Worked for ${took} · ${steps.length} step${steps.length === 1 ? "" : "s"}`}
      </Button>
      <Collapse in={expanded}>
        <Stack component="ol" spacing={0.5} aria-live={live ? "polite" : undefined} sx={{ m: 0, mt: 0.5, p: 0, listStyle: "none" }}>
          {live && steps.length > 4 ? (
            <Typography component="li" variant="caption" sx={{ color: "text.disabled" }}>
              +{steps.length - 4} earlier
            </Typography>
          ) : null}
          {shownSteps.map((s, i) => {
            const current = live && i === shownSteps.length - 1;
            return (
              <Stack component="li" key={`${i}-${s}`} direction="row" spacing={1} sx={{ alignItems: "flex-start", typography: "caption", color: current ? "text.primary" : "text.secondary" }}>
                {current ? <CircularProgress size={12} color="inherit" sx={{ mt: 0.25, flex: "none" }} /> : <Iconify icon="eva:checkmark-fill" width={14} sx={{ mt: 0.25, flex: "none", color: "success.main" }} />}
                <Box component="span" sx={{ minWidth: 0, overflowWrap: "anywhere" }}>
                  {s}
                </Box>
              </Stack>
            );
          })}
        </Stack>
      </Collapse>
    </Box>
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
  // Chat history: the template chat nav (open beside the thread from md, a drawer below md).
  // The panel renders only after the client-side capability probe, so reading the
  // viewport in the initializer is safe (no server render to mismatch).
  const conversationsNav = useCollapseNav();
  const isNarrow = () =>
    typeof window !== "undefined" &&
    window.matchMedia("(max-width:620px)").matches;
  const [narrow, setNarrow] = useState(isNarrow);
  useEffect(() => {
    const mq = window.matchMedia("(max-width:620px)");
    const sync = () => setNarrow(mq.matches);
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  }, []);
  // The top-bar dock slot (SSR has no document). TR1-#12: a one-time lookup missed the slot on routes
  // whose header committed after this panel (and kept a detached node after a header remount), so the
  // launcher vanished from the header on /verify and /protocol-adherence. Track the live slot instead:
  // re-resolve whenever the current one is missing or detached. guard: ask-mesha-docked
  const [dockSlot, setDockSlot] = useState<HTMLElement | null>(null);
  useEffect(() => {
    let current: HTMLElement | null = null;
    const resolve = () => {
      if (current?.isConnected) return;
      current = document.getElementById("topbar-ai-slot");
      setDockSlot(current);
    };
    resolve();
    const observer = new MutationObserver(resolve);
    observer.observe(document.body, { childList: true, subtree: true });
    return () => observer.disconnect();
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
  const [threadLoading, setThreadLoading] = useState(false);

  const scrollRef = useRef<HTMLDivElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const resumeSeq = useRef(0);

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
    // An empty chat shows the greeting and suggestions from the top (nothing to follow yet).
    if (el && stickRef.current && messages.length) el.scrollTo({ top: el.scrollHeight });
  }, [messages, open, pending]);
  useEffect(() => {
    if (pending) stickRef.current = true;
  }, [pending]);
  // The template Scrollbar scrolls its inner SimpleBar node (the ref): listen there.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return undefined;
    const onScroll = () => {
      stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48;
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => el.removeEventListener("scroll", onScroll);
  }, [open, view, threadLoading]);

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

  const startNewChat = useCallback(async () => {
    resumeSeq.current += 1;
    setThreadLoading(false);
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
      const seq = ++resumeSeq.current;
      stopGenerating();
      setConversationId(id);
      setBanner(null);
      setShowStarters(false);
      trackCeoAiEvent(CeoAiEvents.ResumeChat);
      setThreadLoading(true);
      const stored = await loadConversationMessages(id).catch(() => []);
      // A later click won: drop this stale load.
      if (seq !== resumeSeq.current) return;
      setThreadLoading(false);
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
  const current = conversations.find((c) => c.id === conversationId);
  const openPanel = () => {
    setOpen(true);
    trackCeoAiEvent(CeoAiEvents.Open);
  };

  if (!open) {
    return (
      <>
        {/* Closed: the launcher DOCKS into the top bar's slot when the shell offers one, at EVERY
            width (TR1-#13: the phone's floating bubble covered page content and sat on top of the
            open phone menu; the template header has no FAB). It is a template header IconButton
            (transparent, 40px, 44px tap on phones) carrying the goat mark, like the template's
            language flag. Without a slot (no shell) it stays a draggable floating Fab.
            guard: ask-mesha-docked (features/ceo-ai/ceo-ai-dock.test.mjs) */}
        {dockSlot
          ? createPortal(
              <IconButton onClick={openPanel} aria-label={copy.open} title={copy.title}>
                <GoatAvatar size="calc(3 * var(--spacing))" />
              </IconButton>,
              dockSlot,
            )
          : (
          <Fab
            color="default"
            sx={{
              position: "fixed",
              zIndex: 80,
              touchAction: "none",
              ...(bubblePos ? { left: bubblePos.x, top: bubblePos.y } : { right: 24, bottom: 24 }),
            }}
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
              if (!d.moved && Math.hypot(e.clientX - d.sx, e.clientY - d.sy) < 6) return;
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
                x: r.left + size / 2 < window.innerWidth / 2 ? 12 : window.innerWidth - size - 12,
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
              openPanel();
            }}
            aria-label={copy.open}
            title={copy.title}
          >
            <GoatAvatar />
          </Fab>
            )}
      </>
    );
  }

  const starterChips = (
    <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1, justifyContent: messages.length ? "flex-start" : "center" }}>
      {starters.map((question) => (
        <Chip
          key={question}
          label={question}
          variant="outlined"
          clickable
          disabled={pending}
          onClick={() => {
            trackCeoAiEvent(CeoAiEvents.StarterClick);
            void ask(question);
          }}
          sx={STARTER_SX}
        />
      ))}
    </Stack>
  );

  const fileChips = (files: PreviewFile[] | undefined) =>
    files?.length ? (
      <Stack direction="row" sx={{ flexWrap: "wrap", gap: 0.75, mt: 1 }}>
        {files.map((f, i) => (
          <Thumb key={f.url} file={f} onOpen={() => setLightbox({ files, start: i })} />
        ))}
      </Stack>
    ) : null;

  const renderMessage = (message: ChatMessage) => {
    const me = message.role === "user";
    const streaming = message.state === "streaming";
    const complete = message.state === "complete";
    if (me) {
      return (
        <ChatMessageItem key={message.id} me>
          <Box component="span" sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
            {message.text}
          </Box>
          {fileChips(message.files)}
        </ChatMessageItem>
      );
    }
    const source = formatSource(message.source);
    return (
      <ChatMessageItem
        key={message.id}
        me={false}
        info={CHROME.sender}
        firstName={CHROME.sender}
        avatar={<MeshaLogo size={32} />}
        slotProps={message.state === "error" ? { ...ANSWER_SLOTS, body: ERROR_BODY } : ANSWER_SLOTS}
        actions={
          complete && message.id !== "hello" && message.text && message.text !== CHROME.stoppedEmpty ? (
            <CopyButton text={message.text} />
          ) : undefined
        }
      >
        {message.steps?.length ? (
          <AgentSteps steps={message.steps} live={streaming} startedAt={message.startedAt} workedMs={message.workedMs} />
        ) : null}
        {message.watch ? (
          <CeoAiWatchCard
            watch={message.watch}
            onStop={streaming ? () => sendCeoAiWatchStop(runRequestIdRef.current) : undefined}
          />
        ) : null}
        {message.text ? <CeoAiMarkdown text={message.text} /> : null}
        {fileChips(message.files)}
        {/* Chart belongs to the answer: inside its bubble, above its Copy action. */}
        {complete && message.chart ? <CeoAiChart chart={message.chart} /> : null}
        {streaming && message.text && message.checking ? (
          <Typography variant="caption" role="status" aria-live="polite" sx={{ mt: 1, color: "text.secondary" }}>
            {CHROME.checkingAnswer}
          </Typography>
        ) : null}
        {streaming && !message.text && !message.steps?.length ? (
          <Stack direction="row" spacing={1} role="status" aria-label={copy.checking} sx={{ alignItems: "center", color: "text.secondary" }}>
            <CircularProgress size={16} color="inherit" />
            {message.progress ? (
              <Typography variant="caption" aria-live="polite">
                {message.progress}
              </Typography>
            ) : null}
          </Stack>
        ) : null}
        {complete && message.citations?.length ? (
          <Stack direction="row" sx={{ flexWrap: "wrap", gap: 0.75, mt: 1.5 }}>
            {message.citations.map((cite, i) => {
              const freshness = formatFreshness(cite.as_of);
              return (
                <Label key={`${message.id}-c${i}`} variant="soft" color={cite.tier === "cube" ? "info" : "default"}>
                  {formatCitationSurface(cite.surface)}
                  {freshness ? ` · ${freshness}` : ""}
                </Label>
              );
            })}
          </Stack>
        ) : null}
        {complete && message.id !== "hello" && message.mode !== "agent" ? (
          <Typography variant="caption" sx={{ mt: 1, color: message.mode === "degraded" ? "error.main" : "text.disabled" }}>
            {source ? `${source} · ` : ""}
            {modeLabel(message.mode, copy)}
          </Typography>
        ) : null}
      </ChatMessageItem>
    );
  };

  const windowAction = (label: string, icon: string, onClick: () => void, extra?: { hideOnPhone?: boolean; expanded?: boolean }) => (
    <IconButton
      aria-label={label}
      title={label}
      aria-expanded={extra?.expanded}
      sx={extra?.hideOnPhone ? { display: { xs: "none", sm: "inline-flex" } } : undefined}
      onClick={(e) => {
        e.stopPropagation();
        onClick();
      }}
    >
      <Iconify icon={icon as "mingcute:close-line"} />
    </IconButton>
  );

  return (
    <Box sx={frameSx(view)}>
      <ChatLayout
        role="region"
        aria-label={copy.title}
        sx={windowSx(view)}
        // Minimized: the bar restores on a click; otherwise a double click on the header toggles
        // the full-window size (the header buttons handle their own clicks).
        onClick={view === "min" ? () => setView("normal") : undefined}
        onDoubleClick={(e) => {
          if (view === "min" || (e.target as HTMLElement).closest("button")) return;
          if (e.clientY - e.currentTarget.getBoundingClientRect().top > HEADER_HEIGHT) return;
          setView(view === "max" ? "normal" : "max");
        }}
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
          if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDragging(false);
        }}
        onDrop={(e) => {
          e.preventDefault();
          setDragging(false);
          addFiles(e.dataTransfer.files);
          if (view === "min") setView("normal");
        }}
        slots={{
          nav:
            view === "min" ? null : (
              <ChatNav
                title={<Typography variant="h6">{CHROME.threads}</Typography>}
                collapseNav={conversationsNav}
                onCompose={() => void startNewChat()}
                composeLabel={CHROME.newChat}
                toggleLabel={CHROME.toggleThreads}
              >
                {conversations.length === 0 ? (
                  conversationsNav.collapseDesktop ? null : (
                    <Typography component="li" variant="body2" sx={{ px: 2.5, py: 1.5, color: "text.secondary" }}>
                      {CHROME.noThreads}
                    </Typography>
                  )
                ) : (
                  conversations.map((thread) => (
                    <ChatNavItem
                      key={thread.id}
                      conversation={{ id: thread.id, unreadCount: 0 }}
                      selected={thread.id === conversationId}
                      collapse={conversationsNav.collapseDesktop}
                      onCloseMobile={conversationsNav.onCloseMobile}
                      displayName={thread.title || CHROME.newChat}
                      lastActivity={thread.updated_at ? fmtDate(thread.updated_at) : undefined}
                      avatar={<MeshaLogo size={48} />}
                      onClickConversation={(id) => void resumeThread(id)}
                    />
                  ))
                )}
              </ChatNav>
            ),
          header: (
            <ChatHeaderDetails
              name={copy.title}
              status={copy.subtitle}
              avatar={<MeshaLogo size={40} />}
              collapseNav={conversationsNav}
              toggleLabel={CHROME.toggleThreads}
              moreLabel={CHROME.moreActions}
              deleteLabel={CHROME.delete}
              onDelete={conversationId ? () => setConfirmDelete(conversationId) : undefined}
              menuActions={(close) => (
                <MenuItem
                  onClick={() => {
                    close();
                    setRenameText(current?.title ?? "");
                    setRenaming(conversationId ?? null);
                  }}
                >
                  <Iconify icon="solar:pen-bold" />
                  {CHROME.rename}
                </MenuItem>
              )}
              actions={
                <>
                  {windowAction(view === "min" ? "Restore panel" : "Minimize", view === "min" ? "eva:arrow-ios-upward-fill" : "mingcute:minimize-line", () => setView(view === "min" ? "normal" : "min"), { expanded: view !== "min" })}
                  {windowAction(view === "max" ? "Restore size" : "Maximize", view === "max" ? "solar:quit-full-screen-square-outline" : "solar:full-screen-square-outline", () => setView(view === "max" ? "normal" : "max"), { hideOnPhone: true })}
                  {windowAction(copy.close, "mingcute:close-line", () => {
                    setOpen(false);
                    setView("normal");
                  })}
                </>
              }
            />
          ),
          main: (
            <>
              {dragging ? (
                <Box sx={DROP_SX}>
                  <Typography variant="h6">{CHROME.drop}</Typography>
                </Box>
              ) : null}
              <ChatMessageList loading={threadLoading} scrollRef={scrollRef}>
                {messages.length === 0 ? (
                  <EmptyContent
                    title={copy.hello}
                    description={copy.helloMeta}
                    action={<Box sx={{ mt: 3 }}>{starterChips}</Box>}
                  />
                ) : (
                  messages.map(renderMessage)
                )}
              </ChatMessageList>

              {banner ? (
                <Alert severity={banner.kind === "err" ? "error" : "warning"} sx={{ mx: 2, mb: 1.5 }}>
                  {banner.text}
                </Alert>
              ) : null}

              {messages.length > 0 ? (
                <Box sx={{ px: 2, pb: 1 }}>
                  <Button
                    size="small"
                    color="inherit"
                    startIcon={<Iconify icon="solar:atom-bold-duotone" />}
                    onClick={() => setShowStarters((v) => !v)}
                    aria-expanded={startersVisible}
                  >
                    {CHROME.suggestions}
                  </Button>
                  <Collapse in={startersVisible}>
                    <Box sx={{ pt: 1 }}>{starterChips}</Box>
                  </Collapse>
                </Box>
              ) : null}

              {previews.length ? (
                <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1, px: 2, pb: 1.5 }}>
                  {previews.map((f, i) => (
                    <Thumb
                      key={f.url}
                      file={f}
                      onOpen={() => setLightbox({ files: previews, start: i })}
                      onRemove={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
                    />
                  ))}
                </Stack>
              ) : null}

              <ChatMessageInput
                value={input}
                disabled={false}
                placeholder={listening ? "Listening…" : narrow ? "Ask Mesha…" : copy.placeholder}
                inputLabel={CHROME.inputLabel}
                attachLabel={CHROME.attach}
                accept="image/png,image/jpeg,image/gif,image/webp,application/pdf,.csv,.tsv,.txt,.md,.json"
                onChange={setInput}
                onAttach={addFiles}
                onKeyDown={(e) => {
                  if (e.key === "Escape" && pending) {
                    e.preventDefault();
                    e.stopPropagation();
                    stopGenerating();
                    return;
                  }
                  if (e.key === "Enter" && !e.shiftKey) {
                    e.preventDefault();
                    void ask(input, files);
                  }
                }}
                onPaste={(e) => {
                  if (e.clipboardData.files.length) {
                    e.preventDefault();
                    addFiles(e.clipboardData.files);
                  }
                }}
                voice={
                  speechSupported ? (
                    <IconButton
                      color={listening ? "error" : "default"}
                      onClick={toggleVoice}
                      aria-pressed={listening}
                      aria-label={listening ? "Stop voice input" : "Voice input"}
                      title={listening ? "Stop voice input" : "Voice input"}
                    >
                      <Iconify icon={listening ? "solar:stop-circle-bold" : "solar:microphone-bold"} />
                    </IconButton>
                  ) : undefined
                }
                send={
                  pending && !input.trim() && !files.length ? (
                    <IconButton color="error" onClick={stopGenerating} aria-label={CHROME.stop} title="Stop (Esc)">
                      <Iconify icon="solar:stop-circle-bold" />
                    </IconButton>
                  ) : (
                    <IconButton
                      color="primary"
                      onClick={() => void ask(input, files)}
                      aria-label={copy.send}
                      disabled={!input.trim() && !files.length}
                    >
                      <Iconify icon="custom:send-fill" />
                    </IconButton>
                  )
                }
              />
            </>
          ),
          details: null,
        }}
      />

      {lightbox ? <Lightbox files={lightbox.files} start={lightbox.start} onClose={() => setLightbox(null)} /> : null}

      <Dialog open={renaming !== null} onClose={() => setRenaming(null)} fullWidth maxWidth="xs">
        <DialogTitle>{CHROME.renameTitle}</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            fullWidth
            label={CHROME.rename}
            value={renameText}
            onChange={(e) => setRenameText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && renaming) void commitRename(renaming);
            }}
            sx={{ mt: 1 }}
          />
        </DialogContent>
        <DialogActions>
          <Button variant="outlined" color="inherit" onClick={() => setRenaming(null)}>
            {CHROME.cancel}
          </Button>
          <Button variant="contained" color="primary" onClick={() => renaming && void commitRename(renaming)}>
            {CHROME.save}
          </Button>
        </DialogActions>
      </Dialog>

      <ConfirmDialog
        open={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        title={CHROME.deleteTitle}
        action={
          <Button
            variant="contained"
            color="error"
            onClick={() => {
              const id = confirmDelete;
              setConfirmDelete(null);
              if (id) void removeThread(id);
            }}
          >
            {CHROME.delete}
          </Button>
        }
      />
    </Box>
  );
}
