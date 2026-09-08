import Link from "@/components/no-prefetch-link";
import {
  CheckCircle2,
  ClipboardList,
  Clock3,
  Mic2,
  Paperclip,
  Plus,
  UserRoundCheck,
  Video,
} from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
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
  priority: "High" | "Medium" | "Normal";
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
    priority: "High",
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
    priority: "Medium",
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
    priority: "Normal",
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
  const selected = tasks.find((task) => task.id === selectedTaskID) ?? tasks[0];
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
            {preview ? "Preview data" : "Live backend data"} / Manual work
            assigned across directors, park heads, and employees.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {preview || page?.can_raise ? <Tag tone="ok">Can raise</Tag> : null}
      </div>

      <div className="lt-kpis">
        {scopes.map((scope) => (
          <KPI
            key={scope.key}
            label={scope.label}
            value={String(scope.count)}
            detail={scope.detail}
            tone={scope.tone}
          />
        ))}
        {!scopes.length ? (
          <KPI
            label="Team progress"
            value="0"
            detail="Live data unavailable"
            tone="warn"
          />
        ) : null}
      </div>

      <div
        className="subtabs"
        style={{ marginBottom: 14 }}
        aria-label="Task scopes"
      >
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

      <div className="lt-grid">
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
          <div
            className="bd lt-tablewrap"
            tabIndex={0}
            role="group"
            aria-label="Leadership task progress"
          >
            <table data-enh="1">
              <thead>
                <tr>
                  <th>Task</th>
                  <th>Assignee</th>
                  <th>Raised by</th>
                  <th>Status</th>
                  <th>Evidence</th>
                  <th>Priority</th>
                </tr>
              </thead>
              <tbody>
                {tasks.length === 0 ? (
                  <tr>
                    <td colSpan={6}>
                      <span className="muted small">
                        {selectedScope?.emptyMessage ||
                          "No tasks in this scope."}
                      </span>
                    </td>
                  </tr>
                ) : null}
                {tasks.map((task) => (
                  <tr
                    key={task.id}
                    className={task.id === selected?.id ? "is-selected" : ""}
                  >
                    <td>
                      <Link
                        href={`${preview ? "/tasks-preview" : "/tasks"}?scope=${selectedScope?.key ?? "team_progress"}&task=${encodeURIComponent(task.id)}`}
                        className="lt-tasklink"
                      >
                        <b>{task.number}</b>
                        <span className="muted small">{task.title}</span>
                      </Link>
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
                    <td>
                      <Tag tone={priorityTone(task.priority)}>
                        {task.priority}
                      </Tag>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        {page?.can_raise ? (
          <aside className="card lt-card">
            <div className="hd">
              <Plus
                className="ic"
                style={{ color: "var(--brand)" }}
                aria-hidden="true"
              />
              <h3>New task</h3>
            </div>
            <form
              className="bd"
              action={raiseLeadershipTaskAction}
              style={{ display: "grid", gap: 12 }}
            >
              <input
                type="hidden"
                name="idempotency_key"
                value={`admin-web-leadership-task:${crypto.randomUUID()}`}
              />
              <input
                type="hidden"
                name="return_to"
                value="/tasks?scope=assigned_by_me"
              />
              <label className="fld">
                <span>Assignee</span>
                <select name="assignee_user_id" required defaultValue="">
                  <option value="" disabled>
                    Select person
                  </option>
                  {assignees.map((assignee) => (
                    <option key={assignee.user_id} value={assignee.user_id}>
                      {assignee.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="fld">
                <span>Title</span>
                <input
                  name="title"
                  required
                  maxLength={80}
                  placeholder="What needs follow-up?"
                />
              </label>
              <label className="fld">
                <span>Note</span>
                <textarea
                  name="body"
                  maxLength={4000}
                  rows={4}
                  placeholder="Add context for the assignee."
                />
              </label>
              <label className="fld">
                <span>Attachment / voice note</span>
                <input
                  type="file"
                  name="attachment_file"
                  multiple
                  accept="audio/*,video/*,image/*,.pdf,.doc,.docx,.xls,.xlsx,.csv,.txt"
                />
              </label>
              <div className="metagrid">
                <label className="fld">
                  <span>Existing proof ID</span>
                  <input
                    name="attachment_proof_id"
                    placeholder="Optional existing proof id"
                  />
                </label>
                <label className="fld">
                  <span>Type</span>
                  <select name="attachment_kind" defaultValue="file">
                    <option value="file">File</option>
                    <option value="audio">Voice note</option>
                    <option value="video">Video</option>
                    <option value="photo">Photo</option>
                  </select>
                </label>
              </div>
              <label className="fld">
                <span>Attachment name</span>
                <input
                  name="attachment_file_name"
                  placeholder="Optional file name"
                />
              </label>
              <button
                type="submit"
                className="btn p"
                disabled={!assignees.length}
              >
                <Plus className="ic" aria-hidden="true" />
                Create
              </button>
            </form>
          </aside>
        ) : null}

        {selected ? (
          <aside className="card lt-card">
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
              <h3 style={{ margin: "7px 0 12px", fontSize: 16 }}>
                {selected.title}
              </h3>
              <div className="metagrid" style={{ marginBottom: 14 }}>
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
                <div className="note-box" style={{ marginBottom: 12 }}>
                  <b>Brief</b>
                  <p>{selected.body}</p>
                </div>
              ) : null}
              {selected.comment ? (
                <div className="note-box" style={{ marginBottom: 12 }}>
                  <b>Assignee note</b>
                  <p>{selected.comment}</p>
                </div>
              ) : null}
              {selected.statusOptions.length ? (
                <div
                  style={{
                    display: "flex",
                    flexWrap: "wrap",
                    gap: 8,
                    marginBottom: 12,
                  }}
                >
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
                  style={{ display: "grid", gap: 10, marginBottom: 12 }}
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
                    <span>Task note</span>
                    <textarea
                      name="comment"
                      maxLength={2000}
                      rows={3}
                      placeholder="Add a reply or update."
                      required
                    />
                  </label>
                  <button type="submit" className="btn p">
                    <Mic2 className="ic" aria-hidden="true" />
                    Add note
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
              <div className="lt-feed" style={{ maxHeight: "none" }}>
                {selected.notes.length ? (
                  selected.notes.map((note) => (
                    <FeedRow
                      key={note.note_id}
                      icon={Mic2}
                      tone="f-pur"
                      title={note.author_name || "Task note"}
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
      priority:
        task.status === "open"
          ? "High"
          : task.status === "in_progress"
            ? "Medium"
            : "Normal",
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

function KPI({
  label,
  value,
  detail,
  tone,
}: {
  label: string;
  value: string;
  detail: string;
  tone: Tone;
}) {
  return (
    <div className={`kpi lt-kpi k-${tone === "pur" ? "purple" : tone}`}>
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
      <div className="dl">{detail}</div>
    </div>
  );
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

function priorityTone(priority: TaskRow["priority"]): Tone {
  if (priority === "High") return "dng";
  if (priority === "Medium") return "warn";
  return "mut";
}

function initials(name: string): string {
  return name
    .split(/\s+/)
    .map((part) => part.charAt(0))
    .join("")
    .slice(0, 2)
    .toUpperCase();
}
