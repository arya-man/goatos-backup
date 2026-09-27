import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { InfoTip } from "@/components/app/info-tip";
import { LinkButton } from "@/components/app/link-button";
import { FilterChip } from "@/components/app/list/filter-chip";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { PageHeader, type PageCrumb } from "@/components/app/page-header";
import { TemplateTabs } from "@/components/app/template-tabs";
import { COURSE_WIDGET_ICONS } from "@/lib/minimal-icons";
import type { PaletteColorKey } from "@/theme/core";
import { getVaccinationActionCenter, getVaccinationVerificationQueue } from "@/lib/api/server";
import { actionFeedbackCopy, copy, optionalCopy, optionGroup, tableLabels, tablePageSizes, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  ActionCenterObligation,
  ProcessIntegritySeverity,
  VaccinationQueueItem,
  WorkState,
} from "@/lib/api/server";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { listOrEmpty } from "@/lib/list-or-empty";
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
import { VerificationRowActions } from "./verification-row-actions";
import { WorkBoard } from "./work-board";
import { fmtDate } from "@/lib/format";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { ChipRowSkeleton, KanbanSkeleton, KpiRowSkeleton, PagerSkeleton, StackSkeleton, TableSkeleton, ToolbarSkeleton } from "@/components/app/skeletons";

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

  const items: ActionCenterObligation[] = actionCenter.ok ? listOrEmpty(actionCenter.data.items) : [];
  const boardRows = items;
  const queueItems: VaccinationQueueItem[] = queue?.ok ? listOrEmpty(queue.data.items) : [];
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
  if (actionCenter.ok) for (const c of listOrEmpty(actionCenter.data.counts_by_work_state)) stateCounts.set(c.work_state, c.count);
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


  // Breadcrumb trail. The parent segment is the contract's own `crumb` copy -- rendered only when
  // the backend actually supplies one AND it is not just the page title again, which is what the
  // old `<div className="crumb"><b>{title}</b></div>` rendered on every route in this module. No
  // section name is invented here: a missing key means a single muted current segment, and the
  // real two-level trail lands the moment the contract carries the section label.
  const crumbSection = optionalCopy(pageContract, "crumb");
  const crumbItems: PageCrumb[] = [
    ...(crumbSection && crumbSection !== pageContract.title ? [{ label: crumbSection, href: "/" }] : []),
    { label: pageContract.title },
  ];

  // Server-authoritative quick totals as the template Course overview KPI row. Each tile is the
  // old quick filter chip: tapping it narrows the board to that work state (tap again to clear).
  const quickState = stateFilter === "overdue" || stateFilter === "due" ? stateFilter : "all";
  const quickTiles: Array<{ key: "all" | "overdue" | "due"; title: string; total: number; color: PaletteColorKey; icon: string }> = [
    { key: "all", title: copy(pageContract, "filter.all"), total: totalCount, color: "info", icon: COURSE_WIDGET_ICONS.progress },
    { key: "overdue", title: copy(pageContract, "filter.overdue"), total: overdueCount, color: "error", icon: COURSE_WIDGET_ICONS.certificates },
    { key: "due", title: copy(pageContract, "filter.due"), total: dueCount, color: "warning", icon: COURSE_WIDGET_ICONS.completed },
  ];

  // Template composition: page-level view Tabs (account layout), then either the Course overview
  // KPI row + filter toolbar card + kanban-style status lanes (board), or ONE order-list card
  // (CardHeader + Label count, toolbar, Scrollbar + TableHeadCustom, pagination) for the queue.
  return (
    <Stack spacing={3}>
      <PageHeader title={pageContract.title} crumbs={crumbItems} />

      {actionStatus ? (
        actionStatus === "success" ? (
          <Alert severity="success">
            <b>{copy(pageContract, "action.success_tag")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        ) : (
          <Alert severity="error">
            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        )
      ) : null}

      {/* View switch — Status board vs the SOP/verification queue. */}
      <TemplateTabs
        ariaLabel={copy(pageContract, "view.status_board")}
        value={view}
        items={[
          { value: "board", label: copy(pageContract, "view.status_board"), href: hrefWith({ bucket: undefined }) },
          { value: "verify", label: copy(pageContract, "view.sop_queues"), count: view === "verify" ? queueTotalCount : undefined, href: hrefWith({ bucket: "verify" }) },
        ]}
      />

      {/* The view below the strip (guard: url-keyed-panel): a board / queue click shows the clicked
          view's skeleton in the same frame; header and strip stay on screen. */}
      <UrlSuspense searchParams={sp} watch={["bucket"]} fallback={VIEW_SKELETON[view === "verify" ? "verify" : ""]} fallbackBy={{ param: "bucket", shapes: VIEW_SKELETON }}>
      <Stack spacing={3}>
      {!actionCenter.ok ? (
        <Alert severity="error">
          <b>{actionCenter.error.code ?? actionCenter.error.kind}</b>&nbsp;{actionCenter.error.message}
        </Alert>
      ) : null}
      {actionCenter.ok && queue && !queue.ok ? (
        <Alert severity="error">
          <b>{queue.error.code ?? queue.error.kind}</b>&nbsp;{queue.error.message}
        </Alert>
      ) : null}

      {view === "verify" ? (
        // ===== SOP queues — verification surface (accept / reject / request rework) =====
        <Card data-filter-scope>
          <CardHeader
            title={copy(pageContract, "section.verification.title")}
            subheader={copy(pageContract, "section.verification.note")}
            action={<Label variant="soft" color={queueTotalCount ? "warning" : "default"}>{queueTotalCount}</Label>}
            sx={{ "& .MuiCardHeader-action": { alignSelf: "center" } }}
          />
          <Box sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.verification.search")} />
            <VaccinationFilterButton
              pageContract={pageContract}
              title={copy(pageContract, "filter.verification.title")}
              searchReason={copy(pageContract, "filter.verification.search_reason")}
              filterReason={copy(pageContract, "filter.verification.filter_reason")}
              rowsLabel={`${queuePaged.start}-${queuePaged.end} of ${queueTotalCount} rows · ${copy(pageContract, "filter.verification.rows_suffix")}`}
              facets={verificationHeaders}
            />
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {queuePaged.start}-{queuePaged.end} of {queueTotalCount} rows
            </Typography>
          </Box>
          <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<TableSkeleton bare header={false} columns={verificationHeaders.length || 4} rows={queuePaged.pageSize} />}>
          {queueTotalCount === 0 ? (
            <Typography variant="body2" sx={{ color: "text.secondary", px: 3, pb: 3 }}>
              {copy(pageContract, "section.verification.empty")}
            </Typography>
          ) : (
            <Scrollbar>
              <Table sx={{ minWidth: 720 }} aria-label={copy(pageContract, "section.verification.aria")}>
                <TableHeadCustom headCells={verificationHeaders.map((label, index) => ({ id: `col-${index}`, label, sortable: false }))} />
                <TableBody>
                  {queuePaged.items.map((q) => {
                    const canReview = hasReviewHandle(q.sop_task_id, q.sop_task_row_version);
                    return (
                      <TableRow key={q.completion_id} hover>
                        <TableCell>
                          <Typography variant="subtitle2" component="span">{shortId(q.goat_id)}</Typography>
                        </TableCell>
                        <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(q.administered_at)}</TableCell>
                        <TableCell>{q.doses}</TableCell>
                        <TableCell>
                          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
                            {canReview ? null : <Label variant="soft" color="warning">{copy(pageContract, "reason.no_sop_review_handle")}</Label>}
                            <VerificationRowActions
                              verifyAction={verifyCompletionAction}
                              rejectAction={rejectCompletionAction}
                              completionId={q.completion_id}
                              taskId={q.sop_task_id}
                              rowVersion={q.sop_task_row_version}
                              returnTo={verifyReturnTo}
                              canReview={canReview}
                              passportHref={`/goats/${q.goat_id}`}
                              labels={{
                                verify: copy(pageContract, "action.verify"),
                                reject: copy(pageContract, "action.reject"),
                                rework: copy(pageContract, "action.request_rework"),
                                passport: copy(pageContract, "action.passport"),
                                menu: copy(pageContract, "label.row_actions"),
                                noHandle: copy(pageContract, "reason.no_sop_review_handle"),
                              }}
                            />
                          </Box>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </Scrollbar>
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
          </UrlSuspense>
        </Card>
      ) : (
        // ===== Status board — kanban-style lanes from the real Action Center process-integrity rows =====
        <Stack spacing={3} data-filter-scope>
          <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<KpiRowSkeleton count={3} icon />}>
          <Grid container spacing={3}>
            {quickTiles.map((tile) => {
              const on = quickState === tile.key;
              const href = hrefWith({ state: on && tile.key !== "all" ? "all" : tile.key, ac_page: "1", ac_row: undefined });
              return (
                <Grid key={tile.key} size={{ xs: 12, sm: 4 }}>
                  <Link href={href} scroll={false} aria-pressed={on} style={{ display: "block", height: "100%", color: "inherit", textDecoration: "none" }}>
                    <CourseWidgetSummary
                      title={tile.title}
                      total={tile.total}
                      color={tile.color}
                      icon={tile.icon}
                      sx={on ? { height: 1, outline: 2, outlineColor: `${tile.color}.main` } : { height: 1 }}
                    />
                  </Link>
                </Grid>
              );
            })}
          </Grid>
          </UrlSuspense>

          {/* Template list toolbar card: severity (server-side) chips, search + My tasks + Filters,
              then the applied-filter chips (each removes exactly its own filter) with Clear all. */}
          <Card>
            <Box id="acToggles" sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
              <Box role="group" aria-label={copy(pageContract, "filter.all_severity")} sx={{ display: "flex", flexWrap: "wrap", gap: 1, flex: "1 1 auto" }}>
                {[
                  { value: "all", label: copy(pageContract, "filter.all_severity"), href: hrefWith({ severity: "all", ac_page: "1", ac_row: undefined }) },
                  ...SEVERITY_ORDER.map((s2) => ({ value: s2, label: optionLabel(severityOptions, s2), href: hrefWith({ severity: s2, ac_page: "1", ac_row: undefined }) })),
                ].map((chip) => (
                  <FilterChip key={chip.value} href={chip.href} on={severityFilter === chip.value} label={chip.label} />
                ))}
              </Box>
              <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_visible_cards")} />
              {/* Board paging note as a template info tooltip beside the visible-cards search it
                  qualifies, not a loose line of text above the lanes. Tap opens it in the webview.
                  guard: action-center-paging-tooltip */}
              {boardNextCursor ? <InfoTip title={copy(pageContract, "note.board_paging")} testId="ac-board-paging-info" /> : null}
              <ActionCenterFiltersButton
                pageContract={pageContract}
                label={copy(pageContract, "filter.my_tasks.title")}
                mode="my"
                rowsLabel={`${boardPaged.start}-${boardPaged.end} of ${boardPaged.total} rows`}
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
            </Box>
            {hasBoardFilters ? (
              <Box sx={{ px: 2.5, pb: 2.5, display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
                {stateFilter !== "all" ? (
                  <FilterChip removable href={hrefWith({ state: "all", ac_page: "1", ac_row: undefined })} label={optionLabel(workStateOptions, stateFilter)} />
                ) : null}
                {severityFilter !== "all" ? (
                  <FilterChip removable href={hrefWith({ severity: "all", ac_page: "1", ac_row: undefined })} label={optionLabel(severityOptions, severityFilter)} />
                ) : null}
                <LinkButton href={clearFiltersHref} scroll={false} color="error" startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}>
                  {copy(pageContract, "filter.clear_all")}
                </LinkButton>
              </Box>
            ) : null}
          </Card>

          {/* The lanes (guard: url-keyed-panel): a chip / filter / page click swaps them to their
              skeleton at once; the toolbar card above stays on screen. */}
          <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={BOARD_LANES_SKELETON}>
          <Stack spacing={3}>
          {totalCount === 0 ? (
            <Alert
              severity="info"
              action={
                hasBoardFilters ? (
                  <LinkButton href={clearFiltersHref} color="inherit" size="small">
                    {copy(pageContract, "filter.clear_all")}
                  </LinkButton>
                ) : (
                  <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
                    <LinkButton href="/vaccination/plan" color="inherit" size="small">
                      {copy(pageContract, "action.open_config")}
                    </LinkButton>
                    <LinkButton href="/vaccination/plan" color="inherit" size="small">
                      {copy(pageContract, "action.open_sops")}
                    </LinkButton>
                  </Box>
                )
              }
            >
              {hasBoardFilters ? copy(pageContract, "empty.work_board_filtered") : copy(pageContract, "empty.work_board_detail")}
            </Alert>
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
          </Stack>
          </UrlSuspense>
        </Stack>
      )}
      </Stack>
      </UrlSuspense>
    </Stack>
  );
}

/** Params that never change the Action Center's data: the local row drawer and the action feedback banner. */
const PANEL_IGNORE = ["ac_row", "action_status", "action_key"] as const;
const BOARD_LANES_SKELETON = (
  <StackSkeleton spacing={2}>
    <KanbanSkeleton layout="grid" lanes={[3, 3, 2, 2, 1]} minHeight={360} />
    <PagerSkeleton />
  </StackSkeleton>
);
/** Each view's skeleton ("" = the status board, the default). */
const VIEW_SKELETON = {
  "": (
    <StackSkeleton spacing={3}>
      <KpiRowSkeleton count={3} icon />
      <ToolbarSkeleton left={<ChipRowSkeleton count={4} />} fields={["search", 120, 120]} />
      {BOARD_LANES_SKELETON}
    </StackSkeleton>
  ),
  verify: <TableSkeleton columns={4} rows={10} toolbar={<ToolbarSkeleton fields={["search", 120]} sx={{ p: 2.5 }} />} />,
};
