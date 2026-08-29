"use client";

import { Clock, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ClockEntryDetail, ClockEventDetail } from "@/lib/api/server";
import { loadClockEntryDetailAction } from "./clock-actions";

/** Reads the selected clocking from the address bar. "" means closed. */
function readClockingParam(): string {
  return new URL(window.location.href).searchParams.get("clocking") ?? "";
}

function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

function flagTone(key: string): Tone {
  switch (key) {
    case "offline":
      return "info";
    case "no_location":
      return "warn";
    case "not_clocked_out":
      return "dng";
    default:
      return "mut";
  }
}

/**
 * The Clock In / Out record drawer: one clocking in full — both punches with
 * device time vs server time, location (with a map link), device identity,
 * connection, and battery. Client state driven by the URL (the
 * LocalOverlayLink contract); the detail is fetched INSIDE the drawer through
 * an authenticated Server Action, never pre-loaded for every list row.
 */
export function ClockEntryDrawer({
  pageContract,
  listHref,
}: {
  pageContract: AdminUiPageContract;
  listHref: string;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readClockingParam, () => "");
  const open = selection !== "";

  const [detail, setDetail] = useState<ClockEntryDetail | null>(null);
  const [error, setError] = useState("");
  const [, startTransition] = useTransition();

  // Reset + refetch during render when the selection moves.
  const [syncedSelection, setSyncedSelection] = useState("");
  if (syncedSelection !== selection) {
    setSyncedSelection(selection);
    setDetail(null);
    setError("");
  }

  useEffect(() => {
    if (!selection) return;
    let active = true;
    startTransition(async () => {
      const result = await loadClockEntryDetailAction(selection);
      if (!active || readClockingParam() !== selection) return;
      if (!result.ok) {
        setError(result.message);
        return;
      }
      setDetail(result.detail);
    });
    return () => {
      active = false;
    };
  }, [selection]);

  const close = useCallback(() => {
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(listHref);
  }, [listHref]);

  useEffect(() => {
    if (!open) return;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, close]);

  const none = copy(pageContract, "clock.value.none");
  const k = (key: string) => copy(pageContract, `clock.drawer.${key}`);
  const entry = detail?.entry;
  const title = entry ? entry.person_name : copy(pageContract, "clock.drawer.title");

  const cell = (label: string, value: string | null | undefined) => (
    <div key={label}>
      <div className="k">{label}</div>
      <div className="v">{value === null || value === undefined || value === "" ? none : value}</div>
    </div>
  );

  const eventBlock = (event: ClockEventDetail) => {
    const coords =
      event.latitude !== undefined && event.longitude !== undefined
        ? `${event.latitude!.toFixed(5)}, ${event.longitude!.toFixed(5)}`
        : "";
    return (
      <div key={event.clock_event_id} style={{ marginBottom: 16 }}>
        <h4 style={{ margin: "10px 0 6px" }}>
          {event.event_type === "clock_in" ? k("event.clock_in") : k("event.clock_out")}
        </h4>
        <div className="metagrid">
          {cell(k("captured_at"), new Date(event.captured_at).toLocaleString("en-IN", { timeZone: "Asia/Kolkata" }))}
          {cell(k("recorded_at"), new Date(event.recorded_at).toLocaleString("en-IN", { timeZone: "Asia/Kolkata" }))}
          {cell(
            k("location"),
            event.location_status === "captured" ? event.address || coords : copy(pageContract, "clock.drawer.no_location"),
          )}
          {cell(k("coordinates"), coords)}
          {cell(k("accuracy"), event.gps_accuracy_m !== undefined ? `${Math.round(event.gps_accuracy_m!)} m` : "")}
          {cell(k("device"), event.device_model ?? "")}
          {cell(k("app_version"), event.app_version ?? "")}
          {cell(k("os"), event.os_version ?? "")}
          {cell(k("network"), copy(pageContract, `clock.drawer.network.${event.network_type}`, event.network_type))}
          {cell(k("battery"), event.battery_pct !== undefined ? `${event.battery_pct}%` : "")}
        </div>
        {coords ? (
          <a
            className="btn sm ghost"
            style={{ marginTop: 8, display: "inline-flex" }}
            href={`https://maps.google.com/?q=${event.latitude},${event.longitude}`}
            target="_blank"
            rel="noreferrer"
          >
            {k("map_link")}
          </a>
        ) : null}
      </div>
    );
  };

  return (
    <>
      <div
        className={`scrim${open ? " on" : ""}`}
        aria-label={copy(pageContract, "action.close")}
        aria-hidden={!open}
        tabIndex={open ? 0 : -1}
        onClick={close}
      />
      <aside className={`drawer${open ? " on" : ""}`} aria-label={title} aria-hidden={!open} inert={!open}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--info)" }}>
            <Clock className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "clock.tab.title")}</div>
            <h2>{title}</h2>
            {entry ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {entry.business_date}
                {entry.designation ? ` · ${entry.designation}` : ""}
                {entry.park_label ? ` · ${entry.park_label}` : ""}
              </div>
            ) : null}
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="btn icon" onClick={close} aria-label={copy(pageContract, "action.close")}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        <div className="dc">
          {error ? (
            <div className="alert" role="alert">
              {error}
            </div>
          ) : null}

          {entry ? (
            <>
              <div className="metagrid" style={{ marginBottom: 8 }}>
                {cell(copy(pageContract, "clock.column.clock_in"), entry.clock_in_label)}
                {cell(copy(pageContract, "clock.column.clock_out"), entry.clock_out_label ?? "")}
                {cell(copy(pageContract, "clock.column.hours"), entry.hours_label ?? "")}
              </div>
              {entry.flags.length > 0 ? (
                <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 10 }}>
                  {entry.flags.map((flag) => (
                    <Tag key={flag.key} tone={flagTone(flag.key)}>
                      {flag.label}
                    </Tag>
                  ))}
                </div>
              ) : null}
              <h3 style={{ margin: "14px 0 4px" }}>{k("punches")}</h3>
              {detail?.events.map(eventBlock)}
            </>
          ) : null}
        </div>
      </aside>
    </>
  );
}
