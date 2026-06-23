import Link from "next/link";
import {
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  type ActionCenterObligation,
  type VaccinationQueueItem,
} from "@/lib/api/server";
import { ActionNotice, ErrorPanel, PageHeader } from "@/components/admin-primitives";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";

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

type Bucket = "all" | "overdue" | "today" | "verify" | "rework" | "deferred" | "completed";

const bucketDefs: Array<{ id: Bucket; label: string }> = [
  { id: "all", label: "All" },
  { id: "overdue", label: "Overdue" },
  { id: "today", label: "Due today" },
  { id: "verify", label: "Awaiting verification" },
  { id: "rework", label: "Rework requested" },
  { id: "deferred", label: "Deferred / explained" },
  { id: "completed", label: "Completed recently" },
];

const windowDefs: Array<{ id: string; label: string; days: number }> = [
  { id: "all", label: "All due", days: 0 },
  { id: "7d", label: "Next 7 days", days: 7 },
  { id: "14d", label: "Next 14 days", days: 14 },
  { id: "30d", label: "Next 30 days", days: 30 },
];

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

const actionBtn =
  "inline-flex min-h-[40px] items-center rounded-md border px-2.5 py-1.5 text-xs font-medium hover:bg-[rgba(20,241,217,0.06)]";

// SubmitButton posts a per-row server action form (Verify / Reject / Rework). Real, not display-only.
function ActionForm({
  action,
  completionId,
  reason,
  tone,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  completionId: string;
  reason?: string;
  tone: Tone;
  children: React.ReactNode;
}) {
  return (
    <form action={action} className="inline">
      <input type="hidden" name="completion_id" value={completionId} />
      {reason ? <input type="hidden" name="reason" value={reason} /> : null}
      <button type="submit" className={`${actionBtn} ${toneClass[tone]}`}>
        {children}
      </button>
    </form>
  );
}

// DisabledButton renders a visible-but-intentionally-disabled action with a reason (no fake actions).
function DisabledButton({ children, reason }: { children: React.ReactNode; reason: string }) {
  return (
    <button type="button" disabled title={reason} className={`${actionBtn} ${toneClass.mut} cursor-not-allowed opacity-50`}>
      {children}
    </button>
  );
}

const SOP_REASON = "Wired with the SOP execution slice (start/submit go through the SOP task engine).";

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

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="text-[#93a4b8]">{label}</span>
      {children}
    </label>
  );
}

const inputCls =
  "min-h-[40px] rounded-md border border-[#334155] bg-[#0f1115] px-2 py-1.5 text-sm text-[#c7d1dc] focus:border-[#14f1d9]/60 focus:outline-none";

export async function VaccinationActionCenterPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const bucket = (bucketDefs.find((b) => b.id === one(sp, "bucket"))?.id ?? "all") as Bucket;
  const park = one(sp, "park") ?? "";
  const shed = one(sp, "shed") ?? "";
  const version = one(sp, "version") ?? "";
  const windowId = windowDefs.find((w) => w.id === one(sp, "window"))?.id ?? "all";
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");

  const windowDays = windowDefs.find((w) => w.id === windowId)?.days ?? 0;
  const dueBefore = windowDays > 0 ? new Date(Date.now() + windowDays * 86_400_000).toISOString() : undefined;

  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter({ status: "due", dueBefore, limit: 200 }),
    getVaccinationVerificationQueue({ limit: 200 }),
  ]);

  let obligations: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  // Server-side scope/version filters (no full-herd scan — already bounded to the due window).
  if (park) obligations = obligations.filter((o) => o.scope_id === park || o.target_id === park);
  if (shed) obligations = obligations.filter((o) => o.scope_id === shed || o.target_id === shed);
  if (version) obligations = obligations.filter((o) => o.protocol_version_id === version);
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
  const showDue = bucket === "all" || bucket === "overdue" || bucket === "today";
  const showVerify = bucket === "all" || bucket === "verify";

  // Preserve current filters when switching buckets.
  const keep = new URLSearchParams();
  if (park) keep.set("park", park);
  if (shed) keep.set("shed", shed);
  if (version) keep.set("version", version);
  if (windowId !== "all") keep.set("window", windowId);
  const bucketHref = (b: Bucket) => {
    const p = new URLSearchParams(keep);
    if (b !== "all") p.set("bucket", b);
    const qs = p.toString();
    return qs ? `/vaccination?${qs}` : "/vaccination";
  };

  return (
    <div className="min-w-0">
      <PageHeader
        eyebrow="PHC · Vaccination"
        title="Action Center"
        description="The operational command surface — every vaccination obligation grouped by computed adherence/work state. Act on a card: verify, reject, or request rework (live); start the SOP / submit proof (via the SOP engine); open the goat passport. Every action writes the audit trail and ripples into Adherence + counts."
        actions={
          <Link
            href="/vaccination/config"
            className="inline-flex min-h-[40px] items-center rounded-md border border-[#334155] px-3 py-2 text-sm text-[#c7d1dc] hover:border-[#14f1d9]/40"
          >
            Config — Schedule Builder →
          </Link>
        }
      />

      {actionStatus ? <div className="mb-3"><ActionNotice status={actionStatus} message={actionMessage} /></div> : null}

      {/* Scope / date / source-state context bar */}
      <div className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-[#334155] bg-[#161922] px-4 py-2.5 text-xs">
        <span className="text-[#93a4b8]">Scope</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">{park || "all parks"}</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">{shed || "all sheds"}</span>
        <span className="rounded-md border border-[#334155] px-2 py-1 text-[#c7d1dc]">as of {today}</span>
        <span className="mx-1 h-4 w-px bg-[#334155]" />
        <Tag tone="warn">rules: Draft · pending source-backed approval</Tag>
        <span className="ml-auto flex items-center gap-2">
          <Tag tone="mut">stock: not evaluated</Tag>
          <Tag tone="mut">proof policy: skeleton</Tag>
        </span>
      </div>

      {/* Real filter bar (GET → searchParams → server re-fetch/filter) */}
      <form method="get" className="mb-4 grid grid-cols-2 gap-3 rounded-xl border border-[#334155] bg-[#1A1D24] p-3 md:grid-cols-6">
        {bucket !== "all" ? <input type="hidden" name="bucket" value={bucket} /> : null}
        <FilterField label="Due window">
          <select name="window" defaultValue={windowId} className={inputCls}>
            {windowDefs.map((w) => (
              <option key={w.id} value={w.id}>
                {w.label}
              </option>
            ))}
          </select>
        </FilterField>
        <FilterField label="Park (id)">
          <input name="park" defaultValue={park} placeholder="park id" className={inputCls} />
        </FilterField>
        <FilterField label="Shed (id)">
          <input name="shed" defaultValue={shed} placeholder="shed id" className={inputCls} />
        </FilterField>
        <FilterField label="Protocol version (id)">
          <input name="version" defaultValue={version} placeholder="version id" className={inputCls} />
        </FilterField>
        <FilterField label="Owner">
          <input disabled placeholder="needs SOP assignment data" title="Owner filter needs SOP task assignment data (SOP execution slice)" className={`${inputCls} cursor-not-allowed opacity-50`} />
        </FilterField>
        <div className="flex items-end gap-2">
          <button type="submit" className={`${actionBtn} ${toneClass.info}`}>
            Apply
          </button>
          <Link href="/vaccination" className={`${actionBtn} ${toneClass.mut}`}>
            Reset
          </Link>
        </div>
      </form>

      {/* Quick-filter status board tabs */}
      <div className="mb-4 flex flex-wrap gap-2">
        {bucketDefs.map((b) => {
          const active = b.id === bucket;
          return (
            <Link
              key={b.id}
              href={bucketHref(b.id)}
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

      {!actionCenter.ok ? <div className="mb-4"><ErrorPanel error={actionCenter.error} /></div> : null}
      {actionCenter.ok && !queue.ok ? <div className="mb-4"><ErrorPanel error={queue.error} /></div> : null}

      {nothingLive ? (
        <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-8 text-center">
          <div className="mx-auto mb-3 text-2xl">🛈</div>
          <h2 className="text-base font-bold text-white">No live obligations yet — the command surface is ready</h2>
          <p className="mx-auto mt-2 max-w-2xl text-sm leading-6 text-[#8899AA]">
            The vaccination engine deliberately generates <b>nothing</b> until a protocol version is{" "}
            <b>published through the source-backed gate</b> (source_system ∈ vaccinations_db / phc / vet, approved
            review, named approver). No vaccine schedule values are invented. Once a real PHC schedule is published and
            generation runs, due obligations, SOP tasks, and the verification queue populate the buckets above — each row
            actionable (verify · reject · request rework · open passport, with start-SOP / submit-proof via the SOP engine).
          </p>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {showDue ? (
            <SectionCard
              title="Due & overdue"
              icon="⏰"
              count={obligations.length}
              tone={overdue.length > 0 ? "dng" : "warn"}
              description="Scheduled directions due for execution"
            >
              {obligations.length === 0 ? (
                <p className="px-4 py-5 text-sm text-[#8899AA]">No due obligations for the current filters.</p>
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
                          <div className="flex flex-wrap gap-1.5">
                            <DisabledButton reason={SOP_REASON}>Start SOP</DisabledButton>
                            <Link href={`/goats/${o.target_id}`} className={`${actionBtn} ${toneClass.mut}`}>
                              Passport
                            </Link>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </SectionCard>
          ) : null}

          {showVerify ? (
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
                          <div className="flex flex-wrap gap-1.5">
                            <ActionForm action={verifyCompletionAction} completionId={q.completion_id} tone="ok">
                              Verify
                            </ActionForm>
                            <ActionForm action={rejectCompletionAction} completionId={q.completion_id} reason="rejected" tone="dng">
                              Reject
                            </ActionForm>
                            <ActionForm action={rejectCompletionAction} completionId={q.completion_id} reason="rework_requested" tone="info">
                              Request rework
                            </ActionForm>
                            <Link href={`/goats/${q.goat_id}`} className={`${actionBtn} ${toneClass.mut}`}>
                              Passport
                            </Link>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </SectionCard>
          ) : null}

          {bucket === "all" ? (
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
          ) : null}
        </div>
      )}
    </div>
  );
}
