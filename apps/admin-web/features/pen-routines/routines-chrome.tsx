"use client";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";

import { createContext, useContext, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { Iconify } from "@/components/minimal/iconify";
import Button from "@mui/material/Button";
import { RowMenu } from "@/components/app/row-menu";
import { FilterBar } from "@/components/app/filter-bar";
import { DenseToggle } from "@/components/app/dense-toggle";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { ROUTINES_SELECT_WIDTH } from "./routines-layout";
import { DateRangeField } from "@/components/app/date-range-field";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";

export type ChromeFilter = {
  /** Query parameter this control writes. */
  param: string;
  label: string;
  value: string;
  allLabel: string;
  options: Array<{ value: string; label: string }>;
};

export type ChromeDateRange = {
  label: string;
  fromParam: string;
  toParam: string;
  from: string;
  to: string;
  fromLabel: string;
  toLabel: string;
};

export type RoutinesChromeProps = {
  /** The server-rendered <Table>, passed through so row data never crosses twice. */
  children: ReactNode;
  tableAriaLabel: string;
  basePath: string;
  currentQuery: string;
  searchParam: string;
  searchValue: string;
  searchPlaceholder: string;
  searchLabel: string;
  filters: ChromeFilter[];
  dateRange?: ChromeDateRange;
  /** Params the "reset filters" action clears. */
  clearable: string[];
  /** Params dropped whenever a filter changes, so paging restarts. */
  cursorParams: string[];
  shown: number;
  total: number;
  csv: string;
  csvName: string;
  footer: {
    /** Href for the next window, empty on the last one. */
    nextHref: string;
    hasPrevious: boolean;
    pageSizeOptions: number[];
    pageSize: number;
    limitParam: string;
    /** Range text, composed by the server from the backend's own nouns. */
    rangeLabel: string;
  };
  labels: Record<string, string>;
};

const DenseContext = createContext<{ dense: boolean; setDense: (next: boolean | ((d: boolean) => boolean)) => void }>({ dense: false, setDense: () => undefined });

/**
 * One routines card's dense switch, shared by the toolbar (OUTSIDE the URL-keyed panel) and the
 * table + footer (INSIDE it): a context, because the two halves sit on either side of a server
 * Suspense boundary.
 */
export function RoutinesDenseScope({ children }: { children: ReactNode }) {
  const [dense, setDense] = useState(false);
  return <DenseContext.Provider value={{ dense, setDense }}>{children}</DenseContext.Provider>;
}

type Patcher = { basePath: string; currentQuery: string; cursorParams: string[] };
function hrefPatcher({ basePath, currentQuery, cursorParams }: Patcher) {
  return (entries: Record<string, string | null>, keepCursor = false) => {
    const params = new URLSearchParams(currentQuery);
    for (const [k, v] of Object.entries(entries)) {
      if (!v) params.delete(k);
      else params.set(k, v);
    }
    if (!keepCursor) for (const p of cursorParams) params.delete(p);
    const qs = params.toString();
    return qs ? `${basePath}?${qs}` : basePath;
  };
}

export type RoutinesToolbarRowProps = Omit<RoutinesChromeProps, "children" | "tableAriaLabel" | "footer">;

/**
 * The filter toolbar of a routines card. It renders OUTSIDE the card's UrlSuspense (guard:
 * routines-toolbar-outside-panel): a filter change swaps only the rows to their skeleton, the
 * control just used keeps its value on screen, and an empty result still offers the filters.
 * Every control writes a query parameter and lets the server re-render; nothing here mutates a
 * routine or a task.
 */
export function RoutinesToolbarRow({
  basePath,
  currentQuery,
  searchParam,
  searchValue,
  searchPlaceholder,
  searchLabel,
  filters,
  dateRange,
  clearable,
  cursorParams,
  shown,
  total,
  csv,
  csvName,
  labels,
}: RoutinesToolbarRowProps) {
  const router = useRouter();
  const [query, setQuery] = useState(searchValue);
  // Every label is backend copy handed in by the page (chromeLabels in routines-page.tsx); the
  // chrome composes none of its own.
  const L = (key: string) => labels[key] ?? "";
  const patchHref = hrefPatcher({ basePath, currentQuery, cursorParams });
  const patch = (entries: Record<string, string | null>, keepCursor = false) => {
    router.push(patchHref(entries, keepCursor), { scroll: false });
  };

  const chips: Array<{ id: string; label: string; clear: () => void }> = [];
  if (searchValue) chips.push({ id: searchParam, label: `"${searchValue}"`, clear: () => { setQuery(""); patch({ [searchParam]: null }); } });
  for (const f of filters) {
    if (!f.value) continue;
    chips.push({ id: f.param, label: f.options.find((o) => o.value === f.value)?.label ?? f.value, clear: () => patch({ [f.param]: null }) });
  }
  if (dateRange && (dateRange.from || dateRange.to)) {
    chips.push({
      id: "range",
      label: `${dateRange.from || "…"} → ${dateRange.to || "…"}`,
      clear: () => patch({ [dateRange.fromParam]: null, [dateRange.toParam]: null }),
    });
  }

  const clearAll = () => {
    setQuery("");
    patch(Object.fromEntries(clearable.map((p) => [p, null])));
  };

  const exportCsv = () => {
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = csvName;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
      <Box>
        <FilterBar
          search={{
            value: query,
            placeholder: searchPlaceholder,
            ariaLabel: searchLabel,
            onChange: setQuery,
          }}
          actions={
            <>
              {/* Template list toolbar: one ⋮ popover (TR1-#23). Dense lives in the table footer switch. */}
              <RowMenu
                ariaLabel={L("more")}
                actions={[
                  { label: L("export"), icon: <Iconify icon="solar:download-bold" width={16} />, onSelect: exportCsv },
                  { label: L("apply_search"), onSelect: () => patch({ [searchParam]: query.trim() || null }) },
                  { label: L("reset"), icon: <Iconify icon="mingcute:close-line" width={16} />, onSelect: clearAll, disabled: chips.length === 0 },
                ]}
              />
            </>
          }
          summary={
            chips.length ? (
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
              {chips.length ? <Box component="span" sx={{ typography: "body2", color: "text.secondary" }}>{shown} / {total}</Box> : null}
              {/* Template filters-result chip: soft small Chip with its own delete affordance. */}
              {chips.map((c) => (
                <Chip
                  key={c.id}
                  size="small"
                  variant="soft"
                  label={c.label}
                  onDelete={c.clear}
                  deleteIcon={<Iconify icon="solar:close-circle-bold" aria-label={`${L("remove_filter")}: ${c.label}`} role="button" />}
                />
              ))}
              {chips.length ? <Button color="primary" variant="text" size="small" onClick={clearAll}>{L("clear_all")}</Button> : null}
            </Box>
            ) : null
          }
        >
          {filters.map((f) => (
            <TextField
              key={f.param}
              select
              label={f.label}
              value={f.options.some((option) => option.value === f.value) ? f.value : ""}
              onChange={({ target: { value: v } }) => patch({ [f.param]: v || null })}
              sx={{ minWidth: { xs: 0, sm: ROUTINES_SELECT_WIDTH }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              <MenuItem value="">{f.allLabel}</MenuItem>
              {f.options.map((option) => (
                <MenuItem key={option.value} value={option.value}>
                  {option.label}
                </MenuItem>
              ))}
            </TextField>
          ))}
          {dateRange ? (
            <DateRangeField
              label={dateRange.label}
              from={dateRange.from}
              to={dateRange.to}
              fromLabel={dateRange.fromLabel}
              toLabel={dateRange.toLabel}
              onChange={(next) => patch({ [dateRange.fromParam]: next.from || null, [dateRange.toParam]: next.to || null })}
            />
          ) : null}
        </FilterBar>
      </Box>
  );
}

/**
 * Table + footer for a routines table (the toolbar is RoutinesToolbarRow, outside the keyed panel).
 * The table stays a server component, handed in as children; this client boundary exists only to
 * share the card's dense switch with the footer.
 */
export function RoutinesTableChrome({
  children,
  tableAriaLabel,
  basePath,
  currentQuery,
  cursorParams,
  footer,
  labels,
}: Pick<RoutinesChromeProps, "children" | "tableAriaLabel" | "basePath" | "currentQuery" | "cursorParams" | "footer" | "labels">) {
  const router = useRouter();
  const { dense, setDense } = useContext(DenseContext);
  const L = (key: string) => labels[key] ?? "";
  const patchHref = hrefPatcher({ basePath, currentQuery, cursorParams });
  return (
    <>
      <Box
        tabIndex={0}
        role="group"
        aria-label={tableAriaLabel}
        data-dense={dense ? "true" : undefined}
        // Dense rows are the template `size="small"` body padding (6px 16px).
        sx={{ overflow: "auto", maxHeight: "62vh", ...(dense ? { "& .MuiTableCell-body": { py: 0.75 } } : null) }}
      >
        {children}
      </Box>

      <TablePaginationLinks
        className="pr-tfoot"
        page={footer.hasPrevious ? 1 : 0}
        rowsPerPage={footer.pageSize}
        count={-1}
        rowsPerPageHrefs={footer.pageSizeOptions.map((n) => ({ value: n, href: patchHref({ [footer.limitParam]: String(n) }) }))}
        labelRowsPerPage={`${L("rows_per_page")}:`}
        rangeLabel={footer.rangeLabel}
        prevLabel={L("previous")}
        nextLabel={L("next")}
        onPrevClick={footer.hasPrevious ? () => router.back() : undefined}
        nextHref={footer.nextHref || null}
        left={<DenseToggle checked={dense} onChange={setDense} label={L("dense")} />}
      />
    </>
  );
}
