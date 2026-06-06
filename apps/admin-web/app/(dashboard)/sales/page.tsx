"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChartCard } from "@/components/charts/chart-card";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { VerticalBarChart } from "@/components/charts/vertical-bar";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatNumber } from "@/lib/constants";
import { getChartDescription } from "@/lib/display-utils";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";

type SalesRow = {
  month: string;
  saleCount: number;
  salesAmount: number;
};

type Farm = "Total" | "CBE" | "CPT";

const FARM_PARAM: Record<Farm, string> = {
  Total: "TOTAL",
  CBE: "CBE",
  CPT: "CPT",
};

type FeedVsSalesRow = {
  month: string;
  monthLabel: string;
  farm: string;
  feedSpend: number;
  salesRevenue: number;
};

type SalesResponse = {
  data: SalesRow[];
  feedVsSales: FeedVsSalesRow[];
};

function formatINRCompactWhole(value: number): string {
  const sign = value < 0 ? "-" : "";
  const abs = Math.abs(value);
  if (abs >= 1_00_000) return `${sign}\u20B9${Math.round(abs / 1_00_000)}L`;
  if (abs >= 1_000) return `${sign}\u20B9${Math.round(abs / 1_000)}K`;
  return `${sign}\u20B9${Math.round(abs)}`;
}

export default function SalesPage() {
  const isMobile = useIsMobile();
  const [farm, setFarm] = useState<Farm>("Total");

  const { data: response, isLoading, error, refetch } = useQuery<SalesResponse>({
    queryKey: ["sales", farm],
    queryFn: async () => {
      const res = await fetch(`/api/sales?farm=${FARM_PARAM[farm]}`);
      if (!res.ok) throw new Error("Failed to fetch sales data");
      const json = await res.json();
      return {
        data: json.data ?? [],
        feedVsSales: json.feedVsSales ?? [],
      };
    },
    staleTime: 15 * 60 * 1000,
  });

  const data = response?.data ?? [];
  const feedVsSales = response?.feedVsSales ?? [];

  const countData = data.map((r) => ({
    name: r.month,
    value: r.saleCount,
  }));

  const amountData = data.map((r) => ({
    name: r.month,
    value: r.salesAmount,
  }));

  const feedVsSalesData = feedVsSales.map((r) => ({
    name: r.monthLabel,
    "Sales Revenue": r.salesRevenue,
    "Feed Expenditure": r.feedSpend,
  }));

  return (
    <div className="space-y-6">
      {isLoading && <LoadingState />}
      {error && <ErrorState message="Failed to load sales data" onRetry={() => refetch()} />}

      {!isLoading && !error && (
        <div className="space-y-4">
          <div role="group" aria-label="Farm filter" className="flex flex-wrap items-center gap-1">
            {(["Total", "CBE", "CPT"] as Farm[]).map((f) => (
              <button
                key={f}
                aria-pressed={farm === f}
                onClick={() => setFarm(f)}
                className={
                  farm === f
                    ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                    : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
                }
              >
                {f}
              </button>
            ))}
          </div>

          <ChartCard
            title="Month-wise Sale Count"
            subtitle={getChartDescription("Month-wise Sale Count")}
          >
            <VerticalBarChart
              data={countData}
              valueLabel="Animals sold"
              valueFormatter={(v) => formatNumber(Math.round(v))}
              showLabels={!isMobile}
              compact={false}
              tiltXLabels={countData.length > 8}
            />
          </ChartCard>

          <ChartCard
            title="Month-wise Sales Amount"
            subtitle={getChartDescription("Month-wise Sales Amount")}
          >
            <VerticalBarChart
              data={amountData}
              valueLabel="Sales amount"
              valueFormatter={formatINRCompactWhole}
              yAxisFormatter={formatINRCompactWhole}
              showLabels={!isMobile}
              compact={false}
              tiltXLabels={amountData.length > 8}
            />
          </ChartCard>

          <ChartCard
            title="Sales vs Feed Expenditure"
            subtitle={getChartDescription("Sales vs Feed Expenditure")}
          >
            <GroupedBarChart
              data={feedVsSalesData}
              keys={["Sales Revenue", "Feed Expenditure"]}
              height={isMobile ? 320 : 380}
              showDataLabels={!isMobile}
              tiltXLabels={feedVsSalesData.length > 8}
              valueFormatter={formatINRCompactWhole}
              labelFormatter={(v) => formatINRCompactWhole(Number(v))}
              tooltipFormatter={(value) => formatINRCompactWhole(Number(value))}
            />
          </ChartCard>
        </div>
      )}
    </div>
  );
}
