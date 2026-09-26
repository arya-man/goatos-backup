"use client";
import Box from "@mui/material/Box";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import MuiLink from "@mui/material/Link";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import TextField from "@mui/material/TextField";

import { Banknote, Save, Trash2 } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { startTransition, useActionState, useCallback, useEffect, useId, useRef, useState, useSyncExternalStore, type FormEvent, type ReactNode } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerTableScroll } from "@/components/app/detail-drawer";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesDeal, SalesOptions } from "@/lib/api/procurement";
import type { ProcurementVendorOption, ProcurementVendorOptions } from "@/lib/api/server";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { breedBeyondProduct, dealStatusTone, inr, num, plannedSaleDateIfDifferent, quantityAtRate } from "./sales-format";
import { newSaleLine, type SaleLineDraft } from "./sale-lines";
import { SaleLinesEditor } from "./sale-lines-editor";
import {
  deleteSalesDealPaymentAction,
  recordSaleAction,
  recordSalesDealPaymentAction,
  setSalesDealStatusAction,
  updateSalesDealPaymentAction,
  type SalesPaymentActionError,
} from "./sales-actions";
import { FormSelect } from "./form-select";
import { listOptions } from "./option-utils";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import { PAYMENT_IDEMPOTENCY_FIELD, paymentKeyFor, type MintedPaymentKey } from "./payment-idempotency";

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
 * search param would never open it), rendered in the template temporary drawer (DetailDrawer:
 * portal, backdrop, header + close, Scrollbar body, footer actions) with a RECORD body of
 * label/value cells (DrawerMetaGrid).
 */
export function SalesRecordDrawer({
  deals,
  pageContract,
  listHref,
  canRecord,
  vendorOptions,
  salesOptions,
  stockConfirmNeeded,
  statusStockConfirmNeeded,
  stockConfirmDetail,
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
  /**
   * What the farm sells and what each may be sold as, read with the page from /sales/options --
   * the SAME answer the phone's record-sale form reads, so the two surfaces cannot offer different
   * products. `null` means the read failed; the form then says so rather than rendering an empty
   * product list that reads as "this farm sells nothing".
   */
  salesOptions: SalesOptions | null;
  /**
   * The last submit came back asking the desk to confirm a sale that takes more feed than the
   * store shows. It puts the tick on the form; the sentence itself is backend copy shown in the
   * page's banner.
   */
  stockConfirmNeeded: boolean;
  /** The same confirmation, raised by CLOSING an expected sale -- when its feed actually leaves. */
  statusStockConfirmNeeded: boolean;
  /**
   * The backend's own sentence naming what the store holds. It is rendered HERE, beside the tick,
   * rather than only in the page banner: the drawer reopens over that banner, so a person being
   * asked to confirm would have to close their half-filled form to read the question.
   */
  stockConfirmDetail?: string;
}) {
  const recordFormId = useId();

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
  const canUpdatePayment = controlEnabled(pageContract, "update_sales_deal_payment", false);
  const canDeletePayment = controlEnabled(pageContract, "delete_sales_deal_payment", false);
  const canEditStatus = controlEnabled(pageContract, "update_sales_deal_status", false);
  const dealStatusOptions = optionGroup(pageContract, "sales_deal_statuses");
  // What THIS deal may be set to is the backend's call (status_options): nothing for a failed
  // deal, which is final (2026-09-25). An older payload without the field offers every status.
  const editStatusOptions = deal?.status_options
    ? dealStatusOptions.filter((option) => deal.status_options?.includes(option.key as SalesDeal["status"]))
    : dealStatusOptions;
  // Where the payment action returns to: the SAME deal, so the drawer reopens showing the new
  // receipt rather than closing over the operator's work.
  const dealHref = deal
    ? `${listHref}${listHref.includes("?") ? "&" : "?"}deal_id=${encodeURIComponent(deal.deal_id)}`
    : listHref;

  // ONE sale, MANY lines (maintainer decision 2026-09-12): the product/breed/animals/weight/value
  // live on the lines, one card each; the deal keeps date, farm, buyer, advance, status. Reset
  // during render when the selection changes, not in an effect.
  // WHAT THE FARM SELLS IS ITS OWN REGISTRY (migration 000422), read from the backend rather than
  // compiled into this page's contract, because the phone's form reads the same answer.
  const products = salesOptions?.products ?? [];
  const variants = (salesOptions?.breeds ?? {}) as Record<string, string[]>;
  const [lines, setLines] = useState<SaleLineDraft[]>(() => [newSaleLine(1, products[0]?.name ?? "")]);
  // The vendor IS the buyer, so picking one fills the buyer snapshot fields. They stay EDITABLE
  // (maintainer decision 2026-08-27): the sale is still recorded under the name it was made in,
  // which may differ from the register's spelling, and buyer_name is what the ledger and the buyer
  // board have always shown. The vendor id is the durable link; these two are the snapshot.
  const [vendorId, setVendorId] = useState("");
  const [vendorQuery, setVendorQuery] = useState("");
  const [buyerName, setBuyerName] = useState("");
  const [buyerPlace, setBuyerPlace] = useState("");
  const [recordError, setRecordError] = useState<{ code: string; message: string } | null>(null);
  // Save sits at the drawer's FOOT and the refusal renders at the form's HEAD (or beside the lines,
  // for the stock question): without this the desk pressed Save, the drawer stayed scrolled to the
  // bottom, and nothing on screen changed -- "Advance amount cannot be more than the sale value"
  // was on the page, just not where anyone was looking.
  const recordAlertRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (recordError) recordAlertRef.current?.scrollIntoView({ block: "center", behavior: "smooth" });
  }, [recordError]);
  const [recordPending, setRecordPending] = useState(false);
  // A REF as well as the state: two clicks in one frame both see the render's `recordPending`
  // (false), so only a ref read at the click can refuse the second one.
  const recordPendingRef = useRef(false);
  // The sale's idempotency key belongs to the FORM, like a receipt's (payment-idempotency.ts):
  // minted when the form opens, carried by every submit of it, and rotated once an attempt
  // settles -- so a double click is one sale and a deliberate second sale is a new key.
  const [saleKey, setSaleKey] = useState(mintPaymentKey);
  // The short-stock tick confirms the feed on screen WHEN it was ticked. Controlled, and cleared
  // whenever a line or the farm changes: an uncontrolled box stayed ticked after the quantity went
  // up, and the backend (which trusts the tick) recorded the bigger sale unasked.
  const [stockAck, setStockAck] = useState(false);
  const changeLines = (next: SaleLineDraft[]) => {
    setLines(next);
    setStockAck(false);
  };
  const [syncedSelection, setSyncedSelection] = useState(selection);
  if (syncedSelection !== selection) {
    // Reset during render when the drawer opens on a different record, never in an effect -- an
    // effect would let one submit's values flash into the next form.
    setSyncedSelection(selection);
    setRecordError(null);
    setLines([newSaleLine(1, products[0]?.name ?? "")]);
    setStockAck(false);
    setSaleKey(mintPaymentKey());
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

  // Escape and the backdrop reach `close` through the template Drawer's onClose (MUI traps focus in
  // the paper and returns it to the opener); a second Escape listener here would step history back twice.

  const none = copy(pageContract, "value.none");
  const notApplicable = copy(pageContract, "value.not_applicable");
  const field = (key: string) => copy(pageContract, `field.${key}`);
  const title = isAdding ? copy(pageContract, "drawer.record_sale.title") : copy(pageContract, "drawer.detail.title");
  const showPaymentActions = canUpdatePayment || canDeletePayment;

  // The write vocabulary excludes the read-scope "all" entry: a deal happens at ONE farm.
  const farmOptions = optionGroup(pageContract, "sales_farms").filter((option) => option.key !== "all");

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

  const plannedSaleDate = deal ? plannedSaleDateIfDifferent(deal) : null;

  // One read-only cell pair of the record body.
  const cell = (label: string, value: string | number | null | undefined) => (
    <DrawerMetaItem key={label} label={label}>
      {value === null || value === undefined || value === "" ? none : value}
    </DrawerMetaItem>
  );

  const saveButton = (
    // A sale cannot be recorded without a vendor, so Save is disabled-with-reason rather than left
    // live to fail at the backend with a message about a field the form could not offer. The
    // route validates the same rule regardless. It sits in the drawer footer and submits the form
    // by id.
    <Button
      type="submit"
      form={recordFormId}
      variant="contained"
      color="primary"
      loading={recordPending}
      disabled={!canPickVendor}
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
    </Button>
  );

  return (
    <DetailDrawer
      open={open}
      onClose={close}
      title={isAdding ? title : (deal?.buyer_name ?? title)}
      eyebrow={copy(pageContract, "crumb")}
      icon={<Banknote aria-hidden="true" />}
      iconColors={{ bg: "var(--brand-soft)", fg: "var(--info)" }}
      subtitle={deal ? `${fmtDate(deal.sale_date)} · ${deal.farm}` : undefined}
      ariaLabel={title}
      closeLabel={copy(pageContract, "action.close")}
      footer={
        isAdding ? (
          <>
            <Button type="button" variant="outlined" color="inherit" onClick={close}>
              {copy(pageContract, "action.cancel")}
            </Button>
            {saveButton}
          </>
        ) : undefined
      }
    >
        {isAdding ? (
          <form onSubmit={async (event) => {
            event.preventDefault();
            if (recordPendingRef.current) return;
            recordPendingRef.current = true;
            const data = new FormData(event.currentTarget);
            setRecordPending(true);
            setRecordError(null);
            try {
              const result = await recordSaleAction(data);
              setRecordError(result ?? null);
            } finally {
              recordPendingRef.current = false;
              setRecordPending(false);
              // The attempt settled (a refusal came back; a save redirects and remounts anyway):
              // the next submit is a new intent with a new key.
              setSaleKey(mintPaymentKey());
            }
          }} id={recordFormId} style={{ display: "contents" }}>
              {/* Refusals return in place, preserving controlled and native form fields. */}
              <input type="hidden" name="return_to" value={listHref} />
              <input type="hidden" name={PAYMENT_IDEMPOTENCY_FIELD} value={saleKey} />

              {recordError && recordError.code !== "feed_stock_confirmation_required" ? <Alert ref={recordAlertRef} role="alert" severity="warning">{recordError.message}</Alert> : null}
              <DrawerHint>{copy(pageContract, "required.hint")}</DrawerHint>

              <Box sx={FIELD_SX}>
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
              </Box>
              <Box sx={FIELD_SX}>
                <FormSelect
                  label={field("farm")}
                  name="farm"
                  id="s-farm"
                  onValueChange={() => setStockAck(false)}
                  defaultValue=""
                  required
                  options={listOptions(farmOptions, (option) => option.key, (option) => option.label, "—")}
                />
              </Box>
              <SaleLinesEditor
                lines={lines}
                onChange={changeLines}
                pageContract={pageContract}
                products={products}
                variants={variants}
              />
              {stockConfirmNeeded || recordError?.code === "feed_stock_confirmation_required" ? (
                // The short-feed-sale confirmation (maintainer decision 2026-09-23). It appears
                // only after the backend has asked for it, and it is NOT checked by default: a
                // tick the form carries on its own is not a confirmation of anything.
                <Box sx={FIELD_SX} ref={recordError?.code === "feed_stock_confirmation_required" ? recordAlertRef : undefined}>
                  {recordError?.message || stockConfirmDetail ? <Alert role="alert" severity="warning">{recordError?.message || stockConfirmDetail}</Alert> : null}
                  <FormControlLabel
                    control={
                      <Checkbox
                        id="s-stock_ack"
                        name="stock_shortfall_acknowledged"
                        value="1"
                        checked={stockAck}
                        onChange={(event) => setStockAck(event.target.checked)}
                        sx={{ p: { xs: 1.5, sm: 1 } }}
                      />
                    }
                    label={<>{" "}
                      {copy(pageContract, "field.stock_shortfall_ack")}</>}
                  />
                  <DrawerHint>{copy(pageContract, "hint.stock_shortfall_ack")}</DrawerHint>
                </Box>
              ) : null}

              <DrawerGroup>{field("vendor")}</DrawerGroup>
              <Box sx={FIELD_SX}>
                {vendorsUnavailable ? (
                  // The register could not be READ. Say that, rather than render an empty dropdown
                  // that reads as "no vendors exist" and sends the person to add a duplicate.
                  <Alert severity="warning">{copy(pageContract, "error.vendors_unavailable")}</Alert>
                ) : vendorsTruncated ? (
                  <>
                    <Alert severity="warning">{copy(pageContract, "hint.vendor_truncated")}</Alert>
                    <Button component={Link} href="/procurement/vendors" size="small" variant="outlined" color="inherit" sx={{ alignSelf: "flex-start" }}>
                      {copy(pageContract, "action.open_vendors")}
                    </Button>
                  </>
                ) : vendorsEmpty ? (
                  <>
                    <DrawerHint>{copy(pageContract, "hint.vendor_empty")}</DrawerHint>
                    <Button component={Link} href="/procurement/vendors" size="small" variant="outlined" color="inherit" sx={{ alignSelf: "flex-start" }}>
                      {copy(pageContract, "action.open_vendors")}
                    </Button>
                  </>
                ) : (
                  <>
                    <Box sx={{ display: "flex", gap: 0.75, alignItems: "center", mb: 0.75 }}>
                      <TextField
                        fullWidth
                        size="small"
                        sx={{ flex: "1 1 auto", minWidth: 0 }}
                        id="s-vendor_search"
                        type="search"
                        // NOT part of the form payload: this filters the options and is never
                        // submitted. The select below still carries the id that gets recorded.
                        name="vendor_search"
                        autoComplete="off"
                        placeholder={copy(pageContract, "search.vendor.placeholder")}
                        value={vendorQuery}
                        onChange={(event) => setVendorQuery(event.target.value)}
                        slotProps={{ htmlInput: { "aria-controls": "s-buyer_vendor_id" } }}
                      />
                      {vendorQuery === "" ? null : (
                        <Button type="button" size="small" variant="outlined" sx={{ flex: "none", whiteSpace: "nowrap" }} onClick={() => setVendorQuery("")}>
                          {copy(pageContract, "action.clear_search")}
                        </Button>
                      )}
                    </Box>
                    {/* The kit field, not the OS listbox: the search input above still narrows
                        `shownVendors`, and the value is reported exactly as the native control
                        reported it, so the form's payload is unchanged. */}
                    <FormSelect
                      label={copy(pageContract, "select.vendor.placeholder")}
                      name="buyer_vendor_id"
                      id="s-buyer_vendor_id"
                      required
                      value={vendorId}
                      onValueChange={onVendorChange}
                      // The field's floating label already says "choose the vendor"; the empty
                      // entry is the dash every other field uses, so the label is not printed twice.
                      options={listOptions(
                        shownVendors,
                        (vendor) => vendor.vendor_id,
                        (vendor) => vendorLabel(vendor),
                        "—",
                      )}
                    />
                    {noMatches ? <DrawerHint>{copy(pageContract, "hint.vendor_no_match")}</DrawerHint> : null}
                    {/* The exit from a required field the person may not be able to fill: the
                        buyer might simply not be on the register yet. */}
                    <DrawerHint>
                      {copy(pageContract, "hint.vendor")}{" "}
                      <MuiLink component={Link} href="/procurement/vendors" color="info" underline="always">
                        {copy(pageContract, "action.open_vendors")}
                      </MuiLink>
                    </DrawerHint>
                  </>
                )}
              </Box>
              <Box sx={FIELD_SX}>
                <TextField
                  fullWidth
                  id="s-buyer_name"
                  name="buyer_name"
                  label={field("buyer_name")}
                  required
                  value={buyerName}
                  onChange={(event) => setBuyerName(event.target.value)}
                  slotProps={{ htmlInput: { maxLength: 160 }, inputLabel: { shrink: true } }}
                />
              </Box>
              <Box sx={FIELD_SX}>
                <TextField
                  fullWidth
                  id="s-buyer_place"
                  name="buyer_place"
                  label={field("buyer_place")}
                  value={buyerPlace}
                  onChange={(event) => setBuyerPlace(event.target.value)}
                  slotProps={{ htmlInput: { maxLength: 160 }, inputLabel: { shrink: true } }}
                />
              </Box>
              <DrawerGroup>{copy(pageContract, "section.payments.title")}</DrawerGroup>
              <Box sx={FIELD_SX}>
                <TextField
                  fullWidth
                  id="s-advance_amount"
                  name="advance_amount"
                  type="number"
                  label={field("advance_amount")}
                  slotProps={{ htmlInput: { min: 0, step: 0.01 }, inputLabel: { shrink: true } }}
                />
              </Box>
              <Box sx={FIELD_SX}>
                {/* Defaults to Deal Closed — a recorded sale is a finished one unless the desk says
                    otherwise. Advance Paid / In Discussion record an EXPECTED sale (a future sale
                    date is fine); the receipt itself is dated by when the money arrived. */}
                <FormSelect
                  label={field("status")}
                  name="status"
                  id="s-status"
                  defaultValue="Deal Closed"
                  options={listOptions(dealStatusOptions, (option) => option.key, (option) => option.label)}
                />
                <DrawerHint>{copy(pageContract, "hint.status")}</DrawerHint>
              </Box>
              <Box sx={FIELD_SX}>
                <TextField
                  fullWidth
                  multiline
                  rows={2}
                  id="s-comments"
                  name="comments"
                  label={field("comments")}
                  slotProps={{ htmlInput: { maxLength: 2000 }, inputLabel: { shrink: true } }}
                />
              </Box>
          </form>
        ) : deal ? (
          <>
            {/* RECORD drawer body: label/value cells, never a flat stack. */}
            <DrawerMetaGrid>
              {cell(field("sale_date"), fmtDate(deal.sale_date))}
              {plannedSaleDate ? cell(field("planned_sale_date"), fmtDate(plannedSaleDate)) : null}
              {cell(field("farm"), deal.farm)}
              {cell(field("product_type"), deal.product_type)}
              {/* A manure line's breed is only its own name again: the cell is left out rather than
                  repeating the product or claiming a breed was not recorded. */}
              {breedBeyondProduct(deal.product_type, deal.breed) ? cell(field("breed"), breedBeyondProduct(deal.product_type, deal.breed)) : null}
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
              <DrawerMetaItem label={copy(pageContract, "column.status")}>
                <Tag tone={dealStatusTone(deal.status)}>{deal.status}</Tag>
              </DrawerMetaItem>
              {cell(field("comments"), deal.comments)}
            </DrawerMetaGrid>

            {/* WHAT WAS SOLD: one row per product/breed line (migration 000296). The product,
                breed, animals, weight and value cells above are the backend's ROLLUP of these. */}
            <DrawerGroup>{copy(pageContract, "section.lines.title")}</DrawerGroup>
            {deal.lines.length === 0 ? (
              <DrawerHint>{copy(pageContract, "detail.lines.empty")}</DrawerHint>
            ) : (
              // Its own pan region: headers, breed and quantity wrap so the six columns fit a
              // laptop drawer, but a phone is narrower than any readable six-column table, so the
              // table pans here rather than painting off the screen.
              <DrawerTableScroll>
              <Table size="small" sx={{ minWidth: 640 }} data-testid="sale-detail-lines" aria-label={copy(pageContract, "section.lines.title")}>
                <TableHead>
                  <TableRow>
                    <TableCell component="th">{field("product_type")}</TableCell>
                    <TableCell component="th">{field("breed")}</TableCell>
                    <TableCell component="th" align="right">{copy(pageContract, "field.line_animal_count")}</TableCell>
                    <TableCell component="th" align="right">{copy(pageContract, "field.line_total_weight_kg")}</TableCell>
                    <TableCell component="th" align="right">{copy(pageContract, "field.line_quantity_rate")}</TableCell>
                    <TableCell component="th" align="right">{copy(pageContract, "field.sales_value")}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {deal.lines.map((line) => {
                    // A cell that does not belong to the line's kind reads as a dash, never as
                    // "Not recorded": an animal line owes no quantity, and a line sold by the unit
                    // owes no head count or weight.
                    const byUnit = line.quantity != null;
                    const isAnimal = line.product_kind === "animal";
                    return (
                      <TableRow key={line.line_id}>
                        <TableCell>{line.product_type}</TableCell>
                        <TableCell>{breedBeyondProduct(line.product_type, line.breed) ?? ""}</TableCell>
                        <TableCell align="right">{line.animal_count == null ? (byUnit ? notApplicable : none) : num(line.animal_count)}</TableCell>
                        <TableCell align="right">{line.total_weight_kg == null ? (byUnit ? notApplicable : none) : num(line.total_weight_kg, 1)}</TableCell>
                        <TableCell align="right">{quantityAtRate(line.quantity, line.unit, line.rate_per_unit) || (isAnimal ? notApplicable : none)}</TableCell>
                        <TableCell align="right">{inr(line.sales_value)}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
              </DrawerTableScroll>
            )}

            {/* PAYMENTS: what the buyer has handed over, what is still owed, the receipt history,
                and — behind its backend control — the add-payment write. */}
            <DrawerGroup>{copy(pageContract, "section.payments.title")}</DrawerGroup>
            <DrawerMetaGrid>
              {cell(
                copy(pageContract, "payments.received_so_far"),
                deal.payment_received == null ? null : inr(deal.payment_received),
              )}
              {/* BACKEND-derived; this cell renders the figure and never subtracts anything itself. */}
              {cell(copy(pageContract, "payments.balance"), inr(deal.payment_balance))}
            </DrawerMetaGrid>

            {deal.payments.length === 0 ? (
              <DrawerHint>{copy(pageContract, "payments.empty")}</DrawerHint>
            ) : (
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: showPaymentActions ? 560 : 400 }} aria-label={copy(pageContract, "section.payments.title")}>
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{copy(pageContract, "payments.column.received_on")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "payments.column.amount")}</TableCell>
                      <TableCell component="th">{copy(pageContract, "payments.column.note")}</TableCell>
                      {showPaymentActions ? <TableCell component="th" /> : null}
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {deal.payments.map((payment) => (
                      <PaymentRow
                        key={payment.payment_id}
                        payment={payment}
                        dealId={deal.deal_id}
                        dealHref={dealHref}
                        pageContract={pageContract}
                        canUpdate={canUpdatePayment}
                        canDelete={canDeletePayment}
                        showActions={showPaymentActions}
                      />
                    ))}
                  </TableBody>
                </Table>
              </DrawerTableScroll>
            )}

            {canRecordPayment ? (
              <RecordPaymentForm
                // One form per deal: opening another deal is a new receipt, never the last one's key.
                key={deal.deal_id}
                deal={deal}
                dealHref={dealHref}
                pageContract={pageContract}
              />
            ) : null}

            {canEditStatus && editStatusOptions.length > 0 ? (
              <Box component="form" action={setSalesDealStatusAction} sx={FIELD_SX}>
                <input type="hidden" name="return_to" value={dealHref} />
                <input type="hidden" name="deal_id" value={deal.deal_id} />
                <Box sx={{ display: "flex", gap: 1, alignItems: "flex-end" }}>
                  <FormSelect
                    label={field("status")}
                    name="status"
                    id="sds-status"
                    defaultValue={deal.status}
                    required
                    options={listOptions(editStatusOptions, (option) => option.key, (option) => option.label)}
                  />
                  <Button type="submit" variant="outlined">
                    {copy(pageContract, "action.update_deal_status.label")}
                  </Button>
                </Box>
                {statusStockConfirmNeeded ? (
                  // Closing takes the sale's feed off the store, so the close asks the same
                  // question recording it did. Shown only after the backend has asked, and NOT
                  // ticked by default: a tick the form carries on its own confirms nothing.
                  <Box sx={FIELD_SX}>
                    {stockConfirmDetail ? <Alert severity="warning">{stockConfirmDetail}</Alert> : null}
                    <FormControlLabel
                      control={<Checkbox id="sds-stock_ack" name="stock_shortfall_acknowledged" value="1" sx={{ p: { xs: 1.5, sm: 1 } }} />}
                      label={<>{" "}
                    {copy(pageContract, "field.stock_shortfall_ack")}</>}
                    />
                    <DrawerHint>{copy(pageContract, "hint.stock_shortfall_ack")}</DrawerHint>
                  </Box>
                ) : null}
              </Box>
            ) : null}
          </>
        ) : null}
    </DetailDrawer>
  );
}

type paymentActionResult = Promise<SalesPaymentActionError | undefined>;
type paymentAction = (
  formData: FormData,
) => paymentActionResult;
type PaymentFormState = { settled: number; error: SalesPaymentActionError | null };
const PAYMENT_FORM_IDLE: PaymentFormState = { settled: 0, error: null };

function mintPaymentKey(): string {
  return crypto.randomUUID();
}

/**
 * One payment form's write state: the action, whether it is in flight, the refusal it came back
 * with, and the idempotency key the form carries.
 *
 * The key is minted when the form is shown and held while nothing about the form's outcome has
 * moved, so every click on the same form posts the same key and the backend replays the first
 * receipt rather than recording the money twice. It rotates when `outcome` changes (the receipt
 * landed and the deal's payments re-read) or an attempt settles, so the next receipt is a new key.
 * A successful save redirects, which remounts the drawer and mints afresh anyway.
 */
function usePaymentFormAction(action: paymentAction, outcome: string) {
  const [state, formAction, pending] = useActionState(
    async (previous: PaymentFormState, formData: FormData): Promise<PaymentFormState> => ({
      settled: previous.settled + 1,
      error: (await action(formData)) ?? null,
    }),
    PAYMENT_FORM_IDLE,
  );
  const rotation = `${outcome}#${state.settled}`;
  const [minted, setMinted] = useState<MintedPaymentKey>(() => paymentKeyFor(null, rotation, mintPaymentKey));
  const next = paymentKeyFor(minted, rotation, mintPaymentKey);
  if (next !== minted) {
    // Reset during render, never in an effect: an effect would let one render submit the old key.
    setMinted(next);
  }
  // Submitted through onSubmit, NOT `<form action>`: React resets a form after an action-prop
  // submit settles, so a refused receipt ("Received on cannot be in the future.") came back with
  // the amount and note the desk had typed wiped -- and the next click posted an empty form. The
  // FormData is read at the click, so a double click still carries the one key.
  const onSubmit = (event: FormEvent<HTMLFormElement>) => {
    // A field's own submit guard (ThemedDatePicker with no date) already refused this submit and
    // showed its reason; posting anyway would reach the action without the field.
    if (event.defaultPrevented) return;
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    startTransition(() => formAction(data));
  };
  return { onSubmit, pending, error: state.error, key: next.key };
}

/** The backend's field sentence when it named one, else this form's contract copy. */
function paymentErrorText(pageContract: AdminUiPageContract, error: SalesPaymentActionError, fallbackKey: string): string {
  return error.message || copy(pageContract, fallbackKey);
}

/** The "add a receipt" form under a deal's payment history. */
function RecordPaymentForm({
  deal,
  dealHref,
  pageContract,
}: {
  deal: SalesDeal;
  dealHref: string;
  pageContract: AdminUiPageContract;
}) {
  const field = (key: string) => copy(pageContract, `field.${key}`);
  const outcome = deal.payments.map((payment) => payment.payment_id).join(",");
  const { onSubmit, pending, error, key } = usePaymentFormAction(recordSalesDealPaymentAction, outcome);
  return (
    <form onSubmit={onSubmit} aria-busy={pending}>
      <input type="hidden" name="return_to" value={dealHref} />
      <input type="hidden" name="deal_id" value={deal.deal_id} />
      <input type="hidden" name={PAYMENT_IDEMPOTENCY_FIELD} value={key} />
      {error ? (
        <Alert role="alert" severity="warning" sx={{ mb: 1.5 }}>
          {paymentErrorText(pageContract, error, "action.payment_record_failed")}
        </Alert>
      ) : null}
      <Box sx={FIELD_SX}>
        <ThemedDatePicker
          name="received_on"
          label={field("received_on")}
          required
          previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
          nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
          invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
        />
      </Box>
      <Box sx={FIELD_SX}>
        <TextField
          fullWidth
          id="sdp-amount"
          name="amount_rupees"
          type="number"
          label={field("amount_rupees")}
          required
          slotProps={{ htmlInput: { min: 0.01, step: 0.01 }, inputLabel: { shrink: true } }}
        />
      </Box>
      <Box sx={FIELD_SX}>
        <TextField
          fullWidth
          id="sdp-note"
          name="note"
          label={field("note")}
          slotProps={{ htmlInput: { maxLength: 300 }, inputLabel: { shrink: true } }}
        />
        <DrawerHint>{copy(pageContract, "hint.record_payment")}</DrawerHint>
      </Box>
      <Button type="submit" variant="contained" color="primary" disabled={pending} aria-disabled={pending}>
        {copy(pageContract, "action.record_deal_payment.label")}
      </Button>
    </form>
  );
}

/**
 * One receipt in the payment history, with its edit and remove forms. The two forms live in the
 * actions cell (a form may sit in a cell, never across a row); the row's date/amount/note inputs
 * join the edit form through their `form` attribute.
 */
function PaymentRow({
  payment,
  dealId,
  dealHref,
  pageContract,
  canUpdate,
  canDelete,
  showActions,
}: {
  payment: SalesDeal["payments"][number];
  dealId: string;
  dealHref: string;
  pageContract: AdminUiPageContract;
  canUpdate: boolean;
  canDelete: boolean;
  showActions: boolean;
}) {
  const none = copy(pageContract, "value.none");
  const editFormId = `sales-payment-edit-${payment.payment_id}`;
  const deleteFormId = `sales-payment-delete-${payment.payment_id}`;
  // The edit key rotates once the stored receipt changes, so a second edit -- even one back to an
  // earlier value -- is a new intent with a new key.
  const edit = usePaymentFormAction(
    updateSalesDealPaymentAction,
    `${payment.received_on}|${payment.amount_rupees}|${payment.note}`,
  );
  const remove = usePaymentFormAction(deleteSalesDealPaymentAction, payment.payment_id);
  const busy = edit.pending || remove.pending;
  const refusal = edit.error
    ? paymentErrorText(pageContract, edit.error, "action.payment_update_failed")
    : remove.error
      ? paymentErrorText(pageContract, remove.error, "action.payment_delete_failed")
      : "";
  return (
    <>
      <TableRow aria-busy={busy}>
        <TableCell style={{ whiteSpace: "nowrap" }}>
          {canUpdate ? (
            <TextField
              size="small"
              type="date"
              required
              defaultValue={payment.received_on}
              slotProps={{
                htmlInput: {
                  form: editFormId,
                  name: "received_on",
                  "aria-label": copy(pageContract, "payments.column.received_on"),
                },
              }}
            />
          ) : (
            fmtDate(payment.received_on)
          )}
        </TableCell>
        <TableCell style={{ whiteSpace: "nowrap" }}>
          {canUpdate ? (
            <TextField
              size="small"
              type="number"
              required
              defaultValue={payment.amount_rupees}
              slotProps={{
                htmlInput: {
                  form: editFormId,
                  name: "amount_rupees",
                  min: 0.01,
                  step: 0.01,
                  "aria-label": copy(pageContract, "payments.column.amount"),
                },
              }}
            />
          ) : (
            inr(payment.amount_rupees)
          )}
        </TableCell>
        <TableCell>
          {canUpdate ? (
            <TextField
              size="small"
              defaultValue={payment.note}
              slotProps={{
                htmlInput: {
                  form: editFormId,
                  name: "note",
                  maxLength: 300,
                  "aria-label": copy(pageContract, "payments.column.note"),
                },
              }}
            />
          ) : (
            payment.note || none
          )}
        </TableCell>
        {showActions ? (
          <TableCell style={{ whiteSpace: "nowrap" }}>
            {canUpdate ? (
              <form id={editFormId} onSubmit={edit.onSubmit} hidden>
                <input type="hidden" name="return_to" value={dealHref} />
                <input type="hidden" name="deal_id" value={dealId} />
                <input type="hidden" name="payment_id" value={payment.payment_id} />
                <input type="hidden" name={PAYMENT_IDEMPOTENCY_FIELD} value={edit.key} />
              </form>
            ) : null}
            {canDelete ? (
              <form id={deleteFormId} onSubmit={remove.onSubmit} hidden>
                <input type="hidden" name="return_to" value={dealHref} />
                <input type="hidden" name="deal_id" value={dealId} />
                <input type="hidden" name="payment_id" value={payment.payment_id} />
                <input type="hidden" name={PAYMENT_IDEMPOTENCY_FIELD} value={remove.key} />
              </form>
            ) : null}
            <Box sx={{ display: "flex", gap: 1, justifyContent: "flex-end" }}>
              {canUpdate ? (
                <IconButton
                  type="submit"
                  form={editFormId}
                  size="small"
                  disabled={busy}
                  aria-disabled={busy}
                  aria-label={copy(pageContract, "action.update_deal_payment.label")}
                  title={copy(pageContract, "action.update_deal_payment.label")}
                >
                  <Save className="ic" aria-hidden="true" />
                </IconButton>
              ) : null}
              {canDelete ? (
                <IconButton
                  type="submit"
                  form={deleteFormId}
                  size="small"
                  disabled={busy}
                  aria-disabled={busy}
                  aria-label={copy(pageContract, "action.delete_deal_payment.label")}
                  title={copy(pageContract, "action.delete_deal_payment.label")}
                >
                  <Trash2 className="ic" aria-hidden="true" />
                </IconButton>
              ) : null}
            </Box>
          </TableCell>
        ) : null}
      </TableRow>
      {refusal ? (
        <TableRow>
          <TableCell colSpan={showActions ? 4 : 3}>
            <Alert role="alert" severity="warning">
              {refusal}
            </Alert>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}

/** One field block (control + its hint) in the drawer's form column. */
const FIELD_SX = { display: "flex", flexDirection: "column", gap: 1, minWidth: 0 } as const;

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
