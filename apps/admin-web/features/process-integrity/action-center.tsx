import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { Info, Video } from "lucide-react";
import { getVaccinationActionCenter, getVaccinationVerificationQueue } from "@/lib/api/server";
import { actionFeedbackCopy, copy, optionGroup, tableLabels, tablePageSizes, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  ActionCenterObligation,
  ProcessIntegritySeverity,
  VaccinationQueueItem,
  WorkState,
} from "@/lib/api/server";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import {
  SEVERITY_ORDER,
  WORK_STATE_ORDER,
} from "./process-integrity";
import {
  VaccinationFilterButton,
  VisibleTableSearch,
  VaccinationTablePager,
  type VaccinationPageSize,
} from "@/features/preventive-care-vaccination";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { ActionCenterFiltersButton } from "./action-center-filters";
import { actionCenterRequestPlan } from "./action-center-request-plan";
import { ActionCenterLocalDrawer } from "./action-center-local-drawer";
import { WorkBoard } from "./work-board";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

const PATH = "/action-center";

function optionLabel(options: AdminUiOption[], key: string): string {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return option.label;
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "—";
}

function hasReviewHandle(taskId?: string, rowVersion?: number): boolean {
  return Boolean(taskId) && Number(rowVersion ?? 0) > 0;
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
  const canReview = hasReviewHandle(taskId, rowVersion);
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
  const requestedView = one(sp, "bucket");
  const view = requestedView === "verify" ? requestedView : "board";
  const stateFilter = (WORK_STATE_ORDER.find((s) => s === one(sp, "state")) ?? "all") as WorkState | "all";
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const { parkId, asOf } = backendScope(parseScope(sp));
  const scope = parseScope(sp);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const boardPageSizeOptions = tablePageSizes(pageContract, "work-board");
  const queuePageSizeOptions = tablePageSizes(pageContract, "verification-queue");
  const requestedBoardPageSize = boardPageSizeOptions.find((size) => size === boundedInt(one(sp, "ac_limit"), 10, 1, 100)) ?? 10;
  const boardCursor = one(sp, "ac_cursor");
  const boardCursorStack = sp.ac_cursor_stack;
  const requestedQueuePageSize = queuePageSizeOptions.find((size) => size === boundedInt(one(sp, "verify_limit"), 10, 1, 100)) ?? 10;
  const queuePage = boundedInt(one(sp, "verify_page"), 1, 1, 1000000);
  const queueCursor = one(sp, "verify_cursor");
  const queueCursorStack = sp.verify_cursor_stack;
  const requestPlan = actionCenterRequestPlan({
    stateFilter,
    severityFilter,
    requestedBoardPage: { pageSize: requestedBoardPageSize },
    boardCursor,
    requestedQueuePageSize,
    queueCursor,
    parkId,
    asOf,
  });

  // Board source = the real process-integrity Action Center contract (server-computed work state,
  // severity, owner, proof/verify state, next action). Verification queue = actionable completions.
  // Both honor the top-bar park scope (park_id) so the SOP/verification queue can't show other parks.
  // IMPORTANT: Fetch only the active tab to avoid OFFSET pagination debt and dual-tab eager fetch.
  // The page shows either the board (view === "board") OR the queue (view === "verify"), never both.
  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter(requestPlan.actionCenter),
    view === "verify" ? getVaccinationVerificationQueue(requestPlan.verificationQueue) : Promise.resolve(null),
  ]);

  const items: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  const boardRows = items;
  const queueItems: VaccinationQueueItem[] = queue?.ok ? queue.data.items : [];
  const queueTotalCount = queue?.ok ? queue.data.total_count : 0;
  const verificationHeaders = tableLabels(pageContract, "verification-queue");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const boardTotalCount = actionCenter.ok ? actionCenter.data.total_count : 0;
  const boardNextCursor = actionCenter.ok ? actionCenter.data.next_cursor : undefined;
  const boardPage = boundedInt(one(sp, "ac_page"), 1, 1, 1000000);
  const boardStart = boardTotalCount === 0 ? 0 : (boardPage - 1) * requestedBoardPageSize + 1;
  const boardEnd = boardTotalCount === 0 ? 0 : Math.min(boardTotalCount, boardStart + items.length - 1);
  const boardPaged = {
    items,
    page: boardPage,
    pageSize: requestedBoardPageSize,
    total: boardTotalCount,
    start: boardStart,
    end: boardEnd,
  };
  const queueTotalPages = Math.max(1, Math.ceil(queueTotalCount / requestedQueuePageSize));
  const normalizedQueuePage = Math.min(queuePage, queueTotalPages);
  const queueStart = queueTotalCount === 0 ? 0 : (normalizedQueuePage - 1) * requestedQueuePageSize + 1;
  const queueEnd = queueTotalCount === 0 ? 0 : Math.min(queueTotalCount, queueStart + queueItems.length - 1);
  const queuePaged = {
    items: queueItems,
    page: normalizedQueuePage,
    pageSize: requestedQueuePageSize,
    total: queueTotalCount,
    totalPages: queueTotalPages,
    start: queueStart,
    end: queueEnd,
  };
  const boardNextHref =
    boardNextCursor
      ? hrefWithPagedCursor(PATH, sp, "ac_cursor", boardNextCursor, "ac_page", "ac_cursor_stack")
      : null;
  const boardPrevHref = hrefPreviousPagedCursor(PATH, sp, "ac_cursor", "ac_page", "ac_cursor_stack");
  if (boardPage > 1 && !boardCursor && !boardCursorStack) {
    redirect(hrefWithPagedCursor(PATH, sp, "ac_cursor", null, "ac_page", "ac_cursor_stack") || hrefWith({ ac_page: "1", ac_cursor: undefined, ac_cursor_stack: undefined }));
  }
  const queueNextHref =
    queue?.ok && queue.data.next_cursor
      ? hrefWithPagedCursor(PATH, sp, "verify_cursor", queue.data.next_cursor, "verify_page", "verify_cursor_stack")
      : null;
  const queuePrevHref = hrefPreviousPagedCursor(PATH, sp, "verify_cursor", "verify_page", "verify_cursor_stack");
  if (queue?.ok && normalizedQueuePage > 1 && !queueCursor && !queueCursorStack) {
    redirect(hrefWith({ bucket: "verify", verify_page: "1", verify_limit: String(requestedQueuePageSize), verify_cursor: undefined, verify_cursor_stack: undefined }));
  }
  const selectedActionRowId = one(sp, "ac_row");

  // Filter chips use server totals; WorkBoard lane headers stay derived from visible rows.
  const stateCounts = new Map<WorkState, number>();
  if (actionCenter.ok) for (const c of actionCenter.data.counts_by_work_state) stateCounts.set(c.work_state, c.count);
  const totalCount = actionCenter.ok ? actionCenter.data.total_count : 0;
  const overdueCount = stateCounts.get("overdue") ?? 0;
  const dueCount = stateCounts.get("due") ?? 0;
  const hasBoardFilters = stateFilter !== "all" || severityFilter !== "all";

  // Filter links preserve the FULL top-bar scope (scopeHref) and layer the page filters on top — never
  // hand-rolled, so park/range/as_of/date_from/date_to are never dropped.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/action-center", scope, {}, {
      bucket: view === "board" ? undefined : view,
      severity: severityFilter,
      state: stateFilter,
      ac_page: String(boardPaged.page),
      ac_limit: String(boardPaged.pageSize),
      ac_cursor: undefined,
      ac_cursor_stack: undefined,
      verify_page: String(queuePaged.page),
      verify_limit: String(queuePaged.pageSize),
      verify_cursor: undefined,
      verify_cursor_stack: undefined,
      ...overrides,
    });
  }
  function boardPagerHref(page: number): string {
    if (page > boardPaged.page) return boardNextHref ?? hrefWith({ bucket: undefined });
    if (page < boardPaged.page) return boardPrevHref ?? hrefWith({ bucket: undefined });
    return hrefWith({ ac_page: String(page), ac_row: undefined });
  }
  function boardPageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ ac_page: "1", ac_limit: String(pageSize), ac_cursor: undefined, ac_cursor_stack: undefined, ac_row: undefined });
  }
  function queuePagerHref(page: number): string {
    if (page > queuePaged.page) return queueNextHref ?? hrefWith({ bucket: "verify" });
    if (page < queuePaged.page) return queuePrevHref ?? hrefWith({ bucket: "verify" });
    return hrefWith({ bucket: "verify", verify_page: String(page) });
  }
  function queuePageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ bucket: "verify", verify_page: "1", verify_limit: String(pageSize), verify_cursor: undefined, verify_cursor_stack: undefined });
  }
  const verifyReturnTo = hrefWithoutAction(PATH, { ...sp, bucket: "verify" });
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
    { label: copy(pageContract, "filter.all_severity"), href: hrefWith({ severity: "all", ac_page: "1", ac_row: undefined }), active: severityFilter === "all" },
    ...SEVERITY_ORDER.map((severity) => ({
      label: optionLabel(severityOptions, severity),
      href: hrefWith({ severity, ac_page: "1", ac_row: undefined }),
      active: severityFilter === severity,
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
	          {copy(pageContract, "view.sop_queues")} <span className="cbq">{queueTotalCount}</span>
	        </Link>
	      </div>

      {!actionCenter.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{actionCenter.error.code ?? actionCenter.error.kind}</b>&nbsp;{actionCenter.error.message}
        </div>
      ) : null}
      {actionCenter.ok && queue && !queue.ok ? (
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
	            <Tag tone={queueTotalCount ? "warn" : "mut"}>{queueTotalCount}</Tag>
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
	              rowsLabel={`${queuePaged.start}-${queuePaged.end} of ${queueTotalCount} rows · ${copy(pageContract, "filter.verification.rows_suffix")}`}
	              facets={verificationHeaders}
	            />
            <span className="muted small">
              {queuePaged.start}-{queuePaged.end} of {queueTotalCount} rows
            </span>
          </div>
          {queueTotalCount === 0 ? (
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
                  {queuePaged.items.map((q) => {
                    const canReview = hasReviewHandle(q.sop_task_id, q.sop_task_row_version);
                    return (
                      <tr key={q.completion_id}>
                        <td>
                          <span className="gid">{shortId(q.goat_id)}</span>
                        </td>
                        <td>{fmtDate(q.administered_at)}</td>
                        <td>{q.doses}</td>
                        <td>
                          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                            {canReview ? (
                              <>
                                <ActionForm action={verifyCompletionAction} completionId={q.completion_id} taskId={q.sop_task_id} rowVersion={q.sop_task_row_version} returnTo={verifyReturnTo} primary>
                                  {copy(pageContract, "action.verify")}
                                </ActionForm>
                                <ActionForm action={rejectCompletionAction} completionId={q.completion_id} taskId={q.sop_task_id} rowVersion={q.sop_task_row_version} returnTo={verifyReturnTo} reason="rejected">
                                  {copy(pageContract, "action.reject")}
                                </ActionForm>
                                <ActionForm action={rejectCompletionAction} completionId={q.completion_id} taskId={q.sop_task_id} rowVersion={q.sop_task_row_version} returnTo={verifyReturnTo} reason="rework_requested">
                                  {copy(pageContract, "action.request_rework")}
                                </ActionForm>
                              </>
                            ) : (
                              <Tag tone="warn">{copy(pageContract, "reason.no_sop_review_handle")}</Tag>
                            )}
	                            <Link href={`/goats/${q.goat_id}`} className="btn sm">
	                              {copy(pageContract, "action.passport")}
	                            </Link>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
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
	                {copy(pageContract, "filter.awaiting_verification")} <span className="qc">{queueTotalCount}</span>
	              </Link>
            </div>
            <span className="sp" style={{ flex: 1 }} />
	            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_visible_cards")} />
	            <ActionCenterFiltersButton
	              pageContract={pageContract}
	              label={copy(pageContract, "filter.my_tasks.title")}
	              mode="my"
	              rowsLabel={`${boardPaged.start}-${boardPaged.end} of ${boardPaged.total} rows · owner filter applies after the server page loads`}
	              clearHref={clearFiltersHref}
	              stateLinks={stateFilterLinks}
	              severityLinks={severityFilterLinks}
	            />
	            <ActionCenterFiltersButton
	              pageContract={pageContract}
	              label={copy(pageContract, "action.filters")}
	              rowsLabel={`${boardPaged.start}-${boardPaged.end} of ${boardPaged.total} rows`}
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
	                {hasBoardFilters
	                  ? copy(pageContract, "empty.work_board_filtered")
	                  : copy(pageContract, "empty.work_board_detail")}
	              </span>
	              {hasBoardFilters ? (
	                <Link href={clearFiltersHref} className="btn sm">
	                  {copy(pageContract, "filter.clear_all")}
	                </Link>
	              ) : (
	                <>
	                  <Link href="/config?category=vaccination" className="btn sm">
	                    {copy(pageContract, "action.open_config")}
	                  </Link>
	                  <Link href="/sops" className="btn sm">
	                    {copy(pageContract, "action.open_sops")}
	                  </Link>
	                </>
	              )}
            </div>
          ) : boardNextCursor ? (
	            <div className="muted small" style={{ marginBottom: 12 }}>
	              {copy(pageContract, "note.board_paging")}
	            </div>
	          ) : null}

          <WorkBoard
            pageContract={pageContract}
            rows={boardRows}
            drawerHrefForRow={(row) => `${hrefWith({ ac_row: undefined })}#ac_row=${encodeURIComponent(row.row_id)}`}
          />
          <ActionCenterLocalDrawer
            rows={boardRows}
            pageContract={pageContract}
            initialSelectedRowId={selectedActionRowId}
            drawerHrefs={Object.fromEntries(
              boardRows.map((row) => [
                row.row_id,
                `${hrefWith({ ac_row: undefined })}#ac_row=${encodeURIComponent(row.row_id)}`,
              ]),
            )}
            closeHref={hrefWith({ ac_row: undefined })}
            workflowHrefs={Object.fromEntries(
              boardRows.map((row) => [
                row.row_id,
                scopeHref(`/workflows/${encodeURIComponent(row.row_id)}`, scope, {}, { from: "action-center" }),
              ]),
            )}
            passportHrefs={Object.fromEntries(
              boardRows.flatMap((row) => (row.goat_id ? [[row.row_id, `/goats/${encodeURIComponent(row.goat_id)}`]] : [])),
            )}
          />
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
        </div>
      )}
    </div>
  );
}
