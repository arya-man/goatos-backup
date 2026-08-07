import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { ArrowLeft, ArrowRight, Ban, Check, ChevronRight, Workflow } from "lucide-react";
import { getVaccinationActionCenter, getVaccinationActionCenterCounts } from "@/lib/api/server";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { TONE_SWATCH, type Tone } from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { operationalLocationLabel } from "@/lib/operational-location.ts";
import { actionDriveLabel, actionWorkTitle } from "./work-board";
import { VaccinationFilterButton, VisibleTableSearch, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";

// Top-level Workflows screen — vaccination-only, ported from the mock orchestration layout: KPI tiles,
// module pill row, a left workflow catalog, and a right chain-reaction map. Each catalog row drills into
// /workflows/{row_id}.

type ChainStep = { key: string; stage: string; title: string; detail: string };

function chainSteps(pageContract: AdminUiPageContract): ChainStep[] {
  return optionGroup(pageContract, "chain_steps").map((step) => ({
    key: step.key,
    stage: copy(pageContract, `stage.${step.key}.short`),
    title: step.label,
    detail: step.title,
  }));
}

type NodeState = "done" | "cur" | "next" | "pending" | "blocked";

// Per-step chain state computed (read-only) from the active obligation's lifecycle. Steps are marked done
// up to the leading run of satisfied stages; the first unsatisfied stage is the current step (or blocked
// when the obligation is gated), the next is "next", the rest pending — exactly the mock's onode states.
function chainStates(w: ActionCenterObligation | undefined, stepCount: number): NodeState[] {
  const pending = Array.from({ length: stepCount }, () => "pending" as NodeState);
  if (!w) return pending;
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
  const blocked = Boolean(w.blocker_reason) || w.owner_state === "missing" || ["blocked", "rejected"].includes(w.work_state);
  return pending.map((_, i) => {
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

function hrefPreservingWorkflowPage(
  pathname: string,
  params: RouteSearchParams,
  workflowId?: string,
  overrides: Record<string, string | string[] | null | undefined> = {},
): string {
  return hrefWithParams(pathname, params, workflowId ? { ...overrides, workflow: workflowId } : overrides, ["workflow"]);
}

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
const BLOCKED_STATES: WorkState[] = ["blocked", "proof_pending", "rejected"];
const PATH = "/workflows";

export async function VaccinationWorkflowsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const selectedWorkflowId = one(sp, "workflow");
  const workflowCursor = one(sp, "wf_cursor");
  const hasWorkflowCursorStack = Boolean(sp.wf_cursor_stack);
  const workflowCursorStackValues = Array.isArray(sp.wf_cursor_stack) ? sp.wf_cursor_stack.filter(Boolean) : sp.wf_cursor_stack ? [sp.wf_cursor_stack] : undefined;

  const pageSizeOptions = tablePageSizes(pageContract, "workflow-catalog");
  const requestedPageSize = pageSizeOptions.find((size) => size === boundedInt(one(sp, "wf_limit"), 10, 1, 100)) ?? 10;
  const requestedPage = boundedInt(one(sp, "wf_page"), 1, 1, 1000000);
  const [countsResult, result] = await Promise.all([
    getVaccinationActionCenterCounts({ parkId, asOf }),
    getVaccinationActionCenter({ parkId, asOf, cursor: workflowCursor, limit: requestedPageSize }),
  ]);
  const totalFromBackend = countsResult.ok ? countsResult.data.total_count : 0;
  const rows: ActionCenterObligation[] = result.ok ? result.data.items : [];
  const nextCursor = result.ok ? result.data.next_cursor : undefined;
  if (requestedPage > 1 && !workflowCursor && !hasWorkflowCursorStack) {
    redirect(hrefWithPagedCursor(PATH, sp, "wf_cursor", null, "wf_page", "wf_cursor_stack") || scopeHref(PATH, scope, {}, { wf_page: "1", wf_cursor: undefined, wf_cursor_stack: undefined }));
  }
  const start = totalFromBackend === 0 ? 0 : (requestedPage - 1) * requestedPageSize + 1;
  const end = totalFromBackend === 0 ? 0 : Math.min(totalFromBackend, start + rows.length - 1);
  const paged = {
    page: requestedPage,
    pageSize: requestedPageSize,
    total: totalFromBackend,
    start,
    end,
  };
  const selectedWorkflow = selectedWorkflowId ? rows.find((row) => row.row_id === selectedWorkflowId) : undefined;
  const selectedWorkflowMissing = Boolean(selectedWorkflowId && !selectedWorkflow);
  const activeWorkflow = selectedWorkflowId ? selectedWorkflow : rows[0];
  const steps = chainSteps(pageContract);
  const catalogLabels = tableLabels(pageContract, "workflow-catalog");
  const chainNodeStates = chainStates(activeWorkflow, steps.length);
  const listHref = hrefPreservingWorkflowPage(PATH, sp);
  const actionCenterHref = activeWorkflow
    ? scopeHref("/action-center", scope, {}, { ac_row: activeWorkflow.row_id })
    : scopeHref("/action-center", scope);

  // Server-authoritative counts for the KPI tiles (not the capped page).
  const counts = new Map<WorkState, number>();
  if (countsResult.ok) for (const c of countsResult.data.counts_by_work_state) counts.set(c.work_state, c.count);
  const sum = (states: WorkState[]) => states.reduce((n, s) => n + (counts.get(s) ?? 0), 0);
  const totalWorkflows = totalFromBackend;
  const activeRuns = sum(ACTIVE_STATES);
  const blocked = sum(BLOCKED_STATES);
  const withExpected = rows.filter((r) => r.expected_count > 0);
  const avgProgress = withExpected.length
    ? Math.round(withExpected.reduce((a, r) => a + (r.completed_count / r.expected_count) * 100, 0) / withExpected.length)
    : 0;
  function pagerHref(page: number): string {
    if (page > paged.page && nextCursor) {
      return hrefWithPagedCursor(PATH, sp, "wf_cursor", nextCursor, "wf_page", "wf_cursor_stack") ?? scopeHref(PATH, scope);
    }
    if (page < paged.page) {
      return hrefPreviousPagedCursor(PATH, sp, "wf_cursor", "wf_page", "wf_cursor_stack") ?? scopeHref(PATH, scope);
    }
    return hrefPreservingWorkflowPage(PATH, sp, selectedWorkflowId, {
      wf_page: String(page),
      wf_limit: String(paged.pageSize),
      wf_cursor: workflowCursor,
      wf_cursor_stack: workflowCursorStackValues,
    });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return scopeHref(PATH, scope, {}, { wf_page: "1", wf_limit: String(pageSize), wf_cursor: undefined, wf_cursor_stack: undefined });
  }

  return (
	    <div className="screen on">
	      <div className="phead">
	        <div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
	        </div>
	      </div>

      {!countsResult.ok || !result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          {(() => {
            const error = !countsResult.ok ? countsResult.error : !result.ok ? result.error : undefined;
            return error ? <><b>{error.code ?? error.kind}</b>&nbsp;{error.message}</> : null;
          })()}
        </div>
      ) : null}

      {/* KPI tiles (mock #wfTiles). */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
	        <Kpi label={copy(pageContract, "label.workflows")} value={totalWorkflows} sub={copy(pageContract, "label.vaccination_chain")} tone="ok" icon={<Workflow className="ic" />} />
	        <Kpi label={copy(pageContract, "label.active_runs")} value={activeRuns} sub={copy(pageContract, "label.scheduled_in_progress")} tone={activeRuns ? "info" : "mut"} />
	        <Kpi label={copy(pageContract, "label.blocked_gated")} value={blocked} sub={copy(pageContract, "label.awaiting_proof_owner")} tone={blocked ? "dng" : "mut"} />
	        <Kpi label={copy(pageContract, "label.avg_progress")} value={<>{avgProgress}<small>%</small></>} sub={copy(pageContract, "label.across_shown")} tone="warn" />
	      </div>

      {/* Toolbar — module pill. Only the active vaccination module is visible in this slice. */}
      <div className="wftoolbar">
	        <div className="subtabs" id="wfPills" style={{ margin: 0 }} aria-label={copy(pageContract, "filter.domains.aria")}>
	          <Link href={scopeHref(PATH, scope)} className="on">
	            {copy(pageContract, "label.all_domains")} <span className="cbq">{totalWorkflows}</span>
	          </Link>
	        </div>
      </div>

      {/* Catalog (left) + chain-reaction map (right) — mock .wfwrap. */}
      <div className="wfwrap">
        <div className="wfcat" id="wfcat" data-filter-scope>
	          <div className="wfgrp">
	            {copy(pageContract, "section.catalog.title")}<span className="muted">{totalWorkflows}</span>
	          </div>
	          <div className="tbar" style={{ margin: "8px 6px 10px" }}>
	            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_label")} />
	            <VaccinationFilterButton
	              pageContract={pageContract}
	              title={copy(pageContract, "filter.drawer.title")}
	              searchReason={copy(pageContract, "filter.search_reason")}
	              filterReason={copy(pageContract, "filter.reason")}
	              rowsLabel={`${paged.start}-${paged.end} of ${totalWorkflows} rows · ${copy(pageContract, "filter.rows_suffix")}`}
	              actionHref={scopeHref("/action-center", scope)}
	              actionLabel={copy(pageContract, "action.open_action_center")}
	              facets={catalogLabels}
	            />
	          </div>
          {rows.length === 0 ? (
	            <div className="note" style={{ margin: 6 }}>
	              {result.ok
	                ? copy(pageContract, "empty.catalog")
	                : copy(pageContract, "empty.unavailable")}
	            </div>
	          ) : (
	            rows.map((row) => {
	              const title = actionWorkTitle(pageContract, row);
	              const pct = row.expected_count > 0 ? Math.round((row.completed_count / row.expected_count) * 100) : 0;
	              const isActive = activeWorkflow?.row_id === row.row_id;
              return (
                <Link
                  key={row.row_id}
                  href={hrefPreservingWorkflowPage(PATH, sp, row.row_id)}
                  scroll={false}
                  className={`wfrow${isActive ? " on" : ""}`}
                  aria-current={isActive ? "true" : undefined}
	                  aria-label={`${copy(pageContract, "action.open_record")} ${title}`}
	                  title={`${title} · ${row.park_name} · ${row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label })} · ${optionLabel(pageContract, "work_state_filter_chips", row.work_state)}`}
	                >
	                  <span className="wfdot" style={{ background: TONE_SWATCH[optionTone(pageContract, "severity_chips", row.severity) as Tone] }} />
	                  <div className="wftx">
	                    <b title={title}>{title}</b>
	                    <div className="wfsub" title={`${row.park_name} · ${row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label })} · ${optionLabel(pageContract, "work_state_filter_chips", row.work_state)}`}>
	                      {row.park_name} · {row.shed_name} · {optionLabel(pageContract, "work_state_filter_chips", row.work_state)}
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
          {rows.length > 0 ? (
            <VaccinationTablePager
              pageContract={pageContract}
              pageSizeOptions={pageSizeOptions}
              page={paged.page}
              pageSize={paged.pageSize}
              total={paged.total}
              start={paged.start}
              end={paged.end}
	              noun={copy(pageContract, "label.workflows")}
              hrefForPage={pagerHref}
              hrefForPageSize={pageSizeHref}
            />
          ) : null}
        </div>

        <div className="wfmain">
          <section className="card">
            <div className="hd">
              <Workflow className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	              <h3>{activeWorkflow ? actionDriveLabel(pageContract, activeWorkflow) : copy(pageContract, "section.chain.title")}</h3>
              <div className="sp" style={{ flex: 1 }} />
              {selectedWorkflowId ? (
	                <Link href={listHref} replace scroll={false} className="btn sm">
	                  <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.back_to_list")}
	                </Link>
	              ) : (
	                <span className="muted small">{pageContract.subtitle}</span>
	              )}
            </div>
            <div className="bd">
              {selectedWorkflowMissing ? (
                <div className="note" style={{ marginBottom: 14 }}>
                  {copy(pageContract, "empty.unavailable")}
                </div>
              ) : activeWorkflow ? (
                <div className="fchipsbar" style={{ marginBottom: 14 }}>
		                  <Tag tone={optionTone(pageContract, "work_state_filter_chips", activeWorkflow.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", activeWorkflow.work_state)}</Tag>
		                  <Tag tone={optionTone(pageContract, "severity_chips", activeWorkflow.severity) as Tone}>{optionLabel(pageContract, "severity_chips", activeWorkflow.severity)}</Tag>
	                  <Tag tone={optionTone(pageContract, "sop_state_chips", activeWorkflow.sop_task_state) as Tone}>{optionLabel(pageContract, "sop_state_chips", activeWorkflow.sop_task_state)}</Tag>
	                  <Tag tone={optionTone(pageContract, "proof_state_chips", activeWorkflow.proof_state) as Tone}>{optionLabel(pageContract, "proof_state_chips", activeWorkflow.proof_state)}</Tag>
	                  <Tag tone={optionTone(pageContract, "verification_state_chips", activeWorkflow.verification_state) as Tone}>
	                    {optionLabel(pageContract, "verification_state_chips", activeWorkflow.verification_state)}
	                  </Tag>
                  <div className="sp" style={{ flex: 1 }} />
                  <span className="muted small">
	                    {activeWorkflow.completed_count}/{activeWorkflow.expected_count} {copy(pageContract, "label.done")}
                  </span>
                </div>
              ) : null}
              {/* Stage pills — the lifecycle stages, with done/current highlighted (mock .ostages). */}
	              <div className="ostages" aria-label={copy(pageContract, "section.chain.aria")}>
	                {steps.map((c, i) => {
                  const st = chainNodeStates[i];
                  return (
                    <span
                      key={c.key}
                      className={`ostage${st === "done" ? " on" : ""}${st === "cur" || st === "blocked" ? " cur" : ""}`}
                      title={`${c.title}: ${c.detail}`}
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
	                  {copy(pageContract, "label.done")}
	                </span>
	                <span>
	                  <i className="sw" style={{ background: "var(--brand)", boxShadow: "0 0 0 3px var(--brand-soft)" }} aria-hidden="true" />
	                  {copy(pageContract, "label.current")}
	                </span>
	                <span>
	                  <i className="sw" style={{ background: "var(--info)" }} aria-hidden="true" />
	                  {copy(pageContract, "label.next")}
	                </span>
	                <span>
	                  <i className="sw" style={{ background: "var(--line)" }} aria-hidden="true" />
	                  {copy(pageContract, "label.pending")}
	                </span>
	                <span>
	                  <i className="sw" style={{ background: "var(--danger)" }} aria-hidden="true" />
	                  {copy(pageContract, "label.blocked")}
	                </span>
	              </div>

              {/* Chain-reaction map — mock .onode tree with computed done/current/next/pending/blocked + YOU ARE HERE. */}
	              <div className="otree" role="group" aria-label={copy(pageContract, "section.chain.aria")}>
	                {steps.map((c, i) => {
                  const st = chainNodeStates[i];
                  return (
                    <div
                      className={`onode ${st}`}
                      key={c.key}
                      title={`${c.title}: ${c.detail}`}
                    >
                      <div className="dotn">
                        {st === "done" ? <Check className="ic" style={{ width: 13, strokeWidth: 2.6 }} aria-hidden="true" /> : i + 1}
                      </div>
                      <div className="obody">
                        <b>{c.title}</b>
                        <div className="ometa">{c.detail}</div>
                      </div>
	                      {st === "cur" ? (
	                        <span className="here">{copy(pageContract, "label.you_are_here")}</span>
	                      ) : st === "next" ? (
	                        <span className="nxt">{copy(pageContract, "label.next_upper")}</span>
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
	                      {copy(pageContract, "label.chain_note_selected")} <b>{activeWorkflow.shed_name}</b>. {copy(pageContract, "label.chain_note_selected_tail")}
	                    </span>
                  </>
                ) : rows.length ? (
                  <>
                    <ChevronRight className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
	                    <span>{copy(pageContract, "label.chain_note_open")}</span>
                  </>
                ) : (
                  <>
                    <Ban className="ic" style={{ width: 14, color: "var(--muted)" }} aria-hidden="true" />
	                    <span>{copy(pageContract, "label.chain_note_empty")}</span>
                  </>
                )}
              </div>
              {activeWorkflow ? (
                <div className="row" style={{ marginTop: 14, gap: 10 }}>
                  <Link href={actionCenterHref} scroll={false} className="btn p">
                    {copy(pageContract, "action.open_action_center")}
                    <ArrowRight className="ic" style={{ width: 14 }} aria-hidden="true" />
                  </Link>
	                  <Link href={scopeHref(`/workflows/${encodeURIComponent(activeWorkflow.row_id)}`, scope)} className="btn">
	                    {copy(pageContract, "action.open_record")}
	                  </Link>
                </div>
              ) : null}
            </div>
          </section>

          <div className="note" style={{ marginTop: 14 }}>
	            {copy(pageContract, "note.engine")}{" "}
	            <Link href={scopeHref("/action-center", scope)} className="lk">
	              {copy(pageContract, "action.open_action_center")}
	            </Link>
	            .
          </div>
        </div>
      </div>
    </div>
  );
}
