"use client";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";

import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { Download, X } from "lucide-react";
import Button from "@mui/material/Button";
import { RowMenu } from "@/components/app/row-menu";
import { LEAVE_SELECT_WIDTH } from "./leave-layout";
import { FilterBar } from "@/components/app/filter-bar";
import { DenseToggle } from "@/components/app/dense-toggle";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { DateRangeField } from "@/components/app/date-range-field";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";

type LeaveToolbarProps = {
  /** Current values, read from the URL by the server page. */
  value: { q: string; park: string; designation: string; from: string; to: string };
  parkOptions: string[];
  designationOptions: string[];
  /** Base path + current query, used to patch a single param client-side. */
  basePath: string;
  currentQuery: string;
  /** Backend copy by key, resolved by the server page (a function cannot cross to a client component). */
  labels: Record<string, string>;
  shown: number;
  total: number;
  /** CSV rows, already serialised by the server (export is read-only). */
  csv: string;
  csvName: string;
  dense: boolean;
  onDenseChange: (dense: boolean) => void;
};

/**
 * Spec §2 toolbar for the leave tables.
 *
 * Every control writes a query parameter and lets the server re-render; nothing here mutates leave
 * data. The filters narrow the window the route already fetched — the backend list endpoint takes
 * only `status`, `limit` and `cursor`, so person/park/designation/date are applied in the page.
 */
function LeaveToolbar({ value, parkOptions, designationOptions, basePath, currentQuery, labels, shown, total, csv, csvName }: LeaveToolbarProps) {
  const copyFor = (key: string) => labels[key] ?? "";
  const router = useRouter();
  const [q, setQ] = useState(value.q);

  const patch = useCallback(
    (entries: Record<string, string | null>) => {
      const params = new URLSearchParams(currentQuery);
      for (const [k, v] of Object.entries(entries)) {
        if (!v) params.delete(k);
        else params.set(k, v);
      }
      params.delete("cursor");
      params.delete("q_cursor");
      const qs = params.toString();
      router.push(qs ? `${basePath}?${qs}` : basePath);
    },
    [basePath, currentQuery, router],
  );

  const chips: Array<{ id: string; label: string; clear: () => void }> = [];
  if (value.q) chips.push({ id: "q", label: `"${value.q}"`, clear: () => { setQ(""); patch({ q: null }); } });
  if (value.park) chips.push({ id: "park", label: value.park, clear: () => patch({ park: null }) });
  if (value.designation) chips.push({ id: "des", label: value.designation, clear: () => patch({ designation: null }) });
  if (value.from || value.to) chips.push({ id: "d", label: `${value.from || "…"} → ${value.to || "…"}`, clear: () => patch({ from: null, to: null }) });

  const clearAll = () => {
    setQ("");
    patch({ q: null, park: null, designation: null, from: null, to: null });
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
    <FilterBar
      className="lv-toolbar"
      search={{
        value: q,
        placeholder: copyFor("filter.search_placeholder"),
        ariaLabel: copyFor("filter.search_label"),
        onChange: setQ,
      }}
      actions={
        <>
          {/* Template list toolbar: one ⋮ popover (TR1-#23). Dense lives in the table footer switch. */}
          <RowMenu
            ariaLabel={copyFor("action.more")}
            actions={[
              { label: copyFor("action.export"), icon: <Download size={15} />, onSelect: exportCsv },
              { label: copyFor("action.apply_search"), onSelect: () => patch({ q: q.trim() || null }) },
              { label: copyFor("action.reset_filters"), icon: <X size={15} />, onSelect: clearAll, disabled: chips.length === 0 },
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
              deleteIcon={<X aria-label={`${copyFor("action.remove_filter")}: ${c.label}`} role="button" />}
            />
          ))}
          {chips.length ? <Button color="primary" variant="text" size="small" onClick={clearAll}>{copyFor("action.clear_all")}</Button> : null}
        </Box>
        ) : null
      }
    >
      <form
        style={{ display: "contents" }}
        onSubmit={(e) => {
          e.preventDefault();
          patch({ q: q.trim() || null });
        }}
      >
        <button type="submit" className="sr-only" aria-label={copyFor("action.apply_search")} />
      </form>
      <TextField
        select
        label={copyFor("filter.park")}
        value={parkOptions.includes(value.park) ? value.park : ""}
        onChange={({ target: { value: v } }) => patch({ park: v || null })}
        sx={{ minWidth: { xs: 0, sm: LEAVE_SELECT_WIDTH }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">{copyFor("filter.park.all")}</MenuItem>
        {parkOptions.map((option) => (
          <MenuItem key={option} value={option}>
            {option}
          </MenuItem>
        ))}
      </TextField>
      <TextField
        select
        label={copyFor("filter.designation")}
        value={designationOptions.includes(value.designation) ? value.designation : ""}
        onChange={({ target: { value: v } }) => patch({ designation: v || null })}
        sx={{ minWidth: { xs: 0, sm: LEAVE_SELECT_WIDTH }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">{copyFor("filter.designation.all")}</MenuItem>
        {designationOptions.map((option) => (
          <MenuItem key={option} value={option}>
            {option}
          </MenuItem>
        ))}
      </TextField>
      <DateRangeField
        label={copyFor("filter.dates")}
        from={value.from}
        to={value.to}
        fromLabel={copyFor("filter.dates_from")}
        toLabel={copyFor("filter.dates_to")}
        onChange={(next) => patch({ from: next.from || null, to: next.to || null })}
      />
    </FilterBar>
  );
}

type LeaveFooterProps = {
  shown: number;
  /** Cursor for the next window, empty when this is the last one. */
  nextHref: string;
  /** True when this is not the first window, so "previous" is reachable through history. */
  hasPrevious: boolean;
  pageSizeOptions: number[];
  pageSize: number;
  /** Rows-per-page writes a query param, so the footer patches the URL itself. */
  basePath: string;
  currentQuery: string;
  limitParam: string;
  cursorParam: string;
  dense: boolean;
  onDenseChange: (dense: boolean) => void;
  /** Backend copy by key, resolved by the server page. */
  labels: Record<string, string>;
};

/**
 * Table footer for a CURSOR-paged list: the template table pagination, but the range is "1–20 in this
 * window" rather than "of N", because the backend returns a cursor and no total. Previous walks
 * browser history, which is the only correct previous for an opaque forward cursor.
 */
function LeaveCursorFooter({ shown, nextHref, hasPrevious, pageSizeOptions, pageSize, basePath, currentQuery, limitParam, cursorParam, dense, onDenseChange, labels }: LeaveFooterProps) {
  const copyFor = (key: string) => labels[key] ?? "";
  const router = useRouter();
  const rowsHref = (size: number) => {
    const params = new URLSearchParams(currentQuery);
    params.set(limitParam, String(size));
    params.delete(cursorParam);
    const qs = params.toString();
    return qs ? `${basePath}?${qs}` : basePath;
  };
  return (
    <TablePaginationLinks
      className="lv-tfoot"
      page={hasPrevious ? 1 : 0}
      rowsPerPage={pageSize}
      count={-1}
      rowsPerPageHrefs={pageSizeOptions.map((n) => ({ value: n, href: rowsHref(n) }))}
      labelRowsPerPage={`${copyFor("label.rows_per_page")}:`}
      rangeLabel={shown === 0 ? "0" : `1–${shown}`}
      prevLabel={copyFor("action.previous")}
      nextLabel={copyFor("action.next")}
      // Previous walks browser history: the only correct previous for an opaque forward cursor.
      onPrevClick={hasPrevious ? () => router.back() : undefined}
      nextHref={nextHref || null}
      left={<DenseToggle checked={dense} onChange={onDenseChange} />}
    />
  );
}

const DenseContext = createContext<{ dense: boolean; setDense: (dense: boolean) => void }>({ dense: false, setDense: () => undefined });

/**
 * One leave card's dense switch, shared by the toolbar (OUTSIDE the URL-keyed panel) and the table +
 * footer (INSIDE it). A context, not props, because the two halves sit on either side of a server
 * Suspense boundary.
 */
export function LeaveDenseScope({ children }: { children: ReactNode }) {
  const [dense, setDense] = useState(false);
  return <DenseContext.Provider value={{ dense, setDense }}>{children}</DenseContext.Provider>;
}

/**
 * The filter toolbar row of one leave card. It renders OUTSIDE the card's UrlSuspense (guard:
 * leave-toolbar-outside-panel): a filter change swaps only the rows to their skeleton, the control
 * the reader just used stays on screen with its value, and an empty result still offers the
 * filters to widen it.
 */
export function LeaveToolbarRow({ toolbar }: { toolbar: Omit<LeaveToolbarProps, "dense" | "onDenseChange"> }) {
  const { dense, setDense } = useContext(DenseContext);
  return (
    <Box>
      <LeaveToolbar {...toolbar} dense={dense} onDenseChange={setDense} />
    </Box>
  );
}

export type LeaveTableChromeProps = {
  /** The server-rendered <Table>, handed through as children so it stays a server component. */
  children: ReactNode;
  footer: Omit<LeaveFooterProps, "dense" | "onDenseChange">;
  tableAriaLabel: string;
};

/**
 * Table + footer for one leave table (the toolbar is LeaveToolbarRow, outside the keyed panel).
 *
 * It is a client component only so the dense switch and the pager can share the card's
 * LeaveDenseScope; the table itself is still rendered on the server and passed through as
 * children, so no row data crosses into the client bundle twice.
 */
export function LeaveTableChrome({ children, footer, tableAriaLabel }: LeaveTableChromeProps) {
  const { dense, setDense } = useContext(DenseContext);
  return (
    <>
      <Box className={`tablewrap${dense ? " kit-dense" : ""}`} tabIndex={0} role="group" aria-label={tableAriaLabel} sx={{ overflow: "auto" }}>
        {children}
      </Box>
      <LeaveCursorFooter {...footer} dense={dense} onDenseChange={setDense} />
    </>
  );
}
