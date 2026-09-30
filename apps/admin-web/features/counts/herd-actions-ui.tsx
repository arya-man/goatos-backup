"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

// Counts -> Herd Register write UI: the "Register goat" modal (single create) and the "Import sheet" modal
// (bulk preview -> commit). Mock-faithful modal behavior (centered overlay, backdrop/Escape close, focus, footer
// actions) over the Mesha theme. Single create posts a real server action (createGoatAction) and redirects
// with a banner; bulk calls real preview/commit server actions and holds ONLY the backend's response as
// transient UI state. No fixtures, no fake totals, no client-only business mutation.
import { useRef, useState, useTransition } from "react";
import { useFormStatus } from "react-dom";
import { useRouter } from "next/navigation";

import { FormSelect } from "@/components/form-select";
import { Tag, type Tone } from "@/components/ui-primitives";
import { RowMenu } from "@/components/app/row-menu";
import type { LocationOption } from "@/lib/api/herd-locations";
import type { AdminGoatBulkResponse, CreateAdminGoatRequest } from "@/lib/api/server";
import { copy, optionalOptionGroup, optionGroup, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { todayIso } from "@/lib/format";
import {
  commitGoatsAction,
  commitShedsAction,
  createGoatAction,
  createShedAction,
  previewGoatsAction,
  previewShedsAction,
  reproductiveGoatAction,
  type ShedImportCommitRow,
  type ShedImportResponse,
} from "./herd-actions";
import { csvCell, isSpreadsheetFile, parseCSVRecords, sheetImportAccept, spreadsheetArrayBufferToCSV, stableCSVContentHash } from "./herd-import-utils";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { UploadFile } from "@/components/app/upload-file";
import { useBackCloses } from "@/components/use-back-closes";

export type HerdAnimalStageOption = {
  code: string;
  label: string;
};

export type HerdOperationalLocationOption = {
  key: string;
  shedId: string;
  parkId: string | null;
  partitionLabel: string | null;
  label: string;
};

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function decisionLabel(pageContract: AdminUiPageContract, decision: string): string {
  return optionLabel(pageContract, "herd_bulk_decisions", String(decision));
}

function csvFilename(label: string): string {
  return label.toLowerCase().endsWith(".csv") ? label : `${label}.csv`;
}

type ImportResultRow = {
  row_number: number;
  decision: string;
  errors: { field: string; message: string }[];
  warnings?: { message: string }[];
};

function importRowFailed(row: ImportResultRow): boolean {
  return row.errors.length > 0 || row.decision === "requires_review";
}

function failedImportRows<T extends ImportResultRow>(rows: T[]): T[] {
  return rows.filter(importRowFailed);
}

function failureNotes(row: ImportResultRow): string {
  const messages = [
    ...row.errors.map((error) => `${error.field}: ${error.message}`),
    ...(row.warnings ?? []).map((warning) => warning.message),
  ];
  return messages.join(" · ");
}

function downloadCSV(filename: string, records: unknown[][]) {
  const body = `${records.map((record) => record.map(csvCell).join(",")).join("\n")}\n`;
  const blob = new Blob([body], { type: "text/csv" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = csvFilename(filename);
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

function downloadFailedRows(filename: string, csv: string, templateColumns: string[], rows: ImportResultRow[], failureHeader: string, parseErrorMessage: string) {
  const failed = failedImportRows(rows);
  if (failed.length === 0) return;
  const parsed = parseCSVRecords(csv, { unterminatedQuoteMessage: parseErrorMessage });
  const header = parsed[0]?.length ? parsed[0] : templateColumns;
  const records: unknown[][] = [[...header, failureHeader]];
  failed.forEach((row) => {
    const source = parsed[row.row_number - 1] ?? [];
    records.push([...header.map((_, index) => source[index] ?? ""), failureNotes(row)]);
  });
  downloadCSV(filename, records);
}

function mergeGoatCommitResult(preview: AdminGoatBulkResponse, committed: AdminGoatBulkResponse): AdminGoatBulkResponse {
  const sentRows = preview.rows.filter((row) => row.decision === "create" && row.normalized);
  const committedRows = committed.rows.map((row, index) => {
    const source = sentRows[index];
    return {
      ...row,
      row_number: source?.row_number ?? row.row_number,
      normalized: row.normalized ?? source?.normalized,
    };
  });
  const rows = [
    ...failedImportRows(preview.rows),
    ...committedRows,
  ].sort((a, b) => a.row_number - b.row_number);
  const failed = failedImportRows(rows).length;
  return {
    ...committed,
    rows,
    summary: {
      ...committed.summary,
      total: preview.summary.total,
      create_ready: preview.summary.create_ready,
      requires_review: failed,
      skipped: preview.summary.skipped + committed.summary.skipped,
      failed,
    },
  };
}

function mergeShedCommitResult(preview: ShedImportResponse, committed: ShedImportResponse): ShedImportResponse {
  const committedByRow = new Map(committed.rows.map((row) => [row.row_number, row]));
  const rows = preview.rows
    .map((row) => committedByRow.get(row.row_number) ?? row)
    .sort((a, b) => a.row_number - b.row_number);
  const failed = failedImportRows(rows).length;
  return {
    ...committed,
    rows,
    summary: {
      ...committed.summary,
      total: preview.summary.total,
      create_ready: preview.summary.create_ready,
      requires_review: failed,
      failed,
    },
  };
}

// ---- Dialog shell: template MUI Dialog (portal above the FAB, focus trap + restore, Escape/scrim close,
// body scroll lock). Browser Back closes it too (the open state is component state, not a URL). A form
// dialog is the template quick-edit anatomy: the form wraps DialogContent + DialogActions, so the
// footer buttons stay pinned under the scrolling fields and the submit button still sees the form. ----
type DialogForm = {
  action: (formData: FormData) => void | Promise<void>;
  onSubmit: () => void;
};

export function HerdActionDialog({
  open,
  onClose,
  closeLabel,
  title,
  maxWidth = 760,
  form,
  actions,
  children,
}: {
  open: boolean;
  onClose: () => void;
  closeLabel: string;
  title: string;
  subtitle?: string;
  /** Widest the dialog gets; always clamped to the viewport. */
  maxWidth?: number;
  /** Server action form wrapping the content and the footer actions. */
  form?: DialogForm;
  /** Template DialogActions row (Cancel + primary). */
  actions?: React.ReactNode;
  children: React.ReactNode;
}) {
  useBackCloses(open, onClose);
  const body = (
    <>
      <DialogContent dividers sx={{ pt: 1 }}>
        <Stack spacing={2.5} sx={{ py: 2 }}>{children}</Stack>
      </DialogContent>
      {actions ? <DialogActions sx={{ gap: 1, flexWrap: "wrap" }}>{actions}</DialogActions> : null}
    </>
  );
  return (
    <Dialog
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth={false}
      scroll="paper"
      slotProps={{ paper: { "aria-label": title, sx: { width: 1, maxWidth } } as object }}
    >
      <DialogTitle component="div" sx={{ display: "flex", alignItems: "center", gap: 1.5, pr: 1 }}>
        <Box component="span" aria-hidden="true" sx={{ display: "inline-flex", color: "primary.main" }}>
          <Iconify icon="mingcute:add-line" />
        </Box>
        {/* Title only: the page-contract subtitle stays unrendered (no prose under titles), as before. */}
        <Typography variant="h6" component="h3" sx={{ flexGrow: 1, minWidth: 0 }}>{title}</Typography>
        <IconButton onClick={onClose} aria-label={closeLabel}>
          <Iconify icon="mingcute:close-line" aria-hidden="true" />
        </IconButton>
      </DialogTitle>
      {form ? (
        <Box
          component="form"
          action={form.action}
          onSubmit={form.onSubmit}
          sx={{ display: "flex", flexDirection: "column", flex: "1 1 auto", minHeight: 0 }}
        >
          {body}
        </Box>
      ) : (
        body
      )}
    </Dialog>
  );
}

function SubmitButton({ pageContract, children }: { pageContract: AdminUiPageContract; children: React.ReactNode }) {
  const { pending } = useFormStatus();
  return (
    <Button type="submit" variant="contained" color="primary" disabled={pending} aria-busy={pending}>
      {pending ? copy(pageContract, "action.working") : children}
    </Button>
  );
}

function CancelButton({ onClick, children }: { onClick: () => void; children: React.ReactNode }) {
  return (
    <Button type="button" variant="outlined" color="inherit" onClick={onClick}>
      {children}
    </Button>
  );
}

/** Template quick-edit field grid: one column on phones, `columns` from sm. */
function FieldGrid({ columns = 2, children }: { columns?: number; children: React.ReactNode }) {
  return (
    <Box sx={{ display: "grid", gap: 2, gridTemplateColumns: { xs: "minmax(0, 1fr)", sm: `repeat(${columns}, minmax(0, 1fr))` } }}>
      {children}
    </Box>
  );
}

/** Outlined template text field with its label floating over the control (never a label above it). */
function Field({
  id,
  name,
  label,
  placeholder,
  required,
  type,
  defaultValue,
  htmlInput,
}: {
  id: string;
  name: string;
  label: string;
  placeholder?: string;
  required?: boolean;
  type?: string;
  defaultValue?: string | number;
  htmlInput?: Record<string, unknown>;
}) {
  return (
    <TextField
      id={id}
      name={name}
      label={label}
      placeholder={placeholder}
      required={required}
      type={type}
      defaultValue={defaultValue}
      fullWidth
      slotProps={{ inputLabel: { shrink: true }, htmlInput }}
    />
  );
}

/** The app date field (ThemedDatePicker) under a caption naming it, so a picked date keeps its name. */
function DateField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Stack spacing={0.75} sx={{ minWidth: 0 }}>
      <Typography variant="caption" component="span" sx={{ color: "text.secondary", fontWeight: "fontWeightSemiBold" }}>
        {label}
      </Typography>
      {children}
    </Stack>
  );
}

function ErrorAlert({ title, body }: { title: string; body: React.ReactNode }) {
  return (
    <Alert severity="error">
      <AlertTitle>{title}</AlertTitle>
      {body}
    </Alert>
  );
}

function Note({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

const MUTED_CELL = { color: "text.secondary" } as const;
const MONO_FIELD = { "& textarea": { fontFamily: "monospace", typography: "caption" } } as const;

/** Import results: the template table card (Scrollbar + Table) inside the dialog. */
function ImportResultsTable({ head, children }: { head: React.ReactNode; children: React.ReactNode }) {
  return (
    <Card variant="outlined">
      <Scrollbar>
        <Table size="small" sx={{ minWidth: 640 }}>
          <TableHead>
            <TableRow>{head}</TableRow>
          </TableHead>
          <TableBody>{children}</TableBody>
        </Table>
      </Scrollbar>
    </Card>
  );
}

/** Step 1 of both import dialogs: download the template, upload or paste the sheet, preview. */
function ImportSource({
  pageContract,
  columns,
  templateLabel,
  templateNote,
  onDownloadTemplate,
  fileInputId,
  csvId,
  csv,
  onCSV,
  onFile,
  fileInput,
  pending,
  hasPreview,
  onPreview,
  accept,
}: {
  pageContract: AdminUiPageContract;
  columns: string[];
  templateLabel: string;
  templateNote: string;
  onDownloadTemplate: () => void;
  fileInputId: string;
  csvId: string;
  csv: string;
  onCSV: (next: string) => void;
  onFile: (file: File | null) => void;
  fileInput: React.RefObject<HTMLInputElement | null>;
  pending: boolean;
  hasPreview: boolean;
  onPreview: () => void;
  /** The picker's accepted types (CSV + spreadsheets). */
  accept: string;
}) {
  return (
    <>
      <Stack spacing={1} sx={{ alignItems: "flex-start" }}>
        <Typography variant="subtitle2">
          {copy(pageContract, "field.bulk_template")}{" "}
          <Box component="span" sx={{ color: "text.secondary", typography: "caption" }}>
            ({columns.length} {copy(pageContract, "label.columns")})
          </Box>
        </Typography>
        <Button type="button" size="small" variant="outlined" color="inherit" startIcon={<Iconify icon="solar:download-bold" aria-hidden="true" />} onClick={onDownloadTemplate}>
          {templateLabel}
        </Button>
        <Typography variant="caption" sx={{ color: "text.secondary" }}>{columns.join(" · ")}</Typography>
        <Note>{templateNote}</Note>
      </Stack>
      <UploadFile
        inputRef={fileInput}
        testId={fileInputId}
        accept={accept}
        ariaLabel={copy(pageContract, "field.bulk_upload")}
        title={copy(pageContract, "field.bulk_upload")}
        onFileChange={onFile}
      />
      <TextField
        id={csvId}
        label={copy(pageContract, "field.bulk_paste")}
        multiline
        rows={5}
        value={csv}
        onChange={(e) => onCSV(e.target.value)}
        placeholder={columns.join(",")}
        fullWidth
        sx={MONO_FIELD}
        slotProps={{ inputLabel: { shrink: true } }}
      />
      <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}>
        <Button type="button" variant="contained" color="primary" onClick={onPreview} disabled={pending || csv.trim() === ""} aria-busy={pending}>
          {pending ? copy(pageContract, "action.previewing_rows") : copy(pageContract, "action.preview_rows")}
        </Button>
        {hasPreview ? <Typography variant="caption" sx={{ color: "text.secondary" }}>{copy(pageContract, "note.preview_ready")}</Typography> : null}
      </Stack>
    </>
  );
}

// ---- Register goat modal (single create) ----
function RegisterGoatDrawer({
  open,
  onClose,
  parks,
  sheds,
  operationalLocations,
  operationalLocationsAvailable,
  farms,
  animalStages,
  locationsAvailable,
  stagesAvailable,
  idempotencyKey,
  returnTo,
  pageContract,
}: {
  open: boolean;
  onClose: () => void;
  parks: LocationOption[];
  sheds: LocationOption[];
  operationalLocations: HerdOperationalLocationOption[];
  operationalLocationsAvailable: boolean;
  farms: LocationOption[];
  animalStages: HerdAnimalStageOption[];
  locationsAvailable: boolean;
  stagesAvailable: boolean;
  idempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const [parkId, setParkId] = useState<string>(parks[0]?.id ?? "");
  const [selectedLocationKey, setSelectedLocationKey] = useState<string>("");
  const [species, setSpecies] = useState<string>("");
  const scopedSheds = sheds.filter((s) => s.parentId === parkId);
  const shedOptions = scopedSheds.length > 0 ? scopedSheds : sheds;
  const partitionedShedIds = new Set(
    operationalLocations
      .filter((location) => location.partitionLabel)
      .map((location) => location.shedId),
  );
  const operationalOptions = operationalLocations
    .filter((location) => !parkId || location.parkId === parkId)
    .filter((location) => location.partitionLabel || !partitionedShedIds.has(location.shedId));
  const operationalShedIds = new Set(operationalOptions.map((location) => location.shedId));
  const unpartitionedEmptyShedOptions = operationalLocationsAvailable
    ? shedOptions
      .filter((shed) => !partitionedShedIds.has(shed.id) && !operationalShedIds.has(shed.id))
      .map((shed) => ({
        key: shed.id,
        shedId: shed.id,
        parkId: shed.parentId,
        partitionLabel: null,
        label: `${shed.name}${shed.code ? ` · ${shed.code}` : ""}`,
      }))
    : [];
  const locationOptions = operationalLocationsAvailable
    ? [...operationalOptions, ...unpartitionedEmptyShedOptions]
    : [];
  const selectedLocation = locationOptions.find((location) => location.key === selectedLocationKey) ?? locationOptions[0] ?? null;
  const sexOptions = optionGroup(pageContract, "herd_sex");
  const speciesOptions = optionGroup(pageContract, "herd_species");
  const originOptions = optionGroup(pageContract, "herd_origin");
  // The farm's own breeds (Configuration > Breeds), each naming its species in `group`. A backend
  // that predates the list serves no group, and the form keeps its free-text breed box.
  const breedGroup = pageContract.option_groups.find((group) => group.id === "herd_breeds");
  const breedOptions = (breedGroup?.options ?? []).filter((o) => !o.group || o.group === species);

  const hasLocations = locationsAvailable && parks.length > 0 && locationOptions.length > 0;
  const hasStages = stagesAvailable && animalStages.length > 0;
  const canCreate = hasLocations && hasStages;

  return (
    <HerdActionDialog
      open={open}
      onClose={onClose}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.register.title")}
      subtitle={copy(pageContract, "drawer.register.subtitle")}
      maxWidth={820}
      // The action redirects (banner). Close the modal as the form submits so the banner is visible and
      // the client modal state does not linger over the navigated page. onSubmit fires only after the
      // browser's required-field validation passes, and React still dispatches the action this event.
      form={{ action: createGoatAction, onSubmit: () => onClose() }}
      actions={
        <>
          <CancelButton onClick={onClose}>{copy(pageContract, "action.cancel")}</CancelButton>
          {canCreate ? (
            <SubmitButton pageContract={pageContract}>{copy(pageContract, "action.register_goat")}</SubmitButton>
          ) : (
            <Button type="button" variant="contained" color="primary" disabled aria-disabled="true">{copy(pageContract, "action.register_goat")}</Button>
          )}
        </>
      }
    >
      {!hasLocations ? (
        <ErrorAlert title={copy(pageContract, "alert.locations.title")} body={copy(pageContract, "alert.locations.body")} />
      ) : null}
      {!hasStages ? (
        <ErrorAlert title={copy(pageContract, "alert.stages.title")} body={copy(pageContract, "alert.stages.body")} />
      ) : null}

      <input type="hidden" name="idempotency_key" value={idempotencyKey} />
      <input type="hidden" name="return_to" value={returnTo} />
      <input type="hidden" name="evidence_type" value="source_record" />

      <FieldGrid>
        <Field id="rg_animal_id_1" name="animal_identifier_1" required label={copy(pageContract, "field.animal_identifier_1")} placeholder={copy(pageContract, "placeholder.animal_identifier_1")} />
        <Field id="rg_animal_id_2" name="animal_identifier_2" label={copy(pageContract, "field.animal_identifier_2")} placeholder={copy(pageContract, "placeholder.animal_identifier_2")} />
      </FieldGrid>
      <FieldGrid columns={3}>
        <FormSelect
          name="species"
          required
          fullWidth
          label={copy(pageContract, "field.species")}
          value={species}
          onChange={setSpecies}
          options={[
            { value: "", label: copy(pageContract, "option.select_species") },
            ...speciesOptions.map((o) => ({ value: o.key, label: o.label })),
          ]}
        />
        <FormSelect
          name="park_id"
          required
          fullWidth
          disabled={!canCreate}
          label={copy(pageContract, "field.park_required")}
          value={parkId}
          onChange={setParkId}
          options={
            parks.length === 0
              ? [{ value: "", label: copy(pageContract, "option.no_parks") }]
              : parks.map((p) => ({ value: p.id, label: `${p.name}${p.code ? ` · ${p.code}` : ""}` }))
          }
        />
        <Box sx={{ minWidth: 0 }}>
          <input type="hidden" name="shed_id" value={selectedLocation?.shedId ?? ""} />
          <input type="hidden" name="partition_label" value={selectedLocation?.partitionLabel ?? ""} />
          <FormSelect
            required
            fullWidth
            disabled={!canCreate}
            label={copy(pageContract, "field.shed_required")}
            value={selectedLocation?.key ?? ""}
            onChange={setSelectedLocationKey}
            options={
              locationOptions.length === 0
                ? [{ value: "", label: copy(pageContract, "option.no_vaccination_sheds") }]
                : locationOptions.map((location) => ({ value: location.key, label: location.label }))
            }
          />
        </Box>
      </FieldGrid>
      <FieldGrid columns={3}>
        <FormSelect
          name="farm_id"
          fullWidth
          label={copy(pageContract, "field.farm")}
          defaultValue=""
          options={[
            { value: "", label: copy(pageContract, "option.optional") },
            ...farms.map((f) => ({ value: f.id, label: `${f.name}${f.code ? ` · ${f.code}` : ""}` })),
          ]}
        />
        {breedGroup ? (
          // Keyed on the species so a breed picked under another species never survives the switch.
          <FormSelect
            key={species}
            name="breed"
            fullWidth
            label={copy(pageContract, "field.breed")}
            defaultValue=""
            disabled={!species}
            helperText={species && breedOptions.length === 0 ? copy(pageContract, "note.no_breeds_for_species") : undefined}
            options={[
              { value: "", label: copy(pageContract, species ? "option.select_breed" : "option.select_species_first") },
              ...breedOptions.map((o) => ({ value: o.key, label: o.label })),
            ]}
          />
        ) : (
          <Field id="rg_breed" name="breed" label={copy(pageContract, "field.breed")} placeholder={copy(pageContract, "placeholder.breed")} />
        )}
        <FormSelect
          name="management_stage"
          required
          fullWidth
          disabled={!hasStages}
          label={copy(pageContract, "field.management_stage")}
          defaultValue={animalStages[0]?.code ?? ""}
          options={
            animalStages.length === 0
              ? [{ value: "", label: copy(pageContract, "alert.stages.title") }]
              : animalStages.map((stage) => ({ value: stage.code, label: stage.label }))
          }
        />
      </FieldGrid>

      <FieldGrid>
        <FormSelect
          name="sex"
          required
          fullWidth
          label={copy(pageContract, "field.sex")}
          defaultValue=""
          options={[
            { value: "", label: copy(pageContract, "option.select_sex") },
            ...sexOptions.map((o) => ({ value: o.key, label: o.label })),
          ]}
        />
        <FormSelect
          name="origin_type"
          required
          fullWidth
          label={copy(pageContract, "field.origin")}
          defaultValue=""
          options={[
            { value: "", label: copy(pageContract, "option.select_origin") },
            ...originOptions.map((o) => ({ value: o.key, label: o.label })),
          ]}
        />
      </FieldGrid>

      <FieldGrid columns={3}>
        <DateField label={copy(pageContract, "field.dob")}>
          <ThemedDatePicker
            name="dob"
            label={copy(pageContract, "field.dob")}
            max={todayIso()}
            required
            previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
            nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
            invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
          />
        </DateField>
        <Field id="rg_weight" name="weight_kg" type="number" label={copy(pageContract, "field.weight_kg")} placeholder={copy(pageContract, "placeholder.weight_kg")} htmlInput={{ min: 0, step: "0.1" }} />
        <DateField label={copy(pageContract, "field.entry_date_required")}>
          <ThemedDatePicker
            name="entry_date"
            label={copy(pageContract, "field.entry_date_required")}
            defaultValue={todayIso()}
            required
            previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
            nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
            invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
          />
        </DateField>
      </FieldGrid>
      <FormControlLabel control={<Checkbox name="dob_estimated" sx={{ p: { xs: 1.5, sm: 1 } }} />} label={copy(pageContract, "field.dob_estimated")} />

      <FieldGrid>
        <Field id="rg_dam" name="dam_id" label={copy(pageContract, "field.dam_id")} placeholder={copy(pageContract, "placeholder.dam_id")} />
        <Field id="rg_sire" name="sire_or_lot" label={copy(pageContract, "field.sire_or_lot")} placeholder={copy(pageContract, "placeholder.sire_or_lot")} />
      </FieldGrid>

      <Field id="rg_evidence" name="evidence_id" label={copy(pageContract, "field.evidence_ref")} placeholder={copy(pageContract, "placeholder.evidence_ref")} />
    </HerdActionDialog>
  );
}

// ---- Register shed modal (single create) ----
function RegisterShedDrawer({
  open,
  onClose,
  parks,
  idempotencyKey,
  returnTo,
  pageContract,
}: {
  open: boolean;
  onClose: () => void;
  parks: LocationOption[];
  idempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const canCreate = parks.length > 0;

  return (
    <HerdActionDialog
      open={open}
      onClose={onClose}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.shed_register.title")}
      subtitle={copy(pageContract, "drawer.shed_register.subtitle")}
      maxWidth={760}
      form={{ action: createShedAction, onSubmit: () => onClose() }}
      actions={
        <>
          <CancelButton onClick={onClose}>{copy(pageContract, "action.cancel")}</CancelButton>
          {canCreate ? (
            <SubmitButton pageContract={pageContract}>{copy(pageContract, "action.register_shed")}</SubmitButton>
          ) : (
            <Button type="button" variant="contained" color="primary" disabled aria-disabled="true">{copy(pageContract, "action.register_shed")}</Button>
          )}
        </>
      }
    >
      {!canCreate ? (
        <ErrorAlert title={copy(pageContract, "alert.locations.title")} body={copy(pageContract, "alert.shed_locations.body")} />
      ) : null}

      <input type="hidden" name="idempotency_key" value={idempotencyKey} />
      <input type="hidden" name="return_to" value={returnTo} />

      <FormSelect
        name="park_id"
        required
        fullWidth
        disabled={!canCreate}
        label={copy(pageContract, "field.park_required")}
        defaultValue={parks[0]?.id ?? ""}
        options={
          parks.length === 0
            ? [{ value: "", label: copy(pageContract, "option.no_parks") }]
            : parks.map((p) => ({ value: p.id, label: `${p.name}${p.code ? ` · ${p.code}` : ""}` }))
        }
      />

      <FieldGrid>
        <Field id="rs_code" name="location_code" label={copy(pageContract, "field.shed_code")} placeholder={copy(pageContract, "placeholder.shed_code")} />
        <Field id="rs_name" name="name" required label={copy(pageContract, "field.shed_name_required")} placeholder={copy(pageContract, "placeholder.shed_name")} />
      </FieldGrid>

      <FieldGrid>
        <Field id="rs_order" name="display_order" type="number" defaultValue={0} label={copy(pageContract, "field.display_order")} htmlInput={{ min: 0, step: 1 }} />
        <Field id="rs_notes" name="notes" label={copy(pageContract, "field.notes")} placeholder={copy(pageContract, "placeholder.shed_notes")} />
      </FieldGrid>

      <Note>{copy(pageContract, "note.shed_create")}</Note>
    </HerdActionDialog>
  );
}

// ---- Bulk import modal (download template -> paste/upload CSV -> preview -> commit) ----
function BulkImportDrawer({ open, onClose, pageContract }: { open: boolean; onClose: () => void; pageContract: AdminUiPageContract }) {
  const router = useRouter();
  const fileInput = useRef<HTMLInputElement | null>(null);
  const [csv, setCsv] = useState("");
  const [preview, setPreview] = useState<AdminGoatBulkResponse | null>(null);
  const [previewHash, setPreviewHash] = useState<string | null>(null);
  const [committed, setCommitted] = useState<AdminGoatBulkResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const configuredBulkColumns = optionGroup(pageContract, "herd_import_columns").map((column) => column.label);
  const bulkColumns = configuredBulkColumns;

  function reset() {
    setCsv("");
    setPreview(null);
    setPreviewHash(null);
    setCommitted(null);
    setError(null);
  }

  function updateCSV(next: string) {
    setCsv(next);
    setPreview(null);
    setPreviewHash(null);
    setCommitted(null);
    setError(null);
  }

  function close() {
    reset();
    onClose();
  }

  function downloadTemplate() {
    const header = bulkColumns.join(",");
    const blob = new Blob([`${header}\n`], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = csvFilename(copy(pageContract, "action.download_template"));
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  async function onFile(file: File | null) {
    if (!file) return;
    try {
      const next = isSpreadsheetFile(file.name, file.type)
        ? await spreadsheetArrayBufferToCSV(
            await file.arrayBuffer(),
            copy(pageContract, "error.xlsx_empty"),
            copy(pageContract, "error.xlsx_parse_failed"),
          )
        : await file.text();
      updateCSV(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : copy(pageContract, "error.xlsx_parse_failed"));
    } finally {
      // Clear the picker so the same file can be picked again after a parse error.
      if (fileInput.current) fileInput.current.value = "";
    }
  }

  function runPreview() {
    setError(null);
    setCommitted(null);
    startTransition(async () => {
      const hash = await stableCSVContentHash(csv);
      const res = await previewGoatsAction(csv, hash);
      if (res.ok) {
        setPreview(res.data);
        setPreviewHash(hash);
      } else {
        setPreviewHash(null);
        setError(`${res.error.code ?? res.error.kind}: ${res.error.message}`);
      }
    });
  }

  function runCommit() {
    if (!preview) return;
    const rows = preview.rows
      .filter((r) => r.decision === "create" && r.normalized)
      .map((r) => ({ row_number: r.row_number, normalized: r.normalized as CreateAdminGoatRequest }));
    if (rows.length === 0) return;
    setError(null);
    startTransition(async () => {
      const hash = await stableCSVContentHash(csv);
      if (hash !== previewHash) {
        setError(copy(pageContract, "error.preview_stale"));
        return;
      }
      const res = await commitGoatsAction(rows, hash, preview.preview_token ?? "");
      if (res.ok) {
        setCommitted(mergeGoatCommitResult(preview, res.data));
        router.refresh();
      } else {
        setError(`${res.error.code ?? res.error.kind}: ${res.error.message}`);
      }
    });
  }

  const view = committed ?? preview;
  // Count what we will actually send (create rows that carry a normalized payload), so the button number
  // never overstates the commit versus summary.create_ready.
  const committable = preview ? preview.rows.filter((r) => r.decision === "create" && r.normalized).length : 0;
  const failedCount = view ? failedImportRows(view.rows).length : 0;

  return (
    <HerdActionDialog
      open={open}
      onClose={close}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.import.title")}
      subtitle={copy(pageContract, "drawer.import.subtitle")}
      maxWidth={900}
      actions={
        view ? (
          <>
            {failedCount > 0 ? (
              <Button type="button" variant="outlined" color="inherit" startIcon={<Iconify icon="solar:download-bold" aria-hidden="true" />} onClick={() => downloadFailedRows(copy(pageContract, "action.download_failed_goat_rows"), csv, bulkColumns, view.rows, copy(pageContract, "field.failure_reason"), copy(pageContract, "error.csv_unterminated_quote"))}>
                {copy(pageContract, "action.export_failed_rows")} ({failedCount})
              </Button>
            ) : null}
            {committed ? (
              <Button type="button" variant="contained" color="primary" onClick={close}>{copy(pageContract, "action.done")}</Button>
            ) : (
              <>
                <CancelButton onClick={() => { setPreview(null); setError(null); }}>{copy(pageContract, "action.re_edit")}</CancelButton>
                <Button
                  type="button"
                  variant="contained"
                  color="primary"
                  startIcon={<Iconify icon="eva:checkmark-fill" aria-hidden="true" />}
                  onClick={runCommit}
                  disabled={pending || committable === 0}
                  aria-disabled={committable === 0}
                  aria-busy={pending}
                >
                  {pending ? copy(pageContract, "action.creating_records") : `${copy(pageContract, "action.create_records")} (${committable})`}
                </Button>
              </>
            )}
          </>
        ) : null
      }
    >
      {/* Step 1 — template + input. Hidden once a commit result is shown. */}
      {!committed ? (
        <ImportSource
          pageContract={pageContract}
          columns={bulkColumns}
          templateLabel={copy(pageContract, "action.download_template")}
          templateNote={copy(pageContract, "note.bulk_template")}
          onDownloadTemplate={downloadTemplate}
          fileInputId="bulk_file"
          csvId="bulk_csv"
          csv={csv}
          onCSV={updateCSV}
          onFile={onFile}
          fileInput={fileInput}
          pending={pending}
          hasPreview={Boolean(preview)}
          onPreview={runPreview}
          accept={sheetImportAccept}
        />
      ) : null}

      {error ? (
        <ErrorAlert title={committed ? copy(pageContract, "action.commit_failed") : copy(pageContract, "action.preview_failed")} body={error} />
      ) : null}

      {committed ? (
        <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}>
          <Tag tone="ok">{copy(pageContract, "label.committed")}</Tag>
          <Note>
            {copy(pageContract, "label.created")} {committed.summary.created} · {copy(pageContract, "label.failed")} {committed.summary.failed} · {copy(pageContract, "label.skip")} {committed.summary.skipped} {copy(pageContract, "label.of")} {committed.summary.total} {copy(pageContract, "label.rows")}. {copy(pageContract, "note.committed_suffix")}
          </Note>
        </Stack>
      ) : null}

      {/* Step 3 — preview/commit results table. */}
      {view ? (
        <>
          <Stack direction="row" sx={{ gap: 1, flexWrap: "wrap" }}>
            <Tag tone="mut">{view.summary.total} {copy(pageContract, "label.rows")}</Tag>
            <Tag tone="ok">{view.summary.create_ready} {copy(pageContract, "label.create_ready")}</Tag>
            <Tag tone="warn">{view.summary.requires_review} {copy(pageContract, "label.review")}</Tag>
            <Tag tone="mut">{view.summary.skipped} {copy(pageContract, "label.skip")}</Tag>
            {committed ? <Tag tone={view.summary.failed ? "dng" : "ok"}>{view.summary.created} {copy(pageContract, "label.created")}</Tag> : null}
          </Stack>
          <ImportResultsTable
            head={
              <>
                <TableCell component="th">{copy(pageContract, "field.import_row")}</TableCell>
                <TableCell component="th">{copy(pageContract, "field.import_decision")}</TableCell>
                <TableCell component="th">{copy(pageContract, "field.import_identity")}</TableCell>
                <TableCell component="th">{copy(pageContract, "field.import_notes")}</TableCell>
                {committed ? <TableCell component="th">{copy(pageContract, "field.import_result")}</TableCell> : null}
              </>
            }
          >
            {view.rows.map((r) => {
              const ident = r.normalized?.animal_identifier_1 || r.normalized?.animal_identifier_2 || copy(pageContract, "label.placeholder");
              const notes = [
                ...r.errors.map((e) => `${e.field}: ${e.message}`),
                ...r.warnings.map((w) => w.message),
              ].join(" · ");
              return (
                <TableRow key={r.row_number}>
                  <TableCell sx={MUTED_CELL}>{r.row_number}</TableCell>
                  <TableCell><Tag tone={contractTone(pageContract, "herd_bulk_decisions", String(r.decision))}>{decisionLabel(pageContract, r.decision)}</Tag></TableCell>
                  <TableCell>{ident}</TableCell>
                  <TableCell sx={MUTED_CELL}>{notes || copy(pageContract, "label.placeholder")}</TableCell>
                  {committed ? (
                    <TableCell sx={MUTED_CELL}>
                      {r.result ? r.result.goat.display_id : r.errors.length ? copy(pageContract, "label.failed") : copy(pageContract, "label.placeholder")}
                    </TableCell>
                  ) : null}
                </TableRow>
              );
            })}
          </ImportResultsTable>
        </>
      ) : null}
    </HerdActionDialog>
  );
}

// ---- Shed bulk import modal (download template -> paste/upload CSV -> preview -> commit) ----
function ShedImportDrawer({ open, onClose, pageContract }: { open: boolean; onClose: () => void; pageContract: AdminUiPageContract }) {
  const router = useRouter();
  const fileInput = useRef<HTMLInputElement | null>(null);
  const [csv, setCsv] = useState("");
  const [preview, setPreview] = useState<ShedImportResponse | null>(null);
  const [previewHash, setPreviewHash] = useState<string | null>(null);
  const [committed, setCommitted] = useState<ShedImportResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const shedColumns = optionGroup(pageContract, "shed_import_columns").map((column) => column.label);

  function reset() {
    setCsv("");
    setPreview(null);
    setPreviewHash(null);
    setCommitted(null);
    setError(null);
  }

  function updateCSV(next: string) {
    setCsv(next);
    setPreview(null);
    setPreviewHash(null);
    setCommitted(null);
    setError(null);
  }

  function close() {
    reset();
    onClose();
  }

  function downloadTemplate() {
    const header = shedColumns.join(",");
    const blob = new Blob([`${header}\n`], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = csvFilename(copy(pageContract, "action.download_shed_template"));
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  }

  async function onFile(file: File | null) {
    if (!file) return;
    try {
      const next = isSpreadsheetFile(file.name, file.type)
        ? await spreadsheetArrayBufferToCSV(
            await file.arrayBuffer(),
            copy(pageContract, "error.xlsx_empty"),
            copy(pageContract, "error.xlsx_parse_failed"),
          )
        : await file.text();
      updateCSV(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : copy(pageContract, "error.xlsx_parse_failed"));
    } finally {
      // Clear the picker so the same file can be picked again after a parse error.
      if (fileInput.current) fileInput.current.value = "";
    }
  }

  function runPreview() {
    setError(null);
    setCommitted(null);
    startTransition(async () => {
      const [hash, res] = await Promise.all([stableCSVContentHash(csv), previewShedsAction(csv)]);
      if (res.ok) {
        setPreview(res.data);
        setPreviewHash(hash);
      } else {
        setPreviewHash(null);
        setError(res.message);
      }
    });
  }

  function runCommit() {
    if (!preview) return;
    const rows: ShedImportCommitRow[] = preview.rows.flatMap((r) =>
      r.decision === "create" && r.normalized ? [{ row_number: r.row_number, normalized: r.normalized }] : [],
    );
    if (rows.length === 0) return;
    setError(null);
    startTransition(async () => {
      const hash = await stableCSVContentHash(csv);
      if (hash !== previewHash) {
        setError(copy(pageContract, "error.preview_stale"));
        return;
      }
      const res = await commitShedsAction(rows, hash);
      if (res.ok) {
        setCommitted(mergeShedCommitResult(preview, res.data));
        router.refresh();
      } else {
        setError(res.message);
      }
    });
  }

  const view = committed ?? preview;
  const committable = preview ? preview.rows.filter((r) => r.decision === "create" && r.normalized).length : 0;
  const failedCount = view ? failedImportRows(view.rows).length : 0;

  return (
    <HerdActionDialog
      open={open}
      onClose={close}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.shed_import.title")}
      subtitle={copy(pageContract, "drawer.shed_import.subtitle")}
      maxWidth={900}
      actions={
        view ? (
          <>
            {failedCount > 0 ? (
              <Button type="button" variant="outlined" color="inherit" startIcon={<Iconify icon="solar:download-bold" aria-hidden="true" />} onClick={() => downloadFailedRows(copy(pageContract, "action.download_failed_shed_rows"), csv, shedColumns, view.rows, copy(pageContract, "field.failure_reason"), copy(pageContract, "error.csv_unterminated_quote"))}>
                {copy(pageContract, "action.export_failed_rows")} ({failedCount})
              </Button>
            ) : null}
            {committed ? (
              <Button type="button" variant="contained" color="primary" onClick={close}>{copy(pageContract, "action.done")}</Button>
            ) : (
              <>
                <CancelButton onClick={() => { setPreview(null); setError(null); }}>{copy(pageContract, "action.re_edit")}</CancelButton>
                <Button
                  type="button"
                  variant="contained"
                  color="primary"
                  startIcon={<Iconify icon="eva:checkmark-fill" aria-hidden="true" />}
                  onClick={runCommit}
                  disabled={pending || committable === 0}
                  aria-disabled={committable === 0}
                  aria-busy={pending}
                >
                  {pending ? copy(pageContract, "action.creating_records") : `${copy(pageContract, "action.create_sheds")} (${committable})`}
                </Button>
              </>
            )}
          </>
        ) : null
      }
    >
      {!committed ? (
        <ImportSource
          pageContract={pageContract}
          columns={shedColumns}
          templateLabel={copy(pageContract, "action.download_shed_template")}
          templateNote={copy(pageContract, "note.shed_bulk_template")}
          onDownloadTemplate={downloadTemplate}
          fileInputId="shed_bulk_file"
          csvId="shed_bulk_csv"
          csv={csv}
          onCSV={updateCSV}
          onFile={onFile}
          fileInput={fileInput}
          pending={pending}
          hasPreview={Boolean(preview)}
          onPreview={runPreview}
          accept={sheetImportAccept}
        />
      ) : null}

      {error ? (
        <ErrorAlert title={committed ? copy(pageContract, "action.commit_failed") : copy(pageContract, "action.preview_failed")} body={error} />
      ) : null}

      {committed ? (
        <Stack direction="row" spacing={1} sx={{ alignItems: "center", flexWrap: "wrap" }}>
          <Tag tone="ok">{copy(pageContract, "label.committed")}</Tag>
          <Note>
            {copy(pageContract, "label.created")} {committed.summary.created} · {copy(pageContract, "label.failed")} {committed.summary.failed} {copy(pageContract, "label.of")} {committed.summary.total} {copy(pageContract, "label.rows")}. {copy(pageContract, "note.shed_committed_suffix")}
          </Note>
        </Stack>
      ) : null}

      {view ? (
        <>
          <Stack direction="row" sx={{ gap: 1, flexWrap: "wrap" }}>
            <Tag tone="mut">{view.summary.total} {copy(pageContract, "label.rows")}</Tag>
            <Tag tone="ok">{view.summary.create_ready} {copy(pageContract, "label.create_ready")}</Tag>
            <Tag tone="warn">{view.summary.requires_review} {copy(pageContract, "label.review")}</Tag>
            {committed ? <Tag tone={view.summary.failed ? "dng" : "ok"}>{view.summary.created} {copy(pageContract, "label.created")}</Tag> : null}
          </Stack>
          <ImportResultsTable
            head={
              <>
                <TableCell component="th">{copy(pageContract, "field.import_row")}</TableCell>
                <TableCell component="th">{copy(pageContract, "field.import_decision")}</TableCell>
                <TableCell component="th">{copy(pageContract, "field.shed")}</TableCell>
                <TableCell component="th">{copy(pageContract, "field.import_notes")}</TableCell>
                {committed ? <TableCell component="th">{copy(pageContract, "field.import_result")}</TableCell> : null}
              </>
            }
          >
            {view.rows.map((r) => {
              const ident = r.normalized
                ? `${r.normalized.location_code ? `${r.normalized.location_code} · ` : ""}${r.normalized.name}`
                : r.source_label || copy(pageContract, "label.placeholder");
              const notes = r.errors.map((e) => `${e.field}: ${e.message}`).join(" · ");
              return (
                <TableRow key={r.row_number}>
                  <TableCell sx={MUTED_CELL}>{r.row_number}</TableCell>
                  <TableCell><Tag tone={contractTone(pageContract, "herd_bulk_decisions", r.decision)}>{decisionLabel(pageContract, r.decision)}</Tag></TableCell>
                  <TableCell>{ident}</TableCell>
                  <TableCell sx={MUTED_CELL}>{notes || copy(pageContract, "label.placeholder")}</TableCell>
                  {committed ? (
                    <TableCell sx={MUTED_CELL}>{r.result ? r.result.location.name : r.errors.length ? copy(pageContract, "label.failed") : copy(pageContract, "label.placeholder")}</TableCell>
                  ) : null}
                </TableRow>
              );
            })}
          </ImportResultsTable>
        </>
      ) : null}
    </HerdActionDialog>
  );
}

// ---- Header CTA group rendered inside the page header (replaces the disabled buttons) ----
export function HerdActions({
  parks,
  sheds,
  operationalLocations,
  operationalLocationsAvailable,
  farms,
  animalStages,
  locationsAvailable,
  stagesAvailable,
  idempotencyKey,
  returnTo,
  pageContract,
}: {
  parks: LocationOption[];
  sheds: LocationOption[];
  operationalLocations: HerdOperationalLocationOption[];
  operationalLocationsAvailable: boolean;
  farms: LocationOption[];
  animalStages: HerdAnimalStageOption[];
  locationsAvailable: boolean;
  stagesAvailable: boolean;
  idempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const [openDrawer, setOpenDrawer] = useState<"shed" | "shed-bulk" | "register" | "bulk" | null>(null);

  return (
    <>
      {/* ONE primary (Register animal) + the three secondary writes behind a ⋮ menu: five
          buttons stacked as a ragged grid at phone width. Same drawers, same handlers. */}
      <Button type="button" variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" aria-hidden="true" />} onClick={() => setOpenDrawer("register")}>
        {copy(pageContract, "action.register_goat")}
      </Button>
      <RowMenu
        ariaLabel={copy(pageContract, "action.more", "More actions")}
        actions={[
          { label: copy(pageContract, "action.register_shed"), icon: <Iconify icon="mingcute:add-line" aria-hidden="true" />, onSelect: () => setOpenDrawer("shed") },
          { label: copy(pageContract, "action.import_sheet"), icon: <Iconify icon="solar:import-bold" aria-hidden="true" />, onSelect: () => setOpenDrawer("bulk") },
          { label: copy(pageContract, "action.import_sheds"), icon: <Iconify icon="solar:import-bold" aria-hidden="true" />, onSelect: () => setOpenDrawer("shed-bulk") },
        ]}
      />

      <RegisterGoatDrawer
        open={openDrawer === "register"}
        onClose={() => setOpenDrawer(null)}
        parks={parks}
        sheds={sheds}
        operationalLocations={operationalLocations}
        operationalLocationsAvailable={operationalLocationsAvailable}
        farms={farms}
        animalStages={animalStages}
        locationsAvailable={locationsAvailable}
        stagesAvailable={stagesAvailable}
        idempotencyKey={idempotencyKey}
        returnTo={returnTo}
        pageContract={pageContract}
      />
      <RegisterShedDrawer
        open={openDrawer === "shed"}
        onClose={() => setOpenDrawer(null)}
        parks={parks}
        idempotencyKey={idempotencyKey}
        returnTo={returnTo}
        pageContract={pageContract}
      />
      <BulkImportDrawer open={openDrawer === "bulk"} onClose={() => setOpenDrawer(null)} pageContract={pageContract} />
      <ShedImportDrawer open={openDrawer === "shed-bulk"} onClose={() => setOpenDrawer(null)} pageContract={pageContract} />
    </>
  );
}

// ---- Reproductive status edit (record drawer affordance) ----
// Sits beside the read-only reproductive Tag in the Animal Passport record drawer. The status choices come
// only from the backend-owned `herd_reproductive` option group (compiled from active reproductive
// status_definitions) — no hardcoded vocabulary. When that group is empty the control is disabled with a
// backend-owned reason instead of falling back to a made-up list. row_version is resolved server-side.
export function HerdReproductiveEdit({
  goatId,
  displayId,
  currentStatus,
  idempotencyKey,
  returnTo,
  pageContract,
}: {
  goatId: string;
  displayId: string;
  currentStatus: string | null | undefined;
  idempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const [open, setOpen] = useState(false);
  const options = optionalOptionGroup(pageContract, "herd_reproductive");
  const canEdit = options.length > 0;
  const defaultStatus = options.some((option) => option.key === currentStatus) ? (currentStatus ?? "") : "";

  return (
    <>
      {/* The reason rides on a wrapper: a disabled button receives no pointer events of its own. */}
      <Box component="span" title={canEdit ? undefined : copy(pageContract, "reason.reproductive_unavailable")} sx={{ display: "inline-flex" }}>
        <Button
          type="button"
          size="small"
          variant="outlined"
          color="inherit"
          startIcon={<Iconify icon="solar:pen-bold" aria-hidden="true" />}
          onClick={() => setOpen(true)}
          disabled={!canEdit}
          aria-disabled={!canEdit}
        >
          {copy(pageContract, "action.edit_reproductive")}
        </Button>
      </Box>

      <HerdActionDialog
        open={open && canEdit}
        onClose={() => setOpen(false)}
        closeLabel={copy(pageContract, "action.close")}
        title={copy(pageContract, "drawer.reproductive.title")}
        subtitle={`${displayId} · ${copy(pageContract, "drawer.reproductive.subtitle")}`}
        maxWidth={560}
        // Submits the operator's chosen backend status key; the action reads current row_version + writes.
        form={{ action: reproductiveGoatAction, onSubmit: () => setOpen(false) }}
        actions={
          <>
            <CancelButton onClick={() => setOpen(false)}>{copy(pageContract, "action.cancel")}</CancelButton>
            <SubmitButton pageContract={pageContract}>{copy(pageContract, "action.save_reproductive")}</SubmitButton>
          </>
        }
      >
        <input type="hidden" name="goat_id" value={goatId} />
        <input type="hidden" name="idempotency_key" value={idempotencyKey} />
        <input type="hidden" name="return_to" value={returnTo} />
        <input type="hidden" name="evidence_type" value="source_record" />

        <FormSelect
          name="reproductive_status"
          required
          fullWidth
          label={copy(pageContract, "field.reproductive_status")}
          defaultValue={defaultStatus}
          options={[
            { value: "", label: copy(pageContract, "option.select_reproductive_status") },
            ...options.map((option) => ({ value: option.key, label: option.label })),
          ]}
        />

        <FieldGrid>
          <DateField label={copy(pageContract, "field.breeding_date")}>
            <ThemedDatePicker
              name="breeding_date"
              label={copy(pageContract, "field.breeding_date")}
              previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
              nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
              invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
            />
          </DateField>
          <DateField label={copy(pageContract, "field.last_delivery_date")}>
            <ThemedDatePicker
              name="last_delivery_date"
              label={copy(pageContract, "field.last_delivery_date")}
              previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
              nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
              invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
            />
          </DateField>
        </FieldGrid>
        <Note>{copy(pageContract, "note.reproductive_dates_optional")}</Note>

        <TextField
          id="repro_reason"
          name="reproductive_reason"
          label={copy(pageContract, "field.reproductive_reason")}
          multiline
          rows={3}
          required
          fullWidth
          placeholder={copy(pageContract, "placeholder.reproductive_reason")}
          slotProps={{ inputLabel: { shrink: true }, htmlInput: { minLength: 3, maxLength: 500 } }}
        />
      </HerdActionDialog>
    </>
  );
}
