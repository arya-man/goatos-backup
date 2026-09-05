"use client";

import { useCallback, useEffect, useState } from "react";

import { CeoAiAdminEvents, trackCeoAiAdminError, trackCeoAiAdminEvent } from "./telemetry";
import type { TraceError, TraceRecord } from "./types";
import { dateTime } from "@/lib/format";

// CeoAiAdminTraceViewer is the ADMIN-ONLY step-trace debug surface. An
// engineer/admin (ceo_internal/superadmin — enforced server-side by the
// backend) enters a request_id and sees the internal execution trace:
// sub-questions, resolved tool/tier, redacted params, row counts, latency, and
// the review verdict. This is the sanctioned way to "see each step" WITHOUT
// leaking the trace into the leadership chat answer (Internal Tracking rule).
//
// The component holds no business data and no gating logic: the proxy + backend
// own auth. A non-admin session receives 403, surfaced honestly below.

type ViewState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "loaded"; trace: TraceRecord }
  | { kind: "error"; status: number; message: string };

// Status tones are theme tokens, never raw palette values, so the trace viewer
// reads as the same product in both the dark `:root` and `:root.light` themes.
const STATUS_TONE: Record<string, React.CSSProperties> = {
  ok: { background: "var(--okx)", color: "var(--ok)" },
  rejected: { background: "var(--warnx)", color: "var(--warn)" },
  over_budget: { background: "var(--warnx)", color: "var(--warn)" },
  degraded: { background: "var(--warnx)", color: "var(--warn)" },
  error: { background: "var(--dangerx)", color: "var(--danger)" },
};

const NEUTRAL_TONE: React.CSSProperties = { background: "var(--panel-2)", color: "var(--muted)" };

function toneFor(status: string): React.CSSProperties {
  return STATUS_TONE[status] ?? NEUTRAL_TONE;
}

const SURFACE: React.CSSProperties = {
  background: "var(--panel)",
  border: "1px solid var(--line)",
  borderRadius: "var(--r)",
};

const CHIP: React.CSSProperties = {
  background: "var(--panel-2)",
  color: "var(--muted)",
  borderRadius: "var(--r-pill)",
};

const UPPER_LABEL: React.CSSProperties = {
  fontFamily: "var(--fm)",
  letterSpacing: ".18em",
  textTransform: "uppercase",
  color: "var(--faint)",
};

async function fetchTrace(requestId: string): Promise<{ ok: true; trace: TraceRecord } | { ok: false; status: number; message: string }> {
  const res = await fetch(`/api/ceo-ai/admin/trace/${encodeURIComponent(requestId)}`, {
    method: "GET",
    headers: { Accept: "application/json" },
    cache: "no-store",
  });
  if (res.ok) {
    const trace = (await res.json()) as TraceRecord;
    return { ok: true, trace };
  }
  let message = `Request failed (${res.status}).`;
  try {
    const body = (await res.json()) as TraceError;
    if (body?.error === "leadership_required" || body?.error === "admin_required" || res.status === 403) {
      message = "You do not have permission to view assistant traces (admin only).";
    } else if (body?.error === "trace_not_found" || res.status === 404) {
      message = "No trace found for that request id in your tenant.";
    } else if (body?.message) {
      message = body.message;
    } else if (body?.error) {
      message = body.error;
    }
  } catch {
    // keep default message
  }
  return { ok: false, status: res.status, message };
}

export function CeoAiAdminTraceViewer({ initialRequestId = "" }: { initialRequestId?: string }): React.ReactElement {
  const [requestId, setRequestId] = useState(initialRequestId);
  const [view, setView] = useState<ViewState>({ kind: "idle" });

  useEffect(() => {
    trackCeoAiAdminEvent(CeoAiAdminEvents.Open);
  }, []);

  const lookup = useCallback(async (id: string) => {
    const trimmed = id.trim();
    if (!trimmed) {
      setView({ kind: "error", status: 400, message: "Enter a request id to look up its trace." });
      return;
    }
    setView({ kind: "loading" });
    trackCeoAiAdminEvent(CeoAiAdminEvents.Lookup);
    try {
      const result = await fetchTrace(trimmed);
      if (result.ok) {
        setView({ kind: "loaded", trace: result.trace });
        trackCeoAiAdminEvent(CeoAiAdminEvents.Loaded, {
          route_tier: result.trace.route_tier || "unknown",
          status: result.trace.status || "unknown",
          steps: String(result.trace.steps?.length ?? 0),
        });
      } else {
        setView({ kind: "error", status: result.status, message: result.message });
        trackCeoAiAdminError("lookup", result.message, result.status);
      }
    } catch (err) {
      const message = err instanceof Error ? err.message : "The trace service is not reachable.";
      setView({ kind: "error", status: 0, message });
      trackCeoAiAdminError("lookup_exception", message);
    }
  }, []);

  useEffect(() => {
    const id = initialRequestId.trim();
    if (!id) return;
    // Defer out of the effect body so the initial fetch's setState does not run
    // synchronously during the effect (react-hooks/set-state-in-effect).
    queueMicrotask(() => void lookup(id));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <section className="flex flex-col gap-6" style={{ color: "var(--ink)" }}>
      <header className="flex flex-col gap-1">
        <h1 className="text-lg" style={{ fontFamily: "var(--f-serif)", fontWeight: 400, color: "var(--ink)" }}>Assistant step-trace (admin debug)</h1>
        <p className="max-w-2xl text-sm" style={{ color: "var(--muted)" }}>
          Internal execution trace for one leadership-assistant request: sub-questions, resolved tool/tier,
          redacted params, row counts, latency, and review verdict. Admin/engineering only — this trace never
          appears in the leadership chat answer.
        </p>
      </header>

      <form
        className="flex flex-wrap items-end gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          void lookup(requestId);
        }}
      >
        <label className="flex flex-col gap-1 text-xs" style={UPPER_LABEL}>
          Request ID
          <input
            className="w-96 max-w-full px-3 py-2 text-sm outline-none"
            style={{
              background: "var(--panel)",
              border: "1px solid var(--line)",
              borderRadius: "var(--r)",
              color: "var(--ink)",
              fontFamily: "var(--f)",
            }}
            value={requestId}
            onChange={(e) => setRequestId(e.target.value)}
            placeholder="e.g. 9f2c1b7a-..."
            spellCheck={false}
            autoComplete="off"
          />
        </label>
        <button
          type="submit"
          className="btn p"
          disabled={view.kind === "loading"}
        >
          {view.kind === "loading" ? "Looking up…" : "Look up trace"}
        </button>
      </form>

      {view.kind === "error" ? (
        <div
          className="px-4 py-3 text-sm"
          style={{
            background: "var(--dangerx)",
            border: "1px solid var(--danger)",
            borderRadius: "var(--r)",
            color: "var(--danger)",
          }}
        >
          {view.message}
        </div>
      ) : null}

      {view.kind === "loaded" ? <TraceDetail trace={view.trace} /> : null}
    </section>
  );
}

function TraceDetail({ trace }: { trace: TraceRecord }): React.ReactElement {
  const steps = trace.steps ?? [];
  const views = trace.source_views ?? [];
  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Meta label="Status" value={<span className="px-2 py-0.5 text-xs" style={{ ...toneFor(trace.status), borderRadius: "var(--r-pill)" }}>{trace.status || "—"}</span>} />
        <Meta label="Route tier" value={trace.route_tier || "—"} />
        <Meta label="Tool" value={trace.tool_called || "—"} />
        <Meta label="Latency" value={`${trace.latency_ms} ms`} />
        <Meta label="Rows" value={String(trace.row_count)} />
        <Meta label="Actor role" value={trace.actor_role || "—"} />
        <Meta label="Model" value={trace.model_version || "—"} />
        <Meta label="Prompt" value={trace.prompt_version || "—"} />
      </div>

      <Panel title="Question (redacted)">
        <p className="whitespace-pre-wrap break-words text-sm" style={{ color: "var(--muted)" }}>{trace.question_redacted || "—"}</p>
      </Panel>

      {trace.rejection_reason ? (
        <Panel title="Rejection reason">
          <p className="text-sm" style={{ color: "var(--warn)" }}>{trace.rejection_reason}</p>
        </Panel>
      ) : null}

      {trace.review_verdict ? (
        <Panel title="Review verdict">
          <p className="text-sm" style={{ color: "var(--muted)" }}>{trace.review_verdict}</p>
        </Panel>
      ) : null}

      {views.length > 0 ? (
        <Panel title="Source views">
          <div className="flex flex-wrap gap-2">
            {views.map((v) => (
              <span key={v} className="px-2 py-0.5 text-xs" style={CHIP}>
                {v}
              </span>
            ))}
          </div>
        </Panel>
      ) : null}

      <Panel title={`Steps (${steps.length})`}>
        {steps.length === 0 ? (
          <p className="text-sm" style={{ color: "var(--faint)" }}>No steps recorded for this request.</p>
        ) : (
          <ol className="flex flex-col gap-3">
            {steps.map((step, i) => (
              <li key={i} className="p-3" style={{ ...SURFACE, background: "var(--panel-2)" }}>
                <div className="mb-1 flex flex-wrap items-center gap-2 text-xs" style={{ color: "var(--muted)" }}>
                  <span className="px-2 py-0.5" style={{ ...CHIP, background: "var(--panel)", color: "var(--ink)" }}>#{i + 1}</span>
                  <span className="px-2 py-0.5" style={{ ...CHIP, background: "var(--panel)" }}>{step.route || "—"}</span>
                  <span className="font-medium" style={{ color: "var(--ink)" }}>{step.tool_name || "—"}</span>
                  <span className="ml-auto">{step.duration_ms} ms · {step.row_count} rows</span>
                </div>
                {step.sub_question ? (
                  <p className="text-sm" style={{ color: "var(--muted)" }}>{step.sub_question}</p>
                ) : null}
                {step.params ? (
                  <pre
                    className="mt-2 overflow-x-auto p-2 text-xs"
                    style={{ background: "var(--bg)", border: "1px solid var(--line2)", borderRadius: "var(--r)", color: "var(--muted)", fontFamily: "var(--fm)" }}
                  >{step.params}</pre>
                ) : null}
                {step.verdict ? <p className="mt-1 text-xs" style={{ color: "var(--muted)" }}>verdict: {step.verdict}</p> : null}
                {step.err ? <p className="mt-1 text-xs" style={{ color: "var(--danger)" }}>error: {step.err}</p> : null}
              </li>
            ))}
          </ol>
        )}
      </Panel>

      <p className="text-xs" style={{ color: "var(--faint)" }}>Recorded {dateTime(trace.created_at)}</p>
    </div>
  );
}

function Meta({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return (
    <div className="px-3 py-2" style={SURFACE}>
      <div className="text-[11px]" style={UPPER_LABEL}>{label}</div>
      <div className="mt-0.5 text-sm" style={{ color: "var(--ink)" }}>{value}</div>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="p-4" style={SURFACE}>
      <h2 className="mb-2 text-sm font-semibold" style={{ color: "var(--ink)" }}>{title}</h2>
      {children}
    </div>
  );
}
