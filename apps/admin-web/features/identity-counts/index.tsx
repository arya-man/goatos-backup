import { Activity, MapPinned, Tag, Venus } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { KPICard } from "@/components/charts/kpi-card";
import { PieChart } from "@/components/charts/pie-chart";
import { EmptyPanel, ErrorPanel, Panel } from "@/components/admin-primitives";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { firstAuthRequiredError, getIdentityCounts, type IdentityCountsResponse } from "@/lib/api/server";

const tabs = [
  { id: "overall", label: "Overall" },
  { id: "core-farms", label: "Core Farms" },
  { id: "cbe", label: "CBE" },
  { id: "cpt", label: "CPT" },
  { id: "holdings", label: "Holdings" },
] as const;

type CountItem = IdentityCountsResponse["items"][number];
type CountsResult = Awaited<ReturnType<typeof getIdentityCounts>>;
type ChartRow = { name: string; value: number };

export async function IdentityCountsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const activeTab = normalizeTab(one(searchParams, "view"));
  const [tenantLifecycle, growthCohort, breedSexLifecycle, parkLifecycle] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 50 }),
    getIdentityCounts({ grain: "growth_cohort", limit: 50 }),
    getIdentityCounts({ grain: "breed_sex_lifecycle", lifecycle_status: "alive", limit: 500 }),
    getIdentityCounts({ grain: "park_lifecycle", lifecycle_status: "alive", limit: 100 }),
  ]);
  const authError = firstAuthRequiredError(tenantLifecycle, growthCohort, breedSexLifecycle, parkLifecycle);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  const tenantItems = tenantLifecycle.ok ? tenantLifecycle.data.items : [];
  const growthRows = growthCohort.ok ? growthCohort.data.items : [];
  const breedSexRows = breedSexLifecycle.ok ? breedSexLifecycle.data.items : [];
  const parkRows = parkLifecycle.ok ? parkLifecycle.data.items : [];

  const activeGoats = dimensionCount(tenantItems, "lifecycle_status", "alive");
  const breedRows = aggregateRows(breedSexRows, breedLabel);
  const genderRows = aggregateRows(breedSexRows, genderLabel);
  const locationRows = aggregateRows(parkRows, parkLabel);
  const locatedCount = locationRows.filter((row) => row.name !== "Unassigned").reduce((sum, row) => sum + row.value, 0);
  const breedCount = breedRows.filter((row) => row.name !== "Unspecified").length;
  const femaleCount = genderRows.find((row) => row.name === "Female")?.value ?? 0;
  const maleCount = genderRows.find((row) => row.name === "Male")?.value ?? 0;
  const cbeParkID = findParkID(parkRows, "CBE", "Coimbatore");
  const cptParkID = findParkID(parkRows, "CPT", "Channapatna");
  let shedLifecycle: CountsResult | null = null;
  if (activeTab === "cbe" && cbeParkID) {
    shedLifecycle = await getIdentityCounts({ grain: "shed_lifecycle", lifecycle_status: "alive", park_id: cbeParkID, limit: 500 });
  } else if (activeTab === "cpt" && cptParkID) {
    shedLifecycle = await getIdentityCounts({ grain: "shed_lifecycle", lifecycle_status: "alive", park_id: cptParkID, limit: 500 });
  }
  const shedAuthError = firstAuthRequiredError(shedLifecycle);
  if (shedAuthError) {
    redirect(INTERNAL_LOGIN_PATH);
  }
  const shedRows = shedLifecycle?.ok ? shedLifecycle.data.items : [];
  const shedError = shedLifecycle && !shedLifecycle.ok ? shedLifecycle.error : null;
  const statusUpdatedAt = firstFreshness(shedLifecycle, growthCohort, breedSexLifecycle, parkLifecycle, tenantLifecycle);

  return (
    <>
      <nav className="mb-5 flex flex-wrap items-center gap-3" aria-label="Counts tabs">
        {tabs.map((tab) => (
          <Link
            key={tab.id}
            href={tab.id === "overall" ? "/counts" : `/counts?view=${tab.id}`}
            scroll={false}
            aria-current={activeTab === tab.id ? "page" : undefined}
            className={
              activeTab === tab.id
                ? "inline-flex h-10 min-w-[5rem] items-center justify-center rounded-xl border border-transparent bg-[#22262E] px-4 text-lg font-semibold leading-none text-[#14F1D9] outline-none focus-visible:ring-2 focus-visible:ring-[#14F1D9]/50"
                : "inline-flex h-10 min-w-[5rem] items-center justify-center rounded-xl border border-transparent px-4 text-lg font-semibold leading-none text-[#8899AA] outline-none hover:text-[#c7d1dc] focus-visible:ring-2 focus-visible:ring-[#14F1D9]/50"
            }
          >
            {tab.label}
          </Link>
        ))}
      </nav>

      {activeTab === "overall" ? (
        <>
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
              label="Breeds tracked"
              value={breedCount === 0 ? "Not tracked" : breedCount.toLocaleString("en-IN")}
              subtitle="alive goats with breed evidence"
              icon={<Tag size={18} />}
              delay={1}
              variant={breedCount === 0 ? "amber" : "positive"}
            />
            <KPICard
              label="Female / male"
              value={`${femaleCount.toLocaleString("en-IN")} / ${maleCount.toLocaleString("en-IN")}`}
              subtitle="alive goats by sex"
              icon={<Venus size={18} />}
              delay={2}
              variant={femaleCount + maleCount === 0 ? "amber" : "positive"}
            />
            <KPICard
              label="Location mapped"
              value={locatedCount === 0 ? "Not tracked" : locatedCount.toLocaleString("en-IN")}
              subtitle="CBE/CPT park assignments"
              icon={<MapPinned size={18} />}
              delay={3}
              variant={locatedCount === 0 ? "amber" : "positive"}
            />
          </div>

          <div className="mt-6 grid gap-6 xl:grid-cols-2">
            <ChartCard title="Growth Cohort" subtitle={freshSubtitle("Live growth-cohort buckets", statusUpdatedAt)}>
              {growthRows.length > 0 ? (
                <HorizontalBarChart
                  data={growthChartRows(growthRows)}
                  height={Math.max(220, growthChartRows(growthRows).length * 46 + 50)}
                  valueLabel="Count"
                  yAxisWidth={132}
                />
              ) : growthCohort.ok ? (
                <EmptyPanel message="No growth-cohort counter rows returned." />
              ) : (
                <ErrorPanel error={growthCohort.error} />
              )}
            </ChartCard>

            <ChartCard title="Count by Breed" subtitle={freshSubtitle("Alive goats grouped by breed", statusUpdatedAt)}>
              {breedRows.length > 0 ? (
                <HorizontalBarChart
                  data={topRows(breedRows, 12)}
                  height={Math.max(260, topRows(breedRows, 12).length * 38 + 52)}
                  valueLabel="Count"
                  yAxisWidth={148}
                />
              ) : breedSexLifecycle.ok ? (
                <EmptyPanel message="No alive breed counter rows returned." />
              ) : (
                <ErrorPanel error={breedSexLifecycle.error} />
              )}
            </ChartCard>

            <ChartCard title="Count by Location" subtitle={freshSubtitle("CBE, CPT, and unassigned current-location buckets", statusUpdatedAt)}>
              {locationRows.length > 0 ? (
                <HorizontalBarChart data={locationRows} height={220} valueLabel="Count" yAxisWidth={132} />
              ) : parkLifecycle.ok ? (
                <EmptyPanel message="No live location counter rows returned." />
              ) : (
                <ErrorPanel error={parkLifecycle.error} />
              )}
            </ChartCard>

            <ChartCard title="Gender" subtitle={freshSubtitle("Alive goats grouped by sex", statusUpdatedAt)}>
              {genderRows.length > 0 ? (
                <HorizontalBarChart data={genderRows} height={190} valueLabel="Count" yAxisWidth={104} />
              ) : breedSexLifecycle.ok ? (
                <EmptyPanel message="No alive sex counter rows returned." />
              ) : (
                <ErrorPanel error={breedSexLifecycle.error} />
              )}
            </ChartCard>
          </div>
        </>
      ) : null}

      {activeTab === "core-farms" ? (
        <div className="grid gap-6 xl:grid-cols-2">
          <ChartCard title="Count by Location" subtitle={freshSubtitle("Alive goats grouped by CBE, CPT, and unassigned", statusUpdatedAt)}>
            {locationRows.length > 0 ? (
              <HorizontalBarChart data={locationRows} height={220} valueLabel="Count" yAxisWidth={132} />
            ) : parkLifecycle.ok ? (
              <EmptyPanel message="No live location counter rows returned." />
            ) : (
              <ErrorPanel error={parkLifecycle.error} />
            )}
          </ChartCard>
          <ChartCard title="Location Distribution" subtitle={freshSubtitle("Percentage share across current-location buckets", statusUpdatedAt)}>
            {locationRows.length > 0 ? (
              <PieChart data={locationRows} height={300} showPercentInLegend />
            ) : parkLifecycle.ok ? (
              <EmptyPanel message="No live location counter rows returned." />
            ) : (
              <ErrorPanel error={parkLifecycle.error} />
            )}
          </ChartCard>
        </div>
      ) : null}

      {activeTab === "cbe" ? (
        <LocationDetail title="CBE Counts" parkID={cbeParkID} rows={shedRows} statusUpdatedAt={statusUpdatedAt} error={shedError} />
      ) : null}

      {activeTab === "cpt" ? (
        <LocationDetail title="CPT Counts" parkID={cptParkID} rows={shedRows} statusUpdatedAt={statusUpdatedAt} error={shedError} />
      ) : null}

      {activeTab === "holdings" ? (
        <Panel
          title="Holdings / Unassigned"
          description="The old dashboard has a holding bucket. In the current Mesha identity counters, these are alive goats without a CBE/CPT park assignment."
        >
          <HorizontalBarChart data={locationRows.filter((row) => row.name === "Unassigned")} height={140} valueLabel="Count" yAxisWidth={132} />
        </Panel>
      ) : null}
    </>
  );
}

function LocationDetail({
  title,
  parkID,
  rows,
  statusUpdatedAt,
  error,
}: {
  title: string;
  parkID: string | null;
  rows: CountItem[];
  statusUpdatedAt: string | null;
  error: Parameters<typeof ErrorPanel>[0]["error"] | null;
}) {
  if (error) {
    return <ErrorPanel error={error} />;
  }
  if (!parkID) {
    return <EmptyPanel message="This location is not present in the live location counters." />;
  }
  const shedRows = topRows(aggregateRows(rows, shedLabel), 20);
  return (
    <ChartCard title={title} subtitle={freshSubtitle("Alive goats grouped by shed or park-only assignment", statusUpdatedAt)}>
      {shedRows.length > 0 ? (
        <HorizontalBarChart data={shedRows} height={Math.max(260, shedRows.length * 34 + 52)} valueLabel="Count" yAxisWidth={164} />
      ) : (
        <EmptyPanel message="No live shed counter rows returned for this location." />
      )}
    </ChartCard>
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

function growthChartRows(items: CountItem[]): ChartRow[] {
  return aggregateRows(items, (item) => normalizeStatusLabel(item.dimensions.growth_cohort_tag) ?? "Unspecified");
}

function aggregateRows(items: CountItem[], label: (item: CountItem) => string): ChartRow[] {
  const counts = new Map<string, number>();
  for (const item of items) {
    const name = label(item);
    counts.set(name, (counts.get(name) ?? 0) + item.count_value);
  }
  return Array.from(counts, ([name, value]) => ({ name, value }))
    .filter((row) => row.value > 0)
    .sort((a, b) => b.value - a.value || a.name.localeCompare(b.name));
}

function topRows(rows: ChartRow[], limit: number): ChartRow[] {
  if (rows.length <= limit) return rows;
  const head = rows.slice(0, limit - 1);
  const otherValue = rows.slice(limit - 1).reduce((sum, row) => sum + row.value, 0);
  return [...head, { name: "Other", value: otherValue }];
}

function breedLabel(item: CountItem): string {
  return item.dimensions.breed_name ?? (item.dimensions.breed_id ? shortId(item.dimensions.breed_id) : "Unspecified");
}

function genderLabel(item: CountItem): string {
  switch (item.dimensions.sex) {
    case "female":
      return "Female";
    case "male":
      return "Male";
    case "unknown":
      return "Unknown";
    default:
      return "Unspecified";
  }
}

function parkLabel(item: CountItem): string {
  if (!item.dimensions.park_id) return "Unassigned";
  return item.dimensions.park_name ?? shortId(item.dimensions.park_id);
}

function shedLabel(item: CountItem): string {
  if (item.dimensions.shed_id) {
    return item.dimensions.shed_name ?? shortId(item.dimensions.shed_id);
  }
  if (item.dimensions.park_id) {
    return `${item.dimensions.park_name ?? "Park"} only`;
  }
  return "Unassigned";
}

function findParkID(items: CountItem[], code: string, name: string): string | null {
  const match = items.find((item) => {
    const parkName = item.dimensions.park_name?.toLowerCase();
    return item.dimensions.park_id && (parkName === name.toLowerCase() || item.dimensions.park_name === code);
  });
  return match?.dimensions.park_id ?? null;
}

function firstFreshness(...results: Array<CountsResult | null | undefined>): string | null {
  for (const result of results) {
    if (result && result.ok && result.data.freshness.as_of_recorded_at) {
      return result.data.freshness.as_of_recorded_at;
    }
  }
  return null;
}

function freshSubtitle(label: string, updatedAt: string | null): string {
  return updatedAt ? `${label} · Updated ${dateTime(updatedAt)}` : label;
}

function normalizeStatusLabel(value?: string | null): string | null {
  if (!value) return null;
  if (value === "-") return "Unspecified";
  return value;
}
