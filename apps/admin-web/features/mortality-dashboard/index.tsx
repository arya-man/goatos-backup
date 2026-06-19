import Link from "next/link";
import { randomUUID } from "node:crypto";
import { redirect } from "next/navigation";
import { Activity, AlertTriangle, Database, HeartPulse, Percent, RotateCcw } from "lucide-react";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { KPICard } from "@/components/charts/kpi-card";
import { ActionNotice, EmptyPanel, ErrorPanel, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { formatLabel } from "@/lib/display-utils";
import { dateTime } from "@/lib/format";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getMortalityDashboard,
  type MortalityDashboardResponse,
  type MortalityPeriod,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { createMortalitySyncRunAction } from "./actions";

const periods: Array<{ id: MortalityPeriod; label: string }> = [
  { id: "overall", label: "Overall" },
  { id: "this-month", label: "This Month" },
  { id: "month-wise", label: "Month Wise" },
];

type MortalityRow = MortalityDashboardResponse["summary"][number];

export async function MortalityDashboardPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const period = normalizePeriod(one(searchParams, "period"));
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const dashboard = await getMortalityDashboard({ period });
  const authError = firstAuthRequiredError(dashboard);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  return (
    <>
      <PageHeader
        eyebrow="Mortality"
        title="Mortality Dashboard"
        description="Mortality sections with numerator and denominator provenance."
      />
      <ActionNotice status={actionStatus} message={actionMessage} />

      <nav className="mb-5 flex flex-wrap items-center gap-2" aria-label="Mortality periods">
        {periods.map((item) => (
          <Link
            key={item.id}
            href={item.id === "overall" ? "/dashboard/mortality" : `/dashboard/mortality?period=${item.id}`}
            scroll={false}
            aria-current={period === item.id ? "page" : undefined}
            className={
              period === item.id
                ? "inline-flex h-10 items-center justify-center rounded-lg border border-[#14F1D9]/60 bg-[#22262E] px-3 text-sm font-semibold text-[#14F1D9] outline-none focus-visible:ring-2 focus-visible:ring-[#14F1D9]/50"
                : "inline-flex h-10 items-center justify-center rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#B0BEC5] outline-none hover:border-[#14F1D9]/50 hover:text-[#14F1D9] focus-visible:ring-2 focus-visible:ring-[#14F1D9]/50"
            }
          >
            {item.label}
          </Link>
        ))}
      </nav>

      <div className="mb-5">
        <MortalitySyncPanel />
      </div>

      {!dashboard.ok ? <ErrorPanel error={dashboard.error} /> : <MortalityBody data={dashboard.data} />}
    </>
  );
}

function MortalitySyncPanel() {
  return (
    <Panel title="Sync Controls" description="Manual backend sync normalizes event rows, applies logical dedup, and rebuilds mortality projections.">
      <form action={createMortalitySyncRunAction} className="grid grid-cols-1 gap-3 lg:grid-cols-[150px_minmax(0,1fr)_auto]">
        <input type="hidden" name="return_to" value="/dashboard/mortality" />
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        <label className="min-w-0">
          <span className="text-xs uppercase text-[#93a4b8]">Mode</span>
          <select
            name="mode"
            defaultValue="dry_run"
            className="mt-1 h-10 w-full min-w-0 rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
          >
            <option value="dry_run">Preview only</option>
            <option value="execute">Run and publish</option>
          </select>
        </label>
        <div className="min-w-0 rounded-md border border-[#334155] bg-[#10141b] px-3 py-2 text-xs leading-5 text-[#93a4b8]">
          Preview only checks event rows and location resolution without writing. Run and publish applies dedup and review handling, then rebuilds mortality projections; missing source rows stay unavailable.
        </div>
        <div className="flex items-end">
          <ConfirmSubmitButton
            message="Start Mortality sync run?"
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

function MortalityBody({ data }: { data: MortalityDashboardResponse }) {
  const sectionEntries = Object.entries(data.sections ?? {});
  const headline = firstValue(data.summary);
  const deaths = firstMetric(data.summary, ["death_count", "deaths", "mortality_count"]);
  const denominator = firstDenominator(data.summary);
  const sectionRows = sectionEntries.reduce((sum, [, rows]) => sum + rows.length, 0);

  return (
    <>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KPICard
          label="Mortality"
          value={headline === null ? "No rows" : formatValue(headline, data.summary[0]?.unit)}
          subtitle={formatLabel(data.period)}
          icon={<Percent size={18} />}
          variant={headline === null ? "amber" : "default"}
        />
        <KPICard
          label="Deaths"
          value={deaths === null ? "No rows" : formatNumber(deaths)}
          subtitle={denominator === null ? "No denominator" : `${formatNumber(denominator)} denominator`}
          icon={<HeartPulse size={18} />}
          variant={deaths && deaths > 0 ? "amber" : "default"}
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
          icon={<Activity size={18} />}
          variant={sectionRows === 0 ? "amber" : "default"}
        />
      </div>

      <div className="mt-5">
        <Panel title="Freshness" description={data.freshness.unavailable_sources.length > 0 ? data.freshness.unavailable_sources.join(", ") : undefined}>
          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <StatPill label="Serving" value={formatLabel(data.freshness.serving_state)} tone={data.freshness.stale ? "warn" : "good"} />
            <StatPill label="Source" value={formatLabel(data.freshness.source_composition)} />
            <StatPill label="Watermark" value={dateTime(data.freshness.source_watermark)} />
            <StatPill label="Conflicts" value={data.freshness.conflict_count.toLocaleString("en-IN")} tone={data.freshness.conflict_count > 0 ? "warn" : "neutral"} />
          </div>
        </Panel>
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

function SectionChart({ title, rows }: { title: string; rows: MortalityRow[] }) {
  const chartRows = rows
    .map((row) => ({ name: row.dimension_label || row.dimension_key, value: row.value }))
    .filter((row) => Number.isFinite(row.value))
    .slice(0, 14);

  return (
    <ChartCard title={title} subtitle={rows.length === 0 ? "No rows" : `${rows.length.toLocaleString("en-IN")} rows`}>
      {chartRows.length > 0 ? (
        <HorizontalBarChart
          data={chartRows}
          height={Math.max(200, chartRows.length * 38 + 64)}
          valueLabel={formatLabel(rows[0]?.unit ?? "value")}
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

function RowsTable({ rows, compact = false }: { rows: MortalityRow[]; compact?: boolean }) {
  return (
    <div className="max-w-full min-w-0 overflow-x-auto">
      <table className="min-w-full text-left text-sm">
        <thead className="text-xs uppercase text-[#8899AA]">
          <tr>
            <th className="whitespace-nowrap px-3 py-2 font-semibold">Label</th>
            <th className="whitespace-nowrap px-3 py-2 text-right font-semibold">Value</th>
            <th className="whitespace-nowrap px-3 py-2 text-right font-semibold">Numerator</th>
            <th className="whitespace-nowrap px-3 py-2 text-right font-semibold">Denominator</th>
            {!compact ? <th className="whitespace-nowrap px-3 py-2 font-semibold">Source</th> : null}
          </tr>
        </thead>
        <tbody className="divide-y divide-[#334155] text-[#E0E8F0]">
          {rows.map((row, index) => (
            <tr key={`${row.section}-${row.grain}-${row.dimension_key}-${row.metric_key}-${index}`}>
              <td className="px-3 py-2">{row.dimension_label || row.dimension_key}</td>
              <td className="px-3 py-2 text-right font-semibold text-white">{formatValue(row.value, row.unit)}</td>
              <td className="px-3 py-2 text-right text-[#B0BEC5]">{row.numerator === null ? "—" : formatNumber(row.numerator)}</td>
              <td className="px-3 py-2 text-right text-[#B0BEC5]">{row.denominator === null ? "—" : formatNumber(row.denominator)}</td>
              {!compact ? (
                <td className="px-3 py-2 text-[#8899AA]">
                  {formatLabel(row.source_composition)}
                  {row.mixed_composition_exception_id ? (
                    <span className="ml-2 inline-flex items-center gap-1 text-[#fb923c]">
                      <AlertTriangle className="h-3.5 w-3.5" aria-hidden="true" />
                      exception
                    </span>
                  ) : null}
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function normalizePeriod(raw: string | undefined): MortalityPeriod {
  if (raw === "this-month" || raw === "month-wise") return raw;
  return "overall";
}

function firstValue(rows: MortalityRow[]): number | null {
  return rows.length > 0 && Number.isFinite(rows[0].value) ? rows[0].value : null;
}

function firstMetric(rows: MortalityRow[], keys: string[]): number | null {
  for (const key of keys) {
    const row = rows.find((item) => item.metric_key === key || item.dimension_key === key);
    if (!row) continue;
    if (typeof row.numerator === "number") return row.numerator;
    if (Number.isFinite(row.value)) return row.value;
  }
  return null;
}

function firstDenominator(rows: MortalityRow[]): number | null {
  const row = rows.find((item) => typeof item.denominator === "number");
  return row?.denominator ?? null;
}

function formatValue(value: number, unit: string | null | undefined): string {
  const formatted = formatNumber(value);
  return unit && unit !== "count" ? `${formatted} ${unit}` : formatted;
}

function formatNumber(value: number): string {
  return Number.isInteger(value)
    ? value.toLocaleString("en-IN")
    : value.toLocaleString("en-IN", { maximumFractionDigits: 2 });
}
