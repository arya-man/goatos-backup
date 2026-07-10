import Link from "next/link";
import { Syringe } from "lucide-react";
import {
  getGoatVaccinationPassport,
  type VaccinationPassport,
  type VaccinationPassportDue,
  type VaccinationPassportHistoryItem,
} from "@/lib/api/server";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
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
  return <Tag tone="mut">{item.status}</Tag>;
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

// VaccinationPassportSection renders a goat's vaccination passport: next due, open obligations, last
// accepted, and the administered/verified history with proof status. Read-only. Wrapped in `.screen`
// so its table inherits the canonical table styling regardless of the host passport page.
export async function VaccinationPassportSection({ goatId, pageContract }: { goatId: string; pageContract: AdminUiPageContract }) {
  const res = await getGoatVaccinationPassport(goatId);
  if (!res.ok) {
    // Soft-fail: the rest of the identity passport still renders.
    return (
      <div className="screen on">
        <section className="card">
          <div className="hd">
            <Syringe className="ic" aria-hidden="true" />
	            <h3>{copy(pageContract, "section.vaccination.title")}</h3>
          </div>
          <div className="bd">
            <p className="muted small">{copy(pageContract, "vaccination.unavailable_prefix")}: {res.error.message ?? res.error.code}</p>
          </div>
        </section>
      </div>
    );
  }
  const p: VaccinationPassport = res.data;
  const history = p.vaccination_history ?? [];
  const open = p.open_obligations ?? [];

  return (
    <div className="screen on">
      <section className="card">
        <div className="hd">
          <Syringe className="ic" aria-hidden="true" />
	          <h3>{copy(pageContract, "section.vaccination.title")}</h3>
          <Tag tone="mut">
            {history.length} {copy(pageContract, history.length === 1 ? "vaccination.dose_singular" : "vaccination.dose_plural")}
          </Tag>
          <div className="sp" />
          {p.next_due ? (
            <span className="muted small">
              {copy(pageContract, "vaccination.next_due_inline")} <b>{fmtDate(p.next_due.due_at)}</b> · {copy(pageContract, "vaccination.dose_singular")} {p.next_due.sequence}
            </span>
          ) : (
            <span className="muted small">{copy(pageContract, "vaccination.no_upcoming")}</span>
          )}
        </div>

        <div className="bd">
          <div className="metagrid" style={{ gridTemplateColumns: "1fr 1fr 1fr" }}>
            <div>
              <div className="k">{copy(pageContract, "vaccination.next_due")}</div>
              <div className="v">
                {p.next_due ? (
                  <>
                    {fmtDate(p.next_due.due_at)} <Tag tone="warn">{p.next_due.status}</Tag>
                  </>
                ) : (
                  copy(pageContract, "label.placeholder")
                )}
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "vaccination.open_obligations")}</div>
              <div className="v">{open.length}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "vaccination.last_accepted")}</div>
              <div className="v">{p.last_accepted ? fmtDate(p.last_accepted.administered_at) : copy(pageContract, "label.placeholder")}</div>
            </div>
          </div>
        </div>

        <div
          className="muted small"
          style={{
            padding: "10px 16px",
            borderTop: "1px solid var(--line2)",
            fontWeight: 700,
            textTransform: "uppercase",
            letterSpacing: ".4px",
          }}
        >
          {copy(pageContract, "vaccination.open_due_rows")}
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "vaccination.open_due_rows")}>
          {open.length === 0 ? (
            <div className="bd">
              <p className="muted small">{copy(pageContract, "vaccination.empty_open")}</p>
            </div>
          ) : (
            <table>
              <thead>
                <tr>
                  {tableLabels(pageContract, "vaccination-open-obligations").map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {open.map((due) => {
                  const rowId = realWorkflowRowId(due.workflow_row_id);
                  return (
                    <tr key={due.obligation_id}>
                      <td>{fmtDate(due.due_at)}</td>
                      <td>{due.sequence}</td>
                      <td>
                        <Tag tone={statusTone(due.status)}>{due.status}</Tag>
                      </td>
                      <td>
                        {rowId ? (
                          <Link href={workflowHref(rowId)} className="lk small">
                            {copy(pageContract, "action.open_workflow")} →
                          </Link>
                        ) : (
                          <span className="gid" title={due.obligation_id}>{sourceObligationLabel(due.obligation_id)}</span>
                        )}
                      </td>
                      <td>
                        {rowId ? (
                          <Link href={actionCenterHref(rowId)} className="lk small">
                            {copy(pageContract, "action.open_action_center")} →
                          </Link>
                        ) : (
                          <span className="muted small">—</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        <div
          className="muted small"
          style={{
            padding: "10px 16px",
            borderTop: "1px solid var(--line2)",
            fontWeight: 700,
            textTransform: "uppercase",
            letterSpacing: ".4px",
          }}
        >
          {copy(pageContract, "vaccination.history")}
        </div>
	        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.vaccination.aria")}>
          {history.length === 0 ? (
            <div className="bd">
              <p className="muted small">
                {copy(pageContract, "vaccination.empty_history")}
              </p>
            </div>
          ) : (
            <table>
              <thead>
                <tr>
	                  {tableLabels(pageContract, "vaccination-history").map((label) => (
	                    <th key={label}>{label}</th>
	                  ))}
                </tr>
              </thead>
              <tbody>
                {history.map((h) => (
                  <tr key={h.completion_id}>
                    <td>{fmtDate(h.administered_at)}</td>
                    <td>{h.doses}</td>
                    <td className="muted">{h.route_site || copy(pageContract, "label.placeholder")}</td>
                    <td>
                      <Tag tone={statusTone(h.status)}>{h.status}</Tag>
                    </td>
                    <td>{proofLabel(h, pageContract)}</td>
                    <td>
                      <span className="gid">{h.obligation_id.slice(0, 8)}</span>
                    </td>
                    <td>
                      <span className="gid" title={h.obligation_id}>{sourceObligationLabel(h.obligation_id)}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
    </div>
  );
}
