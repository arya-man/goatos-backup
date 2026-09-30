import Box from "@mui/material/Box";
import { FOUR_LANE_COLUMN_WIDTH } from "@/components/app/kanban/board-layout";
import { TASK_BOARD_SKELETON_LANES } from "./tasks-layout";
import Card from "@mui/material/Card";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";

import Link from "@/components/no-prefetch-link";
import type { SegmentedOption } from "@/components/segmented-links";
import { WorklistPager } from "@/components/worklist-pager";
import { EmptyContent } from "@/components/minimal/empty-content";
import {
  copy,
  table,
  tablePageSizes,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import {
  hrefPreviousPagedCursor,
  hrefWithPagedCursor,
  type RouteSearchParams,
} from "@/lib/search-params";
import type { LeadershipTaskAssignee, LeadershipTaskPage } from "@/lib/api/server";

import { raiseLeadershipTaskAction } from "./actions";
import { LeadershipTasksBoard } from "./leadership-tasks-board";
import { LeadershipTasksFilters, type TaskStatusChip } from "./leadership-tasks-filters";
import { LeadershipTasksTableLazy as LeadershipTasksTable } from "./leadership-tasks-table-lazy";
import { PageHeader } from "@/components/app/page-header";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { KanbanSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { RetryButton } from "@/components/app/retry-button";
import { NewTaskModal } from "./new-task-modal";
import {
  hasTaskFilters,
  hasTaskNarrowing,
  parseTasksParams,
  TASK_PARAM,
  tasksAliasFixedHref,
  tasksClearedHref,
  tasksHref,
  tasksSearchParams,
} from "./params";
import { personOptions, rowFromTask, rowsFromPage, type TaskRow } from "./task-row";
import { TaskDrawerHost } from "./task-drawer-host";
import { TaskViewBody, TaskViewProvider, TaskViewToggle } from "./task-view-switch";
import { TaskFeedbackBanner } from "./task-feedback-banner";
import { TASK_VIEW_ALIAS, TASK_VIEWS, TASKS_PATHNAME } from "./task-url";
import { withCancelledRows } from "./task-detail-pick";

const TABLE_ID = "leadership-task-progress";

/**
 * The Tasks desk: one backend-filtered, keyset-paged worklist with a Jira-shaped toolbar.
 *
 * WHAT IS AND IS NOT OWNED HERE
 * Rows, whole-list chip counts, the status vocabulary, the deadline countdown and its colour are
 * all the backend's and are rendered verbatim. This component owns the URL state (the filter
 * parameters, the sort, the page size and the cursor STACK) and nothing else. There is no
 * `total_count` and no `offset` anywhere in this feature: OFFSET pagination is blocked by
 * `make scale-guard`, so page numbers come from the cursor stack in `lib/search-params.ts` and a
 * "1-25 of N" range is simply not a thing this endpoint can honestly say.
 *
 * The top-bar scope (park / as_of / range / scope_mode / date_from / date_to) is NEVER read here.
 * It belongs to the shell; `scripts/check-ia-guard.mjs` is explicit about it.
 */
export function LeadershipTasksPage({
  page,
  pageContract,
  searchParams,
  selectedTask,
  assignees = [],
  cancelledRows = [],
}: {
  page?: LeadershipTaskPage | null;
  pageContract: AdminUiPageContract;
  searchParams?: RouteSearchParams;
  /** The selected task from the DETAIL read (full notes + activity), fetched by the route in
   *  parallel with the list when `task=` is in the URL. The list rows carry no activity, so
   *  the drawer is fed from here; a task not on the current page still opens. */
  selectedTask?: LeadershipTaskPage["rows"][number] | null;
  assignees?: LeadershipTaskAssignee[];
  /** The first page of the list's `cancelled` filter, read beside the board's page (route). */
  cancelledRows?: LeadershipTaskPage["rows"];
}) {
  const sp = searchParams ?? {};
  const tableContract = table(pageContract, TABLE_ID);
  const pageSizeOptions = tablePageSizes(pageContract, TABLE_ID);
  const params = parseTasksParams(sp, pageSizeOptions);
  const basePath = TASKS_PATHNAME;

  const tasks = page ? rowsFromPage(page) : [];
  // A failed list read still gets the scope strip: the three scopes are a fixed vocabulary
  // (task-url TASK_SCOPES), so the reader can switch to a scope that loads instead of staring at
  // a dead toolbar. Labels come from the contract; counts are unknown, so none are shown.
  const scopes = page?.scopes?.length
    ? page.scopes
    : fixtureScopes.map((scope) => ({ ...scope, label: copy(pageContract, `scope.${scope.key}`, scope.label), count: undefined as unknown as number, total: undefined }));
  const readFailed = !page;
  const selectedTaskID = params.selectedTaskID;
  const selected = selectedTaskID
    ? selectedTask && selectedTask.task_id === selectedTaskID
      ? rowFromTask(selectedTask)
      : tasks.find((task) => task.id === selectedTaskID)
    : undefined;
  const hasTasks = tasks.length > 0;
  // The board's Cancelled column is filled from the server's own cancelled list; the table keeps
  // the page it was asked for.
  const boardTasks = withCancelledRows(tasks, cancelledRows.map(rowFromTask));
  const hasBoardTasks = boardTasks.length > 0;

  /**
   * The scope this screen is showing.
   *
   * Every read of it is OPTIONAL below. It was dereferenced unguarded in two places while the
   * fallback chain that produces it can genuinely end in `undefined` — an empty `scopes[]`, which
   * is exactly what an unavailable contract or a principal with no scopes yields — and the page
   * crashed on render instead of showing its own unavailable state.
   */
  const selectedScope =
    scopes.find((scope) => scope.key === params.scope) ??
    scopes.find((scope) => scope.selected) ??
    scopes[0];
  const scopeKey = selectedScope?.key ?? params.scope;

  /**
   * THE NUMBERS ON THIS SCREEN RECONCILE (Gate-1 #2, #4).
   *
   * `scopes[].count` is the backend's count under the request's filters; `scopes[].total` is the
   * tab's size with none applied. The tabs show the TOTAL: a search for "zzzz" used to collapse
   * them to "Raised by me (0) · Team progress (0)" and the desk read as empty on top of 408
   * tasks. The card header shows what the list is narrowed to against that total ("142 of 408"
   * under the Overdue chip, "0 of 408" under a search with no hits), and reads the plain total
   * when nothing narrows it. `shownCount` is the selected status chip's count when one is
   * active (that is the number the chip itself shows, so the two agree by construction) and
   * the scope's filtered count otherwise.
   */
  const scopeTotal = (scope: (typeof scopes)[number]) => scope.total ?? scope.count;
  const narrowed = hasTaskNarrowing(params);
  const selectedChip = (page?.filters ?? []).find((filter) => filter.key === params.filter);
  const shownCount =
    params.filter !== "all" && selectedChip ? selectedChip.count : selectedScope?.count;
  const headerCount = !selectedScope
    ? copy(pageContract, "label.placeholder")
    : !narrowed || shownCount === undefined || shownCount === scopeTotal(selectedScope)
      ? `${scopeTotal(selectedScope)}`
      : copy(pageContract, "count.of", "{shown} of {total}")
          .replace("{shown}", `${shownCount}`)
          .replace("{total}", `${scopeTotal(selectedScope)}`);

  const scopeHref = (key: string) =>
    tasksHref(basePath, sp, { [TASK_PARAM.scope]: key, [TASK_PARAM.task]: null }, { resetPaging: true });

  /**
   * Board / List, Jira's own pair.
   *
   * A view change is presentation ONLY: it keeps the scope, every filter, the sort, the page size,
   * the cursor stack and the selected task, because both views render the SAME list read. That is
   * also why `view` is not in `TASK_FILTER_PARAMS` — switching it must not restart paging.
   */
  const viewOptions: SegmentedOption[] = TASK_VIEWS.map((view) => ({
    value: view,
    label: copy(pageContract, `board.view.${view}`, view === "board" ? "Board" : "List"),
    // The unprefixed `view` alias is DROPPED by the toggle: it is honoured on read, but a URL
    // carrying both `view=list` and `t_view=board` is one where the reader cannot tell which won.
    href: tasksHref(basePath, sp, { [TASK_PARAM.view]: view, [TASK_VIEW_ALIAS]: null }, { resetPaging: false }),
  }));

  const statusChips: TaskStatusChip[] = (page?.filters ?? []).map((filter) => ({
    key: filter.key,
    label: filter.label,
    count: filter.count,
    selected: filter.key === params.filter,
  }));

  const nextCursor = page?.next_cursor ?? null;
  const nextHref = hrefWithPagedCursor(
    basePath,
    sp,
    TASK_PARAM.cursor,
    nextCursor,
    TASK_PARAM.page,
    TASK_PARAM.cursorStack,
  );
  const prevHref = hrefPreviousPagedCursor(
    basePath,
    sp,
    TASK_PARAM.cursor,
    TASK_PARAM.page,
    TASK_PARAM.cursorStack,
  );
  // The pager speaks offsets; this list has none. The number is a DISPLAY position derived from
  // the cursor stack's depth, and the only two offsets the pager ever asks for are "one page
  // forward" and "one page back", which map onto the two keyset links. Nothing here is sent to
  // the backend.
  const displayOffset = (params.page - 1) * params.limit;
  const hrefForOffset = (offset: number) =>
    (offset >= displayOffset + params.limit ? nextHref : prevHref) ?? basePath;
  const hrefForLimit = (limit: number) =>
    tasksHref(basePath, sp, { [TASK_PARAM.limit]: String(limit) }, { resetPaging: true });

  const assigneeChoices = personOptions(assignees);
  /**
   * The detail panel exists ONLY when there is a task to show, and then it is a DRAWER over the
   * board or the table (`task-detail-drawer.tsx`), never a rail beside them.
   *
   * There is no "Select a task" placeholder rail any more, in either view. The table kept one
   * (Gate-1 #1): with nothing selected the empty rail took 54% of `.lt-grid` at 1440, the table
   * was squeezed into a 475px scroll box with no scrollbar, and four of its six columns (Raised
   * by, Status, Evidence, Days left) were simply off-screen. The drawer IS the detail now, so an
   * empty rail was dead space bought with the columns the list exists to show. Both views take
   * the full width (`lt-grid-solo`). `TaskDetailPanel` stays the single call site; the drawer
   * merely wraps it. Same href as the panel's own Close link.
   */
  const closeHref = tasksHref(
    TASKS_PATHNAME,
    tasksSearchParams(params),
    { [TASK_PARAM.task]: null },
    { resetPaging: false },
  );
  // The drawer is client-local (task-drawer-host.tsx): it opens from the row on screen with no
  // navigation, fetches the feed inside, and closes through history. A deep link hands it the
  // detail row from the server so the first paint already carries the feed.
  const detailPanel = (
    <TaskDrawerHost
      rows={boardTasks}
      initialDetail={selectedTask && selected ? selected : null}
      pageContract={pageContract}
      scopeKey={scopeKey}
      params={params}
      assignees={assignees}
      canRaise={Boolean(page?.can_raise)}
      ariaLabel={copy(pageContract, "section.selected.title")}
      closeLabel={copy(pageContract, "action.close")}
      closeHref={closeHref}
    />
  );

  /**
   * The empty body, ONE for both views. The board used to render nothing at all with zero rows,
   * so a search with no hits on the board read as a blank page; the "No tasks match. Clear the
   * filters" state lived only in the list (2026-09-25).
   */
  const emptyState =
    narrowed && selectedScope && scopeTotal(selectedScope) > 0 ? (
      /* NO HITS IS NOT AN EMPTY QUEUE (Gate-1 #2): the scope holds tasks and the reader's
         search / chip / person / dates hid every one of them, so say that and offer the way
         back. The scope's own empty_message is for a scope that is truly empty. */
      <EmptyContent
        data-lt-empty="filtered"
        title={copy(pageContract, "empty.filtered", "No tasks match.")}
        description={copy(pageContract, "empty.filtered_rest", "to see all {count}.").replace("{count}", `${scopeTotal(selectedScope)}`)}
        action={
          <Button component={Link} href={tasksClearedHref(basePath, sp)} variant="soft" sx={{ mt: 2 }}>
            {copy(pageContract, "empty.filtered_action", "Clear the filters")}
          </Button>
        }
        sx={{ py: 8 }}
      />
    ) : (
      <EmptyContent
        title={selectedScope?.empty_message || copy(pageContract, "empty.tasks")}
        description={copy(pageContract, "empty.tasks_detail")}
        sx={{ py: 8 }}
      />
    );

  const pager = (
    <WorklistPager
      pageContract={pageContract}
      offset={displayOffset}
      limit={params.limit}
      rowCount={tasks.length}
      hasMore={Boolean(nextHref)}
      noun={copy(pageContract, "table.tasks.noun")}
      pageSizeOptions={pageSizeOptions}
      hrefForOffset={hrefForOffset}
      hrefForLimit={hrefForLimit}
    />
  );

  const filters = (
    <LeadershipTasksFilters
      pageContract={pageContract}
      basePath={basePath}
      q={params.rawQ}
      assignee={params.assigneeUserID ?? ""}
      raiser={params.raisedBy ?? ""}
      assigneeOptions={assigneeChoices}
      raiserOptions={assigneeChoices}
      assigneePinned={scopeKey === "assigned_to_me"}
      raiserPinned={scopeKey === "assigned_by_me"}
      deadlineFrom={params.deadline.from ?? ""}
      deadlineTo={params.deadline.to ?? ""}
      raisedFrom={params.raised.from ?? ""}
      raisedTo={params.raised.to ?? ""}
      rangeIncomplete={params.deadline.incomplete || params.raised.incomplete}
      sort={params.sort}
      statusChips={statusChips}
      rows={tasks}
      hasFilters={hasTaskFilters(params)}
      clearedHref={tasksClearedHref(basePath, sp)}
      scope={
        scopes.length
          ? {
              label: copy(pageContract, "scope.aria"),
              value: scopeKey,
              options: scopes.map((scope) => {
                const total = readFailed ? undefined : scopeTotal(scope);
                return { value: scope.key, label: total == null ? scope.label : `${scope.label} (${total.toLocaleString("en-IN")})`, href: scopeHref(scope.key) };
              }),
            }
          : undefined
      }
      assigneeCounts={countBy(tasks, (row) => row.assigneeUserID)}
      raiserCounts={countBy(tasks, (row) => row.raisedByUserID)}
    />
  );

  // Template list page (sections/user/view/user-list-view.tsx): CustomBreadcrumbs with the add
  // action, the page-level scope Tabs (sections/account/account-layout.tsx) with the file-manager
  // view switch at their end, then ONE Card: status Tabs with Label counts, toolbar, applied
  // filters and -- in the list view -- the table and its pager. The board view keeps the same card
  // head and lays the template kanban columns under it on the page ground.
  return (
    <TaskViewProvider initial={params.view}>
    {/* The page gap under the header (template CustomBreadcrumbs mb): the scope tabs sat 0px under the
        breadcrumbs. guard: rhythm|header-gap */}
    <Box sx={{ minWidth: 0, "& > [data-page-header]": { mb: 3 } }}>
      <PageHeader
        title={page?.title || pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        // One strip of tabs only (TR1-#41): the scope is the card toolbar's first select, and the
        // board / list switch sits with the page action (template file-manager view toggle).
        actions={
          <>
            <TaskViewToggle options={viewOptions} ariaLabel={copy(pageContract, "board.view.aria", "Task view")} />
            {page?.can_raise || readFailed ? (
              <NewTaskModal
                assignees={assignees}
                action={raiseLeadershipTaskAction}
                returnTo={`${TASKS_PATHNAME}?scope=assigned_by_me`}
                pageContract={pageContract}
              />
            ) : null}
          </>
        }
      />

      {params.feedbackStatus ? (
        <TaskFeedbackBanner
          pageContract={pageContract}
          status={params.feedbackStatus}
          code={params.feedbackCode}
          statusNow={params.feedbackStatusNow}
          who={params.feedbackWho}
        />
      ) : null}

      {/* AN IGNORED PARAMETER, SAID OUT LOUD. `view` is read as an alias of `t_view`; the
          remaining unprefixed names (`q`, `sort`, `limit`, `page`, ...) are other screens'
          generic parameters and stay unread, so the page names what it did not read and offers
          the corrected link. */}
      {params.ignoredAliases.length ? (
        <Alert
          severity="info"
          role="status"
          sx={{ mb: 3 }}
          action={
            <Button component={Link} href={tasksAliasFixedHref(basePath, sp, params.ignoredAliases)} color="inherit" size="small">
              {copy(pageContract, "action.fix_link", "Use the link this page reads")}
            </Button>
          }
        >
          {copy(
            pageContract,
            "state.ignored_params",
            "This link sets a filter this page does not read. Tasks names its own filters with a t_ prefix.",
          )}{" "}
          <Box component="code" sx={{ typography: "caption", fontFamily: "monospace" }}>
            {params.ignoredAliases.map((hint) => `${hint.alias} → ${hint.param}`).join(", ")}
          </Box>
        </Alert>
      ) : null}

      {/* The list READ failed (nothing to show in either view): one proper error state with a
          retry. The scope select in the card toolbar keeps the other scopes one click away. */}
      {readFailed ? (
        <Alert severity="error" role="alert" sx={{ mb: 3 }} action={<RetryButton label={copy(pageContract, "action.retry", "Retry")} />}>
          {copy(pageContract, "state.unavailable_tasks")}
        </Alert>
      ) : null}

      <TaskViewBody
        board={
          <>
            <Card sx={{ mb: 3 }}>{filters}</Card>
            <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={TASK_PANEL_IGNORE} fallback={<KanbanSkeleton lanes={TASK_BOARD_SKELETON_LANES} laneWidth={FOUR_LANE_COLUMN_WIDTH} />}>
            {hasBoardTasks ? (
              <>
                <LeadershipTasksBoard
                  pageContract={pageContract}
                  rows={boardTasks}
                  filters={page?.filters ?? []}
                  basePath={basePath}
                  sp={sp}
                  scopeKey={scopeKey}
                  activeFilter={params.filter}
                  selectedTaskID={selected?.id}
                />
                {hasTasks ? <Card sx={{ mt: 1 }}>{pager}</Card> : null}
              </>
            ) : (
              <Card data-testid="lt-board-empty">{emptyState}</Card>
            )}
            </UrlSuspense>
          </>
        }
        list={
          <Card sx={{ minWidth: 0 }} aria-label={selectedScope?.label || tableContract.title} data-count={headerCount}>
            {filters}
            <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={TASK_PANEL_IGNORE} fallback={<TableSkeleton bare columns={tableContract.columns.length || 6} rows={params.limit} header={false} />}>
            {hasTasks ? (
              <>
                <LeadershipTasksTable
                  pageContract={pageContract}
                  contract={tableContract}
                  rows={tasks}
                  basePath={basePath}
                  sp={sp}
                  scopeKey={scopeKey}
                  selectedTaskID={selected?.id}
                />
                {pager}
              </>
            ) : (
              emptyState
            )}
            </UrlSuspense>
          </Card>
        }
      />
      {detailPanel}
    </Box>
    </TaskViewProvider>
  );
}



// ---------------------------------------------------------------- scope fallback
// The fixed scope vocabulary shown when the list read fails (labels re-read from the contract,
// counts dropped above).
const fixtureScopes: NonNullable<LeadershipTaskPage["scopes"]> = [
  {
    key: "assigned_to_me",
    label: "Assigned to me",
    count: 2,
    selected: false,
    empty_message: "No tasks assigned to you.",
  },
  {
    key: "assigned_by_me",
    label: "Assigned by me",
    count: 7,
    selected: false,
    empty_message: "You have not raised any tasks.",
  },
  {
    key: "team_progress",
    label: "Team progress",
    count: 18,
    selected: true,
    empty_message: "No tasks in this scope.",
  },
];

/** Cards on this page per person id, for the picker's per-person count. */
function countBy(items: readonly TaskRow[], key: (item: TaskRow) => string): Record<string, number> {
  const out: Record<string, number> = {};
  for (const item of items) {
    const k = key(item);
    if (k) out[k] = (out[k] ?? 0) + 1;
  }
  return out;
}

/** Params that never change the task panels: the drawer's task and the client-local Board/List view. */
const TASK_PANEL_IGNORE = ["task", "t_view", TASK_VIEW_ALIAS] as const;
