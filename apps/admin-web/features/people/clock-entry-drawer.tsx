"use client";

import { useCallback, useEffect, useState, useSyncExternalStore, useTransition, type ReactNode } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";

import { MinimalDrawer } from "@/components/minimal/drawer";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ClockEntryDetail, ClockEventDetail } from "@/lib/api/server";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { loadClockEntryDetailAction } from "./clock-actions";
import Alert from "@mui/material/Alert";

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

/** A label / value cell of the record grid (template drawer detail rows: caption over value). */
function MetaCell({ label, children }: { label: string; children: ReactNode }) {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography variant="caption" component="div" sx={{ color: "text.secondary", fontWeight: "fontWeightSemiBold" }}>
        {label}
      </Typography>
      <Typography variant="subtitle2" component="div" sx={{ overflowWrap: "anywhere" }}>
        {children}
      </Typography>
    </Box>
  );
}

const metaGridSx = { display: "grid", gridTemplateColumns: "repeat(2, minmax(0, 1fr))", gap: 2 } as const;

/**
 * The Clock In / Out record drawer: one clocking in full — both punches with
 * device time vs server time, location (with a map link), device identity,
 * connection, and battery. Client state driven by the URL (the
 * LocalOverlayLink contract, so Back closes it); the detail is fetched INSIDE the drawer through
 * an authenticated Server Action, never pre-loaded for every list row. Shell: the template
 * MinimalDrawer (portal, focus trap, Escape / scrim / X close, focus back on the opener).
 */
export function ClockEntryDrawer({
  pageContract,
  listHref,
}: {
  pageContract: AdminUiPageContract;
  listHref: string;
}) {
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

  const none = copy(pageContract, "clock.value.none");
  const k = (key: string) => copy(pageContract, `clock.drawer.${key}`);
  const entry = detail?.entry;
  const title = entry ? entry.person_name : copy(pageContract, "clock.drawer.title");

  const cell = (label: string, value: string | null | undefined) => (
    <MetaCell key={label} label={label}>
      {value === null || value === undefined || value === "" ? none : value}
    </MetaCell>
  );

  const eventBlock = (event: ClockEventDetail) => {
    const coords =
      event.latitude !== undefined && event.longitude !== undefined
        ? `${event.latitude!.toFixed(5)}, ${event.longitude!.toFixed(5)}`
        : "";
    return (
      <Box key={event.clock_event_id} sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
        <Typography variant="subtitle1" component="h4">
          {event.event_type === "clock_in" ? k("event.clock_in") : k("event.clock_out")}
        </Typography>
        <Box sx={metaGridSx}>
          {cell(k("captured_at"), fmtDateTime(event.captured_at))}
          {cell(k("recorded_at"), fmtDateTime(event.recorded_at))}
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
        </Box>
        {coords ? (
          <Box>
            <Button
              size="small"
              variant="outlined"
              color="inherit"
              href={`https://maps.google.com/?q=${event.latitude},${event.longitude}`}
              target="_blank"
              rel="noreferrer"
            >
              {k("map_link")}
            </Button>
          </Box>
        ) : null}
      </Box>
    );
  };

  return (
    <MinimalDrawer
      open={open}
      onClose={close}
      title={title}
      width={380}
      closeLabel={copy(pageContract, "action.close")}
      aria-label={title}
    >
      <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2.5 }}>
        <Box>
          <Typography variant="caption" component="div" sx={{ color: "text.secondary", fontWeight: "fontWeightSemiBold" }}>
            {copy(pageContract, "clock.tab.title")}
          </Typography>
          {entry ? (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {fmtDate(entry.business_date)}
              {entry.designation ? ` · ${entry.designation}` : ""}
              {entry.park_label ? ` · ${entry.park_label}` : ""}
            </Typography>
          ) : null}
        </Box>

        {error ? (
          <Alert severity="error" role="alert">
            {error}
          </Alert>
        ) : null}

        {entry ? (
          <>
            <Box sx={{ ...metaGridSx, gridTemplateColumns: "repeat(3, minmax(0, 1fr))" }}>
              {cell(copy(pageContract, "clock.column.clock_in"), entry.clock_in_label)}
              {cell(copy(pageContract, "clock.column.clock_out"), entry.clock_out_label ?? "")}
              {cell(copy(pageContract, "clock.column.hours"), entry.hours_label ?? "")}
            </Box>
            {entry.flags.length > 0 ? (
              <Box sx={{ display: "flex", gap: 0.75, flexWrap: "wrap" }}>
                {entry.flags.map((flag) => (
                  <Tag key={flag.key} tone={flagTone(flag.key)}>
                    {flag.label}
                  </Tag>
                ))}
              </Box>
            ) : null}
            <Typography variant="h6" component="h3">{k("punches")}</Typography>
            {detail?.events.map(eventBlock)}
          </>
        ) : null}
      </Box>
    </MinimalDrawer>
  );
}
