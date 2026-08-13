import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getVerificationOversightAnalytics } from "@/lib/api/server";

/**
 * CEO/PC-Director-only aggregate analytics rendered ABOVE the /verify queue table (KPI strip,
 * pending backlog by module, per-verifier last-14-day activity + watch integrity). Server-computed
 * only: no client mega-fetch, no page-local recompute.
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "oversight_analytics", false)`
 * before rendering this component (see verification-review-page.tsx) -- the SAME capability
 * (permissions.VerificationOversee) gates both the page contract control and the backend endpoint
 * this component reads, so a verifier's build never even calls the endpoint. This component reads
 * the contract for copy only, never for its own gating decision -- it trusts the caller.
 */
export async function OversightAnalytics({ pageContract }: { pageContract: AdminUiPageContract }) {
  const result = await getVerificationOversightAnalytics();
  if (!result.ok) {
    return (
      <section className="card" style={{ marginBottom: 14 }}>
        <div className="bt">{copy(pageContract, "oversight_analytics.title")}</div>
        <div className="small muted" style={{ padding: "10px 4px" }}>
          {copy(pageContract, "oversight_analytics.unavailable")}
        </div>
      </section>
    );
  }
  const { kpis, pending_by_module: pendingByModule, verifier_activity: verifierActivity } = result.data;

  return (
    <section className="card" style={{ marginBottom: 14 }}>
      <div className="bt">{copy(pageContract, "oversight_analytics.title")}</div>

      <div className="vr-kpirow" style={{ display: "flex", flexWrap: "wrap", gap: 12, marginBottom: 14 }}>
        <KpiTile label={copy(pageContract, "oversight_analytics.videos_waiting")} value={String(kpis.videos_waiting)} />
        <KpiTile
          label={copy(pageContract, "oversight_analytics.oldest_pending")}
          value={formatHours(kpis.oldest_pending_age_hours)}
        />
        <KpiTile
          label={copy(pageContract, "oversight_analytics.review_speed")}
          value={`${kpis.verdicts_per_active_day_last_7d.toFixed(1)}/day`}
        />
        <KpiTile
          label={copy(pageContract, "oversight_analytics.est_days_to_clear")}
          value={kpis.est_days_to_clear_backlog != null ? kpis.est_days_to_clear_backlog.toFixed(1) : "—"}
        />
        <KpiTile
          label={copy(pageContract, "oversight_analytics.reject_rate")}
          value={kpis.reject_rate_last_30d != null ? formatRejectRate(kpis.reject_rate_last_30d) : "—"}
        />
      </div>

      {kpis.per_module_median_review_latency_hours.length ? (
        <div style={{ marginBottom: 14 }}>
          <div className="small muted" style={{ marginBottom: 6 }}>
            {copy(pageContract, "oversight_analytics.module_latency")}
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            {kpis.per_module_median_review_latency_hours.map((row) => (
              <Tag key={row.module} tone="info">
                {row.module} · {row.median_hours.toFixed(1)}h
              </Tag>
            ))}
          </div>
        </div>
      ) : null}

      {pendingByModule.length ? (
        <div style={{ marginBottom: 14 }}>
          <div className="small muted" style={{ marginBottom: 6 }}>
            {copy(pageContract, "oversight_analytics.pending_by_module")}
          </div>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            {pendingByModule.map((row) => (
              <Tag key={row.module} tone="info">
                {row.module}: {row.count}
              </Tag>
            ))}
          </div>
        </div>
      ) : null}

      {verifierActivity.length ? (
        <div style={{ overflowX: "auto" }}>
          <div className="small muted" style={{ marginBottom: 6 }}>
            {copy(pageContract, "oversight_analytics.verifier_activity")}
          </div>
          <table data-enh="1" className="vr-table">
            <thead>
              <tr>
                <th>{copy(pageContract, "oversight_analytics.col.verifier")}</th>
                <th>{copy(pageContract, "oversight_analytics.col.verdicts")}</th>
                <th>{copy(pageContract, "oversight_analytics.col.approved")}</th>
                <th>{copy(pageContract, "oversight_analytics.col.rejected")}</th>
                <th>{copy(pageContract, "oversight_analytics.col.busiest_day")}</th>
                <th>{copy(pageContract, "oversight_analytics.col.watch_integrity")}</th>
              </tr>
            </thead>
            <tbody>
              {verifierActivity.map((row) => (
                <tr key={row.verifier_id}>
                  <td>{verifierLabel(row)}</td>
                  <td>{row.verdicts}</td>
                  <td>{row.approved}</td>
                  <td>{row.rejected}</td>
                  <td className="muted">{row.busiest_day || "—"}</td>
                  <td className="muted small">
                    {row.items_tracked} {copy(pageContract, "oversight_analytics.tracked")} ·{" "}
                    {row.watched_to_end_count} {copy(pageContract, "oversight_analytics.watched_full")} ·{" "}
                    {row.verdict_without_play_count} {copy(pageContract, "oversight_analytics.no_play")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}

function KpiTile({ label, value }: { label: string; value: string }) {
  return (
    <div className="card" style={{ padding: "10px 14px", minWidth: 140 }}>
      <div className="small muted">{label}</div>
      <div style={{ fontSize: 20, fontWeight: 600 }}>{value}</div>
    </div>
  );
}

function formatHours(hours: number | undefined | null): string {
  if (hours == null) return "—";
  if (hours < 24) return `${hours.toFixed(1)}h`;
  return `${(hours / 24).toFixed(1)}d`;
}

// "1 in 8 rejected" reads CEO-plain (per the copy rule in the task brief); a bare percentage does
// not. Rounds to the nearest whole ratio; falls back to a percentage when the rate is too small
// for a clean "1 in N" phrase.
function formatRejectRate(rate: number): string {
  if (rate <= 0) return "0%";
  const oneInN = Math.round(1 / rate);
  if (oneInN >= 2 && oneInN <= 100) return `1 in ${oneInN} rejected`;
  return `${Math.round(rate * 100)}% rejected`;
}

// verifierLabel keeps a raw UUID out of a leadership-facing table. A verdict whose actor has no
// workforce record is not a reviewer at all -- on staging these are backfill/automation writes that
// stamp a batch of items in the same second -- and printing its id beside a real name reads as a
// second person reviewing videos. Name it for what it is and keep the id as a short trailing
// reference so it stays traceable.
function verifierLabel(row: { verifier_id: string; verifier_name?: string | null }): React.ReactNode {
  if (row.verifier_name?.trim()) return row.verifier_name;
  return (
    <span>
      Automated / unassigned account{" "}
      <span className="muted small mono">{row.verifier_id.slice(0, 8)}</span>
    </span>
  );
}
