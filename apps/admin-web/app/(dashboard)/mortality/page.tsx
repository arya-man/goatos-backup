'use client';

import { useState } from "react";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";
import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { VerticalBarChart } from "@/components/charts/vertical-bar";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { PieChart } from "@/components/charts/pie-chart";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { getChartDescription } from "@/lib/display-utils";

const topTabs = [
  { key: "overall", label: "Overall" },
  { key: "this-month", label: "This Month" },
] as const;

type TopTab = (typeof topTabs)[number]["key"];

const allSecondaryTabs = [
  { key: "breedwise", label: "Breed-wise" },
  { key: "farmwise", label: "Farm-wise" },
  { key: "loadwise", label: "Load-wise" },
  { key: "delivery", label: "By Delivery" },
  { key: "trends", label: "Trends" },
] as const;

type SecondaryTab = (typeof allSecondaryTabs)[number]["key"];

type BreedToggle = "by-breed" | "within-breed";
type FarmToggle = "by-farm" | "within-farm";


const pctLabel = (v: number) => `${v}%`;

export default function MortalityPage() {
  const isMobile = useIsMobile();
  const [topTab, setTopTab] = useState<TopTab>("overall");
  const [secondaryTab, setSecondaryTab] = useState<SecondaryTab>("breedwise");
  const [breedToggle, setBreedToggle] = useState<BreedToggle>("by-breed");
  const [farmToggle, setFarmToggle] = useState<FarmToggle>("by-farm");

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["mortality", topTab],
    queryFn: async () => {
      const res = await fetch(`/api/mortality?period=${topTab}`);
      if (!res.ok) throw new Error("Failed to fetch mortality data");
      return res.json();
    },
    staleTime: 15 * 60 * 1000,
  });

  const secondaryTabs = allSecondaryTabs.filter(
    (tab) => tab.key !== "loadwise" || topTab === "overall"
  ).filter(
    (tab) => !(tab.key === "trends" && topTab === "this-month")
  );

  if (secondaryTab === "loadwise" && topTab !== "overall") {
    setSecondaryTab("breedwise");
  }
  if (secondaryTab === "trends" && topTab === "this-month") {
    setSecondaryTab("breedwise");
  }

  const breedByBreed = data?.breedwise ?? [];
  const breedWithin = data?.breedwiseWithin ?? [];
  const breedwise = topTab === "overall"
    ? (breedToggle === "by-breed" ? breedByBreed : breedWithin)
    : (breedToggle === "by-breed" ? breedWithin : breedByBreed);
  const farmByFarm = data?.farmwise ?? [];
  const farmWithin = data?.farmwiseWithin ?? [];
  const farmwise = farmToggle === "by-farm" ? farmByFarm : farmWithin;
  const loadwise = data?.loadwise ?? [];
  const loadwiseFarmMortality = data?.loadwiseFarmMortality ?? [];
  const breedByVendor = data?.breedByVendor ?? [];
  const delivery = data?.delivery ?? {};
  const trends = data?.trends ?? {};
  const summary = data?.summary;

  const summaryItems = summary ? [
    { label: "Total Deaths", value: String(summary.totalDeaths ?? 0) },
    { label: "Kid Deaths", value: String(summary.kidDeaths ?? 0) },
    { label: "Adult Deaths", value: String(summary.adultDeaths ?? 0) },
    { label: "Total Mortality Rate", value: summary.mortalityRate ?? "–" },
    { label: "Kid Mortality Rate", value: summary.kidMortalityRate ?? "–" },
    { label: "Adult Mortality Rate", value: summary.adultMortalityRate ?? "–" },
  ] : [];

  const breedToggleDesc = breedToggle === "by-breed"
    ? "Out of all deaths across all breeds, what share belongs to each breed"
    : "Out of all animals within a breed, what percentage died";
  const farmToggleDesc = farmToggle === "by-farm"
    ? "Out of all deaths, what share belongs to each farm"
    : "Out of animals within that farm, what percentage died";

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load mortality data" onRetry={() => refetch()} />;

  return (
    <div className="space-y-4">
      {/* Top tabs */}
      <div className="flex flex-wrap items-center gap-1">
        {topTabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setTopTab(tab.key)}
            className={
              topTab === tab.key
                ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
            }
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Summary bar */}
      <div className="flex flex-wrap items-center gap-2">
        {summaryItems.map((item) => (
          <div
            key={item.label}
            className="bg-[#22262E] rounded-full px-3 py-1 text-xs flex items-center"
          >
            <span className="text-[#14F1D9]">{item.label}</span>
            <span className="text-[#FFFFFF] font-semibold ml-1.5">
              {item.value}
            </span>
          </div>
        ))}
      </div>

      {/* Secondary tabs */}
      <div className="flex flex-wrap items-center gap-1">
        {secondaryTabs.map((tab) => (
          <button
            key={tab.key}
            onClick={() => setSecondaryTab(tab.key)}
            className={
              secondaryTab === tab.key
                ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
            }
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* ── Breed-wise ── */}
      {secondaryTab === "breedwise" && (
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1">
              {(["by-breed", "within-breed"] as BreedToggle[]).map((t) => (
                <button
                  key={t}
                  onClick={() => setBreedToggle(t)}
                  className={
                    breedToggle === t
                      ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                      : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                  }
                >
                  {t === "by-breed" ? "By Breed" : "Within Breed"}
                </button>
              ))}
            </div>
            <div className="group relative">
              <Info size={14} className="text-[#8899AA] hover:text-[#14F1D9] cursor-help" />
              <div className="invisible group-hover:visible absolute left-6 top-0 z-50 w-72 rounded-lg border border-[#334155] bg-[#22262E] p-3 text-[11px] text-[#B0BEC5] shadow-xl">
                {breedToggleDesc}
              </div>
            </div>
          </div>

          <ChartCard
            title="Kids Mortality & Abortion by Breed"
            subtitle={getChartDescription("Kids Mortality & Abortion by Breed")}
          >
            <GroupedBarChart
              data={[...breedwise]
                .sort((a: { kidMortality: number }, b: { kidMortality: number }) => a.kidMortality - b.kidMortality)
                .map((d: { breed: string; kidMortality: number; kidAbortion: number }) => ({
                  name: d.breed,
                  "Mortality %": d.kidMortality,
                  "Abortion %": d.kidAbortion,
                }))}
              keys={["Mortality %", "Abortion %"]}
              showDataLabels={!isMobile}
              valueSuffix="%"
            />
          </ChartCard>

          <ChartCard
            title="Adults Mortality by Breed"
            subtitle={getChartDescription("Adults Mortality by Breed")}
          >
            <VerticalBarChart
              data={[...breedwise]
                .sort((a: { adultMortality: number }, b: { adultMortality: number }) => a.adultMortality - b.adultMortality)
                .map((d: { breed: string; adultMortality: number }) => ({
                  name: d.breed,
                  value: d.adultMortality,
                }))}
              valueFormatter={pctLabel}
              valueLabel="Mortality (%)"
              showLabels={!isMobile}
            />
          </ChartCard>

          <ChartCard
            title="Total Mortality by Breed"
            subtitle={getChartDescription("Total Mortality by Breed")}
          >
            <VerticalBarChart
              data={[...breedwise]
                .sort((a: { totalMortality: number }, b: { totalMortality: number }) => a.totalMortality - b.totalMortality)
                .map((d: { breed: string; totalMortality: number }) => ({
                  name: d.breed,
                  value: d.totalMortality,
                }))}
              valueFormatter={pctLabel}
              valueLabel="Mortality (%)"
              showLabels={!isMobile}
            />
          </ChartCard>
        </div>
      )}

      {/* ── Farm-wise ── */}
      {secondaryTab === "farmwise" && (
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-1">
              {(["by-farm", "within-farm"] as FarmToggle[]).map((t) => (
                <button
                  key={t}
                  onClick={() => setFarmToggle(t)}
                  className={
                    farmToggle === t
                      ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                      : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                  }
                >
                  {t === "by-farm" ? "By Farm" : "Within Farm"}
                </button>
              ))}
            </div>
            <div className="group relative">
              <Info size={14} className="text-[#8899AA] hover:text-[#14F1D9] cursor-help" />
              <div className="invisible group-hover:visible absolute left-6 top-0 z-50 w-72 rounded-lg border border-[#334155] bg-[#22262E] p-3 text-[11px] text-[#B0BEC5] shadow-xl">
                {farmToggleDesc}
              </div>
            </div>
          </div>

          <ChartCard
            title="Kids Mortality & Abortion by Farm"
            subtitle={getChartDescription("Kids Mortality & Abortion by Farm")}
          >
            <GroupedBarChart
              data={[...farmwise]
                .sort((a: { kidMortality: number }, b: { kidMortality: number }) => a.kidMortality - b.kidMortality)
                .map((d: { farm: string; kidMortality: number; kidAbortion: number }) => ({
                  name: d.farm,
                  "Mortality %": d.kidMortality,
                  "Abortion %": d.kidAbortion,
                }))}
              keys={["Mortality %", "Abortion %"]}
              showDataLabels={!isMobile}
              valueSuffix="%"
            />
          </ChartCard>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <ChartCard title="Adults Mortality by Farm" subtitle={getChartDescription("Adults Mortality by Farm")}>
              <PieChart
                data={farmwise.map(
                  (d: { farm: string; adultMortality: number }) => ({
                    name: d.farm,
                    value: d.adultMortality,
                  })
                )}
                showPercentInLegend
              />
            </ChartCard>

            <ChartCard title="Total Mortality by Farm" subtitle={getChartDescription("Total Mortality by Farm")}>
              <PieChart
                data={farmwise.map(
                  (d: { farm: string; totalMortality: number }) => ({
                    name: d.farm,
                    value: d.totalMortality,
                  })
                )}
                showPercentInLegend
              />
            </ChartCard>
          </div>
        </div>
      )}

      {/* ── Load-wise ── */}
      {secondaryTab === "loadwise" && (
        <div className="space-y-4">
          <ChartCard
            title="Load-wise Mortality %"
            subtitle={getChartDescription("Load-wise Mortality %")}
          >
            <HorizontalBarChart
              data={loadwise.map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: d.value,
                })
              )}
              labelFormatter={pctLabel}
              valueLabel="Mortality (%)"
            />
          </ChartCard>

          <ChartCard title="Breed-wise Mortality by Vendor" subtitle={getChartDescription("Breed-wise Mortality by Vendor")}>
            <GroupedBarChart
              data={(() => {
                const breedMap: Record<string, Record<string, number>> = {};
                const vendors = new Set<string>();
                breedByVendor.forEach(
                  (d: { breed: string; vendor: string; mortality: number }) => {
                    if (!breedMap[d.breed]) breedMap[d.breed] = {};
                    breedMap[d.breed][d.vendor] = (breedMap[d.breed][d.vendor] ?? 0) + d.mortality;
                    vendors.add(d.vendor);
                  }
                );
                return Object.entries(breedMap)
                  .map(([breed, values]) => ({
                    name: breed,
                    ...values,
                  }))
                  .sort((a, b) => {
                    const totalA = Object.values(a).reduce((s, v) => s + (typeof v === 'number' ? v : 0), 0);
                    const totalB = Object.values(b).reduce((s, v) => s + (typeof v === 'number' ? v : 0), 0);
                    return totalB - totalA;
                  });
              })()}
              keys={(() => {
                const vendors = new Set<string>();
                breedByVendor.forEach((d: { vendor: string }) => vendors.add(d.vendor));
                return Array.from(vendors);
              })()}
              stacked
              showDataLabels={!isMobile}
            />
          </ChartCard>

          <ChartCard title="Total Mortality by Farm" subtitle={getChartDescription("Total Mortality by Farm")}>
            <PieChart
              data={loadwiseFarmMortality.map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: d.value,
                })
              )}
              showPercentInLegend
            />
          </ChartCard>
        </div>
      )}

      {/* ── By Delivery ── */}
      {secondaryTab === "delivery" && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <ChartCard title="Mother Mortality by Breed" subtitle={getChartDescription("Mother Mortality by Breed")}>
            <VerticalBarChart
              data={(delivery.motherByBreed ?? []).map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: d.value,
                })
              )}
              valueFormatter={pctLabel}
              valueLabel="Mortality (%)"
              showLabels={!isMobile}
            />
          </ChartCard>

          <ChartCard title="Mother Mortality by Litter Size" subtitle={getChartDescription("Mother Mortality by Litter Size")}>
            <VerticalBarChart
              data={(delivery.motherByLitter ?? []).map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: d.value,
                })
              )}
              valueFormatter={pctLabel}
              valueLabel="Mortality (%)"
              showLabels={!isMobile}
            />
          </ChartCard>

          <ChartCard title="Kids Mortality by Litter Size" subtitle={getChartDescription("Kids Mortality by Litter Size")}>
            <VerticalBarChart
              data={(delivery.kidsByLitter ?? []).map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: d.value,
                })
              )}
              valueFormatter={pctLabel}
              valueLabel="Mortality (%)"
              showLabels={!isMobile}
            />
          </ChartCard>

          <ChartCard
            title="Kids Mortality Split"
            subtitle={getChartDescription("Kids Mortality Split")}
          >
            <GroupedBarChart
              data={(delivery.kidsSplit ?? []).map(
                (d: { name: string; death: number; abortion: number }) => ({
                  name: d.name,
                  Death: d.death,
                  Abortion: d.abortion,
                })
              )}
              keys={["Death", "Abortion"]}
              showDataLabels={!isMobile}
            />
          </ChartCard>
        </div>
      )}

      {/* ── Trends ── */}
      {secondaryTab === "trends" && (
        <div className="space-y-4">
          {/* Row 1 – full width */}
          <ChartCard title="Mortality by Status" subtitle={getChartDescription("Mortality by Status")}>
            <VerticalBarChart
              data={(trends.byStatus ?? []).map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: Math.round(d.value * 100) / 100,
                })
              )}
              valueFormatter={pctLabel}
              valueLabel="Mortality (%)"
              showLabels={!isMobile}
            />
          </ChartCard>

          {/* Row 2 – full width */}
          <ChartCard title="Mortality by Housing" subtitle={getChartDescription("Mortality by Housing")}>
            <HorizontalBarChart
              data={(trends.byHousing ?? []).map(
                (d: { name: string; value: number }) => ({
                  name: d.name,
                  value: Math.round(d.value * 100) / 100,
                })
              )}
              labelFormatter={pctLabel}
              valueLabel="Mortality (%)"
            />
          </ChartCard>

          <div className={`grid grid-cols-1 ${topTab === "overall" ? "md:grid-cols-2" : ""} gap-4`}>
            {topTab === "overall" && (
              <ChartCard
                title="Mortality by Season"
                subtitle={getChartDescription("Mortality by Season")}
              >
                <HorizontalBarChart
                  data={(trends.bySeason ?? []).map(
                    (d: { name: string; value: number }) => ({
                      name: d.name,
                      value: d.value,
                    })
                  )}
                  valueLabel="Deaths"
                />
              </ChartCard>
            )}

            <ChartCard title="Mortality by Gender" subtitle={getChartDescription("Mortality by Gender")}>
              <VerticalBarChart
                data={(trends.byGender ?? []).map(
                  (d: { name: string; value: number }) => ({
                    name: d.name,
                    value: d.value,
                  })
                )}
                compact
                valueFormatter={pctLabel}
                valueLabel="Mortality (%)"
              />
            </ChartCard>
          </div>
        </div>
      )}
    </div>
  );
}
