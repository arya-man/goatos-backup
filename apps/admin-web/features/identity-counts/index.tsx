import { Activity, IndianRupee, Scale, Tag } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { KPICard } from "@/components/charts/kpi-card";
import { EmptyPanel, ErrorPanel, Panel } from "@/components/admin-primitives";
import { dateTime } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
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
  const [tenantLifecycle, growthCohort] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 50 }),
    getIdentityCounts({ grain: "growth_cohort", limit: 50 }),
  ]);
  const authError = firstAuthRequiredError(tenantLifecycle, growthCohort);
  if (authError) {
    redirect("/login");
  }

  const tenantItems = tenantLifecycle.ok ? tenantLifecycle.data.items : [];
  const activeGoats = dimensionCount(tenantItems, "lifecycle_status", "alive");
  const growthRows = growthCohort.ok ? growthCohort.data.items : [];
  const statusRows = growthRows.length > 0 ? growthRows : tenantItems;
  const statusError = !growthCohort.ok ? growthCohort.error : !tenantLifecycle.ok ? tenantLifecycle.error : null;
  const chartUsesGrowthCohort = growthRows.length > 0;
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
          title={chartUsesGrowthCohort ? "Growth Cohort" : "Count by Lifecycle Status"}
          subtitle={
            statusUpdatedAt
              ? `${chartUsesGrowthCohort ? "Live growth-cohort buckets" : "Live lifecycle buckets"} · Updated ${dateTime(statusUpdatedAt)}`
              : chartUsesGrowthCohort
                ? "Live growth-cohort buckets"
                : "Live lifecycle buckets"
          }
        >
          {statusRows.length > 0 ? (
            <HorizontalBarChart
              data={chartRows(statusRows)}
              height={Math.max(168, chartRows(statusRows).length * 52 + 52)}
              valueLabel="Count"
              yAxisWidth={128}
            />
          ) : statusError ? (
            <ErrorPanel error={statusError} />
          ) : (
            <EmptyPanel message="No counter rows returned for the overall view." />
          )}
        </ChartCard>
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
