import { redirect } from "next/navigation";
import { AlertTriangle, CheckCircle2, ListChecks, ShieldCheck, UserPlus } from "lucide-react";
import { ActionNotice, EmptyPanel, ErrorPanel, FormField, FormSelect, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { formatLabel } from "@/lib/display-utils";
import { dateTime, shortId } from "@/lib/format";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getTask,
  listTasks,
  type TaskListResponse,
  type TaskResponse,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { assignTaskAction, createTaskAction, reworkTaskAction, verifyTaskAction } from "./actions";

type TaskRow = TaskListResponse["items"][number];
type SubmissionRow = NonNullable<TaskResponse["submissions"]>[number];

export async function TasksPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const selectedID = one(searchParams, "task_id");
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const filters = {
    limit: 100,
    state: one(searchParams, "state"),
    assigned_to: one(searchParams, "assigned_to"),
  };
  const tasks = await listTasks(filters);
  const authError = firstAuthRequiredError(tasks);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  if (!tasks.ok) {
    return (
      <>
        <Header />
        <ErrorPanel error={tasks.error} />
      </>
    );
  }

  const selected = selectedID ?? tasks.data.items[0]?.task_id;
  const detail = selected ? await getTask(selected) : null;
  const secondAuthError = firstAuthRequiredError(detail);
  if (secondAuthError) redirect(INTERNAL_LOGIN_PATH);

  return (
    <>
      <Header />
      <ActionNotice status={actionStatus} message={actionMessage} />
      <FilterBar filters={filters} />
      <TasksBody
        tasks={tasks.data.items}
        selected={detail?.ok ? detail.data : null}
        selectedError={detail && !detail.ok ? detail.error : null}
        returnTo={returnPath(searchParams)}
      />
    </>
  );
}

function Header() {
  return (
    <PageHeader
      eyebrow="Task Engine"
      title="Tasks"
      description="Assigned SOP work, proof review, rework requests, and Shifting submission history."
    />
  );
}

function FilterBar({ filters }: { filters: { state?: string; assigned_to?: string } }) {
  return (
    <form className="mb-5 grid gap-3 rounded-xl border border-[#334155] bg-[#1A1D24] p-4 md:grid-cols-[180px_minmax(0,1fr)_auto]" action="/tasks">
      <FormSelect name="state" label="State" defaultValue={filters.state} options={["queued", "assigned", "in_progress", "needs_review", "rework_requested", "accepted", "rejected", "canceled"]} />
      <FormField name="assigned_to" label="Assigned To" defaultValue={filters.assigned_to} placeholder="Operator user UUID" />
      <button type="submit" className="mt-5 h-10 rounded-lg border border-[#14F1D9]/50 px-4 text-sm font-bold text-[#14F1D9] hover:border-[#14F1D9] md:mt-6">
        Apply
      </button>
    </form>
  );
}

function TasksBody({
  tasks,
  selected,
  selectedError,
  returnTo,
}: {
  tasks: TaskRow[];
  selected: TaskResponse | null;
  selectedError: Parameters<typeof ErrorPanel>[0]["error"] | null;
  returnTo: string;
}) {
  const needsReview = tasks.filter((task) => task.state === "needs_review").length;
  const rework = tasks.filter((task) => task.state === "rework_requested").length;
  const accepted = tasks.filter((task) => task.state === "accepted").length;

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Tasks" value={tasks.length} detail="bounded queue" icon={<ListChecks size={18} />} />
        <StatCard label="Proof Review" value={needsReview} detail="waiting verifier" icon={<ShieldCheck size={18} />} tone={needsReview > 0 ? "warn" : "neutral"} />
        <StatCard label="Rework" value={rework} detail="sent back" icon={<AlertTriangle size={18} />} tone={rework > 0 ? "warn" : "neutral"} />
        <StatCard label="Accepted" value={accepted} detail="movement handoff" icon={<CheckCircle2 size={18} />} tone={accepted > 0 ? "good" : "neutral"} />
      </div>

      <div className="mt-6 grid gap-5 xl:grid-cols-[minmax(0,1.2fr)_minmax(420px,0.8fr)]">
        <Panel title="Task Queue" action={<CreateTaskForm returnTo={returnTo} />}>
          {tasks.length === 0 ? (
            <EmptyPanel message="No tasks returned." />
          ) : (
            <div className="overflow-x-auto">
              <table className="min-w-full text-left text-sm">
                <thead className="text-xs uppercase text-[#8899AA]">
                  <tr>
                    <th className="px-3 py-2 font-semibold">Task</th>
                    <th className="px-3 py-2 font-semibold">State</th>
                    <th className="px-3 py-2 font-semibold">Scope</th>
                    <th className="px-3 py-2 font-semibold">Due</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-[#334155] text-[#E0E8F0]">
                  {tasks.map((task) => (
                    <tr key={task.task_id} className={selected?.task.task_id === task.task_id ? "bg-[rgba(20,241,217,0.06)]" : undefined}>
                      <td className="px-3 py-3">
                        <a href={`/tasks?task_id=${encodeURIComponent(task.task_id)}`} className="font-semibold text-white hover:text-[#14F1D9]">
                          {task.title}
                        </a>
                        <div className="mt-1 text-xs text-[#8899AA]">{task.sop_code} · {formatLabel(task.task_type)} · {shortId(task.task_id)}</div>
                      </td>
                      <td className="px-3 py-3"><StatusBadge status={task.state} /></td>
                      <td className="px-3 py-3 text-[#B0BEC5]">{task.scope_type} · {shortId(task.scope_id)}</td>
                      <td className="px-3 py-3 text-[#8899AA]">{task.due_at ? dateTime(task.due_at) : "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel title="Task Detail">
          {selectedError ? (
            <ErrorPanel error={selectedError} />
          ) : selected ? (
            <TaskDetail data={selected} returnTo={returnTo} />
          ) : (
            <EmptyPanel message="Select a task." />
          )}
        </Panel>
      </div>
    </>
  );
}

function TaskDetail({ data, returnTo }: { data: TaskResponse; returnTo: string }) {
  const task = data.task;
  return (
    <div className="space-y-5">
      <div>
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-lg font-bold text-white">{task.title}</h2>
            <p className="mt-1 text-sm text-[#93a4b8]">{task.description || `${task.sop_code} ${formatLabel(task.task_type)}`}</p>
          </div>
          <StatusBadge status={task.state} />
        </div>
        <div className="mt-4 grid gap-2 sm:grid-cols-3">
          <StatPill label="Priority" value={formatLabel(task.priority)} tone={task.priority === "urgent" || task.priority === "high" ? "warn" : "neutral"} />
          <StatPill label="Version" value={shortId(task.sop_version_id)} />
          <StatPill label="Updated" value={dateTime(task.updated_at)} />
        </div>
      </div>

      <section className="rounded-lg border border-[#334155] bg-[#11151C] p-4">
        <div className="mb-3 text-sm font-bold text-white">Actions</div>
        <div className="grid gap-3">
          <form action={assignTaskAction} className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
            <input type="hidden" name="return_to" value={returnTo} />
            <input type="hidden" name="task_id" value={task.task_id} />
            <input type="hidden" name="row_version" value={task.row_version} />
            <FormField name="assigned_to" label="Assign To" defaultValue={task.assigned_to ?? ""} placeholder="Operator user UUID" required />
            <button type="submit" className="mt-5 inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-[#14f1d9]/50 px-3 text-sm font-bold text-[#14f1d9] hover:border-[#14f1d9]">
              <UserPlus size={16} /> Assign
            </button>
          </form>
          <div className="flex flex-wrap gap-2">
            <form action={verifyTaskAction}>
              <input type="hidden" name="return_to" value={returnTo} />
              <input type="hidden" name="task_id" value={task.task_id} />
              <input type="hidden" name="row_version" value={task.row_version} />
              <input type="hidden" name="reason" value="Proof accepted from task review." />
              <ConfirmSubmitButton message="Accept proof and create movement handoff?" className="h-9 rounded-md border border-[#22c55e]/60 px-3 text-sm font-bold text-[#86efac] hover:border-[#22c55e]">
                Verify
              </ConfirmSubmitButton>
            </form>
            <form action={reworkTaskAction}>
              <input type="hidden" name="return_to" value={returnTo} />
              <input type="hidden" name="task_id" value={task.task_id} />
              <input type="hidden" name="row_version" value={task.row_version} />
              <input type="hidden" name="reason" value="Proof requires operator rework." />
              <ConfirmSubmitButton message="Request rework for this task?" className="h-9 rounded-md border border-[#ef4444]/50 px-3 text-sm font-bold text-[#fca5a5] hover:border-[#ef4444]">
                Rework
              </ConfirmSubmitButton>
            </form>
          </div>
        </div>
      </section>

      <section>
        <h3 className="text-sm font-bold text-white">Submissions</h3>
        {data.submissions && data.submissions.length > 0 ? (
          <div className="mt-3 space-y-3">
            {data.submissions.map((submission) => (
              <SubmissionCard key={submission.submission_id} submission={submission} />
            ))}
          </div>
        ) : (
          <div className="mt-3"><EmptyPanel message="No submissions yet." /></div>
        )}
      </section>
    </div>
  );
}

function CreateTaskForm({ returnTo }: { returnTo: string }) {
  return (
    <form action={createTaskAction} className="grid gap-2 lg:grid-cols-[160px_160px_180px_120px_auto]">
      <input type="hidden" name="return_to" value={returnTo} />
      <input type="hidden" name="sop_code" value="shifting" />
      <input type="hidden" name="task_type" value="shifting_direction" />
      <FormField name="title" label="Title" placeholder="Shift goats" required />
      <FormField name="scope_id" label="Scope ID" placeholder="Park UUID" required />
      <FormField name="assigned_to" label="Assignee" placeholder="User UUID" />
      <FormSelect name="priority" label="Priority" options={["low", "normal", "high", "urgent"]} defaultValue="normal" emptyLabel="Priority" />
      <button type="submit" className="mt-5 h-10 rounded-lg border border-[#14f1d9]/50 px-3 text-sm font-bold text-[#14f1d9] hover:border-[#14f1d9]">
        Create
      </button>
    </form>
  );
}

function SubmissionCard({ submission }: { submission: SubmissionRow }) {
  return (
    <div className="rounded-lg border border-[#334155] bg-[#11151C] p-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="font-semibold text-white">{shortId(submission.submission_id)}</div>
          <div className="mt-1 text-xs text-[#93a4b8]">{dateTime(submission.submitted_at)}</div>
        </div>
        <StatusBadge status={submission.state} />
      </div>
      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        <StatPill label="Items" value={submission.items.length} tone={submission.items.some((item) => item.state === "needs_review") ? "warn" : "good"} />
        <StatPill label="Proof" value={submission.proof_refs.length} tone={submission.proof_refs.length > 0 ? "good" : "warn"} />
      </div>
      <pre tabIndex={0} className="mt-3 max-h-44 overflow-auto rounded border border-[#334155] bg-[#0f1115] p-3 text-xs text-[#93a4b8]">{JSON.stringify(submission.answers, null, 2)}</pre>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const good = status === "accepted";
  const warn = status === "needs_review" || status === "rework_requested" || status === "assigned";
  return (
    <span className={`inline-flex rounded border px-2 py-1 text-[10px] font-semibold uppercase ${good ? "border-[#1f8f65] text-[#86efac]" : warn ? "border-[#a16207] text-[#facc15]" : "border-[#334155] text-[#c7d1dc]"}`}>
      {formatLabel(status)}
    </span>
  );
}

function StatCard({
  label,
  value,
  detail,
  icon,
  tone = "neutral",
}: {
  label: string;
  value: number;
  detail: string;
  icon: React.ReactNode;
  tone?: "neutral" | "good" | "warn";
}) {
  const toneClass = tone === "good" ? "text-[#4ade80]" : tone === "warn" ? "text-[#fb923c]" : "text-[#14F1D9]";
  return (
    <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className={`text-[10px] font-medium uppercase tracking-wider ${toneClass}`}>{label}</p>
          <p className="mt-1.5 text-2xl font-bold text-white">{value.toLocaleString("en-IN")}</p>
          <p className="mt-1 text-xs text-[#8899AA]">{detail}</p>
        </div>
        <div className={`flex h-9 w-9 items-center justify-center rounded-lg bg-[#22262E] ${toneClass}`}>{icon}</div>
      </div>
    </div>
  );
}

function returnPath(searchParams: RouteSearchParams): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (typeof value === "string") params.set(key, value);
  }
  const qs = params.toString();
  return qs ? `/tasks?${qs}` : "/tasks";
}
