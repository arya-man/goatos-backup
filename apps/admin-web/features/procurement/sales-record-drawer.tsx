"use client";

import { Banknote, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, optionalOptionGroup, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesDeal } from "@/lib/api/procurement";
import { fmtDate } from "@/lib/format";
import { dealStatusTone, inr, num } from "./sales-format";
import { recordSaleAction } from "./sales-actions";

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
}: {
  /** The rendered ledger page. The detail view opens from this data — it issues no fetch of its own. */
  deals: SalesDeal[];
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current farm/paging, without the deal param). */
  listHref: string;
  /** Backend-declared record_sale capability; without it the form never renders. */
  canRecord: boolean;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  // The URL is an EXTERNAL store — LocalOverlayLink mutates history outside React — so it is read
  // via useSyncExternalStore rather than mirrored into state in an effect. SSR always renders the
  // drawer closed.
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readDealParam, () => "");

  const isAdding = selection === "new" && canRecord;
  const deal = selection && selection !== "new" ? (deals.find((d) => d.deal_id === selection) ?? null) : null;
  const open = isAdding || deal !== null;

  // The breed vocabulary follows the selected product. Tracked as state so changing the product
  // select swaps the breed group; reset during render when the selection changes, not in an effect.
  const productOptions = optionGroup(pageContract, "sales_product_types");
  const [product, setProduct] = useState(productOptions[0]?.key ?? "");
  const [syncedSelection, setSyncedSelection] = useState(selection);
  if (syncedSelection !== selection) {
    setSyncedSelection(selection);
    setProduct(productOptions[0]?.key ?? "");
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
                <input id="s-sale_date" name="sale_date" type="date" required />
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
                <label htmlFor="s-buyer_name">{field("buyer_name")}</label>
                <input id="s-buyer_name" name="buyer_name" required maxLength={160} />
              </div>
              <div className="fld">
                <label htmlFor="s-buyer_place">{field("buyer_place")}</label>
                <input id="s-buyer_place" name="buyer_place" maxLength={160} />
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
                <label htmlFor="s-comments">{field("comments")}</label>
                <textarea id="s-comments" name="comments" maxLength={2000} rows={2} />
              </div>
            </div>
            <div className="df">
              <button type="submit" className="btn p">
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
          </div>
        ) : null}
      </aside>
    </>
  );
}
