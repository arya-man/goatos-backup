"use client";

import { Download, Loader2, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  LocalOverlayLink,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { exportWeightsCsvAction } from "./weights-export-action";

export type WeightsExportPark = { park_id: string; name: string };

export type WeightsExportShed = {
  location_id: string;
  label: string;
  /** The owning park, so the shed list follows the drawer's park select. */
  park_id: string;
};

const EXPORT_PARAM = "wt_export";

/** Reads the export flag from the address bar. "" means the drawer is closed. */
function readExportParam(): string {
  return new URL(window.location.href).searchParams.get(EXPORT_PARAM) ?? "";
}

/**
 * Subscribes to both ways the export param can change: a LocalOverlayLink click (which
 * dispatches the shared event) and browser Back/Forward (popstate).
 */
function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

/**
 * The Weights page's Download control: a button in the page header that opens a
 * right-side drawer holding the export selection — the period calendar, a park
 * select and a shed list — and hands the finished CSV to the browser.
 *
 * Same overlay mechanics as the sales record drawer: CLIENT state driven by the URL
 * (LocalOverlayLink changes history WITHOUT an RSC request), the mock's drawer
 * anatomy — `.scrim`/`.drawer.on`, `.dh`/`.dc`/`.df` — and SSR always renders it
 * closed. The FILE itself is fetched on click through a server action, because a
 * year of weighings is not something the page should carry on every load.
 */
export function WeightsExportControl({
  pageContract,
  parks,
  sheds,
  initialParkId,
  initialFrom,
  initialTo,
  today,
  openHref,
  closeHref,
}: {
  pageContract: AdminUiPageContract;
  parks: WeightsExportPark[];
  sheds: WeightsExportShed[];
  /** The page's current park filter, so the drawer opens on the scope the reader is looking at. */
  initialParkId: string;
  /** The page's selected window, both ends "YYYY-MM-DD" Asia/Kolkata business dates. */
  initialFrom: string;
  initialTo: string;
  today: string;
  /** Real deep links for new tabs / no-JS; ordinary clicks stay client-local. */
  openHref: string;
  closeHref: string;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  // The URL is an EXTERNAL store — LocalOverlayLink mutates history outside React — so it is
  // read via useSyncExternalStore rather than mirrored into state. SSR renders the drawer closed.
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readExportParam, () => "");
  const open = selection !== "";

  const [from, setFrom] = useState(initialFrom);
  const [to, setTo] = useState(initialTo);
  const [parkId, setParkId] = useState(initialParkId);
  // Empty set = every shed ("All sheds"), which is also what the backend receives.
  const [selectedSheds, setSelectedSheds] = useState<ReadonlySet<string>>(new Set());
  const [failed, setFailed] = useState(false);
  const [pending, startTransition] = useTransition();

  const close = useCallback(() => {
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

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

  const title = copy(pageContract, "export.title");
  const parkNameById = new Map(parks.map((park) => [park.park_id, park.name]));
  const visibleSheds = parkId === "" ? sheds : sheds.filter((shed) => shed.park_id === parkId);
  const visibleShedIds = new Set(visibleSheds.map((shed) => shed.location_id));
  // Only sheds of the selected park travel: a selection made under "All parks" must not
  // silently pin the export to another park's pens after the park select narrows.
  const effectiveShedIds = [...selectedSheds].filter((id) => visibleShedIds.has(id));
  const allSheds = effectiveShedIds.length === 0;

  function toggleShed(locationId: string): void {
    setSelectedSheds((previous) => {
      const next = new Set(previous);
      if (next.has(locationId)) next.delete(locationId);
      else next.add(locationId);
      return next;
    });
  }

  const rangeLabels: DateRangePickerLabels = {
    field: copy(pageContract, "export.period.label"),
    today: copy(pageContract, "filter.period.today"),
    single: copy(pageContract, "filter.period.single"),
    range: copy(pageContract, "filter.period.range"),
    aria: copy(pageContract, "filter.period.aria"),
    previousMonth: copy(pageContract, "filter.period.previous_month"),
    nextMonth: copy(pageContract, "filter.period.next_month"),
    rangeStartHint: copy(pageContract, "filter.period.range_start_hint"),
    rangeEndHint: copy(pageContract, "filter.period.range_end_hint"),
    rangeSeparator: copy(pageContract, "filter.period.range_separator"),
  };

  function download(): void {
    setFailed(false);
    startTransition(async () => {
      const result = await exportWeightsCsvAction({
        from,
        to,
        parkId: parkId || undefined,
        shedIds: allSheds ? undefined : effectiveShedIds,
      });
      if (!result.ok) {
        setFailed(true);
        return;
      }
      writeCsvFile(`weights-${from}-${to}.csv`, result.csv);
    });
  }

  return (
    <>
      <LocalOverlayLink href={openHref} className="btn sm" scroll={false} aria-haspopup="dialog">
        <Download className="ic" aria-hidden="true" /> {copy(pageContract, "export.button")}
      </LocalOverlayLink>

      <div
        className={`scrim${open ? " on" : ""}`}
        aria-label={copy(pageContract, "filter.clear_all")}
        aria-hidden={!open}
        tabIndex={open ? 0 : -1}
        onClick={close}
      />
      <aside className={`drawer${open ? " on" : ""}`} aria-label={title} aria-hidden={!open} inert={!open}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Download className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "export.eyebrow")}</div>
            <h2>{title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={title} onClick={close}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        <div className="dc">
          <p className="muted small" style={{ marginTop: 0 }}>
            {copy(pageContract, "export.hint")}
          </p>

          <div className="fld">
            <label>{copy(pageContract, "export.period.label")}</label>
            <DateRangePicker
              labels={rangeLabels}
              from={from}
              to={to}
              today={today}
              busy={pending}
              onChange={(nextFrom, nextTo) => {
                setFrom(nextFrom);
                setTo(nextTo);
              }}
            />
          </div>

          <div className="fld">
            <label htmlFor="wt-export-park">{copy(pageContract, "export.park.label")}</label>
            <select
              id="wt-export-park"
              value={parkId}
              onChange={(event) => setParkId(event.target.value)}
              disabled={pending}
            >
              <option value="">{copy(pageContract, "export.park.all")}</option>
              {parks.map((park) => (
                <option key={park.park_id} value={park.park_id}>
                  {park.name}
                </option>
              ))}
            </select>
          </div>

          <fieldset className="fld" style={{ border: 0, padding: 0, margin: 0 }}>
            <legend>
              <label>{copy(pageContract, "export.sheds.label")}</label>
            </legend>
            <label className="wt-export-shed-row">
              <input
                type="checkbox"
                checked={allSheds}
                disabled={pending}
                onChange={() => setSelectedSheds(new Set())}
              />
              {copy(pageContract, "export.sheds.all")}
            </label>
            <div className="wt-export-shed-list">
              {visibleSheds.map((shed) => (
                <label key={shed.location_id} className="wt-export-shed-row">
                  <input
                    type="checkbox"
                    checked={selectedSheds.has(shed.location_id)}
                    disabled={pending}
                    onChange={() => toggleShed(shed.location_id)}
                  />
                  {/* With no park selected the park travels on the row: 39 shed names exist in
                      BOTH parks, so a bare shed name would appear twice, indistinguishably. */}
                  {parkId === "" ? `${parkNameById.get(shed.park_id) ?? ""} · ${shed.label}` : shed.label}
                </label>
              ))}
            </div>
          </fieldset>

          {failed ? <p className="small warn">{copy(pageContract, "export.error")}</p> : null}
        </div>

        <div className="df">
          <button type="button" className="btn primary" onClick={download} disabled={pending} aria-busy={pending}>
            {pending ? <Loader2 className="ic wt-export-spin" aria-hidden="true" /> : <Download className="ic" aria-hidden="true" />}
            {pending ? copy(pageContract, "export.preparing") : copy(pageContract, "export.download")}
          </button>
        </div>
      </aside>
    </>
  );
}

/** Hands the CSV text to the browser as a saved file. */
function writeCsvFile(filename: string, csv: string): void {
  // The BOM makes Excel read this as UTF-8. Without it a shed name with an accent, or any
  // non-ASCII operator name, opens as mojibake.
  const blob = new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // Revoking on the next tick rather than immediately: Safari has not always started the
  // download by the time click() returns, and revoking too early cancels it.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
