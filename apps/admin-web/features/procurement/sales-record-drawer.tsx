"use client";

import { Banknote, X } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { controlEnabled, copy, optionalOptionGroup, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesDeal } from "@/lib/api/procurement";
import type { ProcurementVendorOption, ProcurementVendorOptions } from "@/lib/api/server";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { dealStatusTone, inr, num } from "./sales-format";
import { recordSaleAction, recordSalesDealPaymentAction, setSalesDealStatusAction } from "./sales-actions";

/** Reads the selected deal from the address bar. "" means the drawer is closed; "new" is the form. */
function readDealParam(): string {
  return new URL(window.location.href).searchParams.get("deal_id") ?? "";
}

/**
 * Subscribes to both ways the deal param can change: a LocalOverlayLink click (which dispatches the
 * shared event) and browser Back/Forward (popstate).
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
 * How one vendor reads in the picker: the business name, then where they are.
 *
 * Two vendors really do share a business name in the register, so the location is what tells them
 * apart. It is appended only when the register actually carries one -- a trailing " · " on a vendor
 * with no city or state would read as missing data rather than absent data.
 */
function vendorLabel(vendor: ProcurementVendorOption): string {
  const place = [vendor.city, vendor.state].filter((part) => part.trim() !== "").join(", ");
  return place === "" ? vendor.business_name : `${vendor.business_name} · ${place}`;
}

/**
 * The sales board's record / detail overlay, modeled on the vendor register drawer: CLIENT state
 * driven by the URL (LocalOverlayLink changes history WITHOUT an RSC request, so a server-read
 * search param would never open it), and the mock's drawer anatomy — `.scrim`/`.drawer.on`,
 * `.dh`/`.dc`/`.df`, with a RECORD body as a `.metagrid` of `.k`/`.v` cells.
 */
export function SalesRecordDrawer({
  deals,
  pageContract,
  listHref,
  canRecord,
  vendorOptions,
}: {
  /** The rendered ledger page. The detail view opens from this data — it issues no fetch of its own. */
  deals: SalesDeal[];
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current farm/paging, without the deal param). */
  listHref: string;
  /** Backend-declared record_sale capability; without it the form never renders. */
  canRecord: boolean;
  /**
   * The ACTIVE vendor register, read with the page. `null` means the register could not be READ
   * (it sits behind its own permission) -- a different fact from an EMPTY register, and the two
   * get different copy: one says the list is unavailable, the other says to go add the buyer.
   */
  vendorOptions: ProcurementVendorOptions | null;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  // The URL is an EXTERNAL store — LocalOverlayLink mutates history outside React — so it is read
  // via useSyncExternalStore rather than mirrored into state in an effect. SSR always renders the
  // drawer closed.
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readDealParam, () => "");

  const isAdding = selection === "new" && canRecord;
  const deal = selection && selection !== "new" ? (deals.find((d) => d.deal_id === selection) ?? null) : null;
  const open = isAdding || deal !== null;

  // The receipt write is a backend capability, never a role string: the same detail view serves a
  // read-only principal (no form) and the sales desk (form).
  const canRecordPayment = controlEnabled(pageContract, "record_sales_deal_payment", false);
  const canEditStatus = controlEnabled(pageContract, "update_sales_deal_status", false);
  const dealStatusOptions = optionGroup(pageContract, "sales_deal_statuses");
  // Where the payment action returns to: the SAME deal, so the drawer reopens showing the new
  // receipt rather than closing over the operator's work.
  const dealHref = deal
    ? `${listHref}${listHref.includes("?") ? "&" : "?"}deal_id=${encodeURIComponent(deal.deal_id)}`
    : listHref;

  // The breed vocabulary follows the selected product. Tracked as state so changing the product
  // select swaps the breed group; reset during render when the selection changes, not in an effect.
  const productOptions = optionGroup(pageContract, "sales_product_types");
  const [product, setProduct] = useState(productOptions[0]?.key ?? "");
  // The vendor IS the buyer, so picking one fills the buyer snapshot fields. They stay EDITABLE
  // (maintainer decision 2026-08-27): the sale is still recorded under the name it was made in,
  // which may differ from the register's spelling, and buyer_name is what the ledger and the buyer
  // board have always shown. The vendor id is the durable link; these two are the snapshot.
  const [vendorId, setVendorId] = useState("");
  const [vendorQuery, setVendorQuery] = useState("");
  const [buyerName, setBuyerName] = useState("");
  const [buyerPlace, setBuyerPlace] = useState("");
  const [syncedSelection, setSyncedSelection] = useState(selection);
  if (syncedSelection !== selection) {
    // Reset during render when the drawer opens on a different record, never in an effect -- an
    // effect would let one submit's values flash into the next form.
    setSyncedSelection(selection);
    setProduct(productOptions[0]?.key ?? "");
    setVendorId("");
    setVendorQuery("");
    setBuyerName("");
    setBuyerPlace("");
  }

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
  const title = isAdding ? copy(pageContract, "drawer.record_sale.title") : copy(pageContract, "drawer.detail.title");

  // The write vocabulary excludes the read-scope "all" entry: a deal happens at ONE farm.
  const farmOptions = optionGroup(pageContract, "sales_farms").filter((option) => option.key !== "all");
  const breedOptions = optionalOptionGroup(pageContract, `sales_breeds_${product.toLowerCase()}`);

  // Three distinct states, and they are NOT the same fact:
  //   unavailable -> the register could not be read (its own permission failed, or the API errored)
  //   empty       -> the register was read and holds no active vendor
  //   ready       -> there is someone to sell to
  // Collapsing the first two would tell a person to go add a vendor that already exists, or leave
  // them staring at an empty dropdown with no reason.
  const vendorsUnavailable = vendorOptions === null;
  const vendors = vendorOptions?.vendors ?? [];
  const vendorsTruncated = vendorOptions?.truncated === true;
  const vendorsEmpty = !vendorsUnavailable && vendors.length === 0;
  const canPickVendor = !vendorsUnavailable && !vendorsEmpty && !vendorsTruncated;
  const selectedVendor = vendors.find((vendor) => vendor.vendor_id === vendorId) ?? null;

  // A few hundred vendors is more than anyone scrolls, so typing narrows the list. TWO characters
  // is the floor: a single letter matches most of the register and would make the field feel
  // broken rather than filtered.
  const trimmedQuery = vendorQuery.trim().toLowerCase();
  const searchActive = trimmedQuery.length >= 2;
  const matchedVendors = searchActive
    ? vendors.filter((vendor) =>
        // Name AND place, because "the Anantapur one" is how a buyer is remembered at least as
        // often as by business name.
        [vendor.business_name, vendor.city, vendor.state]
          .join(" ")
          .toLowerCase()
          .includes(trimmedQuery),
      )
    : vendors;
  // The chosen vendor stays in the list even when the search excludes it. Dropping it would clear
  // the select silently and submit a sale against no vendor -- the search is a lens over the
  // options, never an edit to the selection.
  const shownVendors =
    selectedVendor && !matchedVendors.some((vendor) => vendor.vendor_id === selectedVendor.vendor_id)
      ? [selectedVendor, ...matchedVendors]
      : matchedVendors;
  const noMatches = searchActive && matchedVendors.length === 0;

  const onVendorChange = (nextVendorId: string) => {
    setVendorId(nextVendorId);
    const vendor = vendors.find((candidate) => candidate.vendor_id === nextVendorId);
    if (!vendor) return;
    // Prefill both snapshot fields from the vendor. Overwriting rather than filling-only-if-blank
    // is deliberate: changing the vendor mid-form must not leave the PREVIOUS buyer's name sitting
    // in the box, which is exactly how a sale gets recorded against the wrong counterparty.
    setBuyerName(vendor.business_name);
    setBuyerPlace([vendor.city, vendor.state].filter((part) => part.trim() !== "").join(", "));
  };

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
            <Banknote className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "crumb")}</div>
            <h2>{isAdding ? title : (deal?.buyer_name ?? title)}</h2>
            {deal ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {fmtDate(deal.sale_date)} · {deal.farm}
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

        {isAdding ? (
          <form action={recordSaleAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />

              <div className="note">{copy(pageContract, "required.hint")}</div>

              <div className="fld">
                <label htmlFor="s-sale_date">{field("sale_date")}</label>
                {/* The app's shared date control -- the same one the vaccination schedule uses --
                    rather than a native input, whose browser-drawn calendar and locale date order
                    match nothing else on the page. The max used to be TODAY ("a sale may be
                    recorded after it happened, never before") -- retired 2026-08-31: an EXPECTED
                    sale (status Advance Paid / In Discussion, advance already in hand) is dated by
                    the day the animals are due to leave, which is in the future. Bounded to a
                    60-day horizon so a typo cannot date a sale into next year. */}
                <ThemedDatePicker
                  name="sale_date"
                  label={copy(pageContract, "date.sale_date.placeholder")}
                  max={istDayPlus(todayIso(), 60)}
                  previousMonthLabel={copy(pageContract, "date.prev_month")}
                  nextMonthLabel={copy(pageContract, "date.next_month")}
                  invalidDateText={copy(pageContract, "date.invalid_sale_date")}
                  required
                />
              </div>
              <div className="fld">
                <label htmlFor="s-farm">{field("farm")}</label>
                <select id="s-farm" name="farm" required defaultValue="">
                  <option value="" disabled>
                    —
                  </option>
                  {farmOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="s-product_type">{field("product_type")}</label>
                <select
                  id="s-product_type"
                  name="product_type"
                  required
                  value={product}
                  onChange={(event) => setProduct(event.target.value)}
                >
                  {productOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="s-breed">{field("breed")}</label>
                {/* key remounts the select when the product changes, so a breed from the previous
                    product's vocabulary can never ride along into the submit. */}
                <select id="s-breed" name="breed" required defaultValue="" key={product}>
                  <option value="" disabled>
                    —
                  </option>
                  {breedOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="s-buyer_vendor_id">{field("vendor")}</label>
                {vendorsUnavailable ? (
                  // The register could not be READ. Say that, rather than render an empty dropdown
                  // that reads as "no vendors exist" and sends the person to add a duplicate.
                  <div className="note warn">{copy(pageContract, "error.vendors_unavailable")}</div>
                ) : vendorsTruncated ? (
                  <>
                    <div className="note warn">{copy(pageContract, "hint.vendor_truncated")}</div>
                    <Link href="/procurement/vendors" className="btn sm">
                      {copy(pageContract, "action.open_vendors")}
                    </Link>
                  </>
                ) : vendorsEmpty ? (
                  <>
                    <div className="note">{copy(pageContract, "hint.vendor_empty")}</div>
                    <Link href="/procurement/vendors" className="btn sm">
                      {copy(pageContract, "action.open_vendors")}
                    </Link>
                  </>
                ) : (
                  <>
                    <div className="sales-vendor-search">
                      <input
                        id="s-vendor_search"
                        type="search"
                        // NOT part of the form payload: this filters the options and is never
                        // submitted. The select below still carries the id that gets recorded.
                        name="vendor_search"
                        autoComplete="off"
                        placeholder={copy(pageContract, "search.vendor.placeholder")}
                        aria-controls="s-buyer_vendor_id"
                        value={vendorQuery}
                        onChange={(event) => setVendorQuery(event.target.value)}
                      />
                      {vendorQuery === "" ? null : (
                        <button type="button" className="btn sm" onClick={() => setVendorQuery("")}>
                          {copy(pageContract, "action.clear_search")}
                        </button>
                      )}
                    </div>
                    <select
                      id="s-buyer_vendor_id"
                      name="buyer_vendor_id"
                      required
                      // Sized to show several rows at once while filtering, so a narrowed list
                      // reads as a result set rather than a one-line box.
                      size={searchActive ? Math.min(8, Math.max(2, shownVendors.length + 1)) : undefined}
                      value={vendorId}
                      onChange={(event) => onVendorChange(event.target.value)}
                    >
                      <option value="" disabled>
                        {copy(pageContract, "select.vendor.placeholder")}
                      </option>
                      {shownVendors.map((vendor) => (
                        <option key={vendor.vendor_id} value={vendor.vendor_id}>
                          {vendorLabel(vendor)}
                        </option>
                      ))}
                    </select>
                    {noMatches ? <div className="note">{copy(pageContract, "hint.vendor_no_match")}</div> : null}
                    {/* The exit from a required field the person may not be able to fill: the
                        buyer might simply not be on the register yet. */}
                    <div className="note sales-vendor-hint">
                      {copy(pageContract, "hint.vendor")}{" "}
                      <Link href="/procurement/vendors">{copy(pageContract, "action.open_vendors")}</Link>
                    </div>
                  </>
                )}
              </div>
              <div className="fld">
                <label htmlFor="s-buyer_name">{field("buyer_name")}</label>
                <input
                  id="s-buyer_name"
                  name="buyer_name"
                  required
                  maxLength={160}
                  value={buyerName}
                  onChange={(event) => setBuyerName(event.target.value)}
                />
              </div>
              <div className="fld">
                <label htmlFor="s-buyer_place">{field("buyer_place")}</label>
                <input
                  id="s-buyer_place"
                  name="buyer_place"
                  maxLength={160}
                  value={buyerPlace}
                  onChange={(event) => setBuyerPlace(event.target.value)}
                />
                {selectedVendor ? <div className="note">{copy(pageContract, "hint.vendor_prefill")}</div> : null}
              </div>
              <div className="fld">
                <label htmlFor="s-animal_count">{field("animal_count")}</label>
                {/* No default of 0: blank means "not recorded", which is a different fact. */}
                <input id="s-animal_count" name="animal_count" type="number" min={0} step={1} />
              </div>
              <div className="fld">
                <label htmlFor="s-total_weight_kg">{field("total_weight_kg")}</label>
                <input id="s-total_weight_kg" name="total_weight_kg" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="s-sales_value">{field("sales_value")}</label>
                <input id="s-sales_value" name="sales_value" type="number" min={1} step="0.01" required />
              </div>
              <div className="fld">
                <label htmlFor="s-advance_amount">{field("advance_amount")}</label>
                <input id="s-advance_amount" name="advance_amount" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="s-status">{field("status")}</label>
                {/* Defaults to Deal Closed — a recorded sale is a finished one unless the desk says
                    otherwise. Advance Paid / In Discussion record an EXPECTED sale (a future sale
                    date is fine); the receipt itself is dated by when the money arrived. */}
                <select id="s-status" name="status" defaultValue="Deal Closed">
                  {dealStatusOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
                <div className="muted small">{copy(pageContract, "hint.status")}</div>
              </div>
              <div className="fld">
                <label htmlFor="s-comments">{field("comments")}</label>
                <textarea id="s-comments" name="comments" maxLength={2000} rows={2} />
              </div>
            </div>
            <div className="df">
              {/* A sale cannot be recorded without a vendor, so Save is disabled-with-reason rather
                  than left live to fail at the backend with a message about a field the form could
                  not offer. The route validates the same rule regardless. */}
              <button
                type="submit"
                className="btn p"
                disabled={!canPickVendor}
                aria-disabled={!canPickVendor}
                title={
                  canPickVendor
                    ? undefined
                    : copy(
                        pageContract,
                        vendorsUnavailable
                          ? "error.vendors_unavailable"
                          : vendorsTruncated
                            ? "hint.vendor_truncated"
                            : "hint.vendor_empty",
                      )
                }
              >
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={close}>
                {copy(pageContract, "action.cancel")}
              </button>
            </div>
          </form>
        ) : deal ? (
          <div className="dc">
            {/* RECORD drawer body: the mock's .metagrid of uppercase-key cells, never a flat stack. */}
            <div className="metagrid">
              {cell(field("sale_date"), deal.sale_date)}
              {cell(field("farm"), deal.farm)}
              {cell(field("product_type"), deal.product_type)}
              {cell(field("breed"), deal.breed)}
              {/* Resolved to the register's NAME, never the raw id -- a uuid on a farm screen is
                  banned copy. An id that resolves to nothing (a vendor since deactivated, or the
                  imported sheet history, which predates the register) renders as absent rather
                  than as a broken-looking string. */}
              {cell(
                field("vendor"),
                vendors.find((vendor) => vendor.vendor_id === deal.buyer_vendor_id)?.business_name ?? null,
              )}
              {cell(field("buyer_name"), deal.buyer_name)}
              {cell(field("buyer_place"), deal.buyer_place)}
              {cell(field("animal_count"), deal.animal_count == null ? null : num(deal.animal_count))}
              {cell(field("male_count"), deal.male_count == null ? null : num(deal.male_count))}
              {cell(field("female_count"), deal.female_count == null ? null : num(deal.female_count))}
              {cell(field("total_weight_kg"), deal.total_weight_kg == null ? null : num(deal.total_weight_kg, 1))}
              {cell(field("sales_value"), inr(deal.sales_value))}
              {cell(field("advance_amount"), deal.advance_amount == null ? null : inr(deal.advance_amount))}
              <div>
                <div className="k">{copy(pageContract, "column.status")}</div>
                <div className="v">
                  <Tag tone={dealStatusTone(deal.status)}>{deal.status}</Tag>
                </div>
              </div>
              {cell(field("comments"), deal.comments)}
            </div>

            {/* PAYMENTS: what the buyer has handed over, what is still owed, the receipt history,
                and — behind its backend control — the add-payment write. */}
            <div className="dgrp">{copy(pageContract, "section.payments.title")}</div>
            <div className="metagrid">
              {cell(
                copy(pageContract, "payments.received_so_far"),
                deal.payment_received == null ? null : inr(deal.payment_received),
              )}
              {/* BACKEND-derived; this cell renders the figure and never subtracts anything itself. */}
              {cell(copy(pageContract, "payments.balance"), inr(deal.payment_balance))}
            </div>

            {deal.payments.length === 0 ? (
              <div className="muted small">{copy(pageContract, "payments.empty")}</div>
            ) : (
              <div className="twrap">
                <table aria-label={copy(pageContract, "section.payments.title")}>
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "payments.column.received_on")}</th>
                      <th>{copy(pageContract, "payments.column.amount")}</th>
                      <th>{copy(pageContract, "payments.column.note")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {deal.payments.map((payment) => (
                      <tr key={payment.payment_id}>
                        <td style={{ whiteSpace: "nowrap" }}>{fmtDate(payment.received_on)}</td>
                        <td style={{ whiteSpace: "nowrap" }}>{inr(payment.amount_rupees)}</td>
                        <td>{payment.note || none}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {canRecordPayment ? (
              <form action={recordSalesDealPaymentAction}>
                <input type="hidden" name="return_to" value={dealHref} />
                <input type="hidden" name="deal_id" value={deal.deal_id} />
                <div className="fld">
                  <label htmlFor="sdp-received_on">{field("received_on")}</label>
                  <input id="sdp-received_on" name="received_on" type="date" required />
                </div>
                <div className="fld">
                  <label htmlFor="sdp-amount">{field("amount_rupees")}</label>
                  <input id="sdp-amount" name="amount_rupees" type="number" min={0.01} step="0.01" required />
                </div>
                <div className="fld">
                  <label htmlFor="sdp-note">{field("note")}</label>
                  <input id="sdp-note" name="note" maxLength={300} />
                  <div className="muted small">{copy(pageContract, "hint.record_payment")}</div>
                </div>
                <button type="submit" className="btn p">
                  {copy(pageContract, "action.record_deal_payment.label")}
                </button>
              </form>
            ) : null}

            {canEditStatus ? (
              <form action={setSalesDealStatusAction} className="fld">
                <input type="hidden" name="return_to" value={dealHref} />
                <input type="hidden" name="deal_id" value={deal.deal_id} />
                <label htmlFor="sds-status">{field("status")}</label>
                <div style={{ display: "flex", gap: 9, alignItems: "center" }}>
                  <select id="sds-status" name="status" required defaultValue={deal.status}>
                    {dealStatusOptions.map((option) => (
                      <option key={option.key} value={option.key}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                  <button type="submit" className="btn">
                    {copy(pageContract, "action.update_deal_status.label")}
                  </button>
                </div>
              </form>
            ) : null}
          </div>
        ) : null}
      </aside>
    </>
  );
}
