import { ClipboardList } from "lucide-react";

import { SegmentedLinks, type SegmentedOption } from "@/components/segmented-links";
import { Tag } from "@/components/ui-primitives";
import { WorklistPager } from "@/components/worklist-pager";
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
import { LeadershipTasksTable } from "./leadership-tasks-table";
import { NewTaskModal } from "./new-task-modal";
import {
  hasTaskFilters,
  parseTasksParams,
  TASK_PARAM,
  tasksAliasFixedHref,
  tasksClearedHref,
  tasksHref,
} from "./params";
import { personOptions, rowsFromPage, type TaskRow } from "./task-row";
import { TaskDetailPanel } from "./task-detail-panel";
import { TaskFeedbackBanner } from "./task-feedback-banner";
import { TASK_VIEW_ALIAS, TASK_VIEWS, TASKS_PATHNAME, TASKS_PREVIEW_PATHNAME } from "./task-url";

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
  preview = false,
  selectedTaskID: selectedTaskIDProp,
  assignees = [],
}: {
  page?: LeadershipTaskPage | null;
  pageContract: AdminUiPageContract;
  searchParams?: RouteSearchParams;
  preview?: boolean;
  /** Only the fixture host passes this; the live page reads it off the URL. */
  selectedTaskID?: string;
  assignees?: LeadershipTaskAssignee[];
}) {
  const sp = searchParams ?? {};
  const tableContract = table(pageContract, TABLE_ID);
  const pageSizeOptions = tablePageSizes(pageContract, TABLE_ID);
  const params = parseTasksParams(sp, pageSizeOptions);
  const basePath = preview ? TASKS_PREVIEW_PATHNAME : TASKS_PATHNAME;

  const tasks = page ? rowsFromPage(page) : preview ? fixtureTasks : [];
  const scopes = page?.scopes?.length ? page.scopes : preview ? fixtureScopes : [];
  const selectedTaskID = selectedTaskIDProp ?? params.selectedTaskID;
  const selected = selectedTaskID ? tasks.find((task) => task.id === selectedTaskID) : undefined;
  const hasTasks = tasks.length > 0;

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

  const scopeOptions: SegmentedOption[] = scopes.map((scope) => ({
    value: scope.key,
    label: `${scope.label} (${scope.count})`,
    // A scope change restarts paging and drops the selected task: a cursor and a row id from one
    // scope mean nothing in another.
    href: tasksHref(
      basePath,
      sp,
      { [TASK_PARAM.scope]: scope.key, [TASK_PARAM.task]: null },
      { resetPaging: true },
    ),
  }));

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
  const isBoard = params.view === "board";
  /**
   * The detail rail takes a column of the page ONLY when it has a task to show, or when the table
   * is on screen.
   *
   * The board needs the width: four columns beside a 360px rail left two of them off-screen at
   * 1440, so a reader comparing this with Jira saw half a board next to an empty placeholder
   * card. The table has always reserved the rail (its rows are narrow and the reader expects the
   * panel to be there), so that behaviour is unchanged.
   */
  const hasSidePanel = Boolean(selected) || (!isBoard && hasTasks);

  return (
    <div className="screen on lt-page">
      <div className="phead lt-phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{pageContract.title}</b>
          </div>
          <h1>{page?.title || pageContract.title}</h1>
          <div className="sub">
            {preview ? copy(pageContract, "state.preview") : pageContract.subtitle}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {page?.can_raise ? (
          <NewTaskModal
            assignees={assignees}
            action={raiseLeadershipTaskAction}
            returnTo={`${TASKS_PATHNAME}?scope=assigned_by_me`}
          />
        ) : preview ? (
          <Tag tone="ok">{copy(pageContract, "state.can_raise")}</Tag>
        ) : null}
      </div>

      {params.feedbackStatus ? (
        <TaskFeedbackBanner
          pageContract={pageContract}
          status={params.feedbackStatus}
          code={params.feedbackCode}
          statusNow={params.feedbackStatusNow}
          who={params.feedbackWho}
        />
      ) : null}

      {/* AN IGNORED PARAMETER, SAID OUT LOUD.
          Silently defaulting on a parameter that LOOKS like one of ours is what handed the
          maintainer a board they thought was a list (`?view=list`, B5). `view` is now read as an
          alias of `t_view` (`resolveTaskView`); the remaining unprefixed names (`q`, `sort`,
          `limit`, `page`, ...) are other screens' generic parameters and stay unread, so the page
          names what it did not read and offers the corrected link. */}
      {params.ignoredAliases.length ? (
        <div className="lt-aliasnote" role="status">
          <span>
            {copy(
              pageContract,
              "state.ignored_params",
              "This link sets a filter this page does not read. Tasks names its own filters with a t_ prefix.",
            )}
          </span>
          <code>
            {params.ignoredAliases.map((hint) => `${hint.alias} → ${hint.param}`).join(", ")}
          </code>
          <a className="achip" href={tasksAliasFixedHref(basePath, sp, params.ignoredAliases)}>
            {copy(pageContract, "action.fix_link", "Use the link this page reads")}
          </a>
        </div>
      ) : null}

      {/* ONE STICKY GROUP: the scope tabs and the filter bar pin together. The filter bar alone
          was `position:sticky`, so after the slightest scroll the page's primary navigation (For
          me / Raised by me / Team progress, and the Board / List pair) slid up behind the toolbar
          and stayed there -- both of the maintainer's 2000px screenshots show the tabs half
          clipped. `.lt-fsticky` owns the stickiness now and the bar inside it is static. */}
      <div className="lt-fsticky">
      <div className="lt-scopebar">
        {scopeOptions.length ? (
          <SegmentedLinks
            options={scopeOptions}
            current={scopeKey}
            ariaLabel={copy(pageContract, "scope.aria")}
          />
        ) : (
          <div className="lt-unavailable">{copy(pageContract, "state.unavailable_tasks")}</div>
        )}
        <div className="ltb-viewswitch">
          <SegmentedLinks
            options={viewOptions}
            current={params.view}
            ariaLabel={copy(pageContract, "board.view.aria", "Task view")}
          />
        </div>
      </div>

      <LeadershipTasksFilters
        pageContract={pageContract}
        basePath={basePath}
        q={params.rawQ}
        assignee={params.assigneeUserID ?? ""}
        raiser={params.raisedBy ?? ""}
        assigneeOptions={assigneeChoices}
        raiserOptions={assigneeChoices}
        // The scope already pins one side of the pair; the backend ignores the parameter rather
        // than erroring, and an inert control says so instead of pretending to narrow.
        assigneePinned={scopeKey === "assigned_to_me"}
        raiserPinned={scopeKey === "assigned_by_me"}
        deadlineFrom={params.deadline.from ?? ""}
        deadlineTo={params.deadline.to ?? ""}
        raisedFrom={params.raised.from ?? ""}
        raisedTo={params.raised.to ?? ""}
        rangeIncomplete={params.deadline.incomplete || params.raised.incomplete}
        sort={params.sort}
        statusChips={statusChips}
        hasFilters={hasTaskFilters(params)}
        clearedHref={tasksClearedHref(basePath, sp)}
      />
      </div>

      <div className={`lt-grid${hasSidePanel ? "" : " lt-grid-solo"}`}>
        <section className="card lt-card" style={{ minWidth: 0 }}>
          <div className="hd">
            <ClipboardList className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>{selectedScope?.label || tableContract.title}</h3>
            <div className="sp" style={{ flex: 1 }} />
            <Tag tone="info">{selectedScope ? `${selectedScope.count}` : "—"}</Tag>
          </div>
          {hasTasks && isBoard ? (
            <div className="bd ltb-bd">
              <LeadershipTasksBoard
                pageContract={pageContract}
                rows={tasks}
                filters={page?.filters ?? []}
                basePath={basePath}
                sp={sp}
                scopeKey={scopeKey}
                activeFilter={params.filter}
                selectedTaskID={selected?.id}
              />
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
            </div>
          ) : hasTasks ? (
            <div className="bd lt-tablewrap">
              <LeadershipTasksTable
                pageContract={pageContract}
                contract={tableContract}
                rows={tasks}
                basePath={basePath}
                sp={sp}
                scopeKey={scopeKey}
                selectedTaskID={selected?.id}
              />
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
            </div>
          ) : (
            <div className="bd lt-empty-state">
              <ClipboardList className="ic" aria-hidden="true" />
              <div>
                <b>
                  {selectedScope?.empty_message ||
                    copy(pageContract, "empty.tasks")}
                </b>
                <p>{copy(pageContract, "empty.tasks_detail")}</p>
              </div>
            </div>
          )}
        </section>

        {/* The detail rail is one call site on purpose: `task-detail-panel.tsx` owns the whole
            panel, including the no-selection state, so the panel can be redesigned without the
            page changing. `detail` is the selected `TaskRow` or `null`. */}
        {hasSidePanel ? (
          <TaskDetailPanel
            detail={selected ?? null}
            pageContract={pageContract}
            scopeKey={scopeKey}
            params={params}
            assignees={assignees}
            canRaise={Boolean(page?.can_raise)}
          />
        ) : null}
      </div>
    </div>
  );
}



// ---------------------------------------------------------------- fixture rows for /tasks-preview
const fixtureTasks: TaskRow[] = [
  {
    id: "11111111-1111-4111-8111-111111111111",
    number: "#18",
    title: "Check CPT west fence repair before evening close",
    body: "Confirm the west fence patch before close and attach the completion proof.",
    comment: "Park team acknowledged.",
    canComment: true,
    canEdit: true,
    rowVersion: 4,
    status: "in_progress",
    statusLabel: "Doing",
    statusOptions: [{ key: "done", label: "Done" }],
    assignee: "Satish",
    assigneeRole: "Park Head",
    raisedBy: "Manju",
    raisedByUserID: "22222222-2222-4222-8222-222222222222",
    assigneeUserID: "33333333-3333-4333-8333-333333333333",
    age: "Today",
    attachments: 3,
    evidence: "video, voice note",
    attachmentKinds: ["video", "audio"],
    attachmentRows: [],
    notes: [],
    daysLeft: 4,
    daysLeftLabel: "4 days left",
    deadlineTone: "ok",
    deadlineLabel: "18/09/2026 17:00",
    deadlineStateLabel: "Due in 4 days",
    deadlineAt: "2026-09-18T17:00:00+05:30",
  },
  {
    id: "44444444-4444-4444-8444-444444444444",
    number: "#17",
    title: "Confirm director handoff for feed unloading delay",
    body: "Capture what delayed unloading and who owns the next checkpoint.",
    comment: "Waiting for vendor note.",
    canComment: true,
    canEdit: false,
    rowVersion: 2,
    status: "open",
    statusLabel: "Open",
    statusOptions: [{ key: "in_progress", label: "Doing" }],
    assignee: "Manohar",
    assigneeRole: "Feed Director",
    raisedBy: "Ravi",
    raisedByUserID: "55555555-5555-4555-8555-555555555555",
    assigneeUserID: "66666666-6666-4666-8666-666666666666",
    age: "Today",
    attachments: 2,
    evidence: "note, file",
    attachmentKinds: ["file"],
    attachmentRows: [],
    notes: [],
    daysLeft: -4,
    daysLeftLabel: "4 days over",
    deadlineTone: "over",
    deadlineLabel: "10/09/2026 12:00",
    deadlineStateLabel: "Overdue by 4 days",
    deadlineAt: "2026-09-10T12:00:00+05:30",
  },
];

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
