"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Server-side filtering for the Feed pages. Same idiom as the Counts Breakdown filter bar: read the
// LIVE URL, rewrite one param, let the next server render do the actual filtering. No client-side
// row hiding — a hidden row would make the day's totals disagree with the sheet.
//
// Every option list is passed in from the server component and comes from canonical data (the
// locations master, or the park's own session template). Nothing is invented here.
//
// The feed day and the park are REQUIRED by the backend rather than optional filters — the ration
// grid, the session split and the dispatch clock are all park-scoped, and a feed sheet is generated
// for exactly one business day. So those two controls are always rendered, never as an "All" option.

export type FeedFilterOption = { value: string; label: string };

export type FeedFilterField =
  | {
      kind: "select";
      param: string;
      label: string;
      value: string;
      options: FeedFilterOption[];
      /** Omitted means the control offers an "All" sentinel; false makes a value mandatory. */
      allowAll?: boolean;
      /** When set, the control renders visibly disabled and shows this as its reason. */
      disabledReason?: string;
    }
  | {
      kind: "date";
      param: string;
      label: string;
      value: string;
      /** Inclusive lower/upper bounds for the picker, so an out-of-window day cannot be selected. */
      min?: string;
      max?: string;
      disabledReason?: string;
    };

export function FeedFilters({
  basePath,
  pageParam,
  fields,
  pageContract,
}: {
  basePath: string;
  /** Offset param reset on every filter change, so a narrowed result never lands past its end. */
  pageParam: string;
  fields: FeedFilterField[];
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const routerSearchParams = useSearchParams();
  const current = routerSearchParams?.toString() ?? "";

  const allLabel = copy(pageContract, "filter.all_option");
  // Only the optional filters count toward "something is filtered" — the mandatory park and feed day
  // always carry a value, so including them would leave Clear all permanently lit.
  const clearable = fields.filter((field) => field.kind === "select" && field.allowAll !== false);
  const hasAnyFilter = clearable.some((field) => field.value !== "");

  function push(next: URLSearchParams) {
    next.delete(pageParam);
    const qs = next.toString();
    router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false });
  }

  function applyFilter(param: string, value: string) {
    const next = new URLSearchParams(current);
    if (value) next.set(param, value);
    else next.delete(param);
    push(next);
  }

  function clearAll() {
    const next = new URLSearchParams(current);
    for (const field of clearable) next.delete(field.param);
    push(next);
  }

  return (
    <div
      className="tbar"
      style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", flexWrap: "wrap" }}
      role="group"
      aria-label={copy(pageContract, "filter.bar_aria")}
    >
      {fields.map((field) => (
        <label
          key={field.param}
          style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}
        >
          <span className="muted">{field.label}</span>
          {field.kind === "date" ? (
            <input
              className="tsize"
              type="date"
              value={field.value}
              min={field.min}
              max={field.max}
              aria-label={field.label}
              disabled={Boolean(field.disabledReason)}
              title={field.disabledReason}
              style={field.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
              onChange={(event) => applyFilter(field.param, event.target.value)}
            />
          ) : (
            <select
              className="tsize"
              value={field.value}
              aria-label={field.label}
              disabled={Boolean(field.disabledReason)}
              title={field.disabledReason}
              style={field.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
              onChange={(event) => applyFilter(field.param, event.target.value)}
            >
              {field.allowAll === false ? null : <option value="">{allLabel}</option>}
              {field.options.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          )}
        </label>
      ))}
      {hasAnyFilter ? (
        <button type="button" className="btn sm" onClick={clearAll}>
          {copy(pageContract, "filter.clear_all")}
        </button>
      ) : null}
    </div>
  );
}
