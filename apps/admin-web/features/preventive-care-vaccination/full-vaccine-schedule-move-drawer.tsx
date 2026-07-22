"use client";

import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ComponentProps } from "react";
import { ThemedDatePicker } from "./themed-date-picker";

export type ScheduleMoveDrawerRow = {
  eventId: string;
  plannedDate: string;
  operatorName: string;
  parkId: string;
  parkName: string;
  animals: number;
  totalDoses: number;
  vaccineCodes: string[];
  vaccineNames: string[];
  returnTo: string;
};

type MoveAction = ComponentProps<"form">["action"];

function selectedMoveIdFromUrl(): string | undefined {
  const url = new URL(window.location.href);
  const hash = new URLSearchParams(url.hash.replace(/^#/, ""));
  return hash.get("schedule_move") ?? url.searchParams.get("schedule_move") ?? undefined;
}

export function ScheduleMoveDrawer({
  rows,
  initialSelectedEventId,
  closeHref,
  pageContract,
  action,
}: {
  rows: ScheduleMoveDrawerRow[];
  initialSelectedEventId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
  action: MoveAction;
}) {
  const initialRow = rows.find((row) => row.eventId === initialSelectedEventId);
  const [displayedRow, setDisplayedRow] = useState<ScheduleMoveDrawerRow | undefined>(initialRow);
  const [drawerOpen, setDrawerOpen] = useState(Boolean(initialRow));
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  const showRow = useCallback((row: ScheduleMoveDrawerRow) => {
    setDisplayedRow(row);
    window.requestAnimationFrame(() => setDrawerOpen(true));
  }, []);

  const hideRow = useCallback(() => setDrawerOpen(false), []);

  useEffect(() => {
    function syncSelectionFromUrl(): void {
      const row = rows.find((item) => item.eventId === selectedMoveIdFromUrl());
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
    if (!drawerOpen) return undefined;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
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

  if (!displayedRow) return null;

  return (
    <div className="schedule-drawer-backdrop schedule-move-backdrop" role="presentation" aria-hidden={!drawerOpen} style={{ opacity: drawerOpen ? 1 : 0 }}>
      <button type="button" className="schedule-drawer-close-layer" aria-label={copy(pageContract, "schedule.move.close")} onClick={closeDrawer} />
      <aside className="schedule-side-drawer schedule-move-drawer" role="dialog" aria-modal="false" aria-hidden={!drawerOpen} inert={!drawerOpen} aria-labelledby="schedule-move-title" style={{ transform: drawerOpen ? "translateX(0)" : "translateX(100%)" }}>
        <div className="schedule-drawer-head">
          <div style={{ minWidth: 0 }}>
            <span className="eyebrow">{fmtDate(displayedRow.plannedDate)} · {displayedRow.operatorName}</span>
            <h3 id="schedule-move-title">{copy(pageContract, "schedule.move.title")}</h3>
            <p className="muted small">
              {displayedRow.parkName} · {displayedRow.animals} {copy(pageContract, "schedule.unit.animals")} · {displayedRow.totalDoses} {copy(pageContract, "schedule.unit.doses")}
            </p>
          </div>
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "schedule.move.close")} onClick={closeDrawer}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>
        <form action={action} className="schedule-move-form">
          <input type="hidden" name="park_id" value={displayedRow.parkId} />
          <input type="hidden" name="original_drive_date" value={displayedRow.plannedDate} />
          <input type="hidden" name="reason" value={copy(pageContract, "schedule.postpone.reason_default")} />
          <input type="hidden" name="return_to" value={displayedRow.returnTo} />
          <label>
            <span>{copy(pageContract, "schedule.postpone.vaccine")}</span>
            <select name="vaccine_code" required>
              {displayedRow.vaccineCodes.map((code, index) => (
                <option key={code} value={code}>{displayedRow.vaccineNames[index] ?? code}</option>
              ))}
            </select>
          </label>
          <label>
            <span>{copy(pageContract, "schedule.postpone.new_date")}</span>
            <ThemedDatePicker
              name="override_date"
              label={copy(pageContract, "schedule.move.date_placeholder")}
              min={displayedRow.plannedDate}
              previousMonthLabel={copy(pageContract, "schedule.move.previous_month")}
              nextMonthLabel={copy(pageContract, "schedule.move.next_month")}
              invalidFutureDateText={copy(pageContract, "schedule.move.invalid_future_date")}
              required
            />
          </label>
          <div className="schedule-move-vaccines">
            {displayedRow.vaccineNames.map((name) => <Tag key={name} tone="teal">{name}</Tag>)}
          </div>
          <button className="btn" type="submit">
            {copy(pageContract, "schedule.postpone.action")}
          </button>
        </form>
      </aside>
    </div>
  );
}
