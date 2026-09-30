"use client";

import Link from "@/components/no-prefetch-link";
import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Label } from "@/components/minimal/label";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";
import ListItemButton from "@mui/material/ListItemButton";
import { EmptyState } from "@/components/app/empty-state";
import { Iconify } from "@/components/minimal/iconify";
import { MinimalDrawer } from "@/components/app/drawer";

const PAGE_SIZE = 12;

export type ScheduleDrawerRow = {
  eventId: string;
  date: string;
  parkName: string;
  totalSheds: number;
  totalAnimals: number;
  vaccines: string[];
  sheds: Array<{ label: string; count: number; href?: string }>;
};

function selectedEventIdFromUrl(): string | undefined {
  const url = new URL(window.location.href);
  const hash = new URLSearchParams(url.hash.replace(/^#/, ""));
  return hash.get("schedule_event") ?? url.searchParams.get("schedule_event") ?? undefined;
}

export function ScheduleLocalDrawer({
  rows,
  initialSelectedEventId,
  closeHref,
  pageContract,
}: {
  rows: ScheduleDrawerRow[];
  initialSelectedEventId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  const initialRow = rows.find((row) => row.eventId === initialSelectedEventId);
  const [displayedRow, setDisplayedRow] = useState<ScheduleDrawerRow | undefined>(initialRow);
  const [drawerOpen, setDrawerOpen] = useState(Boolean(initialRow));
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const selectedIdRef = useRef(initialRow?.eventId);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const showRow = useCallback((row: ScheduleDrawerRow) => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (selectedIdRef.current !== row.eventId) {
      previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setQuery("");
      setPage(1);
    }
    selectedIdRef.current = row.eventId;
    setDisplayedRow(row);
    openFrameRef.current = window.requestAnimationFrame(() => {
      setDrawerOpen(true);
      openFrameRef.current = null;
    });
  }, []);

  const hideRow = useCallback(() => {
    selectedIdRef.current = undefined;
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    setDrawerOpen(false);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedRow(undefined);
      closeTimerRef.current = null;
    }, 280);
  }, []);

  useEffect(() => () => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
  }, []);

  useEffect(() => {
    function syncSelectionFromUrl(): void {
      const row = rows.find((item) => item.eventId === selectedEventIdFromUrl());
      if (row) showRow(row);
      else hideRow();
    }
    window.addEventListener("popstate", syncSelectionFromUrl);
    window.addEventListener("hashchange", syncSelectionFromUrl);
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    const initialFrame = window.requestAnimationFrame(syncSelectionFromUrl);
    return () => {
      window.cancelAnimationFrame(initialFrame);
      window.removeEventListener("popstate", syncSelectionFromUrl);
      window.removeEventListener("hashchange", syncSelectionFromUrl);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    };
  }, [hideRow, rows, showRow]);

  // The MUI Drawer focuses itself on open and restores focus on close; this only covers a close
  // that happens from the URL (Back) while focus sat inside the drawer.
  useEffect(() => {
    if (!drawerOpen) previousFocusRef.current?.focus();
  }, [drawerOpen]);

  const closeDrawer = useCallback(() => {
    hideRow();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideRow]);

  const filteredSheds = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return normalized
      ? displayedRow?.sheds.filter((shed) => shed.label.toLowerCase().includes(normalized)) ?? []
      : displayedRow?.sheds ?? [];
  }, [displayedRow, query]);
  const totalPages = Math.max(1, Math.ceil(filteredSheds.length / PAGE_SIZE));
  const normalizedPage = Math.min(page, totalPages);
  const visibleSheds = filteredSheds.slice((normalizedPage - 1) * PAGE_SIZE, normalizedPage * PAGE_SIZE);

  if (!displayedRow) return null;

  const closeLabel = copy(pageContract, "schedule.drawer.close");
  const rowSx = { gap: 1.25, flex: "none", borderBottom: 1, borderColor: "divider" } as const;

  // Template MinimalDrawer (calendar-filters drawer shell): portalled MUI Drawer, focus trapped and
  // restored; X, Escape and the scrim call closeDrawer, which steps the local-overlay history back
  // (the URL hash #schedule_event stays the source of truth, so Back closes it too).
  return (
    <MinimalDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={copy(pageContract, "schedule.drawer.title")}
      closeLabel={closeLabel}
      width={360}
      role="dialog"
      aria-labelledby="schedule-shed-drawer-title"
      footer={
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1.25, width: 1, flexWrap: "wrap" }}>
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "schedule.drawer.page_label")} {normalizedPage} / {totalPages} · {filteredSheds.length} {copy(pageContract, "schedule.drawer.rows_label")}
          </Typography>
          <Stack direction="row" spacing={1}>
            <Button size="small" variant="outlined" color="inherit" disabled={normalizedPage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>
              {copy(pageContract, "schedule.drawer.previous_page")}
            </Button>
            <Button size="small" variant="outlined" color="inherit" disabled={normalizedPage >= totalPages} onClick={() => setPage((value) => Math.min(totalPages, value + 1))}>
              {copy(pageContract, "schedule.drawer.next_page")}
            </Button>
          </Stack>
        </Box>
      }
    >
      <Box sx={{ px: 2.5, pt: 2, pb: 1.5 }}>
        <Typography variant="overline" sx={{ color: "text.secondary" }}>
          {displayedRow.date ? `${fmtDate(displayedRow.date)} · ${displayedRow.parkName}` : copy(pageContract, "label.placeholder")}
        </Typography>
        <Typography id="schedule-shed-drawer-title" variant="subtitle1" component="h3">{copy(pageContract, "schedule.drawer.title")}</Typography>
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {displayedRow.totalSheds} {copy(pageContract, "schedule.unit.sheds")} · {displayedRow.totalAnimals} {copy(pageContract, "schedule.unit.animals")} · {displayedRow.vaccines.join(", ") || copy(pageContract, "label.placeholder")}
        </Typography>
      </Box>

      <Box
        component="form"
        sx={{ display: "flex", alignItems: "center", gap: 1, px: 2.5, pb: 2 }}
        onSubmit={(event: React.FormEvent<HTMLFormElement>) => {
          event.preventDefault();
          setPage(1);
        }}
      >
        <TextField
          name="schedule_sheds_q"
          size="small"
          fullWidth
          value={query}
          onChange={(event) => {
            setQuery(event.target.value.slice(0, 80));
            setPage(1);
          }}
          placeholder={copy(pageContract, "schedule.drawer.search")}
          slotProps={{
            htmlInput: { "aria-label": copy(pageContract, "schedule.drawer.search") },
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                </InputAdornment>
              ),
            },
          }}
        />
        <Button type="submit" variant="outlined" color="inherit" sx={{ flex: "none" }}>
          {copy(pageContract, "schedule.drawer.search_action")}
        </Button>
      </Box>

      <Stack role="list" aria-label={copy(pageContract, "schedule.drawer.title")} sx={{ px: 2.5, pb: 2.5 }}>
        {visibleSheds.length > 0 ? visibleSheds.map((shed) => {
          const contents = (
            <>
              <Iconify icon="solar:home-angle-bold-duotone" width={16} aria-hidden="true" sx={{ color: "primary.main", flexShrink: 0 }} />
              <Box component="span" sx={{ flexGrow: 1, minWidth: 0, typography: "body2" }}>{shed.label}</Box>
              <Label color="info">{shed.count > 0 ? `${shed.count} ${copy(pageContract, "schedule.unit.animals")}` : copy(pageContract, "schedule.drawer.open_roster")}</Label>
            </>
          );
          return shed.href ? (
            <ListItemButton key={shed.label} component={Link} href={shed.href} role="listitem" sx={rowSx}>{contents}</ListItemButton>
          ) : (
            <Box key={shed.label} role="listitem" sx={{ ...rowSx, display: "flex", alignItems: "center", px: 2, py: 1 }}>{contents}</Box>
          );
        }) : (
          <EmptyState title={copy(pageContract, "schedule.drawer.empty")} />
        )}
      </Stack>
    </MinimalDrawer>
  );
}
