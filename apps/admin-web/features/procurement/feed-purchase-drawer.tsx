"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Pencil, Wheat } from "lucide-react";
import { useCallback, useId, useSyncExternalStore, type ReactNode } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerTableScroll } from "@/components/app/detail-drawer";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedPurchase, FeedPurchaseOptions } from "@/lib/api/procurement";
import type { ProcurementVendorForm } from "@/lib/api/server";
import { FeedPurchaseExtraFields } from "./feed-purchase-extra-fields";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { fmtDate, todayIso } from "@/lib/format";
import { deliveryStatusChip, paymentStatusChip } from "./feed-purchase-format";
import { inr, num } from "./sales-format";
import {
  editFeedPurchaseAction,
  recordFeedPurchaseAction,
  recordFeedPurchaseDeliveryAction,
  recordFeedPurchasePaymentAction,
  setFeedPurchasePaymentStatusAction,
} from "./feed-purchase-actions";
import { FormSelect } from "./form-select";
import { listOptions } from "./option-utils";

type ProcurementVendorFormPage = ProcurementVendorForm["pages"][number];
type ProcurementVendorFormQuestion = ProcurementVendorForm["pages"][number]["questions"][number];

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
  purchaseForm = null,
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
  /**
   * THE FEED PURCHASE FORM IS AUTHORED (2026-09-20): the published document. The ledger's own
   * columns keep the purpose-built inputs below (a date picker, a stepped number, the catalog
   * selects); everything the farm authored BEYOND them renders from this, and the version rides
   * the submit so the backend checks the answers against exactly the form shown here. Null when
   * the read failed -- the drawer then works on its typed fields alone, as it did before the form
   * existed.
   */
  purchaseForm?: ProcurementVendorForm | null;
}) {
  const addFormId = useId();
  const editFormId = useId();

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

  // Escape and the backdrop reach `close` through the template Drawer's onClose (focus is trapped in
  // the paper and returned to the opener); a second Escape listener would step history back twice.

  const none = copy(pageContract, "value.none");
  const field = (key: string) => copy(pageContract, `field.${key}`);
  // Every date on this ledger is a day that has already happened (bought, reached, paid), so each
  // picker is capped at today. The app's shared date control replaces the native input, whose
  // browser-drawn calendar and locale date order match nothing else on the page.
  const today = todayIso();
  const datePicker = (
    name: string,
    key: "purchase_date" | "reached_on" | "paid_on",
    opts: { required?: boolean; min?: string; defaultValue?: string } = {},
  ) => (
    <ThemedDatePicker
      name={name}
      label={copy(pageContract, `date.${key}.placeholder`)}
      min={opts.min}
      max={today}
      defaultValue={opts.defaultValue}
      previousMonthLabel={copy(pageContract, "date.prev_month")}
      nextMonthLabel={copy(pageContract, "date.next_month")}
      invalidDateText={copy(pageContract, `date.invalid_${key}`)}
      required={opts.required}
    />
  );
  // The payment writes are backend capabilities, never a role string: the same detail view serves
  // the read-only Feed Director (no forms) and the procurement desk (both forms).
  const canRecordPayment = controlEnabled(pageContract, "record_feed_purchase_payment", false);
  const canEditStatus = controlEnabled(pageContract, "update_feed_purchase_payment_status", false);
  const canEdit = controlEnabled(pageContract, "edit_feed_purchase", false);
  const canRecordDelivery = controlEnabled(pageContract, "record_feed_purchase_delivery", false);
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
    <DrawerMetaItem key={label} label={label}>
      {value === null || value === undefined || value === "" ? none : value}
    </DrawerMetaItem>
  );

  return (
    <DetailDrawer
      open={open}
      onClose={close}
      title={isAdding ? title : (purchase?.feed_item ?? title)}
      eyebrow={copy(pageContract, "crumb")}
      icon={<Wheat aria-hidden="true" />}
      iconColors={{ bg: "var(--brand-soft)", fg: "var(--info)" }}
      subtitle={purchase ? `${fmtDate(purchase.purchase_date)} · ${purchase.farm}` : undefined}
      ariaLabel={title}
      closeLabel={copy(pageContract, "action.close")}
      footer={
        isAdding ? (
          <>
            <Button type="button" variant="outlined" color="inherit" onClick={close}>
              {copy(pageContract, "action.cancel")}
            </Button>
            <Button type="submit" form={addFormId} variant="contained">
              {copy(pageContract, "action.save")}
            </Button>
          </>
        ) : isEditing ? (
          <>
            <Button type="button" variant="outlined" color="inherit" onClick={() => replaceLocalOverlayUrl(detailHref)}>
              {copy(pageContract, "action.cancel")}
            </Button>
            <Button type="submit" form={editFormId} variant="contained">
              {copy(pageContract, "action.save")}
            </Button>
          </>
        ) : undefined
      }
    >
        {purchase && !isAdding && !isEditing && canEdit ? (
          <Button
            type="button"
            variant="outlined"
            color="inherit"
            startIcon={<Pencil aria-hidden="true" />}
            sx={{ alignSelf: "flex-start" }}
            onClick={() => replaceLocalOverlayUrl(`${detailHref}&edit=1`)}
          >
            {copy(pageContract, "action.edit_feed_purchase.label")}
          </Button>
        ) : null}

        {isAdding ? (
          <Box component="form" id={addFormId} action={recordFeedPurchaseAction} sx={{ display: "contents" }}>
              <input type="hidden" name="return_to" value={listHref} />
              <input type="hidden" name="idempotency_key" value={recordIdempotencyKey} />

              <DrawerHint>{copy(pageContract, "required.hint")}</DrawerHint>
              {/* The authored form's version and every question it asked: readFormAnswers only maps
                  typed fields onto question ids the drawer declares here (40466370a). */}
              {purchaseForm ? <>
                <input type="hidden" name="questionnaire_version" value={purchaseForm.version} />
                {purchaseForm.pages.flatMap((page) => page.questions).map((question) => (
                  <input key={question.id} type="hidden" name="questionnaire_question" value={question.id} />
                ))}
              </> : null}

              <Box sx={FIELD_SX}>
                {datePicker("purchase_date", "purchase_date", { required: true })}
              </Box>
              <Box sx={FIELD_SX}>
                <FormSelect
                  label={field("farm")}
                  name="farm"
                  id="fp-farm"
                  defaultValue=""
                  required
                  options={listOptions(farmOptions, (option) => option.key, (option) => option.label, "—")}
                />
              </Box>
              <Box sx={FIELD_SX}>
                {/* The catalog LABEL is both the option value and what is sent: the backend resolves
                    it through the same normalization the ledger's key uses. */}
                <FormSelect
                  label={field("feed_item")}
                  name="feed_item"
                  id="fp-feed_item"
                  defaultValue=""
                  required
                  options={listOptions(feedItems, (item) => item.label, (item) => item.label, "—")}
                />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-quantity_kg" name="quantity_kg" type="number" required label={field("quantity_kg")} slotProps={{ htmlInput: { min: 0.001, step: "0.001" }, inputLabel: { shrink: true } }} />
              </Box>

              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-feed_cost" name="feed_cost" type="number" label={field("feed_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-transport_cost" name="transport_cost" type="number" label={field("transport_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-loading_cost" name="loading_cost" type="number" label={field("loading_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-unloading_cost" name="unloading_cost" type="number" label={field("unloading_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-total_cost" name="total_cost" type="number" label={field("total_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>

              <Box sx={FIELD_SX}>
                {/* Free text with a suggestion list, not a select: a new supplier must be enterable
                    on the first load bought from them. */}
                <TextField fullWidth id="fp-vendor" name="vendor" required label={field("vendor")} slotProps={{ htmlInput: { maxLength: 160, list: "fp-vendor-options" }, inputLabel: { shrink: true } }} />
                <datalist id="fp-vendor-options">
                  {vendorSuggestions.map((vendor) => (
                    <option key={vendor} value={vendor} />
                  ))}
                </datalist>
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-days_of_stock" name="days_of_stock" type="number" label={field("days_of_stock")} slotProps={{ htmlInput: { min: 1, step: "1" }, inputLabel: { shrink: true } }} />
                <DrawerHint>{copy(pageContract, "hint.days_of_stock")}</DrawerHint>
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-payment_released" name="payment_released" type="number" label={field("payment_released")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <FormSelect
                  label={field("payment_status")}
                  name="payment_status"
                  id="fp-payment_status"
                  defaultValue=""
                  required
                  options={listOptions(paymentOptions, (option) => option.key, (option) => option.label, "—")}
                />
              </Box>

              {/* DELIVERY: blank = the load is still on the road (the normal case). A date here
                  records a load that already came in, reached that day. */}
              <DrawerGroup>{copy(pageContract, "section.delivery.title")}</DrawerGroup>
              <Box sx={FIELD_SX}>
                {datePicker("reached_on", "reached_on")}
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fp-reached_weight_kg" name="reached_weight_kg" type="number" label={field("reached_weight_kg")} slotProps={{ htmlInput: { min: 0.001, step: "0.001" }, inputLabel: { shrink: true } }} />
              </Box>

              {/* Whatever the farm authored beyond the ledger's own columns, from the published
                  form (2026-09-20). Nothing renders when the document adds nothing. */}
              <FeedPurchaseExtraFields form={purchaseForm} pageContract={pageContract} />
          </Box>
        ) : purchase && isEditing ? (
          <Box component="form" id={editFormId} action={editFeedPurchaseAction} sx={{ display: "contents" }}>
              <input type="hidden" name="return_to" value={detailHref} />
              <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />

              {/* Identity is read-only by design: farm, feed and batch are the natural key the
                  stock cards group by. The hint says so rather than leaving greyed boxes mute. */}
              <DrawerMetaGrid>
                <DrawerMetaItem label={field("farm")}>{purchase.farm}</DrawerMetaItem>
                <DrawerMetaItem label={field("feed_item")}>{purchase.feed_item}</DrawerMetaItem>
                <DrawerMetaItem label={field("batch_no")}>{purchase.batch_no}</DrawerMetaItem>
              </DrawerMetaGrid>

              <Box sx={FIELD_SX}>
                {datePicker("purchase_date", "purchase_date", { required: true, defaultValue: purchase.purchase_date })}
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-quantity_kg" name="quantity_kg" type="number" required defaultValue={purchase.quantity_kg} label={field("quantity_kg")} slotProps={{ htmlInput: { min: 0.001, step: "0.001" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-feed_cost" name="feed_cost" type="number" defaultValue={purchase.feed_cost ?? ""} label={field("feed_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-transport_cost" name="transport_cost" type="number" defaultValue={purchase.transport_cost ?? ""} label={field("transport_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-loading_cost" name="loading_cost" type="number" defaultValue={purchase.loading_cost ?? ""} label={field("loading_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-unloading_cost" name="unloading_cost" type="number" defaultValue={purchase.unloading_cost ?? ""} label={field("unloading_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-total_cost" name="total_cost" type="number" defaultValue={purchase.total_cost ?? ""} label={field("total_cost")} slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-vendor" name="vendor" required defaultValue={purchase.vendor} label={field("vendor")} slotProps={{ htmlInput: { maxLength: 160, list: "fp-vendor-options" }, inputLabel: { shrink: true } }} />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField fullWidth id="fpe-days_of_stock" name="days_of_stock" type="number" defaultValue={purchase.days_of_stock ?? ""} label={field("days_of_stock")} slotProps={{ htmlInput: { min: 1, step: "1" }, inputLabel: { shrink: true } }} />
                <DrawerHint>{copy(pageContract, "hint.days_of_stock")}</DrawerHint>
              </Box>
          </Box>
        ) : purchase ? (
          <>
            {/* RECORD drawer body: label/value cells, never a flat stack. */}
            <DrawerMetaGrid>
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
              {(purchase.answer_rows ?? []).map((answer) => (
                <DrawerMetaItem key={answer.question_id} label={answer.label}>{answer.value}</DrawerMetaItem>
              ))}
              {cell(
                field("days_of_stock"),
                purchase.days_of_stock == null
                  ? null
                  : copy(pageContract, "value.days_of_stock").replace("{days}", String(purchase.days_of_stock)),
              )}
              {cell(
                copy(pageContract, "column.entry_source"),
                purchase.entry_source === "app"
                  ? copy(pageContract, "value.entry_app")
                  : copy(pageContract, "value.entry_sheet"),
              )}
            </DrawerMetaGrid>

            {/* DELIVERY: has the load come in, when, and what it is worth in the store. Behind its
                backend control, the mark-reached / update-arrival write. The received weight can
                be entered later, so the same form serves a load already reached. */}
            <DrawerGroup>{copy(pageContract, "section.delivery.title")}</DrawerGroup>
            <DrawerMetaGrid>
              <DrawerMetaItem label={field("delivery_status")}>
                  <Tag tone={deliveryStatusChip(pageContract, purchase.delivery_status, none).tone}>
                    {deliveryStatusChip(pageContract, purchase.delivery_status, none).label}
                  </Tag>
                </DrawerMetaItem>
              {cell(field("reached_on"), purchase.reached_on ? fmtDate(purchase.reached_on) : null)}
              {cell(field("reached_weight_kg"), purchase.reached_weight_kg == null ? null : num(purchase.reached_weight_kg, 1))}
              {/* BACKEND-derived: received weight if entered, else buying weight; absent on the road. */}
              {cell(field("stock_kg"), purchase.stock_kg == null ? null : num(purchase.stock_kg, 1))}
            </DrawerMetaGrid>
            {canRecordDelivery ? (
              <Box component="form" action={recordFeedPurchaseDeliveryAction} sx={FORM_SX}>
                <input type="hidden" name="return_to" value={detailHref} />
                <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />
                <input type="hidden" name="was_reached" value={purchase.reached_on ? "1" : "0"} />
                <Box sx={FIELD_SX}>
                  {/* A load cannot reach before it was bought: the picker's floor is the purchase
                      date, the same rule the backend enforces under the row lock. */}
                  {datePicker("reached_on", "reached_on", {
                    required: true,
                    min: purchase.purchase_date,
                    defaultValue: purchase.reached_on ?? undefined,
                  })}
                </Box>
                <Box sx={FIELD_SX}>
                  <TextField fullWidth id="fpd-reached_weight_kg" name="reached_weight_kg" type="number" defaultValue={purchase.reached_weight_kg ?? ""} label={field("reached_weight_kg")} slotProps={{ htmlInput: { min: 0.001, step: "0.001" }, inputLabel: { shrink: true } }} />
                  <DrawerHint>
                    {purchase.reached_on ? copy(pageContract, "hint.reached_weight") : copy(pageContract, "hint.mark_reached")}
                  </DrawerHint>
                </Box>
                <Button type="submit" variant="contained" sx={{ alignSelf: "flex-start" }}>
                  {purchase.reached_on
                    ? copy(pageContract, "action.update_delivery.label")
                    : copy(pageContract, "action.mark_reached.label")}
                </Button>
              </Box>
            ) : null}

            {/* PAYMENTS: what has been handed over, what is still owed, the instalment history,
                and — behind their backend controls — the add-payment and status-edit writes. */}
            <DrawerGroup>{copy(pageContract, "section.payments.title")}</DrawerGroup>
            <DrawerMetaGrid>
              <DrawerMetaItem label={copy(pageContract, "column.payment_status")}>
                  {/* Tone AND label come from the contract's own option group, so the chip follows a
                      backend vocabulary change instead of a hardcoded comparison here. */}
                  <Tag tone={paymentStatusChip(pageContract, purchase.payment_status, none).tone}>
                    {paymentStatusChip(pageContract, purchase.payment_status, none).label}
                  </Tag>
                </DrawerMetaItem>
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
            </DrawerMetaGrid>

            {purchase.payments.length === 0 ? (
              <DrawerHint>{copy(pageContract, "payments.empty")}</DrawerHint>
            ) : (
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 400 }} aria-label={copy(pageContract, "section.payments.title")}>
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{copy(pageContract, "payments.column.paid_on")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "payments.column.amount")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "payments.column.note")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {purchase.payments.map((payment) => (
                      <TableRow key={payment.payment_id}>
                        <TableCell sx={{ whiteSpace: "nowrap" }}>{fmtDate(payment.paid_on)}</TableCell>
                        <TableCell sx={{ whiteSpace: "nowrap" }}>{inr(payment.amount_rupees)}</TableCell>
                        <TableCell>{payment.note || none}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
            )}

            {canRecordPayment ? (
              <Box component="form" action={recordFeedPurchasePaymentAction} sx={FORM_SX}>
                <input type="hidden" name="return_to" value={detailHref} />
                <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />
                <input type="hidden" name="idempotency_key" value={paymentIdempotencyKey} />
                <Box sx={FIELD_SX}>
                  {datePicker("paid_on", "paid_on", { required: true })}
                </Box>
                <Box sx={FIELD_SX}>
                  <TextField fullWidth id="fpp-amount" name="amount_rupees" type="number" required label={field("amount_rupees")} slotProps={{ htmlInput: { min: 0.01, step: "0.01" }, inputLabel: { shrink: true } }} />
                </Box>
                <Box sx={FIELD_SX}>
                  <TextField fullWidth id="fpp-note" name="note" label={field("note")} slotProps={{ htmlInput: { maxLength: 300 }, inputLabel: { shrink: true } }} />
                </Box>
                <Button type="submit" variant="contained" sx={{ alignSelf: "flex-start" }}>
                  {copy(pageContract, "action.record_feed_payment.label")}
                </Button>
              </Box>
            ) : null}

            {canEditStatus ? (
              <Box component="form" action={setFeedPurchasePaymentStatusAction} sx={FIELD_SX}>
                <input type="hidden" name="return_to" value={detailHref} />
                <input type="hidden" name="feed_purchase_id" value={purchase.feed_purchase_id} />
                <Box sx={{ display: "flex", gap: 1, alignItems: "flex-end" }}>
                  <FormSelect
                    label={field("payment_status")}
                    name="payment_status"
                    id="fpp-status"
                    defaultValue={purchase.payment_status}
                    required
                    options={listOptions(paymentOptions, (option) => option.key, (option) => option.label)}
                  />
                  <Button type="submit" variant="outlined" color="inherit">
                    {copy(pageContract, "action.update_payment_status.label")}
                  </Button>
                </Box>
              </Box>
            ) : null}
          </>
        ) : null}
    </DetailDrawer>
  );
}

/** One field block (control + its hint) in the drawer's form column. */
const FIELD_SX = { display: "flex", flexDirection: "column", gap: 1, minWidth: 0 } as const;
/** An inline form in the record body: its fields and its submit, one column. */
const FORM_SX = { display: "flex", flexDirection: "column", gap: 2, minWidth: 0 } as const;

/** A section title inside the drawer body (template subtitle2 heading). */
function DrawerGroup({ children }: { children: ReactNode }) {
  return (
    <Typography variant="subtitle2" component="h3" sx={{ pt: 1 }}>
      {children}
    </Typography>
  );
}

/** A muted guidance line under a field or section. */
function DrawerHint({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" component="div" sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}
