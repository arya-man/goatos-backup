import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Link from "@/components/no-prefetch-link";
import { Syringe } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import {
  getGoatVaccinationPassport,
  requireAdminWebPageContract,
  type VaccinationPassportHistoryItem,
} from "@/lib/api/server";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";

function obligationTone(status: string): Tone {
  if (status === "deferred" || status === "missed") return "warn";
  if (status === "due" || status === "in_progress") return "info";
  if (status === "scheduled") return "mut";
  return "mut";
}

function historyTone(status: string): Tone {
  if (status === "accepted") return "ok";
  if (status === "rejected") return "dng";
  if (status === "recorded") return "warn";
  return "mut";
}

function proofLabel(item: VaccinationPassportHistoryItem, pageContract: AdminUiPageContract) {
  if (item.status === "accepted") return <Tag tone="ok">{copy(pageContract, "vaccination.proof_verified")}</Tag>;
  if (item.status === "recorded") return <Tag tone="warn">{copy(pageContract, "vaccination.awaiting_verify")}</Tag>;
  if (item.status === "rejected") return <Tag tone="dng">{copy(pageContract, "vaccination.rework_rejected")}</Tag>;
  return <Tag tone="mut">{item.status}</Tag>;
}

function workflowHref(rowId: string): string {
  return `/workflows/${encodeURIComponent(rowId)}`;
}

function realWorkflowRowId(rowId: string | undefined): string | null {
  const trimmed = rowId?.trim();
  if (!trimmed || trimmed.startsWith("obligation:")) return null;
  return trimmed;
}

function sourceObligationLabel(obligationId: string): string {
  return obligationId.slice(0, 8);
}

const DRAWER_ROW_LIMIT = 5;

function vaccineRowLabel(item: { display_label: string }): string {
  return item.display_label;
}

function sameDate(left?: string, right?: string): boolean {
  return Boolean(left && right && left.slice(0, 10) === right.slice(0, 10));
}

export async function HerdPassportVaccinationBlock({ goatId }: { goatId: string }) {
  const [res, pageContract] = await Promise.all([
    getGoatVaccinationPassport(goatId),
    requireAdminWebPageContract("goat-passport"),
  ]);
  if (!res.ok) {
    return (
      <div style={{ marginTop: 16 }}>
        <div
          className="muted small"
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            fontWeight: 700,
            textTransform: "uppercase",
            letterSpacing: ".4px",
          }}
        >
          <Syringe className="ic" aria-hidden="true" style={{ width: 14, height: 14 }} />
          {copy(pageContract, "section.vaccination.title")}
        </div>
        <p className="muted small" style={{ marginTop: 8 }}>
          {copy(pageContract, "vaccination.unavailable_prefix")}: {res.error.message ?? res.error.code}
        </p>
      </div>
    );
  }

  const p = res.data;
  const open = p.open_obligations ?? [];
  const history = p.vaccination_history ?? [];
  const openCols = tableLabels(pageContract, "vaccination-open-obligations");
  const historyCols = tableLabels(pageContract, "vaccination-history");

  return (
    <div style={{ marginTop: 16 }}>
      <div
        className="muted small"
        style={{
          display: "flex",
          alignItems: "center",
          gap: 6,
          fontWeight: 700,
          textTransform: "uppercase",
          letterSpacing: ".4px",
          marginBottom: 10,
        }}
      >
        <Syringe className="ic" aria-hidden="true" style={{ width: 14, height: 14 }} />
        {copy(pageContract, "section.vaccination.title")}
      </div>

      <div className="metagrid" style={{ gridTemplateColumns: "1fr 1fr 1fr", marginBottom: 12 }}>
        <div>
          <div className="k">{copy(pageContract, "vaccination.next_due")}</div>
          <div className="v" style={{ fontSize: 13 }}>
            {p.next_due ? (
              <>
                {fmtDate(p.next_due.scheduled_for || p.next_due.due_at)}{" "}
                <Tag tone={obligationTone(p.next_due.status)}>{p.next_due.status}</Tag>
              </>
            ) : (
              copy(pageContract, "vaccination.no_upcoming")
            )}
          </div>
        </div>
        <div>
          <div className="k">{copy(pageContract, "vaccination.open_obligations")}</div>
          <div className="v">{open.length}</div>
        </div>
        <div>
          <div className="k">{copy(pageContract, "vaccination.last_accepted")}</div>
          <div className="v" style={{ fontSize: 13 }}>
            {p.last_accepted ? fmtDate(p.last_accepted.administered_at) : copy(pageContract, "label.placeholder")}
          </div>
        </div>
      </div>

      <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>
        {copy(pageContract, "vaccination.open_due_rows")}
      </div>
      {open.length === 0 ? (
        <p className="muted small" style={{ margin: "0 0 12px" }}>
          {copy(pageContract, "vaccination.empty_open")}
        </p>
      ) : (
        <div style={{ overflowX: "auto", marginBottom: 12 }} tabIndex={0} role="group" aria-label={copy(pageContract, "vaccination.open_due_rows")}>
          <Table>
            <TableHead>
              <TableRow>
                {openCols.slice(0, 4).map((label) => (
                  <TableCell component="th" key={label}>{label}</TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {open.slice(0, DRAWER_ROW_LIMIT).map((due) => {
                const rowId = realWorkflowRowId(due.workflow_row_id);
                return (
                  <TableRow key={due.obligation_id}>
                    <TableCell>
                      <div>{fmtDate(due.scheduled_for || due.due_at)}</div>
                      {due.scheduled_for && due.clinical_due_at && !sameDate(due.scheduled_for, due.clinical_due_at) ? (
                        <div className="muted small">{copy(pageContract, "vaccination.clinical_due")} {fmtDate(due.clinical_due_at)}</div>
                      ) : null}
                    </TableCell>
                    <TableCell>{vaccineRowLabel(due)}</TableCell>
                    <TableCell>
                      <Tag tone={obligationTone(due.status)}>{due.status}</Tag>
                    </TableCell>
                    <TableCell>
                      {rowId ? (
                        <Link href={workflowHref(rowId)} className="lk small">
                          {copy(pageContract, "action.open_workflow")} →
                        </Link>
                      ) : (
                        <span className="gid" title={due.obligation_id}>{sourceObligationLabel(due.obligation_id)}</span>
                      )}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
          {open.length > DRAWER_ROW_LIMIT ? (
            <p className="muted small" style={{ margin: "6px 0 0" }}>
              +{open.length - DRAWER_ROW_LIMIT} more
            </p>
          ) : null}
        </div>
      )}

      <div className="muted small" style={{ fontWeight: 700, marginBottom: 6 }}>
        {copy(pageContract, "vaccination.history")}
      </div>
      {history.length === 0 ? (
        <p className="muted small" style={{ margin: 0 }}>
          {copy(pageContract, "vaccination.empty_history")}
        </p>
      ) : (
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.vaccination.aria")}>
          <Table>
            <TableHead>
              <TableRow>
                {historyCols.slice(0, 5).map((label) => (
                  <TableCell component="th" key={label}>{label}</TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {history.slice(0, DRAWER_ROW_LIMIT).map((h) => (
                <TableRow key={h.completion_id}>
                  <TableCell>{fmtDate(h.administered_at)}</TableCell>
                  <TableCell>{vaccineRowLabel(h)}</TableCell>
                  <TableCell>
                    <Tag tone={historyTone(h.status)}>{h.status}</Tag>
                  </TableCell>
                  <TableCell>{proofLabel(h, pageContract)}</TableCell>
                  <TableCell>
                    <span className="gid" title={h.obligation_id}>{sourceObligationLabel(h.obligation_id)}</span>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {history.length > DRAWER_ROW_LIMIT ? (
            <p className="muted small" style={{ margin: "6px 0 0" }}>
              +{history.length - DRAWER_ROW_LIMIT} more
            </p>
          ) : null}
        </div>
      )}
    </div>
  );
}
