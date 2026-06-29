import Link from "next/link";
import { Ban, GitBranch, Info, ShieldCheck, Syringe, Video, X } from "lucide-react";
import { getVaccinationActionCenter, getVaccinationVerificationQueue } from "@/lib/api/server";
import { actionFeedbackCopy, copy, optionGroup, tableLabels, tablePageSizes, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  ActionCenterObligation,
  ProcessIntegritySeverity,
  VaccinationQueueItem,
  WorkState,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import {
  SEVERITY_ORDER,
  WORK_STATE_ORDER,
  type Tone,
} from "./process-integrity";
import { SopChecklist } from "./sop-checklist";
import {
  VACCINATION_DRIVE_SOP_STEPS,
  VaccinationFilterButton,
  VisibleTableSearch,
  paginateRows,
  VaccinationTablePager,
  type VaccinationPageSize,
} from "@/features/phc-vaccination";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { ActionCenterFiltersButton } from "./action-center-filters";
import { WorkBoard, actionWorkTitle } from "./work-board";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

// Backend has no manual priority field — it is derived from computed severity. The drawer shows the
// mock's High/Med/Low chips read-only (disabled-with-reason), reflecting severity, never editable.
const PRIORITY_BY_SEVERITY: Record<ProcessIntegritySeverity, "high" | "med" | "low"> = {
  broken: "high",
  at_risk: "med",
  watch: "low",
  ok: "low",
};

function optionLabel(options: AdminUiOption[], key: string): string {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return option.label;
}

function optionTone(options: AdminUiOption[], key: string): Tone {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return (option.tone || "mut") as Tone;
}

// How far the vaccination drive SOP has progressed for this obligation, derived (read-only) from the
// computed lifecycle states — steps below the index are done, the step at it is current. Honest mapping:
// the obligation existing = "Drive scheduled" done; SOP/proof activity advances administration; an
// accepted+verified proof advances the ledger consume; a completed obligation finishes the flow.
function sopDoneThrough(row: ActionCenterObligation): number {
  let n = 1; // obligation generated → "Drive scheduled" done
  if (
    row.sop_task_state === "submitted" ||
    row.sop_task_state === "accepted" ||
    row.proof_state === "uploaded" ||
    row.proof_state === "accepted"
  ) {
    n = 2;
  }
  if (row.proof_state === "accepted" && row.verification_state === "accepted") n = 3;
  if (row.completion_state === "completed") n = 4;
  return n;
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "—";
}

// ActionForm posts a per-row server action (Verify / Reject / Rework) against a real completion id.
function ActionForm({
  action,
  completionId,
  taskId,
  rowVersion,
  returnTo,
  reason,
  disabledTitle,
  primary,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  completionId: string;
  taskId?: string;
  rowVersion?: number;
  returnTo: string;
  reason?: string;
  disabledTitle?: string;
  primary?: boolean;
  children: React.ReactNode;
}) {
  const canReview = Boolean(taskId) && Number(rowVersion ?? 0) > 0;
  return (
    <form action={action} style={{ display: "inline" }}>
      <input type="hidden" name="completion_id" value={completionId} />
      {taskId ? <input type="hidden" name="task_id" value={taskId} /> : null}
      {rowVersion ? <input type="hidden" name="row_version" value={rowVersion} /> : null}
      <input type="hidden" name="return_to" value={returnTo} />
      {reason ? <input type="hidden" name="reason" value={reason} /> : null}
      <button type="submit" className={`btn sm${primary ? " p" : ""}`} disabled={!canReview} aria-disabled={!canReview || undefined} title={!canReview ? disabledTitle : undefined}>
        {children}
      </button>
    </form>
  );
}

export async function VaccinationActionCenterPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const view = one(sp, "bucket") === "verify" ? "verify" : "board";
  const stateFilter = (WORK_STATE_ORDER.find((s) => s === one(sp, "state")) ?? "all") as WorkState | "all";
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const { parkId, asOf } = backendScope(parseScope(sp));
  const scope = parseScope(sp);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  // Board source = the real process-integrity Action Center contract (server-computed work state,
  // severity, owner, proof/verify state, next action). Verification queue = actionable completions.
  // Both honor the top-bar park scope (park_id) so the SOP/verification queue can't show other parks.
  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter({
      parkId,
      asOf,
      workState: stateFilter === "all" ? undefined : stateFilter,
      severity: severityFilter === "all" ? undefined : severityFilter,
      limit: 200,
    }),
    getVaccinationVerificationQueue({ parkId, limit: 200 }),
  ]);

  const items: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  const queueItems: VaccinationQueueItem[] = queue.ok ? queue.data.items : [];
  const verificationHeaders = tableLabels(pageContract, "verification-queue");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const boardPageSizeOptions = tablePageSizes(pageContract, "work-board");
  const queuePageSizeOptions = tablePageSizes(pageContract, "verification-queue");
  const boardPaged = paginateRows(items, sp, "ac", 10, boardPageSizeOptions);
  const queuePaged = paginateRows(queueItems, sp, "verify", 10, queuePageSizeOptions);
  const nextCursor = actionCenter.ok ? actionCenter.data.next_cursor : undefined;
  const selectedActionRowId = one(sp, "ac_row");
  const selectedActionRow = selectedActionRowId ? items.find((row) => row.row_id === selectedActionRowId) : undefined;

  // Server-authoritative counts per work state (not derived from the capped page).
  const stateCounts = new Map<WorkState, number>();
  if (actionCenter.ok) for (const c of actionCenter.data.counts_by_work_state) stateCounts.set(c.work_state, c.count);
  const totalCount = Array.from(stateCounts.values()).reduce((a, b) => a + b, 0);
  const overdueCount = stateCounts.get("overdue") ?? 0;
  const dueCount = stateCounts.get("due") ?? 0;
  const severityCounts = new Map<ProcessIntegritySeverity, number>();
  for (const item of items) severityCounts.set(item.severity, (severityCounts.get(item.severity) ?? 0) + 1);

  // Filter links preserve the FULL top-bar scope (scopeHref) and layer the page filters on top — never
  // hand-rolled, so park/range/as_of/date_from/date_to are never dropped.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/action-center", scope, {}, {
      bucket: view === "verify" ? "verify" : undefined,
      severity: severityFilter,
      state: stateFilter,
      ac_page: String(boardPaged.page),
      ac_limit: String(boardPaged.pageSize),
      verify_page: String(queuePaged.page),
      verify_limit: String(queuePaged.pageSize),
      ...overrides,
    });
  }
  function boardPagerHref(page: number): string {
    return hrefWith({ ac_page: String(page), ac_row: undefined });
  }
  function boardPageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ ac_page: "1", ac_limit: String(pageSize), ac_row: undefined });
  }
  function queuePagerHref(page: number): string {
    return hrefWith({ bucket: "verify", verify_page: String(page) });
  }
  function queuePageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ bucket: "verify", verify_page: "1", verify_limit: String(pageSize) });
  }
  const verifyReturnTo = hrefWith({ bucket: "verify" });
  const stateFilterLinks = [
    { label: copy(pageContract, "filter.all"), href: hrefWith({ state: "all", ac_page: "1", ac_row: undefined }), active: stateFilter === "all", count: totalCount },
    ...WORK_STATE_ORDER.map((state) => ({
      label: optionLabel(workStateOptions, state),
      href: hrefWith({ state, ac_page: "1", ac_row: undefined }),
      active: stateFilter === state,
      count: stateCounts.get(state) ?? 0,
    })),
  ];
  const severityFilterLinks = [
    { label: copy(pageContract, "filter.all_severity"), href: hrefWith({ severity: "all", ac_page: "1", ac_row: undefined }), active: severityFilter === "all", count: items.length },
    ...SEVERITY_ORDER.map((severity) => ({
      label: optionLabel(severityOptions, severity),
      href: hrefWith({ severity, ac_page: "1", ac_row: undefined }),
      active: severityFilter === severity,
      count: severityCounts.get(severity) ?? 0,
    })),
  ];
  const clearFiltersHref = hrefWith({ severity: "all", state: "all", ac_page: "1", ac_row: undefined });

  return (
    <div className="screen on">
	      <div className="phead">
	        <div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
	        </div>
	      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
	          <div className="note" style={{ marginBottom: 14 }}>
	            <Tag tone="ok">{copy(pageContract, "action.success_tag")}</Tag> {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
	          </div>
	        ) : (
	          <div className="alert" style={{ marginBottom: 14 }}>
	            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
	          </div>
	        )
      ) : null}

      {/* View switch — Status board vs the SOP/verification queue (mock #acView). */}
	      <div className="subtabs">
	        <Link href={hrefWith({ bucket: undefined })} replace scroll={false} className={view === "board" ? "on" : ""}>
	          {copy(pageContract, "view.status_board")}
	        </Link>
	        <Link href={hrefWith({ bucket: "verify" })} replace scroll={false} className={view === "verify" ? "on" : ""}>
	          {copy(pageContract, "view.sop_queues")} <span className="cbq">{queueItems.length}</span>
	        </Link>
	      </div>

      {!actionCenter.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{actionCenter.error.code ?? actionCenter.error.kind}</b>&nbsp;{actionCenter.error.message}
        </div>
      ) : null}
      {actionCenter.ok && !queue.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{queue.error.code ?? queue.error.kind}</b>&nbsp;{queue.error.message}
        </div>
      ) : null}

      {view === "verify" ? (
        // ===== SOP queues — verification surface (accept / reject / request rework) =====
        <section className="card" data-filter-scope>
	          <div className="hd">
	            <Video className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.verification.title")}</h3>
	            <Tag tone={queueItems.length ? "warn" : "mut"}>{queueItems.length}</Tag>
	            <div className="sp" style={{ flex: 1 }} />
	            <span className="muted small">{copy(pageContract, "section.verification.note")}</span>
	          </div>
	          <div className="tbar">
	            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.verification.search")} />
	            <VaccinationFilterButton
	              pageContract={pageContract}
	              title={copy(pageContract, "filter.verification.title")}
	              searchReason={copy(pageContract, "filter.verification.search_reason")}
	              filterReason={copy(pageContract, "filter.verification.filter_reason")}
	              rowsLabel={`${queuePaged.start}-${queuePaged.end} of ${queueItems.length} rows · ${copy(pageContract, "filter.verification.rows_suffix")}`}
	              facets={verificationHeaders}
	            />
            <span className="muted small">
              {queuePaged.start}-{queuePaged.end} of {queueItems.length} rows
            </span>
          </div>
          {queueItems.length === 0 ? (
            <div className="bd">
	              <p className="muted small" style={{ margin: 0 }}>
	                {copy(pageContract, "section.verification.empty")}
	              </p>
	            </div>
	          ) : (
	            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.verification.aria")}>
	              <table>
	                <thead>
	                  <tr>
	                    {verificationHeaders.map((label) => (
	                      <th key={label}>{label}</th>
	                    ))}
	                  </tr>
	                </thead>
                <tbody>
                  {queuePaged.items.map((q) => (
                    <tr key={q.completion_id}>
                      <td>
                        <span className="gid">{shortId(q.goat_id)}</span>
                      </td>
                      <td>{fmtDate(q.administered_at)}</td>
                      <td>{q.doses}</td>
                      <td>
                        <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                          <ActionForm action={verifyCompletionAction} completionId={q.completion_id} taskId={q.sop_task_id} rowVersion={q.sop_task_row_version} returnTo={verifyReturnTo} disabledTitle={copy(pageContract, "reason.no_sop_review_handle")} primary>
                            {copy(pageContract, "action.verify")}
                          </ActionForm>
                          <ActionForm action={rejectCompletionAction} completionId={q.completion_id} taskId={q.sop_task_id} rowVersion={q.sop_task_row_version} returnTo={verifyReturnTo} reason="rejected" disabledTitle={copy(pageContract, "reason.no_sop_review_handle")}>
                            {copy(pageContract, "action.reject")}
                          </ActionForm>
                          <ActionForm action={rejectCompletionAction} completionId={q.completion_id} taskId={q.sop_task_id} rowVersion={q.sop_task_row_version} returnTo={verifyReturnTo} reason="rework_requested" disabledTitle={copy(pageContract, "reason.no_sop_review_handle")}>
                            {copy(pageContract, "action.request_rework")}
                          </ActionForm>
	                          <Link href={`/goats/${q.goat_id}`} className="btn sm">
	                            {copy(pageContract, "action.passport")}
	                          </Link>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <VaccinationTablePager
            pageContract={pageContract}
            pageSizeOptions={queuePageSizeOptions}
            page={queuePaged.page}
            pageSize={queuePaged.pageSize}
            total={queuePaged.total}
            start={queuePaged.start}
            end={queuePaged.end}
            noun={copy(pageContract, "table.verification_queue.noun")}
            hrefForPage={queuePagerHref}
            hrefForPageSize={queuePageSizeHref}
          />
        </section>
      ) : (
        // ===== Status board — mock taskboard shell from the real Action Center process-integrity rows =====
        <div data-filter-scope>
          {/* Domain filter. Only the active vaccination module is visible in this slice. */}
	          <div className="subtabs" id="acPillars" aria-label={copy(pageContract, "filter.domains.aria")}>
	            <Link href={hrefWith({ severity: "all", state: "all", ac_page: "1", ac_row: undefined })} replace scroll={false} className="on">
	              {copy(pageContract, "filter.domain.vaccination")} <span className="cbq">{totalCount}</span>
	            </Link>
	          </div>

          {/* Quick tabs (mock #acQuickTabs) + My tasks + Filters. Counts are server-authoritative. */}
          <div id="acToggles" style={{ display: "flex", gap: 8, marginBottom: 14, flexWrap: "wrap", alignItems: "center" }}>
            <div className="subtabs" id="acQuickTabs" style={{ margin: 0 }}>
	              <Link href={hrefWith({ state: "all", ac_page: "1", ac_row: undefined })} replace scroll={false} className={stateFilter === "all" ? "on" : ""}>
	                {copy(pageContract, "filter.all")} <span className="qc">{totalCount}</span>
	              </Link>
	              <Link href={hrefWith({ state: "overdue", ac_page: "1", ac_row: undefined })} replace scroll={false} className={stateFilter === "overdue" ? "on" : ""}>
	                {copy(pageContract, "filter.overdue")} <span className="qc">{overdueCount}</span>
	              </Link>
	              <Link href={hrefWith({ state: "due", ac_page: "1", ac_row: undefined })} replace scroll={false} className={stateFilter === "due" ? "on" : ""}>
	                {copy(pageContract, "filter.due")} <span className="qc">{dueCount}</span>
	              </Link>
	              <Link href={hrefWith({ bucket: "verify", verify_page: "1" })} replace scroll={false} className="">
	                {copy(pageContract, "filter.awaiting_verification")} <span className="qc">{queueItems.length}</span>
	              </Link>
            </div>
            <span className="sp" style={{ flex: 1 }} />
	            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_visible_cards")} />
	            <ActionCenterFiltersButton
	              pageContract={pageContract}
	              label={copy(pageContract, "filter.my_tasks.title")}
	              mode="my"
	              rowsLabel={`${boardPaged.start}-${boardPaged.end} of ${items.length} rows shown · local owner filter applies to visible ${copy(pageContract, "filter.cards_label")}`}
	              clearHref={clearFiltersHref}
	              stateLinks={stateFilterLinks}
	              severityLinks={severityFilterLinks}
	            />
	            <ActionCenterFiltersButton
	              pageContract={pageContract}
	              label={copy(pageContract, "action.filters")}
	              rowsLabel={`${boardPaged.start}-${boardPaged.end} of ${items.length} rows shown · ${totalCount} total in current work-state counts`}
	              clearHref={clearFiltersHref}
              stateLinks={stateFilterLinks}
              severityLinks={severityFilterLinks}
            />
          </div>

          {/* Filters — severity (server-side). Park scope is the top bar's single source of truth. */}
          <div className="chipset" style={{ marginBottom: 12 }}>
	            <Link href={hrefWith({ severity: "all", ac_page: "1", ac_row: undefined })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
	              {copy(pageContract, "filter.all_severity")}
	            </Link>
	            {SEVERITY_ORDER.map((s) => (
	              <Link key={s} href={hrefWith({ severity: s, ac_page: "1", ac_row: undefined })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
	                {optionLabel(severityOptions, s)}
	              </Link>
	            ))}
	          </div>

          {/* Explanatory band (mock). */}
	          <div className="note" style={{ marginBottom: 12 }}>
	            {copy(pageContract, "note.board_explainer")}{" "}
	            <Link href="/protocol-adherence" className="lk">
	              {copy(pageContract, "note.board_explainer.link_adherence")}
	            </Link>{" "}
	            {copy(pageContract, "note.board_explainer.link_joiner")}{" "}
	            <Link href="/" className="lk">
	              {copy(pageContract, "note.board_explainer.link_tower")}
	            </Link>
	            .
	          </div>

          {totalCount === 0 ? (
            <div className="note" style={{ marginBottom: 12, display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
              <Info className="ic" aria-hidden="true" style={{ color: "var(--brand)", flexShrink: 0 }} />
	              <span>
	                {actionCenter.ok
	                  ? copy(pageContract, "empty.work_board_detail")
	                  : copy(pageContract, "empty.unavailable")}
	              </span>
	              {actionCenter.ok ? (
	                <>
	                  <Link href="/config?category=vaccination" className="btn sm">
	                    {copy(pageContract, "action.open_config")}
	                  </Link>
	                  <Link href="/sops" className="btn sm">
	                    {copy(pageContract, "action.open_sops")}
	                  </Link>
	                </>
	              ) : null}
            </div>
          ) : nextCursor ? (
	            <div className="muted small" style={{ marginBottom: 12 }}>
	              {copy(pageContract, "note.board_paging")}
	            </div>
	          ) : null}

          {/* Board shell — always the full work-state column set (mock taskboard), "—" where empty. */}
	          <WorkBoard pageContract={pageContract} rows={boardPaged.items} showAllColumns drawerHrefForRow={(row) => hrefWith({ ac_row: row.row_id })} />
          <VaccinationTablePager
            pageContract={pageContract}
            pageSizeOptions={boardPageSizeOptions}
            page={boardPaged.page}
            pageSize={boardPaged.pageSize}
            total={boardPaged.total}
            start={boardPaged.start}
            end={boardPaged.end}
            noun={copy(pageContract, "filter.rows_label")}
            hrefForPage={boardPagerHref}
            hrefForPageSize={boardPageSizeHref}
          />
          {selectedActionRow ? (
            <ActionCenterRowDrawer
              row={selectedActionRow}
              closeHref={hrefWith({ ac_row: undefined })}
              returnTo={hrefWith({ ac_row: selectedActionRow.row_id })}
              workflowHref={scopeHref(`/workflows/${encodeURIComponent(selectedActionRow.row_id)}`, scope, {}, { from: "action-center" })}
	              passportHref={selectedActionRow.goat_id ? `/goats/${encodeURIComponent(selectedActionRow.goat_id)}` : undefined}
	              pageContract={pageContract}
	            />
          ) : null}
        </div>
      )}
    </div>
  );
}

function ActionCenterRowDrawer({
  row,
  closeHref,
  returnTo,
  workflowHref,
  passportHref,
  pageContract,
}: {
  row: ActionCenterObligation;
  closeHref: string;
  returnTo: string;
  workflowHref: string;
  passportHref?: string;
  pageContract: AdminUiPageContract;
}) {
  const blocker = row.blocker_reason;
  const ownerMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const ownerName = row.owner?.operator_name ?? row.owner?.park_head_name ?? copy(pageContract, "label.unassigned");
  const title = actionWorkTitle(pageContract, row);
  const priority = PRIORITY_BY_SEVERITY[row.severity];
  const priorityOptions = optionGroup(pageContract, "priority_chips");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const sopStateOptions = optionGroup(pageContract, "sop_state_chips");
  const proofStateOptions = optionGroup(pageContract, "proof_state_chips");
  const verificationStateOptions = optionGroup(pageContract, "verification_state_chips");
  const sopProgress = sopDoneThrough(row);
  const hasCompletion = !!row.completion_id;
  const completionId = row.completion_id ?? "";
  const taskId = row.sop_task_id;
  const taskRowVersion = row.sop_task_row_version;
  const fieldActionNote = copy(pageContract, "drawer.disabled_field_action");
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.work_item.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.work_item.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.work_item.eyebrow")}</div>
            <h2>{title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.work_item.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          {/* Computed adherence status — never a manual label (mock). */}
          <div className="fld">
            <label>{copy(pageContract, "drawer.adherence_status_label")}</label>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <Tag tone={optionTone(workStateOptions, row.work_state)}>{optionLabel(workStateOptions, row.work_state)}</Tag>
              <Tag tone={optionTone(severityOptions, row.severity)}>{optionLabel(severityOptions, row.severity)}</Tag>
              <span className="muted small">{copy(pageContract, "drawer.adherence_status_help")}</span>
            </div>
          </div>

          {/* Owner chain + Due. Owner is assigned via the workflow record; due comes from the protocol schedule. */}
          <div className="fld" style={{ display: "flex", gap: 10 }}>
            <div style={{ flex: 1 }}>
              <label>{copy(pageContract, "drawer.owner_chain_label")}</label>
              {ownerMissing ? (
                <div style={{ paddingTop: 4 }}>
                  <Tag tone="dng">{copy(pageContract, "drawer.owner_missing")}</Tag>
                </div>
              ) : (
                <select disabled title={copy(pageContract, "drawer.owner_chain_disabled")} defaultValue={ownerName}>
                  <option>{ownerName}</option>
                </select>
              )}
            </div>
            <div style={{ flex: 1 }}>
              <label>{copy(pageContract, "drawer.due_label")}</label>
              <input disabled title={copy(pageContract, "drawer.due_date_disabled")} defaultValue={fmtDate(row.due_at)} />
            </div>
          </div>

          {/* Priority — derived from computed severity, shown read-only (disabled-with-reason). */}
          <div className="fld">
            <label>{copy(pageContract, "drawer.priority_label")}</label>
            <div className="chipset" title={copy(pageContract, "drawer.priority_disabled")}>
              {priorityOptions.map((p) => (
                <span key={p.key} className={`chip${p.key === priority ? " on" : ""}`} aria-disabled="true">
                  {p.label}
                </span>
              ))}
            </div>
          </div>

          {/* Obligation facts — compact metagrid (mock density), not a flat full-record dump. */}
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "drawer.protocol_label")}</div>
              <div className="v">{row.protocol_name}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.dose_label")}</div>
              <div className="v">{row.dose_code}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.park_shed_label")}</div>
              <div className="v">
                {row.park_name} · {row.shed_name}
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.cohort_progress_label")}</div>
              <div className="v">
                {row.animal_stage} · {row.completed_count}/{row.expected_count} {copy(pageContract, "label.done_suffix")}
              </div>
            </div>
          </div>

          {blocker ? (
            <div className="alert" style={{ marginTop: 14, marginBottom: 0 }}>
              <Ban className="ic" aria-hidden="true" />
              <span>{blocker}</span>
            </div>
          ) : null}

          {/* SOP checklist — the vaccination drive SOP's procedure STEPS with per-step video-proof pills
              (mock #taskDrawer checklist anatomy), NOT the obligation lifecycle chain (that is the Workflow
              record). Step done/current is derived read-only from the computed obligation states. */}
          <div style={{ marginTop: 16 }}>
            <div className="b700" style={{ margin: "4px 0 10px" }}>
              {copy(pageContract, "drawer.sop_checklist.title")}
            </div>
            <SopChecklist steps={VACCINATION_DRIVE_SOP_STEPS} doneThrough={sopProgress} />
          </div>

          {/* Computed SOP / proof / verification state chips. */}
          <div className="chipset" style={{ marginTop: 14 }}>
            <Tag tone={optionTone(sopStateOptions, row.sop_task_state)}>{optionLabel(sopStateOptions, row.sop_task_state)}</Tag>
            <Tag tone={optionTone(proofStateOptions, row.proof_state)}>{optionLabel(proofStateOptions, row.proof_state)}</Tag>
            <Tag tone={optionTone(verificationStateOptions, row.verification_state)}>{optionLabel(verificationStateOptions, row.verification_state)}</Tag>
          </div>

          {/* Linked surfaces (mock "Linked"). lucide icons — never emoji. */}
          <div style={{ marginTop: 16 }}>
            <div className="b700" style={{ margin: "2px 0 8px" }}>
              {copy(pageContract, "drawer.linked_title")}
            </div>
            <div style={{ display: "flex", gap: 7, flexWrap: "wrap" }}>
              <Link href={workflowHref} className="tag t-info" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <GitBranch className="ic" style={{ width: 12 }} aria-hidden="true" />
                {copy(pageContract, "drawer.link.workflow_record")}
              </Link>
              <Link href="/protocol-adherence" className="tag t-warn" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <ShieldCheck className="ic" style={{ width: 12 }} aria-hidden="true" />
                {copy(pageContract, "drawer.link.adherence")}
              </Link>
              <Link href="/vaccination" className="tag t-teal" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <Syringe className="ic" style={{ width: 12 }} aria-hidden="true" />
                {copy(pageContract, "drawer.link.vaccination")}
              </Link>
            </div>
          </div>
        </div>

        {/* Action row (mock #tdFoot). Verify / Request rework require a SOP review handle; un-backed
            steps keep the mock look but are disabled-with-reason. */}
        <div className="df">
          <Link href={workflowHref} className="btn p">
            {row.next_action}
          </Link>
          <button type="button" className="btn" disabled aria-disabled="true" title={fieldActionNote}>
            {copy(pageContract, "action.start_sop")}
          </button>
          <button type="button" className="btn" disabled aria-disabled="true" title={fieldActionNote}>
            {copy(pageContract, "action.submit_proof")}
          </button>
          {hasCompletion ? (
            <ActionForm action={verifyCompletionAction} completionId={completionId} taskId={taskId} rowVersion={taskRowVersion} returnTo={returnTo} disabledTitle={copy(pageContract, "reason.no_sop_review_handle")}>
              {copy(pageContract, "action.verify")}
            </ActionForm>
          ) : (
            <button type="button" className="btn" disabled aria-disabled="true" title={copy(pageContract, "reason.no_recorded_dose_verify")}>
              {copy(pageContract, "action.verify")}
            </button>
          )}
          {hasCompletion ? (
            <ActionForm action={rejectCompletionAction} completionId={completionId} taskId={taskId} rowVersion={taskRowVersion} returnTo={returnTo} reason="rework_requested" disabledTitle={copy(pageContract, "reason.no_sop_review_handle")}>
              {copy(pageContract, "action.request_rework")}
            </ActionForm>
          ) : (
            <button type="button" className="btn" disabled aria-disabled="true" title={copy(pageContract, "reason.no_recorded_dose_rework")}>
              {copy(pageContract, "action.request_rework")}
            </button>
          )}
          <button type="button" className="btn" disabled aria-disabled="true" title={fieldActionNote}>
            {copy(pageContract, "action.escalate")}
          </button>
          <Link href={workflowHref} className="btn">
            {copy(pageContract, "action.workflow_record")}
          </Link>
          {passportHref ? (
            <Link href={passportHref} className="btn">
              {copy(pageContract, "action.goat_passport")}
            </Link>
          ) : null}
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}
