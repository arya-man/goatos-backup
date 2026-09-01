"use client";

import { Boxes, X } from "lucide-react";
import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LoadwiseLoad } from "@/lib/api/procurement";
import { humanDate, inr, num } from "./sales-format";
import { recordLoadCostAction } from "./sales-actions";

/** Reads the selected load from the address bar. "" means the drawer is closed. */
function readCostLoadParam(): string {
  return new URL(window.location.href).searchParams.get("cost_load") ?? "";
}

function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

/**
 * The load-wise section's cost drawer: what one animal load cost to buy and bring in. Modeled on
 * the sales/feed drawers — CLIENT state driven by the URL (LocalOverlayLink changes history
 * WITHOUT an RSC request) and the mock's drawer anatomy (`.scrim`/`.drawer.on`, `.dh`/`.dc`/`.df`).
 *
 * The form is a PUT of the full cost state: it opens prefilled with the load's recorded values, a
 * blank stays null (never coerced to 0), and clearing every box clears the recorded cost.
 */
export function LoadCostDrawer({
  loads,
  pageContract,
  listHref,
  canRecordCost,
}: {
  /** The rendered load-wise rows. The drawer opens from this data — it issues no fetch of its own. */
  loads: LoadwiseLoad[];
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current tab/farm, without the cost_load param). */
  listHref: string;
  /** Backend-declared record_load_cost capability; without it the form never renders. */
  canRecordCost: boolean;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readCostLoadParam, () => "");
  const load = selection ? (loads.find((l) => l.load_id === selection) ?? null) : null;
  const open = load !== null;

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

  const none = copy(pageContract, "value.none");
  const field = (key: string) => copy(pageContract, `field.${key}`);
  const title = copy(pageContract, "drawer.load_cost.title");

  // One read-only cell pair of the record body.
  const cell = (label: string, value: string | number | null | undefined) => (
    <div key={label}>
      <div className="k">{label}</div>
      <div className="v">{value === null || value === undefined || value === "" ? none : value}</div>
    </div>
  );

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
            <Boxes className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "crumb")}</div>
            <h2>
              {load
                ? load.load_ref
                  ? `${copy(pageContract, "column.load")} ${load.load_ref} · ${load.vendor_name || copy(pageContract, "value.none")}`
                  : load.vendor_name.trim() === ""
                    ? title
                    : load.vendor_name
                : title}
            </h2>
            {load?.purchase_date ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {humanDate(load.purchase_date)}
                {load.farm ? ` · ${load.farm}` : ""}
              </div>
            ) : null}
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button
            ref={closeButtonRef}
            type="button"
            className="iconbtn"
            aria-label={copy(pageContract, "action.close")}
            onClick={close}
          >
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        {load ? (
          <form action={recordLoadCostAction} style={{ display: "contents" }} key={load.load_id}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <input type="hidden" name="load_id" value={load.load_id} />

              {/* The reconciliation the cost is being recorded against, read-only. */}
              <div className="metagrid">
                {cell(copy(pageContract, "column.purchased"), num(load.purchased))}
                {cell(copy(pageContract, "column.sold"), num(load.sold))}
                {cell(copy(pageContract, "column.mortality"), num(load.mortality))}
                {cell(copy(pageContract, "column.remaining"), num(load.remaining))}
                {/* Culled / transferred / lost. The table has no column for it, so this is where
                    a load that HAS other exits still shows them. */}
                {load.other_exits > 0 ? cell(copy(pageContract, "column.other_exits"), num(load.other_exits)) : null}
                {cell(
                  copy(pageContract, "column.sold_value"),
                  load.sold_value > 0 ? inr(Math.round(load.sold_value)) : none,
                )}
                <div>
                  <div className="k">{copy(pageContract, "column.unaccounted")}</div>
                  <div className="v">
                    {load.unaccounted === 0 ? <Tag tone="mut">0</Tag> : <Tag tone="dng">{num(load.unaccounted)}</Tag>}
                  </div>
                </div>
              </div>

              {/* The load's pre-system history, with the dates the old records span. */}
              {load.prior_sold || load.prior_dead ? (
                <div style={{ marginTop: 10 }}>
                  <div className="mt">{copy(pageContract, "loadwise.prior.title")}</div>
                  {load.prior_sold ? (
                    <div className="muted small">
                      {copy(pageContract, "loadwise.prior.sold")}: <b>{num(load.prior_sold.count)}</b>{" "}
                      {copy(pageContract, "loadwise.prior.animals")}
                      {load.prior_sold.value ? <> · {inr(Math.round(load.prior_sold.value))}</> : null}
                      {load.prior_sold.first_on ? (
                        <>
                          {" "}· {humanDate(load.prior_sold.first_on)}
                          {load.prior_sold.last_on && load.prior_sold.last_on !== load.prior_sold.first_on
                            ? ` – ${humanDate(load.prior_sold.last_on)}`
                            : ""}
                        </>
                      ) : null}
                    </div>
                  ) : null}
                  {load.prior_dead ? (
                    <div className="muted small">
                      {copy(pageContract, "loadwise.prior.died")}: <b>{num(load.prior_dead.count)}</b>{" "}
                      {copy(pageContract, "loadwise.prior.animals")}
                      {load.prior_dead.first_on ? (
                        <>
                          {" "}· {humanDate(load.prior_dead.first_on)}
                          {load.prior_dead.last_on && load.prior_dead.last_on !== load.prior_dead.first_on
                            ? ` – ${humanDate(load.prior_dead.last_on)}`
                            : ""}
                        </>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              ) : null}

              <div className="note" style={{ marginTop: 10 }}>
                {copy(pageContract, "hint.load_cost")}
              </div>

              <div className="fld">
                <label htmlFor="lc-animal_cost">{field("animal_cost")}</label>
                <input
                  id="lc-animal_cost"
                  name="animal_cost"
                  type="number"
                  min={0}
                  step="0.01"
                  disabled={!canRecordCost}
                  defaultValue={load.animal_cost ?? ""}
                />
              </div>
              <div className="fld">
                <label htmlFor="lc-transport_cost">{field("transport_cost")}</label>
                <input
                  id="lc-transport_cost"
                  name="transport_cost"
                  type="number"
                  min={0}
                  step="0.01"
                  disabled={!canRecordCost}
                  defaultValue={load.transport_cost ?? ""}
                />
              </div>
              <div className="fld">
                <label htmlFor="lc-other_cost">{field("other_cost")}</label>
                <input
                  id="lc-other_cost"
                  name="other_cost"
                  type="number"
                  min={0}
                  step="0.01"
                  disabled={!canRecordCost}
                  defaultValue={load.other_cost ?? ""}
                />
              </div>
            </div>
            {canRecordCost ? (
              <div className="df">
                <button type="submit" className="btn primary">
                  {copy(pageContract, "action.record_load_cost.label")}
                </button>
              </div>
            ) : (
              <div className="df">
                <span className="muted small">{copy(pageContract, "disabled.load_cost")}</span>
              </div>
            )}
          </form>
        ) : null}
      </aside>
    </>
  );
}
