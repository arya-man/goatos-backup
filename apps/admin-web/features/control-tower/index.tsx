import Link from "next/link";
import { redirect } from "next/navigation";
import { Activity, AlertTriangle, DatabaseZap, FileSearch, Search, Syringe } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getAdminRuntimeStatus,
  getIdentityCounts,
  getImportRun,
  getReviewSummary,
  getVaccinationActionCenter,
  getVaccinationVerificationQueue,
  listConflicts,
  listImportRuns,
  searchGoats,
} from "@/lib/api/server";
import { dash, shortId } from "@/lib/format";

type Tone = "ok" | "warn" | "dng" | "info" | "mut";
function Tag({ tone, children }: { tone: Tone; children: React.ReactNode }) {
  return <span className={`tag t-${tone}`}>{children}</span>;
}

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
  const runtime = getAdminRuntimeStatus();
  const [counts, reviewSummary, conflicts, herd, recentRuns, vaccDue, vaccVerify] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 20 }),
    getReviewSummary(),
    listConflicts({ limit: 5, state: "open" }),
    searchGoats({ limit: 6 }),
    runtime.importRunId ? Promise.resolve(null) : listImportRuns({ limit: 1 }),
    getVaccinationActionCenter({ status: "due", limit: 200 }),
    getVaccinationVerificationQueue({ limit: 200 }),
  ]);
  const authError = firstAuthRequiredError(counts, reviewSummary, conflicts, herd, recentRuns);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const selectedImportRunId =
    runtime.importRunId ?? (recentRuns?.ok ? recentRuns.data.items[0]?.import_run_id ?? null : null);
  const importRun = selectedImportRunId ? await getImportRun(selectedImportRunId) : null;
  if (firstAuthRequiredError(importRun)) redirect(INTERNAL_LOGIN_PATH);

  const activeGoats = counts.ok ? tenantLifecycleCount(counts.data.items, "alive") : null;
  const reviewRows = importRun?.ok ? importRun.data.import_run.summary.rows_needing_review : null;
  const openConflicts = reviewSummary.ok ? reviewSummary.data.open_conflicts : null;
  const openCandidates = reviewSummary.ok ? reviewSummary.data.open_candidates : null;
  const dueCount = vaccDue.ok ? vaccDue.data.items.length : null;
  const verifyCount = vaccVerify.ok ? vaccVerify.data.items.length : null;
  const importHref = selectedImportRunId
    ? `/import-review?import_run_id=${encodeURIComponent(selectedImportRunId)}`
    : "/import-review";

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>Control Tower</h1>
          <div className="sub">
            <b>Watch</b> — live herd KPIs &amp; operational queues · All parks · as of {todayLabel()}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/herd" className="btn">
          Herd Search
        </Link>
        <Link href="/vaccination" className="btn p">
          Action Center →
        </Link>
      </div>

      {/* Live identity KPIs */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi
          label="Live goats"
          value={activeGoats === null ? "n/a" : fmtInt(activeGoats)}
          sub={activeGoats === null ? "not tracked" : "tenant_lifecycle · alive"}
          tone={activeGoats === null ? "warn" : "ok"}
          href="/herd"
          icon={<Activity className="ic" />}
        />
        <Kpi
          label="Open conflicts"
          value={openConflicts === null ? "n/a" : fmtInt(openConflicts)}
          sub="data quality"
          tone={openConflicts && openConflicts > 0 ? "warn" : "mut"}
          href="/data-quality?state=open"
          icon={<AlertTriangle className="ic" />}
        />
        <Kpi
          label="Match candidates"
          value={openCandidates === null ? "n/a" : fmtInt(openCandidates)}
          sub="actionable · data quality"
          tone="info"
          href="/data-quality"
          icon={<FileSearch className="ic" />}
        />
        <Kpi
          label="Import review"
          value={reviewRows === null ? "no run" : fmtInt(reviewRows)}
          sub={selectedImportRunId ? `run ${shortId(selectedImportRunId)}` : "no import runs"}
          tone={reviewRows && reviewRows > 0 ? "warn" : "mut"}
          href={importHref}
          icon={<DatabaseZap className="ic" />}
        />
      </div>

      {/* Herd queue + PHC Vaccination */}
      <div className="grid g2" style={{ marginBottom: 14 }}>
        <section className="card">
          <div className="hd">
            <Search className="ic" style={{ color: "var(--brand)" }} />
            <h3>Herd passport queue</h3>
            <div className="sp" />
            <Link href="/herd" className="btn gh sm">
              Open Herd Search →
            </Link>
          </div>
          {!herd.ok ? (
            <div className="bd">
              <div className="alert">
                <b>{herd.error.code}</b>&nbsp;{herd.error.message}
              </div>
            </div>
          ) : herd.data.items.length === 0 ? (
            <div className="bd">
              <p className="muted small">No goats returned on the first page.</p>
            </div>
          ) : (
            <div className="bd feed">
              {herd.data.items.map((goat) => (
                <Link key={goat.goat_id} href={`/goats/${goat.goat_id}`} className="fitem" style={{ cursor: "pointer" }}>
                  <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                    <Search className="ic" />
                  </span>
                  <div className="tx">
                    <b>{goat.display_id}</b>
                    <div className="mt">
                      {dash(goat.location_path.display)} · RFID {dash(goat.rfid)} · {dash(goat.breed)}
                    </div>
                  </div>
                  <span className="tm">
                    <Tag tone="mut">{goat.identity_state}</Tag>
                  </span>
                </Link>
              ))}
            </div>
          )}
        </section>

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
              <Link href="/vaccination/config" className="lk">
                Config — Schedule Builder
              </Link>{" "}
              — due work, the verification queue, and{" "}
              <Link href="/vaccination/adherence" className="lk">
                adherence
              </Link>{" "}
              then populate here.
            </div>
          </div>
        </section>
      </div>

      {/* Data quality + Import review */}
      <div className="grid g2">
        <section className="card">
          <div className="hd">
            <FileSearch className="ic" style={{ color: "var(--brand)" }} />
            <h3>Data quality</h3>
            <div className="sp" />
            <Link href="/data-quality?state=open" className="btn gh sm">
              Open Data Quality →
            </Link>
          </div>
          {!conflicts.ok ? (
            <div className="bd">
              <div className="alert">
                <b>{conflicts.error.code}</b>&nbsp;{conflicts.error.message}
              </div>
            </div>
          ) : conflicts.data.items.length === 0 ? (
            <div className="bd">
              <p className="muted small">No open conflicts returned.</p>
            </div>
          ) : (
            <div className="bd feed">
              {conflicts.data.items.map((conflict) => (
                <div key={conflict.conflict_id} className="fitem">
                  <span className="fic" style={{ background: "var(--warnx)", color: "var(--warn)" }}>
                    <AlertTriangle className="ic" />
                  </span>
                  <div className="tx">
                    <b>{conflict.conflict_type}</b>
                    <div className="mt">
                      {conflict.goat_count} goats · {conflict.source_record_count} sources
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="card">
          <div className="hd">
            <DatabaseZap className="ic" style={{ color: "var(--brand)" }} />
            <h3>Import review</h3>
            <div className="sp" />
            {selectedImportRunId ? (
              <Link href={importHref} className="btn gh sm">
                Open run →
              </Link>
            ) : null}
          </div>
          <div className="bd">
            {recentRuns && !recentRuns.ok ? (
              <div className="alert">
                <b>{recentRuns.error.code}</b>&nbsp;{recentRuns.error.message}
              </div>
            ) : !selectedImportRunId ? (
              <p className="muted small">No import runs found. Run the local RFID import / reconcile flow first.</p>
            ) : importRun && !importRun.ok ? (
              <div className="alert">
                <b>{importRun.error.code}</b>&nbsp;{importRun.error.message}
              </div>
            ) : importRun?.ok ? (
              <>
                <div className="metagrid" style={{ gridTemplateColumns: "1fr 1fr 1fr" }}>
                  <div>
                    <div className="k">Created</div>
                    <div className="v">{fmtInt(importRun.data.import_run.summary.goats_created)}</div>
                  </div>
                  <div>
                    <div className="k">Review</div>
                    <div className="v">{fmtInt(importRun.data.import_run.summary.rows_needing_review)}</div>
                  </div>
                  <div>
                    <div className="k">Errors</div>
                    <div className="v">{fmtInt(importRun.data.import_run.summary.error_count)}</div>
                  </div>
                </div>
                <div className="note" style={{ marginTop: 12 }}>
                  <Tag tone="info">{importRun.data.import_run.status}</Tag> · {importRun.data.import_run.source_dataset}
                </div>
              </>
            ) : null}
          </div>
        </section>
      </div>
    </div>
  );
}

function tenantLifecycleCount(
  items: Array<{ count_value: number; dimensions: { lifecycle_status?: string | null } }>,
  lifecycle: string,
): number | null {
  const row = items.find((item) => item.dimensions.lifecycle_status === lifecycle);
  return row?.count_value ?? null;
}
