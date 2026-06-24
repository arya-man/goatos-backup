import Link from "next/link";
import { redirect } from "next/navigation";
import { Activity, AlertTriangle, ShieldCheck, Syringe } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
} from "@/lib/api/server";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";

const accentVar: Record<Tone, string> = {
  ok: "var(--brand)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  mut: "var(--line)",
};

function Kpi({
  label,
  value,
  sub,
  tone,
  href,
  icon,
}: {
  label: string;
  value: React.ReactNode;
  sub?: React.ReactNode;
  tone: Tone;
  href: string;
  icon: React.ReactNode;
}) {
  return (
    <Link href={href} className="kpi" style={{ display: "block", cursor: "pointer" }}>
      <span className="acc" style={{ background: accentVar[tone] }} />
      <div className="lab">
        {icon}
        {label}
      </div>
      <div className="val">{value}</div>
      {sub ? <div className="dl muted">{sub}</div> : null}
    </Link>
  );
}

// Module-scope so the RSC render path stays pure (no new Date in render).
function todayLabel(): string {
  return new Date().toISOString().slice(0, 10);
}
function fmtInt(n: number): string {
  return n.toLocaleString("en-IN");
}

export async function ControlTowerPage() {
  const [vaccDue, vaccVerify] = await Promise.all([
    getVaccinationActionCenter({ status: "due", limit: 200 }),
    getVaccinationVerificationQueue({ limit: 200 }),
  ]);
  const authError = firstAuthRequiredError(vaccDue, vaccVerify);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const dueCount = vaccDue.ok ? vaccDue.data.items.length : null;
  const verifyCount = vaccVerify.ok ? vaccVerify.data.items.length : null;
  const openWorkCount = dueCount === null || verifyCount === null ? null : dueCount + verifyCount;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>Control Tower</h1>
          <div className="sub">
            <b>Watch</b> — operational gaps, adherence, exceptions, and next actions · All parks · as of {todayLabel()}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/vaccination" className="btn p">
          Action Center →
        </Link>
      </div>

      {/* Operational attention KPIs. Raw census/counts live in the Counts vertical, not Control Tower. */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi
          label="Vaccination due"
          value={dueCount === null ? "n/a" : fmtInt(dueCount)}
          sub="PHC / Vaccination"
          tone={dueCount && dueCount > 0 ? "warn" : "mut"}
          href="/vaccination"
          icon={<Syringe className="ic" />}
        />
        <Kpi
          label="Verify queue"
          value={verifyCount === null ? "n/a" : fmtInt(verifyCount)}
          sub="proof review"
          tone="info"
          href="/vaccination"
          icon={<ShieldCheck className="ic" />}
        />
        <Kpi
          label="Open PHC work"
          value={openWorkCount === null ? "n/a" : fmtInt(openWorkCount)}
          sub="due + proof review"
          tone={openWorkCount && openWorkCount > 0 ? "warn" : "mut"}
          href="/vaccination"
          icon={<Activity className="ic" />}
        />
        <Kpi
          label="Protocol config"
          value="Locked"
          sub="source-backed only"
          tone="mut"
          href="/config?category=vaccination"
          icon={<AlertTriangle className="ic" />}
        />
      </div>

      <div className="grid g2" style={{ marginBottom: 14 }}>
        <section className="card">
          <div className="hd">
            <Syringe className="ic" style={{ color: "var(--brand)" }} />
            <h3>PHC · Vaccination</h3>
            <div className="sp" />
            <Link href="/vaccination" className="btn gh sm">
              Action Center →
            </Link>
          </div>
          <div className="bd">
            <div className="grid g2">
              <div className="metagrid" style={{ gridTemplateColumns: "1fr 1fr" }}>
                <div>
                  <div className="k">Due / overdue</div>
                  <div className="v">{dueCount === null ? "n/a" : fmtInt(dueCount)}</div>
                </div>
                <div>
                  <div className="k">Awaiting verification</div>
                  <div className="v">{verifyCount === null ? "n/a" : fmtInt(verifyCount)}</div>
                </div>
              </div>
            </div>
            <div className="note" style={{ marginTop: 12 }}>
              No vaccination protocol is published yet (source-backed gate), so the engine generates no obligations.
              Author + publish a source-backed schedule in{" "}
              <Link href="/config?category=vaccination" className="lk">
                Config — Protocol Rules
              </Link>{" "}
              — due work, the verification queue, and{" "}
              <Link href="/vaccination/adherence" className="lk">
                adherence
              </Link>{" "}
              then populate here.
            </div>
            <div className="note" style={{ marginTop: 10 }}>
              Census and herd-count KPIs belong to the Counts vertical, not Control Tower. This page only shows work
              that needs operational attention.
            </div>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <ShieldCheck className="ic" style={{ color: "var(--brand)" }} />
            <h3>Current product scope</h3>
            <div className="sp" />
          </div>
          <div className="bd">
            <div className="feed">
              <Link href="/vaccination" className="fitem" style={{ cursor: "pointer" }}>
                <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                  <Syringe className="ic" />
                </span>
                <div className="tx">
                  <b>PHC / Vaccination / Action</b>
                  <div className="mt">Due work, verification state, and evidence-driven actions.</div>
                </div>
              </Link>
              <Link href="/config?category=vaccination" className="fitem" style={{ cursor: "pointer" }}>
                <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                  <ShieldCheck className="ic" />
                </span>
                <div className="tx">
                  <b>Admin / Data Ops / Config</b>
                  <div className="mt">Generic protocol rules, source review, and publish gate (CEO/COO).</div>
                </div>
              </Link>
              <Link href="/vaccination/adherence" className="fitem" style={{ cursor: "pointer" }}>
                <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                  <Activity className="ic" />
                </span>
                <div className="tx">
                  <b>PHC / Vaccination / Adherence</b>
                  <div className="mt">Expected vs actual, gap, owner, next action, and evidence.</div>
                </div>
              </Link>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
