"use client";

import Link from "@/components/no-prefetch-link";
import { ClipboardList, Paperclip } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadershipTaskAssignee } from "@/lib/api/server";

import {
  changeLeadershipTaskStatusAction,
  editLeadershipTaskAction,
  postLeadershipTaskCommentAction,
} from "./actions";
import { DeadlineClock } from "./deadline-clock";
import { EditTaskModal } from "./edit-task-modal";
// `task-presentation.ts`, not the `"use client"` table: this panel is server-rendered and a
// function exported from a client module cannot be called here.
import { initials, statusTone } from "./task-presentation";
import { TASK_PARAM, tasksHref, tasksSearchParams, type TasksParams } from "./params";
import type { TaskRow } from "./task-row";
import { TASKS_PATHNAME } from "./task-url";
import { TaskActivityComposer } from "./task-activity-composer";
import { TaskStatusMenu } from "./task-status-menu";

/**
 * The task detail panel, shaped like a modern Jira ISSUE VIEW and carrying only this product's
 * own data.
 *
 * WHAT IS BORROWED FROM JIRA AND WHAT IS NOT
 * Borrowed: the two-column issue view — breadcrumb + issue key, a large plain-text title, a
 * prominent status control at the top, then quiet labelled sections (Description, Attachments,
 * Activity with the composer last) down the left, and a boxed, collapsible Details panel of
 * label/value rows down the right. Spacing, typography and information order are the whole
 * borrowing.
 *
 * NOT borrowed: the feature set. There are no sprints, no epics, no story points, no backlog, no
 * linked work items, no parent issue and no labels in this product, so none are rendered. There
 * is also NO priority field — the deadline clock (`deadline-clock.tsx`, tone and wording both
 * composed backend-side) is this product's only urgency signal, and it stands where Jira would
 * put priority.
 *
 * Every write is reused verbatim from `task-write-forms.tsx` and `edit-task-modal.tsx`, which
 * already mint their idempotency keys in the browser and carry the row_version fence. Nothing
 * here re-implements a write, and nothing here decides a status vocabulary, a countdown or a
 * colour: those are the backend's.
 *
 * COLOUR is the theme's: `var(--brand|ok|warn|danger|info|purple)` and the panel/ink/muted/line
 * tokens, composed with `color-mix()`. Jira contributes layout, never palette.
 */
export function TaskDetailPanel({
  detail,
  pageContract,
  scopeKey,
  params,
  assignees = [],
  canRaise = false,
  onClose,
  loadingDetail = false,
  detailLoaded = !loadingDetail,
}: {
  /** The selected row, or nothing at all when the reader has not picked one yet. */
  detail?: TaskRow | null;
  pageContract: AdminUiPageContract;
  scopeKey: string;
  params: TasksParams;
  assignees?: readonly LeadershipTaskAssignee[];
  canRaise?: boolean;
  /** Set by the drawer host: Close pops the local overlay instead of navigating. */
  onClose?: () => void;
  /** True while the host is fetching the task's notes and activity. */
  loadingDetail?: boolean;
  /** The row carries its notes and activity (a deep link, or the drawer's own read landed). */
  detailLoaded?: boolean;
}) {
  // The whole URL, rebuilt from the PARSED state: this component is handed `TasksParams` and no
  // raw search params, and `tasksSearchParams` is the seam that keeps the repeated cursor stack
  // (i.e. the reader's back-paging depth) alive in every link minted below.
  const sp = tasksSearchParams(params);

  if (!detail) {
    return (
      <aside className="ltd-panel ltd-panel-empty" aria-label={copy(pageContract, "section.selected.title")}>
        <div className="ltd-blank">
          <span className="ltd-blank-badge" aria-hidden="true">
            <ClipboardList className="ic" />
          </span>
          <b>{copy(pageContract, "empty.selected")}</b>
          <p>{copy(pageContract, "empty.selected_detail")}</p>
          {canRaise ? (
            <p className="ltd-blank-hint">
              {copy(
                pageContract,
                "empty.selected_raise",
                "Or raise a new task from the button above the list.",
              )}
            </p>
          ) : null}
        </div>
      </aside>
    );
  }

  // The write forms come back to THIS scope and THIS task on the live path only: `safeTaskReturnTo`
  // in `actions.ts` rejects anything that is not `/tasks` exactly, so a preview host round-trips
  // to the real desk rather than to itself.
  const returnTo = tasksHref(
    TASKS_PATHNAME,
    sp,
    { [TASK_PARAM.scope]: scopeKey, [TASK_PARAM.task]: detail.id },
    { resetPaging: false },
  );
  const closeHref = tasksHref(TASKS_PATHNAME, sp, { [TASK_PARAM.task]: null }, { resetPaging: false });

  const deadlineWord = copy(pageContract, "label.deadline");
  const dash = copy(pageContract, "label.placeholder");
  const attachmentRows = detail.attachmentRows;

  return (
    <aside className="ltd-panel" aria-label={copy(pageContract, "section.selected.title")}>
      {/* Jira's issue header: the breadcrumb + issue key line, with the per-issue actions kept to
          the two this product actually has — edit (raiser only, open/in_progress only, offered
          only when the ROW says so) and close. */}
      <div className="ltd-top">
        <nav className="ltd-crumb" aria-label={copy(pageContract, "crumb")}>
          <span>{copy(pageContract, "crumb")}</span>
          <span className="ltd-crumb-sep" aria-hidden="true">
            /
          </span>
          <span>{pageContract.title}</span>
          <span className="ltd-crumb-sep" aria-hidden="true">
            /
          </span>
          <b className="ltd-key">{detail.number}</b>
        </nav>
        <div className="ltd-top-actions">
          {detail.canEdit ? (
            // `display: contents` so the anchor adds no box to the flex row; it exists so a
            // test can find THIS panel's Edit opener without matching the list's own.
            <span data-testid="ltd-edit" style={{ display: "contents" }}>
              <EditTaskModal
                task={detail}
                pageContract={pageContract}
                action={editLeadershipTaskAction}
                returnTo={returnTo}
              />
            </span>
          ) : null}
          {onClose ? (
            <button type="button" className="btn sm ltd-close" onClick={onClose}>
              {copy(pageContract, "action.close")}
            </button>
          ) : (
            <Link href={closeHref} scroll={false} className="btn sm ltd-close">
              {copy(pageContract, "action.close")}
            </Link>
          )}
        </div>
      </div>

      <div className="ltd-head">
        <h2 className="ltd-title">{detail.title}</h2>
        {/* THE status control is ONE dropdown (`task-status-menu.tsx`): the pill names the current
            status in the backend's words and opens the Work Board's menu of the backend's own
            `status_options`, Cancel task last. With no options (a reader who may not move this
            task, or a cancelled one) the pill is a plain badge: no caret, nothing dead behind it.
            `data-ltd-status` names which of the two it is for tests. */}
        <div className="ltd-statusrow">
          <span className="ltd-statuslab">{copy(pageContract, "label.status", "Status")}</span>
          {detail.statusOptions.length ? (
            <TaskStatusMenu
              task={detail}
              pageContract={pageContract}
              action={changeLeadershipTaskStatusAction}
              returnTo={returnTo}
            />
          ) : (
            <span className={`ltd-status ltd-status-${statusTone(detail.status)}`} data-ltd-status="badge">
              {detail.statusLabel}
            </span>
          )}
        </div>
        {detail.canEdit ? null : (
          <p className="ltd-quiet ltd-readonly" data-testid="ltd-read-only">
            {detail.status === "done" || detail.status === "cancelled"
              ? copy(pageContract, "detail.read_only_closed", "A finished task can't be edited. Reopen it to change the details.")
              : copy(pageContract, "detail.read_only", "Only the person who raised this task can edit it.")}
          </p>
        )}
      </div>

      <div className="ltd-body">
        <div className="ltd-main">

          <Section title={copy(pageContract, "label.brief")}>
            {detail.body ? (
              <p className="ltd-prose">{detail.body}</p>
            ) : (
              <p className="ltd-quiet">
                {copy(pageContract, "empty.description", "No brief was written for this task.")}
              </p>
            )}
          </Section>

          {/* The assignee's own latest note is part of the record, not of the activity feed: the
              backend keeps it as the task's `comment`. It sits with the brief because that is what
              it is — the other side of the same statement of work. */}
          {detail.comment ? (
            <Section title={copy(pageContract, "label.assignee_note")}>
              <p className="ltd-prose">{detail.comment}</p>
            </Section>
          ) : null}

          <Section
            title={copy(pageContract, "edit.attachments")}
            count={detail.attachments || undefined}
          >
            {attachmentRows.length ? (
              <div className="ltd-atts">
                {attachmentRows.map((attachment) => (
                  <a
                    key={attachment.attachment_id}
                    className="ltd-att"
                    href={`/api/leadership-tasks/attachments/${encodeURIComponent(detail.id)}/${encodeURIComponent(attachment.proof_id)}`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <Paperclip className="ic" aria-hidden="true" />
                    <span className="ltd-att-name">{attachment.file_name || attachment.kind}</span>
                  </a>
                ))}
              </div>
            ) : detail.attachments > 0 ? (
              // A count with no rows is what a narrowed list response carries; the evidence
              // summary is the honest thing to show rather than a fabricated file name.
              <p className="ltd-quiet">{detail.evidence}</p>
            ) : (
              <p className="ltd-quiet">
                {copy(pageContract, "empty.attachments", "No attachments on this task.")}
              </p>
            )}
          </Section>

          {/* ACTIVITY (CEO instruction 2026-09-18): Jira's History / Comments / All tabs over the
              backend's own `activity` feed, newest first — who created the task, who moved its
              status (two chips and an arrow), who edited the title / brief / deadline, who
              commented. `task-activity-feed.tsx` owns the tabs and the rows; this panel only
              hands it the data and the composer. */}
          <Section title={copy(pageContract, "section.activity", "Activity")}>
            {/* The feed and its composer are ONE client island (`task-activity-composer.tsx`):
                a sent comment is appended in place through `useOptimistic` + a returning Server
                Action, the way the board's drag-and-drop applies a move -- no redirect, no route
                render, no page banner. The candidate list, the mention-id contract and the
                no-JS submit are unchanged. The idempotency key minted here is for the NO-JS
                submit only; the island mints a fresh one per press. */}
            {loadingDetail ? (
              <p className="ltd-feed-loading" aria-live="polite">
                {copy(pageContract, "activity.loading", "Loading activity…")}
              </p>
            ) : null}
            <TaskActivityComposer
              // Remounted the moment the detail row (with its feed) replaces the summary row
              // the drawer opened from -- the composer seeds its feed on mount (Judge B, P1-1).
              key={`${detail.id}:${detailLoaded ? "detail" : "summary"}`}
              task={detail}
              pageContract={pageContract}
              action={postLeadershipTaskCommentAction}
              returnTo={returnTo}
              mentionCandidates={assignees}
              initialIdempotencyKey={`admin-web-leadership-task-note:${detail.id}:r${detail.rowVersion}`}
            />
          </Section>
        </div>

        {/* Jira's boxed Details panel: a collapsible header (native <details>, so it works with no
            JavaScript) over label/value rows. NAMES beside the avatars, never a bare circle. */}
        <details className="ltd-details" open>
          <summary className="ltd-details-hd">
            <span>{copy(pageContract, "section.details", "Details")}</span>
            <span className="ltd-details-caret" aria-hidden="true" />
          </summary>
          <div className="ltd-details-bd">
            <Row label={copy(pageContract, "column.assignee")}>
              <Person name={detail.assignee} sub={detail.assigneeRole} dash={dash} />
            </Row>
            <Row label={copy(pageContract, "column.raised_by")}>
              <Person name={detail.raisedBy} dash={dash} />
            </Row>
            <Row label={deadlineWord}>
              {detail.deadlineTone ? (
                <DeadlineClock task={detail} compact deadlineWord={deadlineWord} />
              ) : (
                <span className="ltd-quiet">{dash}</span>
              )}
            </Row>
            <Row label={copy(pageContract, "label.raised_on", "Raised on")}>
              <span className="ltd-val">{detail.age || dash}</span>
            </Row>
            <Row label={copy(pageContract, "column.evidence")}>
              <span className="ltd-val">{detail.evidence || dash}</span>
            </Row>
          </div>
        </details>
      </div>
    </aside>
  );
}

function Section({
  title,
  count,
  children,
}: {
  title: string;
  count?: number;
  children: React.ReactNode;
}) {
  return (
    <section className="ltd-sec">
      <h3 className="ltd-sec-hd">
        {title}
        {count ? <span className="ltd-sec-count">{count}</span> : null}
      </h3>
      {children}
    </section>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="ltd-row">
      <div className="ltd-row-k">{label}</div>
      <div className="ltd-row-v">{children}</div>
    </div>
  );
}

function Person({ name, sub, dash }: { name: string; sub?: string; dash: string }) {
  if (!name) return <span className="ltd-quiet">{dash}</span>;
  return (
    <span className="ltd-person">
      <span className={`ltd-av ltd-av-t${avatarTone(name)}`} aria-hidden="true">
        {initials(name)}
      </span>
      <span className="ltd-person-tx">
        <b>{name}</b>
        {sub ? <span>{sub}</span> : null}
      </span>
    </span>
  );
}

/**
 * Which of the theme's six accents an avatar wears. Deterministic off the name so one person is
 * the same colour everywhere on the screen, and drawn ONLY from the palette tokens — the avatar
 * colours are `var(--brand|info|purple|warn|ok|danger)` mixed into the panel, never a new hue.
 */
function avatarTone(name: string): number {
  let hash = 0;
  for (let index = 0; index < name.length; index += 1) {
    hash = (hash * 31 + name.charCodeAt(index)) % 100_000;
  }
  return hash % 6;
}
