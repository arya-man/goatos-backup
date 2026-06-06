'use client';

import { useState } from "react";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";
import { useQuery } from "@tanstack/react-query";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { getChartDescription } from "@/lib/display-utils";

const toggleOptions = [
  { key: "goats", label: "Goats" },
  { key: "sheep", label: "Sheeps" },
] as const;

type AnimalType = (typeof toggleOptions)[number]["key"];

export default function PurchaseCostPage() {
  const isMobile = useIsMobile();
  const [type, setType] = useState<AnimalType>("goats");

  const { data: costs = [], isLoading, error, refetch } = useQuery({
    queryKey: ["purchase-cost", type],
    queryFn: async () => {
      const res = await fetch(`/api/purchase-cost?type=${type}`);
      if (!res.ok) throw new Error("Failed to fetch purchase costs");
      const json = await res.json();
      return json.data;
    },
    staleTime: 15 * 60 * 1000,
  });

  const chartData = costs.map((d: { label: string; costPerKg: number }) => ({
    name: d.label,
    value: d.costPerKg,
  }));

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-1">
        {toggleOptions.map((opt) => (
          <button
            key={opt.key}
            onClick={() => setType(opt.key)}
            className={
              type === opt.key
                ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
            }
          >
            {opt.label}
          </button>
        ))}
      </div>

      {isLoading && <LoadingState />}
      {error && <ErrorState message="Failed to load purchase cost data" onRetry={() => refetch()} />}

      {!isLoading && !error && (
        <ChartCard
          title={`Landing Cost per Kg – ${type === "goats" ? "Goats" : "Sheeps"}`}
          subtitle={getChartDescription(type === "goats" ? "Landing Cost per Kg – Goats" : "Landing Cost per Kg – Sheep")}
        >
          <HorizontalBarChart data={chartData} valueLabel="Cost per Kg (₹)" showLabels={!isMobile} />
        </ChartCard>
      )}
    </div>
  );
}
