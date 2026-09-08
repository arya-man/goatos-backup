import { CheckCircle2, ClipboardList, Clock3, Mic2, Paperclip, Plus, UserRoundCheck, Video } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import type { LeadershipTaskPage } from "@/lib/api/server";

type TaskStatus = "open" | "doing" | "done";

type TaskRow = {
  id: string;
  number: string;
  title: string;
  status: TaskStatus;
  assignee: string;
  assigneeRole: string;
  raisedBy: string;
  age: string;
  attachments: number;
  evidence: string;
  priority: "High" | "Medium" | "Normal";
};

type ScopeRow = {
  key: string;
  label: string;
  count: number;
  detail: string;
  tone: Tone;
  emptyMessage?: string;
};

const fixtureTasks: TaskRow[] = [
  {
    id: "1",
    number: "#18",
    title: "Check CPT west fence repair before evening close",
    status: "doing",
    assignee: "Satish",
    assigneeRole: "Park Head",
    raisedBy: "Manju",
    age: "Today",
    attachments: 3,
    evidence: "video, voice note",
    priority: "High",
  },
  {
    id: "2",
    number: "#17",
    title: "Confirm director handoff for feed unloading delay",
    status: "open",
    assignee: "Manohar",
    assigneeRole: "Feed Director",
    raisedBy: "Ravi",
    age: "Today",
    attachments: 2,
    evidence: "note, file",
    priority: "Medium",
  },
  {
    id: "3",
    number: "#16",
    title: "Send Borewell-2 motor reading after restart",
    status: "done",
    assignee: "Prakash",
    assigneeRole: "Employee",
    raisedBy: "Manju",
    age: "Yesterday",
    attachments: 4,
    evidence: "completion video",
    priority: "Normal",
  },
];

const fixtureScopes: ScopeRow[] = [
  { key: "assigned_to_me", label: "Assigned to me", count: 2, detail: "My action queue", tone: "info" },
  { key: "assigned_by_me", label: "Assigned by me", count: 7, detail: "Follow-ups I raised", tone: "warn" },
  { key: "team_progress", label: "Team progress", count: 18, detail: "Open team work", tone: "ok" },
];

export function LeadershipTasksPage({ page, preview = false }: { page?: LeadershipTaskPage | null; preview?: boolean }) {
  const tasks = page ? rowsFromPage(page) : preview ? fixtureTasks : [];
  const scopes = page?.scopes?.length ? scopesFromPage(page) : preview ? fixtureScopes : [];
  const selected = tasks[0];
  const selectedScope = scopes.find((scope) => scope.key === "team_progress") ?? scopes.find((scope) => scope.key === "assigned_by_me") ?? scopes[0];

  return (
    <div className="screen on lt-page">
      <div className="phead lt-phead">
        <div>
          <div className="crumb">
            Operations / <b>Tasks</b>
          </div>
          <h1>{page?.title || "Tasks"}</h1>
          <div className="sub">
            {preview ? "Preview data" : "Live backend data"} / Manual work assigned across directors, park heads, and employees.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn p">
          <Plus className="ic" aria-hidden="true" />
          New task
        </button>
      </div>

      <div className="lt-kpis">
        {scopes.map((scope) => (
          <KPI key={scope.key} label={scope.label} value={String(scope.count)} detail={scope.detail} tone={scope.tone} />
        ))}
        {!scopes.length ? <KPI label="Team progress" value="0" detail="Live data unavailable" tone="warn" /> : null}
      </div>

      <div className="subtabs" style={{ marginBottom: 14 }} aria-label="Task scopes">
        {scopes.map((scope) => (
          <button key={scope.key} type="button" className={scope.key === selectedScope?.key ? "on" : ""}>
            {scope.label}
            <span className="cbq">{scope.count}</span>
          </button>
        ))}
      </div>

      <div className="lt-grid">
        <section className="card lt-card" style={{ minWidth: 0 }}>
          <div className="hd">
            <ClipboardList className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Team progress</h3>
            <div className="sp" style={{ flex: 1 }} />
            <Tag tone="info">CEO view</Tag>
          </div>
          <div className="bd lt-tablewrap" tabIndex={0} role="group" aria-label="Leadership task progress">
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
                      <span className="muted small">{selectedScope?.emptyMessage || "No tasks in this scope."}</span>
                    </td>
                  </tr>
                ) : null}
                {tasks.map((task) => (
                  <tr key={task.id}>
                    <td>
                      <b>{task.number}</b>
                      <div className="muted small" style={{ maxWidth: 320 }}>
                        {task.title}
                      </div>
                    </td>
                    <td>
                      <div className="lt-opname">
                        <span className="lt-avx">{initials(task.assignee)}</span>
                        <span>
                          {task.assignee}
                          <span className="lt-code muted">{task.assigneeRole}</span>
                        </span>
                      </div>
                    </td>
                    <td>{task.raisedBy}</td>
                    <td>
                      <Tag tone={statusTone(task.status)}>{statusLabel(task.status)}</Tag>
                    </td>
                    <td>
                      <span style={{ display: "inline-flex", alignItems: "center", gap: 7, minWidth: 0 }}>
                        <Paperclip className="ic" style={{ width: 15, color: "var(--muted)" }} aria-hidden="true" />
                        <b>{task.attachments}</b>
                        <span className="muted small">{task.evidence}</span>
                      </span>
                    </td>
                    <td>
                      <Tag tone={priorityTone(task.priority)}>{task.priority}</Tag>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        {selected ? (
        <aside className="card lt-card">
          <div className="hd">
            <UserRoundCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Selected task</h3>
            <div className="sp" style={{ flex: 1 }} />
            <Tag tone={statusTone(selected.status)}>{statusLabel(selected.status)}</Tag>
          </div>
          <div className="bd">
            <div className="crumb">
              <b>{selected.number}</b> / {selected.age}
            </div>
            <h3 style={{ margin: "7px 0 12px", fontSize: 16 }}>{selected.title}</h3>
            <div className="metagrid" style={{ marginBottom: 14 }}>
              <Meta label="Assignee" value={`${selected.assignee} - ${selected.assigneeRole}`} />
              <Meta label="Raised by" value={selected.raisedBy} />
              <Meta label="Evidence" value={selected.evidence} />
              <Meta label="Attachments" value={String(selected.attachments)} />
            </div>
            <div className="lt-feed" style={{ maxHeight: "none" }}>
              <FeedRow icon={Clock3} tone="f-info" title="Opened today" detail="Moved into Doing after park-head acknowledgement." />
              <FeedRow icon={Mic2} tone="f-pur" title="Voice note attached" detail="Supervisor note is stored against the task comments." />
              <FeedRow icon={Video} tone="f-warn" title="Video evidence attached" detail="Completion proof can be reviewed before closing." />
              <FeedRow icon={CheckCircle2} tone="f-ok" title="Monitoring ready" detail="Status, notes, and files stay visible in the same task row." />
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
    const status = task.status === "done" ? "done" : task.status === "in_progress" ? "doing" : "open";
    const evidence = task.attachments?.map((attachment) => attachment.kind).filter(Boolean).join(", ");
    return {
      id: task.task_id,
      number: task.number_label,
      title: task.title,
      status,
      assignee: task.assignee_name,
      assigneeRole: task.is_assignee ? "Assigned to me" : "Assignee",
      raisedBy: task.raised_by_name,
      age: task.raised_on_label,
      attachments: task.attachment_count,
      evidence: evidence || (task.attachment_count > 0 ? "attached files" : "no attachments"),
      priority: task.status === "open" ? "High" : task.status === "in_progress" ? "Medium" : "Normal",
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
    emptyMessage: scope.empty_message,
  }));
}

function KPI({ label, value, detail, tone }: { label: string; value: string; detail: string; tone: Tone }) {
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
          <Icon className="ic" style={{ width: 14, verticalAlign: -2, marginRight: 5 }} aria-hidden="true" />
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
