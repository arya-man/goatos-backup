import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import { WF_DETAIL_GRID } from "./workflow-drilldown-layout";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import LinearProgress from "@mui/material/LinearProgress";
import { Iconify } from "@/components/minimal/iconify";
import { LinkButton } from "@/components/app/link-button";
import { OrderDetailsToolbar } from "@/components/app/sections/order/order-details-toolbar";
import { OrderDetailsHistory, type OrderHistoryItem, type OrderHistoryTone } from "@/components/app/sections/order/order-details-history";
import { OrderDetailsCustomer } from "@/components/app/sections/order/order-details-customer";
import { detailWrapSx } from "@/components/app/detail-wrap";
import { OrderDetailsDelivery } from "@/components/app/sections/order/order-details-delivery";
import type { LabelColor } from "@/components/minimal/label";
import { operationalLocationLabel } from "@/lib/operational-location";
import { getVaccinationWorkflowDrilldown, type WorkflowNode } from "@/lib/api/server";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { type Tone } from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDateTime } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { actionDriveLabel, actionWorkTitle } from "./work-board";
import { stageLabel } from "@/lib/stage-labels";
import { humanizeEnum } from "@/lib/format";
import Alert from "@mui/material/Alert";

// Template order details view (sections/order/view/order-details-view.tsx): OrderDetailsToolbar
// (back, title, status Label, date line, actions) over Grid md 8 / md 4 — the obligation chain on
// the OrderDetailsHistory timeline (with its dashed summary) on the left, the right-rail Card of
// OrderDetailsCustomer / OrderDetailsDelivery blocks split by dashed Dividers.

const TONE_LABEL: Record<Tone, LabelColor> = { ok: "success", warn: "warning", dng: "error", info: "info", mut: "default", pur: "secondary", teal: "info" };

// Heuristic tone for a free-text node state — done/accepted/published → ok, rejected/blocked → dng, etc.
export function nodeTone(state: string): Tone {
  const s = normalizeState(state);
  if (/(reject|block|fail|overdue)/.test(s)) return "dng";
  if (/(pending|await|progress|submitted|uploaded|open)/.test(s)) return "warn";
  if (s === "missing" || s.includes("missing")) return "warn";
  if (isDone(state)) return "ok";
  if (s === "planned") return "info";
  if (/(not_started|not started|scheduled|n\/a|skipped)/.test(s)) return "mut";
  return "info";
}

function isDone(state: string): boolean {
  return new Set(["accepted", "complete", "completed", "done", "published", "posted", "verified", "generated"]).has(normalizeState(state));
}

function isBlocked(node: WorkflowNode): boolean {
  return !!node.blocker || /(reject|block|fail|overdue)/.test(normalizeState(node.state));
}

function normalizeState(state: string): string {
  return state.trim().toLowerCase().replaceAll("-", "_").replaceAll(" ", "_");
}

// The canonical obligation chain as template timeline rows: done steps fill primary, the first
// not-done step is the current one (info, or error when it is blocked), the rest stay grey.
function chainTimeline(nodes: WorkflowNode[]): OrderHistoryItem[] {
  const firstPendingIdx = nodes.findIndex((n) => !isDone(n.state));
  return nodes.map((node, index) => {
    const done = isDone(node.state);
    const blocked = isBlocked(node) && !done;
    const tone: OrderHistoryTone = done ? "primary" : blocked ? "error" : index === firstPendingIdx ? "info" : "grey";
    const who = [node.actor, node.owner].filter(Boolean).join(" · ");
    return {
      key: node.key,
      title: node.label,
      tone,
      body: (
        <Box component="span" sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
          <Tag tone={nodeTone(node.state)}>{humanizeEnum(node.state)}</Tag>
          {who ? <span>{who}</span> : null}
          {node.evidence ? (
            <Tag tone="teal">
              <Iconify icon="solar:videocamera-record-bold" width={14} sx={{ mr: 0.5 }} />
              {node.evidence}
            </Tag>
          ) : null}
          {node.blocker ? (
            <Box component="span" sx={{ color: "error.main", display: "inline-flex", alignItems: "center", gap: 0.5 }}>
              <Iconify icon="solar:forbidden-circle-bold" width={16} />
              {node.blocker}
            </Box>
          ) : null}
        </Box>
      ),
      time: node.timestamp ? fmtDateTime(node.timestamp) : undefined,
    };
  });
}

export async function VaccinationWorkflowDrilldownPage({
  rowId,
  searchParams,
  pageContract,
}: {
  rowId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const from = one(sp, "from");
  const backHref =
    from === "control-tower"
      ? scopeHref("/", scope, {}, { lens: "control-tower", ct_alert: rowId })
      : from === "action-center"
        ? scopeHref("/action-center", scope, {}, { ac_row: rowId })
        : scopeHref("/workflows", scope);
  const backLabel =
    from === "control-tower"
      ? copy(pageContract, "action.back_control")
      : from === "action-center"
        ? copy(pageContract, "action.back_action")
        : copy(pageContract, "action.back_workflows");
  const result = await getVaccinationWorkflowDrilldown(rowId);

  if (!result.ok) {
    return (
      <div className="screen on">
        <OrderDetailsToolbar title={pageContract.title || copy(pageContract, "fallback.title")} subtitle={copy(pageContract, "crumb")} backHref={backHref} backLabel={backLabel} />
        <Alert severity="error" role="alert">
          <b>{copy(pageContract, "error.row_unavailable", "This workflow record could not be opened.")}</b>
          <Box component="span" sx={{ ml: 1, color: "text.secondary" }}>{result.error.code ?? result.error.kind}</Box>
        </Alert>
        <Box>
          <LinkButton href={backHref} variant="outlined" color="inherit" startIcon={<Iconify icon="eva:arrow-ios-back-fill" />}>
            {backLabel}
          </LinkButton>
        </Box>
      </div>
    );
  }

  const { row, nodes } = result.data;
  const title = actionWorkTitle(pageContract, row);
  const drive = actionDriveLabel(pageContract, row);
  const shedLine = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label });
  const workTone = optionTone(pageContract, "work_state_filter_chips", row.work_state) as Tone;
  const pct = row.expected_count > 0 ? Math.round((row.completed_count / row.expected_count) * 100) : 0;
  const progress = (
    <Box component="span" sx={{ display: "block", minWidth: 160 }}>
      <Box component="span" sx={{ display: "flex", justifyContent: "space-between", gap: 2, mb: 0.75 }}>
        <span>
          <b>{row.completed_count}</b> / {row.expected_count} {copy(pageContract, "label.done")}
        </span>
        <Box component="span" sx={{ typography: "subtitle2" }}>{pct}%</Box>
      </Box>
      <LinearProgress variant="determinate" value={pct} />
    </Box>
  );

  return (
    <div className="screen on">
      <OrderDetailsToolbar
        title={title}
        status={optionLabel(pageContract, "work_state_filter_chips", row.work_state)}
        statusColor={TONE_LABEL[workTone] ?? "default"}
        subtitle={[copy(pageContract, "crumb"), drive, row.park_name, shedLine, stageLabel(row.animal_stage)].join(" · ")}
        backHref={backHref}
        backLabel={backLabel}
        // The status chips + Action Center wrap on a phone instead of running off the left edge.
        slotProps={{ actions: { flexWrap: "wrap", justifyContent: { xs: "flex-start", md: "flex-end" } } }}
        actions={
          <>
            <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
            <Tag tone={optionTone(pageContract, "sop_state_chips", row.sop_task_state) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sop_task_state)}</Tag>
            <Tag tone={optionTone(pageContract, "proof_state_chips", row.proof_state) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proof_state)}</Tag>
            <Tag tone={optionTone(pageContract, "verification_state_chips", row.verification_state) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verification_state)}</Tag>
            <LinkButton href={scopeHref("/action-center", scope, {}, { ac_row: row.row_id })} variant="contained" startIcon={<Iconify icon="solar:list-bold" />}>
              {copy(pageContract, "action.action_center")}
            </LinkButton>
          </>
        }
      />

      {row.blocker_reason ? <Alert severity="error">{row.blocker_reason}</Alert> : null}

      <Grid container spacing={3}>
        <Grid size={WF_DETAIL_GRID.chain}>
          <OrderDetailsHistory
            aria-label={copy(pageContract, "section.chain.title")}
            title={copy(pageContract, "section.chain.title")}
            timeline={chainTimeline(nodes)}
            summary={[
              { key: "drive", label: copy(pageContract, "label.drive", "Drive"), value: drive },
              { key: "progress", label: copy(pageContract, "label.progress", "Progress"), value: progress },
              { key: "next", label: `${copy(pageContract, "section.chain.next_prefix")}`, value: row.next_action },
            ]}
          />
        </Grid>

        <Grid size={WF_DETAIL_GRID.side}>
          <Card>
            <OrderDetailsCustomer slotProps={{ line: detailWrapSx }}
              title={copy(pageContract, "label.vaccination_drive")}
              name={drive}
              lines={[row.protocol_name, row.owner?.operator_name || copy(pageContract, "label.unassigned")]}
            />

            <Divider sx={{ borderStyle: "dashed" }} />
            <OrderDetailsDelivery slotProps={{ row: detailWrapSx }}
              title={copy(pageContract, "label.vaccination")}
              rows={[
                { key: "park", label: copy(pageContract, "label.park", "Park"), value: row.park_name },
                { key: "pen", label: copy(pageContract, "label.pen", "Pen"), value: shedLine },
                { key: "stage", label: copy(pageContract, "label.stage", "Stage"), value: stageLabel(row.animal_stage) },
                { key: "next", label: copy(pageContract, "label.next_action", "Next action"), value: row.next_action },
              ]}
            />

            <Divider sx={{ borderStyle: "dashed" }} />
            <Stack spacing={1} sx={{ p: 3, alignItems: "flex-start" }}>
              {row.goat_id ? (
                <LinkButton href={`/goats/${encodeURIComponent(row.goat_id)}`} size="small" color="inherit" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" width={16} />}>
                  {copy(pageContract, "action.goat_passport")}
                </LinkButton>
              ) : null}
              <LinkButton
                href={scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(row.shed_id)}`, scope, { mode: "park", park: row.park_id }, row.partition_label ? { partition_label: row.partition_label } : {})}
                size="small"
                color="inherit"
                endIcon={<Iconify icon="eva:arrow-ios-forward-fill" width={16} />}
              >
                {copy(pageContract, "action.shed_execution")}
              </LinkButton>
              <LinkButton href={scopeHref("/protocol-adherence", scope)} size="small" color="inherit" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" width={16} />}>
                {copy(pageContract, "action.protocol_adherence")}
              </LinkButton>
            </Stack>
          </Card>
        </Grid>
      </Grid>
    </div>
  );
}
