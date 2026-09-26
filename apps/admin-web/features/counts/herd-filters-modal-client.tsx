"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { Search } from "lucide-react";
import { useState, useTransition } from "react";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
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
  const [isPending, startTransition] = useTransition();
  const [isOpen, setIsOpen] = useState(false);
  const current = routerSearchParams?.toString() ?? "";
  const [optimisticPageSize, setOptimisticPageSize] = useState<{ from: string; value: string } | null>(null);

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
    startTransition(() => {
      router.replace(paramsWith({ q: String(data.get("q") ?? "").trim() || null }), { scroll: false });
    });
  }

  // Takes the chosen value directly: the MUI select reports `event.target.value`.
  function onPageSize(value: string) {
    setOptimisticPageSize({ from: current, value });
    startTransition(() => {
      router.replace(paramsWith({ limit: value }), { scroll: false });
    });
  }

  const selectedPageSize = optimisticPageSize?.from === current ? optimisticPageSize.value : String(pageSize ?? pageSizeOptions[0]);

  return (
    <>
      <div className="tbar hr-tbar" style={{ display: "flex", alignItems: "center", gap: 12, padding: "4px 16px 12px", flexWrap: "wrap", overflow: "visible" }}>
        <form onSubmit={onSearch} className="tsearch" style={{ margin: 0, minWidth: 260, flex: "1 1 280px" }}>
          <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
          <input
            name="q"
            defaultValue={searchValue}
            disabled={isPending}
            placeholder={copy(pageContract, "filter.toolbar_placeholder")}
            aria-label={copy(pageContract, "filter.toolbar_aria")}
          />
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
        <TextField
          select
          label={copy(pageContract, "filter.rows_per_page_aria")}
          value={selectedPageSize}
          disabled={isPending}
          title={isPending ? copy(pageContract, "state.loading") : undefined}
          onChange={(event) => onPageSize(event.target.value)}
          sx={{ minWidth: { xs: 0, sm: 132 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {pageSizeOptions.map((size) => (
            <MenuItem key={String(size)} value={String(size)}>
              {`${size} / ${copy(pageContract, "pager.page")}`}
            </MenuItem>
          ))}
        </TextField>
      </div>

      <HerdFiltersModal open={isOpen} pageContract={pageContract} searchParams={params} onClose={() => setIsOpen(false)} />
    </>
  );
}
