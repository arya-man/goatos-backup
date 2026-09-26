"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Columns3, Download, Trash2, X } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import { useRouter } from "next/navigation";
import { usePopover } from "minimal-shared/hooks";
import Checkbox from "@mui/material/Checkbox";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { TAP_MIN } from "@/components/minimal/_shared/tap";
import { RowMenu } from "@/components/app/row-menu";
import { useBackCloses } from "@/components/use-back-closes";

export type ToolbarChip = {
  id: string;
  label: string;
  /** Where removing this one filter goes. */
  href: string;
};

export type ToolbarAction = {
  id: string;
  label: string;
  icon?: React.ReactNode;
  onSelect: () => void;
};

/**
 * The shared table toolbar for the sales / procurement tables (spec §2).
 *
 * Filter controls are passed in as `children` because they are server-built links (LinkSelect):
 * the toolbar owns the CHROME — the applied-filter chips with an `X` and "Clear all", the Columns
 * visibility list, Export, and the `⋮` overflow — not the filtering itself.
 *
 * Columns and Export work against the real table rather than a column model, because every table on
 * these routes is hand-rolled markup rather than a `DataTable`: `tableId` is the id of the table's
 * `.twrap`, hidden columns are applied as a `kit-col-hidden` class on the nth cell of every row,
 * and Export serialises exactly the visible cells. Both are read-only — nothing is submitted.
 */
export function ProcurementTableToolbar({
  ariaLabel,
  tableId,
  exportName,
  chips,
  clearHref,
  clearLabel,
  columnsLabel,
  exportLabel,
  moreLabel,
  maxInlineActions = 2,
  actions = [],
  children,
}: {
  ariaLabel: string;
  /** Id of the table's scroll wrapper; enables the Columns and Export controls when present. */
  tableId?: string;
  exportName?: string;
  chips?: readonly ToolbarChip[];
  clearHref?: string;
  clearLabel: string;
  columnsLabel: string;
  exportLabel: string;
  moreLabel: string;
  maxInlineActions?: number;
  actions?: readonly ToolbarAction[];
  children?: React.ReactNode;
}) {
  const router = useRouter();
  // The template menu popover (CustomPopover + MenuList): MUI portals it, keeps it in the
  // viewport and closes it on an outside tap and on Escape.
  const columnsMenu = usePopover();
  const columnsOpen = columnsMenu.open;
  // Back closes the Columns sheet instead of leaving the page.
  useBackCloses(columnsOpen, columnsMenu.onClose);
  const [headers, setHeaders] = useState<string[]>([]);
  const [hidden, setHidden] = useState<number[]>([]);

  const table = useCallback(() => {
    if (!tableId) return null;
    return document.getElementById(tableId)?.querySelector("table") ?? null;
  }, [tableId]);

  // Header labels are read from the real table, in the click that opens the panel. Reading them in
  // a mount effect would be a setState inside an effect (and would run on every table, opened or
  // not); the panel cannot be open before that click, so there is nothing to show any earlier.
  const openColumns = (event: React.MouseEvent<HTMLElement>) => {
    const el = table();
    if (el) {
      setHeaders(
        Array.from(el.querySelectorAll("thead th")).map((th, index) => th.textContent?.trim() || `Column ${index + 1}`),
      );
    }
    columnsMenu.onOpen(event);
  };

  useEffect(() => {
    const el = table();
    if (!el) return;
    const set = new Set(hidden);
    for (const row of Array.from(el.rows)) {
      Array.from(row.cells).forEach((cell, index) => cell.classList.toggle("kit-col-hidden", set.has(index)));
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
        .filter((c) => !c.classList.contains("kit-col-hidden"))
        .map((c) => cell((c.textContent ?? "").replace(/\s+/g, " ").trim()))
        .join(","),
    );
    const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${exportName ?? tableId ?? "table"}.csv`;
    link.click();
    URL.revokeObjectURL(url);
  }, [table, exportName, tableId]);

  const allActions = useMemo<ToolbarAction[]>(() => {
    const built: ToolbarAction[] = [];
    if (tableId) built.push({ id: "export", label: exportLabel, icon: <Download aria-hidden="true" />, onSelect: exportCsv });
    return [...built, ...actions];
  }, [tableId, exportLabel, exportCsv, actions]);

  const inline = allActions.slice(0, maxInlineActions);
  const overflow = allActions.slice(maxInlineActions);

  return (
    <Box
      className="proc-toolbar"
      role="group"
      aria-label={ariaLabel}
      // Template table-toolbar rhythm (ecommerce ProductTableToolbar + FiltersResult): filters grow,
      // actions sit right; on a phone both take the full row and the applied chips ellipsize.
      sx={(theme) => ({
        m: 0,
        mb: 2,
        p: 0,
        alignItems: "center",
        [theme.breakpoints.down("sm")]: {
          rowGap: 1.25,
          columnGap: 1.25,
          mb: 1.75,
        },
      })}
    >
      <Box className="proc-toolbar-filters" sx={{ flex: { xs: "1 1 100%", sm: "1 1 35rem" }, minWidth: 0, width: { xs: "100%", sm: "auto" } }}>
        {children}
      </Box>
      <Box
        className="proc-toolbar-actions"
        sx={{ alignSelf: { xs: "auto", sm: "flex-start" }, flex: { xs: "1 1 100%", sm: "0 1 auto" }, width: { xs: "100%", sm: "auto" }, justifyContent: { xs: "flex-end", sm: "initial" } }}
      >
        {tableId ? (
          <div className="proc-toolbar-columns">
            <Button
              variant="outlined"
              color="inherit"
              size="small"
              startIcon={<Columns3 aria-hidden="true" />}
              aria-expanded={columnsOpen}
              aria-haspopup="true"
              onClick={openColumns}
            >
              {columnsLabel}
            </Button>
            <CustomPopover
              open={columnsOpen}
              anchorEl={columnsMenu.anchorEl}
              onClose={columnsMenu.onClose}
              slotProps={{ arrow: { placement: "top-right" }, paper: { sx: { maxHeight: 320, overflowY: "auto" } } }}
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
          </div>
        ) : null}
        {inline.map((action) => (
          <Button key={action.id} variant="outlined" color="inherit" size="small" startIcon={action.icon} onClick={action.onSelect}>
            {action.label}
          </Button>
        ))}
        {overflow.length > 0 ? (
          <RowMenu
            ariaLabel={moreLabel}
            actions={overflow.map((action) => ({ label: action.label, icon: action.icon, onSelect: action.onSelect }))}
          />
        ) : null}
      </Box>
      {chips && chips.length > 0 ? (
        // Template filters-result: soft small Chips with their own delete affordance, then a
        // "Clear" Button. Each chip's delete lands on the URL without that filter.
        <Box
          role="list"
          aria-label={`${ariaLabel} — applied filters`}
          sx={{ flex: "1 0 100%", display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}
        >
          {chips.map((chip) => (
            <Box component="span" role="listitem" key={chip.id} sx={{ display: "inline-flex", maxWidth: "100%", minWidth: 0 }}>
              <Chip
                size="small"
                variant="soft"
                label={chip.label}
                title={chip.label}
                onDelete={() => router.push(chip.href, { scroll: false })}
                deleteIcon={<X aria-label={`Remove ${chip.label}`} role="button" />}
                sx={{ maxWidth: "100%" }}
              />
            </Box>
          ))}
          {clearHref ? (
            <Button component={Link} href={clearHref} scroll={false} color="error" size="small" startIcon={<Trash2 aria-hidden="true" size={16} />}>
              {clearLabel}
            </Button>
          ) : null}
        </Box>
      ) : null}
    </Box>
  );
}
