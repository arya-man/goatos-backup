import type {
  LeadershipTaskActivity,
  LeadershipTaskAssignee,
  LeadershipTaskAttachment,
  LeadershipTaskPage,
} from "@/lib/api/server";

/**
 * One row of the Tasks desk, flattened to PLAIN data.
 *
 * Flat and serializable on purpose: the table, the detail panel and the edit modal are client
 * components, so every value they render has to cross the server/client boundary. Nothing is
 * derived here that the backend already decided — the status chip, the deadline countdown, its
 * tone and its sentence are all composed backend-side and rendered verbatim.
 */
export type TaskStatus = "open" | "in_progress" | "done" | "cancelled";

export type TaskRow = {
  id: string;
  number: string;
  title: string;
  body: string;
  comment: string;
  canComment: boolean;
  canEdit: boolean;
  rowVersion: number;
  status: TaskStatus;
  /** The backend's own chip wording for the status. */
  statusLabel: string;
  statusOptions: Array<{ key: string; label: string }>;
  assignee: string;
  assigneeRole: string;
  raisedBy: string;
  raisedByUserID: string;
  assigneeUserID: string;
  age: string;
  attachments: number;
  evidence: string;
  attachmentKinds: string[];
  attachmentRows: LeadershipTaskAttachment[];
  notes: Array<{
    note_id: string;
    author_name: string;
    body: string;
    created_at: string;
    /** The people the note named, resolved and stored by the backend; the feed draws them as chips. */
    mentions?: Array<{ user_id: string; name: string }>;
  }>;
  /**
   * The task's history feed, NEWEST FIRST, exactly as the backend composed it: who created
   * it, who moved its status, who edited what, who commented. The panel's History / Comments /
   * All tabs are views over this one list (`task-activity-feed.tsx`); nothing is derived here.
   */
  activity: LeadershipTaskActivity[];
  /**
   * The deadline countdown, backend-composed and rendered verbatim (maintainer decision
   * 2026-09-14): days left to the deadline (0 = due today, negative = overdue), its tone
   * (ok = green; near and over = red), the deadline itself and the sentence beneath. A task
   * without a deadline carries none of these and shows no counter.
   */
  daysLeft: number | null;
  daysLeftLabel: string;
  deadlineTone: "" | "ok" | "near" | "over";
  deadlineLabel: string;
  deadlineStateLabel: string;
  /** The stored instant, for the edit form's `datetime-local` field. */
  deadlineAt: string;
};

export function rowsFromPage(page: LeadershipTaskPage): TaskRow[] {
  return page.rows.map((task) => {
    const attachmentKinds =
      task.attachments?.map((attachment) => attachment.kind).filter(Boolean) ?? [];
    const evidence = attachmentKinds.join(", ");
    return {
      id: task.task_id,
      number: task.number_label,
      title: task.title,
      body: task.body,
      comment: task.comment,
      canComment: task.can_comment,
      canEdit: task.can_edit,
      rowVersion: task.row_version,
      status: task.status as TaskStatus,
      statusLabel: task.status_chip,
      statusOptions: task.status_options ?? [],
      assignee: task.assignee_name,
      assigneeRole: task.is_assignee ? "Assigned to me" : "Assignee",
      raisedBy: task.raised_by_name,
      raisedByUserID: task.raised_by_user_id,
      assigneeUserID: task.assignee_user_id,
      age: task.raised_on_label,
      attachments: task.attachment_count,
      evidence:
        evidence || (task.attachment_count > 0 ? "attached files" : "no attachments"),
      attachmentKinds,
      attachmentRows: task.attachments ?? [],
      notes: task.notes ?? [],
      activity: task.activity ?? [],
      daysLeft: task.days_left ?? null,
      daysLeftLabel: task.days_left_label ?? "",
      deadlineTone: (task.deadline_tone ?? "") as TaskRow["deadlineTone"],
      deadlineLabel: task.deadline_label ?? "",
      deadlineStateLabel: task.deadline_state_label ?? "",
      deadlineAt: task.deadline_at ?? "",
    };
  });
}

/** The people pickers are fed by /app/leadership-tasks/assignees, not by the list response. */
export function personOptions(
  assignees: LeadershipTaskAssignee[],
): Array<{ value: string; label: string; title?: string }> {
  return assignees.map((assignee) => ({
    value: assignee.user_id,
    label: assignee.name || assignee.title,
    // The ROLE, carried beside the name because names collide on this roster ("Manju" and
    // "Manju Flokx", two CEO/CXOs) and a picker that shows only the name cannot be used to pick
    // the right one. It is also what the search ranks on after the name.
    title: assignee.name ? assignee.title : "",
  }));
}
