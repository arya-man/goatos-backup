import {
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  type ActionCenterObligation,
  type VaccinationQueueItem,
} from "@/lib/api/server";
import { ErrorPanel, PageHeader, Panel, StatPill } from "@/components/admin-primitives";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";

function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  const tones: Record<Tone, string> = {
    ok: "border-[#1f8f65] text-[#7dd3a7]",
    warn: "border-[#a16207] text-[#facc15]",
    dng: "border-[#b91c1c] text-[#fca5a5]",
    info: "border-[#0e7490] text-[#67e8f9]",
    mut: "border-[#334155] text-[#93a4b8]",
  };
  return (
    <span className={`inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium ${tones[tone]}`}>
      {children}
    </span>
  );
}

function statusTone(status: string): Tone {
  switch (status) {
    case "due":
      return "warn";
    case "in_progress":
      return "info";
    case "scheduled":
      return "mut";
    default:
      return "mut";
  }
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "—";
}

function fmtDate(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toISOString().slice(0, 10);
}

function th(label: string) {
  return (
    <th className="whitespace-nowrap px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-[#93a4b8]">
      {label}
    </th>
  );
}

export async function VaccinationActionCenterPage() {
  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter({ status: "due", limit: 100 }),
    getVaccinationVerificationQueue({ limit: 100 }),
  ]);

  const obligations: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  const queueItems: VaccinationQueueItem[] = queue.ok ? queue.data.items : [];

  return (
    <div className="min-w-0">
      <PageHeader
        eyebrow="PHC · Vaccination"
        title="Action Center"
        description="Vaccination obligations due for execution, and completed doses awaiting verification. Every obligation flows from a published, source-backed protocol — work is closed by SOP proof and verification."
      />

      <div className="mb-5 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatPill label="Due now" value={obligations.length} tone={obligations.length > 0 ? "warn" : "neutral"} />
        <StatPill label="Awaiting verification" value={queueItems.length} tone={queueItems.length > 0 ? "warn" : "neutral"} />
        <StatPill label="Published protocols" value={<span className="text-[#facc15]">draft only</span>} tone="warn" />
        <StatPill label="Source-backed" value="pending PHC values" tone="warn" />
      </div>

      <div className="mb-4 rounded-md border border-[#a16207] bg-[#1f1a07] px-4 py-3 text-sm text-[#facc15]">
        Rules: <b>Draft · pending source-backed approval.</b> No vaccine schedule is published yet — the engine refuses to
        generate obligations until a PHC/vet/vaccinations-db source-backed protocol version passes the publish gate.
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Panel title="Due obligations" description="Scheduled vaccination directions due for execution (per goat).">
          {!actionCenter.ok ? (
            <ErrorPanel error={actionCenter.error} />
          ) : obligations.length === 0 ? (
            <p className="py-6 text-center text-sm text-[#8899AA]">
              No due obligations. They appear here once a source-backed protocol is published and generation runs.
            </p>
          ) : (
            <div className="overflow-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b border-[#334155]">
                    {th("Goat")}
                    {th("Due")}
                    {th("Status")}
                    {th("Scope")}
                  </tr>
                </thead>
                <tbody>
                  {obligations.map((o) => (
                    <tr key={o.obligation_id} className="border-b border-[#23272f]">
                      <td className="px-3 py-2 font-mono text-[#c7d1dc]">{shortId(o.target_id)}</td>
                      <td className="px-3 py-2 text-[#c7d1dc]">{fmtDate(o.due_at)}</td>
                      <td className="px-3 py-2">
                        <Tag tone={statusTone(o.status)}>{o.status}</Tag>
                      </td>
                      <td className="px-3 py-2 text-[#8899AA]">
                        {o.scope_type}:{shortId(o.scope_id)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel title="Verification queue" description="Administered doses recorded, awaiting accept / reject / rework.">
          {!queue.ok ? (
            <ErrorPanel error={queue.error} />
          ) : queueItems.length === 0 ? (
            <p className="py-6 text-center text-sm text-[#8899AA]">Nothing awaiting verification.</p>
          ) : (
            <div className="overflow-auto">
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b border-[#334155]">
                    {th("Goat")}
                    {th("Administered")}
                    {th("Doses")}
                    {th("Action")}
                  </tr>
                </thead>
                <tbody>
                  {queueItems.map((q) => (
                    <tr key={q.completion_id} className="border-b border-[#23272f]">
                      <td className="px-3 py-2 font-mono text-[#c7d1dc]">{shortId(q.goat_id)}</td>
                      <td className="px-3 py-2 text-[#c7d1dc]">{fmtDate(q.administered_at)}</td>
                      <td className="px-3 py-2 text-[#c7d1dc]">{q.doses}</td>
                      <td className="px-3 py-2">
                        <Tag tone="info">verify</Tag>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      </div>
    </div>
  );
}
