import { Syringe } from "lucide-react";
import {
  getGoatVaccinationPassport,
  type VaccinationPassport,
  type VaccinationPassportHistoryItem,
} from "@/lib/api/server";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";
function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return <span className={`tag t-${tone}`}>{children}</span>;
}

function fmtDate(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toISOString().slice(0, 10);
}
function statusTone(status: string): Tone {
  if (status === "accepted") return "ok";
  if (status === "rejected") return "dng";
  if (status === "recorded") return "warn";
  return "mut";
}
function proofLabel(item: VaccinationPassportHistoryItem): React.ReactNode {
  if (item.status === "accepted") return <Tag tone="ok">proof verified</Tag>;
  if (item.status === "recorded") return <Tag tone="warn">awaiting verification</Tag>;
  if (item.status === "rejected") return <Tag tone="dng">rework / rejected</Tag>;
  return <Tag tone="mut">{item.status}</Tag>;
}

// VaccinationPassportSection renders a goat's vaccination passport: next due, open obligations, last
// accepted, and the administered/verified history with proof status. Read-only. Wrapped in `.screen`
// so its table inherits the canonical table styling regardless of the host passport page.
export async function VaccinationPassportSection({ goatId }: { goatId: string }) {
  const res = await getGoatVaccinationPassport(goatId);
  if (!res.ok) {
    // Soft-fail: the rest of the identity passport still renders.
    return (
      <div className="screen on">
        <section className="card">
          <div className="hd">
            <Syringe className="ic" aria-hidden="true" />
            <h3>Vaccination</h3>
          </div>
          <div className="bd">
            <p className="muted small">Vaccination passport unavailable: {res.error.message ?? res.error.code}</p>
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
          <h3>Vaccination</h3>
          <Tag tone="mut">
            {history.length} dose{history.length === 1 ? "" : "s"}
          </Tag>
          <div className="sp" />
          {p.next_due ? (
            <span className="muted small">
              next due <b>{fmtDate(p.next_due.due_at)}</b> · dose {p.next_due.sequence}
            </span>
          ) : (
            <span className="muted small">no upcoming dose</span>
          )}
        </div>

        <div className="bd">
          <div className="metagrid" style={{ gridTemplateColumns: "1fr 1fr 1fr" }}>
            <div>
              <div className="k">Next due</div>
              <div className="v">
                {p.next_due ? (
                  <>
                    {fmtDate(p.next_due.due_at)} <Tag tone="warn">{p.next_due.status}</Tag>
                  </>
                ) : (
                  "—"
                )}
              </div>
            </div>
            <div>
              <div className="k">Open obligations</div>
              <div className="v">{open.length}</div>
            </div>
            <div>
              <div className="k">Last accepted</div>
              <div className="v">{p.last_accepted ? fmtDate(p.last_accepted.administered_at) : "—"}</div>
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
          History
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Vaccination history">
          {history.length === 0 ? (
            <div className="bd">
              <p className="muted small">
                No vaccination history yet. Once a source-backed protocol is published and a dose is administered +
                verified, it appears here with its proof/verification status and the source protocol version.
              </p>
            </div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Administered</th>
                  <th>Doses</th>
                  <th>Route</th>
                  <th>Status</th>
                  <th>Proof</th>
                  <th>Source obligation</th>
                </tr>
              </thead>
              <tbody>
                {history.map((h) => (
                  <tr key={h.completion_id}>
                    <td>{fmtDate(h.administered_at)}</td>
                    <td>{h.doses}</td>
                    <td className="muted">{h.route_site || "—"}</td>
                    <td>
                      <Tag tone={statusTone(h.status)}>{h.status}</Tag>
                    </td>
                    <td>{proofLabel(h)}</td>
                    <td>
                      <span className="gid">{h.obligation_id.slice(0, 8)}</span>
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
