import Link from "next/link";
import { ArrowLeft, ArrowRight, Ban, Check, ChevronRight, Workflow } from "lucide-react";
import { getVaccinationActionCenter } from "@/lib/api/server";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import {
  PROOF_META,
  SEVERITY_META,
  SOP_META,
  TONE_SWATCH,
  VERIFICATION_META,
  WORK_STATE_META,
} from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { actionDriveLabel, actionWorkTitle } from "./work-board";

// Top-level Workflows screen — vaccination-only, ported from the mock orchestration layout: KPI tiles,
// module pill row, a left workflow catalog, and a right chain-reaction map. Each catalog row drills into
// /workflows/{row_id}.

// The fixed vaccination workflow chain — config -> completion. Mirrors the drilldown node order.
const CHAIN_STEPS: Array<{ stage: string; title: string; detail: string }> = [
  { stage: "Config", title: "Config rule published", detail: "source-backed protocol version goes live" },
  { stage: "Obligation", title: "Obligation generated", detail: "per goat / dose against eligible cohorts" },
  { stage: "Drive", title: "Drive / session opened", detail: "shed-drive batch for the cohort" },
  { stage: "SOP", title: "SOP task started", detail: "step template expands into field tasks" },
  { stage: "Proof", title: "Proof uploaded", detail: "shed + vial video per proof policy" },
  { stage: "Verify", title: "Verification", detail: "accept / reject / request rework" },
  { stage: "Close", title: "Completion posted", detail: "dose consumed · booster scheduled if due" },
];

type NodeState = "done" | "cur" | "next" | "pending" | "blocked";

// Per-step chain state computed (read-only) from the active obligation's lifecycle. Steps are marked done
// up to the leading run of satisfied stages; the first unsatisfied stage is the current step (or blocked
// when the obligation is gated), the next is "next", the rest pending — exactly the mock's onode states.
function chainStates(w: ActionCenterObligation | undefined): NodeState[] {
  if (!w) return CHAIN_STEPS.map(() => "pending");
  const sop = w.sop_task_state;
  const proof = w.proof_state;
  const done = [
    true, // config rule published — a live workflow exists
    true, // obligation generated — it exists
    sop !== "not_started" || proof === "uploaded" || proof === "accepted" || w.completed_count > 0, // drive opened
    sop === "submitted" || sop === "accepted", // SOP task
    proof === "uploaded" || proof === "accepted", // proof uploaded
    w.verification_state === "accepted", // verification
    w.completion_state === "completed", // completion posted
  ];
  let doneThrough = 0;
  while (doneThrough < done.length && done[doneThrough]) doneThrough++;
  const blocked =
    Boolean(w.blocker_reason) || w.owner_state === "missing" || ["blocked", "rejected", "owner_missing"].includes(w.work_state);
  return CHAIN_STEPS.map((_, i) => {
    if (i < doneThrough) return "done";
    if (i === doneThrough) return blocked ? "blocked" : "cur";
    if (i === doneThrough + 1) return "next";
    return "pending";
  });
}

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
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const selectedWorkflowId = one(sp, "workflow");

  const result = await getVaccinationActionCenter({ parkId, asOf, limit: 200 });
  const rows: ActionCenterObligation[] = result.ok ? result.data.items : [];
  const nextCursor = result.ok ? result.data.next_cursor : undefined;
  const selectedWorkflow = selectedWorkflowId ? rows.find((row) => row.row_id === selectedWorkflowId) : undefined;
  const activeWorkflow = selectedWorkflow ?? rows[0];
  const chainNodeStates = chainStates(activeWorkflow);
  const listHref = scopeHref("/workflows", scope);
  const actionCenterHref = activeWorkflow
    ? scopeHref("/action-center", scope, {}, { ac_row: activeWorkflow.row_id })
    : scopeHref("/action-center", scope);

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
              const title = actionWorkTitle(row);
              const pct = row.expected_count > 0 ? Math.round((row.completed_count / row.expected_count) * 100) : 0;
              const isActive = activeWorkflow?.row_id === row.row_id;
              return (
                <Link
                  key={row.row_id}
                  href={scopeHref("/workflows", scope, {}, { workflow: row.row_id })}
                  scroll={false}
                  className={`wfrow${isActive ? " on" : ""}`}
                  aria-current={isActive ? "true" : undefined}
                  aria-label={`Select workflow ${title}`}
                >
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
              <h3>{activeWorkflow ? actionDriveLabel(activeWorkflow) : "Vaccination workflow chain"}</h3>
              <div className="sp" style={{ flex: 1 }} />
              {selectedWorkflowId ? (
                <Link href={listHref} replace scroll={false} className="btn sm">
                  <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Back to list
                </Link>
              ) : (
                <span className="muted small">config → obligation → drive → SOP → proof → verify → completion</span>
              )}
            </div>
            <div className="bd">
              {activeWorkflow ? (
                <div className="fchipsbar" style={{ marginBottom: 14 }}>
                  <Tag tone={WORK_STATE_META[activeWorkflow.work_state].tone}>{WORK_STATE_META[activeWorkflow.work_state].label}</Tag>
                  <Tag tone={SEVERITY_META[activeWorkflow.severity].tone}>{SEVERITY_META[activeWorkflow.severity].label}</Tag>
                  <Tag tone={SOP_META[activeWorkflow.sop_task_state].tone}>{SOP_META[activeWorkflow.sop_task_state].label}</Tag>
                  <Tag tone={PROOF_META[activeWorkflow.proof_state].tone}>{PROOF_META[activeWorkflow.proof_state].label}</Tag>
                  <Tag tone={VERIFICATION_META[activeWorkflow.verification_state].tone}>
                    {VERIFICATION_META[activeWorkflow.verification_state].label}
                  </Tag>
                  <div className="sp" style={{ flex: 1 }} />
                  <span className="muted small">
                    {activeWorkflow.completed_count}/{activeWorkflow.expected_count} done
                  </span>
                </div>
              ) : null}
              {/* Stage pills — the lifecycle stages, with done/current highlighted (mock .ostages). */}
              <div className="ostages" aria-label="Workflow stages">
                {CHAIN_STEPS.map((c, i) => {
                  const st = chainNodeStates[i];
                  return (
                    <span
                      key={c.stage}
                      className={`ostage${st === "done" ? " on" : ""}${st === "cur" || st === "blocked" ? " cur" : ""}`}
                    >
                      {c.stage}
                    </span>
                  );
                })}
              </div>

              {/* Legend (mock). */}
              <div className="legend" style={{ marginBottom: 14 }}>
                <span>
                  <i className="sw" style={{ background: "var(--brand)" }} aria-hidden="true" />
                  done
                </span>
                <span>
                  <i className="sw" style={{ background: "var(--brand)", boxShadow: "0 0 0 3px var(--brand-soft)" }} aria-hidden="true" />
                  current (you are here)
                </span>
                <span>
                  <i className="sw" style={{ background: "var(--info)" }} aria-hidden="true" />
                  next
                </span>
                <span>
                  <i className="sw" style={{ background: "var(--line)" }} aria-hidden="true" />
                  pending
                </span>
                <span>
                  <i className="sw" style={{ background: "var(--danger)" }} aria-hidden="true" />
                  blocked (gated)
                </span>
              </div>

              {/* Chain-reaction map — mock .onode tree with computed done/current/next/pending/blocked + YOU ARE HERE. */}
              <div className="otree" role="group" aria-label="Vaccination workflow chain">
                {CHAIN_STEPS.map((c, i) => {
                  const st = chainNodeStates[i];
                  return (
                    <div className={`onode ${st}`} key={c.stage}>
                      <div className="dotn">
                        {st === "done" ? <Check className="ic" style={{ width: 13, strokeWidth: 2.6 }} aria-hidden="true" /> : i + 1}
                      </div>
                      <div className="obody">
                        <b>{c.title}</b>
                        <div className="ometa">{c.detail}</div>
                      </div>
                      {st === "cur" ? (
                        <span className="here">YOU ARE HERE</span>
                      ) : st === "next" ? (
                        <span className="nxt">NEXT</span>
                      ) : null}
                    </div>
                  );
                })}
              </div>
              <div className="note" style={{ marginTop: 14, display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                {activeWorkflow ? (
                  <>
                    <ChevronRight className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
                    <span>
                      Selected workflow for <b>{activeWorkflow.shed_name}</b>. Use the drawer/action page for the next
                      backend-gated action; the full drilldown is available when you need the complete audit chain.
                    </span>
                  </>
                ) : rows.length ? (
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
              {activeWorkflow ? (
                <div className="row" style={{ marginTop: 14, gap: 10 }}>
                  <Link href={actionCenterHref} scroll={false} className="btn p">
                    {activeWorkflow.next_action}
                    <ArrowRight className="ic" style={{ width: 14 }} aria-hidden="true" />
                  </Link>
                  <Link href={scopeHref(`/workflows/${encodeURIComponent(activeWorkflow.row_id)}`, scope)} className="btn">
                    Open workflow detail
                  </Link>
                </div>
              ) : null}
            </div>
          </section>

          <div className="note" style={{ marginTop: 14 }}>
            Every node maps to the engine: <span className="mono">event → obligation → SOP task → verification → closure</span>
            . The SOP is the step template; the engine gates each next step on verified proof. See live work in the{" "}
            <Link href={scopeHref("/action-center", scope)} className="lk">
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
