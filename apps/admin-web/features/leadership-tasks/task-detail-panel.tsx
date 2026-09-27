"use client";

import { visuallyHidden } from "@mui/utils";
import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Avatar from "@mui/material/Avatar";
import Button from "@mui/material/Button";
import Tooltip from "@mui/material/Tooltip";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import ListItemText from "@mui/material/ListItemText";

import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { EmptyContent } from "@/components/minimal/empty-content";
import { BlockLabel } from "@/components/app/kanban/kanban-details";

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
import { initials } from "./task-presentation";
import { TASK_PARAM, tasksHref, tasksSearchParams, type TasksParams } from "./params";
import { attachmentKindLabel, type TaskRow } from "./task-row";
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
  detailError,
  onRetryDetail,
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
  /** Set when the host's detail read failed: an inline sentence instead of a spinner forever. */
  detailError?: string;
  /** Re-runs the host's detail read. */
  onRetryDetail?: () => void;
}) {
  // The whole URL, rebuilt from the PARSED state: this component is handed `TasksParams` and no
  // raw search params, and `tasksSearchParams` is the seam that keeps the repeated cursor stack
  // (i.e. the reader's back-paging depth) alive in every link minted below.
  const sp = tasksSearchParams(params);

  if (!detail) {
    return (
      <Box component="aside" className="ltd-panel ltd-panel-empty" aria-label={copy(pageContract, "section.selected.title")} sx={{ p: 3 }}>
        <EmptyContent
          title={copy(pageContract, "empty.selected")}
          description={[
            copy(pageContract, "empty.selected_detail"),
            canRaise ? copy(pageContract, "empty.selected_raise", "Or raise a new task from the button above the list.") : "",
          ]
            .filter(Boolean)
            .join(" ")}
        />
      </Box>
    );
  }

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
  const closeLabel = copy(pageContract, "action.close");

  // Template kanban details (sections/kanban/details/kanban-details.tsx + -toolbar.tsx): the status
  // control at the left of the toolbar, the per-task actions at its right (edit -- raiser only,
  // open/in_progress only, offered only when the ROW says so -- and close), then the task name and
  // BlockLabel rows, the brief, attachments and the activity feed.
  return (
    <Box component="aside" className="ltd-panel" aria-label={copy(pageContract, "section.selected.title")} sx={{ display: "flex", flexDirection: "column", minHeight: 1 }}>
      <Box
        sx={(theme) => ({
          display: "flex",
          alignItems: "center",
          gap: 1,
          p: theme.spacing(2.5, 1, 2.5, 2.5),
          borderBottom: `solid 1px ${theme.vars.palette.divider}`,
        })}
      >
        {/* THE status control is ONE dropdown (`task-status-menu.tsx`). With no options (a reader
            who may not move this task, or a cancelled one) it is a plain Label: nothing dead
            behind it. `data-ltd-status` names which of the two it is for tests. */}
        {detail.statusOptions.length ? (
          <TaskStatusMenu
            task={detail}
            pageContract={pageContract}
            action={changeLeadershipTaskStatusAction}
            returnTo={returnTo}
          />
        ) : (
          <Label variant="soft" color={statusColor(detail.status)} className="ltd-status" data-ltd-status="badge">
            {detail.statusLabel}
          </Label>
        )}
        <Box component="span" sx={{ flexGrow: 1 }} />
        <Box className="ltd-top-actions" sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          {detail.canEdit ? (
            <span data-testid="ltd-edit" style={{ display: "contents" }}>
              <EditTaskModal
                task={detail}
                pageContract={pageContract}
                action={editLeadershipTaskAction}
                returnTo={returnTo}
              />
            </span>
          ) : null}
          {/* In the drawer (onClose given) the template drawer header owns the one close button. */}
          {onClose ? null : (
            <Tooltip title={closeLabel}>
              <IconButton className="ltd-close" component={Link} href={closeHref} scroll={false} aria-label={closeLabel}>
                <Iconify icon="mingcute:close-line" />
              </IconButton>
            </Tooltip>
          )}
        </Box>
      </Box>

      <Box sx={{ py: 3, px: 2.5, gap: "var(--sp-3)", display: "flex", flexDirection: "column" }}>
        <div>
          <Typography variant="caption" sx={{ color: "text.disabled" }}>
            {copy(pageContract, "crumb")} / {pageContract.title} / <b>{detail.number}</b>
          </Typography>
          <Typography variant="h6" component="h2" sx={{ mt: 0.5, overflowWrap: "anywhere" }}>
            {detail.title}
          </Typography>
          {detail.canEdit ? null : (
            <Box
              component="p"
              className="ltd-readonly"
              data-testid="ltd-read-only"
              sx={visuallyHidden}
            >
              {detail.status === "cancelled"
                ? copy(pageContract, "detail.read_only_cancelled", "A cancelled task can't be edited or reopened.")
                : detail.status === "done"
                  ? copy(pageContract, "detail.read_only_closed", "A finished task can't be edited. Reopen it to change the details.")
                  : copy(pageContract, "detail.read_only", "Only the person who raised this task can edit it.")}
            </Box>
          )}
        </div>

        <Row label={copy(pageContract, "column.assignee")}>
          <Person
            name={detail.assignee}
            sub={detail.assigneeRole || (detail.isAssignee ? copy(pageContract, "label.assigned_to_me", "Assigned to me") : "")}
            dash={dash}
          />
        </Row>
        <Row label={copy(pageContract, "column.raised_by")}>
          <Person name={detail.raisedBy} dash={dash} />
        </Row>
        <Row label={deadlineWord}>
          {detail.deadlineTone ? <DeadlineClock task={detail} compact deadlineWord={deadlineWord} /> : <Quiet>{dash}</Quiet>}
        </Row>
        <Row label={copy(pageContract, "label.raised_on", "Raised on")}>
          <Typography variant="body2">{detail.age || dash}</Typography>
        </Row>
        <Row label={copy(pageContract, "column.evidence")}>
          <Typography variant="body2">{detail.evidence || dash}</Typography>
        </Row>

        <Row label={copy(pageContract, "label.brief")} top>
          {detail.body ? (
            <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{detail.body}</Typography>
          ) : (
            <Quiet>{copy(pageContract, "empty.description", "No brief was written for this task.")}</Quiet>
          )}
        </Row>

        {/* The assignee's own latest note is part of the record (the task's `comment`), so it sits
            with the brief -- the other side of the same statement of work. */}
        {detail.comment ? (
          <Row label={copy(pageContract, "label.assignee_note")} top>
            <Typography variant="body2" sx={{ whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>{detail.comment}</Typography>
          </Row>
        ) : null}

        <Row label={copy(pageContract, "edit.attachments")} top>
          {attachmentRows.length ? (
            <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
              {attachmentRows.map((attachment) => (
                <Chip
                  key={attachment.attachment_id}
                  component="a"
                  clickable
                  variant="soft"
                  icon={<Iconify icon="eva:attach-2-fill" width={16} />}
                  label={attachment.file_name || attachmentKindLabel(attachment.kind)}
                  href={`/api/leadership-tasks/attachments/${encodeURIComponent(detail.id)}/${encodeURIComponent(attachment.proof_id)}`}
                  target="_blank"
                  rel="noreferrer"
                  sx={{ maxWidth: 1 }}
                />
              ))}
            </Box>
          ) : detail.attachments > 0 ? (
            <Quiet>{detail.evidence}</Quiet>
          ) : (
            <Quiet>{copy(pageContract, "empty.attachments", "No attachments on this task.")}</Quiet>
          )}
        </Row>

        {/* ACTIVITY (CEO instruction 2026-09-18): History / Comments / All tabs over the backend's
            own `activity` feed, newest first, and its composer -- ONE client island
            (`task-activity-composer.tsx`). */}
        <Box component="section" sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
          <Typography variant="subtitle2">{copy(pageContract, "section.activity", "Activity")}</Typography>
          {loadingDetail ? (
            <Typography variant="body2" aria-live="polite" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "activity.loading", "Loading activity…")}
            </Typography>
          ) : null}
          {detailError ? (
            <Typography variant="body2" role="alert" data-testid="ltd-detail-error" sx={{ color: "error.main" }}>
              {detailError}{" "}
              {onRetryDetail ? (
                <Button color="primary" size="small" variant="text" onClick={onRetryDetail}>
                  {copy(pageContract, "action.retry", "Try again")}
                </Button>
              ) : null}
            </Typography>
          ) : null}
          <TaskActivityComposer
            key={`${detail.id}:${detailLoaded ? "detail" : "summary"}`}
            task={detail}
            pageContract={pageContract}
            action={postLeadershipTaskCommentAction}
            returnTo={returnTo}
            mentionCandidates={assignees}
            activityLoading={loadingDetail}
            initialIdempotencyKey={`admin-web-leadership-task-note:${detail.id}:r${detail.rowVersion}`}
          />
        </Box>
      </Box>
    </Box>
  );
}

/** Template kanban details row: a fixed-width caption label, the value beside it. */
function Row({ label, children, top = false }: { label: string; children: React.ReactNode; top?: boolean }) {
  return (
    <Box sx={{ display: "flex", alignItems: top ? "flex-start" : "center", minWidth: 0 }}>
      <BlockLabel sx={top ? { pt: 0.25 } : undefined}>{label}</BlockLabel>
      <Box sx={{ minWidth: 0, flex: "1 1 auto" }}>{children}</Box>
    </Box>
  );
}

function Quiet({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" sx={{ color: "text.disabled" }}>
      {children}
    </Typography>
  );
}

function Person({ name, sub, dash }: { name: string; sub?: string; dash: string }) {
  if (!name) return <Quiet>{dash}</Quiet>;
  return (
    <Box sx={{ gap: 1.5, display: "flex", alignItems: "center", minWidth: 0 }}>
      <Avatar alt={name} sx={{ width: "var(--sp-4)", height: "var(--sp-4)", typography: "caption" }}>
        {initials(name)}
      </Avatar>
      <ListItemText
        primary={name}
        secondary={sub || null}
        slotProps={{ primary: { variant: "subtitle2" }, secondary: { variant: "caption", sx: { color: "text.disabled" } } }}
        sx={{ m: 0, minWidth: 0 }}
      />
    </Box>
  );
}

function statusColor(status: TaskRow["status"]): "warning" | "info" | "success" | "default" {
  if (status === "in_progress") return "info";
  if (status === "done") return "success";
  if (status === "cancelled") return "default";
  return "warning";
}
