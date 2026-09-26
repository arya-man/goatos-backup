import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
import { PageHeader } from "@/components/app/page-header";
import Link from "@/components/no-prefetch-link";
import { operationalLocationLabel } from "@/lib/operational-location";
import { ArrowLeft, Syringe } from "lucide-react";
import { getVaccinationWorkflowDrilldown } from "@/lib/api/server";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { type Tone } from "./process-integrity";
import { WorkflowStepper } from "./workflow-stepper";
import { Tag } from "@/components/ui-primitives";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { actionDriveLabel, actionWorkTitle } from "./work-board";
import { stageLabel } from "@/lib/stage-labels";
import Alert from "@mui/material/Alert";

// Order-details layout twin from sections/order/view/order-details-view.tsx:
// toolbar (back + title + status chips) on top, Grid xs=12 md=8 main column, Grid xs=12 md=4
// right summary Card with dashed dividers between blocks. Feature markup + data unchanged;
// only the frame moves onto the template shape.
function SummaryBlock({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <Stack spacing={0.5}>
      <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4 }}>
        {label}
      </Typography>
      <Typography variant="body2" sx={{ color: "text.primary", wordBreak: "break-word" }}>
        {value}
      </Typography>
    </Stack>
  );
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
      <div className="kit-enter screen on">
        <div>
          <PageHeader title={pageContract.title || copy(pageContract, "fallback.title")} backHref={backHref} crumbs={[{ label: copy(pageContract, "crumb"), href: backHref }]} />
        </div>
        <Alert severity="error" role="alert">
          <b>{copy(pageContract, "error.row_unavailable", "This workflow record could not be opened.")}</b>
          <span className="muted small" style={{ marginLeft: 8 }}>{result.error.code ?? result.error.kind}</span>
        </Alert>
        <Link href={backHref} className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {backLabel}
        </Link>
      </div>
    );
  }

  const { row, nodes } = result.data;
  const title = actionWorkTitle(pageContract, row);
  const drive = actionDriveLabel(pageContract, row);
  const shedLine = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label });

  // Template OrderDetailsToolbar puts the status Label right next to the title. Same shape:
  // work-state pill leads, other computed state pills follow, all inside the header actions slot.
  const statusStrip = (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center" }}>
      <Syringe className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
      <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.work_state)}</Tag>
      <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
      <Tag tone={optionTone(pageContract, "sop_state_chips", row.sop_task_state) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sop_task_state)}</Tag>
      <Tag tone={optionTone(pageContract, "proof_state_chips", row.proof_state) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proof_state)}</Tag>
      <Tag tone={optionTone(pageContract, "verification_state_chips", row.verification_state) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verification_state)}</Tag>
    </Box>
  );

  return (
    <div className="kit-enter screen on">
      <div>
        <PageHeader
          title={title}
          backHref={backHref}
          crumbs={[{ label: copy(pageContract, "crumb"), href: backHref }, { label: drive }, { label: row.park_name }, { label: shedLine }, { label: stageLabel(row.animal_stage) }]}
          actions={statusStrip}
        />
      </div>

      {row.blocker_reason ? (
        <Alert severity="error" style={{ marginBottom: 14 }}><div>{row.blocker_reason}</div>
        </Alert>
      ) : null}

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 8 }}>
          <Box sx={{ gap: 3, display: "flex", flexDirection: { xs: "column-reverse", md: "column" } }}>
            <Card className="wf-chain-card" aria-label={copy(pageContract, "section.chain.title")}>
              <CardHeader
                sx={{
                  flexWrap: "wrap",
                  rowGap: 1.5,
                  [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 0 auto" }, minWidth: 0, maxWidth: "100%" },
                }}
                title={
                  <span className="gp-card-title" style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
                    <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
                    {copy(pageContract, "section.chain.title")}
                  </span>
                }
                action={<span className="muted small">{copy(pageContract, "section.chain.next_prefix")} {row.next_action}</span>}
              />
              <Box sx={{ p: 3 }}>
                <WorkflowStepper nodes={nodes} />
              </Box>
            </Card>
          </Box>
        </Grid>

        <Grid size={{ xs: 12, md: 4 }}>
          <Card>
            <Box sx={{ p: 3 }}>
              <Stack spacing={2}>
                <SummaryBlock label={copy(pageContract, "label.drive", "Drive")} value={drive} />
                <SummaryBlock label={copy(pageContract, "label.park", "Park")} value={row.park_name} />
                <SummaryBlock label={copy(pageContract, "label.pen", "Pen")} value={shedLine} />
                <SummaryBlock label={copy(pageContract, "label.stage", "Stage")} value={stageLabel(row.animal_stage)} />
              </Stack>
            </Box>

            <Divider sx={{ borderStyle: "dashed" }} />
            <Box sx={{ p: 3 }}>
              <SummaryBlock
                label={copy(pageContract, "label.progress", "Progress")}
                value={<><b>{row.completed_count}</b> / {row.expected_count} {copy(pageContract, "label.done")}</>}
              />
            </Box>

            <Divider sx={{ borderStyle: "dashed" }} />
            <Box sx={{ p: 3 }}>
              <SummaryBlock
                label={copy(pageContract, "label.next_action", "Next action")}
                value={row.next_action}
              />
            </Box>

            <Divider sx={{ borderStyle: "dashed" }} />
            <Box sx={{ p: 3, display: "flex", gap: 1.5, flexWrap: "wrap" }}>
              {row.goat_id ? (
                <Link href={`/goats/${encodeURIComponent(row.goat_id)}`} className="lk small">
                  {copy(pageContract, "action.goat_passport")} →
                </Link>
              ) : null}
              <Link href={scopeHref(`/vaccination/execution/sheds/${encodeURIComponent(row.shed_id)}`, scope, { mode: "park", park: row.park_id }, row.partition_label ? { partition_label: row.partition_label } : {})} className="lk small">
                {copy(pageContract, "action.shed_execution")} →
              </Link>
              <Link href={scopeHref("/action-center", scope, {}, { ac_row: row.row_id })} className="lk small">
                {copy(pageContract, "action.action_center")} →
              </Link>
              <Link href={scopeHref("/protocol-adherence", scope)} className="lk small">
                {copy(pageContract, "action.protocol_adherence")} →
              </Link>
            </Box>
          </Card>
        </Grid>
      </Grid>
    </div>
  );
}
