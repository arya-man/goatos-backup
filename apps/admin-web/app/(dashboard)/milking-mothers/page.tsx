'use client';

import { useState, useMemo } from "react";
import { useIsMobile } from "@/lib/hooks/use-is-mobile";
import { useQuery } from "@tanstack/react-query";
import { ChartCard } from "@/components/charts/chart-card";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import type { MilkFeedingDashRow } from "@/app/api/milking-mothers/milk-feeding/route";

// ── Shared styles ──
const activeBtn = "rounded-lg bg-[#22262E] px-3 py-1.5 text-sm font-medium text-[#14F1D9]";
const inactiveBtn = "rounded-lg px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9]";
const activeDateBtn = "rounded-lg bg-[#14F1D9]/15 border border-[#14F1D9]/40 px-3 py-1.5 text-sm font-medium text-[#14F1D9]";
const inactiveDateBtn = "rounded-lg border border-[#334155] px-3 py-1.5 text-sm font-medium text-[#8899AA] hover:text-[#14F1D9] hover:border-[#14F1D9]/30";

// ── Types ──
type SectionToggle = "milking-mother" | "milk-fed-kids" | "milk-feeding-report" | "comparison";
type MilkFarm = "TOTAL" | "CBE" | "CPT";
type FeedFarm = "CBE" | "CPT";
type DatePreset = "latest" | "yesterday" | "custom";

interface MilkingRow {
  farm: string;
  goatid: string;
  breed: string;
}

interface MilkFedRow {
  date: string;
  farm: string;
  no_of_kids: number;
  milk_quantity_litres: number;
  milk_fed_litres: number;
}

function formatDisplayDate(dateStr: string): string {
  try {
    const [y, m, d] = dateStr.split("-").map(Number);
    return new Date(y, m - 1, d).toLocaleDateString("en-IN", {
      weekday: "long",
      year: "numeric",
      month: "long",
      day: "numeric",
    });
  } catch {
    return dateStr;
  }
}

function toYMD(date: Date): string {
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

const CHART_KEYS = ["Session 1", "Session 2", "Session 3", "Session 4"];

// ── Page ──
export default function MilkSectionPage() {
  const isMobile = useIsMobile();
  const [section, setSection] = useState<SectionToggle>("milking-mother");
  const [motherFarm, setMotherFarm] = useState<"CBE" | "CPT">("CBE");
  const [milkFarm, setMilkFarm] = useState<MilkFarm>("TOTAL");
  const [feedFarm, setFeedFarm] = useState<FeedFarm>("CBE");
  const [datePreset, setDatePreset] = useState<DatePreset>("latest");
  const [customDate, setCustomDate] = useState<string>("");

  const today = toYMD(new Date());
  const yesterday = toYMD(new Date(Date.now() - 86400000));

  // Resolved date to pass to API — null means "most recent"
  const selectedDate = useMemo((): string | null => {
    if (datePreset === "yesterday") return yesterday;
    if (datePreset === "custom" && customDate) return customDate;
    return null;
  }, [datePreset, customDate, yesterday]);

  // ── Milking Mothers query ──
  const { data: mothersData, isLoading: mothersLoading, error: mothersError, refetch: mothersRefetch } = useQuery({
    queryKey: ["milking-mothers"],
    queryFn: () => fetch("/api/milking-mothers").then((r) => r.json()).then((j) => j.data as MilkingRow[]),
    staleTime: 15 * 60 * 1000,
    enabled: section === "milking-mother",
  });

  // ── Milk Fed Kids query ──
  const { data: milkFedData, isLoading: milkFedLoading, error: milkFedError, refetch: milkFedRefetch } = useQuery({
    queryKey: ["milk-fed-kids", milkFarm],
    queryFn: async () => {
      const res = await fetch(`/api/milking-mothers/milk-fed-kids?farm=${milkFarm}`);
      if (!res.ok) throw new Error("Failed to fetch milk fed kids data");
      const json = await res.json();
      return json.data as MilkFedRow[];
    },
    staleTime: 15 * 60 * 1000,
    enabled: section === "milk-fed-kids",
  });

  // ── Comparison queries — CBE and CPT fetched in parallel ──
  const { data: cmpCBEData, isLoading: cmpCBELoading, error: cmpCBEError, refetch: cmpCBERefetch } = useQuery({
    queryKey: ["milk-fed-kids-cbe", yesterday],
    queryFn: async () => {
      const res = await fetch(`/api/milking-mothers/milk-fed-kids?farm=CBE&date=${yesterday}`);
      if (!res.ok) throw new Error("Failed to fetch CBE data");
      const json = await res.json();
      return json.data as MilkFedRow[];
    },
    staleTime: 15 * 60 * 1000,
    enabled: section === "comparison",
  });

  const { data: cmpCPTData, isLoading: cmpCPTLoading, error: cmpCPTError, refetch: cmpCPTRefetch } = useQuery({
    queryKey: ["milk-fed-kids-cpt", yesterday],
    queryFn: async () => {
      const res = await fetch(`/api/milking-mothers/milk-fed-kids?farm=CPT&date=${yesterday}`);
      if (!res.ok) throw new Error("Failed to fetch CPT data");
      const json = await res.json();
      return json.data as MilkFedRow[];
    },
    staleTime: 15 * 60 * 1000,
    enabled: section === "comparison",
  });

  // ── Milk Feeding Report query — keyed by date so switching date refetches ──
  const { data: feedingData, isLoading: feedingLoading, error: feedingError, refetch: feedingRefetch } = useQuery({
    queryKey: ["milk-feeding-report", selectedDate],
    queryFn: async () => {
      const url = selectedDate
        ? `/api/milking-mothers/milk-feeding?date=${selectedDate}`
        : "/api/milking-mothers/milk-feeding";
      const res = await fetch(url);
      if (!res.ok) throw new Error("Failed to fetch milk feeding data");
      const json = await res.json();
      return json.data as MilkFeedingDashRow[];
    },
    staleTime: 15 * 60 * 1000,
    enabled: section === "milk-feeding-report",
  });

  const filteredMothers = (mothersData ?? []).filter((r) => r.farm === motherFarm);

  // Milk Fed chart data
  const milkFedChartData = (milkFedData ?? [])
    .sort((a, b) => a.date.localeCompare(b.date))
    .map((r) => {
      const d = new Date(r.date);
      const dateLabel = d.toLocaleDateString("en-IN", { month: "short", day: "numeric" });
      return {
        name: `${dateLabel}\n${r.no_of_kids} kids`,
        "Milk Needed (L)": r.milk_quantity_litres,
        "Milk Fed (L)": r.milk_fed_litres,
      };
    });

  // ── Comparison chart data — sums across all dates per farm ──
  const comparisonChartData = useMemo(() => {
    const sumField = (rows: MilkFedRow[] | undefined, field: keyof MilkFedRow) =>
      (rows ?? []).reduce((acc, r) => acc + Number(r[field] || 0), 0);
    return [
      {
        name: "Milk Drinking Kids",
        CBE: sumField(cmpCBEData, "no_of_kids"),
        CPT: sumField(cmpCPTData, "no_of_kids"),
      },
      {
        name: "Milk Consumed (L)",
        CBE: sumField(cmpCBEData, "milk_fed_litres"),
        CPT: sumField(cmpCPTData, "milk_fed_litres"),
      },
    ];
  }, [cmpCBEData, cmpCPTData]);

  // ── Milk Feeding Report derived data ──
  // API returns only the latest date — grab date from first row
  const latestDate = feedingData?.[0]?.report_date ?? "";

  const sheds = useMemo(() => {
    if (!feedingData?.length) return [];
    return feedingData.filter((r) => r.farm === feedFarm);
  }, [feedingData, feedFarm]);

  // Feeding Success = A1 fed + A2 recovered (total kids who drank cow milk)
  const successBySheds = useMemo(() => sheds.map((r) => ({
    name: r.shed_tag,
    "Session 1": r.fed_a1_s1 + r.recovered_a2_s1,
    "Session 2": r.fed_a1_s2 + r.recovered_a2_s2,
    "Session 3": r.fed_a1_s3 + r.recovered_a2_s3,
    "Session 4": r.fed_a1_s4 + r.recovered_a2_s4,
  })), [sheds]);

  // Did Not Drink Milk = A1 refused + A2 refused (i.e., total who never drank cow milk = ors_count)
  // a1_refused = went to retry; ors_count = still didn't drink after retry → both attempts failed
  const refusalsBySheds = useMemo(() => sheds.map((r) => ({
    name: r.shed_tag,
    "Session 1": r.a1_refused_s1 + r.ors_count_s1,
    "Session 2": r.a1_refused_s2 + r.ors_count_s2,
    "Session 3": r.a1_refused_s3 + r.ors_count_s3,
    "Session 4": r.a1_refused_s4 + r.ors_count_s4,
  })), [sheds]);

  // Drank ORS = all kids given ORS in each session (independent of cow milk)
  const orsBySheds = useMemo(() => sheds.map((r) => ({
    name: r.shed_tag,
    "Session 1": r.ors_count_s1,
    "Session 2": r.ors_count_s2,
    "Session 3": r.ors_count_s3,
    "Session 4": r.ors_count_s4,
  })), [sheds]);

  return (
    <div className="flex flex-col h-full gap-6">
      {/* ── Section toggle ── */}
      <div className="flex flex-wrap items-center gap-1 shrink-0">
        {([
          { key: "milking-mother", label: "Milking Mother" },
          { key: "milk-fed-kids", label: "Milk Fed Kids" },
          { key: "milk-feeding-report", label: "Milk Feeding Report" },
          { key: "comparison", label: "Comparison" },
        ] as const).map((t) => (
          <button
            key={t.key}
            onClick={() => setSection(t.key)}
            className={section === t.key ? activeBtn : inactiveBtn}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* ── Milking Mother ── */}
      {section === "milking-mother" && (
        <>
          {mothersLoading && <LoadingState />}
          {mothersError && <ErrorState message="Failed to load milking mothers data" onRetry={() => mothersRefetch()} />}
          {!mothersLoading && !mothersError && (
            <div className="flex flex-col flex-1 gap-6 min-h-0">
              <div className="flex flex-wrap items-center gap-1 shrink-0">
                {(["CBE", "CPT"] as const).map((f) => (
                  <button key={f} onClick={() => setMotherFarm(f)} className={motherFarm === f ? activeBtn : inactiveBtn}>
                    {f}
                  </button>
                ))}
                <span className="text-xs text-[#8899AA] ml-2">{filteredMothers.length} records</span>
              </div>

              <div className="flex-1 min-h-0 overflow-auto rounded-xl border border-[#334155]">
                <table className="w-full text-sm" style={{ minWidth: 360 }}>
                  <thead className="sticky top-0 z-10">
                    <tr className="bg-[#1A1D24]">
                      <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Farm</th>
                      <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Goat ID</th>
                      <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Breed</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredMothers.length === 0 ? (
                      <tr>
                        <td colSpan={3} className="px-4 py-8 text-center text-sm text-[#8899AA]">
                          No records found for {motherFarm}
                        </td>
                      </tr>
                    ) : (
                      filteredMothers.map((r, i) => (
                        <tr key={`${r.goatid}-${i}`} className="border-t border-[#334155] transition-colors hover:bg-[#22262E]/50">
                          <td className="px-3 py-2.5 text-[#8899AA]">{r.farm}</td>
                          <td className="px-3 py-2.5 text-[#B0BEC5] font-medium">{r.goatid}</td>
                          <td className="px-3 py-2.5 text-[#8899AA]">{r.breed}</td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}

      {/* ── Milk Fed Kids ── */}
      {section === "milk-fed-kids" && (
        <>
          <div className="flex flex-wrap items-center gap-1">
            {(["TOTAL", "CBE", "CPT"] as MilkFarm[]).map((f) => (
              <button key={f} onClick={() => setMilkFarm(f)} className={milkFarm === f ? activeBtn : inactiveBtn}>
                {f === "TOTAL" ? "Total" : f}
              </button>
            ))}
          </div>

          {milkFedLoading && <LoadingState />}
          {milkFedError && <ErrorState message="Failed to load milk fed kids data" onRetry={() => milkFedRefetch()} />}
          {!milkFedLoading && !milkFedError && (
            <ChartCard
              title="Milk Needed vs Milk Fed (Last 7 Days)"
              subtitle="Daily comparison of litres needed and litres fed, with number of kids on each day"
            >
              <GroupedBarChart
                data={milkFedChartData}
                keys={["Milk Needed (L)", "Milk Fed (L)"]}
                showDataLabels={!isMobile}
                valueSuffix=" L"
                yPadding={20}
              />
            </ChartCard>
          )}
        </>
      )}

      {/* ── Milk Feeding Report ── */}
      {section === "milk-feeding-report" && (
        <>
          {/* Header: date selector + farm toggle */}
          <div className="shrink-0 flex flex-col gap-2">
            <div className="flex items-center gap-2 flex-wrap">
              {/* Date presets */}
              {(["latest", "yesterday", "custom"] as DatePreset[]).map((preset) => (
                <button
                  key={preset}
                  onClick={() => setDatePreset(preset)}
                  className={datePreset === preset ? activeDateBtn : inactiveDateBtn}
                >
                  {preset === "latest" ? "Most Recent" : preset === "yesterday" ? "Yesterday" : "Select Date"}
                </button>
              ))}
              {/* Date picker input — shown only when custom is selected */}
              {datePreset === "custom" && (
                <input
                  type="date"
                  value={customDate}
                  max={today}
                  onChange={(e) => setCustomDate(e.target.value)}
                  className="rounded-lg border border-[#334155] bg-[#1A1D24] px-3 py-1.5 text-sm text-[#FFFFFF] focus:border-[#14F1D9]/60 focus:outline-none"
                />
              )}
            </div>
            <p className="text-base font-semibold text-[#FFFFFF]">
              {latestDate ? formatDisplayDate(latestDate) : "Loading…"}
            </p>
            <div className="flex flex-wrap items-center gap-1">
              {(["CBE", "CPT"] as FeedFarm[]).map((f) => (
                <button key={f} onClick={() => setFeedFarm(f)} className={feedFarm === f ? activeBtn : inactiveBtn}>
                  {f}
                </button>
              ))}
            </div>
          </div>

          {feedingLoading && <LoadingState />}
          {feedingError && <ErrorState message="Failed to load milk feeding data" onRetry={() => feedingRefetch()} />}

          {!feedingLoading && !feedingError && sheds.length > 0 && (
            <>
              <ChartCard
                title="Feeding Success by Shed"
                subtitle="Kids who drank milk (Attempt 1 + Attempt 2 combined) · session-wise"
              >
                <GroupedBarChart data={successBySheds} keys={CHART_KEYS} height={280} showDataLabels={!isMobile} yPadding={15} />
              </ChartCard>

              <ChartCard
                title="Did Not Drink Milk"
                subtitle="Kids who refused both Attempt 1 and Attempt 2 (all refusals) · session-wise"
              >
                <GroupedBarChart data={refusalsBySheds} keys={CHART_KEYS} height={280} showDataLabels={!isMobile} yPadding={15} />
              </ChartCard>

              <ChartCard
                title="Drank ORS"
                subtitle="Kids who were given ORS · session-wise"
              >
                <GroupedBarChart data={orsBySheds} keys={CHART_KEYS} height={280} showDataLabels={!isMobile} yPadding={15} />
              </ChartCard>
            </>
          )}

          {!feedingLoading && !feedingError && sheds.length === 0 && (
            <div className="flex items-center justify-center py-20">
              <p className="text-sm text-[#8899AA]">No feeding data available</p>
            </div>
          )}
        </>
      )}

      {/* ── Comparison ── */}
      {section === "comparison" && (
        <>
          {(cmpCBELoading || cmpCPTLoading) && <LoadingState />}
          {(cmpCBEError || cmpCPTError) && (
            <ErrorState
              message="Failed to load comparison data"
              onRetry={() => { cmpCBERefetch(); cmpCPTRefetch(); }}
            />
          )}
          {!cmpCBELoading && !cmpCPTLoading && !cmpCBEError && !cmpCPTError && (
            <ChartCard
              title="CBE vs CPT Comparison"
              subtitle={`Milk drinking kids and milk consumed across both farms · ${formatDisplayDate(yesterday)}`}
            >
              <GroupedBarChart
                data={comparisonChartData}
                keys={["CBE", "CPT"]}
                showDataLabels={!isMobile}
                yPadding={20}
              />
            </ChartCard>
          )}
        </>
      )}
    </div>
  );
}
