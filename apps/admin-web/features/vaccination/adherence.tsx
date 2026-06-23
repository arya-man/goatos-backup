import Link from "next/link";
import {
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  type ActionCenterObligation,
  type VaccinationQueueItem,
} from "@/lib/api/server";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";
function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return <span className={`tag t-${tone}`}>{children}</span>;
}

// Module-scope so the RSC render path stays pure (no new Date in render).
function todayIso(): string {
  return new Date().toISOString().slice(0, 10);
}

const accentVar: Record<Tone, string> = {
  ok: "var(--ok)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  mut: "var(--line)",
};

function Kpi({ label, value, sub, tone = "mut" }: { label: string; value: React.ReactNode; sub?: string; tone?: Tone }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentVar[tone] }} />
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
      {sub ? <div className="dl muted">{sub}</div> : null}
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
    <section className="card">
      <div className="hd">
        <span aria-hidden>{icon}</span>
        <h3>{title}</h3>
        <Tag tone={badgeTone}>{badge}</Tag>
        <div className="sp" />
        {rightBadge ?? null}
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={`${title} adherence table`}>
        <table>
          <thead>
            <tr>
              {cols.map((c) => (
                <th key={c}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.length > 0 ? (
              rows
            ) : (
              <tr>
                <td colSpan={cols.length} className="muted" style={{ textAlign: "center", padding: "18px 12px" }}>
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
  const today = todayIso();

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            PHC · <b>Vaccination</b>
          </div>
          <h1>Protocol Adherence</h1>
          <div className="sub">
            Is the agreed process being followed? Adherence is computed from each obligation&apos;s expected rule vs the
            actual SOP submission + proof — Expected → Actual → Gap → Severity → Owner → Next → Evidence. On-track
            obligations are hidden; gaps, deferred/explained, and verification/rework surface here.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/vaccination" className="btn">
          ← Action Center
        </Link>
      </div>

      <div className="fchipsbar" style={{ marginBottom: 14 }}>
        <span className="muted small">Scope</span>
        <Tag tone="mut">all parks</Tag>
        <Tag tone="mut">all sheds</Tag>
        <Tag tone="mut">as of {today}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <Tag tone="warn">rules: Draft · pending source-backed approval</Tag>
      </div>

      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi label="Overall adherence" value={hasData ? "—" : "n/a"} sub="needs published rules + proof" tone="warn" />
        <Kpi label="Open process gaps" value={0} sub="no published protocol" tone="mut" />
        <Kpi
          label="Due (not yet acted)"
          value={obligations.length}
          sub="from generated obligations"
          tone={obligations.length ? "warn" : "mut"}
        />
        <Kpi
          label="Awaiting verification"
          value={queueItems.length}
          sub="recorded doses"
          tone={queueItems.length ? "warn" : "mut"}
        />
      </div>

      <div className="note" style={{ marginBottom: 14 }}>
        Adherence is computed from <b>published</b> rules. No vaccination protocol is published yet (source-backed gate),
        so there are no expected-vs-actual gaps to compute. Publish a source-backed schedule in the{" "}
        <Link href="/vaccination/config" className="lk">
          Config — Schedule Builder
        </Link>
        ; gaps, deferred/explained obligations, and verification/rework then populate the cards below.
      </div>

      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
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
