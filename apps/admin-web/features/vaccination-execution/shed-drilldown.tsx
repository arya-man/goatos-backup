import { redirect } from "next/navigation";
import { Ban, MapPin, ShieldCheck, Syringe, UserRound, Warehouse } from "lucide-react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
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

function Stat({ label, value, tone, pageContract }: { label: string; value: number; tone: Tone; pageContract: AdminUiPageContract }) {
  return (
    <div>
      <div className="k">{label}</div>
      <div className="v" style={{ display: "flex", alignItems: "center", gap: 6 }}>
        {value}
        {value > 0 ? <Tag tone={tone}>{tone === "ok" ? copy(pageContract, "label.done") : copy(pageContract, "label.open")}</Tag> : null}
      </div>
    </div>
  );
}

function StatusChips({ row, pageContract }: { row: VaccinationExecutionRow; pageContract: AdminUiPageContract }) {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
      {row.sopStatus ? <Tag tone={optionTone(pageContract, "sop_state_chips", row.sopStatus) as Tone}>{optionLabel(pageContract, "sop_state_chips", row.sopStatus)}</Tag> : null}
      {row.proofStatus ? <Tag tone={optionTone(pageContract, "proof_state_chips", row.proofStatus) as Tone}>{optionLabel(pageContract, "proof_state_chips", row.proofStatus)}</Tag> : null}
      {row.verificationStatus ? (
        <Tag tone={optionTone(pageContract, "verification_state_chips", row.verificationStatus) as Tone}>{optionLabel(pageContract, "verification_state_chips", row.verificationStatus)}</Tag>
      ) : null}
    </div>
  );
}

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

// The pen could not be opened. Everything on this screen is backend-owned farm copy from the page
// contract: never the pen's raw id and never the API's error string (which is written for logs --
// "shed vaccination execution was not found" -- and put both a uuid and the word "shed" on screen).
function PenUnavailable({ backHref, pageContract }: { backHref: string; pageContract: AdminUiPageContract }) {
  return (
    <div className="screen on">
      <PageHeader title={copy(pageContract, "fallback.title")} backHref={backHref} crumbs={[{ label: copy(pageContract, "crumb"), href: backHref }]} />
      <EmptyContent filled title={copy(pageContract, "fallback.body")} />
    </div>
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

  return (
    <div className="screen on">
      <PageHeader
        title={`${shed.parkName} · ${shedDisplayLabel}`}
        backHref={backHref}
        crumbs={[{ label: copy(pageContract, "crumb"), href: backHref }, { label: shed.parkName }, { label: `${copy(pageContract, "label.animal_stages")}: ${stageList}` }]}
        actions={headerActions}
      />

      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 8 }}>
          <Box sx={{ gap: 3, display: "flex", flexDirection: { xs: "column-reverse", md: "column" } }}>
            <Card aria-label={copy(pageContract, "section.work_state.title")}>
              <CardHeader
                title={
                  <span className="gp-card-title" style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
                    <Syringe className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
                    {copy(pageContract, "section.work_state.title")}
                  </span>
                }
                action={<Tag tone="mut">{s.total} {copy(pageContract, "label.drive_rows")}</Tag>}
              />
              <Box sx={{ p: 3 }}>
                <div className="metagrid shed-workstate" style={{ gridTemplateColumns: "repeat(auto-fill,minmax(150px,1fr))", gap: 14 }}>
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "due")} value={s.due} tone="warn" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "overdue")} value={s.overdue} tone="dng" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "proof_pending")} value={s.proofPending} tone="warn" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "verification_pending")} value={s.verificationPending} tone="pur" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "rejected")} value={s.rejected} tone="dng" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "deferred")} value={s.deferred} tone="mut" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "missed")} value={s.missed} tone="warn" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "blocked")} value={s.blocked} tone="dng" />
                  <Stat pageContract={pageContract} label={optionLabel(pageContract, "work_state_filter_chips", "completed")} value={s.completed} tone="ok" />
                </div>
              </Box>
            </Card>

            {blockers.length > 0 ? (
              <Card>
                <CardHeader
                  title={
                    <span className="gp-card-title" style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
                      <Ban className="ic" style={{ color: "var(--danger)" }} aria-hidden="true" />
                      {copy(pageContract, "section.blocked.title")}
                    </span>
                  }
                  action={<Tag tone="dng">{blockers.length}</Tag>}
                />
                <Box sx={{ p: 3 }}>
                  <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                    {blockers.map((r, i) => (
                      <div key={`${r.driveId ?? "drive"}-${i}`} className="note">
                        <Tag tone={optionTone(pageContract, "work_state_filter_chips", r.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", r.workState)}</Tag>{" "}
                        <b>{r.driveName ?? copy(pageContract, "label.drive_fallback")}</b> ({r.animalStage}) — {r.blockerReason}
                      </div>
                    ))}
                  </div>
                </Box>
              </Card>
            ) : null}

            <Card>
              <CardHeader
                title={
                  <span className="gp-card-title" style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
                    <Warehouse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
                    {copy(pageContract, "section.drive_rows.title")}
                  </span>
                }
                action={<Tag tone="mut">{shed.rows.length}</Tag>}
              />
              <Box sx={{ p: 3 }}>
                <div className="pexec" role="group" aria-label={`${shedDisplayLabel} ${copy(pageContract, "table.drive_rows.aria")}`}>
                  <div className="pexh">
                    {driveRowLabels.map((label) => (
                      <div key={label}>{label}</div>
                    ))}
                  </div>
                  {/* A pen with no drive rows says so (main 5ef37c050). */}
                  {shed.rows.length === 0 ? (
                    <Typography variant="body2" sx={{ color: "text.secondary", px: 1.75, py: 1.25 }}>
                      {copy(pageContract, "empty.drive_rows")}
                    </Typography>
                  ) : null}
                  {shed.rows.map((row, idx) => (
                    <div className="pexr" key={`${row.driveId ?? copy(pageContract, "label.drive_fallback")}-${idx}`}>
                      <div className="pexc">
                        <div className="pexc-h">{driveRowLabels[0]}</div>
                        <span className="small">{row.animalStage}</span>
                      </div>
                      <div className="pexc">
                        <div className="pexc-h">{driveRowLabels[1]}</div>
                        <span className="small">{row.driveName ?? copy(pageContract, "label.placeholder")}</span>
                      </div>
                      <div className="pexc">
                        <div className="pexc-h">{driveRowLabels[2]}</div>
                        <span className="small">{fmtDate(row.dueDate)}</span>
                      </div>
                      <div className="pexc">
                        <div className="pexc-h">{driveRowLabels[3]}</div>
                        <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.workState)}</Tag>
                      </div>
                      <div className="pexc">
                        <div className="pexc-h">{driveRowLabels[4]}</div>
                        <StatusChips row={row} pageContract={pageContract} />
                      </div>
                      <div className="pexc">
                        <div className="pexc-h">{driveRowLabels[5]}</div>
                        <span className="small">{row.nextAction}</span>
                      </div>
                    </div>
                  ))}
                </div>
              </Box>
            </Card>
          </Box>
        </Grid>

        <Grid size={{ xs: 12, md: 4 }}>
          <Card>
            <Box sx={{ p: 3 }}>
              <Stack spacing={2}>
                <SummaryBlock label={copy(pageContract, "label.park", "Park")} value={shed.parkName} />
                <SummaryBlock label={copy(pageContract, "label.pen", "Pen")} value={shedDisplayLabel} />
                <SummaryBlock label={copy(pageContract, "label.animal_stages")} value={stageList} />
              </Stack>
            </Box>

            <Divider sx={{ borderStyle: "dashed" }} />
            <Box sx={{ p: 3 }}>
              <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4, mb: 1, display: "block" }}>
                {copy(pageContract, "section.owner_chain.title")}
              </Typography>
              <Stack spacing={1.5}>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <UserRound className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                  <div>
                    <div className="k">{copy(pageContract, "label.operator_ground")}</div>
                    <div className="v">{owner?.operatorName ?? <Tag tone="dng">{copy(pageContract, "label.unassigned")}</Tag>}</div>
                  </div>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <MapPin className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                  <div>
                    <div className="k">{copy(pageContract, "label.park_head")}</div>
                    <div className="v">{owner?.parkHeadName ?? copy(pageContract, "label.placeholder")}</div>
                  </div>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                  <ShieldCheck className="ic" style={{ width: 14, opacity: 0.75 }} aria-hidden="true" />
                  <div>
                    <div className="k">{copy(pageContract, "label.verifier")}</div>
                    <div className="v">{owner?.verifierName ?? copy(pageContract, "label.verifier_default")}</div>
                  </div>
                </div>
              </Stack>
            </Box>

            <Divider sx={{ borderStyle: "dashed" }} />
            <Box sx={{ p: 3 }}>
              <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4, mb: 1, display: "block" }}>
                {copy(pageContract, "section.drives.title")}
              </Typography>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                {shed.drives.length === 0 ? (
                  <span className="muted small">{copy(pageContract, "empty.drives")}</span>
                ) : (
                  shed.drives.map((d, i) => (
                    <Tag key={`${d.driveId ?? copy(pageContract, "label.drive_fallback")}-${i}`} tone={optionTone(pageContract, "work_state_filter_chips", d.workState) as Tone} title={optionLabel(pageContract, "severity_chips", d.severity)}>
                      {d.driveName ?? copy(pageContract, "label.drive_fallback")} · {optionLabel(pageContract, "work_state_filter_chips", d.workState)}
                    </Tag>
                  ))
                )}
              </div>
            </Box>
          </Card>
        </Grid>
      </Grid>
    </div>
  );
}
