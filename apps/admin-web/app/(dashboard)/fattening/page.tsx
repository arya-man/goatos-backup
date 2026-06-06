'use client';

import { useState } from "react";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";
import { useQuery } from "@tanstack/react-query";
import { TrendingUp } from "lucide-react";
import { KPICard } from "@/components/charts/kpi-card";
import { ChartCard } from "@/components/charts/chart-card";
import { HorizontalBarChart } from "@/components/charts/horizontal-bar";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { StackedBarChart } from "@/components/charts/stacked-bar";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import { formatNumber, formatINR } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { getChartDescription } from "@/lib/display-utils";

const tabs = [
  { key: "farmwise", label: "Farmwise Count" },
  { key: "adg", label: "Average Daily Gain" },
  { key: "loadwise", label: "Loadwise" },
] as const;

type Tab = (typeof tabs)[number]["key"];
type DemoFarm = "all" | "cbe" | "cpt";
type WeighedFarm = "total" | "cbe" | "cpt";

interface MetricCardProps {
  label: string;
  value: string | null;
}

type LoadwiseSummaryRow = {
  loadId: string;
  vendor: string;
  procured: number;
  sales: number;
  mortality: number;
  currentCount: number;
  mortalityRatePct: number;
};

type LoadAgeRow = {
  loadId: string;
  vendor: string;
  unloadedDate: string;
  daysSinceUnloading: number;
};

type LoadSalesComparisonRow = {
  loadId: string;
  vendor: string;
  purchaseCost: number;
  salesCost: number;
};

const EXCLUDED_LOAD_ID = "113";
const EXCLUDED_VENDOR = "nutriplusfoodspvtltd";

function formatCompactINR(value: number) {
  const amount = Math.abs(value);
  if (amount >= 10000000) return `₹${(value / 10000000).toFixed(1)}Cr`;
  if (amount >= 100000) return `₹${(value / 100000).toFixed(1)}L`;
  return formatINR(value);
}

function normalizeVendor(value: string) {
  return value.toLowerCase().replace(/[^a-z0-9]/g, "");
}

function isExcludedLoadVendor(loadId: string, vendor: string) {
  return String(loadId).trim() === EXCLUDED_LOAD_ID && normalizeVendor(vendor).includes(EXCLUDED_VENDOR);
}

function isExcludedLoadLabel(label: string) {
  const [loadId = "", ...vendorParts] = label.split("\n");
  return isExcludedLoadVendor(loadId, vendorParts.join(" "));
}

function MetricCard({ label, value }: MetricCardProps) {
  return (
    <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-3 transition-colors hover:border-[#334155]">
      <p className="text-[10px] uppercase tracking-wider text-[#14F1D9]">{label}</p>
      <p className="text-lg font-bold text-[#FFFFFF] mt-1">{value ?? "–"}</p>
    </div>
  );
}

export default function FatteningPage() {
  const isMobile = useIsMobile();
  const [tab, setTab] = useState<Tab>("farmwise");
  const [demoFarm, setDemoFarm] = useState<DemoFarm>("all");
  const [weighedFarm, setWeighedFarm] = useState<WeighedFarm>("total");

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["fattening"],
    queryFn: () => fetch("/api/fattening").then((r) => r.json()).then((j) => j.data),
    staleTime: 15 * 60 * 1000,
  });

  if (isLoading) return <LoadingState />;
  if (error) return <ErrorState message="Failed to load fattening data" onRetry={() => refetch()} />;

  const adg = data?.adg ?? { overall: 0, cbe: 0, cpt: 0 };
  const adgByGenderBreed = data?.adgByGenderBreed ?? [];
  const adgByBreed = data?.adgByBreed ?? [];
  const adgByStatus = data?.adgByStatus ?? [];
  const adgByGender = data?.adgByGender ?? [];
  const adgByBreedStatus = data?.adgByBreedStatus ?? [];
  const adgByBreedStatusKeys: string[] = data?.adgByBreedStatusKeys ?? ["F2", "K2"];
  const loadADGByBreed = (data?.loadADGByBreed ?? []).filter((d: { load: string }) => !isExcludedLoadLabel(d.load));
  const avgWeightByLoad = (data?.avgWeightByLoad ?? []).filter((d: { load: string }) => !isExcludedLoadLabel(d.load));
  const last5Weighings = (data?.last5Weighings ?? []).filter((d: { load: string }) => !isExcludedLoadLabel(d.load));
  const weightChanges = (data?.weightChanges ?? []).filter((d: { load: string }) => !isExcludedLoadLabel(d.load));
  const kidsWeighedPct = data?.kidsWeighedPct ?? { total: [], cbe: [], cpt: [] };
  const loadwiseSummary: LoadwiseSummaryRow[] = data?.loadwiseSummary ?? [];
  const loadAge: LoadAgeRow[] = data?.loadAge ?? [];
  const loadSalesComparison: LoadSalesComparisonRow[] = data?.loadSalesComparison ?? [];

  const defaultMetrics = { count: 0, weight: 0, avgWeight: 0, value: 0, males: 0, females: 0, over30: 0, over35: 0 };
  const overview = data?.overview ?? { total: defaultMetrics, cbe: defaultMetrics, cpt: defaultMetrics };
  const allDemographics = data?.demographics ?? { all: [], cbe: [], cpt: [] };
  const demographics = allDemographics[demoFarm] ?? [];

  return (
    <div className="space-y-6">
      <nav className="flex flex-wrap items-center gap-1">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={
              tab === t.key
                ? "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]"
                : "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]"
            }
          >
            {t.label}
          </button>
        ))}
      </nav>

      {tab === "farmwise" && (
        <div className="space-y-8">
          <div className="rounded-lg border border-[#334155] bg-[#22262E]/50 px-4 py-2 text-xs text-[#B0BEC5]">
            Based on latest weighing data
          </div>
          <div>
            <h2 className="text-sm font-medium text-[#FFFFFF] mb-4">Overview</h2>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-6">
              {(["total", "cbe", "cpt"] as const).map((key) => {
                const m = overview[key];
                const title = key === "total" ? "Total" : key.toUpperCase();
                return (
                  <div key={key} className="space-y-2">
                    <h3 className="text-xs font-medium text-[#B0BEC5] uppercase tracking-wider">{title}</h3>
                    <div className="grid grid-cols-2 sm:grid-cols-2 gap-2">
                      <MetricCard label="Count" value={formatNumber(m.count)} />
                      <MetricCard label="Weight" value={`${formatNumber(Math.round(m.weight))} kg`} />
                      <MetricCard label="Avg Weight" value={`${m.avgWeight} kg`} />
                      <MetricCard label="Value" value={formatINR(m.value)} />
                      <MetricCard label="Males" value={formatNumber(m.males)} />
                      <MetricCard label="Females" value={formatNumber(m.females)} />
                      <MetricCard label="Weight > 30" value={formatNumber(m.over30)} />
                      <MetricCard label="Weight > 35" value={formatNumber(m.over35)} />
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          <div>
            <div className="flex items-center gap-4 mb-4">
              <h2 className="text-sm font-medium text-[#FFFFFF]">Demographics</h2>
              <div className="flex items-center gap-1">
                {(["all", "cbe", "cpt"] as DemoFarm[]).map((f) => (
                  <button
                    key={f}
                    onClick={() => setDemoFarm(f)}
                    className={
                      demoFarm === f
                        ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                    }
                  >
                    {f === "all" ? "All" : f.toUpperCase()}
                  </button>
                ))}
              </div>
            </div>

            <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-4">
              {demographics.map((s: { stage: string; count: number | null; weight: number | null; avgWeight: number | null; value: number | null; males: number | null; females: number | null }) => (
                <div key={s.stage} className="space-y-2">
                  <h3 className="text-xs font-medium text-[#B0BEC5] text-center">{s.stage}</h3>
                  <MetricCard label="Count" value={s.count != null ? formatNumber(s.count) : null} />
                  <MetricCard label="Weight" value={s.weight != null ? `${formatNumber(Math.round(s.weight))} kg` : null} />
                  <MetricCard label="Avg Wt" value={s.avgWeight != null ? `${s.avgWeight} kg` : null} />
                  <MetricCard label="Value" value={s.value != null ? formatINR(s.value) : null} />
                  <MetricCard label="Males" value={s.males != null ? formatNumber(s.males) : null} />
                  <MetricCard label="Females" value={s.females != null ? formatNumber(s.females) : null} />
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {tab === "adg" && (
        <div className="space-y-6">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <KPICard label="Overall ADG" value={`${adg.overall}g`} icon={<TrendingUp size={18} />} delay={0} />
            <KPICard label="CBE ADG" value={`${adg.cbe}g`} icon={<TrendingUp size={18} />} delay={1} />
            <KPICard label="CPT ADG" value={`${adg.cpt}g`} icon={<TrendingUp size={18} />} delay={2} />
          </div>

          <ChartCard title="ADG by Gender & Breed" subtitle={getChartDescription("ADG by Gender & Breed")}>
            <GroupedBarChart
              data={adgByGenderBreed.map((d: { breed: string; male: number; female: number }) => ({
                name: d.breed,
                Male: d.male,
                Female: d.female,
              }))}
              keys={["Male", "Female"]}
            />
          </ChartCard>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ChartCard title="ADG by Breed" subtitle={getChartDescription("ADG by Breed")}>
              <HorizontalBarChart
                data={adgByBreed.map((d: { breed: string; adg: number }) => ({
                  name: d.breed,
                  value: d.adg,
                }))}
                valueLabel="ADG (g)"
              />
            </ChartCard>

            <ChartCard title="ADG by Status" subtitle={getChartDescription("ADG by Status")}>
              <HorizontalBarChart
                data={adgByStatus.map((d: { status: string; adg: number }) => ({
                  name: d.status,
                  value: d.adg,
                }))}
                valueLabel="ADG (g)"
              />
            </ChartCard>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ChartCard title="ADG by Gender" subtitle={getChartDescription("ADG by Gender")}>
              <HorizontalBarChart data={adgByGender} valueLabel="ADG (g)" showLabels={!isMobile} />
            </ChartCard>

            <ChartCard title="Average Daily Gain by Breed & Status (grams)" subtitle={getChartDescription("ADG by Breed & Status")}>
              <StackedBarChart
                data={adgByBreedStatus}
                keys={adgByBreedStatusKeys}
                layout="horizontal"
                showSegmentLabels
                showLabels={!isMobile}
                colors={['#14F1D9', '#C6FF00', '#14D4E8', '#10FF98', '#1AF0B0', '#A8D900', '#22E0A0', '#40BA80']}
              />
            </ChartCard>
          </div>

          <div>
            <div className="flex items-center gap-4 mb-3">
              <span className="text-sm font-medium text-[#FFFFFF]">Kids Weighed %</span>
              <div className="flex items-center gap-1">
                {(["total", "cbe", "cpt"] as WeighedFarm[]).map((f) => (
                  <button
                    key={f}
                    onClick={() => setWeighedFarm(f)}
                    className={
                      weighedFarm === f
                        ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                    }
                  >
                    {f === "total" ? "Total" : f.toUpperCase()}
                  </button>
                ))}
              </div>
            </div>
            <ChartCard title="Percentage weighed per week" subtitle={weighedFarm === "total" ? "All farms" : weighedFarm.toUpperCase()}>
              <HorizontalBarChart
                data={(kidsWeighedPct[weighedFarm] ?? []).map((d: { week: string; pct: number }) => ({
                  name: d.week,
                  value: d.pct,
                }))}
                valueLabel="Weighed (%)"
              />
            </ChartCard>
          </div>
        </div>
      )}

      {tab === "loadwise" && (
        <div className="space-y-6">
          {loadwiseSummary.length > 0 && (
            <ChartCard title="Load-wise Summary" subtitle="Procured Count vs Current Count vs Sales vs Mortality">
              <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                <div className="lg:col-span-2">
                  <GroupedBarChart
                    data={loadwiseSummary.map((r) => ({
                      name: `${r.loadId}\n${r.vendor}`,
                      "Procured Count": r.procured,
                      "Current Count": r.currentCount,
                      Sales: r.sales,
                      Mortality: r.mortality,
                    }))}
                    keys={["Procured Count", "Current Count", "Sales", "Mortality"]}
                    showDataLabels={!isMobile}
                    showAllXLabels
                    xLabelTiltThreshold={6}
                    yPadding={10}
                  />
                </div>
                <div className="self-center overflow-auto rounded-xl border border-[#334155]">
                  <table className="w-full text-xs">
                    <thead className="sticky top-0 z-10">
                      <tr className="border-b border-[#334155] bg-[#1A1D24]">
                        <th className="px-2 py-1.5 text-left font-medium text-[#B0BEC5]">Load</th>
                        <th className="px-2 py-1.5 text-right font-medium text-[#B0BEC5]">Mortality Rate</th>
                      </tr>
                    </thead>
                    <tbody>
                      {loadwiseSummary.map((r) => (
                        <tr key={r.loadId} className="border-b border-[#334155] last:border-0 transition-colors hover:bg-[#22262E]/50">
                          <td className="px-2 py-1 text-[#B0BEC5] font-medium whitespace-pre-line leading-tight">{`${r.loadId}\n${r.vendor}`}</td>
                          <td className={cn(
                            "px-2 py-1 text-right font-medium",
                            r.mortalityRatePct > 5 ? "text-[#f87171]" : r.mortalityRatePct > 2 ? "text-[#fbbf24]" : "text-[#4ade80]"
                          )}>
                            {r.mortalityRatePct}%
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            </ChartCard>
          )}

          {loadAge.length > 0 && (
            <ChartCard title="Load-wise Age" subtitle={getChartDescription("Load-wise Age")}>
              <HorizontalBarChart
                data={loadAge.map((r) => ({
                  name: `${r.loadId}\n${r.vendor}`,
                  value: r.daysSinceUnloading,
                }))}
                valueLabel="Days on farm"
                labelFormatter={(v) => `${Math.round(v)}d`}
                showLabels={!isMobile}
                yAxisWidth={isMobile ? 136 : 176}
              />
            </ChartCard>
          )}

          {loadSalesComparison.length > 0 && (
            <ChartCard title="Purchase Cost vs Sales Cost" subtitle="Loads sold at least 90%">
              <GroupedBarChart
                data={loadSalesComparison.map((r) => ({
                  name: `${r.loadId}\n${r.vendor}`,
                  "Purchase Cost": r.purchaseCost,
                  "Sales Cost": r.salesCost,
                }))}
                keys={["Purchase Cost", "Sales Cost"]}
                layout="horizontal"
                showDataLabels={!isMobile}
                yAxisWidth={isMobile ? 136 : 176}
                valueFormatter={formatCompactINR}
              />
            </ChartCard>
          )}

          <ChartCard title="Load-wise Average Daily Gain by Breed (grams)" subtitle={getChartDescription("Load-wise ADG by Breed")}>
            <GroupedBarChart
              data={(() => {
                const loadMap: Record<string, Record<string, number>> = {};
                const breeds = new Set<string>();
                loadADGByBreed.forEach((d: { load: string; breed: string; adg: number }) => {
                  if (!loadMap[d.load]) loadMap[d.load] = {};
                  loadMap[d.load][d.breed] = d.adg;
                  breeds.add(d.breed);
                });
                const sortOrder = avgWeightByLoad.map((d: { load: string }) => d.load);
                return Object.entries(loadMap)
                  .sort(([a], [b]) => {
                    const ai = sortOrder.indexOf(a);
                    const bi = sortOrder.indexOf(b);
                    return (ai < 0 ? 999 : ai) - (bi < 0 ? 999 : bi);
                  })
                  .map(([load, values]) => ({
                    name: load,
                    ...values,
                  }));
              })()}
              keys={(() => {
                const breeds = new Set<string>();
                loadADGByBreed.forEach((d: { breed: string }) => breeds.add(d.breed));
                return Array.from(breeds);
              })()}
              layout="horizontal"
              showDataLabels={!isMobile}
            />
          </ChartCard>

          <ChartCard title="Average Weight by Load (kg)" subtitle={getChartDescription("Average Weight by Load")}>
            <HorizontalBarChart
              data={avgWeightByLoad.map((d: { load: string; avgWeight: number }) => ({
                name: d.load,
                value: d.avgWeight,
              }))}
              valueLabel="Average Weight (kg)"
            />
          </ChartCard>

          <ChartCard title="Last 5 Weekly ADG by Load (g/day)" subtitle={getChartDescription("Last 5 Weekly ADG by Load")}>
            <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
              <div>
                <GroupedBarChart
                  data={last5Weighings.map((d: { load: string; w1?: number; w2?: number; w3?: number; w4?: number; w5?: number }) => {
                    const entry: { name: string; [key: string]: string | number } = { name: d.load };
                    const weeks: [string, number | undefined][] = [
                      ["Week 1", d.w1], ["Week 2", d.w2], ["Week 3", d.w3],
                      ["Week 4", d.w4], ["Week 5 (This week)", d.w5],
                    ];
                    for (const [k, v] of weeks) {
                      if (v != null) {
                        entry[k] = Math.abs(v);          // bar always goes right
                        entry[`${k}_orig`] = v;          // original signed value for label
                      }
                    }
                    return entry;
                  })}
                  keys={["Week 5 (This week)", "Week 4", "Week 3", "Week 2", "Week 1"]}
                  layout="horizontal"
                  showDataLabels={!isMobile}
                  labelDataKeySuffix="_orig"
                  labelFormatter={(v: unknown) => {
                    const n = Number(v);
                    return `${n < 0 ? '-' : ''}${Math.abs(Math.round(n))}g`;
                  }}
                  tooltipFormatter={(_, name, item) => {
                    const orig = item.payload[`${name}_orig`];
                    const n = orig !== undefined ? Number(orig) : NaN;
                    if (isNaN(n)) return `${_}`;
                    return `${n < 0 ? '-' : ''}${Math.abs(Math.round(n))}g`;
                  }}
                />
              </div>

              <div className="overflow-auto max-h-[300px] rounded-xl border border-[#334155]">
                <table className="w-full text-xs">
                  <thead className="sticky top-0 z-10">
                    <tr className="border-b border-[#334155] bg-[#1A1D24]">
                      <th className="px-2 py-2 text-left font-medium text-[#B0BEC5]">Load</th>
                      <th className="px-2 py-2 text-right font-medium text-[#B0BEC5]">Last 7 Days Gain</th>
                    </tr>
                  </thead>
                  <tbody>
                    {weightChanges.map((r: { load: string; gain: number | null }) => (
                      <tr key={r.load} className="border-b border-[#334155] transition-colors hover:bg-[#22262E]/50">
                        <td className="px-2 py-1.5 text-[#B0BEC5] font-medium whitespace-pre-line">{r.load}</td>
                        <td className={cn(
                          "px-2 py-1.5 text-right font-medium",
                          r.gain == null || r.gain === 0 ? "text-[#8899AA]" : r.gain > 0 ? "text-[#4ade80]" : "text-[#f87171]"
                        )}>
                          {r.gain == null || r.gain === 0 ? "–" : r.gain > 0 ? `+${Math.round(r.gain)}g` : `${Math.round(r.gain)}g`}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          </ChartCard>
        </div>
      )}
    </div>
  );
}
