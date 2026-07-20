"use client";

import Link from "@/components/no-prefetch-link";
import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { Search, Warehouse, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

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
  const closeButtonRef = useRef<HTMLButtonElement>(null);
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

  useEffect(() => {
    if (drawerOpen) {
      const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
      return () => window.cancelAnimationFrame(frame);
    }
    previousFocusRef.current?.focus();
  }, [drawerOpen]);

  const closeDrawer = useCallback(() => {
    hideRow();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideRow]);

  useEffect(() => {
    if (!drawerOpen) return undefined;
    function onKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

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

  return (
    <div
      className="schedule-drawer-backdrop"
      role="presentation"
      aria-hidden={!drawerOpen}
      style={{ opacity: drawerOpen ? 1 : 0, transition: "opacity 260ms cubic-bezier(.4,0,.2,1)" }}
    >
      <button
        type="button"
        className="schedule-drawer-close-layer"
        aria-label={copy(pageContract, "schedule.drawer.close")}
        disabled={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside
        className="schedule-side-drawer"
        role="dialog"
        aria-modal="false"
        aria-hidden={!drawerOpen}
        inert={!drawerOpen}
        aria-labelledby="schedule-shed-drawer-title"
        style={{
          transform: drawerOpen ? "translateX(0)" : "translateX(100%)",
          transition: "transform 260ms cubic-bezier(.4,0,.2,1)",
        }}
      >
        <div className="schedule-drawer-head">
          <div style={{ minWidth: 0 }}>
            <span className="eyebrow">
              {displayedRow.date ? `${fmtDate(displayedRow.date)} · ${displayedRow.parkName}` : copy(pageContract, "label.placeholder")}
            </span>
            <h3 id="schedule-shed-drawer-title">{copy(pageContract, "schedule.drawer.title")}</h3>
            <p className="muted small">
              {displayedRow.totalSheds} {copy(pageContract, "schedule.unit.sheds")} · {displayedRow.totalAnimals} {copy(pageContract, "schedule.unit.animals")} · {displayedRow.vaccines.join(", ") || copy(pageContract, "label.placeholder")}
            </p>
          </div>
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "schedule.drawer.close")} onClick={closeDrawer}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        <form
          className="schedule-drawer-search"
          onSubmit={(event) => {
            event.preventDefault();
            setPage(1);
          }}
        >
          <Search className="ic" aria-hidden="true" />
          <input
            name="schedule_sheds_q"
            value={query}
            onChange={(event) => {
              setQuery(event.target.value.slice(0, 80));
              setPage(1);
            }}
            placeholder={copy(pageContract, "schedule.drawer.search")}
          />
          <button className="btn sm" type="submit">{copy(pageContract, "schedule.drawer.search_action")}</button>
        </form>

        <div className="schedule-drawer-list" role="list" aria-label={copy(pageContract, "schedule.drawer.title")}>
          {visibleSheds.length > 0 ? visibleSheds.map((shed) => {
            const contents = (
              <>
                <Warehouse className="ic" aria-hidden="true" />
                <span>{shed.label}</span>
                <Tag tone="info">{shed.count > 0 ? `${shed.count} ${copy(pageContract, "schedule.unit.animals")}` : copy(pageContract, "schedule.drawer.open_roster")}</Tag>
              </>
            );
            return shed.href ? (
              <Link key={shed.label} href={shed.href} className="schedule-drawer-shed-row" role="listitem">{contents}</Link>
            ) : (
              <div key={shed.label} className="schedule-drawer-shed-row" role="listitem">{contents}</div>
            );
          }) : (
            <div className="empty">{copy(pageContract, "schedule.drawer.empty")}</div>
          )}
        </div>

        <div className="schedule-drawer-foot">
          <span className="muted small">
            {copy(pageContract, "schedule.drawer.page_label")} {normalizedPage} / {totalPages} · {filteredSheds.length} {copy(pageContract, "schedule.drawer.rows_label")}
          </span>
          <div className="chips">
            <button type="button" className={`chip${normalizedPage <= 1 ? " disabled" : ""}`} disabled={normalizedPage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>
              {copy(pageContract, "schedule.drawer.previous_page")}
            </button>
            <button type="button" className={`chip${normalizedPage >= totalPages ? " disabled" : ""}`} disabled={normalizedPage >= totalPages} onClick={() => setPage((value) => Math.min(totalPages, value + 1))}>
              {copy(pageContract, "schedule.drawer.next_page")}
            </button>
          </div>
        </div>
      </aside>
    </div>
  );
}
