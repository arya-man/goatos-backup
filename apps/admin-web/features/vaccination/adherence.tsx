import Link from "next/link";
import {
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  type ActionCenterObligation,
  type VaccinationQueueItem,
} from "@/lib/api/server";
import { PageHeader } from "@/components/admin-primitives";

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

function Kpi({ label, value, sub, tone = "mut" }: { label: string; value: React.ReactNode; sub?: string; tone?: Tone }) {
  const accent: Record<Tone, string> = {
    ok: "bg-[#4ade80]",
    warn: "bg-[#facc15]",
    dng: "bg-[#f87171]",
    info: "bg-[#14f1d9]",
    mut: "bg-[#334155]",
  };
  return (
    <div className="overflow-hidden rounded-xl border border-[#334155] bg-[#1A1D24]">
      <div className={`h-1 ${accent[tone]}`} />
      <div className="p-4">
        <div className="text-xs uppercase text-[#93a4b8]">{label}</div>
        <div className="mt-1 text-2xl font-bold text-white">{value}</div>
        {sub ? <div className="mt-1 text-xs text-[#8899AA]">{sub}</div> : null}
      </div>
    </div>
  );
}

const cols = ["Expected", "Actual", "Gap", "Severity", "Owner", "Next action", "Evidence"];

function ModuleCard({
  icon,
  title,
  badge,
  badgeTone,
  rightBadge,
  rows,
  emptyNote,
}: {
  icon: string;
  title: string;
  badge: string;
  badgeTone: Tone;
  rightBadge?: React.ReactNode;
  rows: React.ReactNode[];
  emptyNote: string;
}) {
  return (
    <section className="rounded-xl border border-[#334155] bg-[#1A1D24]">
      <div className="flex flex-wrap items-center gap-3 border-b border-[#334155] px-4 py-3">
        <span aria-hidden>{icon}</span>
        <h2 className="text-sm font-bold text-white">{title}</h2>
        <Tag tone={badgeTone}>{badge}</Tag>
        {rightBadge ? <span className="ml-auto">{rightBadge}</span> : null}
      </div>
      <div className="overflow-auto" tabIndex={0} role="group" aria-label={`${title} adherence table`}>
        <table className="w-full min-w-[760px] border-collapse text-sm">
          <thead>
            <tr className="border-b border-[#334155]">
              {cols.map((c) => (
                <th key={c} className="px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wide text-[#93a4b8]">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length > 0 ? (
              rows
            ) : (
              <tr>
                <td colSpan={cols.length} className="px-3 py-5 text-center text-sm text-[#8899AA]">
                  {emptyNote}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export async function ProtocolAdherencePage() {
  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter({ status: "due", limit: 200 }),
    getVaccinationVerificationQueue({ limit: 200 }),
  ]);
  const obligations: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  const queueItems: VaccinationQueueItem[] = queue.ok ? queue.data.items : [];
  const hasData = obligations.length > 0 || queueItems.length > 0;
  const today = new Date().toISOString().slice(0, 10);

  return (
    <div className="min-w-0">
      <PageHeader
        eyebrow="PHC · Vaccination"
        title="Protocol Adherence"
        description="Is the agreed process being followed? Adherence is COMPUTED from each obligation's expected rule vs the actual SOP submission + proof — Expected → Actual → Gap → Severity → Owner → Next → Evidence. On-track obligations are hidden; gaps, deferred/explained, and verification/rework surface here."
        actions={
          <Link href="/vaccination" className="inline-flex min-h-[40px] items-center rounded-md border border-[#334155] px-3 py-2 text-sm text-[#c7d1dc] hover:border-[#14f1d9]/40">
            ← Action Center
          </Link>
        }
      />

      <div className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-[#334155] bg-[#161922] px-4 py-2.5 text-xs">
        <span className="text-[#93a4b8]">Scope</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">all parks</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">all sheds</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">as of {today}</span>
        <span className="mx-1 h-4 w-px bg-[#334155]" />
        <Tag tone="warn">rules: Draft · pending source-backed approval</Tag>
      </div>

      <div className="mb-4 grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Kpi label="Overall adherence" value={hasData ? "—" : "n/a"} sub="needs published rules + proof" tone="warn" />
        <Kpi label="Open process gaps" value={0} sub="no published protocol" tone="mut" />
        <Kpi label="Due (not yet acted)" value={obligations.length} sub="from generated obligations" tone={obligations.length ? "warn" : "mut"} />
        <Kpi label="Awaiting verification" value={queueItems.length} sub="recorded doses" tone={queueItems.length ? "warn" : "mut"} />
      </div>

      <div className="mb-4 rounded-md border border-[#334155] bg-[#161922] px-4 py-3 text-sm text-[#8899AA]">
        Adherence is computed from <b className="text-[#c7d1dc]">published</b> rules. No vaccination protocol is published
        yet (source-backed gate), so there are no expected-vs-actual gaps to compute. Publish a source-backed schedule in
        the <b className="text-[#c7d1dc]">Config — Schedule Builder</b> (link above); gaps, deferred/explained obligations,
        and verification/rework then populate the cards below.
      </div>

      <div className="flex flex-col gap-4">
        <ModuleCard
          icon="💉"
          title="Vaccination"
          badge="adherence: pending"
          badgeTone="warn"
          rightBadge={<Tag tone="warn">rules: Draft · pending source-backed approval</Tag>}
          rows={[]}
          emptyNote="No computed gaps — publish a source-backed vaccination schedule to generate obligations, then expected-vs-actual gaps appear here (skipped, missing-proof, overdue, deferred)."
        />
        <ModuleCard
          icon="🎥"
          title="Verification / SOP proof"
          badge={`${queueItems.length} awaiting`}
          badgeTone={queueItems.length ? "warn" : "mut"}
          rows={[]}
          emptyNote="Recorded doses awaiting review + rework requests surface here once drives run. Act on them in the Action Center verification queue."
        />
        <ModuleCard
          icon="⏸"
          title="Deferred / explained"
          badge="SM-1 defer"
          badgeTone="mut"
          rows={[]}
          emptyNote="ICU / quarantine / sick goats are deferred by SM-1 and surfaced here (never silently skipped) — visible with an auto-resume-on-recovery next action."
        />
      </div>
    </div>
  );
}
