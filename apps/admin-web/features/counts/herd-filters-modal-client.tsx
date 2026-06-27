"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Search } from "lucide-react";
import { useState } from "react";
import { HerdFiltersModal } from "./herd-filters-modal";
import type { RouteSearchParams } from "@/lib/search-params";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Filters button + modal. Reads the LIVE URL via useSearchParams (not a server-render snapshot) so the
// modal's initial chip state and preserved scope always reflect the current address bar, even after a
// client-side filter navigation without a full reload.
export function HerdFiltersModalClient({
  rowCount,
  pageSize,
  pageSizeOptions,
  hasFilters,
  pageContract,
}: {
  rowCount?: number;
  pageSize?: number;
  pageSizeOptions: number[];
  hasFilters?: boolean;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const routerSearchParams = useSearchParams();
  const [isOpen, setIsOpen] = useState(false);

  const params: RouteSearchParams = {};
  routerSearchParams?.forEach((value, key) => {
    const existing = params[key];
    if (existing === undefined) {
      params[key] = value;
    } else if (Array.isArray(existing)) {
      existing.push(value);
    } else {
      params[key] = [existing, value];
    }
  });
  const searchValue = typeof params.q === "string" ? params.q : Array.isArray(params.q) ? params.q[0] : "";

  function paramsWith(updates: Record<string, string | null>): string {
    const next = new URLSearchParams(routerSearchParams?.toString() ?? "");
    next.delete("cursor");
    next.delete("page");
    for (const [key, value] of Object.entries(updates)) {
      if (value === null || value === "") next.delete(key);
      else next.set(key, value);
    }
    const qs = next.toString();
    return qs ? `/counts/herd?${qs}` : "/counts/herd";
  }

  function onSearch(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    router.replace(paramsWith({ q: String(data.get("q") ?? "").trim() || null }), { scroll: false });
  }

  function onPageSize(event: React.ChangeEvent<HTMLSelectElement>) {
    router.replace(paramsWith({ limit: event.target.value }), { scroll: false });
  }

  return (
    <>
      <div className="tbar" style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", flexWrap: "wrap" }}>
        <form onSubmit={onSearch} className="tsearch" style={{ margin: 0, minWidth: 260, flex: "1 1 280px" }}>
          <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
          <input name="q" defaultValue={searchValue} placeholder={copy(pageContract, "filter.toolbar_placeholder")} aria-label={copy(pageContract, "filter.toolbar_aria")} />
        </form>
        <button type="button" className="btn" onClick={() => setIsOpen(true)}>
          <Search className="ic" style={{ width: 14 }} aria-hidden="true" />
          {copy(pageContract, "action.filters")}
          {hasFilters ? (
            <span className="fbadge" style={{ color: "var(--brand-d)", fontWeight: 700 }}>
              {copy(pageContract, "filter.active_badge")}
            </span>
          ) : null}
        </button>
        <span className="muted small">{rowCount ?? 0} {copy(pageContract, "label.rows")}</span>
        <select className="tsize" value={pageSize ?? pageSizeOptions[0]} onChange={onPageSize} aria-label={copy(pageContract, "filter.rows_per_page_aria")}>
          {pageSizeOptions.map((size) => (
            <option key={size} value={size}>{size} / {copy(pageContract, "pager.page")}</option>
          ))}
        </select>
      </div>

      <HerdFiltersModal open={isOpen} pageContract={pageContract} searchParams={params} onClose={() => setIsOpen(false)} />
    </>
  );
}
