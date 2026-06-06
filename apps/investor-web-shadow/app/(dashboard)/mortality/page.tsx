'use client';

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { ChartCard } from "@/components/charts/chart-card";
import { VerticalBarChart } from "@/components/charts/vertical-bar";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { PieChart } from "@/components/charts/pie-chart";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { getChartDescription } from "@/lib/display-utils";

const secondaryTabs = [
  { key: "breedwise", label: "Breed-wise" },
  { key: "delivery", label: "By Delivery" },
  { key: "farmwise", label: "Overall Farm-wise" },
] as const;

type SecondaryTab = (typeof secondaryTabs)[number]["key"];

type BreedToggle = "by-breed" | "within-breed";

const pctLabel = (v: number) => `${v}%`;

export default function MortalityPage() {
  const [secondaryTab, setSecondaryTab] = useState<SecondaryTab>("breedwise");
  const [breedToggle, setBreedToggle] = useState<BreedToggle>("by-breed");

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["mortality", "this-month"],
    queryFn: async () => {
      const res = await fetch(`/api/mortality?period=this-month`);
      if (!res.ok) throw new Error("Failed to fetch mortality data");
      return res.json();
    },
    staleTime: 15 * 60 * 1000,
  });

  const breedByBreed = data?.breedwise ?? [];
  const breedWithin = data?.breedwiseWithin ?? [];
  const breedwise = breedToggle === "by-breed" ? breedWithin : breedByBreed;
  const farmwise = data?.farmwise ?? [];
  const delivery = data?.delivery ?? {};

  const breedToggleDesc = breedToggle === "by-breed"
    ? "Out of all deaths across all breeds, what share belongs to each breed"
    : "Out of all animals within a breed, what percentage died";

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load mortality data" onRetry={() => refetch()} />;

  return (
    <div className="space-y-4">
      {/* Secondary tabs */}
      <div className="flex items-center gap-1">
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
              showDataLabels
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
            />
          </ChartCard>
        </div>
      )}

      {/* ── Farm-wise (overall only) ── */}
      {secondaryTab === "farmwise" && (
        <div className="space-y-4">
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
              showDataLabels
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
              showDataLabels
            />
          </ChartCard>
        </div>
      )}

    </div>
  );
}
