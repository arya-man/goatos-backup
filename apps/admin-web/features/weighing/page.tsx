import Link from "@/components/no-prefetch-link";
import { AlertTriangle, CalendarDays, CheckCircle2, ClipboardList, Edit3, Eye, Play, Scale, Send, Video } from "lucide-react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate } from "@/lib/format";
import { getWeighingPageData, roleFromSearchParam, type WeighingCampaignState, type WeighingCategory, type WeighingScopeStatus } from "./data";

const statusTone: Record<WeighingCampaignState | WeighingScopeStatus | "no_task", Tone> = {
  no_task: "mut",
  draft: "mut",
  published: "info",
  in_progress: "info",
  delayed: "warn",
  completed: "ok",
  pending: "mut",
  needs_review: "warn",
};

const categoryTone: Record<WeighingCategory, Tone> = {
  individual_animal: "info",
  per_shed_partition: "pur",
};

function label(value: string): string {
  return value.replaceAll("_", " ");
}

function pct(done: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((done / total) * 100)));
}

function CapabilityButton({
  enabled,
  children,
  icon,
}: {
  enabled: boolean;
  children: React.ReactNode;
  icon: React.ReactNode;
}) {
  return (
    <button className={enabled ? "btn sm" : "btn sm ghost"} type="button" aria-disabled={!enabled}>
      {icon}
      {children}
    </button>
  );
}

export async function WeighingPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const role = roleFromSearchParam(one(searchParams ?? {}, "role"));
  const result = await getWeighingPageData(role);
  if (!result.ok) {
    return (
      <div className="screen on weighing-page">
        <section className="card pad">
          <h1>Weighing unavailable</h1>
          <p className="muted">{result.error.message}</p>
        </section>
      </div>
    );
  }

  const { campaign, weeks } = result.data;
  const individualPct = pct(campaign.individualCompleted, campaign.individualExpected);
  const shedPct = pct(campaign.shedPartitionCompleted, campaign.shedPartitionExpected);

  return (
    <div className="screen on weighing-page">
      <div className="phead">
        <div>
          <div className="crumb">Preventive Care (PC) / Weighing</div>
          <h1>Weighing</h1>
          <div className="sub">Weekly kids work, grouped by shed/partition and category.</div>
        </div>
        <div className="sp" />
        <div className="weighing-role-switch" aria-label="Preview persona">
          {(["leadership", "director", "operator"] as const).map((item) => (
            <Link key={item} href={`/weighing?role=${item}`} className={`chip${role === item ? " on" : ""}`} scroll={false}>
              {item === "leadership" ? "CEO/CXO" : item}
            </Link>
          ))}
        </div>
      </div>

      <section className="card weighing-hero">
        <div className="hd">
          <Scale className="ic" aria-hidden="true" />
          <h3>{campaign.weekLabel} campaign</h3>
          <div className="sp" />
          <Tag tone={statusTone[campaign.state]}>{label(campaign.state)}</Tag>
        </div>
        <div className="bd weighing-hero-grid">
          <div>
            <div className="weighing-week-row" aria-label="Week selector">
              {weeks.map((week) => (
                <Link key={week.key} href={`/weighing?role=${role}&week=${week.key}`} className={`weighing-week${week.key === campaign.weekStart ? " on" : ""}`} scroll={false}>
                  <span>{week.label}</span>
                  <Tag tone={statusTone[week.state]}>{label(week.state)}</Tag>
                </Link>
              ))}
            </div>
            <div className="metagrid weighing-meta">
              <div>
                <div className="k">Lane</div>
                <div className="v">{campaign.laneLabel}</div>
              </div>
              <div>
                <div className="k">Start business date</div>
                <div className="v">{fmtDate(campaign.startBusinessDate)}</div>
              </div>
              <div>
                <div className="k">Selected scopes</div>
                <div className="v">{campaign.selectedScopes} shed/partitions</div>
              </div>
              <div>
                <div className="k">Operator</div>
                <div className="v">{campaign.operatorName}</div>
              </div>
            </div>
          </div>
          <div className="weighing-command-panel">
            <div className="weighing-command-title">
              {campaign.reviewOnly ? <Eye className="ic" aria-hidden="true" /> : campaign.canExecute ? <Play className="ic" aria-hidden="true" /> : <Edit3 className="ic" aria-hidden="true" />}
              {campaign.reviewOnly ? "Monitor / review only" : campaign.canExecute ? "Operator execution view" : "Leadership planner"}
            </div>
            <p className="muted small">
              {campaign.reviewOnly
                ? "Director persona can review progress and evidence but cannot create, publish, edit, or execute."
                : campaign.canExecute
                  ? "Operator persona sees execution state but planning actions remain disabled."
                  : "CEO/CXO can create, edit, and publish weekly kids weighing tasks."}
            </p>
            <div className="weighing-actions">
              <CapabilityButton enabled={campaign.canCreate} icon={<Edit3 className="ic" aria-hidden="true" />}>Create/edit</CapabilityButton>
              <CapabilityButton enabled={campaign.canPublish} icon={<Send className="ic" aria-hidden="true" />}>Publish</CapabilityButton>
              <CapabilityButton enabled={campaign.canExecute} icon={<Play className="ic" aria-hidden="true" />}>Execute</CapabilityButton>
            </div>
          </div>
        </div>
      </section>

      <section className="weighing-metrics">
        <div className="card pad weighing-metric">
          <div className="k">Individual animal</div>
          <div className="v">{campaign.individualCompleted}<span>/{campaign.individualExpected}</span></div>
          <div className="weighing-bar"><i style={{ width: `${individualPct}%` }} /></div>
          <p className="muted small">RFID + animal identity + weight + mandatory per-animal video.</p>
        </div>
        <div className="card pad weighing-metric">
          <div className="k">Per shed/partition</div>
          <div className="v">{campaign.shedPartitionCompleted}<span>/{campaign.shedPartitionExpected}</span></div>
          <div className="weighing-bar pur"><i style={{ width: `${shedPct}%` }} /></div>
          <p className="muted small">Selected-scope result + scope proof; no individual weight update.</p>
        </div>
        <div className="card pad weighing-metric">
          <div className="k">Needs attention</div>
          <div className="v">{campaign.wrongShedScans + campaign.unavailableAnimals + campaign.proofPending}</div>
          <div className="weighing-attention">
            <Tag tone="warn">{campaign.wrongShedScans} wrong shed</Tag>
            <Tag tone="pur">{campaign.unavailableAnimals} unavailable</Tag>
            <Tag tone="info">{campaign.proofPending} proof pending</Tag>
          </div>
        </div>
      </section>

      <section className="card">
        <div className="hd">
          <ClipboardList className="ic" aria-hidden="true" />
          <h3>Shed / partition work groups</h3>
          <div className="sp" />
          <span className="muted small">Category-aware progress; grouping is preserved.</span>
        </div>
        <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
          <table className="weighing-table">
            <thead>
              <tr>
                <th>Park</th>
                <th>Shed / partition</th>
                <th>Category</th>
                <th>Progress</th>
                <th>Proof</th>
                <th>Wrong shed</th>
                <th>Planned</th>
                <th>Current date</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {campaign.scopes.map((row) => (
                <tr key={row.id}>
                  <td>{row.parkName}</td>
                  <td>
                    <b>{row.shedName}</b>
                    <span className="muted small blockish">{row.partitionName}</span>
                  </td>
                  <td><Tag tone={categoryTone[row.category]}>{row.category === "per_shed_partition" ? "lumpsum" : "individual"}</Tag></td>
                  <td>
                    <b>{row.completedCount}</b> / {row.expectedCount}
                    <span className="muted small blockish">{row.remainingCount} remaining · {row.unavailableCount} unavailable</span>
                  </td>
                  <td><Tag tone={row.proofPendingCount > 0 ? "warn" : "ok"}><Video className="ic" aria-hidden="true" />{row.proofPendingCount > 0 ? `${row.proofPendingCount} pending` : "linked"}</Tag></td>
                  <td>{row.wrongShedCount > 0 ? <Tag tone="warn">{row.wrongShedCount} visible</Tag> : <span className="muted">-</span>}</td>
                  <td>{fmtDate(row.plannedDate)}</td>
                  <td>{fmtDate(row.effectiveDate)}</td>
                  <td><Tag tone={statusTone[row.status]}>{label(row.status)}</Tag></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <div className="weighing-review-grid">
        <section className="card">
          <div className="hd">
            <AlertTriangle className="ic" aria-hidden="true" />
            <h3>Wrong-shed scans</h3>
            <div className="sp" />
            <Tag tone="warn">expected vs actual</Tag>
          </div>
          <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
            <table className="weighing-table compact">
              <thead>
                <tr>
                  <th>Animal</th>
                  <th>Expected / original</th>
                  <th>Actual / current</th>
                  <th>Scan</th>
                </tr>
              </thead>
              <tbody>
                {campaign.wrongShedRows.map((row) => (
                  <tr key={row.id}>
                    <td><b>{row.animalDisplayId}</b><span className="muted small blockish">{row.rfid}</span></td>
                    <td>{row.expectedShed}<span className="muted small blockish">{row.originalPartition}</span></td>
                    <td>{row.actualShed}<span className="muted small blockish">{row.currentPartition}</span></td>
                    <td>{fmtDate(row.scannedAt)}<span className="muted small blockish">{row.operatorName}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>

        <section className="card">
          <div className="hd">
            <CheckCircle2 className="ic" aria-hidden="true" />
            <h3>Missing / unavailable</h3>
            <div className="sp" />
            <Tag tone="pur">current herd truth</Tag>
          </div>
          <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
            <table className="weighing-table compact">
              <thead>
                <tr>
                  <th>Animal</th>
                  <th>Expected</th>
                  <th>Classification</th>
                  <th>Checked</th>
                </tr>
              </thead>
              <tbody>
                {campaign.missingRows.map((row) => (
                  <tr key={row.id}>
                    <td><b>{row.animalDisplayId}</b></td>
                    <td>{row.expectedShed}</td>
                    <td><Tag tone="pur">{label(row.classification)}</Tag><span className="muted small blockish">{row.currentTruth}</span></td>
                    <td>{fmtDate(row.checkedAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <section className="card weighing-integration">
        <div className="hd">
          <CalendarDays className="ic" aria-hidden="true" />
          <h3>Integration notes</h3>
        </div>
        <div className="bd">
          <p className="muted small">
            This admin-web slice is wired through a typed Weighing read-model adapter. The generated OpenAPI
            client has no Weighing endpoints in this checkout yet, so create/edit/publish buttons are gated UI
            controls only until backend commands land.
          </p>
        </div>
      </section>
    </div>
  );
}
