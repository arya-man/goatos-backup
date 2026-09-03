"use client";

import { Building2, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ProcurementVendor, ProcurementVendorCatalog } from "@/lib/api/server";
import { changeVendorStatusAction, createVendorAction, updateVendorAction } from "./vendor-actions";

type CatalogEntry = { value: string; label: string; is_active: boolean };

/** Reads the selected vendor from the address bar. "" means the drawer is closed. */
function readVendorParam(): string {
  return new URL(window.location.href).searchParams.get("vendor") ?? "";
}

/**
 * Subscribes to both ways the vendor param can change: a LocalOverlayLink click (which dispatches
 * the shared event) and browser Back/Forward (popstate).
 */
function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

function statusTone(status: string): Tone {
  switch (status) {
    case "active":
      return "ok";
    case "negotiating":
      return "warn";
    case "banned":
      return "dng";
    default:
      return "mut";
  }
}

/**
 * Options for one select.
 *
 * A retired entry (is_active false) is dropped UNLESS the vendor being edited already carries it --
 * otherwise the select would silently re-save that vendor with a different value than it had. This
 * is the write-form half of the catalog rule; the filter bar shows everything, because a filter is
 * a read.
 */
function optionsFor(entries: CatalogEntry[] | undefined, current: string | null | undefined): CatalogEntry[] {
  const list = (entries ?? []).filter((e) => e.is_active || (current && e.value === current));
  if (current && !list.some((e) => e.value === current)) {
    list.unshift({ value: current, label: current, is_active: false });
  }
  return list;
}

/**
 * The register's record / add / edit overlay.
 *
 * TWO things about this component were wrong before and are recorded so they are not reintroduced:
 *
 * 1. It must be CLIENT state driven by the URL. LocalOverlayLink deliberately changes history
 *    WITHOUT requesting a new RSC payload, so an overlay gated on a server-read `searchParams`
 *    never appears -- the server component it depends on does not re-run. It also listens on the
 *    SHARED event name exported by local-overlay-link; a privately-invented event string fails
 *    silently, because the link dispatches and nothing is listening.
 *
 * 2. It must use the MOCK's drawer anatomy, and the open class is `.on`. `aside.drawer.on` is the
 *    rule that sets `transform:none` -- without it the panel is translated OFF-SCREEN, so it
 *    mounts, occupies the DOM, and is invisible. `.drawer-scrim` / `.drawer-body` / `.ft` are not
 *    classes this product has; the real ones are `.scrim`, `.dh`, `.dc`, `.df`, and a RECORD body
 *    is a `.metagrid` of `.k`/`.v` cells rather than a flat stack.
 */
export function VendorLocalDrawer({
  vendors,
  catalog,
  pageContract,
  listHref,
}: {
  /** The rendered page of vendors. The drawer opens from this data -- it issues no fetch of its own. */
  vendors: ProcurementVendor[];
  catalog: ProcurementVendorCatalog | null;
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current filters, without the vendor param). */
  listHref: string;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  // The URL is an EXTERNAL store here -- LocalOverlayLink mutates history directly, outside React --
  // so it is subscribed to rather than mirrored into state inside an effect. Copying it with
  // setState-in-an-effect renders once with the stale value and again with the fresh one, which is
  // the cascading render the lint rule flags; useSyncExternalStore reads it during render instead.
  // getServerSnapshot returns "" because the drawer is always closed in SSR output.
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readVendorParam, () => "");

  // Opening "new" starts in the form; opening an existing vendor starts read-only. Tracked as state
  // so the Edit button can switch modes, and reset during render when the selection changes rather
  // than in an effect -- same reason as above.
  const [editing, setEditing] = useState(selection === "new");
  const [syncedSelection, setSyncedSelection] = useState(selection);
  if (syncedSelection !== selection) {
    setSyncedSelection(selection);
    setEditing(selection === "new");
  }

  const close = useCallback(() => {
    setEditing(false);
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    // replaceLocalOverlayUrl notifies the subscription above, so `selection` clears on its own.
    replaceLocalOverlayUrl(listHref);
  }, [listHref]);

  const isAdding = selection === "new";
  const vendor = isAdding ? null : (vendors.find((v) => v.vendor_id === selection) ?? null);
  const open = Boolean(catalog) && (isAdding || vendor !== null);

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

  if (!catalog) return null;

  const none = copy(pageContract, "value.none");
  const field = (key: string) => copy(pageContract, `field.${key}`);
  const title = isAdding
    ? copy(pageContract, "drawer.add.title")
    : editing
      ? copy(pageContract, "drawer.edit.title")
      : copy(pageContract, "drawer.detail.title");

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
            <Building2 className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "crumb")}</div>
            <h2>{isAdding ? title : (vendor?.business_name ?? title)}</h2>
            {vendor ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {vendor.record_type} · {vendor.location_display}
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

        {editing ? (
          <form action={isAdding ? createVendorAction : updateVendorAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              {!isAdding && vendor ? (
                <>
                  <input type="hidden" name="vendor_id" value={vendor.vendor_id} />
                  {/* The optimistic fence, carried from the row this form was opened on. */}
                  <input type="hidden" name="row_version" value={vendor.row_version} />
                </>
              ) : null}

              <div className="note">{copy(pageContract, isAdding ? "required.hint.create" : "required.hint")}</div>

              <div className="fld">
                <label htmlFor="v-business_name">{field("business_name")}</label>
                <input id="v-business_name" name="business_name" required maxLength={160} defaultValue={vendor?.business_name ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-record_type">{field("record_type")}</label>
                <select id="v-record_type" name="record_type" required defaultValue={vendor?.record_type ?? ""}>
                  <option value="" disabled>
                    —
                  </option>
                  {optionsFor(catalog.record_types, vendor?.record_type).map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="v-contact">{field("contact_person_name")}</label>
                <input id="v-contact" name="contact_person_name" required={isAdding} maxLength={160} defaultValue={vendor?.contact_person_name ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-phone">{field("phone_number")}</label>
                <input id="v-phone" name="phone_number" required={isAdding} maxLength={64} defaultValue={vendor?.phone_number ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-status">{field("status")}</label>
                <select id="v-status" name="status" required defaultValue={vendor?.status ?? "active"}>
                  {optionsFor(catalog.statuses, vendor?.status).map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="v-state">{field("state")}</label>
                <select id="v-state" name="state" required defaultValue={vendor?.state ?? ""}>
                  <option value="" disabled>
                    —
                  </option>
                  {optionsFor(catalog.states, vendor?.state).map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="v-city">{field("city")}</label>
                <input id="v-city" name="city" required={isAdding} maxLength={160} defaultValue={vendor?.city ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-breed">{field("breed")}</label>
                <select id="v-breed" name="breed" defaultValue={vendor?.breed ?? ""}>
                  <option value="">—</option>
                  {optionsFor(catalog.breeds, vendor?.breed).map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="v-feed">{field("feed")}</label>
                <select id="v-feed" name="feed" defaultValue={vendor?.feed ?? ""}>
                  <option value="">—</option>
                  {optionsFor(catalog.feeds, vendor?.feed).map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="v-stock">{field("filtered_stock")}</label>
                {/* No default of 0 for an absent reading: blank means "not recorded", 0 means
                    "checked, none available". They are different facts. */}
                <input id="v-stock" name="filtered_stock" type="number" min={0} step={1} defaultValue={vendor?.filtered_stock ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-price">{field("price_per_goat")}</label>
                <input id="v-price" name="price_per_goat" inputMode="decimal" defaultValue={vendor?.price_per_goat ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-eta">{field("eta_after_order")}</label>
                <input id="v-eta" name="eta_after_order_days" type="number" min={0} step={1} defaultValue={vendor?.eta_after_order_days ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-ready">{field("ready_to_filtered")}</label>
                <input id="v-ready" name="ready_to_filtered" maxLength={160} defaultValue={vendor?.ready_to_filtered ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="v-details">{field("details")}</label>
                <textarea id="v-details" name="details" maxLength={2000} rows={2} defaultValue={vendor?.details ?? ""} />
              </div>

              {/* Payment is only editable by a caller who can also READ it. The backend additionally
                  PRESERVES these columns for such a caller, so a blank submit cannot erase a bank
                  account they were never shown. */}
              {vendor?.finance_redacted ? (
                <div className="note">{copy(pageContract, "payment.hidden")}</div>
              ) : (
                <>
                  <div className="fld">
                    <label htmlFor="v-bank">{field("bank_name")}</label>
                    <input id="v-bank" name="bank_name" maxLength={160} defaultValue={vendor?.bank_name ?? ""} />
                  </div>
                  <div className="fld">
                    <label htmlFor="v-account">{field("account_no")}</label>
                    <input id="v-account" name="account_no" maxLength={160} defaultValue={vendor?.account_no ?? ""} />
                  </div>
                  <div className="fld">
                    <label htmlFor="v-ifsc">{field("ifsc_code")}</label>
                    <input id="v-ifsc" name="ifsc_code" maxLength={160} defaultValue={vendor?.ifsc_code ?? ""} />
                  </div>
                  <div className="fld">
                    <label htmlFor="v-upi">{field("upi_id")}</label>
                    <input id="v-upi" name="upi_id" maxLength={160} defaultValue={vendor?.upi_id ?? ""} />
                  </div>
                  <div className="fld">
                    <label htmlFor="v-pan">{field("pan_number")}</label>
                    <input id="v-pan" name="pan_number" maxLength={160} defaultValue={vendor?.pan_number ?? ""} />
                  </div>
                </>
              )}

              <div className="fld">
                <label htmlFor="v-comments">{field("comments")}</label>
                <textarea id="v-comments" name="comments" maxLength={2000} rows={2} defaultValue={vendor?.comments ?? ""} />
              </div>
            </div>
            <div className="df">
              <button type="submit" className="btn p">
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={() => (isAdding ? close() : setEditing(false))}>
                {copy(pageContract, "action.cancel")}
              </button>
            </div>
          </form>
        ) : vendor ? (
          <>
            <div className="dc">
              {/* RECORD drawer body: the mock's .metagrid of uppercase-key cells, never a flat stack. */}
              <div className="metagrid">
                {cell(field("record_type"), vendor.record_type)}
                <div>
                  <div className="k">{field("status")}</div>
                  <div className="v">
                    <Tag tone={statusTone(vendor.status)}>{vendor.status_label}</Tag>
                  </div>
                </div>
                {cell(field("contact_person_name"), vendor.contact_person_name)}
                {cell(field("phone_number"), vendor.phone_number)}
                {cell(field("state"), vendor.state)}
                {cell(field("city"), vendor.city)}
                {cell(field("breed"), vendor.breed)}
                {cell(field("feed"), vendor.feed)}
                {cell(field("filtered_stock"), vendor.filtered_stock)}
                {cell(field("price_per_goat"), vendor.price_per_goat)}
                {cell(field("eta_after_order"), vendor.eta_after_order_days)}
                {cell(field("ready_to_filtered"), vendor.ready_to_filtered)}
                {cell(field("details"), vendor.details)}
                {cell(field("comments"), vendor.comments)}
              </div>

              <div className="mt" style={{ marginTop: 4 }}>
                {copy(pageContract, "group.payment")}
              </div>
              {vendor.finance_redacted ? (
                // Never a blank block: withheld and absent must not look the same.
                <div className="note">{copy(pageContract, "payment.hidden")}</div>
              ) : vendor.bank_name || vendor.account_no || vendor.ifsc_code || vendor.upi_id || vendor.pan_number ? (
                <div className="metagrid">
                  {cell(field("bank_name"), vendor.bank_name)}
                  {cell(field("account_no"), vendor.account_no)}
                  {cell(field("ifsc_code"), vendor.ifsc_code)}
                  {cell(field("upi_id"), vendor.upi_id)}
                  {cell(field("pan_number"), vendor.pan_number)}
                </div>
              ) : (
                <div className="note">{copy(pageContract, "payment.none")}</div>
              )}
            </div>

            {/* Quick status change, without opening the full form. It posts to the NARROW status
                endpoint, so it cannot clear a field this view did not render. */}
            <div className="df">
              <button type="button" className="btn p" onClick={() => setEditing(true)}>
                {copy(pageContract, "action.edit")}
              </button>
              <form action={changeVendorStatusAction} style={{ display: "flex", gap: 8, alignItems: "center", marginLeft: "auto" }}>
                <input type="hidden" name="return_to" value={listHref} />
                <input type="hidden" name="vendor_id" value={vendor.vendor_id} />
                <input type="hidden" name="row_version" value={vendor.row_version} />
                <select name="status" defaultValue={vendor.status} aria-label={field("status")} style={{ padding: "7px 9px", borderRadius: 9 }}>
                  {optionsFor(catalog.statuses, vendor.status).map((e) => (
                    <option key={e.value} value={e.value}>
                      {e.label}
                    </option>
                  ))}
                </select>
                <button type="submit" className="btn">
                  {copy(pageContract, "action.save_status")}
                </button>
              </form>
            </div>
          </>
        ) : null}
      </aside>
    </>
  );
}
