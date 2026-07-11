"use client";

// Counts -> Herd Register write UI: the "Register goat" modal (single create) and the "Import sheet" modal
// (bulk preview -> commit). Mock-faithful modal behavior (centered overlay, backdrop/Escape close, focus, footer
// actions) over the Mesha theme. Single create posts a real server action (createGoatAction) and redirects
// with a banner; bulk calls real preview/commit server actions and holds ONLY the backend's response as
// transient UI state. No fixtures, no fake totals, no client-only business mutation.
import { useEffect, useRef, useState, useTransition } from "react";
import { useFormStatus } from "react-dom";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, Download, Plus, SquarePen, Upload, X } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
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

export type HerdAnimalStageOption = {
  code: string;
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

// ---- Modal shell (centered overlay; backdrop + Escape close; focus trap entry; body scroll lock) ----
function Drawer({
  open,
  onClose,
  closeLabel,
  title,
  subtitle,
  width = 760,
  children,
}: {
  open: boolean;
  onClose: () => void;
  closeLabel: string;
  title: string;
  subtitle?: string;
  width?: number;
  children: React.ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const node = panelRef.current;
    const first = node?.querySelector<HTMLElement>(
      'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])',
    );
    (first ?? node)?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <>
      <div onClick={onClose} aria-hidden="true" style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,.6)", zIndex: 210 }} />
      <div
        ref={panelRef}
        className="modal on card"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
        style={{
          width: `min(${width}px, calc(100vw - 32px))`,
          maxHeight: "calc(100vh - 48px)",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          outline: "none",
        }}
      >
        <div className="hd" style={{ borderBottom: "1px solid var(--line2)", flex: "0 0 auto" }}>
          <Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <div>
            <h3>{title}</h3>
            {subtitle ? <div className="muted small" style={{ marginTop: 2 }}>{subtitle}</div> : null}
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="iconbtn" onClick={onClose} aria-label={closeLabel}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>
        <div className="bd" style={{ flex: "1 1 auto", minHeight: 0, overflowY: "auto", padding: "18px 22px" }}>{children}</div>
      </div>
    </>
  );
}

function SubmitButton({ pageContract, children }: { pageContract: AdminUiPageContract; children: React.ReactNode }) {
  const { pending } = useFormStatus();
  return (
    <button type="submit" className="btn p" disabled={pending} aria-busy={pending}>
      {pending ? copy(pageContract, "action.working") : children}
    </button>
  );
}

function Row({ children }: { children: React.ReactNode }) {
  return <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>{children}</div>;
}

// ---- Register goat modal (single create) ----
function RegisterGoatDrawer({
  open,
  onClose,
  parks,
  sheds,
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
  farms: LocationOption[];
  animalStages: HerdAnimalStageOption[];
  locationsAvailable: boolean;
  stagesAvailable: boolean;
  idempotencyKey: string;
  returnTo: string;
  pageContract: AdminUiPageContract;
}) {
  const [parkId, setParkId] = useState<string>(parks[0]?.id ?? "");
  const scopedSheds = sheds.filter((s) => s.parentId === parkId);
  const shedOptions = scopedSheds.length > 0 ? scopedSheds : sheds;
  const sexOptions = optionGroup(pageContract, "herd_sex");
  const speciesOptions = optionGroup(pageContract, "herd_species");
  const originOptions = optionGroup(pageContract, "herd_origin");

  const hasLocations = locationsAvailable && parks.length > 0 && shedOptions.length > 0;
  const hasStages = stagesAvailable && animalStages.length > 0;
  const canCreate = hasLocations && hasStages;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.register.title")}
      subtitle={copy(pageContract, "drawer.register.subtitle")}
      width={820}
    >
      {!hasLocations ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{copy(pageContract, "alert.locations.title")}</b>
            <div className="small">{copy(pageContract, "alert.locations.body")}</div>
          </div>
        </div>
      ) : null}
      {!hasStages ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{copy(pageContract, "alert.stages.title")}</b>
            <div className="small">{copy(pageContract, "alert.stages.body")}</div>
          </div>
        </div>
      ) : null}

      {/* The action redirects (banner). Close the modal as the form submits so the banner is visible and
          the client modal state does not linger over the navigated page. onSubmit fires only after the
          browser's required-field validation passes, and React still dispatches the action this event. */}
      <form action={createGoatAction} onSubmit={() => onClose()} className="fld" style={{ margin: 0 }}>
        <input type="hidden" name="idempotency_key" value={idempotencyKey} />
        <input type="hidden" name="return_to" value={returnTo} />
        <input type="hidden" name="evidence_type" value="source_record" />

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_animal_id_1">{copy(pageContract, "field.animal_identifier_1")}</label>
            <input id="rg_animal_id_1" name="animal_identifier_1" required placeholder={copy(pageContract, "placeholder.animal_identifier_1")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_animal_id_2">{copy(pageContract, "field.animal_identifier_2")}</label>
            <input id="rg_animal_id_2" name="animal_identifier_2" placeholder={copy(pageContract, "placeholder.animal_identifier_2")} />
          </div>
        </Row>
        <div className="note" style={{ marginBottom: 12 }}>{copy(pageContract, "note.identifier_required")}</div>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_species">{copy(pageContract, "field.species")}</label>
            <select id="rg_species" name="species" required defaultValue="">
              <option value="" disabled>{copy(pageContract, "option.select_species")}</option>
              {speciesOptions.map((o) => (
                <option key={o.key} value={o.key}>{o.label}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_park">{copy(pageContract, "field.park_required")}</label>
            <select id="rg_park" name="park_id" value={parkId} onChange={(e) => setParkId(e.target.value)} required disabled={!canCreate}>
              {parks.length === 0 ? <option value="">{copy(pageContract, "option.no_parks")}</option> : null}
              {parks.map((p) => (
                <option key={p.id} value={p.id}>{p.name}{p.code ? ` · ${p.code}` : ""}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_shed">{copy(pageContract, "field.shed_required")}</label>
            <select id="rg_shed" name="shed_id" required disabled={!canCreate} defaultValue={shedOptions[0]?.id ?? ""}>
              {shedOptions.length === 0 ? <option value="">{copy(pageContract, "option.no_vaccination_sheds")}</option> : null}
              {shedOptions.map((s) => (
                <option key={s.id} value={s.id}>{s.name}{s.code ? ` · ${s.code}` : ""}</option>
              ))}
            </select>
          </div>
        </Row>
        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_farm">{copy(pageContract, "field.farm")}</label>
            <select id="rg_farm" name="farm_id" defaultValue="">
              <option value="">{copy(pageContract, "option.optional")}</option>
              {farms.map((f) => (
                <option key={f.id} value={f.id}>{f.name}{f.code ? ` · ${f.code}` : ""}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_breed">{copy(pageContract, "field.breed")}</label>
            <input id="rg_breed" name="breed" placeholder={copy(pageContract, "placeholder.breed")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_stage">{copy(pageContract, "field.management_stage")}</label>
            <select id="rg_stage" name="management_stage" required disabled={!hasStages} defaultValue={animalStages[0]?.code ?? ""}>
              {animalStages.length === 0 ? <option value="">{copy(pageContract, "alert.stages.title")}</option> : null}
              {animalStages.map((stage) => (
                <option key={stage.code} value={stage.code}>{stage.label}</option>
              ))}
            </select>
          </div>
        </Row>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 140 }}>
            <label htmlFor="rg_sex">{copy(pageContract, "field.sex")}</label>
            <select id="rg_sex" name="sex" required defaultValue="">
              <option value="" disabled>{copy(pageContract, "option.select_sex")}</option>
              {sexOptions.map((o) => (
                <option key={o.key} value={o.key}>{o.label}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_origin">{copy(pageContract, "field.origin")}</label>
            <select id="rg_origin" name="origin_type" required defaultValue="">
              <option value="" disabled>{copy(pageContract, "option.select_origin")}</option>
              {originOptions.map((o) => (
                <option key={o.key} value={o.key}>{o.label}</option>
              ))}
            </select>
          </div>
        </Row>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 150 }}>
            <label htmlFor="rg_dob">{copy(pageContract, "field.dob")}</label>
            <input id="rg_dob" name="dob" type="date" required />
          </div>
          <div className="fld" style={{ width: 140 }}>
            <label htmlFor="rg_weight">{copy(pageContract, "field.weight_kg")}</label>
            <input id="rg_weight" name="weight_kg" type="number" min={0} step="0.1" placeholder={copy(pageContract, "placeholder.weight_kg")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 150 }}>
            <label htmlFor="rg_entry">{copy(pageContract, "field.entry_date_required")}</label>
            <input id="rg_entry" name="entry_date" type="date" defaultValue={todayIso()} required />
          </div>
        </Row>
        <label style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 10, fontSize: 12.5 }}>
          <input name="dob_estimated" type="checkbox" style={{ width: "auto" }} /> {copy(pageContract, "field.dob_estimated")}
        </label>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_dam">{copy(pageContract, "field.dam_id")}</label>
            <input id="rg_dam" name="dam_id" placeholder={copy(pageContract, "placeholder.dam_id")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_sire">{copy(pageContract, "field.sire_or_lot")}</label>
            <input id="rg_sire" name="sire_or_lot" placeholder={copy(pageContract, "placeholder.sire_or_lot")} />
          </div>
        </Row>

        <div className="fld">
          <label htmlFor="rg_evidence">{copy(pageContract, "field.evidence_ref")}</label>
          <input id="rg_evidence" name="evidence_id" placeholder={copy(pageContract, "placeholder.evidence_ref")} />
        </div>
        <div className="note" style={{ marginBottom: 12 }}>
          {copy(pageContract, "note.media_capture")}
        </div>

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", paddingTop: 4 }}>
          <button type="button" className="btn" onClick={onClose}>{copy(pageContract, "action.cancel")}</button>
          {canCreate ? (
            <SubmitButton pageContract={pageContract}>{copy(pageContract, "action.register_goat")}</SubmitButton>
          ) : (
            <button type="button" className="btn p" disabled aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>{copy(pageContract, "action.register_goat")}</button>
          )}
        </div>
      </form>
    </Drawer>
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
    <Drawer
      open={open}
      onClose={onClose}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.shed_register.title")}
      subtitle={copy(pageContract, "drawer.shed_register.subtitle")}
      width={760}
    >
      {!canCreate ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{copy(pageContract, "alert.locations.title")}</b>
            <div className="small">{copy(pageContract, "alert.shed_locations.body")}</div>
          </div>
        </div>
      ) : null}

      <form action={createShedAction} onSubmit={() => onClose()} className="fld" style={{ margin: 0 }}>
        <input type="hidden" name="idempotency_key" value={idempotencyKey} />
        <input type="hidden" name="return_to" value={returnTo} />

        <div className="fld">
          <label htmlFor="rs_park">{copy(pageContract, "field.park_required")}</label>
          <select id="rs_park" name="park_id" required disabled={!canCreate}>
            {parks.length === 0 ? <option value="">{copy(pageContract, "option.no_parks")}</option> : null}
            {parks.map((p) => (
              <option key={p.id} value={p.id}>{p.name}{p.code ? ` · ${p.code}` : ""}</option>
            ))}
          </select>
        </div>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 180 }}>
            <label htmlFor="rs_code">{copy(pageContract, "field.shed_code")}</label>
            <input id="rs_code" name="location_code" placeholder={copy(pageContract, "placeholder.shed_code")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 220 }}>
            <label htmlFor="rs_name">{copy(pageContract, "field.shed_name_required")}</label>
            <input id="rs_name" name="name" required placeholder={copy(pageContract, "placeholder.shed_name")} />
          </div>
        </Row>

        <Row>
          <div className="fld" style={{ width: 150 }}>
            <label htmlFor="rs_order">{copy(pageContract, "field.display_order")}</label>
            <input id="rs_order" name="display_order" type="number" min={0} step={1} defaultValue={0} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 220 }}>
            <label htmlFor="rs_notes">{copy(pageContract, "field.notes")}</label>
            <input id="rs_notes" name="notes" placeholder={copy(pageContract, "placeholder.shed_notes")} />
          </div>
        </Row>

        <div className="note" style={{ marginBottom: 12 }}>{copy(pageContract, "note.shed_create")}</div>

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", paddingTop: 4 }}>
          <button type="button" className="btn" onClick={onClose}>{copy(pageContract, "action.cancel")}</button>
          {canCreate ? (
            <SubmitButton pageContract={pageContract}>{copy(pageContract, "action.register_shed")}</SubmitButton>
          ) : (
            <button type="button" className="btn p" disabled aria-disabled="true" style={{ opacity: 0.5, cursor: "not-allowed" }}>{copy(pageContract, "action.register_shed")}</button>
          )}
        </div>
      </form>
    </Drawer>
  );
}

// ---- Bulk import modal (download template -> paste/upload CSV -> preview -> commit) ----
function BulkImportDrawer({ open, onClose, pageContract }: { open: boolean; onClose: () => void; pageContract: AdminUiPageContract }) {
  const router = useRouter();
  const [csv, setCsv] = useState("");
  const [preview, setPreview] = useState<AdminGoatBulkResponse | null>(null);
  const [previewHash, setPreviewHash] = useState<string | null>(null);
  const [committed, setCommitted] = useState<AdminGoatBulkResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [pending, startTransition] = useTransition();
  const bulkColumns = optionGroup(pageContract, "herd_import_columns").map((column) => column.label);

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

  async function onFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
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
      event.target.value = "";
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
    <Drawer
      open={open}
      onClose={close}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.import.title")}
      subtitle={copy(pageContract, "drawer.import.subtitle")}
      width={900}
    >
      {/* Step 1 — template + input. Hidden once a commit result is shown. */}
      {!committed ? (
        <>
          <div className="fld">
            <label>{copy(pageContract, "field.bulk_template")} <span className="muted small">({bulkColumns.length} {copy(pageContract, "label.columns")})</span></label>
            <button type="button" className="btn sm" onClick={downloadTemplate}>
              <Download className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.download_template")}
            </button>
            <div className="muted small" style={{ marginTop: 6 }}>{bulkColumns.join(" · ")}</div>
            <div className="note" style={{ marginTop: 8 }}>
              {copy(pageContract, "note.bulk_template")}
            </div>
          </div>
          <div className="fld">
            <label htmlFor="bulk_file">{copy(pageContract, "field.bulk_upload")}</label>
            <input id="bulk_file" type="file" accept={sheetImportAccept} onChange={onFile} />
          </div>
          <div className="fld">
            <label htmlFor="bulk_csv">{copy(pageContract, "field.bulk_paste")}</label>
            <textarea
              id="bulk_csv"
              rows={5}
              value={csv}
              onChange={(e) => updateCSV(e.target.value)}
              placeholder={bulkColumns.join(",")}
              style={{ fontFamily: "var(--mono, monospace)", fontSize: 12 }}
            />
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 14 }}>
            <button type="button" className="btn p" onClick={runPreview} disabled={pending || csv.trim() === ""} aria-busy={pending}>
              {pending && !committed ? copy(pageContract, "action.previewing_rows") : copy(pageContract, "action.preview_rows")}
            </button>
            {preview ? <span className="muted small">{copy(pageContract, "note.preview_ready")}</span> : null}
          </div>
        </>
      ) : null}

      {error ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div><b>{committed ? copy(pageContract, "action.commit_failed") : copy(pageContract, "action.preview_failed")}</b><div className="small">{error}</div></div>
        </div>
      ) : null}

      {committed ? (
        <div className="note" style={{ marginBottom: 14 }}>
          <Tag tone="ok">{copy(pageContract, "label.committed")}</Tag> {copy(pageContract, "label.created")} {committed.summary.created} · {copy(pageContract, "label.failed")} {committed.summary.failed} · {copy(pageContract, "label.skip")} {committed.summary.skipped} {copy(pageContract, "label.of")} {committed.summary.total} {copy(pageContract, "label.rows")}. {copy(pageContract, "note.committed_suffix")}
        </div>
      ) : null}

      {/* Step 3 — preview/commit results table. */}
      {view ? (
        <>
          <div className="chipset" style={{ marginBottom: 10 }}>
            <Tag tone="mut">{view.summary.total} {copy(pageContract, "label.rows")}</Tag>
            <Tag tone="ok">{view.summary.create_ready} {copy(pageContract, "label.create_ready")}</Tag>
            <Tag tone="warn">{view.summary.requires_review} {copy(pageContract, "label.review")}</Tag>
            <Tag tone="mut">{view.summary.skipped} {copy(pageContract, "label.skip")}</Tag>
            {committed ? <Tag tone={view.summary.failed ? "dng" : "ok"}>{view.summary.created} {copy(pageContract, "label.created")}</Tag> : null}
          </div>
          <div className="card" style={{ marginBottom: 14 }}>
            <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
              <table>
                <thead>
                  <tr>
                    <th>{copy(pageContract, "field.import_row")}</th>
                    <th>{copy(pageContract, "field.import_decision")}</th>
                    <th>{copy(pageContract, "field.import_identity")}</th>
                    <th>{copy(pageContract, "field.import_notes")}</th>
                    {committed ? <th>{copy(pageContract, "field.import_result")}</th> : null}
                  </tr>
                </thead>
                <tbody>
                  {view.rows.map((r) => {
                    const ident = r.normalized?.animal_identifier_1 || r.normalized?.animal_identifier_2 || copy(pageContract, "label.placeholder");
                    const notes = [
                      ...r.errors.map((e) => `${e.field}: ${e.message}`),
                      ...r.warnings.map((w) => w.message),
                    ].join(" · ");
                    return (
                      <tr key={r.row_number}>
                        <td className="muted">{r.row_number}</td>
                        <td><Tag tone={contractTone(pageContract, "herd_bulk_decisions", String(r.decision))}>{decisionLabel(pageContract, r.decision)}</Tag></td>
                        <td>{ident}</td>
                        <td className="muted small">{notes || copy(pageContract, "label.placeholder")}</td>
                        {committed ? (
                          <td className="muted small">
                            {r.result ? r.result.goat.display_id : r.errors.length ? copy(pageContract, "label.failed") : copy(pageContract, "label.placeholder")}
                          </td>
                        ) : null}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
            {failedCount > 0 ? (
              <button type="button" className="btn" onClick={() => downloadFailedRows(copy(pageContract, "action.download_failed_goat_rows"), csv, bulkColumns, view.rows, copy(pageContract, "field.failure_reason"), copy(pageContract, "error.csv_unterminated_quote"))}>
                <Download className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.export_failed_rows")} ({failedCount})
              </button>
            ) : null}
            {committed ? (
              <button type="button" className="btn p" onClick={close}>{copy(pageContract, "action.done")}</button>
            ) : (
              <>
                <button type="button" className="btn" onClick={() => { setPreview(null); setError(null); }}>{copy(pageContract, "action.re_edit")}</button>
                <button
                  type="button"
                  className="btn p"
                  onClick={runCommit}
                  disabled={pending || committable === 0}
                  aria-disabled={committable === 0}
                  aria-busy={pending}
                >
                  <Check className="ic" aria-hidden="true" /> {pending ? copy(pageContract, "action.creating_records") : `${copy(pageContract, "action.create_records")} (${committable})`}
                </button>
              </>
            )}
          </div>
        </>
      ) : null}
    </Drawer>
  );
}

// ---- Shed bulk import modal (download template -> paste/upload CSV -> preview -> commit) ----
function ShedImportDrawer({ open, onClose, pageContract }: { open: boolean; onClose: () => void; pageContract: AdminUiPageContract }) {
  const router = useRouter();
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

  async function onFile(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
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
      event.target.value = "";
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
    <Drawer
      open={open}
      onClose={close}
      closeLabel={copy(pageContract, "action.close")}
      title={copy(pageContract, "drawer.shed_import.title")}
      subtitle={copy(pageContract, "drawer.shed_import.subtitle")}
      width={900}
    >
      {!committed ? (
        <>
          <div className="fld">
            <label>{copy(pageContract, "field.bulk_template")} <span className="muted small">({shedColumns.length} {copy(pageContract, "label.columns")})</span></label>
            <button type="button" className="btn sm" onClick={downloadTemplate}>
              <Download className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.download_shed_template")}
            </button>
            <div className="muted small" style={{ marginTop: 6 }}>{shedColumns.join(" · ")}</div>
            <div className="note" style={{ marginTop: 8 }}>
              {copy(pageContract, "note.shed_bulk_template")}
            </div>
          </div>
          <div className="fld">
            <label htmlFor="shed_bulk_file">{copy(pageContract, "field.bulk_upload")}</label>
            <input id="shed_bulk_file" type="file" accept={sheetImportAccept} onChange={onFile} />
          </div>
          <div className="fld">
            <label htmlFor="shed_bulk_csv">{copy(pageContract, "field.bulk_paste")}</label>
            <textarea
              id="shed_bulk_csv"
              rows={5}
              value={csv}
              onChange={(e) => updateCSV(e.target.value)}
              placeholder={shedColumns.join(",")}
              style={{ fontFamily: "var(--mono, monospace)", fontSize: 12 }}
            />
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 14 }}>
            <button type="button" className="btn p" onClick={runPreview} disabled={pending || csv.trim() === ""} aria-busy={pending}>
              {pending && !committed ? copy(pageContract, "action.previewing_rows") : copy(pageContract, "action.preview_rows")}
            </button>
            {preview ? <span className="muted small">{copy(pageContract, "note.preview_ready")}</span> : null}
          </div>
        </>
      ) : null}

      {error ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div><b>{committed ? copy(pageContract, "action.commit_failed") : copy(pageContract, "action.preview_failed")}</b><div className="small">{error}</div></div>
        </div>
      ) : null}

      {committed ? (
        <div className="note" style={{ marginBottom: 14 }}>
          <Tag tone="ok">{copy(pageContract, "label.committed")}</Tag> {copy(pageContract, "label.created")} {committed.summary.created} · {copy(pageContract, "label.failed")} {committed.summary.failed} {copy(pageContract, "label.of")} {committed.summary.total} {copy(pageContract, "label.rows")}. {copy(pageContract, "note.shed_committed_suffix")}
        </div>
      ) : null}

      {view ? (
        <>
          <div className="chipset" style={{ marginBottom: 10 }}>
            <Tag tone="mut">{view.summary.total} {copy(pageContract, "label.rows")}</Tag>
            <Tag tone="ok">{view.summary.create_ready} {copy(pageContract, "label.create_ready")}</Tag>
            <Tag tone="warn">{view.summary.requires_review} {copy(pageContract, "label.review")}</Tag>
            {committed ? <Tag tone={view.summary.failed ? "dng" : "ok"}>{view.summary.created} {copy(pageContract, "label.created")}</Tag> : null}
          </div>
          <div className="card" style={{ marginBottom: 14 }}>
            <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
              <table>
                <thead>
                  <tr>
                    <th>{copy(pageContract, "field.import_row")}</th>
                    <th>{copy(pageContract, "field.import_decision")}</th>
                    <th>{copy(pageContract, "field.shed")}</th>
                    <th>{copy(pageContract, "field.import_notes")}</th>
                    {committed ? <th>{copy(pageContract, "field.import_result")}</th> : null}
                  </tr>
                </thead>
                <tbody>
                  {view.rows.map((r) => {
                    const ident = r.normalized
                      ? `${r.normalized.location_code ? `${r.normalized.location_code} · ` : ""}${r.normalized.name}`
                      : r.source_label || copy(pageContract, "label.placeholder");
                    const notes = r.errors.map((e) => `${e.field}: ${e.message}`).join(" · ");
                    return (
                      <tr key={r.row_number}>
                        <td className="muted">{r.row_number}</td>
                        <td><Tag tone={contractTone(pageContract, "herd_bulk_decisions", r.decision)}>{decisionLabel(pageContract, r.decision)}</Tag></td>
                        <td>{ident}</td>
                        <td className="muted small">{notes || copy(pageContract, "label.placeholder")}</td>
                        {committed ? (
                          <td className="muted small">{r.result ? r.result.location.name : r.errors.length ? copy(pageContract, "label.failed") : copy(pageContract, "label.placeholder")}</td>
                        ) : null}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
            {failedCount > 0 ? (
              <button type="button" className="btn" onClick={() => downloadFailedRows(copy(pageContract, "action.download_failed_shed_rows"), csv, shedColumns, view.rows, copy(pageContract, "field.failure_reason"), copy(pageContract, "error.csv_unterminated_quote"))}>
                <Download className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.export_failed_rows")} ({failedCount})
              </button>
            ) : null}
            {committed ? (
              <button type="button" className="btn p" onClick={close}>{copy(pageContract, "action.done")}</button>
            ) : (
              <>
                <button type="button" className="btn" onClick={() => { setPreview(null); setError(null); }}>{copy(pageContract, "action.re_edit")}</button>
                <button
                  type="button"
                  className="btn p"
                  onClick={runCommit}
                  disabled={pending || committable === 0}
                  aria-disabled={committable === 0}
                  aria-busy={pending}
                >
                  <Check className="ic" aria-hidden="true" /> {pending ? copy(pageContract, "action.creating_records") : `${copy(pageContract, "action.create_sheds")} (${committable})`}
                </button>
              </>
            )}
          </div>
        </>
      ) : null}
    </Drawer>
  );
}

// ---- Header CTA group rendered inside the page header (replaces the disabled buttons) ----
export function HerdActions({
  parks,
  sheds,
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
      <button type="button" className="btn" onClick={() => setOpenDrawer("shed-bulk")}>
        <Upload className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.import_sheds")}
      </button>
      <button type="button" className="btn" onClick={() => setOpenDrawer("shed")}>
        <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.register_shed")}
      </button>
      <button type="button" className="btn" onClick={() => setOpenDrawer("bulk")}>
        <Upload className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.import_sheet")}
      </button>
      <button type="button" className="btn p" onClick={() => setOpenDrawer("register")}>
        <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.register_goat")}
      </button>

      <RegisterGoatDrawer
        open={openDrawer === "register"}
        onClose={() => setOpenDrawer(null)}
        parks={parks}
        sheds={sheds}
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
      <button
        type="button"
        className="btn sm"
        onClick={() => setOpen(true)}
        disabled={!canEdit}
        aria-disabled={!canEdit}
        title={canEdit ? undefined : copy(pageContract, "reason.reproductive_unavailable")}
        style={canEdit ? undefined : { opacity: 0.5, cursor: "not-allowed" }}
      >
        <SquarePen className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.edit_reproductive")}
      </button>

      <Drawer
        open={open && canEdit}
        onClose={() => setOpen(false)}
        closeLabel={copy(pageContract, "action.close")}
        title={copy(pageContract, "drawer.reproductive.title")}
        subtitle={`${displayId} · ${copy(pageContract, "drawer.reproductive.subtitle")}`}
        width={560}
      >
        {/* Submits the operator's chosen backend status key; the action reads current row_version + writes. */}
        <form action={reproductiveGoatAction} onSubmit={() => setOpen(false)} className="fld" style={{ margin: 0 }}>
          <input type="hidden" name="goat_id" value={goatId} />
          <input type="hidden" name="idempotency_key" value={idempotencyKey} />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="evidence_type" value="source_record" />

          <div className="fld">
            <label htmlFor="repro_status">{copy(pageContract, "field.reproductive_status")}</label>
            <select id="repro_status" name="reproductive_status" required defaultValue={defaultStatus}>
              <option value="" disabled>{copy(pageContract, "option.select_reproductive_status")}</option>
              {options.map((option) => (
                <option key={option.key} value={option.key}>{option.label}</option>
              ))}
            </select>
          </div>

          <Row>
            <div className="fld" style={{ flex: 1, minWidth: 160 }}>
              <label htmlFor="repro_breeding_date">{copy(pageContract, "field.breeding_date")}</label>
              <input id="repro_breeding_date" name="breeding_date" type="date" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 160 }}>
              <label htmlFor="repro_last_delivery_date">{copy(pageContract, "field.last_delivery_date")}</label>
              <input id="repro_last_delivery_date" name="last_delivery_date" type="date" />
            </div>
          </Row>
          <div className="note" style={{ marginBottom: 12 }}>{copy(pageContract, "note.reproductive_dates_optional")}</div>

          <div className="fld">
            <label htmlFor="repro_reason">{copy(pageContract, "field.reproductive_reason")}</label>
            <textarea
              id="repro_reason"
              name="reproductive_reason"
              rows={3}
              required
              minLength={3}
              maxLength={500}
              placeholder={copy(pageContract, "placeholder.reproductive_reason")}
            />
          </div>

          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", paddingTop: 4 }}>
            <button type="button" className="btn" onClick={() => setOpen(false)}>{copy(pageContract, "action.cancel")}</button>
            <SubmitButton pageContract={pageContract}>{copy(pageContract, "action.save_reproductive")}</SubmitButton>
          </div>
        </form>
      </Drawer>
    </>
  );
}
