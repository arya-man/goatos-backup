import { ClipboardCheck, Clock, ShieldCheck, Snowflake, Video } from "lucide-react";
import type { ReactNode } from "react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { listPCCareTasks, type ApiResult, type PCCareTask, type PCCareTaskPage } from "@/lib/api/server";
import { parseScope } from "@/lib/scope";
import type { RouteSearchParams } from "@/lib/search-params";

type InventoryStatusKey = "open" | "pending_verification" | "completed" | "rework";
type InventoryWorkState = PCCareTask["work_state"];

const STATUS_LABELS: Record<InventoryStatusKey, string> = {
  open: "Open",
  pending_verification: "Verifier review",
  completed: "Done",
  rework: "Rework",
};

const WORK_LABELS: Record<InventoryWorkState, string> = {
  scheduled: "Scheduled",
  delayed: "Overdue",
  completed: "Done",
  closed: "Closed",
  canceled: "Canceled",
};

const ACTIVE_STATES = new Set<InventoryWorkState>(["scheduled", "delayed"]);

function todayBusinessDate() {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
}

function statusTone(task: PCCareTask): Tone {
  if (task.work_state === "delayed" || task.status === "rework") return "dng";
  if (task.status === "pending_verification") return "info";
  if (task.status === "completed" || task.work_state === "completed") return "ok";
  if (task.work_state === "scheduled") return "warn";
  return "mut";
}

function metric(label: string, value: number, icon: ReactNode) {
  return (
    <div className="card" style={{ padding: 12, borderRadius: 8, minWidth: 0 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        {icon}
        <span className="small muted">{label}</span>
      </div>
      <div style={{ marginTop: 4, fontSize: 24, fontWeight: 700, lineHeight: 1 }}>{value}</div>
    </div>
  );
}

function requirementLine(task: PCCareTask) {
  const reqs = task.inventory_requirements ?? [];
  if (!reqs.length) return "No vaccine stock requirement lines";
  return reqs.map((r) => `${r.required_doses} ${r.vaccine_label}`).join(" · ");
}

function assigneeLine(task: PCCareTask) {
  return task.assignee_names.length ? task.assignee_names.join(", ") : "Unassigned";
}

function visibleInventoryRows(tasks: PCCareTask[], asOf: string) {
  return tasks
    .filter((task) => task.category === "inventory_vaccine")
    .filter((task) => ACTIVE_STATES.has(task.work_state) || (task.work_state === "completed" && task.due_business_date === asOf))
    .sort((a, b) => {
      const aDelayed = a.work_state === "delayed" ? 0 : 1;
      const bDelayed = b.work_state === "delayed" ? 0 : 1;
      return aDelayed - bDelayed || a.due_business_date.localeCompare(b.due_business_date) || a.shed_label.localeCompare(b.shed_label);
    });
}

async function listAllInventoryTasks(params: {
  date: string;
  parkId?: string;
  category: "inventory_vaccine";
}): Promise<ApiResult<PCCareTaskPage>> {
  const items: PCCareTask[] = [];
  let cursor = "";
  for (let page = 0; page < 20; page += 1) {
    const result = await listPCCareTasks({
      date: params.date,
      parkId: params.parkId,
      category: params.category,
      limit: 100,
      cursor,
      currentOrCarry: true,
    });
    if (!result.ok) return result;
    items.push(...result.data.items);
    cursor = result.data.next_cursor ?? "";
    if (!cursor) {
      return { ok: true, data: { items, next_cursor: "" } };
    }
  }
  return { ok: true, data: { items, next_cursor: cursor } };
}

function InventoryProgressContent({
  result,
  asOf,
}: {
  result: ApiResult<PCCareTaskPage>;
  asOf: string;
}) {
  if (!result.ok) {
    return (
      <section className="card" id="pc-care-inventory-progress" style={{ scrollMarginTop: 80 }}>
        <div className="hd">
          <h2>Vaccine fridge stock checks</h2>
        </div>
        <div className="bd">
          <div className="muted">Inventory task progress is unavailable right now.</div>
        </div>
      </section>
    );
  }

  const rows = visibleInventoryRows(result.data.items, asOf);
  const delayed = rows.filter((task) => task.work_state === "delayed").length;
  const waitingVerifier = rows.filter((task) => task.status === "pending_verification").length;
  const completed = rows.filter((task) => task.status === "completed" || task.work_state === "completed").length;

  return (
    <section className="card" id="pc-care-inventory-progress" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
          <Snowflake className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h2 style={{ margin: 0 }}>Vaccine fridge stock checks</h2>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">PC Care director tasks · {fmtDate(asOf)}</span>
      </div>
      <div className="bd" style={{ display: "grid", gap: 12 }}>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
            gap: 10,
          }}
        >
          {metric("Current or carry-over", rows.length, <ClipboardCheck className="ic" aria-hidden="true" />)}
          {metric("Overdue", delayed, <Clock className="ic" aria-hidden="true" />)}
          {metric("Verifier review", waitingVerifier, <ShieldCheck className="ic" aria-hidden="true" />)}
          {metric("Done today", completed, <Video className="ic" aria-hidden="true" />)}
        </div>

        {rows.length === 0 ? (
          <div className="muted" style={{ padding: "10px 2px" }}>
            No current or carry-over vaccine inventory tasks.
          </div>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table className="data-table">
              <thead>
                <tr>
                  <th>Farm</th>
                  <th>Shed</th>
                  <th>Director</th>
                  <th>Vaccines</th>
                  <th>Due</th>
                  <th>State</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((task) => (
                  <tr key={task.task_id}>
                    <td>{task.park_label}</td>
                    <td>
                      <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                        <span>{task.operational_location_display || task.shed_label}</span>
                        {task.partition_label ? <span className="small muted">{task.partition_label}</span> : null}
                      </div>
                    </td>
                    <td>{assigneeLine(task)}</td>
                    <td>{requirementLine(task)}</td>
                    <td>{fmtDate(task.due_business_date)}</td>
                    <td>
                      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                        <Tag tone={statusTone(task)}>{WORK_LABELS[task.work_state]}</Tag>
                        <Tag tone={statusTone(task)}>{STATUS_LABELS[task.status]}</Tag>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  );
}

export async function InventoryVaccineProgressSection({ searchParams }: { searchParams?: RouteSearchParams }) {
  const scope = parseScope(searchParams ?? {});
  const asOf = todayBusinessDate();
  const result = await listAllInventoryTasks({
    date: asOf,
    parkId: scope.parkId,
    category: "inventory_vaccine",
  });
  return <InventoryProgressContent result={result} asOf={asOf} />;
}
