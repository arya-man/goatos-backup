/* eslint-disable @typescript-eslint/no-explicit-any */
'use client';

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { ChartCard } from "@/components/charts/chart-card";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatNumber, TEXT_MUTED, TEXT_TERTIARY } from "@/lib/constants";

type Farm = "Total" | "CBE" | "CPT";
const FARM_PARAM: Record<Farm, string> = { Total: "TOTAL", CBE: "CBE", CPT: "CPT" };

type MISTab = "expenditure-gain" | "delta";

const activeBtn  = "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]";
const inactiveBtn = "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]";

function compactINR(v: number): string {
  const abs = Math.abs(v);
  if (abs >= 1_00_00_000) return `₹${(v / 1_00_00_000).toFixed(1)}Cr`;
  if (abs >= 1_00_000)    return `₹${Math.ceil(v / 1_00_000)}L`;
  if (abs >= 1_000)       return `₹${Math.ceil(v / 1_000)}K`;
  return `₹${Math.ceil(v)}`;
}

const TOOLTIP_STYLE = {
  contentStyle: { backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 },
  labelStyle: { color: "#14F1D9", fontSize: 11 },
  itemStyle: { color: "#FFFFFF" },
};

function inrFormatter(value: unknown, name: any): [string, any] {
  return [`₹${formatNumber(Number(value ?? 0))}`, name];
}


// ── Farm toggle ──────────────────────────────────────────────────────────────

function FarmToggle({ farm, setFarm }: { farm: Farm; setFarm: (f: Farm) => void }) {
  return (
    <div role="group" aria-label="Farm filter" className="flex items-center gap-1">
      {(["Total", "CBE", "CPT"] as Farm[]).map((f) => (
        <button key={f} aria-pressed={farm === f} onClick={() => setFarm(f)}
          className={farm === f ? activeBtn : inactiveBtn}>
          {f}
        </button>
      ))}
    </div>
  );
}

// ── Expenditure vs Gain ──────────────────────────────────────────────────────

interface BusinessRow {
  date: string;
  feed_spend: number;
  daily_gain_value: number;
  kid_count: number;
}

function ExpenditureGainTab() {
  const [farm, setFarm] = useState<Farm>("Total");

  const { data, isLoading, error, refetch } = useQuery<{ chartData: BusinessRow[] }>({
    queryKey: ["business", farm],
    queryFn: () =>
      fetch(`/api/business?farm=${FARM_PARAM[farm]}`).then((r) => r.json()).then((j) => j.data),
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load expenditure data" onRetry={() => refetch()} />;

  const chartData = data?.chartData ?? [];

  return (
    <div className="space-y-6">
      <FarmToggle farm={farm} setFarm={setFarm} />

      <ChartCard
        title="Feed Expenditure vs Daily Gain Value"
        subtitle="How much we spend on feed daily vs the weight gain revenue it generates"
      >
        <ResponsiveContainer width="100%" height={320}>
          <BarChart data={chartData} margin={{ top: 20, right: 16, bottom: 4, left: 8 }} barCategoryGap="30%">
            <XAxis
              dataKey="date"
              axisLine={false} tickLine={false}
              tick={{ fontSize: 10, fill: TEXT_MUTED }}
              interval={Math.max(0, Math.floor((chartData.length ?? 0) / 8))}
              angle={-45} textAnchor="end" height={50}
            />
            <YAxis
              axisLine={false} tickLine={false}
              tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
              tickFormatter={compactINR}
              width={72}
            />
            <Tooltip {...TOOLTIP_STYLE} formatter={inrFormatter} />
            <Legend wrapperStyle={{ fontSize: 12, color: TEXT_MUTED, paddingTop: 8 }} />
            <Bar dataKey="feed_spend" name="Feed Expenditure" fill="#14F1D9" animationDuration={800} animationEasing="ease-out" />
            <Bar dataKey="daily_gain_value" name="Daily Gain Value" fill="#C6FF00" animationDuration={800} animationEasing="ease-out" />
          </BarChart>
        </ResponsiveContainer>
      </ChartCard>
    </div>
  );
}

// ── Delta ────────────────────────────────────────────────────────────────────

interface DeltaRow {
  month: string;
  month_label: string;
  farm: string;
  feed_spend: number;
  hr_expense: number;
  total_expense: number;
  meat_gain_value: number;
  milk_revenue: number;
  total_revenue: number;
  farm_value: number;
  net_delta: number;
}

interface DeltaData {
  rows: DeltaRow[];
}

function DeltaTab() {
  const [farm, setFarm] = useState<Farm>("Total");

  const { data, isLoading, error, refetch } = useQuery<DeltaData>({
    queryKey: ["delta", farm],
    queryFn: () =>
      fetch(`/api/delta?farm=${FARM_PARAM[farm]}`).then((r) => r.json()).then((j) => j.data),
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load delta data" onRetry={() => refetch()} />;

  // API returns DESC; reverse to chronological for charts
  const rows = (data?.rows ?? []).slice().reverse();

  return (
    <div className="space-y-6">
      <FarmToggle farm={farm} setFarm={setFarm} />

      {/* Combined chart – Expenditure vs Revenue vs Farm Value vs Profit */}
      <ChartCard
        title="Monthly Delta Overview"
        subtitle="Total expenditure, total revenue in stock, farm value and profit"
      >
        <ResponsiveContainer width="100%" height={360}>
          <BarChart data={rows} margin={{ top: 28, right: 16, bottom: 4, left: 8 }} barCategoryGap="25%" barGap={3}>
            <XAxis dataKey="month_label" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} />
            <YAxis
              axisLine={false} tickLine={false}
              tick={{ fontSize: 11, fill: TEXT_TERTIARY }}
              tickFormatter={compactINR}
              width={68}
              scale="log"
              domain={[1, 'auto']}
              allowDataOverflow
            />
            <Tooltip
              contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
              labelStyle={{ color: "#14F1D9", fontSize: 11 }}
              itemStyle={{ color: "#FFFFFF" }}
              formatter={(value: unknown, name: any) => [`₹${formatNumber(Number(value ?? 0))}`, name]}
            />
            <Legend wrapperStyle={{ fontSize: 12, color: TEXT_MUTED, paddingTop: 8 }} />
            <Bar dataKey="total_expense" name="Total Expenditure" fill="#14F1D9" animationDuration={800} animationEasing="ease-out"
              label={{ position: "top", fontSize: 10, fill: "#FFFFFF", formatter: (v: unknown) => String(compactINR(Number(v))) }}
            />
            <Bar dataKey="total_revenue" name="Total Revenue in Stock" fill="#C6FF00" animationDuration={800} animationEasing="ease-out"
              label={{ position: "top", fontSize: 10, fill: "#FFFFFF", formatter: (v: unknown) => String(compactINR(Number(v))) }}
            />
            <Bar dataKey="farm_value" name="Farm Value" fill="#40BA80" animationDuration={800} animationEasing="ease-out"
              label={{ position: "top", fontSize: 10, fill: "#FFFFFF", formatter: (v: unknown) => String(compactINR(Number(v))) }}
            />
            <Bar dataKey="net_delta" name="Profit" fill="#146488" animationDuration={800} animationEasing="ease-out"
              label={{ position: "top", fontSize: 10, fill: "#FFFFFF", formatter: (v: unknown) => String(compactINR(Number(v))) }}
            />
          </BarChart>
        </ResponsiveContainer>
      </ChartCard>
    </div>
  );
}

// ── MIS Page ─────────────────────────────────────────────────────────────────

const MIS_TABS: { id: MISTab; label: string }[] = [
  { id: "expenditure-gain", label: "Expenditure vs Gain" },
  { id: "delta", label: "Delta" },
];

export default function MISPage() {
  const [activeTab, setActiveTab] = useState<MISTab>("expenditure-gain");

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-1">
        {MIS_TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => setActiveTab(t.id)}
            className={activeTab === t.id ? activeBtn : inactiveBtn}
          >
            {t.label}
          </button>
        ))}
      </div>

      {activeTab === "expenditure-gain" && <ExpenditureGainTab />}
      {activeTab === "delta" && <DeltaTab />}
    </div>
  );
}
