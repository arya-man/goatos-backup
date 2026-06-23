import Link from "next/link";
import {
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  type ActionCenterObligation,
  type VaccinationQueueItem,
} from "@/lib/api/server";
import { ErrorPanel, PageHeader } from "@/components/admin-primitives";
import { one, type RouteSearchParams } from "@/lib/search-params";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";

const toneClass: Record<Tone, string> = {
  ok: "border-[#1f8f65] text-[#7dd3a7]",
  warn: "border-[#a16207] text-[#facc15]",
  dng: "border-[#b91c1c] text-[#fca5a5]",
  info: "border-[#0e7490] text-[#67e8f9]",
  mut: "border-[#334155] text-[#93a4b8]",
};

function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return (
    <span className={`inline-flex items-center rounded-md border px-2 py-0.5 text-xs font-medium ${toneClass[tone]}`}>
      {children}
    </span>
  );
}

// Quick-filter work-state buckets (the mock's status board). URL-driven (?bucket=) so the screen
// stays a server component.
type Bucket = "all" | "overdue" | "today" | "verify" | "rework" | "deferred" | "completed";

const buckets: Array<{ id: Bucket; label: string }> = [
  { id: "all", label: "All" },
  { id: "overdue", label: "Overdue" },
  { id: "today", label: "Due today" },
  { id: "verify", label: "Awaiting verification" },
  { id: "rework", label: "Rework requested" },
  { id: "deferred", label: "Deferred / explained" },
  { id: "completed", label: "Completed recently" },
];

function normalizeBucket(v: string | undefined): Bucket {
  return (buckets.find((b) => b.id === v)?.id ?? "all") as Bucket;
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "—";
}

function fmtDate(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toISOString().slice(0, 10);
}

function isOverdue(iso: string): boolean {
  const d = new Date(iso);
  return !Number.isNaN(d.getTime()) && d.getTime() < Date.now();
}

function th(label: string) {
  return (
    <th className="whitespace-nowrap px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wide text-[#93a4b8]">
      {label}
    </th>
  );
}

function ActionButton({ children, tone = "mut", href }: { children: React.ReactNode; tone?: Tone; href?: string }) {
  const cls = `inline-flex min-h-[40px] items-center rounded-md border px-2.5 py-1.5 text-xs font-medium ${toneClass[tone]} hover:bg-[rgba(20,241,217,0.06)]`;
  if (href) {
    return (
      <Link href={href} className={cls}>
        {children}
      </Link>
    );
  }
  return (
    <button type="button" className={cls}>
      {children}
    </button>
  );
}

// SectionCard is the mock's per-state operational card: title + count tag + dense table or empty row.
function SectionCard({
  title,
  icon,
  count,
  tone,
  description,
  children,
}: {
  title: string;
  icon: string;
  count: number;
  tone: Tone;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-xl border border-[#334155] bg-[#1A1D24]">
      <div className="flex items-center gap-3 border-b border-[#334155] px-4 py-3">
        <span aria-hidden className="text-base">
          {icon}
        </span>
        <h2 className="text-sm font-bold text-white">{title}</h2>
        <Tag tone={tone}>{count}</Tag>
        <span className="ml-auto hidden text-xs text-[#8899AA] sm:block">{description}</span>
      </div>
      <div className="overflow-auto">{children}</div>
    </section>
  );
}

export async function VaccinationActionCenterPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const bucket = normalizeBucket(one(sp, "bucket"));
  const scope = one(sp, "scope") ?? "all parks";

  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter({ status: "due", limit: 200 }),
    getVaccinationVerificationQueue({ limit: 200 }),
  ]);

  const obligations: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  const queueItems: VaccinationQueueItem[] = queue.ok ? queue.data.items : [];

  const overdue = obligations.filter((o) => isOverdue(o.due_at));
  const counts: Record<Bucket, number> = {
    all: obligations.length + queueItems.length,
    overdue: overdue.length,
    today: obligations.length - overdue.length,
    verify: queueItems.length,
    rework: 0,
    deferred: 0,
    completed: 0,
  };

  const today = new Date().toISOString().slice(0, 10);
  const nothingLive = obligations.length === 0 && queueItems.length === 0;

  return (
    <div className="min-w-0">
      <PageHeader
        eyebrow="PHC · Vaccination"
        title="Action Center"
        description="The operational command surface — every vaccination obligation grouped by computed adherence/work state. Act on a card: start the SOP, submit proof, verify, reject, request rework, escalate, or open the goat passport. Every action writes the audit trail and ripples into Adherence + counts."
      />

      {/* Scope / date / source-state context bar */}
      <div className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-[#334155] bg-[#161922] px-4 py-2.5 text-xs">
        <span className="text-[#93a4b8]">Scope</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">{scope}</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">all sheds</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">as of {today}</span>
        <span className="mx-1 h-4 w-px bg-[#334155]" />
        <Tag tone="warn">rules: Draft · pending source-backed approval</Tag>
        <span className="ml-auto flex items-center gap-2">
          <Tag tone="mut">stock: not evaluated</Tag>
          <Tag tone="mut">proof policy: skeleton</Tag>
        </span>
      </div>

      {/* Quick-filter status board tabs */}
      <div className="mb-4 flex flex-wrap gap-2">
        {buckets.map((b) => {
          const active = b.id === bucket;
          const href = b.id === "all" ? "/vaccination" : `/vaccination?bucket=${b.id}`;
          return (
            <Link
              key={b.id}
              href={href}
              className={`inline-flex min-h-[40px] items-center gap-2 rounded-md border px-3 py-2 text-xs font-medium ${
                active
                  ? "border-[#14f1d9] bg-[rgba(20,241,217,0.08)] text-[#14f1d9]"
                  : "border-[#334155] text-[#c7d1dc] hover:border-[#14f1d9]/40"
              }`}
            >
              {b.label}
              <span className={`rounded px-1.5 py-0.5 text-[11px] ${active ? "bg-[#14f1d9]/15" : "bg-[#22262E]"}`}>
                {counts[b.id]}
              </span>
            </Link>
          );
        })}
      </div>

      {(!actionCenter.ok || !queue.ok) && (
        <div className="mb-4">{!actionCenter.ok ? <ErrorPanel error={actionCenter.error} /> : queue.ok ? null : <ErrorPanel error={queue.error} />}</div>
      )}

      {nothingLive ? (
        <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-8 text-center">
          <div className="mx-auto mb-3 text-2xl">🛈</div>
          <h2 className="text-base font-bold text-white">No live obligations yet — the command surface is ready</h2>
          <p className="mx-auto mt-2 max-w-2xl text-sm leading-6 text-[#8899AA]">
            The vaccination engine deliberately generates <b>nothing</b> until a protocol version is{" "}
            <b>published through the source-backed gate</b> (source_system ∈ vaccinations_db / phc / vet, approved
            review, named approver). No vaccine schedule values are invented. Once a real PHC schedule is published and
            generation runs, due obligations, SOP tasks, and the verification queue populate the buckets above — each row
            actionable (start SOP · submit proof · verify · reject · request rework · escalate · open passport).
          </p>
          <div className="mt-4 flex justify-center gap-2">
            <ActionButton tone="info" href="/vaccination/config">
              Open Config — Schedule Builder
            </ActionButton>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <SectionCard
            title="Due & overdue"
            icon="⏰"
            count={obligations.length}
            tone={overdue.length > 0 ? "dng" : "warn"}
            description="Scheduled directions due for execution"
          >
            {obligations.length === 0 ? (
              <p className="px-4 py-5 text-sm text-[#8899AA]">No due obligations.</p>
            ) : (
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b border-[#334155]">
                    {th("Goat")}
                    {th("Due")}
                    {th("Status")}
                    {th("Scope")}
                    {th("Next action")}
                  </tr>
                </thead>
                <tbody>
                  {obligations.map((o) => (
                    <tr key={o.obligation_id} className="border-b border-[#23272f]">
                      <td className="px-3 py-2 font-mono text-[#c7d1dc]">{shortId(o.target_id)}</td>
                      <td className="px-3 py-2 text-[#c7d1dc]">
                        {fmtDate(o.due_at)} {isOverdue(o.due_at) ? <Tag tone="dng">overdue</Tag> : null}
                      </td>
                      <td className="px-3 py-2">
                        <Tag tone="warn">{o.status}</Tag>
                      </td>
                      <td className="px-3 py-2 text-[#8899AA]">
                        {o.scope_type}:{shortId(o.scope_id)}
                      </td>
                      <td className="px-3 py-2">
                        <div className="flex gap-1.5">
                          <ActionButton tone="info">Start SOP</ActionButton>
                          <ActionButton tone="mut" href={`/goats/${o.target_id}`}>
                            Passport
                          </ActionButton>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </SectionCard>

          <SectionCard
            title="Awaiting verification"
            icon="🎥"
            count={queueItems.length}
            tone={queueItems.length > 0 ? "warn" : "mut"}
            description="Administered doses recorded, awaiting review"
          >
            {queueItems.length === 0 ? (
              <p className="px-4 py-5 text-sm text-[#8899AA]">Nothing awaiting verification.</p>
            ) : (
              <table className="w-full border-collapse text-sm">
                <thead>
                  <tr className="border-b border-[#334155]">
                    {th("Goat")}
                    {th("Administered")}
                    {th("Doses")}
                    {th("Verify")}
                  </tr>
                </thead>
                <tbody>
                  {queueItems.map((q) => (
                    <tr key={q.completion_id} className="border-b border-[#23272f]">
                      <td className="px-3 py-2 font-mono text-[#c7d1dc]">{shortId(q.goat_id)}</td>
                      <td className="px-3 py-2 text-[#c7d1dc]">{fmtDate(q.administered_at)}</td>
                      <td className="px-3 py-2 text-[#c7d1dc]">{q.doses}</td>
                      <td className="px-3 py-2">
                        <div className="flex gap-1.5">
                          <ActionButton tone="ok">Verify</ActionButton>
                          <ActionButton tone="dng">Reject</ActionButton>
                          <ActionButton tone="info">Request rework</ActionButton>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </SectionCard>

          <div className="grid gap-4 xl:grid-cols-3">
            <SectionCard title="Rework requested" icon="↩︎" count={0} tone="mut" description="Returned for re-do">
              <p className="px-4 py-5 text-sm text-[#8899AA]">None.</p>
            </SectionCard>
            <SectionCard title="Deferred / explained" icon="⏸" count={0} tone="mut" description="SM-1 defer (ICU / quarantine)">
              <p className="px-4 py-5 text-sm text-[#8899AA]">None — defers are surfaced here, never silently hidden.</p>
            </SectionCard>
            <SectionCard title="Completed recently" icon="✓" count={0} tone="ok" description="Closed by accepted proof">
              <p className="px-4 py-5 text-sm text-[#8899AA]">None yet.</p>
            </SectionCard>
          </div>
        </div>
      )}

      <p className="mt-4 text-xs text-[#8899AA]">
        Action wiring (verify / reject / rework / start-SOP) connects to the SM-5 verification endpoints in the next
        slice; Passport links are live now.
      </p>
    </div>
  );
}
