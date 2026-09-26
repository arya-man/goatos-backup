"use client";

import { Building2 } from "lucide-react";
import { useCallback, useState, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ProcurementVendor, ProcurementVendorCatalog, ProcurementVendorForm } from "@/lib/api/server";
import { changeVendorStatusAction, createVendorAction, updateVendorAction } from "./vendor-actions";
import { VendorFormFields, vendorAnswerRows } from "./vendor-form-fields";
import { VendorVoiceNote } from "./vendor-voice-note";
import { FormSelect } from "./form-select";
import Typography from "@mui/material/Typography";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import { MinimalDrawer } from "@/components/minimal/drawer";

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
 * 2. It renders in the template MinimalDrawer (portalled MUI Drawer: backdrop, focus trap, Escape),
 *    so no ancestor animation can trap it and the page behind never jumps.
 */
export function VendorLocalDrawer({
  vendors,
  catalog,
  form = null,
  pageContract,
  listHref,
}: {
  /** The rendered page of vendors. The drawer opens from this data -- it issues no fetch of its own. */
  vendors: ProcurementVendor[];
  catalog: ProcurementVendorCatalog | null;
  /**
   * VENDOR FORM IS AUTHORED (2026-09-19): the published sales.vendor form. When present the
   * add / edit body is rendered from it, question by question; the hard-coded field list below
   * is the fallback for a backend that could not serve the form.
   */
  form?: ProcurementVendorForm | null;
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current filters, without the vendor param). */
  listHref: string;
}) {
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

  if (!catalog) return null;

  const none = copy(pageContract, "value.none");
  const field = (key: string) => copy(pageContract, `field.${key}`);
  const title = isAdding
    ? copy(pageContract, "drawer.add.title")
    : editing
      ? copy(pageContract, "drawer.edit.title")
      : copy(pageContract, "drawer.detail.title");

  // One read-only label/value cell of the record body (template order-detail info rows).
  const cell = (label: string, value: React.ReactNode) => (
    <Stack key={label} spacing={0.5} sx={{ minWidth: 0 }}>
      <Typography variant="caption" sx={{ color: "text.secondary" }}>
        {label}
      </Typography>
      <Typography variant="body2" component="div" sx={{ overflowWrap: "anywhere" }}>
        {value === null || value === undefined || value === "" ? none : value}
      </Typography>
    </Stack>
  );
  const recordGrid = (children: React.ReactNode) => (
    <Box sx={{ display: "grid", gap: 2, gridTemplateColumns: { xs: "repeat(2, minmax(0, 1fr))" } }}>{children}</Box>
  );
  const note = (text: string) => <Alert severity="info">{text}</Alert>;
  const textField = (
    name: string,
    label: string,
    options: { id?: string; required?: boolean; maxLength?: number; defaultValue?: string | number | null; type?: "number"; inputMode?: "decimal"; multiline?: boolean; min?: number; step?: number } = {},
  ) => (
    <TextField
      fullWidth
      id={options.id}
      name={name}
      label={label}
      required={options.required}
      type={options.type}
      multiline={options.multiline}
      rows={options.multiline ? 2 : undefined}
      defaultValue={options.defaultValue ?? ""}
      slotProps={{
        inputLabel: { shrink: true },
        htmlInput: { maxLength: options.maxLength, inputMode: options.inputMode, min: options.min, step: options.step },
      }}
    />
  );
  const formId = "vendor-drawer-form";

  const footer = editing ? (
    <>
      <Button type="button" variant="outlined" color="inherit" onClick={() => (isAdding ? close() : setEditing(false))}>
        {copy(pageContract, "action.cancel")}
      </Button>
      <Button type="submit" form={formId} variant="contained" color="primary">
        {copy(pageContract, "action.save")}
      </Button>
    </>
  ) : vendor ? (
    <Stack direction="row" spacing={1.5} sx={{ width: 1, alignItems: "center", flexWrap: "wrap", rowGap: 1.5 }}>
      <Button type="button" variant="contained" color="primary" onClick={() => setEditing(true)}>
        {copy(pageContract, "action.edit")}
      </Button>
      {/* Quick status change, without opening the full form. It posts to the NARROW status
          endpoint, so it cannot clear a field this view did not render. */}
      <Box component="form" action={changeVendorStatusAction} sx={{ display: "flex", gap: 1, alignItems: "center", ml: "auto" }}>
        <input type="hidden" name="return_to" value={listHref} />
        <input type="hidden" name="vendor_id" value={vendor.vendor_id} />
        <input type="hidden" name="row_version" value={vendor.row_version} />
        <FormSelect size="small" label={field("status")} name="status" defaultValue={vendor.status} minWidth={140} options={optionsFor(catalog.statuses, vendor.status)} />
        <Button type="submit" variant="outlined" color="inherit">
          {copy(pageContract, "action.save_status")}
        </Button>
      </Box>
    </Stack>
  ) : null;

  return (
    <MinimalDrawer
      open={open}
      onClose={close}
      title={isAdding ? title : (vendor?.business_name ?? title)}
      closeLabel={copy(pageContract, "action.close")}
      width={480}
      footer={footer}
      aria-label={title}
    >
      <Box sx={{ p: 2.5 }}>
        {vendor ? (
          <Stack direction="row" spacing={1.5} sx={{ alignItems: "center", mb: 3 }}>
            <Building2 className="ic" aria-hidden="true" style={{ color: "var(--info)" }} />
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {vendor.record_type} · {vendor.location_display}
            </Typography>
          </Stack>
        ) : null}

        {editing ? (
          <form id={formId} action={isAdding ? createVendorAction : updateVendorAction}>
            <input type="hidden" name="return_to" value={listHref} />
            {!isAdding && vendor ? (
              <>
                <input type="hidden" name="vendor_id" value={vendor.vendor_id} />
                {/* The optimistic fence, carried from the row this form was opened on. */}
                <input type="hidden" name="row_version" value={vendor.row_version} />
              </>
            ) : null}
            <Stack spacing={3}>
              {form ? (
                <VendorFormFields form={form} vendor={vendor} pageContract={pageContract} />
              ) : (
                <Stack spacing={2.5}>
                  {note(copy(pageContract, isAdding ? "required.hint.create" : "required.hint"))}
                  {textField("business_name", field("business_name"), { id: "v-business_name", required: true, maxLength: 160, defaultValue: vendor?.business_name })}
                  <FormSelect
                    fullWidth
                    label={field("record_type")}
                    name="record_type"
                    id="v-record_type"
                    defaultValue={vendor?.record_type ?? ""}
                    required
                    options={[{ value: "", label: none }, ...optionsFor(catalog.record_types, vendor?.record_type)]}
                  />
                  {textField("contact_person_name", field("contact_person_name"), { id: "v-contact", required: isAdding, maxLength: 160, defaultValue: vendor?.contact_person_name })}
                  {textField("phone_number", field("phone_number"), { id: "v-phone", required: isAdding, maxLength: 64, defaultValue: vendor?.phone_number })}
                  <FormSelect fullWidth label={field("status")} name="status" id="v-status" defaultValue={vendor?.status ?? "active"} required options={optionsFor(catalog.statuses, vendor?.status)} />
                  <FormSelect
                    fullWidth
                    label={field("state")}
                    name="state"
                    id="v-state"
                    defaultValue={vendor?.state ?? ""}
                    required
                    options={[{ value: "", label: none }, ...optionsFor(catalog.states, vendor?.state)]}
                  />
                  {textField("city", field("city"), { id: "v-city", required: isAdding, maxLength: 160, defaultValue: vendor?.city })}
                  <FormSelect fullWidth label={field("breed")} name="breed" id="v-breed" defaultValue={vendor?.breed ?? ""} options={[{ value: "", label: none }, ...optionsFor(catalog.breeds, vendor?.breed)]} />
                  <FormSelect fullWidth label={field("feed")} name="feed" id="v-feed" defaultValue={vendor?.feed ?? ""} options={[{ value: "", label: none }, ...optionsFor(catalog.feeds, vendor?.feed)]} />
                  {/* No default of 0 for an absent reading: blank means "not recorded", 0 means
                      "checked, none available". They are different facts. */}
                  {textField("filtered_stock", field("filtered_stock"), { id: "v-stock", type: "number", min: 0, step: 1, defaultValue: vendor?.filtered_stock })}
                  {textField("price_per_goat", field("price_per_goat"), { id: "v-price", inputMode: "decimal", defaultValue: vendor?.price_per_goat })}
                  {/* Optional. Sent verbatim as a string so the backend validates the number; a blank
                      stores NULL ("not recorded"), never 0. */}
                  {textField("average_animal_weight_kg", field("average_animal_weight"), { id: "v-avg_weight", inputMode: "decimal", defaultValue: vendor?.average_animal_weight_kg })}
                  {textField("eta_after_order_days", field("eta_after_order"), { id: "v-eta", type: "number", min: 0, step: 1, defaultValue: vendor?.eta_after_order_days })}
                  {textField("ready_to_filtered", field("ready_to_filtered"), { id: "v-ready", maxLength: 160, defaultValue: vendor?.ready_to_filtered })}
                  {textField("details", field("details"), { id: "v-details", maxLength: 2000, multiline: true, defaultValue: vendor?.details })}

                  {/* CAPACITY (maintainer decision 2026-09-03): how much per delivery, in what unit,
                      how often. Quantity and unit are a pair; the backend refuses one without the
                      other, so they sit on one row. The vocabularies are catalog entries. */}
                  <Divider sx={{ borderStyle: "dashed" }} />
                  <Typography variant="subtitle2" component="h4">
                    {field("capacity")}
                  </Typography>
                  <Box sx={{ display: "grid", gridTemplateColumns: "2fr 1fr", gap: 2 }}>
                    {textField("capacity_quantity", field("capacity_quantity"), { id: "v-capacity_quantity", inputMode: "decimal", defaultValue: vendor?.capacity_quantity })}
                    <FormSelect
                      fullWidth
                      label={field("capacity_unit")}
                      name="capacity_unit"
                      id="v-capacity_unit"
                      defaultValue={vendor?.capacity_unit ?? ""}
                      options={[{ value: "", label: none }, ...optionsFor(catalog.capacity_units, vendor?.capacity_unit ?? undefined)]}
                    />
                  </Box>
                  <Stack spacing={0.75}>
                    <FormSelect
                      fullWidth
                      label={field("supply_frequency")}
                      name="supply_frequency"
                      id="v-supply_frequency"
                      defaultValue={vendor?.supply_frequency ?? ""}
                      options={[{ value: "", label: none }, ...optionsFor(catalog.supply_frequencies, vendor?.supply_frequency ?? undefined)]}
                    />
                    <Typography variant="caption" sx={{ color: "text.secondary" }}>
                      {copy(pageContract, "hint.capacity")}
                    </Typography>
                  </Stack>
                </Stack>
              )}
              {/* The voice note is recorded on the phone; the web edit carries it through unchanged. */}
              <input type="hidden" name="voice_note_proof_ref" value={vendor?.voice_note_proof_ref ?? ""} />

              {/* Payment is only editable by a caller who can also READ it. The backend additionally
                  PRESERVES these columns for such a caller, so a blank submit cannot erase a bank
                  account they were never shown. */}
              {vendor?.finance_redacted ? (
                note(copy(pageContract, "payment.hidden"))
              ) : (
                <Stack spacing={2.5}>
                  <Divider sx={{ borderStyle: "dashed" }} />
                  <Typography variant="subtitle2" component="h4">
                    {copy(pageContract, "group.payment")}
                  </Typography>
                  {textField("bank_name", field("bank_name"), { id: "v-bank", maxLength: 160, defaultValue: vendor?.bank_name })}
                  {textField("account_no", field("account_no"), { id: "v-account", maxLength: 160, defaultValue: vendor?.account_no })}
                  {textField("ifsc_code", field("ifsc_code"), { id: "v-ifsc", maxLength: 160, defaultValue: vendor?.ifsc_code })}
                  {textField("upi_id", field("upi_id"), { id: "v-upi", maxLength: 160, defaultValue: vendor?.upi_id })}
                  {textField("pan_number", field("pan_number"), { id: "v-pan", maxLength: 160, defaultValue: vendor?.pan_number })}
                </Stack>
              )}

              {form ? null : textField("comments", field("comments"), { id: "v-comments", maxLength: 2000, multiline: true, defaultValue: vendor?.comments })}
            </Stack>
          </form>
        ) : vendor ? (
          <Stack spacing={3}>
            {recordGrid(
              <>
                {cell(field("record_type"), vendor.record_type)}
                {cell(field("status"), <Tag tone={statusTone(vendor.status)}>{vendor.status_label}</Tag>)}
                {cell(field("contact_person_name"), vendor.contact_person_name)}
                {cell(field("phone_number"), vendor.phone_number)}
                {cell(field("state"), vendor.state)}
                {cell(field("city"), vendor.city)}
                {cell(field("breed"), vendor.breed)}
                {cell(field("feed"), vendor.feed)}
                {cell(field("filtered_stock"), vendor.filtered_stock)}
                {cell(field("price_per_goat"), vendor.price_per_goat)}
                {/* BACKEND-composed "35 kg"; empty renders the shared "Not recorded" copy. */}
                {cell(field("average_animal_weight"), vendor.average_animal_weight_display)}
                {cell(field("eta_after_order"), vendor.eta_after_order_days)}
                {cell(field("ready_to_filtered"), vendor.ready_to_filtered)}
                {cell(field("details"), vendor.details)}
                {/* BACKEND-composed capacity line ("5,000 kg · Every 2 weeks"); the raw fields are
                    for prefilling the form only. */}
                {cell(field("capacity"), vendor.capacity_display)}
                {cell(field("comments"), vendor.comments)}
                {/* Answers to the questions the published form added beyond the register's
                    columns, labelled by the form the drawer holds (a question since removed shows
                    its key). */}
                {vendorAnswerRows(form, vendor).map((row) => cell(row.label, row.value))}
              </>,
            )}

            <Divider sx={{ borderStyle: "dashed" }} />
            <Typography variant="subtitle2" component="h4">
              {field("voice_note")}
            </Typography>
            {vendor.voice_note_proof_ref ? (
              <VendorVoiceNote
                proofRef={vendor.voice_note_proof_ref}
                loadLabel={copy(pageContract, "voice_note.play")}
                unavailableCopy={copy(pageContract, "voice_note.unavailable")}
              />
            ) : (
              note(copy(pageContract, "voice_note.none"))
            )}

            <Divider sx={{ borderStyle: "dashed" }} />
            <Typography variant="subtitle2" component="h4">
              {copy(pageContract, "group.payment")}
            </Typography>
            {vendor.finance_redacted ? (
              // Never a blank block: withheld and absent must not look the same.
              note(copy(pageContract, "payment.hidden"))
            ) : vendor.bank_name || vendor.account_no || vendor.ifsc_code || vendor.upi_id || vendor.pan_number ? (
              recordGrid(
                <>
                  {cell(field("bank_name"), vendor.bank_name)}
                  {cell(field("account_no"), vendor.account_no)}
                  {cell(field("ifsc_code"), vendor.ifsc_code)}
                  {cell(field("upi_id"), vendor.upi_id)}
                  {cell(field("pan_number"), vendor.pan_number)}
                </>,
              )
            ) : (
              note(copy(pageContract, "payment.none"))
            )}
          </Stack>
        ) : null}
      </Box>
    </MinimalDrawer>
  );
}
