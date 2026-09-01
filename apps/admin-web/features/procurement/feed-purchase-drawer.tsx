"use client";

import { Wheat, X } from "lucide-react";
import { useCallback, useEffect, useRef, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedPurchase, FeedPurchaseOptions } from "@/lib/api/procurement";
import { fmtDate } from "@/lib/format";
import { paymentStatusChip } from "./feed-purchase-format";
import { inr, num } from "./sales-format";
import {
  editFeedPurchaseAction,
  recordFeedPurchaseAction,
  recordFeedPurchasePaymentAction,
  setFeedPurchasePaymentStatusAction,
} from "./feed-purchase-actions";

/** Reads the selected purchase from the address bar. "" means closed; "new" is the entry form. */
function readPurchaseParam(): string {
  return new URL(window.location.href).searchParams.get("purchase_id") ?? "";
}

/** Reads the edit flag from the address bar, so Back leaves edit mode before closing the drawer. */
function readEditParam(): string {
  return new URL(window.location.href).searchParams.get("edit") ?? "";
}

/**
 * Subscribes to both ways the purchase param can change: a LocalOverlayLink click (which dispatches
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

/**
 * The feed purchase ledger's record / detail overlay, modeled on the sales and vendor drawers:
 * CLIENT state driven by the URL (LocalOverlayLink changes history WITHOUT an RSC request, so a
 * server-read search param would never open it), and the mock's drawer anatomy — `.scrim`/
 * `.drawer.on`, `.dh`/`.dc`/`.df`, with a RECORD body as a `.metagrid` of `.k`/`.v` cells.
 */
export function FeedPurchaseDrawer({
  purchases,
  options,
  recordIdempotencyKey,
  paymentIdempotencyKey,
  pageContract,
  listHref,
  canRecord,
}: {
  /** The rendered ledger page. The detail view opens from this data — it issues no fetch of its own. */
  purchases: FeedPurchase[];
  /** Backend-owned form vocabulary (farms, the ACTIVE feed catalog, payment states, vendors seen). */
  options: FeedPurchaseOptions | null;
  /** Stable key for the currently rendered record form. Reusing it makes retry/double-submit safe. */
  recordIdempotencyKey: string;
  /** Stable key for the currently rendered add-payment form, for the same retry safety. */
  paymentIdempotencyKey: string;
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current farm/paging, without the purchase param). */
  listHref: string;
  /** Backend-declared record_feed_purchase capability; without it the form never renders. */
  canRecord: boolean;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  // The URL is an EXTERNAL store — LocalOverlayLink mutates history outside React — so it is read
  // via useSyncExternalStore rather than mirrored into state in an effect. SSR renders it closed.
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readPurchaseParam, () => "");
  const editFlag = useSyncExternalStore(subscribeToOverlayUrl, readEditParam, () => "");

  const isAdding = selection === "new" && canRecord;
  const purchase =
    selection && selection !== "new"
      ? (purchases.find((p) => p.feed_purchase_id === selection) ?? null)
      : null;
  const open = isAdding || purchase !== null;

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
  // The payment writes are backend capabilities, never a role string: the same detail view serves
  // the read-only Feed Director (no forms) and the procurement desk (both forms).
  const canRecordPayment = controlEnabled(pageContract, "record_feed_purchase_payment", false);
  const canEditStatus = controlEnabled(pageContract, "update_feed_purchase_payment_status", false);
  const canEdit = controlEnabled(pageContract, "edit_feed_purchase", false);
  // Where the payment actions return to: the SAME record, so the drawer reopens showing the new
  // instalment rather than closing over the operator's work.
  const detailHref = purchase
    ? `${listHref}${listHref.includes("?") ? "&" : "?"}purchase_id=${encodeURIComponent(purchase.feed_purchase_id)}`
    : listHref;
  const isEditing = purchase !== null && canEdit && editFlag === "1";
  const title = isAdding
    ? copy(pageContract, "drawer.record_purchase.title")
    : isEditing
      ? copy(pageContract, "drawer.edit.title")
      : copy(pageContract, "drawer.detail.title");

  // The write vocabulary excludes the read-scope "all" entry: a load is bought for ONE farm.
  const farmOptions = optionGroup(pageContract, "feed_purchase_farms").filter((option) => option.key !== "all");
  const paymentOptions = optionGroup(pageContract, "feed_purchase_payment_statuses");
  // The feed list is live tenant rows, so it arrives from the options endpoint rather than from a
  // contract option group — a constant list of feed labels in contract code is the banned pattern.
  const feedItems = options?.feed_items ?? [];
  const vendorSuggestions = options?.vendors ?? [];

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
            <Wheat className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "crumb")}</div>
            <h2>{isAdding ? title : (purchase?.feed_item ?? title)}</h2>
            {purchase ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {fmtDate(purchase.purchase_date)} · {purchase.farm}
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
          <form action={recordFeedPurchaseAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <input type="hidden" name="idempotency_key" value={recordIdempotencyKey} />

              <div className="note">{copy(pageContract, "required.hint")}</div>

              <div className="fld">
                <label htmlFor="fp-purchase_date">{field("purchase_date")}</label>
                <input id="fp-purchase_date" name="purchase_date" type="date" required />
              </div>
              <div className="fld">
                <label htmlFor="fp-farm">{field("farm")}</label>
                <select id="fp-farm" name="farm" required defaultValue="">
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
                <label htmlFor="fp-feed_item">{field("feed_item")}</label>
                {/* The catalog LABEL is both the option value and what is sent: the backend resolves
                    it through the same normalization the ledger's key uses. */}
                <select id="fp-feed_item" name="feed_item" required defaultValue="">
                  <option value="" disabled>
                    —
                  </option>
                  {feedItems.map((item) => (
                    <option key={item.key} value={item.label}>
                      {item.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="fp-quantity_kg">{field("quantity_kg")}</label>
                <input id="fp-quantity_kg" name="quantity_kg" type="number" min={0.001} step="0.001" required />
              </div>
              <div className="fld">
                <label htmlFor="fp-batch_no">{field("batch_no")}</label>
                {/* Blank on purpose: the backend assigns the next load number for this farm and
                    feed. A default here would guess a number the ledger may already hold. */}
                <input id="fp-batch_no" name="batch_no" type="number" min={1} step={1} />
                <div className="muted small">{copy(pageContract, "hint.batch_no")}</div>
              </div>

              <div className="fld">
                <label htmlFor="fp-feed_cost">{field("feed_cost")}</label>
                <input id="fp-feed_cost" name="feed_cost" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="fp-transport_cost">{field("transport_cost")}</label>
                <input id="fp-transport_cost" name="transport_cost" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="fp-loading_cost">{field("loading_cost")}</label>
                <input id="fp-loading_cost" name="loading_cost" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="fp-unloading_cost">{field("unloading_cost")}</label>
                <input id="fp-unloading_cost" name="unloading_cost" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="fp-total_cost">{field("total_cost")}</label>
                <input id="fp-total_cost" name="total_cost" type="number" min={0} step="0.01" />
                <div className="muted small">{copy(pageContract, "hint.total_cost")}</div>
              </div>

              <div className="fld">
                <label htmlFor="fp-vendor">{field("vendor")}</label>
                {/* Free text with a suggestion list, not a select: a new supplier must be enterable
                    on the first load bought from them. */}
                <input id="fp-vendor" name="vendor" required maxLength={160} list="fp-vendor-options" />
                <datalist id="fp-vendor-options">
                  {vendorSuggestions.map((vendor) => (
                    <option key={vendor} value={vendor} />
                  ))}
                </datalist>
              </div>
              <div className="fld">
                <label htmlFor="fp-payment_released">{field("payment_released")}</label>
                <input id="fp-payment_released" name="payment_released" type="number" min={0} step="0.01" />
              </div>
              <div className="fld">
                <label htmlFor="fp-payment_status">{field("payment_status")}</label>
                <select id="fp-payment_status" name="payment_status" required defaultValue="">
                  <option value="" disabled>
                    —
                  </option>
                  {paymentOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
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
        ) : purchase && isEditing ? (
          <form action={editFeedPurchaseAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={detailHref} />
              <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />

              {/* Identity is read-only by design: farm, feed and batch are the natural key the
                  stock cards group by. The hint says so rather than leaving greyed boxes mute. */}
              <div className="note">{copy(pageContract, "hint.edit_identity")}</div>
              <div className="metagrid">
                <div>
                  <div className="k">{field("farm")}</div>
                  <div className="v">{purchase.farm}</div>
                </div>
                <div>
                  <div className="k">{field("feed_item")}</div>
                  <div className="v">{purchase.feed_item}</div>
                </div>
                <div>
                  <div className="k">{field("batch_no")}</div>
                  <div className="v">{purchase.batch_no}</div>
                </div>
              </div>

              <div className="fld">
                <label htmlFor="fpe-purchase_date">{field("purchase_date")}</label>
                <input id="fpe-purchase_date" name="purchase_date" type="date" required defaultValue={purchase.purchase_date} />
              </div>
              <div className="fld">
                <label htmlFor="fpe-quantity_kg">{field("quantity_kg")}</label>
                <input id="fpe-quantity_kg" name="quantity_kg" type="number" min={0.001} step="0.001" required defaultValue={purchase.quantity_kg} />
              </div>
              <div className="fld">
                <label htmlFor="fpe-feed_cost">{field("feed_cost")}</label>
                <input id="fpe-feed_cost" name="feed_cost" type="number" min={0} step="0.01" defaultValue={purchase.feed_cost ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="fpe-transport_cost">{field("transport_cost")}</label>
                <input id="fpe-transport_cost" name="transport_cost" type="number" min={0} step="0.01" defaultValue={purchase.transport_cost ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="fpe-loading_cost">{field("loading_cost")}</label>
                <input id="fpe-loading_cost" name="loading_cost" type="number" min={0} step="0.01" defaultValue={purchase.loading_cost ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="fpe-unloading_cost">{field("unloading_cost")}</label>
                <input id="fpe-unloading_cost" name="unloading_cost" type="number" min={0} step="0.01" defaultValue={purchase.unloading_cost ?? ""} />
              </div>
              <div className="fld">
                <label htmlFor="fpe-total_cost">{field("total_cost")}</label>
                <input id="fpe-total_cost" name="total_cost" type="number" min={0} step="0.01" defaultValue={purchase.total_cost ?? ""} />
                <div className="muted small">{copy(pageContract, "hint.total_cost")}</div>
              </div>
              <div className="fld">
                <label htmlFor="fpe-vendor">{field("vendor")}</label>
                <input id="fpe-vendor" name="vendor" required maxLength={160} defaultValue={purchase.vendor} list="fp-vendor-options" />
              </div>
            </div>
            <div className="df">
              <button type="submit" className="btn p">
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={() => replaceLocalOverlayUrl(detailHref)}>
                {copy(pageContract, "action.cancel")}
              </button>
            </div>
          </form>
        ) : purchase ? (
          <div className="dc">
            {canEdit ? (
              <div>
                <button
                  type="button"
                  className="btn"
                  onClick={() => replaceLocalOverlayUrl(`${detailHref}&edit=1`)}
                >
                  {copy(pageContract, "action.edit_feed_purchase.label")}
                </button>
              </div>
            ) : null}
            {/* RECORD drawer body: the mock's .metagrid of uppercase-key cells, never a flat stack. */}
            <div className="metagrid">
              {cell(field("purchase_date"), fmtDate(purchase.purchase_date))}
              {cell(field("farm"), purchase.farm)}
              {cell(field("feed_item"), purchase.feed_item)}
              {cell(field("batch_no"), purchase.batch_no)}
              {cell(field("quantity_kg"), num(purchase.quantity_kg, 1))}
              {cell(field("feed_cost"), purchase.feed_cost == null ? null : inr(purchase.feed_cost))}
              {cell(field("transport_cost"), purchase.transport_cost == null ? null : inr(purchase.transport_cost))}
              {cell(field("loading_cost"), purchase.loading_cost == null ? null : inr(purchase.loading_cost))}
              {cell(field("unloading_cost"), purchase.unloading_cost == null ? null : inr(purchase.unloading_cost))}
              {cell(field("total_cost"), purchase.total_cost == null ? null : inr(purchase.total_cost))}
              {cell(field("per_kg_cost"), purchase.per_kg_cost == null ? null : inr(purchase.per_kg_cost, 2))}
              {cell(field("vendor"), purchase.vendor)}
              {cell(
                copy(pageContract, "column.entry_source"),
                purchase.entry_source === "app"
                  ? copy(pageContract, "value.entry_app")
                  : copy(pageContract, "value.entry_sheet"),
              )}
            </div>

            {/* PAYMENTS: what has been handed over, what is still owed, the instalment history,
                and — behind their backend controls — the add-payment and status-edit writes. */}
            <div className="dgrp">{copy(pageContract, "section.payments.title")}</div>
            <div className="metagrid">
              <div>
                <div className="k">{copy(pageContract, "column.payment_status")}</div>
                <div className="v">
                  {/* Tone AND label come from the contract's own option group, so the chip follows a
                      backend vocabulary change instead of a hardcoded comparison here. */}
                  <Tag tone={paymentStatusChip(pageContract, purchase.payment_status, none).tone}>
                    {paymentStatusChip(pageContract, purchase.payment_status, none).label}
                  </Tag>
                </div>
              </div>
              {cell(
                copy(pageContract, "payments.paid_so_far"),
                purchase.payment_released == null ? null : inr(purchase.payment_released),
              )}
              {/* The balance is BACKEND-derived; this cell renders it and never subtracts anything
                  itself. Null while the landed cost is unknown. */}
              {cell(
                copy(pageContract, "payments.balance"),
                purchase.payment_balance == null ? null : inr(purchase.payment_balance),
              )}
            </div>

            {purchase.payments.length === 0 ? (
              <div className="muted small">{copy(pageContract, "payments.empty")}</div>
            ) : (
              <div className="twrap">
                <table aria-label={copy(pageContract, "section.payments.title")}>
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "payments.column.paid_on")}</th>
                      <th>{copy(pageContract, "payments.column.amount")}</th>
                      <th>{copy(pageContract, "payments.column.note")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {purchase.payments.map((payment) => (
                      <tr key={payment.payment_id}>
                        <td style={{ whiteSpace: "nowrap" }}>{fmtDate(payment.paid_on)}</td>
                        <td style={{ whiteSpace: "nowrap" }}>{inr(payment.amount_rupees)}</td>
                        <td>{payment.note || none}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {canRecordPayment ? (
              <form action={recordFeedPurchasePaymentAction}>
                <input type="hidden" name="return_to" value={detailHref} />
                <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />
                <input type="hidden" name="idempotency_key" value={paymentIdempotencyKey} />
                <div className="fld">
                  <label htmlFor="fpp-paid_on">{field("paid_on")}</label>
                  <input id="fpp-paid_on" name="paid_on" type="date" required />
                </div>
                <div className="fld">
                  <label htmlFor="fpp-amount">{field("amount_rupees")}</label>
                  <input id="fpp-amount" name="amount_rupees" type="number" min={0.01} step="0.01" required />
                </div>
                <div className="fld">
                  <label htmlFor="fpp-note">{field("note")}</label>
                  <input id="fpp-note" name="note" maxLength={300} />
                  <div className="muted small">{copy(pageContract, "hint.record_payment")}</div>
                </div>
                <button type="submit" className="btn p">
                  {copy(pageContract, "action.record_feed_payment.label")}
                </button>
              </form>
            ) : null}

            {canEditStatus ? (
              <form action={setFeedPurchasePaymentStatusAction} className="fld">
                <input type="hidden" name="return_to" value={detailHref} />
                <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />
                <label htmlFor="fpp-status">{field("payment_status")}</label>
                <div style={{ display: "flex", gap: 9, alignItems: "center" }}>
                  <select id="fpp-status" name="payment_status" required defaultValue={purchase.payment_status}>
                    {paymentOptions.map((option) => (
                      <option key={option.key} value={option.key}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                  <button type="submit" className="btn">
                    {copy(pageContract, "action.update_payment_status.label")}
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
