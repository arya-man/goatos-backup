import Alert from "@mui/material/Alert";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Typography from "@mui/material/Typography";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { DividedStack } from "@/components/app/divided-stack";
import { EmptyState } from "@/components/app/empty-state";
import { InvoiceAnalytic } from "@/components/app/sections/invoice/invoice-analytic";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { INVENTORY } from "./command-board-layout";
import { copy, optionLabel, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { listPCCareTasks, type ApiResult, type PCCareTask, type PCCareTaskPage } from "@/lib/api/server";
import { operationalLocationLabel } from "@/lib/operational-location";
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

function statusColor(task: PCCareTask): LabelColor {
  if (task.work_state === "delayed" || task.status === "rework") return "error";
  if (task.status === "pending_verification") return "info";
  if (task.status === "completed" || task.work_state === "completed") return "success";
  if (task.work_state === "scheduled") return "warning";
  return "default";
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
      const aLabel = a.task_label || a.operational_location_display || operationalLocationLabel({ shedName: a.shed_label, partitionLabel: a.partition_label });
      const bLabel = b.task_label || b.operational_location_display || operationalLocationLabel({ shedName: b.shed_label, partitionLabel: b.partition_label });
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
  const title = copy(pageContract, "section.inventory_progress.title");
  if (!result.ok) {
    return (
      <Card id="pc-care-inventory-progress" sx={{ scrollMarginTop: "calc(10 * var(--spacing))" }}>
        <CardHeader title={title} />
        <Alert severity="error" sx={{ m: 3 }}>{copy(pageContract, "inventory_progress.unavailable")}</Alert>
      </Card>
    );
  }

  const rows = visibleInventoryRows(result.data.items, asOf);
  const delayed = rows.filter((task) => task.work_state === "delayed").length;
  const waitingVerifier = rows.filter((task) => task.status === "pending_verification").length;
  const completed = rows.filter((task) => task.status === "completed" || task.work_state === "completed").length;
  const share = (value: number) => (rows.length ? Math.round((value / rows.length) * 100) : 0);
  const metrics = [
    { key: "current", title: copy(pageContract, "inventory_progress.metric.current"), value: rows.length, percent: 100, icon: "solar:file-check-bold-duotone" as const, color: "info.main" },
    { key: "overdue", title: copy(pageContract, "inventory_progress.metric.overdue"), value: delayed, percent: share(delayed), icon: "solar:clock-circle-bold" as const, color: "error.main" },
    { key: "verifier", title: copy(pageContract, "inventory_progress.metric.verifier"), value: waitingVerifier, percent: share(waitingVerifier), icon: "solar:shield-check-bold" as const, color: "warning.main" },
    { key: "done_today", title: copy(pageContract, "inventory_progress.metric.done_today"), value: completed, percent: share(completed), icon: "solar:videocamera-record-bold" as const, color: "success.main" },
  ];
  const headCells = tableLabels(pageContract, "inventory-vaccine-progress").map((label) => ({ id: label, label }));

  // Template invoice list card: CardHeader, the InvoiceAnalytic strip, the task table in the Scrollbar.
  return (
    <Card id="pc-care-inventory-progress" sx={{ scrollMarginTop: "calc(10 * var(--spacing))" }}>
      <CardHeader title={title} subheader={`${copy(pageContract, "inventory_progress.subtitle")} · ${fmtDate(asOf)}`} />
      <Scrollbar sx={{ minHeight: INVENTORY.stripMinHeight }}>
        <DividedStack dividerOrientation="vertical" direction="row" sx={{ py: 2 }}>
          {metrics.map((metric) => (
            <InvoiceAnalytic key={metric.key} title={metric.title} total={metric.value} percent={metric.percent} icon={metric.icon} color={metric.color} />
          ))}
        </DividedStack>
      </Scrollbar>

      {rows.length === 0 ? (
        <EmptyState title={copy(pageContract, "inventory_progress.empty")} sx={{ mx: 3, mb: 3 }} />
      ) : (
        <Scrollbar>
          <Table aria-label={title} sx={{ minWidth: INVENTORY.tableMinWidth }}>
            <TableHeadCustom headCells={headCells} />
            <TableBody>
              {rows.map((task) => (
                <TableRow key={task.task_id} hover>
                  <TableCell>{task.park_label}</TableCell>
                  <TableCell>
                    <Typography variant="body2" component="div">
                      {task.task_label || task.operational_location_display || operationalLocationLabel({ shedName: task.shed_label, partitionLabel: task.partition_label })}
                    </Typography>
                    {task.partition_label ? (
                      <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{task.partition_label}</Typography>
                    ) : null}
                  </TableCell>
                  <TableCell>{assigneeLine(pageContract, task)}</TableCell>
                  <TableCell>{requirementLine(pageContract, task)}</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(task.due_business_date)}</TableCell>
                  <TableCell>
                    <Stack direction="row" useFlexGap sx={{ flexWrap: "wrap", gap: 0.75 }}>
                      <Label color={statusColor(task)}>{optionLabel(pageContract, "inventory_task_work_states", task.work_state)}</Label>
                      <Label color={statusColor(task)}>{optionLabel(pageContract, "inventory_task_statuses", task.status)}</Label>
                    </Stack>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Scrollbar>
      )}
    </Card>
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
