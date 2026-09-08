import { CheckCircle2, ClipboardList, Clock3, Paperclip, Plus, UserRoundCheck } from "lucide-react";

type TaskRow = {
  id: string;
  number: string;
  title: string;
  status: "open" | "doing" | "done";
  assignee: string;
  assigneeRole: string;
  raisedBy: string;
  age: string;
  attachments: number;
  evidence: string;
};

const tasks: TaskRow[] = [
  { id: "1", number: "#18", title: "Check CPT west fence repair before evening close", status: "doing", assignee: "Satish", assigneeRole: "Park Head", raisedBy: "Manju", age: "Today", attachments: 3, evidence: "video + voice note" },
  { id: "2", number: "#17", title: "Confirm director handoff for feed unloading delay", status: "open", assignee: "Manohar", assigneeRole: "Feed Director", raisedBy: "Ravi", age: "Today", attachments: 2, evidence: "photo + file" },
  { id: "3", number: "#16", title: "Send Borewell-2 motor reading after restart", status: "done", assignee: "Prakash", assigneeRole: "Employee", raisedBy: "Manju", age: "Yesterday", attachments: 4, evidence: "completion video" },
];

const scopes = [
  { key: "assigned_to_me", label: "Assigned to me", count: 2 },
  { key: "assigned_by_me", label: "Assigned by me", count: 7 },
  { key: "team_progress", label: "Team progress", count: 18 },
];

const statusTone: Record<TaskRow["status"], string> = {
  open: "border-amber-400/30 bg-amber-400/10 text-amber-200",
  doing: "border-sky-400/30 bg-sky-400/10 text-sky-200",
  done: "border-emerald-400/30 bg-emerald-400/10 text-emerald-200",
};

export function LeadershipTasksPage() {
  const selected = tasks[0];
  return (
    <main className="min-h-screen bg-[#0b0f12] text-zinc-100">
      <div className="mx-auto flex w-full max-w-7xl flex-col gap-5 px-6 py-6">
        <header className="flex flex-wrap items-center justify-between gap-3 border-b border-white/10 pb-4">
          <div>
            <p className="text-xs font-semibold uppercase text-zinc-500">Operations</p>
            <h1 className="mt-1 text-2xl font-semibold tracking-normal text-white">Tasks</h1>
          </div>
          <button className="inline-flex h-10 items-center gap-2 rounded-md bg-emerald-500 px-4 text-sm font-semibold text-emerald-950 shadow-sm">
            <Plus className="h-4 w-4" />
            New task
          </button>
        </header>

        <section className="grid gap-3 md:grid-cols-3">
          {scopes.map((scope, index) => (
            <div key={scope.key} className={`rounded-md border p-4 ${index === 2 ? "border-emerald-400/50 bg-emerald-400/10" : "border-white/10 bg-white/[0.03]"}`}>
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold text-zinc-100">{scope.label}</span>
                <span className="text-2xl font-semibold text-white">{scope.count}</span>
              </div>
            </div>
          ))}
        </section>

        <section className="grid gap-4 lg:grid-cols-[1.35fr_0.9fr]">
          <div className="rounded-md border border-white/10 bg-white/[0.03]">
            <div className="grid grid-cols-4 gap-2 border-b border-white/10 p-3 text-xs font-semibold text-zinc-400">
              <span>Task</span>
              <span>Assignee</span>
              <span>Status</span>
              <span>Evidence</span>
            </div>
            <div className="divide-y divide-white/10">
              {tasks.map((task) => (
                <div key={task.id} className="grid grid-cols-4 gap-2 p-3 text-sm">
                  <div className="min-w-0">
                    <div className="font-semibold text-white">{task.number}</div>
                    <div className="truncate text-zinc-300">{task.title}</div>
                    <div className="text-xs text-zinc-500">Raised by {task.raisedBy} · {task.age}</div>
                  </div>
                  <div>
                    <div className="font-medium text-zinc-100">{task.assignee}</div>
                    <div className="text-xs text-zinc-500">{task.assigneeRole}</div>
                  </div>
                  <div>
                    <span className={`inline-flex rounded-md border px-2 py-1 text-xs font-semibold ${statusTone[task.status]}`}>{task.status === "doing" ? "Doing" : task.status === "done" ? "Done" : "Open"}</span>
                  </div>
                  <div className="flex items-center gap-2 text-zinc-300">
                    <Paperclip className="h-4 w-4 text-zinc-500" />
                    <span>{task.attachments}</span>
                    <span className="truncate text-xs text-zinc-500">{task.evidence}</span>
                  </div>
                </div>
              ))}
            </div>
          </div>

          <aside className="rounded-md border border-white/10 bg-white/[0.03] p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-xs font-semibold uppercase text-zinc-500">Selected</p>
                <h2 className="mt-1 text-lg font-semibold text-white">{selected.number}</h2>
              </div>
              <span className={`rounded-md border px-2 py-1 text-xs font-semibold ${statusTone[selected.status]}`}>Doing</span>
            </div>
            <p className="mt-4 text-base font-semibold text-white">{selected.title}</p>
            <div className="mt-4 grid gap-3 text-sm">
              <div className="flex items-center gap-3 text-zinc-300"><UserRoundCheck className="h-4 w-4 text-emerald-300" /> {selected.assignee} · {selected.assigneeRole}</div>
              <div className="flex items-center gap-3 text-zinc-300"><Clock3 className="h-4 w-4 text-sky-300" /> Opened today, moved to Doing</div>
              <div className="flex items-center gap-3 text-zinc-300"><ClipboardList className="h-4 w-4 text-amber-300" /> Details, video evidence, voice note attached</div>
              <div className="flex items-center gap-3 text-zinc-300"><CheckCircle2 className="h-4 w-4 text-emerald-300" /> Status, notes, and evidence visible from this row</div>
            </div>
          </aside>
        </section>
      </div>
    </main>
  );
}
