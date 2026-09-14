import Link from "@/components/no-prefetch-link";
import {
  CheckCircle2,
  ClipboardList,
  Clock3,
  MessageSquareText,
  Mic2,
  Paperclip,
  Plus,
  UserRoundCheck,
  Video,
} from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import { NewTaskModal } from "./new-task-modal";
import {
  changeLeadershipTaskStatusAction,
  raiseLeadershipTaskAction,
  setLeadershipTaskCommentAction,
} from "./actions";
import type {
  LeadershipTaskAssignee,
  LeadershipTaskAttachment,
  LeadershipTaskPage,
} from "@/lib/api/server";

type TaskStatus = "open" | "doing" | "done";

type TaskRow = {
  id: string;
  number: string;
  title: string;
  body: string;
  comment: string;
  canComment: boolean;
  rowVersion: number;
  status: TaskStatus;
  statusOptions: Array<{ key: string; label: string }>;
  assignee: string;
  assigneeRole: string;
  raisedBy: string;
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
  }>;
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
};

type ScopeRow = {
  key: string;
  label: string;
  count: number;
  detail: string;
  tone: Tone;
  selected: boolean;
  emptyMessage?: string;
};

type FeedTone = "f-info" | "f-pur" | "f-warn" | "f-ok";

const fixtureTasks: TaskRow[] = [
  {
    id: "1",
    number: "#18",
    title: "Check CPT west fence repair before evening close",
    body: "Confirm the west fence patch before close and attach the completion proof.",
    comment: "Park team acknowledged.",
    canComment: true,
    rowVersion: 4,
    status: "doing",
    statusOptions: [{ key: "done", label: "Done" }],
    assignee: "Satish",
    assigneeRole: "Park Head",
    raisedBy: "Manju",
    age: "Today",
    attachments: 3,
    evidence: "video, voice note",
    attachmentKinds: ["video", "audio"],
    attachmentRows: [],
    notes: [
      {
        note_id: "fixture-note-1",
        author_name: "Satish",
        body: "Park team acknowledged.",
        created_at: new Date().toISOString(),
      },
    ],
    daysLeft: 4,
    daysLeftLabel: "4 days left",
    deadlineTone: "ok",
    deadlineLabel: "18/09/2026 17:00",
    deadlineStateLabel: "Due in 4 days",
  },
  {
    id: "2",
    number: "#17",
    title: "Confirm director handoff for feed unloading delay",
    body: "Capture what delayed unloading and who owns the next checkpoint.",
    comment: "Waiting for vendor note.",
    canComment: true,
    rowVersion: 2,
    status: "open",
    statusOptions: [{ key: "in_progress", label: "Doing" }],
    assignee: "Manohar",
    assigneeRole: "Feed Director",
    raisedBy: "Ravi",
    age: "Today",
    attachments: 2,
    evidence: "note, file",
    attachmentKinds: ["file"],
    attachmentRows: [],
    notes: [
      {
        note_id: "fixture-note-2",
        author_name: "Manohar",
        body: "Waiting for vendor note.",
        created_at: new Date().toISOString(),
      },
    ],
    daysLeft: -4,
    daysLeftLabel: "4 days over",
    deadlineTone: "over",
    deadlineLabel: "10/09/2026 12:00",
    deadlineStateLabel: "Overdue by 4 days",
  },
  {
    id: "3",
    number: "#16",
    title: "Send Borewell-2 motor reading after restart",
    body: "Share the post-restart reading with a short clip.",
    comment: "Completed.",
    canComment: false,
    rowVersion: 7,
    status: "done",
    statusOptions: [],
    assignee: "Prakash",
    assigneeRole: "Employee",
    raisedBy: "Manju",
    age: "Yesterday",
    attachments: 4,
    evidence: "completion video",
    attachmentKinds: ["video"],
    attachmentRows: [],
    notes: [
      {
        note_id: "fixture-note-3",
        author_name: "Prakash",
        body: "Completed.",
        created_at: new Date().toISOString(),
      },
    ],
    daysLeft: null,
    daysLeftLabel: "",
    deadlineTone: "",
    deadlineLabel: "",
    deadlineStateLabel: "",
  },
];

const fixtureScopes: ScopeRow[] = [
  {
    key: "assigned_to_me",
    label: "Assigned to me",
    count: 2,
    detail: "My action queue",
    tone: "info",
    selected: false,
  },
  {
    key: "assigned_by_me",
    label: "Assigned by me",
    count: 7,
    detail: "Follow-ups I raised",
    tone: "warn",
    selected: false,
  },
  {
    key: "team_progress",
    label: "Team progress",
    count: 18,
    detail: "Open team work",
    tone: "ok",
    selected: true,
  },
];

export function LeadershipTasksPage({
  page,
  preview = false,
  selectedScopeKey,
  selectedTaskID,
  assignees = [],
}: {
  page?: LeadershipTaskPage | null;
  preview?: boolean;
  selectedScopeKey?: string;
  selectedTaskID?: string;
  assignees?: LeadershipTaskAssignee[];
}) {
  const tasks = page ? rowsFromPage(page) : preview ? fixtureTasks : [];
  const scopes = page?.scopes?.length
    ? scopesFromPage(page)
    : preview
      ? fixtureScopes
      : [];
  const selected = selectedTaskID
    ? tasks.find((task) => task.id === selectedTaskID)
    : undefined;
  const hasTasks = tasks.length > 0;
  const hasSidePanel = Boolean(selected || hasTasks);
  const selectedScope =
    scopes.find((scope) => scope.key === selectedScopeKey) ??
    scopes.find((scope) => scope.selected) ??
    scopes.find((scope) => scope.key === "team_progress") ??
    scopes.find((scope) => scope.key === "assigned_by_me") ??
    scopes[0];

  return (
    <div className="screen on lt-page">
      <div className="phead lt-phead">
        <div>
          <div className="crumb">
            Operations / <b>Tasks</b>
          </div>
          <h1>{page?.title || "Tasks"}</h1>
          <div className="sub">
            {preview
              ? "Preview data"
              : "Tasks raised across CXOs, directors and park heads."}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {page?.can_raise ? (
          <NewTaskModal
            assignees={assignees}
            action={raiseLeadershipTaskAction}
            returnTo="/tasks?scope=assigned_by_me"
          />
        ) : preview ? (
          <Tag tone="ok">Can raise</Tag>
        ) : null}
      </div>

      <div className="lt-scopebar">
        {scopes.length ? (
          <div className="subtabs" aria-label="Task scopes">
            {scopes.map((scope) => (
              <Link
                key={scope.key}
                href={`${preview ? "/tasks-preview" : "/tasks"}?scope=${scope.key}`}
                className={scope.key === selectedScope?.key ? "on" : ""}
              >
                {scope.label}
                <span className="cbq">{scope.count}</span>
              </Link>
            ))}
          </div>
        ) : (
          <div className="lt-unavailable">
            Tasks could not be loaded. Try again.
          </div>
        )}
      </div>

      <div className={`lt-grid${hasSidePanel ? "" : " lt-grid-solo"}`}>
        <section className="card lt-card" style={{ minWidth: 0 }}>
          <div className="hd">
            <ClipboardList
              className="ic"
              style={{ color: "var(--brand)" }}
              aria-hidden="true"
            />
            <h3>{selectedScope?.label || "Tasks"}</h3>
            <div className="sp" style={{ flex: 1 }} />
            <Tag tone="info">{selectedScope?.detail || "Live queue"}</Tag>
          </div>
          {hasTasks ? (
            <div
              className="bd lt-tablewrap"
              tabIndex={0}
              role="group"
              aria-label="Leadership task progress"
            >
              <table data-enh="1" className="lt-task-table">
              <thead>
                <tr>
                  <th>Task</th>
                  {/* The day counter sits beside the task, second column, so it is read without
                      scrolling the table -- on a phone only the first two columns fit. */}
                  <th className="lt-days-col">Days</th>
                  <th>Assignee</th>
                  <th>Raised by</th>
                  <th>Status</th>
                  <th>Evidence</th>
                </tr>
              </thead>
              <tbody>
                {tasks.map((task) => (
                  <tr
                    key={task.id}
                    className={task.id === selected?.id ? "is-selected" : ""}
                    aria-current={task.id === selected?.id ? "true" : undefined}
                  >
                    <td>
                      <Link
                        href={`${preview ? "/tasks-preview" : "/tasks"}?scope=${selectedScope?.key ?? "team_progress"}&task=${encodeURIComponent(task.id)}`}
                        className="lt-tasklink"
                        aria-label={`Open task ${task.number}: ${task.title}`}
                      >
                        <b>{task.number}</b>
                        <span className="muted small">{task.title}</span>
                      </Link>
                      {/* On a phone the table shows only its first column, so the same clock
                          sits under the title there (CSS shows one or the other, never both). */}
                      {task.deadlineTone ? (
                        <div className="lt-clock-inline">
                          <DeadlineClock task={task} compact />
                        </div>
                      ) : null}
                    </td>
                    <td className="lt-days-col">
                      <DeadlineClock task={task} compact />
                    </td>
                    <td>
                      <div className="lt-opname">
                        <span className="lt-avx">
                          {initials(task.assignee)}
                        </span>
                        <span>
                          {task.assignee}
                          <span className="lt-code muted">
                            {task.assigneeRole}
                          </span>
                        </span>
                      </div>
                    </td>
                    <td>{task.raisedBy}</td>
                    <td>
                      <Tag tone={statusTone(task.status)}>
                        {statusLabel(task.status)}
                      </Tag>
                    </td>
                    <td>
                      <span
                        style={{
                          display: "inline-flex",
                          alignItems: "center",
                          gap: 7,
                          minWidth: 0,
                        }}
                      >
                        <Paperclip
                          className="ic"
                          style={{ width: 15, color: "var(--muted)" }}
                          aria-hidden="true"
                        />
                        <b>{task.attachments}</b>
                        <span className="muted small">{task.evidence}</span>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
              </table>
            </div>
          ) : (
            <div className="bd lt-empty-state">
              <ClipboardList className="ic" aria-hidden="true" />
              <div>
                <b>{selectedScope?.emptyMessage || "No tasks in this scope."}</b>
                <p>
                  This queue is clear for the current role and park scope. When
                  work is raised, it will appear here with the owner, evidence,
                  and next status action.
                </p>
              </div>
            </div>
          )}
        </section>

        {selected ? (
          <aside className="card lt-card lt-detail-card">
            <div className="hd">
              <UserRoundCheck
                className="ic"
                style={{ color: "var(--brand)" }}
                aria-hidden="true"
              />
              <h3>Selected task</h3>
              <div className="sp" style={{ flex: 1 }} />
              <Tag tone={statusTone(selected.status)}>
                {statusLabel(selected.status)}
              </Tag>
            </div>
            <div className="bd">
              <div className="crumb">
                <b>{selected.number}</b> / {selected.age}
              </div>
              <h3 className="lt-detail-title">{selected.title}</h3>
              {selected.deadlineTone ? (
                <DeadlineClock task={selected} />
              ) : null}
              <div className="metagrid lt-detail-meta">
                <Meta
                  label="Assignee"
                  value={`${selected.assignee} - ${selected.assigneeRole}`}
                />
                <Meta label="Raised by" value={selected.raisedBy} />
                <Meta label="Evidence" value={selected.evidence} />
                <Meta
                  label="Attachments"
                  value={String(selected.attachments)}
                />
              </div>
              {selected.body ? (
                <div className="note-box lt-note-box">
                  <b>Brief</b>
                  <p>{selected.body}</p>
                </div>
              ) : null}
              {selected.comment ? (
                <div className="note-box lt-note-box">
                  <b>Assignee note</b>
                  <p>{selected.comment}</p>
                </div>
              ) : null}
              {selected.statusOptions.length ? (
                <div className="lt-status-actions">
                  {selected.statusOptions.map((option) => (
                    <form
                      key={option.key}
                      action={changeLeadershipTaskStatusAction}
                    >
                      <input
                        type="hidden"
                        name="idempotency_key"
                        value={`admin-web-leadership-task-status:${selected.id}:${option.key}:${crypto.randomUUID()}`}
                      />
                      <input
                        type="hidden"
                        name="return_to"
                        value={`/tasks?scope=${encodeURIComponent(selectedScope.key)}&task=${encodeURIComponent(selected.id)}`}
                      />
                      <input type="hidden" name="task_id" value={selected.id} />
                      <input
                        type="hidden"
                        name="row_version"
                        value={selected.rowVersion}
                      />
                      <input type="hidden" name="status" value={option.key} />
                      <button type="submit" className="btn">
                        <CheckCircle2 className="ic" aria-hidden="true" />
                        {option.label}
                      </button>
                    </form>
                  ))}
                </div>
              ) : null}
              {selected.canComment ? (
                <form
                  action={setLeadershipTaskCommentAction}
                  className="lt-comment-form"
                >
                  <input
                    type="hidden"
                    name="idempotency_key"
                    value={`admin-web-leadership-task-note:${selected.id}:${crypto.randomUUID()}`}
                  />
                  <input
                    type="hidden"
                    name="return_to"
                    value={`/tasks?scope=${encodeURIComponent(selectedScope.key)}&task=${encodeURIComponent(selected.id)}`}
                  />
                  <input type="hidden" name="task_id" value={selected.id} />
                  <label className="fld">
                    <span>Activity update</span>
                    <textarea
                      name="comment"
                      maxLength={2000}
                      rows={3}
                      placeholder="Write the latest status or reply."
                      required
                    />
                  </label>
                  <button type="submit" className="btn p">
                    <MessageSquareText className="ic" aria-hidden="true" />
                    Send update
                  </button>
                </form>
              ) : null}
              {selected.attachmentRows.length ? (
                <div className="lt-attachments" style={{ marginBottom: 12 }}>
                  {selected.attachmentRows.map((attachment) => (
                    <a
                      key={attachment.attachment_id}
                      className="chip"
                      href={`/api/leadership-tasks/attachments/${encodeURIComponent(selected.id)}/${encodeURIComponent(attachment.proof_id)}`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      <Paperclip className="ic" aria-hidden="true" />
                      {attachment.file_name || attachment.kind}
                    </a>
                  ))}
                </div>
              ) : null}
              <div className="lt-feed lt-detail-feed">
                {selected.notes.length ? (
                  selected.notes.map((note) => (
                    <FeedRow
                      key={note.note_id}
                      icon={MessageSquareText}
                      tone="f-pur"
                      title={note.author_name || "Task update"}
                      detail={note.body}
                    />
                  ))
                ) : preview ? (
                  <>
                    <FeedRow
                      icon={Clock3}
                      tone="f-info"
                      title="Opened today"
                      detail="Moved into Doing after park-head acknowledgement."
                    />
                    <FeedRow
                      icon={Mic2}
                      tone="f-pur"
                      title="Voice note attached"
                      detail="Supervisor note is stored against the task comments."
                    />
                    <FeedRow
                      icon={Video}
                      tone="f-warn"
                      title="Video evidence attached"
                      detail="Completion proof can be reviewed before closing."
                    />
                  </>
                ) : (
                  liveFeedRows(selected).map((row) => (
                    <FeedRow
                      key={row.title}
                      icon={row.icon}
                      tone={row.tone}
                      title={row.title}
                      detail={row.detail}
                    />
                  ))
                )}
                <FeedRow
                  icon={CheckCircle2}
                  tone="f-ok"
                  title="Monitoring ready"
                  detail="Status, notes, and files stay visible in the same task row."
                />
              </div>
            </div>
          </aside>
        ) : hasTasks ? (
          <aside className="card lt-card lt-detail-card">
            <div className="hd">
              <UserRoundCheck
                className="ic"
                style={{ color: "var(--brand)" }}
                aria-hidden="true"
              />
              <h3>Selected task</h3>
            </div>
            <div className="bd lt-empty-state">
              <ClipboardList className="ic" aria-hidden="true" />
              <div>
                <b>Select a task</b>
                <p>
                  Choose a row to view its brief, status actions, attachments,
                  and activity updates.
                </p>
              </div>
            </div>
          </aside>
        ) : null}
      </div>
    </div>
  );
}

function rowsFromPage(page: LeadershipTaskPage): TaskRow[] {
  return page.rows.map((task) => {
    const status =
      task.status === "done"
        ? "done"
        : task.status === "in_progress"
          ? "doing"
          : "open";
    const attachmentKinds =
      task.attachments?.map((attachment) => attachment.kind).filter(Boolean) ??
      [];
    const evidence = attachmentKinds.join(", ");
    return {
      id: task.task_id,
      number: task.number_label,
      title: task.title,
      body: task.body,
      comment: task.comment,
      canComment: task.can_comment,
      rowVersion: task.row_version,
      status,
      statusOptions: task.status_options ?? [],
      assignee: task.assignee_name,
      assigneeRole: task.is_assignee ? "Assigned to me" : "Assignee",
      raisedBy: task.raised_by_name,
      age: task.raised_on_label,
      attachments: task.attachment_count,
      evidence:
        evidence ||
        (task.attachment_count > 0 ? "attached files" : "no attachments"),
      attachmentKinds,
      attachmentRows: task.attachments ?? [],
      notes: task.notes ?? [],
      daysLeft: task.days_left ?? null,
      daysLeftLabel: task.days_left_label ?? "",
      deadlineTone: (task.deadline_tone ?? "") as TaskRow["deadlineTone"],
      deadlineLabel: task.deadline_label ?? "",
      deadlineStateLabel: task.deadline_state_label ?? "",
    };
  });
}

function scopesFromPage(page: LeadershipTaskPage): ScopeRow[] {
  const detailByKey: Record<string, string> = {
    assigned_to_me: "My action queue",
    assigned_by_me: "Follow-ups I raised",
    team_progress: "Open team work",
  };
  const toneByKey: Record<string, Tone> = {
    assigned_to_me: "info",
    assigned_by_me: "warn",
    team_progress: "ok",
  };
  return (page.scopes ?? []).map((scope) => ({
    key: scope.key,
    label: scope.label,
    count: scope.count,
    detail: detailByKey[scope.key] ?? "Task queue",
    tone: toneByKey[scope.key] ?? "info",
    selected: scope.selected,
    emptyMessage: scope.empty_message,
  }));
}

function liveFeedRows(task: TaskRow): Array<{
  icon: typeof Clock3;
  tone: FeedTone;
  title: string;
  detail: string;
}> {
  const rows: Array<{
    icon: typeof Clock3;
    tone: FeedTone;
    title: string;
    detail: string;
  }> = [
    {
      icon: Clock3,
      tone: "f-info",
      title: statusLabel(task.status),
      detail: `${task.number} is ${statusLabel(task.status).toLowerCase()} for ${task.assignee}.`,
    },
  ];
  if (task.attachmentKinds.includes("audio")) {
    rows.push({
      icon: Mic2,
      tone: "f-pur",
      title: "Audio attached",
      detail: "A voice note is attached to this task.",
    });
  }
  if (task.attachmentKinds.includes("video")) {
    rows.push({
      icon: Video,
      tone: "f-warn",
      title: "Video attached",
      detail: "A video file is attached to this task.",
    });
  }
  if (task.attachments > 0 && rows.length === 1) {
    rows.push({
      icon: Paperclip,
      tone: "f-pur",
      title: "Files attached",
      detail: `${task.attachments} attachment${task.attachments === 1 ? "" : "s"} on this task.`,
    });
  }
  return rows;
}

function FeedRow({
  icon: Icon,
  tone,
  title,
  detail,
}: {
  icon: typeof Clock3;
  tone: "f-info" | "f-pur" | "f-warn" | "f-ok";
  title: string;
  detail: string;
}) {
  return (
    <div className="lt-frow" style={{ paddingInline: 0 }}>
      <span className={`lt-fdot ${tone}`} aria-hidden="true" />
      <div className="lt-ftx">
        <b>
          <Icon
            className="ic"
            style={{ width: 14, verticalAlign: -2, marginRight: 5 }}
            aria-hidden="true"
          />
          {title}
        </b>
        <div className="lt-fmeta">{detail}</div>
      </div>
    </div>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v">{value}</div>
    </div>
  );
}

function statusLabel(status: TaskStatus): string {
  if (status === "doing") return "Doing";
  if (status === "done") return "Done";
  return "Open";
}

function statusTone(status: TaskStatus): Tone {
  if (status === "doing") return "info";
  if (status === "done") return "ok";
  return "warn";
}

/**
 * THE BIG NUMBER: days left to the deadline, green while more than two days remain and red
 * from two days out and once overdue, with the deadline beneath. Every value is the
 * backend's -- this component counts nothing and decides no colour; it only maps the tone the
 * backend named onto a class and splits the worded label into number + unit. A task without a
 * deadline renders a quiet dash so the column still lines up.
 */
function DeadlineClock({ task, compact = false }: { task: TaskRow; compact?: boolean }) {
  if (!task.deadlineTone || task.daysLeft === null) {
    return compact ? (
      <span className="muted small">—</span>
    ) : null;
  }
  const tone = task.deadlineTone === "ok" ? "lt-clock-ok" : "lt-clock-late";
  // "5 days left" -> 5 + "days left"; "Due today" has no number and shows the words alone.
  const unit = task.daysLeftLabel.replace(/^\d+\s*/, "");
  const showNumber = task.daysLeft !== 0;
  return (
    <div
      className={`lt-clock ${tone}${compact ? " lt-clock-compact" : ""}`}
      role="group"
      aria-label={`${task.daysLeftLabel}, ${task.deadlineStateLabel.toLowerCase()}, deadline ${task.deadlineLabel}`}
    >
      <div className="lt-clock-num">
        {showNumber ? <b>{Math.abs(task.daysLeft)}</b> : null}
        <span>{unit}</span>
      </div>
      <div className="lt-clock-meta">
        <span className="lt-clock-state">{task.deadlineStateLabel}</span>
        <span className="lt-clock-deadline">Deadline {task.deadlineLabel}</span>
      </div>
    </div>
  );
}

function initials(name: string): string {
  return name
    .split(/\s+/)
    .map((part) => part.charAt(0))
    .join("")
    .slice(0, 2)
    .toUpperCase();
}
