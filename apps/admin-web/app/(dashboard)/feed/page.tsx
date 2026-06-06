/* eslint-disable @typescript-eslint/no-explicit-any */
"use client";

import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  LabelList,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { ChartCard } from "@/components/charts/chart-card";
import { PieChart as VPieChart } from "@/components/charts/pie-chart";
import { GroupedBarChart } from "@/components/charts/grouped-bar";
import { GanttChart } from "@/components/charts/gantt-chart";
import { FeedStockTable } from "@/components/tables/feed-stock-table";
import { FeedLoadSummaryTable } from "@/components/tables/feed-load-summary-table";
import { KPICard } from "@/components/charts/kpi-card";
import { LoadingState } from "@/components/ui/loading-state";
import { ErrorState } from "@/components/ui/error-state";
import {
  formatNumber,
  TEXT_MUTED,
  TEXT_TERTIARY,
} from "@/lib/constants";

const fmt2 = (v: number) => v.toFixed(2);
const tooltipFmt2 = (value: unknown) => Number(value ?? 0).toFixed(2);
function formatINRCompactOneDecimal(value: number): string {
  const abs = Math.abs(value);
  const sign = value < 0 ? "-" : "";
  if (abs >= 1_00_000) return `${sign}\u20B9${(abs / 1_00_000).toFixed(1)}L`;
  if (abs >= 1_000) return `${sign}\u20B9${(abs / 1_000).toFixed(1)}k`;
  return `${sign}\u20B9${Math.round(abs)}`;
}
import {
  DAILY_EXPENDITURE,
  DAILY_QUANTITY,
  EXPENDITURE_BY_TYPE,
  FEED_DISTRIBUTION,
  AGE_DISTRIBUTION,
} from "@/lib/data/feed";
import type { FeedLoadRecord, SeasonRecord, FeedStockSummary, FeedLoadSummaryRecord } from "@/lib/types";
import { Package, AlertTriangle, TrendingDown, ChevronDown, ChevronUp } from "lucide-react";
import { getChartDescription } from "@/lib/display-utils";

const ALLOWED_STOCK_FEEDS = ["masoor dhal bhusa", "concentrate", "vgoats grain mix", "baking soda", "uht milk", "toor dal bhusa pellet"];

type Tab = "consumption" | "consumption-table" | "stock" | "nutrition" | "seasons";
type Farm = "CBE" | "CPT";
type FeedComparisonFarm = "Total" | "CBE" | "CPT";
type NutritionView = "breedwise" | "housingwise";

type MonthlyAnimalsVsFeedRow = {
  month: string;
  monthLabel: string;
  farm: string;
  avgAnimalCount: number;
  feedSpend: number;
};

const TABS: { id: Tab; label: string }[] = [
  { id: "consumption", label: "Consumption Charts" },
  { id: "consumption-table", label: "Consumption Table" },
  { id: "stock", label: "Stock" },
  { id: "nutrition", label: "Nutrition" },
  { id: "seasons", label: "Seasons" },
];

const FEED_TYPE_COLORS: Record<string, string> = {
  "Masoor Dhal Bhusa": "#14F1D9",
  "Concentrate": "#C6FF00",
  "Vgoats Grain Mix": "#14D4E8",
  "UHT Milk": "#FF6B35",
  "Baking Soda": "#1AF0B0",
  "Toor Dal Bhusa Pellet": "#FF9F40",
};

/** Title-case a breed name from BigQuery (e.g. "BEETAL" → "Beetal") */
function titleCase(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1).toLowerCase();
}

/** Sheds that should be split into numbered sub-entries (e.g. "Mandela 1", "Mandela 2") */
const SPLIT_SHEDS = ["Mandela", "Godel", "Sumathi"];

/** Extract shed toggle labels from raw shed names, sorted numerically */
function deriveShedTabs(rawSheds: string[]): string[] {
  const prefixes = new Set<string>();
  for (const s of rawSheds) {
    // For split sheds, extract "Mandela 1" from "Mandela 1 - Part 3"
    const splitMatch = SPLIT_SHEDS.find((p) => s.startsWith(p));
    if (splitMatch) {
      const m = s.match(new RegExp("^(" + splitMatch + "\\s+\\d+)"));
      if (m) prefixes.add(m[1]);
    } else {
      // For others like "Gandhi 1 - Part 2" → "Gandhi", "Castro 2" → "Castro", "Q1" → "Quarantine"
      const baseMatch = s.match(/^(Q)\d/);
      if (baseMatch) {
        prefixes.add("Quarantine");
      } else {
        const base = s.match(/^([A-Za-z][A-Za-z ]*[A-Za-z])/);
        if (base) prefixes.add(base[1].replace(/\s+$/, ""));
      }
    }
  }
  // Sort: extract all numbers for numeric comparison
  return Array.from(prefixes).sort((a, b) => {
    const numsA = a.match(/\d+/g)?.map(Number) ?? [];
    const numsB = b.match(/\d+/g)?.map(Number) ?? [];
    for (let i = 0; i < Math.max(numsA.length, numsB.length); i++) {
      const diff = (numsA[i] ?? 0) - (numsB[i] ?? 0);
      if (diff !== 0) return diff;
    }
    return a.localeCompare(b);
  });
}

export default function FeedPage() {
  const [activeTab, setActiveTab] = useState<Tab>("consumption");
  const [farm, setFarm] = useState<Farm>("CBE");
  const [showStockCards, setShowStockCards] = useState(true);
  const [nutritionView, setNutritionView] = useState<NutritionView>("housingwise");
  const [nutritionFarm, setNutritionFarm] = useState<Farm>("CBE");
  const [selectedBreed, setSelectedBreed] = useState("Beetal");
  const [selectedShed, setSelectedShed] = useState("Gandhi");
  const [concentrateFarm, setConcentrateFarm] = useState<"Total" | "CBE" | "CPT">("Total");
  const [masoorFarm, setMasoorFarm] = useState<"Total" | "CBE" | "CPT">("Total");
  const [uhtMilkFarm, setUhtMilkFarm] = useState<"Total" | "CBE" | "CPT">("Total");
  const [monthlyAnimalsVsFeedFarm, setMonthlyAnimalsVsFeedFarm] = useState<FeedComparisonFarm>("Total");

  // ── Available breeds & sheds for current farm ──
  const { data: availableBreedTabs } = useQuery<string[]>({
    queryKey: ["breed-tabs", nutritionFarm],
    queryFn: async () => {
      const res = await fetch(`/api/feed/nutrition-breed?list=breeds&farm=${nutritionFarm}`);
      if (!res.ok) return [];
      const json = await res.json();
      return json.breeds?.length ? json.breeds.map((b: string) => titleCase(b)).sort() : [];
    },
    staleTime: 15 * 60 * 1000,
  });
  const breedTabs = availableBreedTabs ?? [];

  const { data: availableShedTabs } = useQuery<string[]>({
    queryKey: ["shed-tabs", nutritionFarm],
    queryFn: async () => {
      const res = await fetch(`/api/feed/nutrition-housing?list=sheds&farm=${nutritionFarm}`);
      if (!res.ok) return [];
      const json = await res.json();
      return json.sheds?.length ? deriveShedTabs(json.sheds) : [];
    },
    staleTime: 15 * 60 * 1000,
  });
  const shedTabs = availableShedTabs ?? [];

  // ── Consumption data (live from BigQuery APIs) ──
  const { data: spendData } = useQuery<any[]>({
    queryKey: ["feed-spend"],
    queryFn: async () => {
      const res = await fetch("/api/feed/spend");
      if (!res.ok) return DAILY_EXPENDITURE;
      const json = await res.json();
      return json.data?.length ? json.data : DAILY_EXPENDITURE;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: quantityData } = useQuery<any[]>({
    queryKey: ["feed-consumption"],
    queryFn: async () => {
      const res = await fetch("/api/feed/consumption");
      if (!res.ok) return DAILY_QUANTITY;
      const json = await res.json();
      return json.data?.length ? json.data : DAILY_QUANTITY;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: breakdownData } = useQuery<any[]>({
    queryKey: ["feed-breakdown"],
    queryFn: async () => {
      const res = await fetch("/api/feed/breakdown");
      if (!res.ok) return null;
      const json = await res.json();
      return json.data?.length ? json.data : null;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: distributionData } = useQuery<{ name: string; value: number }[]>({
    queryKey: ["feed-distribution"],
    queryFn: async () => {
      const res = await fetch("/api/feed/distribution");
      if (!res.ok) return FEED_DISTRIBUTION;
      const json = await res.json();
      return json.data?.length ? json.data : FEED_DISTRIBUTION;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: ageData } = useQuery<any[]>({
    queryKey: ["feed-age"],
    queryFn: async () => {
      const res = await fetch("/api/feed/age");
      if (!res.ok) return AGE_DISTRIBUTION;
      const json = await res.json();
      return json.data?.length ? json.data : AGE_DISTRIBUTION;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: concentrateRaw } = useQuery<any[]>({
    queryKey: ["feeddb-concentrate"],
    queryFn: async () => {
      const res = await fetch("/api/feed/feeddb-consumption?feed=Concentrate");
      if (!res.ok) return [];
      const json = await res.json();
      return json.data ?? [];
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: masoorRaw } = useQuery<any[]>({
    queryKey: ["feeddb-masoor"],
    queryFn: async () => {
      const res = await fetch("/api/feed/feeddb-consumption?feed=Masoor Dhal Bhusa");
      if (!res.ok) return [];
      const json = await res.json();
      return json.data ?? [];
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: uhtMilkRaw } = useQuery<any[]>({
    queryKey: ["feeddb-uht-milk"],
    queryFn: async () => {
      const res = await fetch("/api/feed/feeddb-consumption?feed=UHT Milk");
      if (!res.ok) return [];
      const json = await res.json();
      return json.data ?? [];
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: monthlyAnimalsVsFeedRaw } = useQuery<MonthlyAnimalsVsFeedRow[]>({
    queryKey: ["monthly-animals-vs-feed"],
    queryFn: async () => {
      const res = await fetch("/api/feed/monthly-animals-vs-feed");
      if (!res.ok) return [];
      const json = await res.json();
      return json.data ?? [];
    },
    staleTime: 15 * 60 * 1000,
  });

  // Filter to last 30 days excluding today (for spend chart)
  const last30DaysFilter = useMemo(() => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const cutoff = new Date(today);
    cutoff.setDate(cutoff.getDate() - 30);
    return { today, cutoff };
  }, []);

  // Filter to last 8 months excluding today (for breakdown/quantity charts)
  const last8MonthsFilter = useMemo(() => {
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const cutoff = new Date(today);
    cutoff.setMonth(cutoff.getMonth() - 8);
    return { today, cutoff };
  }, []);

  const chartSpend = useMemo(() => {
    const raw = spendData ?? DAILY_EXPENDITURE;
    const { today, cutoff } = last30DaysFilter;
    return raw
      .map((r: any) => ({
        date: String(r.date || ""),
        value: Number(r.total_spend ?? r.value ?? 0),
      }))
      .filter((r: any) => {
        if (!r.date) return false;
        const d = new Date(r.date);
        return !isNaN(d.getTime()) && d >= cutoff && d < today;
      });
  }, [spendData, last30DaysFilter]);

  const chartQuantity = useMemo(() => {
    const raw = quantityData ?? DAILY_QUANTITY;
    const { today, cutoff } = last8MonthsFilter;
    return raw
      .map((r: any) => ({
        date: String(r.date || ""),
        value: Number(r.value ?? 0),
      }))
      .filter((r: any) => {
        if (!r.date) return false;
        const d = new Date(r.date);
        return !isNaN(d.getTime()) && d >= cutoff && d < today;
      });
  }, [quantityData, last8MonthsFilter]);

  // Pivot breakdown data for line chart (rows per date with feed types as keys)
  const chartBreakdown = useMemo(() => {
    if (!breakdownData) return EXPENDITURE_BY_TYPE;
    const { today, cutoff } = last8MonthsFilter;
    const dateMap = new Map<string, Record<string, number>>();
    for (const r of breakdownData) {
      const dateStr = String(r.date || "");
      if (!dateStr) continue;
      const d = new Date(dateStr);
      if (isNaN(d.getTime()) || d < cutoff || d >= today) continue;
      if (!dateMap.has(dateStr)) dateMap.set(dateStr, {});
      const row = dateMap.get(dateStr)!;
      row[r.feed_type] = (row[r.feed_type] ?? 0) + Number(r.total_spend ?? 0);
    }
    return Array.from(dateMap.entries())
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, values]) => ({ date, ...values }));
  }, [breakdownData, last8MonthsFilter]);

  // Feeds active in last 30 days (used to filter both line chart and pie)
  const last30DayFeeds = useMemo(() => {
    if (!breakdownData) return new Set<string>();
    const cutoff30 = new Date();
    cutoff30.setHours(0, 0, 0, 0);
    cutoff30.setDate(cutoff30.getDate() - 30);
    const feeds = new Set<string>();
    for (const r of breakdownData) {
      const d = new Date(String(r.date || ""));
      if (!isNaN(d.getTime()) && d >= cutoff30) {
        feeds.add(r.feed_type);
      }
    }
    return feeds;
  }, [breakdownData]);

  // Detect feed type keys — only feeds consumed in last 30 days
  const breakdownFeedTypes = useMemo(() => {
    if (!last30DayFeeds.size) return ["Masoor Dhal Bhusa", "Concentrate", "Vgoats Grain Mix", "UHT Milk"];
    return Array.from(last30DayFeeds);
  }, [last30DayFeeds]);

  // Feed distribution pie — from feedDB_clean, last 30 days excl. today
  const feedDistribution = distributionData ?? FEED_DISTRIBUTION;

  // Age distribution from API
  const ageDistribution = useMemo(() => {
    const raw = ageData ?? AGE_DISTRIBUTION;
    if (raw.length && raw[0].breed_age != null) {
      return raw.map((r: any) => ({ name: r.breed_age, value: Number(r.feed_amount ?? 0) }));
    }
    return raw;
  }, [ageData]);

  /** Pivot feedDB_clean rows into { date, CBE, CPT, Total }[] for the given raw data */
  function pivotFeedDB(raw: any[] | undefined): { date: string; CBE: number; CPT: number; Total: number }[] {
    if (!raw || raw.length === 0) return [];
    const dateMap = new Map<string, { CBE: number; CPT: number }>();
    for (const r of raw) {
      const date = String(r.date || "");
      if (!date) continue;
      if (!dateMap.has(date)) dateMap.set(date, { CBE: 0, CPT: 0 });
      const row = dateMap.get(date)!;
      const farm = String(r.farm || "").toUpperCase();
      const val = Number(r.value || 0);
      if (farm === "CBE") row.CBE += val;
      else if (farm === "CPT") row.CPT += val;
    }
    return Array.from(dateMap.entries())
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([date, { CBE, CPT }]) => ({ date, CBE, CPT, Total: CBE + CPT }));
  }

  const chartConcentrate = useMemo(() => pivotFeedDB(concentrateRaw), [concentrateRaw]);
  const chartMasoor = useMemo(() => pivotFeedDB(masoorRaw), [masoorRaw]);
  const chartUhtMilk = useMemo(() => pivotFeedDB(uhtMilkRaw), [uhtMilkRaw]);
  const monthlyAnimalsVsFeedData = useMemo(() => {
    const farmKey = monthlyAnimalsVsFeedFarm === "Total" ? "TOTAL" : monthlyAnimalsVsFeedFarm;
    return (monthlyAnimalsVsFeedRaw ?? [])
      .filter((r) => String(r.farm || "").toUpperCase() === farmKey)
      .sort((a, b) => String(a.month).localeCompare(String(b.month)))
      .map((r) => ({
        month: r.monthLabel || r.month,
        avgAnimalCount: Number(r.avgAnimalCount ?? 0),
        feedSpend: Number(r.feedSpend ?? 0),
      }));
  }, [monthlyAnimalsVsFeedRaw, monthlyAnimalsVsFeedFarm]);

  // ── Load summary data (for Consumption Table tab) ──
  const { data: loadSummaryData, isLoading: loadSummaryLoading, error: loadSummaryError } = useQuery<FeedLoadSummaryRecord[]>({
    queryKey: ["feed-load-summary"],
    queryFn: async () => {
      const res = await fetch("/api/feed/load-summary");
      if (!res.ok) return [];
      const json = await res.json();
      return json.data ?? [];
    },
    staleTime: 15 * 60 * 1000,
  });

  // ── Stock data ──
  const { data: stockData, isLoading: stockLoading, error: stockError, refetch: refetchStock } = useQuery<FeedLoadRecord[]>({
    queryKey: ["feed-stock", farm],
    queryFn: async () => {
      const res = await fetch(`/api/feed?farm=${farm}`);
      if (!res.ok) throw new Error("Failed to fetch feed stock");
      const json = await res.json();
      return json.data;
    },
    staleTime: 15 * 60 * 1000,
  });

  const filteredStockData = useMemo(() => {
    if (!stockData) return [];
    return stockData.filter((r: FeedLoadRecord) => ALLOWED_STOCK_FEEDS.includes(r.load_type.toLowerCase()));
  }, [stockData]);

  const stockSummaries = useMemo<FeedStockSummary[]>(() => {
    if (!filteredStockData.length) return [];
    const grouped = new Map<string, FeedLoadRecord[]>();
    for (const rec of filteredStockData) {
      if (!grouped.has(rec.load_type)) grouped.set(rec.load_type, []);
      grouped.get(rec.load_type)!.push(rec);
    }

    return Array.from(grouped.entries()).map(([loadType, records]) => {
      // Sort by purchase_date desc to find latest (most recent first)
      const sorted = [...records].sort((a, b) =>
        String(b.purchase_date).localeCompare(String(a.purchase_date))
      );
      const mostRecent = sorted[0];
      const activeLoad = sorted.find(r => r.consumption_per_day > 0 || r.days_consumed_so_far > 0);
      // Total days left across all loads that have stock remaining
      const daysLeft = records.reduce((s, r) => s + (r.days_stock_can_last > 0 ? r.days_stock_can_last : 0), 0);
      const currentStock = records.reduce((s, r) => s + r.current_stock, 0);
      const consumptionStarted = records.some(r => r.consumption_per_day > 0 || r.days_consumed_so_far > 0 || r.days_stock_can_last > 0);
      const loadId = mostRecent.load_id;
      const totalCost = records.reduce((s, r) => s + r.last_load_total_cost, 0);
      const loadCount = records.length;
      const pendingAmount = records.reduce((s, r) => s + r.pending_amount, 0);

      // If a newer unused load exists beyond the active one, stock is secured
      const hasNextLoad = activeLoad ? sorted.some(r => r !== activeLoad && (r.consumption_per_day === 0 && r.days_consumed_so_far === 0 && r.current_stock > 0)) : false;
      const health: FeedStockSummary["health"] =
        !consumptionStarted ? "stocked" : hasNextLoad ? "stocked" : daysLeft > 15 ? "stocked" : daysLeft >= 5 ? "low" : "out";

      return { loadType, currentStock, daysLeft, totalCost, loadCount, pendingAmount, health, loadId, consumptionStarted, hasNextLoad };
    });
  }, [filteredStockData]);

  // ── Nutrition data (live from BigQuery) ──
  const nutritionEndpoint = nutritionView === "breedwise" ? "nutrition-breed" : "nutrition-housing";

  /** Build nutrition query params for a given feed_type (or none for total) */
  function buildNutritionUrl(feedType?: string) {
    const p = new URLSearchParams({ farm: nutritionFarm });
    if (nutritionView === "breedwise") p.set("breed", selectedBreed);
    if (nutritionView === "housingwise") p.set("shed", selectedShed === "Quarantine" ? "Q" : selectedShed);
    if (feedType) p.set("feed_type", feedType);
    return `/api/feed/${nutritionEndpoint}?${p}`;
  }

  const { data: nutritionTotal } = useQuery<any[]>({
    queryKey: ["nutrition-total", nutritionView, nutritionFarm, selectedBreed, selectedShed],
    queryFn: async () => {
      const res = await fetch(buildNutritionUrl());
      if (!res.ok) return null;
      const json = await res.json();
      return json.data?.length ? json.data : null;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: nutritionConcentrate } = useQuery<any[]>({
    queryKey: ["nutrition-concentrate", nutritionView, nutritionFarm, selectedBreed, selectedShed],
    queryFn: async () => {
      const res = await fetch(buildNutritionUrl("Concentrate"));
      if (!res.ok) return null;
      const json = await res.json();
      return json.data?.length ? json.data : null;
    },
    staleTime: 15 * 60 * 1000,
  });

  const { data: nutritionMasoor } = useQuery<any[]>({
    queryKey: ["nutrition-masoor", nutritionView, nutritionFarm, selectedBreed, selectedShed],
    queryFn: async () => {
      const res = await fetch(buildNutritionUrl("Dry Masoor Bhusa"));
      if (!res.ok) return null;
      const json = await res.json();
      return json.data?.length ? json.data : null;
    },
    staleTime: 15 * 60 * 1000,
  });

  /** Pivot nutrition API response into GroupedBarChart format */
  function pivotNutrition(raw: any[] | null | undefined): { data: any[]; keys: string[] } {
    if (!raw || raw.length === 0) return { data: [], keys: [] };
    const isHousing = nutritionView === "housingwise";
    const nameKey = isHousing ? "housing_unit" : "status";

    // For housingwise, build composite key "shed\n(shed_tag)" to show tag on x-axis
    function getGroupKey(r: any): string {
      if (isHousing) {
        const shed = r.housing_unit || "";
        const tag = r.shed_tag || "";
        return tag ? `${shed}\n(${tag})` : shed;
      }
      return r[nameKey] || "";
    }

    // Collect unique dates and group keys
    const dates = new Set<string>();
    const names = new Set<string>();
    for (const r of raw) {
      const d = r.date ? String(r.date).split("T")[0] : "";
      if (d) dates.add(d);
      const key = getGroupKey(r);
      if (key) names.add(key);
    }
    const sortedDates = Array.from(dates).sort();
    const MONTHS = ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"];
    const dateLabels = sortedDates.map((d) => {
      const parts = d.split("-");
      return `${parseInt(parts[2])} ${MONTHS[parseInt(parts[1]) - 1]} ${parts[0]}`;
    });
    // Sort names numerically (e.g., Part 1, Part 2, ... Part 10)
    const sortedNames = Array.from(names).sort((a, b) => {
      const numsA = a.match(/\d+/g)?.map(Number) ?? [];
      const numsB = b.match(/\d+/g)?.map(Number) ?? [];
      for (let i = 0; i < Math.max(numsA.length, numsB.length); i++) {
        const diff = (numsA[i] ?? 0) - (numsB[i] ?? 0);
        if (diff !== 0) return diff;
      }
      return a.localeCompare(b);
    });
    // Build rows: { name: group key, "label": summed value, ... }
    const rows = sortedNames.map((name) => {
      const row: any = { name };
      for (let i = 0; i < sortedDates.length; i++) {
        const matches = raw.filter((r: any) => getGroupKey(r) === name && String(r.date).split("T")[0] === sortedDates[i]);
        row[dateLabels[i]] = Math.round(matches.reduce((sum: number, r: any) => sum + Number(r.feed_per_animal ?? 0), 0));
      }
      return row;
    });
    return { data: rows, keys: dateLabels };
  }

  const pivotedTotal = useMemo(() => pivotNutrition(nutritionTotal), [pivotNutrition, nutritionTotal]);
  const pivotedConcentrate = useMemo(() => pivotNutrition(nutritionConcentrate), [pivotNutrition, nutritionConcentrate]);
  const pivotedMasoor = useMemo(() => pivotNutrition(nutritionMasoor), [pivotNutrition, nutritionMasoor]);

  // ── Seasons data ──
  const { data: seasonsData } = useQuery<SeasonRecord[]>({
    queryKey: ["seasons"],
    queryFn: async () => {
      const res = await fetch("/api/feed?type=seasons");
      if (!res.ok) throw new Error("Failed to fetch seasons");
      const json = await res.json();
      return json.data;
    },
    staleTime: 15 * 60 * 1000,
  });

  const ganttRows = useMemo(() => {
    if (!seasonsData) return [];
    return seasonsData.map((rec: any) => {
      const startStr = rec.Start_Date || rec.start_date || "";
      const endStr = rec.End_Date || rec.end_date || "";
      const startDate = new Date(String(startStr));
      const endDate = new Date(String(endStr));
      return {
        region: rec.Region || rec.region || "",
        crop: rec.Crop || rec.crop || "",
        startMonth: startDate.getMonth(),
        endMonth: endDate.getMonth(),
      };
    });
  }, [seasonsData]);

  const nutritionSubtitle = useMemo(() => {
    const selection = nutritionView === "breedwise" ? selectedBreed : selectedShed;
    return `${selection}, ${nutritionFarm}`;
  }, [nutritionView, selectedBreed, selectedShed, nutritionFarm]);

  return (
    <div className="space-y-6">
      {/* Tab row */}
      <div className="flex flex-wrap items-center gap-1">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            onClick={() => setActiveTab(tab.id)}
            className={
              activeTab === tab.id
                ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
            }
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* ── Consumption tab ── */}
      {activeTab === "consumption" && (
        <div className="space-y-4">
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ChartCard title="Daily Feed Expenditure" subtitle={getChartDescription("Daily Feed Expenditure")}>
              <ResponsiveContainer width="100%" height={280}>
                <BarChart data={chartSpend} margin={{ top: 20, right: 16, bottom: 4, left: 0 }}>
                  <XAxis dataKey="date" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={Math.max(0, Math.floor((chartSpend?.length ?? 0) / 8))} angle={-45} textAnchor="end" height={50} />
                  <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={(v) => `₹${formatNumber(v)}`} />
                  <Tooltip
                    contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: "#14F1D9" }}
                    itemStyle={{ color: "#FFFFFF" }}
                    formatter={(value: unknown) => [`₹${formatNumber(Number(value ?? 0))}`, "Total cost"]}
                  />
                  <Legend wrapperStyle={{ fontSize: 12, color: TEXT_MUTED }} />
                  <Bar dataKey="value" name="Total cost" fill="#14F1D9" animationDuration={800} animationEasing="ease-out" />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>

            <ChartCard title="Daily Feed Quantity" subtitle={getChartDescription("Daily Feed Quantity")}>
              <ResponsiveContainer width="100%" height={280}>
                <LineChart data={chartQuantity} margin={{ top: 20, right: 16, bottom: 4, left: 0 }}>
                  <XAxis dataKey="date" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={Math.max(0, Math.floor((chartQuantity?.length ?? 0) / 8))} angle={-45} textAnchor="end" height={50} />
                  <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={(v) => `${formatNumber(v)} kg`} />
                  <Tooltip
                    contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: "#14F1D9" }}
                    itemStyle={{ color: "#FFFFFF" }}
                    formatter={(value: unknown) => [`${formatNumber(Number(value ?? 0))} kg`, "Quantity"]}
                  />
                  <Legend wrapperStyle={{ fontSize: 12, color: TEXT_MUTED }} />
                  <Line type="monotone" dataKey="value" name="Quantity (kg)" stroke="#14F1D9" strokeWidth={1.5} dot={false} animationDuration={800} animationEasing="ease-out" />
                </LineChart>
              </ResponsiveContainer>
            </ChartCard>
          </div>

          <ChartCard title="Monthly Animal Count vs Feed Expenditure" subtitle="Average monthly count and feed expenditure">
            <div className="flex items-center gap-1 mb-3">
              {(["Total", "CBE", "CPT"] as const).map((f) => (
                <button
                  key={f}
                  onClick={() => setMonthlyAnimalsVsFeedFarm(f)}
                  className={
                    monthlyAnimalsVsFeedFarm === f
                      ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1 text-xs font-medium"
                      : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1 text-xs"
                  }
                >
                  {f}
                </button>
              ))}
            </div>
            <ResponsiveContainer width="100%" height={320}>
              <BarChart data={monthlyAnimalsVsFeedData} margin={{ top: 28, right: 16, bottom: 4, left: 0 }}>
                <XAxis dataKey="month" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={0} angle={-45} textAnchor="end" height={60} />
                <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={(v) => formatNumber(Number(v))} />
                <Tooltip
                  contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                  labelStyle={{ color: "#14F1D9" }}
                  itemStyle={{ color: "#FFFFFF" }}
                  formatter={(value: unknown, name: unknown) =>
                    name === "Feed Expenditure"
                      ? [formatINRCompactOneDecimal(Number(value ?? 0)), "Feed Expenditure"]
                      : [formatNumber(Math.round(Number(value ?? 0))), "Avg animal count"]
                  }
                />
                <Legend verticalAlign="top" wrapperStyle={{ fontSize: 12, paddingBottom: 8, color: TEXT_MUTED }} />
                <Bar dataKey="avgAnimalCount" name="Avg Animal Count" fill="#14F1D9" animationDuration={800} animationEasing="ease-out">
                  <LabelList
                    dataKey="feedSpend"
                    position="top"
                    fontSize={10}
                    fill="#E0E8F0"
                    formatter={(value: unknown) => formatINRCompactOneDecimal(Number(value ?? 0))}
                  />
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </ChartCard>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ChartCard title="Daily Consumption of Concentrate" subtitle="Last 30 days">
              <div className="flex items-center gap-1 mb-3">
                {(["Total", "CBE", "CPT"] as const).map((f) => (
                  <button
                    key={f}
                    onClick={() => setConcentrateFarm(f)}
                    className={
                      concentrateFarm === f
                        ? "bg-[#22262E] text-[#C6FF00] rounded-lg px-3 py-1 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#C6FF00] px-3 py-1 text-xs"
                    }
                  >
                    {f}
                  </button>
                ))}
              </div>
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={chartConcentrate} margin={{ top: 20, right: 16, bottom: 4, left: 0 }}>
                  <XAxis dataKey="date" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={Math.max(0, Math.floor((chartConcentrate?.length ?? 0) / 8))} angle={-45} textAnchor="end" height={50} />
                  <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={(v) => `${formatNumber(v)} kg`} />
                  <Tooltip
                    contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: "#C6FF00" }}
                    itemStyle={{ color: "#FFFFFF" }}
                    formatter={(value: unknown) => [`${fmt2(Number(value ?? 0))} kg`, concentrateFarm]}
                  />
                  <Bar dataKey={concentrateFarm} fill="#C6FF00" animationDuration={800} animationEasing="ease-out" />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>

            <ChartCard title="Daily Consumption of Masoor Dhal Bhusa" subtitle="Last 30 days">
              <div className="flex items-center gap-1 mb-3">
                {(["Total", "CBE", "CPT"] as const).map((f) => (
                  <button
                    key={f}
                    onClick={() => setMasoorFarm(f)}
                    className={
                      masoorFarm === f
                        ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1 text-xs"
                    }
                  >
                    {f}
                  </button>
                ))}
              </div>
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={chartMasoor} margin={{ top: 20, right: 16, bottom: 4, left: 0 }}>
                  <XAxis dataKey="date" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={Math.max(0, Math.floor((chartMasoor?.length ?? 0) / 8))} angle={-45} textAnchor="end" height={50} />
                  <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={(v) => `${formatNumber(v)} kg`} />
                  <Tooltip
                    contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: "#14F1D9" }}
                    itemStyle={{ color: "#FFFFFF" }}
                    formatter={(value: unknown) => [`${fmt2(Number(value ?? 0))} kg`, masoorFarm]}
                  />
                  <Bar dataKey={masoorFarm} fill="#14F1D9" animationDuration={800} animationEasing="ease-out" />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ChartCard title="Daily Consumption of UHT Milk" subtitle="Last 30 days">
              <div className="flex items-center gap-1 mb-3">
                {(["Total", "CBE", "CPT"] as const).map((f) => (
                  <button
                    key={f}
                    onClick={() => setUhtMilkFarm(f)}
                    className={
                      uhtMilkFarm === f
                        ? "bg-[#22262E] text-[#FF6B35] rounded-lg px-3 py-1 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#FF6B35] px-3 py-1 text-xs"
                    }
                  >
                    {f}
                  </button>
                ))}
              </div>
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={chartUhtMilk} margin={{ top: 20, right: 16, bottom: 4, left: 0 }}>
                  <XAxis dataKey="date" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={Math.max(0, Math.floor((chartUhtMilk?.length ?? 0) / 8))} angle={-45} textAnchor="end" height={50} />
                  <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={(v) => `${formatNumber(v)} L`} />
                  <Tooltip
                    contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: "#FF6B35" }}
                    itemStyle={{ color: "#FFFFFF" }}
                    formatter={(value: unknown) => [`${fmt2(Number(value ?? 0))} L`, uhtMilkFarm]}
                  />
                  <Bar dataKey={uhtMilkFarm} fill="#FF6B35" animationDuration={800} animationEasing="ease-out" />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>
          </div>

          <ChartCard title="Expenditure by Feed Type" subtitle={getChartDescription("Expenditure by Feed Type")}>
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={chartBreakdown} margin={{ top: 20, right: 16, bottom: 4, left: 0 }}>
                <XAxis dataKey="date" axisLine={false} tickLine={false} tick={{ fontSize: 10, fill: TEXT_MUTED }} interval={Math.max(0, Math.floor((chartBreakdown?.length ?? 0) / 12))} angle={-45} textAnchor="end" height={50} />
                <YAxis axisLine={false} tickLine={false} tick={{ fontSize: 11, fill: TEXT_TERTIARY }} tickFormatter={fmt2} />
                <Tooltip
                  contentStyle={{ backgroundColor: "#1A1D24", border: "1px solid #334155", borderRadius: 8, fontSize: 12 }}
                  labelStyle={{ color: "#14F1D9" }}
                  itemStyle={{ color: "#FFFFFF" }}
                  formatter={tooltipFmt2}
                />
                <Legend verticalAlign="top" wrapperStyle={{ fontSize: 12, paddingBottom: 8, color: TEXT_MUTED }} />
                {breakdownFeedTypes.filter((type) => ["Concentrate", "Masoor Dhal Bhusa", "UHT Milk", "Toor Dal Bhusa Pellet"].includes(type)).map((type) => (
                  <Bar
                    key={type}
                    dataKey={type}
                    fill={FEED_TYPE_COLORS[type] || "#8899AA"}
                    animationDuration={800}
                    animationEasing="ease-out"
                  />
                ))}
              </BarChart>
            </ResponsiveContainer>
          </ChartCard>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ChartCard title="Feed Distribution" subtitle={getChartDescription("Feed Distribution")}>
              <VPieChart data={feedDistribution} />
            </ChartCard>
            <ChartCard title="Age Distribution" subtitle={getChartDescription("Age Distribution")}>
              <VPieChart data={ageDistribution} />
            </ChartCard>
          </div>
        </div>
      )}

      {/* ── Consumption Table tab ── */}
      {activeTab === "consumption-table" && (
        <div className="space-y-4">
          {loadSummaryLoading && (
            <div className="flex items-center justify-center py-12 text-xs text-dim">
              Loading consumption table…
            </div>
          )}
          {loadSummaryError && (
            <div className="flex items-center justify-center py-12 text-xs text-red-400">
              Failed to load consumption data
            </div>
          )}
          {!loadSummaryLoading && !loadSummaryError && (
            <FeedLoadSummaryTable
              records={loadSummaryData ?? []}
              tableClassName="max-h-[calc(100dvh-240px)]"
            />
          )}
        </div>
      )}

      {/* ── Stock tab ── */}
      {activeTab === "stock" && (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-1">
              {(["CBE", "CPT"] as Farm[]).map((f) => (
                <button
                  key={f}
                  onClick={() => setFarm(f)}
                  className={
                    farm === f
                      ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                      : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                  }
                >
                  {f}
                </button>
              ))}
            </div>
            <button
              onClick={() => setShowStockCards((v) => !v)}
              className="flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-[11px] text-[#8899AA] hover:text-[#B0BEC5] hover:bg-[#22262E] transition-colors"
            >
              {showStockCards ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
              {showStockCards ? "Hide cards" : "Show cards"}
            </button>
          </div>

          {stockLoading && <LoadingState />}
          {stockError && <ErrorState message="Failed to load stock data" onRetry={() => refetchStock()} />}

          {!stockLoading && !stockError && (
            <>
              {showStockCards && (
                <div className="grid grid-cols-2 lg:grid-cols-5 gap-4">
                  {stockSummaries.map((summary, i) => (
                    <KPICard
                      key={summary.loadType}
                      label={summary.loadType}
                      value={summary.consumptionStarted ? `${summary.daysLeft} days left` : "Not Started"}
                      subtitle={`Load #${summary.loadId} · ${formatNumber(Math.round(summary.currentStock))} kg${summary.consumptionStarted && summary.daysLeft < 5 && !summary.hasNextLoad ? " · ⚠ Low Stock" : ""}`}
                      delay={i}
                      icon={
                        summary.health === "stocked" ? (
                          <Package className="h-4 w-4" />
                        ) : summary.health === "low" ? (
                          <AlertTriangle className="h-4 w-4" />
                        ) : (
                          <TrendingDown className="h-4 w-4" />
                        )
                      }
                    />
                  ))}
                </div>
              )}
              {filteredStockData.length > 0 && (
                <FeedStockTable
                  records={filteredStockData}
                  tableClassName={showStockCards ? "max-h-[calc(100dvh-380px)]" : "max-h-[calc(100dvh-220px)]"}
                />
              )}
            </>
          )}
        </div>
      )}

      {/* ── Nutrition tab ── */}
      {activeTab === "nutrition" && (
        <div className="space-y-4">
          <div className="flex items-center gap-1">
            {(["housingwise", "breedwise"] as NutritionView[]).map((v) => (
              <button
                key={v}
                onClick={() => setNutritionView(v)}
                className={
                  nutritionView === v
                    ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                    : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                }
              >
                {v === "breedwise" ? "Breedwise" : "Housingwise"}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-1">
            {(["CBE", "CPT"] as Farm[]).map((f) => (
              <button
                key={f}
                onClick={() => setNutritionFarm(f)}
                className={
                  nutritionFarm === f
                    ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                    : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                }
              >
                {f}
              </button>
            ))}
          </div>

          <div className="flex items-center gap-1 flex-wrap">
            {nutritionView === "breedwise"
              ? breedTabs.map((breed: string) => (
                  <button
                    key={breed}
                    onClick={() => setSelectedBreed(breed)}
                    className={
                      selectedBreed === breed
                        ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                    }
                  >
                    {breed}
                  </button>
                ))
              : shedTabs.map((shed: string) => (
                  <button
                    key={shed}
                    onClick={() => setSelectedShed(shed)}
                    className={
                      selectedShed === shed
                        ? "bg-[#22262E] text-[#14F1D9] rounded-lg px-3 py-1.5 text-xs font-medium"
                        : "text-[#8899AA] hover:text-[#14F1D9] px-3 py-1.5 text-xs"
                    }
                  >
                    {shed}
                  </button>
                ))}
          </div>

          <div className="space-y-4">
            <ChartCard title="Total Feed per Animal" subtitle={nutritionSubtitle}>
              <GroupedBarChart data={pivotedTotal.data} keys={pivotedTotal.keys} valueSuffix="g" />
            </ChartCard>
            <ChartCard title="Concentrate per Animal" subtitle={nutritionSubtitle}>
              <GroupedBarChart data={pivotedConcentrate.data} keys={pivotedConcentrate.keys} valueSuffix="g" />
            </ChartCard>
            <ChartCard title="Masoor Bhusa per Animal" subtitle={nutritionSubtitle}>
              <GroupedBarChart data={pivotedMasoor.data} keys={pivotedMasoor.keys} valueSuffix="g" />
            </ChartCard>
          </div>
        </div>
      )}

      {/* ── Seasons tab ── */}
      {activeTab === "seasons" && (
        <div className="space-y-4">
          <ChartCard title="Feed Seasons Calendar" subtitle={getChartDescription("Feed Seasons Calendar")}>
            {ganttRows.length > 0 ? (
              <GanttChart data={ganttRows} />
            ) : (
              <div className="flex h-[200px] items-center justify-center text-xs text-[#8899AA]">
                Loading season data...
              </div>
            )}
          </ChartCard>
        </div>
      )}
    </div>
  );
}
