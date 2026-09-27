"use client";

import { useCallback, useEffect, useMemo, useState } from "react";

import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import Divider from "@mui/material/Divider";
import TextField from "@mui/material/TextField";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";

import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { OrderDetailsHistory, type OrderHistoryItem } from "@/components/app/sections/order/order-details-history";
import { OrderDetailsDelivery } from "@/components/app/sections/order/order-details-delivery";
import { detailWrapSx } from "@/components/app/detail-wrap";
import { EmptyState } from "@/components/app/empty-state";
import { dateTime } from "@/lib/format";

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
//
// Layout: the template order-details page (sections/order/view/order-details-view.tsx) — a
// lookup toolbar card (OrderTableToolbar), then Grid md 8/4: the step trace as
// OrderDetailsHistory (timeline + dashed summary) and the question/verdict cards on the left,
// the request facts as OrderDetailsDelivery rows in the right-rail Card.

type ViewState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "loaded"; trace: TraceRecord }
  | { kind: "error"; status: number; message: string };

const STATUS_COLOR: Record<string, LabelColor> = {
  ok: "success",
  rejected: "warning",
  over_budget: "warning",
  degraded: "warning",
  error: "error",
};

function colorFor(status: string): LabelColor {
  return STATUS_COLOR[status] ?? "default";
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
    <Stack spacing={3}>
      <Card>
        <CardHeader
          title="Assistant request history (admin)"
          subheader="Step-by-step history for one leadership-assistant request: sub-questions, resolved tool and tier, redacted inputs, row counts, latency, and review verdict. Admin only — this history never appears in the leadership chat answer."
        />
        <Box
          component="form"
          role="search"
          onSubmit={(e) => {
            e.preventDefault();
            void lookup(requestId);
          }}
        >
          <OrderTableToolbar
            search={<Box sx={orderToolbarSearchSx}><TextField
                fullWidth
                label="Reference"
                value={requestId}
                onChange={(e) => setRequestId(e.target.value)}
                placeholder="e.g. 9f2c1b7a-…"
                autoComplete="off"
                slotProps={{
                  inputLabel: { shrink: true },
                  htmlInput: { spellCheck: false },
                  input: {
                    startAdornment: (
                      <InputAdornment position="start">
                        <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                      </InputAdornment>
                    ),
                  },
                }}
              /></Box>}
            trailing={
              <Button variant="contained" type="submit" color="primary" size="large" loading={view.kind === "loading"} startIcon={<Iconify icon="eva:search-fill" />} sx={{ flexShrink: 0 }}>
                {view.kind === "loading" ? "Looking up…" : "Look up history"}
              </Button>
            }
          />
        </Box>
      </Card>

      {view.kind === "error" ? (
        <Alert severity="error" role="alert">
          {view.message}
        </Alert>
      ) : null}

      {view.kind === "loaded" ? <TraceDetail trace={view.trace} /> : null}
    </Stack>
  );
}

function TraceDetail({ trace }: { trace: TraceRecord }): React.ReactElement {
  const steps = useMemo(() => trace.steps ?? [], [trace.steps]);
  const views = useMemo(() => trace.source_views ?? [], [trace.source_views]);

  const timeline: OrderHistoryItem[] = steps.map((step, i) => ({
    key: String(i),
    tone: step.err ? "error" : i === 0 ? "primary" : "grey",
    title: (
      <Box component="span" sx={{ display: "inline-flex", alignItems: "center", flexWrap: "wrap", gap: 1 }}>
        {`${i + 1}. ${step.tool_name || "—"}`}
        <Label variant="soft" sx={{ textTransform: "none" }}>{step.route || "—"}</Label>
      </Box>
    ),
    body: (
      <>
        <Box component="span" sx={{ display: "block", color: "text.primary", overflowWrap: "anywhere" }}>
          {step.sub_question || "—"}
        </Box>
        {step.params ? (
          <Box
            component="pre"
            sx={{ m: 0, mt: 1, p: 1.5, borderRadius: "var(--r-sm)", bgcolor: "background.neutral", typography: "caption", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}
          >
            {step.params}
          </Box>
        ) : null}
        {step.verdict ? (
          <Box component="span" sx={{ display: "block", mt: 0.5 }}>
            verdict: {step.verdict}
          </Box>
        ) : null}
        {step.err ? (
          <Box component="span" sx={{ display: "block", mt: 0.5, color: "error.main" }}>
            error: {step.err}
          </Box>
        ) : null}
      </>
    ),
    time: `${step.duration_ms} ms · ${step.row_count} rows`,
  }));

  return (
    <Grid container spacing={3}>
      <Grid size={{ xs: 12, md: 8 }}>
        <Box sx={{ gap: 3, display: "flex", flexDirection: "column" }}>
          <Card>
            <CardHeader title="Question (redacted)" />
            <Typography variant="body2" sx={{ p: 3, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
              {trace.question_redacted || "—"}
            </Typography>
          </Card>

          {steps.length === 0 ? (
            <Card>
              <CardHeader title="Steps" />
              <EmptyState title="No steps recorded for this request." />
            </Card>
          ) : (
            <OrderDetailsHistory
              title={
                <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
                  Steps <Label variant="soft">{steps.length}</Label>
                </Box>
              }
              timeline={timeline}
              summary={[
                { key: "latency", label: "Latency", value: `${trace.latency_ms} ms` },
                { key: "rows", label: "Rows", value: String(trace.row_count) },
                { key: "tier", label: "Route tier", value: trace.route_tier || "—" },
                { key: "tool", label: "Tool", value: trace.tool_called || "—" },
              ]}
            />
          )}

          {trace.rejection_reason ? (
            <Card>
              <CardHeader title="Rejection reason" />
              <Typography variant="body2" sx={{ p: 3, color: "warning.main", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                {trace.rejection_reason}
              </Typography>
            </Card>
          ) : null}

          {trace.review_verdict ? (
            <Card>
              <CardHeader title="Review verdict" />
              <Typography variant="body2" sx={{ p: 3, whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
                {trace.review_verdict}
              </Typography>
            </Card>
          ) : null}
        </Box>
      </Grid>

      <Grid size={{ xs: 12, md: 4 }}>
        <Card>
          <OrderDetailsDelivery slotProps={{ row: detailWrapSx }}
            title="Request summary"
            rows={[
              { key: "status", label: "Status", value: <Label variant="soft" color={colorFor(trace.status)}>{trace.status || "—"}</Label> },
              { key: "recorded", label: "Recorded", value: dateTime(trace.created_at) },
              { key: "tier", label: "Route tier", value: trace.route_tier || "—" },
              { key: "tool", label: "Tool", value: trace.tool_called || "—" },
              { key: "latency", label: "Latency", value: `${trace.latency_ms} ms` },
              { key: "rows", label: "Rows", value: String(trace.row_count) },
            ]}
          />

          <Divider sx={{ borderStyle: "dashed" }} />
          <OrderDetailsDelivery slotProps={{ row: detailWrapSx }}
            title="Model"
            rows={[
              { key: "actor", label: "Actor role", value: trace.actor_role || "—" },
              { key: "model", label: "Model", value: trace.model_version || "—" },
              { key: "prompt", label: "Prompt", value: trace.prompt_version || "—" },
              { key: "reference", label: "Reference", value: trace.request_id || "—" },
            ]}
          />

          <Divider sx={{ borderStyle: "dashed" }} />
          <CardHeader
            title={
              <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
                Sources <Label variant="soft">{views.length}</Label>
              </Box>
            }
          />
          <Box sx={{ p: 3, display: "flex", flexWrap: "wrap", gap: 1 }}>
            {views.length === 0 ? (
              <Typography variant="body2" sx={{ color: "text.secondary" }}>
                No source views recorded for this request.
              </Typography>
            ) : (
              views.map((v) => (
                <Label key={v} variant="soft" color="info" sx={{ textTransform: "none" }}>
                  {v}
                </Label>
              ))
            )}
          </Box>
        </Card>
      </Grid>
    </Grid>
  );
}
