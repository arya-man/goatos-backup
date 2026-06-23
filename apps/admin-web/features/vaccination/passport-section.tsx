import {
  getGoatVaccinationPassport,
  type VaccinationPassport,
  type VaccinationPassportHistoryItem,
} from "@/lib/api/server";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";
const toneClass: Record<Tone, string> = {
  ok: "border-[#1f8f65] text-[#7dd3a7]",
  warn: "border-[#a16207] text-[#facc15]",
  dng: "border-[#b91c1c] text-[#fca5a5]",
  info: "border-[#0e7490] text-[#67e8f9]",
  mut: "border-[#334155] text-[#93a4b8]",
};
function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return <span className={`inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium ${toneClass[tone]}`}>{children}</span>;
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

const cardHd = "flex flex-wrap items-center gap-3 border-b border-[#334155] px-4 py-3";

// VaccinationPassportSection renders a goat's vaccination passport: next due, open obligations, last
// accepted, and the administered/verified history with proof status. Read-only.
export async function VaccinationPassportSection({ goatId }: { goatId: string }) {
  const res = await getGoatVaccinationPassport(goatId);
  if (!res.ok) {
    // Soft-fail: the rest of the identity passport still renders.
    return (
      <section className="rounded-xl border border-[#334155] bg-[#1A1D24]">
        <div className={cardHd}>
          <span aria-hidden>💉</span>
          <h2 className="text-sm font-bold text-white">Vaccination</h2>
        </div>
        <p className="px-4 py-4 text-sm text-[#8899AA]">Vaccination passport unavailable: {res.error.message ?? res.error.code}</p>
      </section>
    );
  }
  const p: VaccinationPassport = res.data;
  const history = p.vaccination_history ?? [];
  const open = p.open_obligations ?? [];

  return (
    <section className="rounded-xl border border-[#334155] bg-[#1A1D24]">
      <div className={cardHd}>
        <span aria-hidden>💉</span>
        <h2 className="text-sm font-bold text-white">Vaccination</h2>
        <Tag tone="mut">{history.length} dose{history.length === 1 ? "" : "s"}</Tag>
        {p.next_due ? (
          <span className="ml-auto text-xs text-[#8899AA]">
            next due <b className="text-[#c7d1dc]">{fmtDate(p.next_due.due_at)}</b> · dose {p.next_due.sequence}
          </span>
        ) : (
          <span className="ml-auto text-xs text-[#8899AA]">no upcoming dose</span>
        )}
      </div>

      <div className="grid gap-3 p-4 md:grid-cols-3">
        <div className="rounded-md border border-[#334155] px-3 py-2">
          <div className="text-xs uppercase text-[#93a4b8]">Next due</div>
          <div className="mt-1 text-sm text-white">
            {p.next_due ? (
              <>
                {fmtDate(p.next_due.due_at)} <Tag tone="warn">{p.next_due.status}</Tag>
              </>
            ) : (
              "—"
            )}
          </div>
        </div>
        <div className="rounded-md border border-[#334155] px-3 py-2">
          <div className="text-xs uppercase text-[#93a4b8]">Open obligations</div>
          <div className="mt-1 text-sm text-white">{open.length}</div>
        </div>
        <div className="rounded-md border border-[#334155] px-3 py-2">
          <div className="text-xs uppercase text-[#93a4b8]">Last accepted</div>
          <div className="mt-1 text-sm text-white">{p.last_accepted ? fmtDate(p.last_accepted.administered_at) : "—"}</div>
        </div>
      </div>

      <div className="border-t border-[#334155] px-4 py-2 text-xs font-semibold uppercase tracking-wide text-[#93a4b8]">
        History
      </div>
      <div className="overflow-auto" tabIndex={0} role="group" aria-label="Vaccination history">
        {history.length === 0 ? (
          <p className="px-4 py-5 text-sm text-[#8899AA]">
            No vaccination history yet. Once a source-backed protocol is published and a dose is administered + verified,
            it appears here with its proof/verification status and the source protocol version.
          </p>
        ) : (
          <table className="w-full min-w-[640px] border-collapse text-sm">
            <thead>
              <tr className="border-b border-[#334155] text-left text-[11px] uppercase tracking-wide text-[#93a4b8]">
                <th className="px-3 py-2">Administered</th>
                <th className="px-3 py-2">Doses</th>
                <th className="px-3 py-2">Route</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Proof</th>
                <th className="px-3 py-2">Source obligation</th>
              </tr>
            </thead>
            <tbody>
              {history.map((h) => (
                <tr key={h.completion_id} className="border-b border-[#23272f]">
                  <td className="px-3 py-2 text-[#c7d1dc]">{fmtDate(h.administered_at)}</td>
                  <td className="px-3 py-2 text-[#c7d1dc]">{h.doses}</td>
                  <td className="px-3 py-2 text-[#8899AA]">{h.route_site || "—"}</td>
                  <td className="px-3 py-2">
                    <Tag tone={statusTone(h.status)}>{h.status}</Tag>
                  </td>
                  <td className="px-3 py-2">{proofLabel(h)}</td>
                  <td className="px-3 py-2 font-mono text-[#8899AA]">{h.obligation_id.slice(0, 8)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
