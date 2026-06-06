/* eslint-disable @typescript-eslint/no-explicit-any */
'use client';

import { useQuery } from "@tanstack/react-query";
import { Baby, Users } from "lucide-react";
import { KPICard } from "@/components/charts/kpi-card";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { VerticalBarChart } from "@/components/charts/vertical-bar";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatNumber } from "@/lib/constants";
import { getChartDescription } from "@/lib/display-utils";

export default function BirthsPage() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["births"],
    queryFn: () => fetch("/api/births").then((r) => r.json()).then((j) => j.data),
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load births data" onRetry={() => refetch()} />;

  const kpi = data?.kpi ?? { totalBirths: 0, avgKidsPerMother: 0 };
  const efficiency = data?.kiddingEfficiency ?? [];
  const birthCounts = data?.birthCountLast10Days ?? [];
  const frequency = data?.kiddingFrequency ?? [];
  const litter = data?.litterSize ?? [];

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <KPICard
          label="Total Births"
          value={formatNumber(kpi.totalBirths)}
          icon={<Baby size={18} />}
          delay={0}
        />
        <KPICard
          label="Avg Kids per Mother"
          value={kpi.avgKidsPerMother.toFixed(2)}
          icon={<Users size={18} />}
          delay={1}
        />
      </div>

      <ChartCard
        title="Kidding Efficiency by Breed"
        subtitle={getChartDescription("Kidding Efficiency by Breed")}
      >
        <HorizontalBarChart
          data={efficiency.map((d: { breed: string; pct: number }) => ({
            name: d.breed,
            value: d.pct,
          }))}
          valueLabel="Efficiency (%)"
        />
      </ChartCard>

      <ChartCard
        title="Birth Count – Last 10 Days"
        subtitle={getChartDescription("Birth Count – Last 10 Days")}
      >
      <VerticalBarChart
        data={[...birthCounts]
          .sort((a: { date: string }, b: { date: string }) => {
            const dateA = typeof a.date === "object" ? (a.date as any).value : a.date;
            const dateB = typeof b.date === "object" ? (b.date as any).value : b.date;
            return dateA.localeCompare(dateB);
          })
          .map((d: { date: string; count: number }) => {
            const raw = typeof d.date === "object" ? (d.date as any).value : d.date;
            const parts = raw.split("-");
            return {
              name: `${parts[2]}-${parts[1]}-${parts[0]}`,
              value: d.count,
            };
          })}
        valueLabel="Births"
      />
      </ChartCard>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <ChartCard
          title="Kidding Frequency by Breed"
          subtitle={getChartDescription("Kidding Frequency by Breed")}
        >
          <HorizontalBarChart
            data={frequency.map((d: { breed: string; months: number }) => ({
              name: d.breed,
              value: d.months,
            }))}
            valueLabel="Months"
          />
        </ChartCard>

        <ChartCard
          title="Litter Size by Breed"
          subtitle={getChartDescription("Litter Size by Breed")}
        >
          <HorizontalBarChart
            data={litter.map((d: { breed: string; size: number }) => ({
              name: d.breed,
              value: d.size,
            }))}
            valueLabel="Kids per Mother"
          />
        </ChartCard>
      </div>
    </div>
  );
}
