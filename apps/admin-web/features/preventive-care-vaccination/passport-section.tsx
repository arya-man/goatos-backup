import Table from "@mui/material/Table";
import { DividedStack } from "@/components/app/divided-stack";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { LinkButton } from "@/components/app/link-button";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Paper from "@mui/material/Paper";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";
import {
  getGoatVaccinationPassport,
  type VaccinationPassport,
  type VaccinationPassportHistoryItem,
} from "@/lib/api/server";
import { Tag } from "@/components/ui-primitives";
import { fmtDate, humanizeEnum } from "@/lib/format";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";
function statusTone(status: string): Tone {
  if (status === "accepted") return "ok";
  if (status === "rejected") return "dng";
  if (status === "recorded") return "warn";
  return "mut";
}
function proofLabel(item: VaccinationPassportHistoryItem, pageContract: AdminUiPageContract): React.ReactNode {
  if (item.status === "accepted") return <Tag tone="ok">{copy(pageContract, "vaccination.proof_verified")}</Tag>;
  if (item.status === "recorded") return <Tag tone="warn">{copy(pageContract, "vaccination.awaiting_verify")}</Tag>;
  if (item.status === "rejected") return <Tag tone="dng">{copy(pageContract, "vaccination.rework_rejected")}</Tag>;
  return <Tag tone="mut">{humanizeEnum(item.status)}</Tag>;
}
function workflowHref(rowId: string): string {
  return `/workflows/${encodeURIComponent(rowId)}`;
}
function actionCenterHref(rowId: string): string {
  return `/action-center?ac_row=${encodeURIComponent(rowId)}`;
}
function realWorkflowRowId(rowId: string | undefined): string | null {
  const trimmed = rowId?.trim();
  if (!trimmed || trimmed.startsWith("obligation:")) return null;
  return trimmed;
}
function sourceObligationLabel(obligationId: string): string {
  return obligationId.slice(0, 8);
}

function vaccineRowLabel(item: { display_label: string }): string {
  return item.display_label;
}

function sameDate(left?: string, right?: string): boolean {
  return Boolean(left && right && left.slice(0, 10) === right.slice(0, 10));
}

// VaccinationPassportSection renders a goat's vaccination passport: next due, open obligations, last
// accepted, and the administered/verified history with proof status. Read-only. Template anatomy:
// Card + CardHeader (count Label), the invoice-list analytic strip (dashed dividers), then two
// titled table blocks (TableHeadCustom in a Scrollbar).
export async function VaccinationPassportSection({ goatId, pageContract }: { goatId: string; pageContract: AdminUiPageContract }) {
  const res = await getGoatVaccinationPassport(goatId);
  if (!res.ok) {
    // Soft-fail: the rest of the identity passport still renders.
    return (
      <Card>
        <CardHeader title={copy(pageContract, "section.vaccination.title")} />
        <Box sx={{ p: 3 }}>
          <Alert severity="error">
            {copy(pageContract, "vaccination.unavailable_prefix")}: {res.error.message ?? res.error.code}
          </Alert>
        </Box>
      </Card>
    );
  }
  const p: VaccinationPassport = res.data;
  const history = p.vaccination_history ?? [];
  const open = p.open_obligations ?? [];
  const placeholder = copy(pageContract, "label.placeholder");
  const openHead = tableLabels(pageContract, "vaccination-open-obligations").map((label, index) => ({ id: `o${index}`, label }));
  const historyHead = tableLabels(pageContract, "vaccination-history").map((label, index) => ({ id: `h${index}`, label }));
  const stats = [
    {
      key: "next",
      label: copy(pageContract, "vaccination.next_due"),
      value: p.next_due ? fmtDate(p.next_due.scheduled_for || p.next_due.due_at) : placeholder,
      extra: p.next_due ? <Tag tone="warn">{humanizeEnum(p.next_due.status)}</Tag> : null,
    },
    { key: "open", label: copy(pageContract, "vaccination.open_obligations"), value: String(open.length), extra: null },
    { key: "last", label: copy(pageContract, "vaccination.last_accepted"), value: p.last_accepted ? fmtDate(p.last_accepted.administered_at) : placeholder, extra: null },
  ];
  const blockTitle = (text: string) => (
    <Typography variant="overline" component="h3" sx={{ display: "block", px: 3, pt: 3, pb: 1.5, color: "text.secondary" }}>
      {text}
    </Typography>
  );

  return (
    <Card aria-label={copy(pageContract, "section.vaccination.title")}>
      <CardHeader
        title={
          <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
            {copy(pageContract, "section.vaccination.title")}
            <Label variant="soft">
              {history.length} {copy(pageContract, history.length === 1 ? "vaccination.dose_singular" : "vaccination.dose_plural")}
            </Label>
          </Box>
        }
        subheader={
          p.next_due
            ? `${copy(pageContract, "vaccination.next_due_inline")} ${fmtDate(p.next_due.scheduled_for || p.next_due.due_at)} · ${vaccineRowLabel(p.next_due)}`
            : copy(pageContract, "vaccination.no_upcoming")
        }
      />

      <Paper variant="outlined" sx={{ mx: 3, mt: 3, py: 2, borderStyle: "dashed" }}>
      <DividedStack dividerOrientation="vertical" direction={{ xs: "column", sm: "row" }}>
        {stats.map((stat) => (
          <Box key={stat.key} sx={{ flex: "1 1 0", px: 2.5, py: { xs: 1, sm: 0 }, minWidth: 0 }}>
            <Typography variant="body2" sx={{ color: "text.secondary", mb: 0.5 }}>
              {stat.label}
            </Typography>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
              <Typography variant="h5">{stat.value}</Typography>
              {stat.extra}
            </Box>
          </Box>
        ))}
      </DividedStack>
      </Paper>

      {blockTitle(copy(pageContract, "vaccination.open_due_rows"))}
      {open.length === 0 ? (
        <Typography variant="body2" sx={{ px: 3, pb: 2, color: "text.secondary" }}>
          {copy(pageContract, "vaccination.empty_open")}
        </Typography>
      ) : (
        <Scrollbar>
          <Table sx={{ minWidth: 640 }} aria-label={copy(pageContract, "vaccination.open_due_rows")}>
            <TableHeadCustom headCells={openHead} />
            <TableBody>
              {open.map((due) => {
                const rowId = realWorkflowRowId(due.workflow_row_id);
                return (
                  <TableRow hover key={due.obligation_id}>
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      {fmtDate(due.scheduled_for || due.due_at)}
                      {due.scheduled_for && due.clinical_due_at && !sameDate(due.scheduled_for, due.clinical_due_at) ? (
                        <Box component="span" sx={{ display: "block", typography: "caption", color: "text.secondary" }}>
                          {copy(pageContract, "vaccination.clinical_due")} {fmtDate(due.clinical_due_at)}
                        </Box>
                      ) : null}
                    </TableCell>
                    <TableCell>{vaccineRowLabel(due)}</TableCell>
                    <TableCell>
                      <Tag tone={statusTone(due.status)}>{humanizeEnum(due.status)}</Tag>
                    </TableCell>
                    <TableCell>
                      {rowId ? (
                        <LinkButton href={workflowHref(rowId)} size="small" color="primary" variant="text" sx={{ px: 0.5, minWidth: 0 }}>
                          {copy(pageContract, "action.open_workflow")} →
                        </LinkButton>
                      ) : (
                        <Box component="span" sx={{ fontFamily: "monospace" }} title={due.obligation_id}>
                          {sourceObligationLabel(due.obligation_id)}
                        </Box>
                      )}
                    </TableCell>
                    <TableCell>
                      {rowId ? (
                        <LinkButton href={actionCenterHref(rowId)} size="small" color="primary" variant="text" sx={{ px: 0.5, minWidth: 0 }}>
                          {copy(pageContract, "action.open_action_center")} →
                        </LinkButton>
                      ) : (
                        <Box component="span" sx={{ color: "text.disabled" }}>—</Box>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Scrollbar>
      )}

      {blockTitle(copy(pageContract, "vaccination.history"))}
      {history.length === 0 ? (
        <Typography variant="body2" sx={{ px: 3, pb: 3, color: "text.secondary" }}>
          {copy(pageContract, "vaccination.empty_history")}
        </Typography>
      ) : (
        <Scrollbar>
          <Table sx={{ minWidth: 760 }} aria-label={copy(pageContract, "table.vaccination.aria")}>
            <TableHeadCustom headCells={historyHead} />
            <TableBody>
              {history.map((h) => (
                <TableRow hover key={h.completion_id}>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(h.administered_at)}</TableCell>
                  <TableCell>{vaccineRowLabel(h)}</TableCell>
                  <TableCell sx={{ color: "text.secondary" }}>{h.route_site || placeholder}</TableCell>
                  <TableCell>
                    <Tag tone={statusTone(h.status)}>{humanizeEnum(h.status)}</Tag>
                  </TableCell>
                  <TableCell>{proofLabel(h, pageContract)}</TableCell>
                  <TableCell>
                    <Box component="span" sx={{ fontFamily: "monospace" }}>{h.obligation_id.slice(0, 8)}</Box>
                  </TableCell>
                  <TableCell>
                    <Box component="span" sx={{ fontFamily: "monospace" }} title={h.obligation_id}>
                      {sourceObligationLabel(h.obligation_id)}
                    </Box>
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
