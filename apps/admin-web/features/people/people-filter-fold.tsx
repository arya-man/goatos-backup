"use client";

import { useCallback, useState, type ReactNode } from "react";
import Badge from "@mui/material/Badge";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";

import { MinimalDrawer } from "@/components/minimal/drawer";
import { Iconify } from "@/components/minimal/iconify";
import { useBackCloses } from "@/components/use-back-closes";
import pf from "./people-filter-fold.module.css";

/**
 * People filter row. Desktop: search + selects + Apply on one line, unchanged. Phone: only the
 * search field and a "Filters" button show; the selects and Apply open in the template filters
 * drawer (MinimalDrawer, portaled to <body>). The fields keep submitting the same GET form through
 * their `form` attribute.
 */
export function PeopleFilterFold({
  search,
  filtersLabel,
  closeLabel,
  activeCount,
  children,
}: {
  search: ReactNode;
  filtersLabel: string;
  closeLabel: string;
  activeCount: number;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  // Android Back closes the drawer instead of leaving the page (house drawer rule).
  useBackCloses(open, close);

  return (
    <>
      <div className={pf.searchRow}>
        {search}
        <Button
          variant="outlined"
          color="inherit"
          className={pf.open}
          aria-expanded={open}
          onClick={() => setOpen(true)}
          startIcon={
            <Badge color="error" variant="dot" invisible={activeCount === 0}>
              <Iconify icon="ic:round-filter-list" />
            </Badge>
          }
        >
          {filtersLabel}
          {activeCount > 0 ? ` (${activeCount})` : null}
        </Button>
      </div>
      <MinimalDrawer open={open} onClose={close} title={filtersLabel} aria-label={filtersLabel} closeLabel={closeLabel}>
        <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2.5, "& > *": { width: 1, minWidth: "0 !important" } }}>{children}</Box>
      </MinimalDrawer>
      {open ? null : <div className={pf.rest}>{children}</div>}
    </>
  );
}
