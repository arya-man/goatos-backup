'use client';

import { useQuery } from "@tanstack/react-query";
import { formatNumber } from "@/lib/constants";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { getChartDescription } from "@/lib/display-utils";

interface SummaryPeriod {
  births: number;
  deaths: number;
  purchases: number;
  sales: number;
  abortions: number;
}

const fields: { key: keyof SummaryPeriod; label: string }[] = [
  { key: "births", label: "Births" },
  { key: "deaths", label: "Deaths" },
  { key: "purchases", label: "Purchases" },
  { key: "sales", label: "Sales" },
  { key: "abortions", label: "Abortions" },
];

function SummarySection({ title, subtitle, period }: { title: string; subtitle?: string; period: SummaryPeriod }) {
  return (
    <div className="space-y-3">
      <div>
        <h2 className="text-sm font-bold text-[#FFFFFF]">{title}</h2>
        {subtitle && <p className="text-[11px] text-[#14F1D9] mt-0.5">{subtitle}</p>}
      </div>
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-4">
        {fields.map((f) => {
          const val = period[f.key];
          const isNegative = (f.key === "deaths" || f.key === "abortions") && val > 0;
          return (
            <div
              key={f.key}
              className="rounded-xl border border-[#334155] bg-[#1A1D24] p-5 transition-colors hover:border-[#334155] animate-fade-up"
            >
              <p className="text-[10px] font-medium uppercase tracking-wider text-[#14F1D9]">
                {f.label}
              </p>
              <p
                className={`mt-1.5 text-2xl font-bold ${
                  isNegative ? "text-[#f87171]" : "text-[#FFFFFF]"
                }`}
              >
                {formatNumber(val)}
              </p>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default function SummaryPage() {
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["summary"],
    queryFn: async () => {
      const res = await fetch("/api/summary");
      if (!res.ok) throw new Error("Failed to fetch summary");
      const json = await res.json();
      return json.data;
    },
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load summary data" onRetry={() => refetch()} />;

  const empty: SummaryPeriod = { births: 0, deaths: 0, purchases: 0, sales: 0, abortions: 0 };
  const daily: SummaryPeriod = data?.daily ?? empty;
  const weekly: SummaryPeriod = data?.weekly ?? empty;
  const monthly: SummaryPeriod = data?.monthly ?? empty;
  const overall: SummaryPeriod = data?.overall ?? empty;

  return (
    <div className="space-y-8">
      <SummarySection title="Daily (Yesterday)" subtitle={getChartDescription("Daily (Yesterday)")} period={daily} />
      <SummarySection title="Weekly (Last 7 Days)" subtitle={getChartDescription("Weekly (Last 7 Days)")} period={weekly} />
      <SummarySection title="Monthly (Last 30 Days)" subtitle={getChartDescription("Monthly (Last 30 Days)")} period={monthly} />
      <SummarySection title="Overall" subtitle="All-time breeding stock totals" period={overall} />
    </div>
  );
}
