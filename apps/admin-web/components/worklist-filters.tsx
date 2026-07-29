"use client";

import { useRouter, useSearchParams } from "next/navigation";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export type WorklistFilterOption = { value: string; label: string };

export type WorklistFilterField =
  | {
      kind: "select";
      param: string;
      label: string;
      value: string;
      options: WorklistFilterOption[];
      allowAll?: boolean;
      disabledReason?: string;
    }
  | {
      kind: "date";
      param: string;
      label: string;
      value: string;
      min?: string;
      max?: string;
      disabledReason?: string;
    };

// Shared mock-shaped filter bar for backend-filtered operational worklists. Each change rewrites
// the URL and resets the page offset; the server remains the owner of rows and totals.
export function WorklistFilters({
  basePath,
  pageParam,
  fields,
  pageContract,
}: {
  basePath: string;
  pageParam: string;
  fields: WorklistFilterField[];
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const routerSearchParams = useSearchParams();
  const current = routerSearchParams?.toString() ?? "";
  const allLabel = copy(pageContract, "filter.all_option");
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
        <label key={field.param} style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}>
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
