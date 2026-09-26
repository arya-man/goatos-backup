"use client";

import { useCallback, useEffect, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";

import Chip from "@mui/material/Chip";
import Checkbox from "@mui/material/Checkbox";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";

import { CustomPopover } from "@/components/minimal/custom-popover";
import { Iconify } from "@/components/minimal/iconify";
import { chipProps, FiltersBlock, FiltersResult } from "@/components/minimal/filters-result";
import { OrderTableToolbar, type OrderToolbarMenuAction } from "@/components/minimal/sections/order/order-table-toolbar";
import { TAP_MIN } from "@/components/minimal/_shared/tap";
import { useBackCloses } from "@/components/use-back-closes";

export type ToolbarChip = {
  id: string;
  /** Filter name ("Status"), shown as the template FiltersBlock label. */
  group: string;
  label: string;
  /** Where removing this one filter goes. */
  href: string;
};

/**
 * Columns + Export for the procurement list tables (vendors, feed purchases), as ⋮ menu actions of
 * the template OrderTableToolbar.
 *
 * Both work against the real table rather than a column model: `tableId` is the id of the table's
 * scroll region, a hidden column is `display:none` on the nth cell of every row, and Export
 * serialises exactly the visible cells. Both are read-only — nothing is submitted.
 */
export function useTableColumnsMenu({
  tableId,
  exportName,
  columnsLabel,
  exportLabel,
}: {
  tableId: string;
  exportName: string;
  columnsLabel: string;
  exportLabel: string;
}): { menuActions: OrderToolbarMenuAction[]; popover: ReactNode } {
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const open = Boolean(anchorEl);
  const close = useCallback(() => setAnchorEl(null), []);
  // Back closes the Columns list instead of leaving the page.
  useBackCloses(open, close);
  const [headers, setHeaders] = useState<string[]>([]);
  const [hidden, setHidden] = useState<number[]>([]);

  const table = useCallback(() => document.getElementById(tableId)?.querySelector("table") ?? null, [tableId]);

  useEffect(() => {
    const el = table();
    if (!el) return;
    const set = new Set(hidden);
    for (const row of Array.from(el.rows)) {
      Array.from(row.cells).forEach((cell, index) => {
        cell.style.display = set.has(index) ? "none" : "";
        cell.dataset.colHidden = set.has(index) ? "1" : "";
      });
    }
  }, [hidden, table, headers]);

  const toggleColumn = useCallback(
    (index: number) => {
      setHidden((current) => {
        const next = current.includes(index) ? current.filter((n) => n !== index) : [...current, index];
        // Never let the reader hide the last column and end up with an empty table.
        if (next.length >= headers.length) return current;
        return next;
      });
    },
    [headers.length],
  );

  const exportCsv = useCallback(() => {
    const el = table();
    if (!el) return;
    const cell = (value: string) => `"${value.replace(/"/g, '""')}"`;
    const lines = Array.from(el.rows).map((row) =>
      Array.from(row.cells)
        .filter((c) => c.dataset.colHidden !== "1")
        .map((c) => cell((c.textContent ?? "").replace(/\s+/g, " ").trim()))
        .join(","),
    );
    const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${exportName}.csv`;
    link.click();
    URL.revokeObjectURL(url);
  }, [table, exportName]);

  const menuActions: OrderToolbarMenuAction[] = [
    {
      key: "columns",
      label: columnsLabel,
      icon: <Iconify icon="solar:list-bold" />,
      onClick: (anchor) => {
        // Header labels are read from the real table in the click that opens the list.
        const el = table();
        if (el) {
          setHeaders(Array.from(el.querySelectorAll("thead th")).map((th, index) => th.textContent?.trim() || `Column ${index + 1}`));
        }
        setAnchorEl(anchor);
      },
    },
    { key: "export", label: exportLabel, icon: <Iconify icon="solar:export-bold" />, onClick: exportCsv },
  ];

  const popover = (
    <CustomPopover
      open={open}
      anchorEl={anchorEl}
      onClose={close}
      slotProps={{ arrow: { placement: "right-top" }, paper: { sx: { maxHeight: 320, overflowY: "auto" } } }}
    >
      <MenuList aria-label={columnsLabel}>
        {headers.map((label, index) => {
          const shown = !hidden.includes(index);
          return (
            <MenuItem
              key={`${label}-${index}`}
              role="menuitemcheckbox"
              aria-checked={shown}
              onClick={() => toggleColumn(index)}
              sx={(theme) => ({ [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN } })}
            >
              <Checkbox size="small" checked={shown} disableRipple tabIndex={-1} sx={{ p: 0 }} slotProps={{ input: { "aria-hidden": true } }} />
              {label}
            </MenuItem>
          );
        })}
      </MenuList>
    </CustomPopover>
  );

  return { menuActions, popover };
}

/**
 * The applied filters under the toolbar: the template FiltersResult (result count, one dashed
 * FiltersBlock per filter with a soft Chip, the red Clear button). Removing a chip navigates to the
 * URL without that filter; Clear goes to `clearHref`.
 */
export function ProcurementFiltersResult({
  chips,
  clearHref,
  totalResults,
}: {
  chips: readonly ToolbarChip[];
  clearHref: string;
  totalResults: number;
}) {
  const router = useRouter();
  if (!chips.length) return null;
  return (
    <FiltersResult totalResults={totalResults} onReset={() => router.push(clearHref, { scroll: false })} sx={{ p: 2.5, pt: 0 }}>
      {chips.map((chip) => (
        <FiltersBlock key={chip.id} label={`${chip.group}:`} isShow>
          <Chip {...chipProps} label={chip.label} title={chip.label} onDelete={() => router.push(chip.href, { scroll: false })} sx={{ maxWidth: "100%" }} />
        </FiltersBlock>
      ))}
    </FiltersResult>
  );
}

/**
 * The template OrderTableToolbar for a server-rendered list: the page's own URL-backed controls in
 * the leading / search slots, and Columns + Export behind the ⋮ (see useTableColumnsMenu).
 */
export function ProcurementListToolbar({
  tableId,
  exportName,
  columnsLabel,
  exportLabel,
  moreLabel,
  filters,
  search,
  trailing,
}: {
  tableId: string;
  exportName: string;
  columnsLabel: string;
  exportLabel: string;
  moreLabel: string;
  filters?: ReactNode;
  search?: ReactNode;
  trailing?: ReactNode;
}) {
  const columns = useTableColumnsMenu({ tableId, exportName, columnsLabel, exportLabel });
  return (
    <>
      <OrderTableToolbar filters={filters} search={search} trailing={trailing} menuActions={columns.menuActions} menuLabel={moreLabel} />
      {columns.popover}
    </>
  );
}
