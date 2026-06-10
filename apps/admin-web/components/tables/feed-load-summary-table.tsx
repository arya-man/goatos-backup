"use client";

import { Fragment, useMemo, useState } from "react";
import { cn } from "@/lib/utils";
import { AlertTriangle } from "lucide-react";
import type { FeedLoadSummaryRecord } from "@/lib/types";

interface FeedLoadSummaryTableProps {
  records: FeedLoadSummaryRecord[];
  tableClassName?: string;
}

function fmtDate(d: unknown): string {
  const s = String(d || "");
  if (!s || s === "undefined" || s === "null" || s === "") return "—";
  const dt = new Date(s);
  if (isNaN(dt.getTime())) return s;
  const dd = String(dt.getDate()).padStart(2, "0");
  const mm = String(dt.getMonth() + 1).padStart(2, "0");
  const yyyy = dt.getFullYear();
  return `${dd}-${mm}-${yyyy}`;
}

/**
 * Mismatch: purchased_qty / avg_daily != days_used + days_left
 * i.e. the load duration implied by purchase quantity doesn't match
 * how long the load has actually been/will be used.
 */
// Only flags red when the load is fully consumed and actual days fall SHORT of expected.
// Active loads (stock still available) are excluded — remaining days haven't finalised yet.
function isDurationMismatch(r: FeedLoadSummaryRecord): boolean {
  if (!r.avg_consumption_per_day || r.avg_consumption_per_day <= 0) return false;
  if (!r.total_purchased_qty || r.total_purchased_qty <= 0) return false;
  if (r.days_stock_can_last > 0) return false; // load still active
  const expectedDays = Math.floor(r.total_purchased_qty / r.avg_consumption_per_day);
  const actualDays = r.days_consumed_so_far + r.days_stock_can_last;
  return actualDays < expectedDays;
}

/**
 * Preferred display order for feed groups.
 * Matching is done case-insensitively on partial names because imported source
 * data can have slight naming differences.
 */
function feedSortOrder(name: string): number {
  const n = name.toLowerCase();
  if (n.includes("concentrate")) return 0;
  if (n.includes("masoor")) return 1;
  if (n.includes("toor")) return 2;
  if (n.includes("vgoat") || n.includes("grain mix")) return 3;
  if (n.includes("baking")) return 4;
  if (n.includes("uht") || n.includes("milk")) return 5;
  return 6;
}

const COLS = 10;

export function FeedLoadSummaryTable({ records, tableClassName }: FeedLoadSummaryTableProps) {
  const [farmFilter, setFarmFilter] = useState<string>("All");
  const [feedFilter, setFeedFilter] = useState<string>("All");

  const farms = useMemo(() => {
    const s = new Set<string>();
    for (const r of records) if (r.farm) s.add(r.farm.toUpperCase());
    return Array.from(s).sort();
  }, [records]);

  const feedTypes = useMemo(() => {
    const s = new Set<string>();
    for (const r of records) if (r.feed) s.add(r.feed);
    return Array.from(s).sort((a, b) => feedSortOrder(a) - feedSortOrder(b) || a.localeCompare(b));
  }, [records]);

  const grouped = useMemo(() => {
    const filtered = records.filter((r) => {
      if (farmFilter !== "All" && r.farm.toUpperCase() !== farmFilter) return false;
      if (feedFilter !== "All" && r.feed !== feedFilter) return false;
      return true;
    });
    const map = new Map<string, FeedLoadSummaryRecord[]>();
    for (const r of filtered) {
      if (!map.has(r.feed)) map.set(r.feed, []);
      map.get(r.feed)!.push(r);
    }
    return new Map(
      [...map.entries()].sort(
        ([a], [b]) => feedSortOrder(a) - feedSortOrder(b) || a.localeCompare(b)
      )
    );
  }, [records, farmFilter, feedFilter]);

  const mismatchCount = useMemo(() => {
    let count = 0;
    for (const rows of grouped.values()) count += rows.filter(isDurationMismatch).length;
    return count;
  }, [grouped]);

  const toggleCls = (active: boolean) =>
    cn(
      "rounded-lg px-3 py-1 text-xs font-medium transition-colors",
      active ? "bg-surface-muted text-teal" : "text-dim hover:text-teal"
    );

  return (
    <div className="space-y-3">
      {/* Row 1: Farm filter */}
      <div className="flex items-center gap-1">
        <span className="text-[10px] text-dim w-8 shrink-0">Farm:</span>
        {["All", ...farms].map((f) => (
          <button key={f} onClick={() => setFarmFilter(f)} className={toggleCls(farmFilter === f)}>
            {f}
          </button>
        ))}
      </div>

      {/* Row 2: Feed filter + mismatch badge */}
      <div className="flex items-center gap-1 flex-wrap">
        <span className="text-[10px] text-dim w-8 shrink-0">Feed:</span>
        {["All", ...feedTypes].map((f) => (
          <button key={f} onClick={() => setFeedFilter(f)} className={toggleCls(feedFilter === f)}>
            {f}
          </button>
        ))}
        {mismatchCount > 0 && (
          <div className="flex items-center gap-1.5 ml-auto text-[11px] text-red-400 shrink-0">
            <AlertTriangle size={12} />
            {mismatchCount} load{mismatchCount > 1 ? "s" : ""} with duration mismatch
          </div>
        )}
      </div>

      <div className={cn("overflow-auto rounded-xl border border-[#334155]", tableClassName)}>
        <table className="w-full text-xs" style={{ minWidth: 900 }}>
          <thead className="sticky top-0 z-10">
            <tr className="border-b border-[#334155] bg-[#1A1D24]">
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Farm</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Load ID</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Purchased Date</th>
              <th className="px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap">Purchased Qty</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Consumption Start</th>
              <th className="px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap">Total Consumed</th>
              <th className="px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap">Stock Available</th>
              <th className="px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap">Avg Daily Consumption</th>
              <th className="px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap">Days Used</th>
              <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Remarks</th>
            </tr>
          </thead>

          <tbody>
            {grouped.size === 0 ? (
              <tr>
                <td colSpan={COLS} className="px-3 py-8 text-center text-dim">
                  No records found
                </td>
              </tr>
            ) : (
              Array.from(grouped.entries()).map(([feed, rows]) => (
                <Fragment key={feed}>
                  <tr className="bg-[#22262E]">
                    <td colSpan={COLS} className="px-3 py-2 text-xs font-semibold text-[#B0BEC5]">
                      {feed}
                    </td>
                  </tr>
                  {rows.map((r) => {
                    const mismatch = isDurationMismatch(r);
                    return (
                      <tr
                        key={`${r.farm}-${r.load_id}`}
                        className={cn(
                          "border-b border-[#334155] transition-colors",
                          mismatch ? "bg-red-500/10 hover:bg-red-500/15" : "hover:bg-[#22262E]/50"
                        )}
                      >
                        <td className="px-3 py-2.5 text-left text-[#8899AA]">{r.farm}</td>
                        <td className="px-3 py-2.5 text-left text-[#B0BEC5] font-medium">
                          <span className="inline-flex items-center gap-1.5">
                            #{r.load_id}
                            {mismatch && <AlertTriangle size={11} className="text-red-400 shrink-0" />}
                          </span>
                        </td>
                        <td className="px-3 py-2.5 text-left text-[#8899AA]">
                          {fmtDate(r.purchase_date)}
                        </td>
                        <td className="px-3 py-2.5 text-right text-[#B0BEC5]">
                          {r.total_purchased_qty > 0 ? `${r.total_purchased_qty.toFixed(1)} kg` : "—"}
                        </td>
                        <td className="px-3 py-2.5 text-left text-[#8899AA]">
                          {fmtDate(r.consumption_start_date)}
                        </td>
                        <td className="px-3 py-2.5 text-right text-[#8899AA]">
                          {r.days_consumed_so_far > 0 && r.avg_consumption_per_day > 0
                            ? `${(r.days_consumed_so_far * r.avg_consumption_per_day).toFixed(1)} kg`
                            : "—"}
                        </td>
                        <td className="px-3 py-2.5 text-right text-[#8899AA]">
                          {r.days_stock_can_last > 0 && r.avg_consumption_per_day > 0
                            ? `${(r.days_stock_can_last * r.avg_consumption_per_day).toFixed(1)} kg`
                            : "—"}
                        </td>
                        <td className="px-3 py-2.5 text-right text-[#8899AA]">
                          {r.avg_consumption_per_day > 0
                            ? `${r.avg_consumption_per_day.toFixed(2)} kg`
                            : "—"}
                        </td>
                        <td className={cn(
                          "px-3 py-2.5 text-right font-medium",
                          mismatch ? "text-red-400" : "text-[#B0BEC5]"
                        )}>
                          {r.days_consumed_so_far > 0 ? `${r.days_consumed_so_far}d` : "—"}
                        </td>
                        <td className="px-3 py-2.5 text-left text-xs">
                          {r.avg_consumption_per_day > 0 ? (() => {
                            const expected = Math.floor(r.total_purchased_qty / r.avg_consumption_per_day);
                            const actual = r.days_consumed_so_far + r.days_stock_can_last;
                            const diff = actual - expected;
                            if (diff === 0) return <span className="text-dim">—</span>;
                            if (diff > 0) return <span className="text-emerald-400">{diff}d partially used</span>;
                            return <span className="text-red-400">{Math.abs(diff)}d less than expected</span>;
                          })() : "—"}
                        </td>
                      </tr>
                    );
                  })}
                </Fragment>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
