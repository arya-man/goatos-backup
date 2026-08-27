import { ClipboardCheck, Clock, ShieldCheck, Snowflake, Video } from "lucide-react";
import type { ReactNode } from "react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionLabel, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { listPCCareTasks, type ApiResult, type PCCareTask, type PCCareTaskPage } from "@/lib/api/server";
import { parseScope } from "@/lib/scope";
import type { RouteSearchParams } from "@/lib/search-params";

type InventoryWorkState = PCCareTask["work_state"];

const ACTIVE_STATES = new Set<InventoryWorkState>(["scheduled", "delayed"]);

function todayBusinessDate(pageContract: AdminUiPageContract) {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: copy(pageContract, "inventory_progress.time_zone"),
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

function requirementLine(pageContract: AdminUiPageContract, task: PCCareTask) {
  const reqs = task.inventory_requirements ?? [];
  if (!reqs.length) return copy(pageContract, "inventory_progress.empty_requirements");
  return reqs.map((r) => `${r.required_doses} ${r.vaccine_label}`).join(" · ");
}

function assigneeLine(pageContract: AdminUiPageContract, task: PCCareTask) {
  return task.assignee_names.length ? task.assignee_names.join(", ") : copy(pageContract, "label.unassigned");
}

function visibleInventoryRows(tasks: PCCareTask[], asOf: string) {
  return tasks
    .filter((task) => task.category === "inventory_vaccine")
    .filter((task) => ACTIVE_STATES.has(task.work_state) || (task.work_state === "completed" && task.due_business_date === asOf))
    .sort((a, b) => {
      const aDelayed = a.work_state === "delayed" ? 0 : 1;
      const bDelayed = b.work_state === "delayed" ? 0 : 1;
      const aLabel = a.task_label || a.operational_location_display || a.shed_label;
      const bLabel = b.task_label || b.operational_location_display || b.shed_label;
      return aDelayed - bDelayed || a.due_business_date.localeCompare(b.due_business_date) || aLabel.localeCompare(bLabel);
    });
}

async function listAllInventoryTasks(params: {
  date: string;
  parkId?: string;
  category: "inventory_vaccine";
}): Promise<ApiResult<PCCareTaskPage>> {
  const items: PCCareTask[] = [];
  let cursor = "";
  for (let page = 0; page < 20; page += 1) { // scale-guard:ignore: inventory-vaccine dashboard drains only PC-care stock tasks for one date/carry window, capped at 20x100
    // serial-await: allow cursor pagination; each page depends on the previous next_cursor.
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
  pageContract,
}: {
  result: ApiResult<PCCareTaskPage>;
  asOf: string;
  pageContract: AdminUiPageContract;
}) {
  if (!result.ok) {
    return (
      <section className="card" id="pc-care-inventory-progress" style={{ scrollMarginTop: 80 }}>
        <div className="hd">
          <h2>{copy(pageContract, "section.inventory_progress.title")}</h2>
        </div>
        <div className="bd">
          <div className="muted">{copy(pageContract, "inventory_progress.unavailable")}</div>
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
          <h2 style={{ margin: 0 }}>{copy(pageContract, "section.inventory_progress.title")}</h2>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">{copy(pageContract, "inventory_progress.subtitle")} · {fmtDate(asOf)}</span>
      </div>
      <div className="bd" style={{ display: "grid", gap: 12 }}>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(150px, 1fr))",
            gap: 10,
          }}
        >
          {metric(copy(pageContract, "inventory_progress.metric.current"), rows.length, <ClipboardCheck className="ic" aria-hidden="true" />)}
          {metric(copy(pageContract, "inventory_progress.metric.overdue"), delayed, <Clock className="ic" aria-hidden="true" />)}
          {metric(copy(pageContract, "inventory_progress.metric.verifier"), waitingVerifier, <ShieldCheck className="ic" aria-hidden="true" />)}
          {metric(copy(pageContract, "inventory_progress.metric.done_today"), completed, <Video className="ic" aria-hidden="true" />)}
        </div>

        {rows.length === 0 ? (
          <div className="muted" style={{ padding: "10px 2px" }}>
            {copy(pageContract, "inventory_progress.empty")}
          </div>
        ) : (
          <div style={{ overflowX: "auto" }}>
            <table className="data-table">
              <thead>
                <tr>
                  {tableLabels(pageContract, "inventory-vaccine-progress").map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((task) => (
                  <tr key={task.task_id}>
                    <td>{task.park_label}</td>
                    <td>
                      <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
                        <span>{task.task_label || task.operational_location_display || task.shed_label}</span>
                        {task.partition_label ? <span className="small muted">{task.partition_label}</span> : null}
                      </div>
                    </td>
                    <td>{assigneeLine(pageContract, task)}</td>
                    <td>{requirementLine(pageContract, task)}</td>
                    <td>{fmtDate(task.due_business_date)}</td>
                    <td>
                      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                        <Tag tone={statusTone(task)}>{optionLabel(pageContract, "inventory_task_work_states", task.work_state)}</Tag>
                        <Tag tone={statusTone(task)}>{optionLabel(pageContract, "inventory_task_statuses", task.status)}</Tag>
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

export async function InventoryVaccineProgressSection({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const scope = parseScope(searchParams ?? {});
  const asOf = todayBusinessDate(pageContract);
  const result = await listAllInventoryTasks({
    date: asOf,
    parkId: scope.parkId,
    category: "inventory_vaccine",
  });
  return <InventoryProgressContent result={result} asOf={asOf} pageContract={pageContract} />;
}
