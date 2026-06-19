import Link from "next/link";
import { randomUUID } from "node:crypto";
import { redirect } from "next/navigation";
import { Activity, AlertTriangle, Database, Layers, RotateCcw } from "lucide-react";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { KPICard } from "@/components/charts/kpi-card";
import { ActionNotice, EmptyPanel, ErrorPanel, FormField, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { formatLabel } from "@/lib/display-utils";
import { dash, dateTime } from "@/lib/format";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getCountsDashboard, type CountsDashboardResponse, type CountsView } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { createCountsSyncRunAction } from "./actions";

const views: Array<{ id: CountsView; label: string }> = [
  { id: "overall", label: "Overall" },
  { id: "core-farms", label: "Core Farms" },
  { id: "cbe", label: "CBE" },
  { id: "cpt", label: "CPT" },
  { id: "holdings", label: "Holdings" },
];

type CountsRow = CountsDashboardResponse["summary"][number];

export async function CountsDashboardPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const view = normalizeView(one(searchParams, "view"));
  const snapshotDate = normalizeSnapshotDate(one(searchParams, "snapshot_date"));
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const dashboard = await getCountsDashboard({ view, snapshot_date: snapshotDate });
  const authError = firstAuthRequiredError(dashboard);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  return (
    <>
      <PageHeader
        eyebrow="Counts"
        title="Counts Dashboard"
        description="Legacy count sections served from the governed dashboard projection."
      />
      <ActionNotice status={actionStatus} message={actionMessage} />

      <nav className="mb-3 flex flex-wrap items-center gap-2" aria-label="Counts views">
        {views.map((item) => (
          <Link
            key={item.id}
            href={countsHref(item.id, snapshotDate)}
            scroll={false}
            aria-current={view === item.id ? "page" : undefined}
            className={
              view === item.id
                ? "inline-flex h-10 items-center justify-center rounded-lg border border-[#14F1D9]/60 bg-[#22262E] px-3 text-sm font-semibold text-[#14F1D9] outline-none focus-visible:ring-2 focus-visible:ring-[#14F1D9]/50"
                : "inline-flex h-10 items-center justify-center rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#B0BEC5] outline-none hover:border-[#14F1D9]/50 hover:text-[#14F1D9] focus-visible:ring-2 focus-visible:ring-[#14F1D9]/50"
            }
          >
            {item.label}
          </Link>
        ))}
      </nav>

      <form method="get" className="mb-5 flex flex-wrap items-end gap-2" aria-label="Snapshot date">
        {view !== "overall" ? <input type="hidden" name="view" value={view} /> : null}
        <label className="flex flex-col gap-1">
          <span className="text-xs uppercase text-[#93a4b8]">Snapshot date</span>
          <input
            type="date"
            name="snapshot_date"
            defaultValue={snapshotDate ?? ""}
            className="h-10 rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
          />
        </label>
        <button
          type="submit"
          className="inline-flex h-10 items-center rounded-md border border-[#14F1D9]/60 px-3 text-sm font-semibold text-[#14F1D9] hover:bg-[#22262E]"
        >
          View date
        </button>
        {snapshotDate ? (
          <Link href={countsHref(view, undefined)} scroll={false} className="inline-flex h-10 items-center rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#B0BEC5] hover:border-[#14F1D9]/50 hover:text-[#14F1D9]">
            Latest
          </Link>
        ) : null}
      </form>

      <div className="mb-5">
        <CountsSyncPanel snapshotDate={dashboard.ok ? dashboard.data.snapshot_date : null} />
      </div>

      {!dashboard.ok ? <ErrorPanel error={dashboard.error} /> : <CountsBody data={dashboard.data} />}
    </>
  );
}

function CountsSyncPanel({ snapshotDate }: { snapshotDate: string | null }) {
  return (
    <Panel title="Sync Controls" description="Manual backend sync reads existing source rows, resolves locations, and publishes projection rows atomically.">
      <form action={createCountsSyncRunAction} className="grid gap-3 lg:grid-cols-[150px_180px_minmax(0,1fr)_auto]">
        <input type="hidden" name="return_to" value="/counts" />
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        <label>
          <span className="text-xs uppercase text-[#93a4b8]">Mode</span>
          <select
            name="mode"
            defaultValue="dry_run"
            className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
          >
            <option value="dry_run">Preview only</option>
            <option value="execute">Run and publish</option>
          </select>
        </label>
        <FormField name="snapshot_date" label="Snapshot date" type="date" defaultValue={snapshotDate ?? undefined} />
        <div className="rounded-md border border-[#334155] bg-[#10141b] px-3 py-2 text-xs leading-5 text-[#93a4b8]">
          Preview only checks source rows and location resolution without writing. Run and publish writes sync results, review rows, and dashboard projections; missing source rows stay unavailable instead of writing zeros.
        </div>
        <div className="flex items-end">
          <ConfirmSubmitButton
            message="Start Counts sync run?"
            className="inline-flex h-10 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#5ff7e8]"
          >
            <RotateCcw className="h-4 w-4" aria-hidden="true" />
            Start
          </ConfirmSubmitButton>
        </div>
      </form>
    </Panel>
  );
}

function CountsBody({ data }: { data: CountsDashboardResponse }) {
  const sectionEntries = Object.entries(data.sections ?? {});
  const total = firstMetric(data.summary, ["total_active_goats", "total", "goat_count", "count", "active_goats"]);
  const sectionRows = sectionEntries.reduce((sum, [, rows]) => sum + rows.length, 0);

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KPICard
          label="Total"
          value={total === null ? "No rows" : formatNumber(total)}
          subtitle={dash(data.snapshot_date)}
          icon={<Activity size={18} />}
          variant={total === null ? "amber" : "positive"}
        />
        <KPICard
          label="Freshness"
          value={formatLabel(data.freshness.freshness_status)}
          subtitle={dateTime(data.freshness.as_of)}
          icon={<Database size={18} />}
          variant={data.freshness.stale ? "amber" : "positive"}
        />
        <KPICard
          label="Rows"
          value={sectionRows.toLocaleString("en-IN")}
          subtitle={`${sectionEntries.length.toLocaleString("en-IN")} sections`}
          icon={<Layers size={18} />}
          variant={sectionRows === 0 ? "amber" : "default"}
        />
        <KPICard
          label="Conflicts"
          value={data.freshness.conflict_count.toLocaleString("en-IN")}
          subtitle={`version ${data.freshness.projection_version.toLocaleString("en-IN")}`}
          icon={<AlertTriangle size={18} />}
          variant={data.freshness.conflict_count > 0 ? "amber" : "default"}
        />
      </div>

      <div className="mt-5">
        <FreshnessPanel freshness={data.freshness} sourceDate={data.summary_source_date} />
      </div>

      {data.summary.length > 0 ? (
        <div className="mt-6">
          <Panel title="Summary">
            <RowsTable rows={data.summary} />
          </Panel>
        </div>
      ) : null}

      <div className="mt-6 grid gap-5 xl:grid-cols-2">
        {sectionEntries.map(([section, rows]) => (
          <SectionChart key={section} title={formatLabel(section)} rows={rows} />
        ))}
      </div>
    </>
  );
}

function FreshnessPanel({
  freshness,
  sourceDate,
}: {
  freshness: CountsDashboardResponse["freshness"];
  sourceDate: string | null;
}) {
  return (
    <Panel title="Freshness" description={freshness.unavailable_sources.length > 0 ? freshness.unavailable_sources.join(", ") : undefined}>
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatPill label="Serving" value={formatLabel(freshness.serving_state)} tone={freshness.stale ? "warn" : "good"} />
        <StatPill label="Source" value={formatLabel(freshness.source_composition)} />
        <StatPill label="Watermark" value={dateTime(freshness.source_watermark)} />
        <StatPill label="Source date" value={dash(sourceDate)} />
      </div>
    </Panel>
  );
}

function SectionChart({ title, rows }: { title: string; rows: CountsRow[] }) {
  const chartRows = rows
    .map((row) => ({ name: rowLabel(row), value: rowValue(row) }))
    .filter((row): row is { name: string; value: number } => row.value !== null)
    .slice(0, 14);

  return (
    <ChartCard title={title} subtitle={rows.length === 0 ? "No rows" : `${rows.length.toLocaleString("en-IN")} rows`}>
      {chartRows.length > 0 ? (
        <HorizontalBarChart
          data={chartRows}
          height={Math.max(200, chartRows.length * 38 + 64)}
          valueLabel={formatLabel(rows[0]?.unit ?? "count")}
          yAxisWidth={148}
        />
      ) : (
        <EmptyPanel message="No rows returned." />
      )}
      {rows.length > 0 ? (
        <div className="mt-4">
          <RowsTable rows={rows.slice(0, 8)} compact />
        </div>
      ) : null}
    </ChartCard>
  );
}

function RowsTable({ rows, compact = false }: { rows: CountsRow[]; compact?: boolean }) {
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full text-left text-sm">
        <thead className="text-xs uppercase text-[#8899AA]">
          <tr>
            <th className="whitespace-nowrap px-3 py-2 font-semibold">Label</th>
            <th className="whitespace-nowrap px-3 py-2 font-semibold">Metric</th>
            <th className="whitespace-nowrap px-3 py-2 text-right font-semibold">Value</th>
            {!compact ? <th className="whitespace-nowrap px-3 py-2 font-semibold">Source</th> : null}
          </tr>
        </thead>
        <tbody className="divide-y divide-[#334155] text-[#E0E8F0]">
          {rows.map((row, index) => (
            <tr key={`${row.section}-${row.grain}-${row.dimension_key}-${row.metric_key}-${index}`}>
              <td className="px-3 py-2">{rowLabel(row)}</td>
              <td className="px-3 py-2 text-[#B0BEC5]">{formatLabel(row.metric_key)}</td>
              <td className="px-3 py-2 text-right font-semibold text-white">{formatRowValue(row)}</td>
              {!compact ? <td className="px-3 py-2 text-[#8899AA]">{formatLabel(row.source_composition)}</td> : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function normalizeView(raw: string | undefined): CountsView {
  if (raw === "core-farms" || raw === "cbe" || raw === "cpt" || raw === "holdings") {
    return raw;
  }
  return "overall";
}

function normalizeSnapshotDate(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  return /^\d{4}-\d{2}-\d{2}$/.test(raw.trim()) ? raw.trim() : undefined;
}

function countsHref(view: CountsView, snapshotDate: string | undefined): string {
  const params = new URLSearchParams();
  if (view !== "overall") params.set("view", view);
  if (snapshotDate) params.set("snapshot_date", snapshotDate);
  const qs = params.toString();
  return qs ? `/counts?${qs}` : "/counts";
}

function firstMetric(rows: CountsRow[], keys: string[]): number | null {
  for (const key of keys) {
    const row = rows.find((item) => item.metric_key === key || item.dimension_key === key);
    const value = row ? rowValue(row) : null;
    if (value !== null) return value;
  }
  return rows.length > 0 ? rowValue(rows[0]) : null;
}

function rowLabel(row: CountsRow): string {
  const base = row.dimension_label || row.dimension_key;
  const secondary = row.secondary_dimension_label || row.secondary_dimension_key;
  return secondary ? `${base} / ${secondary}` : base;
}

function rowValue(row: CountsRow): number | null {
  if (typeof row.count_value === "number") return row.count_value;
  if (typeof row.numeric_value === "number") return row.numeric_value;
  return null;
}

function formatRowValue(row: CountsRow): string {
  const value = rowValue(row);
  if (value === null) return "—";
  const formatted = formatNumber(value);
  return row.unit && row.unit !== "count" ? `${formatted} ${row.unit}` : formatted;
}

function formatNumber(value: number): string {
  return Number.isInteger(value)
    ? value.toLocaleString("en-IN")
    : value.toLocaleString("en-IN", { maximumFractionDigits: 2 });
}
