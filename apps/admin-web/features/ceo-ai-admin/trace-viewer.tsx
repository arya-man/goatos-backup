"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertTriangle, Database, ListTree, Search, SearchX } from "lucide-react";

import { CeoAiAdminEvents, trackCeoAiAdminError, trackCeoAiAdminEvent } from "./telemetry";
import { TraceViewerStyles } from "./trace-viewer-styles";
import type { TraceError, TraceRecord } from "./types";
import { dateTime } from "@/lib/format";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import CardContent from "@mui/material/CardContent";
import { toneVars, type KitTone } from "@/lib/tone";
import { IconBadge } from "@/components/app/icon-badge";
import { AnimatedTabs, TabPanel } from "@/components/minimal/list/animated-tabs";

// CeoAiAdminTraceViewer is the ADMIN-ONLY step-trace debug surface. An
// engineer/admin (ceo_internal/superadmin — enforced server-side by the
// backend) enters a request_id and sees the internal execution trace:
// sub-questions, resolved tool/tier, redacted params, row counts, latency, and
// the review verdict. This is the sanctioned way to "see each step" WITHOUT
// leaking the trace into the leadership chat answer (Internal Tracking rule).
//
// The component holds no business data and no gating logic: the proxy + backend
// own auth. A non-admin session receives 403, surfaced honestly below.
//
// Presentation only uses shared mesha theme tokens through the kit + the scoped
// `mzat-` styles, so the surface is correct in both the dark and light themes.

type ViewState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "loaded"; trace: TraceRecord }
  | { kind: "error"; status: number; message: string };

const STATUS_TONE: Record<string, KitTone> = {
  ok: "success",
  rejected: "warning",
  over_budget: "warning",
  degraded: "warning",
  error: "error",
};

function toneFor(status: string): KitTone {
  return STATUS_TONE[status] ?? "neutral";
}

function chipStyle(tone: KitTone) {
  const t = toneVars(tone);
  return { background: t.soft, color: t.ink };
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
      message = "You do not have permission to view assistant request history (admin only).";
    } else if (body?.error === "trace_not_found" || res.status === 404) {
      message = "No request history found for that reference in your tenant.";
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
      setView({ kind: "error", status: 400, message: "Enter a reference to look up its request history." });
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
      const message = err instanceof Error ? err.message : "Request history is not reachable right now. Try again in a moment.";
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
    <div className="kit-enter mzat-stack">
      <TraceViewerStyles />

      <div>
        <Card>
          <CardHeader
            title={
              <span style={{ display: "inline-flex", alignItems: "center", gap: 10 }}>
                <IconBadge icon={<ListTree />} tone="primary" size="sm" />
                Assistant request history (admin)
              </span>
            }
            subheader="Step-by-step history for one leadership-assistant request: sub-questions, resolved tool and tier, redacted inputs, row counts, latency, and review verdict. Admin only — this history never appears in the leadership chat answer."
          />
          <CardContent>
          <form
            className="mzat-form"
            onSubmit={(e) => {
              e.preventDefault();
              void lookup(requestId);
            }}
          >
            <label className="mzat-field">
              <span>Reference</span>
              <input
                className="mzat-input"
                value={requestId}
                onChange={(e) => setRequestId(e.target.value)}
                placeholder="e.g. 9f2c1b7a-…"
                spellCheck={false}
                autoComplete="off"
              />
            </label>
            <Button variant="contained" type="submit" color="primary" loading={view.kind === "loading"} startIcon={<Search />}>
              {view.kind === "loading" ? "Looking up…" : "Look up history"}
            </Button>
          </form>
          </CardContent>
        </Card>
      </div>

      {view.kind === "error" ? (
        <div>
          <div className="mzat-alert" role="alert">
            <AlertTriangle aria-hidden="true" />
            <span>{view.message}</span>
          </div>
        </div>
      ) : null}

      {view.kind === "loaded" ? <TraceDetail trace={view.trace} /> : null}
    </div>
  );
}

function TraceDetail({ trace }: { trace: TraceRecord }): React.ReactElement {
  const steps = useMemo(() => trace.steps ?? [], [trace.steps]);
  const views = useMemo(() => trace.source_views ?? [], [trace.source_views]);
  const [tab, setTab] = useState("steps");
  const tabs = useMemo(
    () => [
      { value: "steps", label: "Steps", icon: <ListTree />, count: steps.length },
      { value: "sources", label: "Sources", icon: <Database />, count: views.length },
    ],
    [steps.length, views.length],
  );

  return (
    <>
      <div>
        <Card>
          <CardHeader title="Request summary" subheader={`Recorded ${dateTime(trace.created_at)}`} />
          <CardContent>
          <div className="mzat-metas">
            <Meta
              label="Status"
              value={
                <span className="mzat-chip" style={chipStyle(toneFor(trace.status))}>
                  {trace.status || "—"}
                </span>
              }
            />
            <Meta label="Route tier" value={trace.route_tier || "—"} />
            <Meta label="Tool" value={trace.tool_called || "—"} />
            <Meta label="Latency" value={`${trace.latency_ms} ms`} />
            <Meta label="Rows" value={String(trace.row_count)} />
            <Meta label="Actor role" value={trace.actor_role || "—"} />
            <Meta label="Model" value={trace.model_version || "—"} />
            <Meta label="Prompt" value={trace.prompt_version || "—"} />
          </div>
          </CardContent>
        </Card>
      </div>

      <div>
        <Card>
          <CardHeader title="Question (redacted)" />
          <CardContent>
            <p className="mzat-body">{trace.question_redacted || "—"}</p>
          </CardContent>
        </Card>
      </div>

      {trace.rejection_reason ? (
        <div>
          <Card>
            <CardHeader title="Rejection reason" />
            <CardContent>
              <p className="mzat-body" style={{ color: "var(--warning-ink)" }}>
                {trace.rejection_reason}
              </p>
            </CardContent>
          </Card>
        </div>
      ) : null}

      {trace.review_verdict ? (
        <div>
          <Card>
            <CardHeader title="Review verdict" />
            <CardContent>
              <p className="mzat-body">{trace.review_verdict}</p>
            </CardContent>
          </Card>
        </div>
      ) : null}

      <div>
        <Card>
          <div style={{ padding: "24px 24px 0" }}>
            <AnimatedTabs items={tabs} value={tab} onChange={setTab} variant="pill" ariaLabel="Request history detail" />
          </div>
          <TabPanel tabKey={tab}>
          {tab === "steps" ? (
            <div style={{ padding: "16px 8px 8px" }}>
              {steps.length === 0 ? (
                <p className="mzat-empty">
                  <SearchX aria-hidden="true" style={{ display: "block", margin: "0 auto 10px", width: 22, height: 22 }} />
                  No steps recorded for this request.
                </p>
              ) : (
                <div className="mzat-tablewrap">
                  <Table className="mzat-table">
                    <TableHead>
                      <TableRow>
                        <TableCell component="th" style={{ width: 56 }}>#</TableCell>
                        <TableCell component="th" style={{ width: 140 }}>Route</TableCell>
                        <TableCell component="th" style={{ width: 200 }}>Tool</TableCell>
                        <TableCell component="th">Sub-question</TableCell>
                        <TableCell component="th" className="mzat-num" style={{ width: 96 }}>
                          Latency
                        </TableCell>
                        <TableCell component="th" className="mzat-num" style={{ width: 80 }}>
                          Rows
                        </TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {steps.map((step, i) => (
                        <TableRow key={i}>
                          <TableCell>
                            <span className="mzat-idx">{i + 1}</span>
                          </TableCell>
                          <TableCell>
                            <span className="mzat-pill">{step.route || "—"}</span>
                          </TableCell>
                          <TableCell style={{ fontWeight: 600 }}>{step.tool_name || "—"}</TableCell>
                          <TableCell>
                            <div className="mzat-q">{step.sub_question || "—"}</div>
                            {step.params ? <pre className="mzat-params">{step.params}</pre> : null}
                            {step.verdict ? <p className="mzat-note">verdict: {step.verdict}</p> : null}
                            {step.err ? <p className="mzat-note err">error: {step.err}</p> : null}
                          </TableCell>
                          <TableCell className="mzat-num">{step.duration_ms} ms</TableCell>
                          <TableCell className="mzat-num">{step.row_count}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          ) : (
            <div style={{ padding: "20px 24px 24px" }}>
              {views.length === 0 ? (
                <p className="mzat-empty">No source views recorded for this request.</p>
              ) : (
                <div className="mzat-pills">
                  {views.map((v) => (
                    <span key={v} className="mzat-pill">
                      {v}
                    </span>
                  ))}
                </div>
              )}
            </div>
          )}
          </TabPanel>
        </Card>
      </div>

      <div>
        <p className="mzat-stamp">Recorded {dateTime(trace.created_at)}</p>
      </div>
    </>
  );
}

function Meta({ label, value }: { label: string; value: React.ReactNode }): React.ReactElement {
  return (
    <div className="mzat-meta">
      <div className="mzat-meta-k">{label}</div>
      <div className="mzat-meta-v">{value}</div>
    </div>
  );
}
