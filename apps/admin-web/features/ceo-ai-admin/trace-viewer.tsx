"use client";

import { useCallback, useEffect, useState } from "react";

import { CeoAiAdminEvents, trackCeoAiAdminError, trackCeoAiAdminEvent } from "./telemetry";
import type { TraceError, TraceRecord } from "./types";

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

const STATUS_TONE: Record<string, string> = {
  ok: "bg-emerald-500/15 text-emerald-300",
  rejected: "bg-amber-500/15 text-amber-300",
  over_budget: "bg-amber-500/15 text-amber-300",
  degraded: "bg-amber-500/15 text-amber-300",
  error: "bg-rose-500/15 text-rose-300",
};

function toneFor(status: string): string {
  return STATUS_TONE[status] ?? "bg-slate-500/15 text-slate-300";
}

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
    <section className="flex flex-col gap-6 text-slate-200">
      <header className="flex flex-col gap-1">
        <h1 className="text-lg font-semibold text-slate-100">Assistant step-trace (admin debug)</h1>
        <p className="max-w-2xl text-sm text-slate-400">
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
        <label className="flex flex-col gap-1 text-xs text-slate-400">
          Request ID
          <input
            className="w-96 max-w-full rounded-md border border-slate-700 bg-slate-900 px-3 py-2 text-sm text-slate-100 outline-none focus:border-slate-500"
            value={requestId}
            onChange={(e) => setRequestId(e.target.value)}
            placeholder="e.g. 9f2c1b7a-..."
            spellCheck={false}
            autoComplete="off"
          />
        </label>
        <button
          type="submit"
          className="rounded-md bg-slate-100 px-4 py-2 text-sm font-medium text-slate-900 hover:bg-white disabled:opacity-50"
          disabled={view.kind === "loading"}
        >
          {view.kind === "loading" ? "Looking up…" : "Look up trace"}
        </button>
      </form>

      {view.kind === "error" ? (
        <div className="rounded-md border border-rose-800 bg-rose-950/40 px-4 py-3 text-sm text-rose-200">
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
        <Meta label="Status" value={<span className={`rounded px-2 py-0.5 text-xs ${toneFor(trace.status)}`}>{trace.status || "—"}</span>} />
        <Meta label="Route tier" value={trace.route_tier || "—"} />
        <Meta label="Tool" value={trace.tool_called || "—"} />
        <Meta label="Latency" value={`${trace.latency_ms} ms`} />
        <Meta label="Rows" value={String(trace.row_count)} />
        <Meta label="Actor role" value={trace.actor_role || "—"} />
        <Meta label="Model" value={trace.model_version || "—"} />
        <Meta label="Prompt" value={trace.prompt_version || "—"} />
      </div>

      <Panel title="Question (redacted)">
        <p className="whitespace-pre-wrap break-words text-sm text-slate-300">{trace.question_redacted || "—"}</p>
      </Panel>

      {trace.rejection_reason ? (
        <Panel title="Rejection reason">
          <p className="text-sm text-amber-300">{trace.rejection_reason}</p>
        </Panel>
      ) : null}

      {trace.review_verdict ? (
        <Panel title="Review verdict">
          <p className="text-sm text-slate-300">{trace.review_verdict}</p>
        </Panel>
      ) : null}

      {views.length > 0 ? (
        <Panel title="Source views">
          <div className="flex flex-wrap gap-2">
            {views.map((v) => (
              <span key={v} className="rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-300">
                {v}
              </span>
            ))}
          </div>
        </Panel>
      ) : null}

      <Panel title={`Steps (${steps.length})`}>
        {steps.length === 0 ? (
          <p className="text-sm text-slate-500">No steps recorded for this request.</p>
        ) : (
          <ol className="flex flex-col gap-3">
            {steps.map((step, i) => (
              <li key={i} className="rounded-md border border-slate-800 bg-slate-900/60 p-3">
                <div className="mb-1 flex flex-wrap items-center gap-2 text-xs text-slate-400">
                  <span className="rounded bg-slate-800 px-2 py-0.5 text-slate-300">#{i + 1}</span>
                  <span className="rounded bg-slate-800 px-2 py-0.5">{step.route || "—"}</span>
                  <span className="font-medium text-slate-200">{step.tool_name || "—"}</span>
                  <span className="ml-auto">{step.duration_ms} ms · {step.row_count} rows</span>
                </div>
                {step.sub_question ? (
                  <p className="text-sm text-slate-300">{step.sub_question}</p>
                ) : null}
                {step.params ? (
                  <pre className="mt-2 overflow-x-auto rounded bg-slate-950 p-2 text-xs text-slate-400">{step.params}</pre>
                ) : null}
                {step.verdict ? <p className="mt-1 text-xs text-slate-400">verdict: {step.verdict}</p> : null}
                {step.err ? <p className="mt-1 text-xs text-rose-300">error: {step.err}</p> : null}
              </li>
            ))}
          </ol>
        )}
      </Panel>

      <p className="text-xs text-slate-500">Recorded {trace.created_at}</p>
    </div>
  );
}

function Meta({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return (
    <div className="rounded-md border border-slate-800 bg-slate-900/60 px-3 py-2">
      <div className="text-[11px] uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-0.5 text-sm text-slate-200">{value}</div>
    </div>
  );
}

function Panel({ title, children }: { title: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="rounded-md border border-slate-800 bg-slate-900/40 p-4">
      <h2 className="mb-2 text-sm font-semibold text-slate-200">{title}</h2>
      {children}
    </div>
  );
}
