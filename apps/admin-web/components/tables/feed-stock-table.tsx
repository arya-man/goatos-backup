"use client";

import { useState, useMemo } from "react";
import { cn } from "@/lib/utils";
import { formatINR, formatNumber } from "@/lib/constants";
import type { FeedLoadRecord } from "@/lib/types";
import { ArrowUpDown } from "lucide-react";

interface FeedStockTableProps {
  records: FeedLoadRecord[];
  tableClassName?: string;
}

// Blue tint cells
const blueTint = "bg-sky-950/20";
// Orange tint cells
const orangeTint = "bg-amber-950/20";

type SortField = "purchase_date" | "last_payment_date";
type SortDir = "asc" | "desc";

/** Format a date string/Date to dd-mm-yyyy */
function fmtDate(d: unknown): string {
  const s = String(d || "");
  if (!s || s === "undefined" || s === "null") return "—";
  const dt = new Date(s);
  if (isNaN(dt.getTime())) return s;
  const dd = String(dt.getDate()).padStart(2, "0");
  const mm = String(dt.getMonth() + 1).padStart(2, "0");
  const yyyy = dt.getFullYear();
  return `${dd}-${mm}-${yyyy}`;
}

function DaysLeftBadge({ days, consumptionStarted }: { days: number; consumptionStarted: boolean }) {
  if (!consumptionStarted && days <= 0) {
    return (
      <span className={cn("inline-flex rounded-full px-2 py-0.5 text-[10px] font-medium", "bg-zinc-400/10 text-zinc-400")}>
        N/A
      </span>
    );
  }
  const cls =
    days > 10
      ? "bg-emerald-400/10 text-emerald-400"
      : days > 0
        ? "bg-amber-400/10 text-amber-400"
        : "bg-red-400/10 text-red-400";
  return (
    <span className={cn("inline-flex rounded-full px-2 py-0.5 text-[10px] font-medium", cls)}>
      {days}d
    </span>
  );
}

export function FeedStockTable({ records, tableClassName }: FeedStockTableProps) {
  const [sortField, setSortField] = useState<SortField>("purchase_date");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  const toggleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortField(field);
      setSortDir("desc");
    }
  };

  const sortedRecords = useMemo(() => {
    const sorted = [...records];
    sorted.sort((a, b) => {
      const aVal = String((a as unknown as Record<string, unknown>)[sortField] || "");
      const bVal = String((b as unknown as Record<string, unknown>)[sortField] || "");
      const cmp = aVal.localeCompare(bVal);
      return sortDir === "asc" ? cmp : -cmp;
    });
    return sorted;
  }, [records, sortField, sortDir]);

  const grouped = useMemo(() => {
    const map = new Map<string, FeedLoadRecord[]>();
    for (const r of sortedRecords) {
      if (!map.has(r.load_type)) map.set(r.load_type, []);
      map.get(r.load_type)!.push(r);
    }
    return map;
  }, [sortedRecords]);

  const SortButton = ({ field, label }: { field: SortField; label: string }) => (
    <button
      onClick={() => toggleSort(field)}
      className="flex items-center gap-1 hover:text-[#B0BEC5] transition-colors"
    >
      {label}
      <ArrowUpDown size={10} className={sortField === field ? "text-[#FFFFFF]" : "text-[#8899AA]"} />
    </button>
  );

  return (
    <div className="space-y-2">
      <div className="flex justify-end items-center gap-1">
        <span className="text-[10px] text-[#8899AA] mr-1">Sort by:</span>
        {([
          { field: "purchase_date" as SortField, label: "Purchase Date" },
          { field: "last_payment_date" as SortField, label: "Last Payment Date" },
        ]).map(({ field, label }) => (
          <button
            key={field}
            onClick={() => toggleSort(field)}
            className={cn(
              "flex items-center gap-1 rounded-lg px-2.5 py-1 text-[11px] transition-colors",
              sortField === field
                ? "bg-[#22262E] text-[#FFFFFF] font-medium"
                : "text-[#8899AA] hover:text-[#B0BEC5]"
            )}
          >
            {label}
            <ArrowUpDown size={10} className={sortField === field ? "text-[#FFFFFF]" : "text-[#8899AA]"} />
          </button>
        ))}
      </div>
    <div className={cn("overflow-auto rounded-xl border border-[#334155]", tableClassName)}>
      <table className="w-full text-xs" style={{ minWidth: 980 }}>
        <thead className="sticky top-0 z-10">
          <tr className="border-b border-[#334155] bg-[#1A1D24]">
            <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Load Type</th>
            <th className="px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap">Load ID</th>
            <th className={cn("px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap", blueTint)}>
              <SortButton field="purchase_date" label="Purchase Date" />
            </th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", blueTint)}>Consumption/Day</th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", blueTint)}>Purchased Qty</th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", blueTint)}>Current Stock</th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", blueTint)}>Days Consumed</th>
            <th className={cn("px-3 py-2.5 text-center font-medium text-[#B0BEC5] whitespace-nowrap", blueTint)}>Days Left</th>
            {/* Visual divider */}
            <th className="w-px bg-[#334155]" />
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", orangeTint)}>Cost</th>
            <th className={cn("px-3 py-2.5 text-left font-medium text-[#B0BEC5] whitespace-nowrap", orangeTint)}>
              <SortButton field="last_payment_date" label="Last Payment" />
            </th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", orangeTint)}>Last Amt</th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", orangeTint)}>Paid</th>
            <th className={cn("px-3 py-2.5 text-right font-medium text-[#B0BEC5] whitespace-nowrap", orangeTint)}>Pending</th>
          </tr>
        </thead>
        <tbody>
          {Array.from(grouped.entries()).map(([loadType, recs]) => (
            <>
              <tr key={`header-${loadType}`} className="bg-[#22262E]">
                <td colSpan={14} className="px-3 py-2 text-xs font-semibold text-[#B0BEC5]">
                  {loadType}
                </td>
              </tr>
              {recs.map((r) => (
                <tr
                  key={`${loadType}-${r.load_id}`}
                  className="border-b border-[#334155] transition-colors hover:bg-[#22262E]/50"
                >
                  <td className="px-3 py-2 text-[#8899AA]">{r.load_type}</td>
                  <td className="px-3 py-2 text-[#B0BEC5] font-medium">#{r.load_id}</td>
                  <td className={cn("px-3 py-2 text-[#B0BEC5]", blueTint)}>{fmtDate(r.purchase_date)}</td>
                  <td className={cn("px-3 py-2 text-right text-[#B0BEC5]", blueTint)}>
                    {r.consumption_per_day ? r.consumption_per_day.toFixed(1) : "—"}
                  </td>
                  <td className={cn("px-3 py-2 text-right text-[#B0BEC5]", blueTint)}>
                    {formatNumber(r.total_purchased_qty)}
                  </td>
                  <td className={cn("px-3 py-2 text-right text-[#FFFFFF] font-medium", blueTint)}>
                    {formatNumber(Math.round(r.current_stock))}
                  </td>
                  <td className={cn("px-3 py-2 text-right text-[#B0BEC5]", blueTint)}>{r.days_consumed_so_far}</td>
                  <td className={cn("px-3 py-2 text-center", blueTint)}>
                    <DaysLeftBadge days={r.days_stock_can_last} consumptionStarted={r.consumption_per_day > 0 || r.days_consumed_so_far > 0} />
                  </td>
                  <td className="w-px bg-[#334155]" />
                  <td className={cn("px-3 py-2 text-right text-[#B0BEC5]", orangeTint)}>
                    {formatINR(r.last_load_total_cost)}
                  </td>
                  <td className={cn("px-3 py-2 text-[#B0BEC5]", orangeTint)}>{fmtDate(r.last_payment_date)}</td>
                  <td className={cn("px-3 py-2 text-right text-[#B0BEC5]", orangeTint)}>
                    {r.last_payment_amount ? formatINR(r.last_payment_amount) : "—"}
                  </td>
                  <td className={cn("px-3 py-2 text-right text-[#B0BEC5]", orangeTint)}>
                    {formatINR(r.paid_so_far)}
                  </td>
                  <td
                    className={cn(
                      "px-3 py-2 text-right font-medium",
                      orangeTint,
                      r.pending_amount > 0 ? "text-red-400" : "text-[#8899AA]"
                    )}
                  >
                    {formatINR(r.pending_amount)}
                  </td>
                </tr>
              ))}
            </>
          ))}
        </tbody>
      </table>
    </div>
    </div>
  );
}
