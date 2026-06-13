import { Activity, IndianRupee, Scale, Tag } from "lucide-react";
import Link from "next/link";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { KPICard } from "@/components/charts/kpi-card";
import { AuthRequiredPanel, EmptyPanel, ErrorPanel, NextPageLink, Panel, StatPill, ValueList } from "@/components/admin-primitives";
import { dateTime, dash, shortId } from "@/lib/format";
import { boundedInt, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { firstAuthRequiredError, getIdentityCounts, type IdentityCountsResponse } from "@/lib/api/server";

const tabs = [
  { id: "overall", label: "Overall", live: true },
  { id: "core-farms", label: "Core Farms", live: false },
  { id: "cbe", label: "CBE", live: false },
  { id: "cpt", label: "CPT", live: false },
  { id: "holdings", label: "Holdings", live: false },
] as const;

export async function IdentityCountsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const activeTab = normalizeTab(one(searchParams, "view"));
  const page = boundedInt(one(searchParams, "page"), 1, 1, 1000000);
  const [tenantLifecycle, growthCohort] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 50 }),
    getIdentityCounts({ grain: "growth_cohort", limit: 50 }),
  ]);
  const authError = firstAuthRequiredError(tenantLifecycle, growthCohort);
  if (authError) {
    return <AuthRequiredPanel error={authError} />;
  }

  const tenantItems = tenantLifecycle.ok ? tenantLifecycle.data.items : [];
  const activeGoats = dimensionCount(tenantItems, "lifecycle_status", "alive");
  const growthRows = growthCohort.ok ? growthCohort.data.items : [];
  const statusRows = growthRows.length > 0 ? growthRows : tenantItems;
  const statusError = !growthCohort.ok ? growthCohort.error : !tenantLifecycle.ok ? tenantLifecycle.error : null;
  const statusUpdatedAt = growthCohort.ok && growthRows.length > 0
    ? growthCohort.data.freshness.as_of_recorded_at
    : tenantLifecycle.ok
      ? tenantLifecycle.data.freshness.as_of_recorded_at
      : null;

  return (
    <>
      <nav className="mb-5 flex flex-wrap items-center gap-5 text-xl font-semibold" aria-label="Counts tabs">
        {tabs.map((tab) => (
          <Link
            key={tab.id}
            href={tab.id === "overall" ? "/counts" : `/counts?view=${tab.id}`}
            scroll={false}
            aria-current={activeTab === tab.id ? "page" : undefined}
            className={
              activeTab === tab.id
                ? "rounded-xl bg-[#22262E] px-4 py-2 text-[#14F1D9]"
                : "px-2 py-2 text-[#8899AA] hover:text-[#c7d1dc]"
            }
          >
            {tab.label}
          </Link>
        ))}
      </nav>

      {activeTab !== "overall" ? (
        <Panel
          title={`${tabs.find((tab) => tab.id === activeTab)?.label} counts`}
          description="This legacy tab is visible for navigation parity, but the corresponding farm/holding rollup is not exposed by the Mesha counters yet."
        >
          <EmptyPanel message="Not tracked yet. Overall identity counts are live; farm, CBE, CPT, and holding rollups need their own backend projections before they can show numbers." />
        </Panel>
      ) : null}

      {activeTab === "overall" ? <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KPICard
          label="Total active goats"
          value={activeGoats === null ? "Not tracked" : activeGoats.toLocaleString("en-IN")}
          subtitle="tenant_lifecycle alive"
          icon={<Activity size={18} />}
          delay={0}
          variant={activeGoats === null ? "amber" : "positive"}
        />
        <KPICard
          label="Farm value"
          value="Not tracked"
          subtitle="valuation rollup not exposed yet"
          icon={<IndianRupee size={18} />}
          delay={1}
        />
        <KPICard
          label="Total weight"
          value="Not tracked"
          subtitle="weight rollup not exposed yet"
          icon={<Scale size={18} />}
          delay={2}
        />
        <KPICard
          label="Breeds tracked"
          value="Not tracked"
          subtitle="breed diversity not exposed yet"
          icon={<Tag size={18} />}
          delay={3}
        />
      </div> : null}

      {activeTab === "overall" ? <div className="mt-6">
        <ChartCard
          title="Count by Status"
          subtitle={
            statusUpdatedAt
              ? `Number of active goats grouped by their current status buckets · Updated ${dateTime(statusUpdatedAt)}`
              : "Number of active goats grouped by their current status buckets"
          }
          className="min-h-[713px]"
        >
          {statusRows.length > 0 ? (
            <HorizontalBarChart
              data={chartRows(statusRows)}
              valueLabel="Count"
              yAxisWidth={150}
            />
          ) : statusError ? (
            <ErrorPanel error={statusError} />
          ) : (
            <EmptyPanel message="No counter rows returned for the overall view." />
          )}
        </ChartCard>
      </div> : null}

      {activeTab === "overall" ? <div className="mt-6">
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
              <NextPageLink
                href={hrefWithCursor("/counts", searchParams, tenantLifecycle.data.next_cursor)}
                currentPage={page}
                pageSize={50}
              />
              <div className="text-xs text-[#8899AA]">Trace {tenantLifecycle.data.trace_id}. Has more: {tenantLifecycle.data.has_more ? "yes" : "no"}.</div>
            </div>
          )}
        </Panel>
      </div> : null}
    </>
  );
}

function normalizeTab(value: string | undefined): typeof tabs[number]["id"] {
  const match = tabs.find((tab) => tab.id === value);
  return match?.id ?? "overall";
}

function dimensionCount(
  items: IdentityCountsResponse["items"],
  dimension: "lifecycle_status" | "growth_cohort_tag",
  value: string,
): number | null {
  const row = items.find((item) => item.dimensions[dimension] === value);
  return row?.count_value ?? null;
}

function chartRows(items: IdentityCountsResponse["items"]) {
  return items.map((item) => ({
    name:
      normalizeStatusLabel(item.dimensions.growth_cohort_tag) ??
      normalizeStatusLabel(item.dimensions.lifecycle_status) ??
      "Unspecified",
    value: item.count_value,
  }));
}

function normalizeStatusLabel(value?: string | null): string | null {
  if (!value) return null;
  if (value === "-") return "Unspecified";
  return value;
}
