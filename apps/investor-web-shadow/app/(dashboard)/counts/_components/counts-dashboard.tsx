'use client';

import { useState, useMemo } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { Activity, IndianRupee, Weight, Tag, Info, X } from "lucide-react";
import { KPICard } from "@/components/charts/kpi-card";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { VerticalBarChart } from "@/components/charts/vertical-bar";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { PieChart } from "@/components/charts/pie-chart";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatINR, formatNumber, FARM_TABS, type FarmTabId } from "@/lib/constants";
import { displayStatus, getChartDescription } from "@/lib/display-utils";
import type { CountingRecord } from "@/lib/types";


interface CountsDashboardProps {
  tab: FarmTabId;
}

function farmParam(tab: FarmTabId): string | undefined {
  switch (tab) {
    case "cbe": return "CBE";
    case "cpt": return "CPT";
    case "core-farms": return "core";
    case "holdings": return "holdings";
    default: return undefined;
  }
}

export function CountsDashboard({ tab }: CountsDashboardProps) {
  const [showStatusTooltip, setShowStatusTooltip] = useState(false);

  const farm = farmParam(tab);
  const { data: response, isLoading, error, refetch } = useQuery({
    queryKey: ["counts", farm],
    queryFn: async () => {
      const params = new URLSearchParams();
      if (farm) params.set("farm", farm);
      const res = await fetch(`/api/counts${params.toString() ? `?${params}` : ""}`);
      if (!res.ok) throw new Error("Failed to fetch counts");
      return res.json();
    },
  });

  const records: CountingRecord[] = response?.data ?? [];
  const kpiSummary = response?.kpiSummary as { totalActive: number; farmValue: number; totalWeight: number } | null;
  const ageBreakdownApi = response?.ageBreakdown as { animal_type: string; total_count: number }[] | null;
  const adultsGenderApi = response?.adultsGender as { fattening_gender: string; total_count: number }[] | null;
  const kidsGenderApi = response?.kidsGender as { fattening_gender: string; total_count: number }[] | null;
  const fatteningGenderApi = response?.fatteningGender as { fattening_gender: string; total_count: number }[] | null;

  // Compute aggregations from records
  const kpi = useMemo(() => {
    if (!records.length && !kpiSummary) return { totalActive: 0, farmValue: 0, totalWeight: 0, avgWeightPerGoat: 0, breedsTracked: 0 };

    const breeds = new Set(records.map(r => r.breed).filter(Boolean));

    // Use BigQuery daily_summary_dev for the 3 main KPIs when available
    if (kpiSummary) {
      const { totalActive, farmValue, totalWeight } = kpiSummary;
      return {
        totalActive,
        farmValue,
        totalWeight,
        avgWeightPerGoat: totalActive > 0 ? totalWeight / totalActive : 0,
        breedsTracked: breeds.size || 7,
      };
    }

    // Fallback: compute from counting records
    const totalActive = records.reduce((s, r) => s + (r.goat_count || 0), 0);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const totalWeight = records.reduce((s, r) => s + ((r as any).total_weight as number || 0), 0) || totalActive * 32;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const farmValue = records.reduce((s, r) => s + ((r as any).value as number || 0), 0) || totalActive * 18160;
    return {
      totalActive,
      farmValue,
      totalWeight,
      avgWeightPerGoat: totalActive > 0 ? totalWeight / totalActive : 0,
      breedsTracked: breeds.size || 7,
    };
  }, [records, kpiSummary]);

  const breedCounts = useMemo(() => {
    const map = new Map<string, number>();
    for (const r of records) {
      if (r.breed) map.set(r.breed, (map.get(r.breed) ?? 0) + (r.goat_count || 0));
    }
    return Array.from(map, ([breed, count]) => ({ breed, count })).sort((a, b) => b.count - a.count);
  }, [records]);

  const statusCounts = useMemo(() => {
    const map = new Map<string, number>();
    for (const r of records) {
      const tag = r.shed_tag || "Unknown";
      map.set(tag, (map.get(tag) ?? 0) + (r.goat_count || 0));
    }
    return Array.from(map, ([status, count]) => ({ status, count })).sort((a, b) => b.count - a.count);
  }, [records]);

  const farmDistribution = useMemo(() => {
    const map = new Map<string, number>();
    for (const r of records) {
      if (r.farm) map.set(r.farm, (map.get(r.farm) ?? 0) + (r.goat_count || 0));
    }
    return Array.from(map, ([farm, count]) => ({ farm, count })).sort((a, b) => b.count - a.count);
  }, [records]);

  const ageBreakdown = useMemo(() => {
    // Use BigQuery counting_kpis_daily data when available
    if (ageBreakdownApi && ageBreakdownApi.length > 0) {
      const map = new Map<string, number>();
      for (const row of ageBreakdownApi) {
        map.set(row.animal_type?.toLowerCase(), row.total_count);
      }
      return {
        adults: map.get("adult") ?? map.get("adults") ?? 0,
        kids: map.get("kid") ?? map.get("kids") ?? 0,
      };
    }
    // Fallback: compute from counting records
    let adults = 0, kids = 0;
    for (const r of records) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const age = ((r as any).age as string)?.toLowerCase();
      if (age === "kid") kids += r.goat_count || 0;
      else if (age === "adult") adults += r.goat_count || 0;
    }
    return { adults, kids };
  }, [ageBreakdownApi, records]);

  const adultsGender = useMemo(() => {
    let male = 0, female = 0;
    for (const r of adultsGenderApi ?? []) {
      const g = (r.fattening_gender || "").toLowerCase();
      if (g === "male") male += Number(r.total_count || 0);
      else if (g === "female") female += Number(r.total_count || 0);
    }
    return { male, female };
  }, [adultsGenderApi]);

  const kidsGender = useMemo(() => {
    let male = 0, female = 0;
    for (const r of kidsGenderApi ?? []) {
      const g = (r.fattening_gender || "").toLowerCase();
      if (g === "male") male += Number(r.total_count || 0);
      else if (g === "female") female += Number(r.total_count || 0);
    }
    return { male, female };
  }, [kidsGenderApi]);

  const fatteningGender = useMemo(() => {
    let male = 0, female = 0;
    for (const r of fatteningGenderApi ?? []) {
      const g = (r.fattening_gender || "").toLowerCase();
      if (g === "male") male += Number(r.total_count || 0);
      else if (g === "female") female += Number(r.total_count || 0);
    }
    return { male, female };
  }, [fatteningGenderApi]);

  const isSingleFarm = tab === "cbe" || tab === "cpt" || tab === "holdings";
  const totalAnimals = kpi.totalActive;

  const pctLabel = (v: number) => {
    const pct = totalAnimals > 0 ? ((v / totalAnimals) * 100).toFixed(1) : "0.0";
    return `${formatNumber(v)} (${pct}%)`;
  };

  const statusData = statusCounts.map((s) => ({ name: displayStatus(s.status), value: s.count }));
  const breedData = breedCounts.map((b) => ({ name: b.breed, value: b.count }));
  const farmPieData = farmDistribution.map((f) => ({ name: f.farm, value: f.count }));
  const farmCountData = farmDistribution.map((f) => ({ name: f.farm, value: f.count }));
  const ageData = [
    { name: "Adult", value: ageBreakdown.adults },
    { name: "Kid", value: ageBreakdown.kids },
  ];
  const fatteningData = [
    { name: "Male", value: fatteningGender.male },
    { name: "Female", value: fatteningGender.female },
  ];
  const adultsGenderData = [
    { name: "Male", value: adultsGender.male },
    { name: "Female", value: adultsGender.female },
  ];
  const kidsGenderData = [
    { name: "Male", value: kidsGender.male },
    { name: "Female", value: kidsGender.female },
  ];
  const fatteningKidsGenderData = [
    { name: "Male", value: fatteningGender.male + kidsGender.male },
    { name: "Female", value: fatteningGender.female + kidsGender.female },
  ];

  const statusByBreedData = useMemo(() => {
    // Build actual (status, breed) → count from records
    const map = new Map<string, Map<string, number>>();
    for (const r of records) {
      const tag = r.shed_tag || "Unknown";
      const breed = r.breed;
      if (!breed) continue;
      if (!map.has(tag)) map.set(tag, new Map());
      const breedMap = map.get(tag)!;
      breedMap.set(breed, (breedMap.get(breed) ?? 0) + (r.goat_count || 0));
    }
    const topBreeds = breedCounts.slice(0, 5).map((b) => b.breed);
    return statusCounts.map((s) => {
      const row: { name: string; [key: string]: string | number } = { name: displayStatus(s.status) };
      const breedMap = map.get(s.status);
      for (const breed of topBreeds) {
        row[breed] = breedMap?.get(breed) ?? 0;
      }
      return row;
    });
  }, [records, statusCounts, breedCounts]);
  const groupedKeys = breedCounts.slice(0, 5).map((b) => b.breed);

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load counts data" onRetry={() => refetch()} />;

  return (
    <div className="space-y-6">
      {/* Tab Navigation */}
      <nav className="flex items-center gap-1">
        {FARM_TABS.map((t) => {
          const href = `/counts/${t.id}`;
          const isActive = t.id === tab;
          return (
            <Link
              key={t.id}
              href={href}
              className={
                isActive
                  ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                  : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
              }
            >
              {t.label}
            </Link>
          );
        })}
      </nav>

      {/* KPI Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <KPICard
          label="Total Active Goats"
          value={formatNumber(kpi.totalActive)}
          icon={<Activity size={18} />}
          delay={0}
        />
        <KPICard
          label="Farm Value"
          value={formatINR(kpi.farmValue)}
          icon={<IndianRupee size={18} />}
          delay={1}
        />
        <KPICard
          label="Total Weight"
          value={`${formatNumber(Math.round(kpi.totalWeight))} kg`}
          subtitle={`Avg ${kpi.avgWeightPerGoat.toFixed(1)} kg/goat`}
          icon={<Weight size={18} />}
          delay={2}
        />
        <KPICard
          label="Breeds Tracked"
          value={kpi.breedsTracked.toString()}
          icon={<Tag size={18} />}
          delay={3}
        />
      </div>

      {/* Row 1: Count by Status (full width) */}
      <div className="relative">
        <ChartCard
          title={
            <span className="flex items-center gap-2">
              Count by Status
              <button
                onClick={() => setShowStatusTooltip(!showStatusTooltip)}
                className="text-[#8899AA] hover:text-[#FFFFFF] transition-colors"
              >
                <Info size={14} />
              </button>
            </span>
          }
          subtitle={getChartDescription("Count by Status")}
        >
          <HorizontalBarChart data={statusData} labelFormatter={pctLabel} valueLabel="Count" />
        </ChartCard>
        {showStatusTooltip && (
          <div className="absolute top-12 left-40 z-50 w-64 rounded-xl border border-[#334155] bg-[#22262E] p-4 shadow-xl">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-medium text-[#FFFFFF]">Status Codes</span>
              <button onClick={() => setShowStatusTooltip(false)} className="text-[#8899AA] hover:text-[#FFFFFF]">
                <X size={14} />
              </button>
            </div>
            <div className="space-y-1">
              {["F0", "F1", "F2", "K0", "K1", "K2", "K3", "K4", "M0"].map((code) => (
                <div key={code} className="flex items-center gap-2 text-[11px]">
                  <span className="text-[#B0BEC5] font-medium w-6">{code}</span>
                  <span className="text-[#B0BEC5]">—</span>
                  <span className="text-[#B0BEC5]">{displayStatus(code)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Count by Breed – full width */}
      <ChartCard
        title="Count by Breed"
        subtitle={getChartDescription("Count by Breed")}
      >
        <HorizontalBarChart data={breedData} labelFormatter={pctLabel} valueLabel="Count" />
      </ChartCard>

      {/* Status by Breed – full width */}
      <ChartCard
        title="Status by Breed"
        subtitle={getChartDescription("Status by Breed")}
      >
        <GroupedBarChart data={statusByBreedData} keys={groupedKeys} tiltXLabels />
      </ChartCard>

      {/* Row 3: Count by Farm | Count Distribution by Farm */}
      {!isSingleFarm && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <ChartCard
            title="Count by Farm"
            subtitle={getChartDescription("Count by Farm")}
          >
            <HorizontalBarChart data={farmCountData} labelFormatter={pctLabel} valueLabel="Count" />
          </ChartCard>
          <ChartCard
            title="Count Distribution by Farm"
            subtitle={getChartDescription("Count Distribution by Farm")}
          >
            <PieChart data={farmPieData} showPercentInLegend />
          </ChartCard>
        </div>
      )}

      {/* Row 4: Count by Age (alone) */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <ChartCard
          title="Count by Age"
          subtitle={getChartDescription("Count by Age")}
        >
          <VerticalBarChart
            data={ageData}
            compact
            valueFormatter={pctLabel}
            valueLabel="Count"
          />
        </ChartCard>
      </div>

      {/* Row 5: Adults by Gender | Kids by Gender (Fattening + K0-K3 combined) */}
      {tab !== "holdings" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <ChartCard
            title="Adults by Gender"
            subtitle={getChartDescription("Adults by Gender")}
          >
            <VerticalBarChart
              data={adultsGenderData}
              compact
              valueFormatter={pctLabel}
              valueLabel="Count"
            />
          </ChartCard>
          <ChartCard
            title="Kids by Gender"
            subtitle={getChartDescription("Kids by Gender")}
          >
            <VerticalBarChart
              data={fatteningKidsGenderData}
              compact
              valueFormatter={pctLabel}
              valueLabel="Count"
            />
          </ChartCard>
        </div>
      )}

      {/* Row 6: K0 to K3 Kids by Gender | Fattening by Gender */}
      {tab !== "holdings" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <ChartCard
            title="K0 to K3 Kids by Gender"
            subtitle={getChartDescription("K0 to K3 Kids by Gender")}
          >
            <VerticalBarChart
              data={kidsGenderData}
              compact
              valueFormatter={pctLabel}
              valueLabel="Count"
            />
          </ChartCard>
          <ChartCard
            title="Fattening by Gender"
            subtitle={getChartDescription("Fattening by Gender")}
          >
            <VerticalBarChart
              data={fatteningData}
              compact
              valueFormatter={pctLabel}
              valueLabel="Count"
            />
          </ChartCard>
        </div>
      )}
    </div>
  );
}
