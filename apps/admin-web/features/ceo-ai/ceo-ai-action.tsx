"use client";

// Ask Mesha write-action card. The agent never writes on its own: it streams an
// `action_proposal`, this card shows what will change, and only the CEO's
// Confirm (typed CONFIRM for high risk) sends the write. Cancel withdraws it.
// The outcome also arrives as a chat message; the card shows it in plain words.

import { useEffect, useId, useRef, useState, type ReactElement } from "react";
import { parseActionProposal } from "@/lib/ceo-ai-stream";
import type { ActionLink, ActionState, ActionStatus } from "./types";

const CONFIRM_WORD = "CONFIRM";

const ERROR_WORDS: Record<string, string> = {
  expired: "This action expired before it was confirmed. Ask again to get a fresh one.",
  proposal_expired: "This action expired before it was confirmed. Ask again to get a fresh one.",
  not_found: "This action is no longer available. Ask again to get a fresh one.",
  proposal_not_found: "This action is no longer available. Ask again to get a fresh one.",
  already_done: "This action was already handled.",
  forbidden: "You don't have permission to make this change.",
  leadership_required: "You don't have permission to make this change.",
  unauthorized: "Your session ended. Sign in again and retry.",
  confirm_text_required: `Type ${CONFIRM_WORD} to confirm this change.`,
  actions_unavailable: "Actions are not available on this assistant yet.",
  assistant_unreachable: "Ask Mesha could not be reached. Nothing was changed. Try again.",
  rate_limited: "Too many requests. Wait a moment and try again.",
};

const STATUS_MAP: Record<string, ActionStatus> = {
  pending: "pending",
  succeeded: "succeeded",
  success: "succeeded",
  done: "succeeded",
  executed: "succeeded",
  completed: "succeeded",
  confirmed: "succeeded",
  cancelled: "cancelled",
  canceled: "cancelled",
  expired: "expired",
  failed: "failed",
  error: "failed",
};

function str(v: unknown, max = 400): string | undefined {
  return typeof v === "string" && v.trim() ? v.trim().slice(0, max) : undefined;
}

// Only same-origin paths and https links become anchors; anything else is text.
function safeHref(v: unknown): string | undefined {
  const h = str(v, 1000);
  if (!h) return undefined;
  if (h.startsWith("/") && !h.startsWith("//")) return h;
  return /^https:\/\//i.test(h) ? h : undefined;
}

function parseLink(result: unknown): ActionLink | undefined {
  if (!result || typeof result !== "object") return undefined;
  const r = result as Record<string, unknown>;
  const nested = r.link && typeof r.link === "object" ? (r.link as Record<string, unknown>) : undefined;
  const text = str(nested?.text, 160) ?? str(r.link_text, 160);
  const href = safeHref(nested?.href ?? nested?.url ?? r.link_url ?? r.href ?? r.url);
  if (!text && !href) return undefined;
  return { text: text ?? "Open", href };
}

function isExpired(a: { expiresAt?: string }, now: number): boolean {
  return !!a.expiresAt && Date.parse(a.expiresAt) <= now;
}

// Outcome of a confirm/cancel POST, from the {ok, status, message, result} body.
// A transport failure (no JSON, 5xx proxy error with no status) leaves the card
// pending so the CEO can retry; the backend owns idempotency.
export function actionOutcome(
  verb: "confirm" | "cancel",
  httpStatus: number,
  body: Record<string, unknown> | null,
): Pick<ActionState, "status" | "message" | "link"> {
  const code = str(body?.status, 60)?.toLowerCase() ?? str(body?.error, 60)?.toLowerCase();
  const mapped = code ? STATUS_MAP[code] : undefined;
  const words = (code && ERROR_WORDS[code]) || undefined;
  const message = str(body?.message) ?? words;
  const link = parseLink(body?.result);
  if (body?.ok === true) {
    const status = mapped && mapped !== "pending" && mapped !== "failed" ? mapped : verb === "cancel" ? "cancelled" : "succeeded";
    return {
      status,
      message: message ?? (status === "cancelled" ? "Cancelled. Nothing was changed." : "Done."),
      link,
    };
  }
  if (httpStatus === 410 || mapped === "expired" || code === "proposal_expired") {
    return { status: "expired", message: message ?? ERROR_WORDS.expired };
  }
  if (mapped === "succeeded" || mapped === "cancelled") {
    return { status: mapped, message: message ?? ERROR_WORDS.already_done, link };
  }
  // Retryable: the change never reached the server or it asked for the typed word.
  if (!body || httpStatus >= 500 || httpStatus === 429 || code === "confirm_text_required" || code === "assistant_unreachable") {
    return { status: "pending", message: message ?? "Something went wrong. Nothing was changed. Try again." };
  }
  return { status: "failed", message: message ?? `The change could not be made (error ${httpStatus}).` };
}

// Card state for a reloaded chat. The stored message carries the proposal and,
// once decided, its status/message/result. A proposal saved without a status
// cannot be proven still open, so it shows as expired rather than live buttons.
export function restoreAction(raw: unknown, now = Date.now()): ActionState | undefined {
  const proposal = parseActionProposal(raw);
  if (!proposal) return undefined;
  const r = raw as Record<string, unknown>;
  const code = str(r.status, 60)?.toLowerCase();
  let status: ActionStatus = (code && STATUS_MAP[code]) || "expired";
  if (status === "pending" && isExpired(proposal, now)) status = "expired";
  return {
    ...proposal,
    status,
    message: str(r.message) ?? (status === "expired" ? "This action expired. Ask again to get a fresh one." : undefined),
    link: parseLink(r.result),
  };
}

async function postAction(
  id: string,
  verb: "confirm" | "cancel",
  confirmText: string | undefined,
): Promise<Pick<ActionState, "status" | "message" | "link">> {
  try {
    const res = await fetch(`/api/ceo-ai/actions/${encodeURIComponent(id)}/${verb}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(confirmText ? { confirm_text: confirmText } : {}),
      cache: "no-store",
    });
    let body: Record<string, unknown> | null = null;
    try {
      const parsed = (await res.json()) as unknown;
      body = parsed && typeof parsed === "object" ? (parsed as Record<string, unknown>) : null;
    } catch {
      body = null;
    }
    return actionOutcome(verb, res.status, body);
  } catch {
    return { status: "pending", message: "Could not reach Ask Mesha. Nothing was changed. Check your connection and try again." };
  }
}

function formatUntil(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getDate())}/${p(d.getMonth() + 1)}/${d.getFullYear()} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

const STATUS_TITLE: Record<ActionStatus, string> = {
  pending: "Needs your confirmation",
  executing: "Working…",
  succeeded: "Done",
  failed: "Not done",
  cancelled: "Cancelled",
  expired: "Expired",
};

export function CeoAiActionCard(props: {
  action: ActionState;
  onChange: (next: Partial<ActionState>) => void;
}): ReactElement {
  const { action, onChange } = props;
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState<"confirm" | "cancel" | null>(null);
  const inflight = useRef(false);
  const inputId = useId();
  const hintId = useId();

  // Flip to expired at expires_at while still open.
  useEffect(() => {
    if (action.status !== "pending" || !action.expiresAt) return;
    const left = Date.parse(action.expiresAt) - Date.now();
    const expire = () =>
      onChange({ status: "expired", message: "This action expired before it was confirmed. Ask again to get a fresh one." });
    if (left <= 0) {
      expire();
      return;
    }
    const t = setTimeout(expire, Math.min(left, 2_147_000_000));
    return () => clearTimeout(t);
  }, [action.status, action.expiresAt, onChange]);

  const open = action.status === "pending";
  const executing = action.status === "executing" || busy !== null;
  const needsWord = action.requiresDoubleConfirm;
  const wordOk = !needsWord || typed.trim() === CONFIRM_WORD;
  const canConfirm = open && !executing && wordOk;

  const run = async (verb: "confirm" | "cancel") => {
    // One request at a time, ever: the ref blocks double clicks before React re-renders.
    if (inflight.current || action.status !== "pending") return;
    if (verb === "confirm" && !wordOk) return;
    if (isExpired(action, Date.now())) {
      onChange({ status: "expired", message: ERROR_WORDS.expired });
      return;
    }
    inflight.current = true;
    setBusy(verb);
    onChange({ status: "executing", message: undefined });
    const outcome = await postAction(action.proposalId, verb, verb === "confirm" && needsWord ? CONFIRM_WORD : undefined);
    inflight.current = false;
    setBusy(null);
    onChange(outcome);
  };

  const tone = action.status === "succeeded" ? "ok" : action.status === "failed" ? "dng" : action.status === "pending" || action.status === "executing" ? "live" : "mut";
  const until = open ? formatUntil(action.expiresAt) : "";

  return (
    <section className={`mzai-act ${tone}${action.risk === "high" ? " high" : ""}`} aria-label={`Action: ${action.title}`}>
      <header className="mzai-act-head">
        <div className="mzai-act-htext">
          <span className="mzai-act-kicker">{STATUS_TITLE[action.status]}</span>
          <strong className="mzai-act-title">{action.title}</strong>
        </div>
        <span className={`mzai-act-risk ${action.risk}`}>{action.risk === "high" ? "High risk" : "Normal risk"}</span>
      </header>
      {action.summary.length ? (
        <ul className="mzai-act-sum">
          {action.summary.map((line, i) => (
            <li key={i}>{line}</li>
          ))}
        </ul>
      ) : null}
      {open || action.status === "executing" ? (
        <div className="mzai-act-controls">
          {needsWord ? (
            <div className="mzai-act-word">
              <label htmlFor={inputId}>
                Type <b>{CONFIRM_WORD}</b> to enable Confirm
              </label>
              <input
                id={inputId}
                value={typed}
                onChange={(e) => setTyped(e.target.value.toUpperCase().slice(0, 12))}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && canConfirm) {
                    e.preventDefault();
                    void run("confirm");
                  }
                }}
                disabled={!open || executing}
                autoComplete="off"
                autoCapitalize="characters"
                spellCheck={false}
                aria-describedby={hintId}
                placeholder={CONFIRM_WORD}
              />
            </div>
          ) : null}
          <div className="mzai-act-btns">
            <button
              type="button"
              className="mzai-act-cancel"
              onClick={() => void run("cancel")}
              disabled={!open || executing}
              aria-busy={busy === "cancel"}
            >
              {busy === "cancel" ? <span className="mzai-act-spin" aria-hidden="true" /> : null}
              Cancel
            </button>
            <button
              type="button"
              className="mzai-act-confirm"
              onClick={() => void run("confirm")}
              disabled={!canConfirm}
              aria-busy={busy === "confirm"}
            >
              {busy === "confirm" ? <span className="mzai-act-spin" aria-hidden="true" /> : null}
              {busy === "confirm" ? "Confirming…" : "Confirm"}
            </button>
          </div>
          <p className="mzai-act-hint" id={hintId}>
            {executing ? "Sending your decision. Please wait." : `Nothing changes until you confirm${until ? `. Open until ${until}` : ""}.`}
          </p>
        </div>
      ) : null}
      <div className="mzai-act-status" role="status" aria-live="polite">
        {action.message ? (
          <p className={`mzai-act-msg ${tone}`}>
            {action.message}
            {action.link ? (
              action.link.href ? (
                <>
                  {" "}
                  <a href={action.link.href} target={action.link.href.startsWith("/") ? undefined : "_blank"} rel="noreferrer">
                    {action.link.text}
                  </a>
                </>
              ) : (
                <> {action.link.text}</>
              )
            ) : null}
          </p>
        ) : null}
      </div>
    </section>
  );
}

// Styles (brand tokens only; light/dark come from the shared theme tokens).
export const ACTION_CSS = `
.mzai-act{border:1px solid var(--line);border-radius:12px;background:var(--panel);margin:4px 0 10px;overflow:hidden;max-width:100%}
.mzai-act.live{border-color:color-mix(in srgb,var(--brand) 45%,var(--line))}
.mzai-act.live.high{border-color:color-mix(in srgb,var(--danger) 55%,var(--line))}
.mzai-act-head{display:flex;align-items:flex-start;gap:10px;padding:10px 12px;border-bottom:1px solid var(--line);background:var(--panel-2)}
.mzai-act-htext{display:flex;flex-direction:column;min-width:0;flex:1}
.mzai-act-kicker{font-size:11px;font-weight:700;letter-spacing:.04em;text-transform:uppercase;color:var(--muted)}
.mzai-act.ok .mzai-act-kicker{color:var(--brand-d)}
.mzai-act.dng .mzai-act-kicker{color:var(--danger)}
.mzai-act-title{font-size:13.5px;color:var(--ink);line-height:1.35;overflow-wrap:anywhere}
.mzai-act-risk{flex:none;border-radius:999px;padding:2px 9px;font-size:11px;font-weight:700;color:var(--ink);background:color-mix(in srgb,var(--muted) 14%,transparent)}
.mzai-act-risk.high{background:var(--dangerx);color:var(--danger)}
.mzai-act-sum{margin:0;padding:10px 12px 4px 30px;font-size:12.5px;line-height:1.5;color:var(--ink)}
.mzai-act-sum li{margin:0 0 4px;overflow-wrap:anywhere}
.mzai-act-controls{padding:6px 12px 10px}
.mzai-act-word{display:flex;flex-direction:column;gap:4px;margin:4px 0 8px;font-size:12px;color:var(--muted)}
.mzai-act-word b{color:var(--ink);letter-spacing:.04em}
.mzai-act-word input{width:160px;max-width:100%;font:inherit;font-size:13px;font-weight:600;letter-spacing:.06em;border:1px solid var(--line);border-radius:8px;padding:7px 10px;background:var(--bg);color:var(--ink);outline:0;min-height:36px}
.mzai-act-word input:focus-visible{border-color:var(--brand);box-shadow:0 0 0 3px var(--brand-soft)}
.mzai-act-btns{display:flex;gap:8px;justify-content:flex-end;flex-wrap:wrap}
.mzai-act-btns button{display:inline-flex;align-items:center;justify-content:center;gap:6px;min-height:36px;min-width:96px;border-radius:999px;padding:6px 16px;font:inherit;font-size:13px;font-weight:600;cursor:pointer;transition:background .15s,opacity .15s}
.mzai-act-btns button:focus-visible{outline:2px solid var(--brand);outline-offset:2px}
.mzai-act-cancel{border:1.5px solid var(--line2,var(--line));background:var(--panel);color:var(--ink)}
.mzai-act-cancel:hover:not(:disabled){background:var(--sidebar-2)}
.mzai-act-confirm{border:1.5px solid var(--brand);background:var(--brand);color:var(--on-brand,#fff)}
.mzai-act-confirm:hover:not(:disabled){background:var(--brand-d);border-color:var(--brand-d)}
.mzai-act-btns button:disabled{opacity:.5;cursor:not-allowed}
.mzai-act-hint{margin:8px 0 0;font-size:11.5px;color:var(--muted);text-align:right}
.mzai-act-status:empty{display:none}
.mzai-act-msg{margin:0;padding:9px 12px;font-size:12.5px;line-height:1.45;color:var(--ink);border-top:1px solid var(--line);overflow-wrap:anywhere}
.mzai-act-msg.ok{background:var(--okx)}
.mzai-act-msg.dng{background:var(--dangerx)}
.mzai-act-msg.live{background:var(--warnx)}
.mzai-act-msg a{color:var(--brand-d);font-weight:600;text-decoration:underline;text-underline-offset:2px}
.mzai-act-spin{width:12px;height:12px;border-radius:50%;border:2px solid currentColor;border-right-color:transparent;animation:mzai-act-spin .8s linear infinite}
@keyframes mzai-act-spin{to{transform:rotate(360deg)}}
@media (max-width:620px){.mzai-act-btns button{flex:1 1 0}.mzai-act-hint{text-align:left}}
@media (prefers-reduced-motion:reduce){.mzai-act-spin{animation:none}}
`;
