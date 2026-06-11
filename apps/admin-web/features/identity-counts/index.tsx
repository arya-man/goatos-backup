import { Activity, BarChart3, CircleDashed, Layers3 } from "lucide-react";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { KPICard } from "@/components/charts/kpi-card";
import { EmptyPanel, ErrorPanel, NextPageLink, PageHeader, Panel, StatPill, ValueList } from "@/components/admin-primitives";
import { dateTime, dash, shortId } from "@/lib/format";
import { hrefWithCursor, type RouteSearchParams } from "@/lib/search-params";
import { getIdentityCounts, type IdentityCountsResponse } from "@/lib/api/server";

const tabs = [
  { id: "overall", label: "Overall", live: true },
  { id: "core-farms", label: "Core Farms", live: false },
  { id: "cbe", label: "CBE", live: false },
  { id: "cpt", label: "CPT", live: false },
  { id: "holdings", label: "Holdings", live: false },
] as const;

export async function IdentityCountsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const [tenantLifecycle, healthStatus, growthCohort] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 50 }),
    getIdentityCounts({ grain: "health_status", limit: 50 }),
    getIdentityCounts({ grain: "growth_cohort", limit: 50 }),
  ]);

  const tenantItems = tenantLifecycle.ok ? tenantLifecycle.data.items : [];
  const activeGoats = dimensionCount(tenantItems, "lifecycle_status", "alive");
  const reviewGoats = dimensionCount(tenantItems, "lifecycle_status", "needs_review");
  const chartSource = healthStatus.ok && healthStatus.data.items.length > 0 ? healthStatus.data : tenantLifecycle.ok ? tenantLifecycle.data : null;
  const growthRows = growthCohort.ok ? growthCohort.data.items : [];

  return (
    <>
      <PageHeader
        eyebrow="Counts"
        title="Counts > Overall"
        description="Live identity counters for the overall herd. Farm-level legacy tabs stay disabled until those rollups are tracked."
      />

      <nav className="mb-5 flex flex-wrap items-center gap-1" aria-label="Counts tabs">
        {tabs.map((tab) => (
          <span
            key={tab.id}
            className={
              tab.live
                ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                : "rounded-lg border border-dashed border-[#334155] px-3 py-1.5 text-sm font-medium text-[#657386]"
            }
          >
            {tab.label}
            {!tab.live ? <span className="ml-2 text-[10px] uppercase tracking-wider">Not tracked</span> : null}
          </span>
        ))}
      </nav>

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KPICard
          label="Total active goats"
          value={activeGoats === null ? "Not tracked" : activeGoats.toLocaleString("en-IN")}
          subtitle="tenant_lifecycle alive"
          icon={<Activity size={18} />}
          delay={0}
          variant={activeGoats === null ? "amber" : "positive"}
        />
        <KPICard
          label="Needs review"
          value={reviewGoats === null ? "Not tracked" : reviewGoats.toLocaleString("en-IN")}
          subtitle="only if lifecycle bucket exists"
          icon={<CircleDashed size={18} />}
          delay={1}
          variant="amber"
        />
        <KPICard
          label="Core Farms"
          value="Not tracked"
          subtitle="farm rollup not exposed yet"
          icon={<Layers3 size={18} />}
          delay={2}
          variant="amber"
        />
        <KPICard
          label="Holdings"
          value="Not tracked"
          subtitle="holding rollup not exposed yet"
          icon={<BarChart3 size={18} />}
          delay={3}
          variant="amber"
        />
      </div>

      <div className="mt-6 grid gap-5 xl:grid-cols-[1.05fr_0.95fr]">
        <ChartCard
          title={chartSource?.grain === "health_status" ? "Count by Health Status" : "Count by Lifecycle Status"}
          subtitle={chartSource ? `Updated ${dateTime(chartSource.freshness.as_of_recorded_at)}` : "Live counter data unavailable"}
        >
          {chartSource ? (
            <HorizontalBarChart
              data={chartRows(chartSource)}
              valueLabel="Goats"
              yAxisWidth={150}
            />
          ) : tenantLifecycle.ok ? (
            <EmptyPanel message="No counter rows returned for the overall view." />
          ) : (
            <ErrorPanel error={tenantLifecycle.error} />
          )}
        </ChartCard>

        <Panel
          title="Growth Cohort"
          description="Live growth-cohort grain when available; missing buckets are not filled with zeroes."
          action={<StatPill label="Rows" value={growthRows.length} tone={growthRows.length > 0 ? "good" : "neutral"} />}
        >
          {!growthCohort.ok ? (
            <ErrorPanel error={growthCohort.error} />
          ) : growthRows.length === 0 ? (
            <EmptyPanel message="No growth-cohort counter rows returned." />
          ) : (
            <div className="space-y-2">
              {growthRows.slice(0, 8).map((item, index) => (
                <div key={`${item.counter_grain}-${index}-${item.updated_at}`} className="flex items-center justify-between gap-4 rounded-lg border border-[#334155] bg-[#11151C] p-3">
                  <div>
                    <div className="font-semibold text-white">{dash(item.dimensions.growth_cohort_tag)}</div>
                    <div className="mt-1 text-xs text-[#8899AA]">Updated {dateTime(item.updated_at)}</div>
                  </div>
                  <div className="text-xl font-bold text-[#14F1D9]">{item.count_value.toLocaleString("en-IN")}</div>
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-6">
        <Panel
          title="Overall Counter Rows"
          description="Projection rows for the live overall grain. Paging stays bounded."
          action={tenantLifecycle.ok ? <StatPill label="Rows" value={tenantLifecycle.data.items.length} tone="good" /> : undefined}
        >
          {!tenantLifecycle.ok ? (
            <ErrorPanel error={tenantLifecycle.error} />
          ) : tenantLifecycle.data.items.length === 0 ? (
            <EmptyPanel message="No lifecycle count rows returned." />
          ) : (
            <div className="space-y-3">
              {tenantLifecycle.data.items.map((item, index) => (
                <div key={`${item.counter_grain}-${index}-${item.updated_at}`} className="rounded-lg border border-[#334155] bg-[#11151C] p-4">
                  <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                    <div>
                      <div className="text-2xl font-semibold text-white">{item.count_value.toLocaleString("en-IN")}</div>
                      <div className="mt-1 text-sm text-[#8899AA]">Updated {dateTime(item.updated_at)}</div>
                    </div>
                    <div className="flex flex-wrap gap-2 text-xs">
                      {item.is_rebuilding ? <span className="rounded border border-[#a16207] px-2 py-1 text-[#facc15]">rebuilding</span> : null}
                      {item.source_import_run_id ? <span className="rounded border border-[#334155] px-2 py-1 text-[#c7d1dc]">run {shortId(item.source_import_run_id)}</span> : null}
                    </div>
                  </div>
                  <div className="mt-4">
                    <ValueList
                      values={[
                        ["lifecycle", dash(item.dimensions.lifecycle_status)],
                        ["tenant", item.dimensions.tenant_id],
                        ["source run", dash(item.source_import_run_id)],
                        ["rebuilding", item.is_rebuilding ? "yes" : "no"],
                      ]}
                    />
                  </div>
                </div>
              ))}
              <NextPageLink href={hrefWithCursor("/counts", searchParams, tenantLifecycle.data.next_cursor)} />
              <div className="text-xs text-[#8899AA]">Trace {tenantLifecycle.data.trace_id}. Has more: {tenantLifecycle.data.has_more ? "yes" : "no"}.</div>
            </div>
          )}
        </Panel>
      </div>
    </>
  );
}

function dimensionCount(
  items: IdentityCountsResponse["items"],
  dimension: "lifecycle_status" | "health_status" | "growth_cohort_tag",
  value: string,
): number | null {
  const row = items.find((item) => item.dimensions[dimension] === value);
  return row?.count_value ?? null;
}

function chartRows(data: IdentityCountsResponse) {
  return data.items.map((item) => ({
    name:
      item.dimensions.health_status ??
      item.dimensions.lifecycle_status ??
      item.dimensions.growth_cohort_tag ??
      "Unspecified",
    value: item.count_value,
  }));
}
