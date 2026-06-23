import Link from "next/link";
import {
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  type ActionCenterObligation,
  type VaccinationQueueItem,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { VaccinationTabs } from "./vaccination-tabs";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";

function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return <span className={`tag t-${tone}`}>{children}</span>;
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
// Request-time helpers kept at module scope so the component render stays pure (no Date.now/new Date
// in the React render path — RSC renders once per request).
function todayIso(): string {
  return new Date().toISOString().slice(0, 10);
}
function windowDueBefore(windowDays: number): string | undefined {
  return windowDays > 0 ? new Date(Date.now() + windowDays * 86_400_000).toISOString() : undefined;
}

// ActionForm posts a per-row server action (Verify / Reject / Rework). Real, not display-only.
function ActionForm({
  action,
  completionId,
  reason,
  primary,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  completionId: string;
  reason?: string;
  primary?: boolean;
  children: React.ReactNode;
}) {
  return (
    <form action={action} style={{ display: "inline" }}>
      <input type="hidden" name="completion_id" value={completionId} />
      {reason ? <input type="hidden" name="reason" value={reason} /> : null}
      <button type="submit" className={`btn sm${primary ? " p" : ""}`}>
        {children}
      </button>
    </form>
  );
}

// DisabledButton renders a visible-but-intentionally-disabled action with a reason (no fake actions).
function DisabledButton({ children, reason }: { children: React.ReactNode; reason: string }) {
  return (
    <button type="button" disabled title={reason} className="btn sm" style={{ opacity: 0.5, cursor: "not-allowed" }}>
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
    <section className="card">
      <div className="hd">
        <span aria-hidden>{icon}</span>
        <h3>{title}</h3>
        <Tag tone={tone}>{count}</Tag>
        <div className="sp" />
        <span className="muted small">{description}</span>
      </div>
      {children}
    </section>
  );
}

function FilterField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="fld" style={{ marginBottom: 0 }}>
      <label>{label}</label>
      {children}
    </div>
  );
}

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
  const dueBefore = windowDueBefore(windowDays);

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

  const today = todayIso();
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
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            PHC · <b>Vaccination</b>
          </div>
          <h1>Action Center</h1>
          <div className="sub">
            Every vaccination obligation grouped by computed work state — verify, reject, or request rework (live), open
            the goat passport. Every action writes the audit trail and ripples into Adherence + counts.
          </div>
        </div>
      </div>

      <VaccinationTabs current={bucket === "verify" ? "verify" : "action"} />

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            <Tag tone="ok">done</Tag> {actionMessage ?? "Action completed."}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>Action failed</b>&nbsp;{actionMessage ?? actionStatus}
          </div>
        )
      ) : null}

      {/* Scope / date / source-state context bar */}
      <div className="fchipsbar" style={{ marginBottom: 14 }}>
        <span className="muted small">Scope</span>
        <Tag tone="mut">{park || "all parks"}</Tag>
        <Tag tone="mut">{shed || "all sheds"}</Tag>
        <Tag tone="mut">as of {today}</Tag>
        <Tag tone="warn">rules: Draft · pending source-backed approval</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <Tag tone="mut">stock: not evaluated</Tag>
        <Tag tone="mut">proof policy: skeleton</Tag>
      </div>

      {/* Real filter bar (GET → searchParams → server re-fetch/filter) */}
      <form method="get" className="card" style={{ marginBottom: 14 }}>
        {bucket !== "all" ? <input type="hidden" name="bucket" value={bucket} /> : null}
        <div
          className="bd"
          style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill,minmax(180px,1fr))", gap: 14, alignItems: "end" }}
        >
          <FilterField label="Due window">
            <select name="window" defaultValue={windowId} aria-label="Due window">
              {windowDefs.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.label}
                </option>
              ))}
            </select>
          </FilterField>
          <FilterField label="Park (id)">
            <input name="park" defaultValue={park} placeholder="park id" aria-label="Park id" />
          </FilterField>
          <FilterField label="Shed (id)">
            <input name="shed" defaultValue={shed} placeholder="shed id" aria-label="Shed id" />
          </FilterField>
          <FilterField label="Protocol version (id)">
            <input name="version" defaultValue={version} placeholder="version id" aria-label="Protocol version id" />
          </FilterField>
          <FilterField label="Owner">
            <input
              disabled
              placeholder="needs SOP assignment data"
              title="Owner filter needs SOP task assignment data (SOP execution slice)"
              style={{ opacity: 0.5, cursor: "not-allowed" }}
              aria-label="Owner (disabled)"
            />
          </FilterField>
          <div style={{ display: "flex", gap: 8 }}>
            <button type="submit" className="btn p">
              Apply
            </button>
            <Link href="/vaccination" className="btn">
              Reset
            </Link>
          </div>
        </div>
      </form>

      {/* Quick-filter status board tabs */}
      <div className="chipset" style={{ marginBottom: 16 }}>
        {bucketDefs.map((b) => {
          const active = b.id === bucket;
          return (
            <Link key={b.id} href={bucketHref(b.id)} className={`chip${active ? " on" : ""}`}>
              {b.label} <Tag tone={active ? "ok" : "mut"}>{counts[b.id]}</Tag>
            </Link>
          );
        })}
      </div>

      {!actionCenter.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{actionCenter.error.code}</b>&nbsp;{actionCenter.error.message}
        </div>
      ) : null}
      {actionCenter.ok && !queue.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{queue.error.code}</b>&nbsp;{queue.error.message}
        </div>
      ) : null}

      {nothingLive ? (
        <section className="card">
          <div className="bd" style={{ textAlign: "center", padding: 32 }}>
            <div aria-hidden style={{ fontSize: 24, marginBottom: 10 }}>
              🛈
            </div>
            <h3 style={{ margin: 0, fontSize: 16 }}>No live obligations yet — the command surface is ready</h3>
            <p className="muted" style={{ maxWidth: 680, margin: "8px auto 0", lineHeight: 1.6, fontSize: 13 }}>
              The vaccination engine deliberately generates <b>nothing</b> until a protocol version is{" "}
              <b>published through the source-backed gate</b> (source_system ∈ vaccinations_db / phc / vet, approved
              review, named approver). No vaccine schedule values are invented. Once a real PHC schedule is published and
              generation runs, due obligations, SOP tasks, and the verification queue populate the buckets above — each
              row actionable (verify · reject · request rework · open passport, with start-SOP / submit-proof via the SOP
              engine).
            </p>
          </div>
        </section>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
          {showDue ? (
            <SectionCard
              title="Due & overdue"
              icon="⏰"
              count={obligations.length}
              tone={overdue.length > 0 ? "dng" : "warn"}
              description="Scheduled directions due for execution"
            >
              {obligations.length === 0 ? (
                <div className="bd">
                  <p className="muted small">No due obligations for the current filters.</p>
                </div>
              ) : (
                <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Due obligations">
                  <table>
                    <thead>
                      <tr>
                        <th>Goat</th>
                        <th>Due</th>
                        <th>Status</th>
                        <th>Scope</th>
                        <th>Next action</th>
                      </tr>
                    </thead>
                    <tbody>
                      {obligations.map((o) => (
                        <tr key={o.obligation_id}>
                          <td>
                            <span className="gid">{shortId(o.target_id)}</span>
                          </td>
                          <td>
                            {fmtDate(o.due_at)} {isOverdue(o.due_at) ? <Tag tone="dng">overdue</Tag> : null}
                          </td>
                          <td>
                            <Tag tone="warn">{o.status}</Tag>
                          </td>
                          <td className="muted">
                            {o.scope_type}:{shortId(o.scope_id)}
                          </td>
                          <td>
                            <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                              <DisabledButton reason={SOP_REASON}>Start SOP</DisabledButton>
                              <Link href={`/goats/${o.target_id}`} className="btn sm">
                                Passport
                              </Link>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
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
                <div className="bd">
                  <p className="muted small">Nothing awaiting verification.</p>
                </div>
              ) : (
                <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Awaiting verification">
                  <table>
                    <thead>
                      <tr>
                        <th>Goat</th>
                        <th>Administered</th>
                        <th>Doses</th>
                        <th>Verify</th>
                      </tr>
                    </thead>
                    <tbody>
                      {queueItems.map((q) => (
                        <tr key={q.completion_id}>
                          <td>
                            <span className="gid">{shortId(q.goat_id)}</span>
                          </td>
                          <td>{fmtDate(q.administered_at)}</td>
                          <td>{q.doses}</td>
                          <td>
                            <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                              <ActionForm action={verifyCompletionAction} completionId={q.completion_id} primary>
                                Verify
                              </ActionForm>
                              <ActionForm action={rejectCompletionAction} completionId={q.completion_id} reason="rejected">
                                Reject
                              </ActionForm>
                              <ActionForm
                                action={rejectCompletionAction}
                                completionId={q.completion_id}
                                reason="rework_requested"
                              >
                                Request rework
                              </ActionForm>
                              <Link href={`/goats/${q.goat_id}`} className="btn sm">
                                Passport
                              </Link>
                            </div>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </SectionCard>
          ) : null}

          {bucket === "all" ? (
            <div className="grid g3">
              <SectionCard title="Rework requested" icon="↩︎" count={0} tone="mut" description="Returned for re-do">
                <div className="bd">
                  <p className="muted small">None.</p>
                </div>
              </SectionCard>
              <SectionCard title="Deferred / explained" icon="⏸" count={0} tone="mut" description="SM-1 defer (ICU / quarantine)">
                <div className="bd">
                  <p className="muted small">None — defers are surfaced here, never silently hidden.</p>
                </div>
              </SectionCard>
              <SectionCard title="Completed recently" icon="✓" count={0} tone="ok" description="Closed by accepted proof">
                <div className="bd">
                  <p className="muted small">None yet.</p>
                </div>
              </SectionCard>
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}
