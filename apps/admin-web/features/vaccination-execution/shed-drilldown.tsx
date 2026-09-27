import { redirect } from "next/navigation";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import Avatar from "@mui/material/Avatar";
import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";
import { OrderDetailsDelivery } from "@/components/minimal/sections/order/order-details-delivery";
import { getVaccinationExecutionShedDrilldown } from "@/lib/api/server";
import type { VaccinationExecutionRow } from "@/lib/api/vaccination-execution";
import { Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionLabel, optionTone, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";
import { stageLabel } from "@/lib/stage-labels";
import { PageHeader } from "@/components/app/page-header";
import { EmptyContent } from "@/components/minimal/empty-content";

// Order-details layout twin from sections/order/view/order-details-view.tsx: toolbar with back
// arrow + title + chips inside PageHeader (template CustomBreadcrumbs), Grid xs=12 md=8 body
// column with the operational cards, Grid xs=12 md=4 right Card with dashed dividers listing
// scope + owner chain + drives. Feature markup + read/write behaviour unchanged.

// One work-state reading: caption label, h4 figure, an open/done Label when non-zero (template
// invoice-analytic cell anatomy, laid out in a dashed-divider grid inside the card).
function Stat({ label, value, tone, pageContract }: { label: string; value: number; tone: Tone; pageContract: AdminUiPageContract }) {
  return (
    <Box sx={{ p: 2.5, minWidth: 0 }}>
      <Typography variant="body2" sx={{ color: "text.secondary", mb: 0.5 }} noWrap title={label}>
        {label}
      </Typography>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
        <Typography variant="h4" component="span">
          {value}
        </Typography>
        {value > 0 ? <Tag tone={tone}>{tone === "ok" ? copy(pageContract, "label.done") : copy(pageContract, "label.open")}</Tag> : null}
      </Box>
    </Box>
  );
}

function StatusChips({ row, pageContract }: { row: VaccinationExecutionRow; pageContract: AdminUiPageContract }) {
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
      {row.sopStatus ? <Tag tone={optionTone(pageContract, "sop_state_chips", row.sopStatus) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sopStatus)}</Tag> : null}
      {row.proofStatus ? <Tag tone={optionTone(pageContract, "proof_state_chips", row.proofStatus) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proofStatus)}</Tag> : null}
      {row.verificationStatus ? (
        <Tag tone={optionTone(pageContract, "verification_state_chips", row.verificationStatus) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verificationStatus)}</Tag>
      ) : null}
    </Box>
  );
}

// The pen could not be opened. Everything on this screen is backend-owned farm copy from the page
// contract: never the pen's raw id and never the API's error string (which is written for logs --
// "shed vaccination execution was not found" -- and put both a uuid and the word "shed" on screen).
function PenUnavailable({ backHref, pageContract }: { backHref: string; pageContract: AdminUiPageContract }) {
  return (
    <Box className="screen on">
      <PageHeader title={copy(pageContract, "fallback.title")} backHref={backHref} crumbs={[{ label: copy(pageContract, "crumb"), href: backHref }]} />
      <EmptyContent filled title={copy(pageContract, "fallback.body")} />
    </Box>
  );
}

export async function ShedExecutionDetailPage({
  shedId,
  partitionLabel,
  scope,
  asOf,
  pageContract,
}: {
  shedId: string;
  partitionLabel?: string;
  scope?: Scope;
  asOf?: string;
  pageContract: AdminUiPageContract;
}) {
  const result = await getVaccinationExecutionShedDrilldown(shedId, { asOf, partitionLabel });
  const fallbackBackHref = scope ? `${scopeHref("/vaccination", scope)}#execution` : "/vaccination#execution";
  if (!result.ok) {
    return <PenUnavailable backHref={fallbackBackHref} pageContract={pageContract} />;
  }
  const shed = result.data;
  if (scope && scope.mode !== "park" && shed.parkId) {
    redirect(
      scopeHref(
        `/vaccination/execution/sheds/${encodeURIComponent(shedId)}`,
        scope,
        { mode: "park", park: shed.parkId },
        { partition_label: partitionLabel },
      ),
    );
  }
  const backHref = scope ? `${scopeHref("/vaccination", scope, { mode: "park", park: shed.parkId })}#execution` : "/vaccination#execution";

  const s = shed.summary;
  const owner = shed.rows.find((r) => r.owner?.operatorName)?.owner ?? shed.rows[0]?.owner;
  const shedDisplayLabel = shed.operationalLocationDisplay;
  const blockers = shed.rows.filter((r) => r.blockerReason);
  const driveRowLabels = tableLabels(pageContract, "shed-drive-rows");
  const stageList = shed.animalStages.map(stageLabel).join(" · ") || copy(pageContract, "label.placeholder");

  const headerActions = (
    <Tag tone="mut">{s.total} {copy(pageContract, "label.drive_rows")}</Tag>
  );

  const stats: { key: string; value: number; tone: Tone }[] = [
    { key: "due", value: s.due, tone: "warn" },
    { key: "overdue", value: s.overdue, tone: "dng" },
    { key: "proof_pending", value: s.proofPending, tone: "warn" },
    { key: "verification_pending", value: s.verificationPending, tone: "pur" },
    { key: "rejected", value: s.rejected, tone: "dng" },
    { key: "deferred", value: s.deferred, tone: "mut" },
    { key: "missed", value: s.missed, tone: "warn" },
    { key: "blocked", value: s.blocked, tone: "dng" },
    { key: "completed", value: s.completed, tone: "ok" },
  ];
  const owners = [
    { key: "operator", icon: "solar:user-rounded-bold" as const, label: copy(pageContract, "label.operator_ground"), value: owner?.operatorName ?? <Tag tone="dng">{copy(pageContract, "label.unassigned")}</Tag> },
    { key: "head", icon: "solar:users-group-rounded-bold" as const, label: copy(pageContract, "label.park_head"), value: owner?.parkHeadName ?? copy(pageContract, "label.placeholder") },
    { key: "verifier", icon: "solar:shield-check-bold" as const, label: copy(pageContract, "label.verifier"), value: owner?.verifierName ?? copy(pageContract, "label.verifier_default") },
  ];

  return (
    <Box className="screen on">
      <PageHeader
        title={`${shed.parkName} · ${shedDisplayLabel}`}
        backHref={backHref}
        crumbs={[{ label: copy(pageContract, "crumb"), href: backHref }, { label: shed.parkName }, { label: `${copy(pageContract, "label.animal_stages")}: ${stageList}` }]}
        actions={headerActions}
      />

      {/* Template order details: md 8 body column (work state, blockers, drive rows) beside the md 4
          customer / delivery rail (scope, owner chain, drives). */}
      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 8 }}>
          <Stack spacing={3}>
            <Card aria-label={copy(pageContract, "section.work_state.title")}>
              <CardHeader title={copy(pageContract, "section.work_state.title")} action={<Label variant="soft">{s.total} {copy(pageContract, "label.drive_rows")}</Label>} />
              <Paper variant="outlined" sx={{ m: 3, borderStyle: "dashed", overflow: "hidden" }}>
                <Box
                  sx={{
                    mr: "-1px",
                    mb: "-1px",
                    display: "grid",
                    gridTemplateColumns: { xs: "repeat(2, minmax(0, 1fr))", sm: "repeat(3, minmax(0, 1fr))" },
                    "& > *": { borderRight: 1, borderBottom: 1, borderColor: "divider", borderStyle: "dashed" },
                  }}
                >
                  {stats.map((stat) => (
                    <Stat key={stat.key} pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", stat.key)} value={stat.value} tone={stat.tone} />
                  ))}
                </Box>
              </Paper>
            </Card>

            {blockers.length > 0 ? (
              <Card>
                <CardHeader
                  title={copy(pageContract, "section.blocked.title")}
                  action={
                    <Label variant="soft" color="error">
                      {blockers.length}
                    </Label>
                  }
                />
                <Stack spacing={1.5} sx={{ p: 3 }}>
                  {blockers.map((r, i) => (
                    <Alert key={`${r.driveId ?? "drive"}-${i}`} severity="error" icon={false}>
                      <Tag tone={optionTone(pageContract, "work_state_filter_chips", r.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", r.workState)}</Tag>{" "}
                      <b>{r.driveName ?? copy(pageContract, "label.drive_fallback")}</b> ({r.animalStage}) — {r.blockerReason}
                    </Alert>
                  ))}
                </Stack>
              </Card>
            ) : null}

            <Card>
              <CardHeader title={copy(pageContract, "section.drive_rows.title")} action={<Label variant="soft">{shed.rows.length}</Label>} sx={{ mb: 3 }} />
              {shed.rows.length === 0 ? (
                <Typography variant="body2" sx={{ color: "text.secondary", px: 3, pb: 3 }}>
                  {copy(pageContract, "empty.drive_rows")}
                </Typography>
              ) : (
                <Scrollbar>
                  <Table sx={{ minWidth: 820 }} aria-label={`${shedDisplayLabel} ${copy(pageContract, "table.drive_rows.aria")}`}>
                    <TableHeadCustom headCells={driveRowLabels.map((label, index) => ({ id: `c${index}`, label }))} />
                    <TableBody>
                      {shed.rows.map((row, idx) => (
                        <TableRow hover key={`${row.driveId ?? copy(pageContract, "label.drive_fallback")}-${idx}`}>
                          <TableCell>{row.animalStage}</TableCell>
                          <TableCell sx={{ typography: "subtitle2" }}>{row.driveName ?? copy(pageContract, "label.placeholder")}</TableCell>
                          <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(row.dueDate)}</TableCell>
                          <TableCell>
                            <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag>
                          </TableCell>
                          <TableCell>
                            <StatusChips row={row} pageContract={pageContract} />
                          </TableCell>
                          <TableCell sx={{ color: "text.secondary" }}>{row.nextAction}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </Scrollbar>
              )}
            </Card>
          </Stack>
        </Grid>

        <Grid size={{ xs: 12, md: 4 }}>
          <Card>
            <OrderDetailsDelivery
              title={copy(pageContract, "label.pen", "Pen")}
              labelWidth={112}
              rows={[
                { key: "park", label: copy(pageContract, "label.park", "Park"), value: shed.parkName },
                { key: "pen", label: copy(pageContract, "label.pen", "Pen"), value: shedDisplayLabel },
                { key: "stages", label: copy(pageContract, "label.animal_stages"), value: stageList },
              ]}
            />

            <Divider sx={{ borderStyle: "dashed" }} />
            <CardHeader title={copy(pageContract, "section.owner_chain.title")} />
            <Stack spacing={2} sx={{ p: 3 }}>
              {owners.map((item) => (
                <Box key={item.key} sx={{ display: "flex", alignItems: "center", gap: 2, minWidth: 0 }}>
                  <Avatar sx={{ width: 40, height: 40, bgcolor: "background.neutral", color: "text.secondary" }}>
                    <Iconify icon={item.icon} width={20} />
                  </Avatar>
                  <Stack spacing={0.25} sx={{ minWidth: 0, typography: "body2" }}>
                    <Box component="span" sx={{ color: "text.secondary" }}>{item.label}</Box>
                    <Box component="span" sx={{ typography: "subtitle2", overflowWrap: "anywhere" }}>{item.value}</Box>
                  </Stack>
                </Box>
              ))}
            </Stack>

            <Divider sx={{ borderStyle: "dashed" }} />
            <CardHeader title={copy(pageContract, "section.drives.title")} />
            <Box sx={{ p: 3, display: "flex", flexWrap: "wrap", gap: 1 }}>
              {shed.drives.length === 0 ? (
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {copy(pageContract, "empty.drives")}
                </Typography>
              ) : (
                shed.drives.map((d, i) => (
                  <Tag key={`${d.driveId ?? copy(pageContract, "label.drive_fallback")}-${i}`} tone={optionTone(pageContract, "work_state_filter_chips", d.workState) as Tone} title={optionLabel(pageContract, "severity_chips", d.severity)}>
                    {d.driveName ?? copy(pageContract, "label.drive_fallback")} · {optionLabel(pageContract, "work_state_filter_chips", d.workState)}
                  </Tag>
                ))
              )}
            </Box>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}
