import Link from "next/link";
import { Ban, ChevronRight, Workflow } from "lucide-react";
import { getVaccinationActionCenter } from "@/lib/api/server";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import { type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope } from "@/lib/scope";
import { SEVERITY_META, TONE_SWATCH, WORK_STATE_META } from "./process-integrity";

// Top-level Workflows screen — vaccination-only, ported from the mock orchestration layout: KPI tiles,
// module pill row, a left workflow catalog, and a right chain-reaction map. Each catalog row drills into
// /workflows/{row_id}.

// The fixed vaccination workflow chain — config -> completion. Mirrors the drilldown node order.
const CHAIN_STEPS: Array<{ step: string; title: string; detail: string }> = [
  { step: "1 · authoring", title: "Config rule published", detail: "source-backed protocol version goes live" },
  { step: "2 · generate", title: "Obligation generated", detail: "per goat / dose against eligible cohorts" },
  { step: "3 · execute", title: "Drive / session opened", detail: "shed-drive batch for the cohort" },
  { step: "4 · SOP", title: "SOP task started", detail: "step template expands into field tasks" },
  { step: "5 · proof", title: "Proof uploaded", detail: "shed + vial video per proof policy" },
  { step: "6 · verify", title: "Verification", detail: "accept / reject / request rework" },
  { step: "7 · close", title: "Completion posted", detail: "dose consumed · booster scheduled if due" },
];

type Tone4 = "ok" | "warn" | "dng" | "info" | "mut";
const accentVar: Record<Tone4, string> = {
  ok: "var(--brand)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  mut: "var(--line)",
};

function Kpi({ label, value, sub, tone, icon }: { label: string; value: React.ReactNode; sub?: string; tone: Tone4; icon?: React.ReactNode }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentVar[tone] }} />
      <div className="lab">
        {icon}
        {label}
      </div>
      <div className="val">{value}</div>
      {sub ? <div className="dl muted">{sub}</div> : null}
    </div>
  );
}

const ACTIVE_STATES: WorkState[] = ["in_progress", "scheduled"];
const BLOCKED_STATES: WorkState[] = ["blocked", "proof_pending", "owner_missing", "rejected"];

export async function VaccinationWorkflowsPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const { parkId, asOf } = backendScope(parseScope(sp));

  const result = await getVaccinationActionCenter({ parkId, asOf, limit: 200 });
  const rows: ActionCenterObligation[] = result.ok ? result.data.items : [];
  const nextCursor = result.ok ? result.data.next_cursor : undefined;

  // Server-authoritative counts for the KPI tiles (not the capped page).
  const counts = new Map<WorkState, number>();
  if (result.ok) for (const c of result.data.counts_by_work_state) counts.set(c.work_state, c.count);
  const sum = (states: WorkState[]) => states.reduce((n, s) => n + (counts.get(s) ?? 0), 0);
  const totalWorkflows = Array.from(counts.values()).reduce((a, b) => a + b, 0);
  const activeRuns = sum(ACTIVE_STATES);
  const blocked = sum(BLOCKED_STATES);
  const withExpected = rows.filter((r) => r.expected_count > 0);
  const avgProgress = withExpected.length
    ? Math.round(withExpected.reduce((a, r) => a + (r.completed_count / r.expected_count) * 100, 0) / withExpected.length)
    : 0;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>Workflows</h1>
          <div className="sub">
            <b>Workflow library</b> — the vaccination chain: config → obligation → drive/session → SOP task → proof →
            verification → completion. Pick a live workflow to open its chain-reaction map.
          </div>
        </div>
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* KPI tiles (mock #wfTiles). */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi label="Workflows" value={totalWorkflows} sub="vaccination chain" tone="ok" icon={<Workflow className="ic" />} />
        <Kpi label="Active runs" value={activeRuns} sub="scheduled + in progress" tone={activeRuns ? "info" : "mut"} />
        <Kpi label="Blocked / gated" value={blocked} sub="awaiting proof / owner" tone={blocked ? "dng" : "mut"} />
        <Kpi label="Avg progress" value={<>{avgProgress}<small>%</small></>} sub="across shown" tone="warn" />
      </div>

      {/* Toolbar — module pill. Only the active vaccination module is visible in this slice. */}
      <div className="wftoolbar">
        <div className="subtabs" id="wfPills" style={{ margin: 0 }} aria-label="Workflow domains">
          <Link href="/workflows" className="on">
            Vaccination <span className="cbq">{totalWorkflows}</span>
          </Link>
        </div>
      </div>

      {/* Catalog (left) + chain-reaction map (right) — mock .wfwrap. */}
      <div className="wfwrap">
        <div className="wfcat" id="wfcat">
          <div className="wfgrp">
            Live vaccination workflows<span className="muted">{rows.length}</span>
          </div>
          {rows.length === 0 ? (
            <div className="note" style={{ margin: 6 }}>
              {result.ok
                ? "No live workflows yet. A workflow starts when a published protocol generates an obligation — publish a source-backed rule in Config and a vaccination SOP."
                : "Live workflows are unavailable until the service responds."}
            </div>
          ) : (
            rows.map((row) => {
              const title = row.drive_name ?? `${row.protocol_name} · ${row.dose_code}`;
              const pct = row.expected_count > 0 ? Math.round((row.completed_count / row.expected_count) * 100) : 0;
              return (
                <Link key={row.row_id} href={`/workflows/${encodeURIComponent(row.row_id)}`} className="wfrow">
                  <span className="wfdot" style={{ background: TONE_SWATCH[SEVERITY_META[row.severity].tone] }} />
                  <div className="wftx">
                    <b>{title}</b>
                    <div className="wfsub">
                      {row.park_name} · {row.shed_name} · {WORK_STATE_META[row.work_state].label}
                    </div>
                    <div className="wfbar">
                      <i style={{ width: `${pct}%` }} />
                    </div>
                  </div>
                  <span className="wfruns">{row.expected_count || "—"}</span>
                </Link>
              );
            })
          )}
        </div>

        <div className="wfmain">
          <section className="card">
            <div className="hd">
              <Workflow className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
              <h3>Vaccination workflow chain</h3>
              <div className="sp" style={{ flex: 1 }} />
              <span className="muted small">config → obligation → drive → SOP → proof → verify → completion</span>
            </div>
            <div className="bd">
              <div className="chain" tabIndex={0} role="group" aria-label="Vaccination workflow chain steps">
                {CHAIN_STEPS.map((c) => (
                  <div className="cstep" key={c.step} style={{ cursor: "default" }}>
                    <div className="s">{c.step}</div>
                    <b>{c.title}</b>
                    <div className="d">{c.detail}</div>
                  </div>
                ))}
              </div>
              <div className="note" style={{ marginTop: 14, display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                {rows.length ? (
                  <>
                    <ChevronRight className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
                    <span>Open a workflow on the left to see its live chain-reaction map for that drive.</span>
                  </>
                ) : (
                  <>
                    <Ban className="ic" style={{ width: 14, color: "var(--muted)" }} aria-hidden="true" />
                    <span>No live instances to map yet — this is the template every vaccination workflow follows.</span>
                  </>
                )}
              </div>
            </div>
          </section>

          <div className="note" style={{ marginTop: 14 }}>
            Every node maps to the engine: <span className="mono">event → obligation → SOP task → verification → closure</span>
            . The SOP is the step template; the engine gates each next step on verified proof. See live work in the{" "}
            <Link href="/action-center" className="lk">
              Action Center
            </Link>
            .
          </div>
        </div>
      </div>

      {nextCursor ? (
        <div className="muted small" style={{ marginTop: 12 }}>
          Showing the first 200 workflows. Filter by park, or narrow in the Action Center, to see the rest.
        </div>
      ) : null}
    </div>
  );
}
