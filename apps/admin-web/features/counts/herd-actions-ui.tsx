"use client";

// Counts -> Herd Register write UI: the "Register goat" drawer (single create) and the "Import sheet" drawer
// (bulk preview -> commit). Mock-faithful drawer behavior (right panel, backdrop/Escape close, focus, footer
// actions) over the Mesha theme. Single create posts a real server action (createGoatAction) and redirects
// with a banner; bulk calls real preview/commit server actions and holds ONLY the backend's response as
// transient UI state. No fixtures, no fake totals, no client-only business mutation.
import { useEffect, useRef, useState, useTransition } from "react";
import { useFormStatus } from "react-dom";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, Download, Plus, Upload, X } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import type { LocationOption } from "@/lib/api/herd-locations";
import type { AdminGoatBulkResponse, CreateAdminGoatRequest } from "@/lib/api/server";
import { copy, optionGroup, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  commitGoatsAction,
  commitShedsAction,
  createGoatAction,
  createShedAction,
  previewGoatsAction,
  previewShedsAction,
  type ShedImportCommitRow,
  type ShedImportResponse,
} from "./herd-actions";
import { csvCell, stableCSVContentHash } from "./herd-import-utils";

export type HerdAnimalStageOption = {
  code: string;
  label: string;
};

function todayISO(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function decisionLabel(pageContract: AdminUiPageContract, decision: string): string {
  return optionLabel(pageContract, "herd_bulk_decisions", String(decision));
}

function csvFilename(label: string): string {
  return label.toLowerCase().endsWith(".csv") ? label : `${label}.csv`;
}

function parseCSVRecords(raw: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let inQuotes = false;
  for (let i = 0; i < raw.length; i += 1) {
    const ch = raw[i];
    if (ch === "\"") {
      if (inQuotes && raw[i + 1] === "\"") {
        cell += "\"";
        i += 1;
      } else {
        inQuotes = !inQuotes;
      }
      continue;
    }
    if (ch === "," && !inQuotes) {
      row.push(cell.trim());
      cell = "";
      continue;
    }
    if ((ch === "\n" || ch === "\r") && !inQuotes) {
      if (ch === "\r" && raw[i + 1] === "\n") i += 1;
      row.push(cell.trim());
      rows.push(row);
      row = [];
      cell = "";
      continue;
    }
    cell += ch;
  }
  if (cell.length > 0 || row.length > 0) {
    row.push(cell.trim());
    rows.push(row);
  }
  return rows;
}

type ImportResultRow = {
  row_number: number;
  decision: string;
  errors: { field: string; message: string }[];
  warnings?: { message: string }[];
};

function importRowFailed(row: ImportResultRow): boolean {
  return row.errors.length > 0 || row.decision === "requires_review" || row.decision === "failed";
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

function downloadFailedRows(filename: string, csv: string, templateColumns: string[], rows: ImportResultRow[], failureHeader: string) {
  const failed = failedImportRows(rows);
  if (failed.length === 0) return;
  const parsed = parseCSVRecords(csv);
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

// ---- Drawer shell (right panel; backdrop + Escape close; focus trap entry; body scroll lock) ----
function Drawer({
  open,
  onClose,
  closeLabel,
  title,
  subtitle,
  width = 560,
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
    <div
      role="presentation"
      onClick={onClose}
      style={{ position: "fixed", inset: 0, zIndex: 220, background: "rgba(0,0,0,.55)", display: "flex", justifyContent: "flex-end" }}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
        style={{
          width: `min(${width}px, 100%)`,
          height: "100%",
          background: "var(--panel)",
          borderLeft: "1px solid var(--line)",
          boxShadow: "var(--shadow)",
          display: "flex",
          flexDirection: "column",
          outline: "none",
        }}
      >
        <div className="hd" style={{ flexShrink: 0 }}>
          <div>
            <h3>{title}</h3>
            {subtitle ? <div className="muted small" style={{ marginTop: 2 }}>{subtitle}</div> : null}
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn sm" onClick={onClose} aria-label={closeLabel}>
            <X className="ic" style={{ width: 14 }} aria-hidden="true" />
          </button>
        </div>
        <div style={{ flex: 1, overflowY: "auto", padding: "14px 16px" }}>{children}</div>
      </div>
    </div>
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

// ---- Register goat drawer (single create) ----
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

      {/* The action redirects (banner). Close the drawer as the form submits so the banner is visible and
          the client drawer state does not linger over the navigated page. onSubmit fires only after the
          browser's required-field validation passes, and React still dispatches the action this event. */}
      <form action={createGoatAction} onSubmit={() => onClose()} className="fld" style={{ margin: 0 }}>
        <input type="hidden" name="idempotency_key" value={idempotencyKey} />
        <input type="hidden" name="return_to" value={returnTo} />
        <input type="hidden" name="evidence_type" value="source_record" />

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_rfid">{copy(pageContract, "field.rfid")}</label>
            <input id="rg_rfid" name="rfid" placeholder={copy(pageContract, "placeholder.rfid")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_oldtag">{copy(pageContract, "field.old_tag")}</label>
            <input id="rg_oldtag" name="old_tag" placeholder={copy(pageContract, "placeholder.old_tag")} />
          </div>
        </Row>
        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label htmlFor="rg_temp">{copy(pageContract, "field.temp_field_id")}</label>
            <input id="rg_temp" name="temp_field_id" placeholder={copy(pageContract, "placeholder.temp_field_id")} />
          </div>
        </Row>
        <div className="note" style={{ marginBottom: 12 }}>{copy(pageContract, "note.identifier_required")}</div>

        <Row>
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
            <select id="rg_sex" name="sex" defaultValue="female">
              {sexOptions.map((o) => (
                <option key={o.key} value={o.key}>{o.label}</option>
              ))}
            </select>
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 160 }}>
            <label htmlFor="rg_origin">{copy(pageContract, "field.origin")}</label>
            <select id="rg_origin" name="origin_type" defaultValue="birth">
              {originOptions.map((o) => (
                <option key={o.key} value={o.key}>{o.label}</option>
              ))}
            </select>
          </div>
        </Row>

        <Row>
          <div className="fld" style={{ flex: 1, minWidth: 150 }}>
            <label htmlFor="rg_dob">{copy(pageContract, "field.dob")}</label>
            <input id="rg_dob" name="dob" type="date" />
          </div>
          <div className="fld" style={{ width: 140 }}>
            <label htmlFor="rg_weight">{copy(pageContract, "field.weight_kg")}</label>
            <input id="rg_weight" name="weight_kg" type="number" min={0} step="0.1" placeholder={copy(pageContract, "placeholder.weight_kg")} />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 150 }}>
            <label htmlFor="rg_entry">{copy(pageContract, "field.entry_date_required")}</label>
            <input id="rg_entry" name="entry_date" type="date" defaultValue={todayISO()} required />
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

// ---- Register shed drawer (single create) ----
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

// ---- Bulk import drawer (download template -> paste/upload CSV -> preview -> commit) ----
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
    updateCSV(await file.text());
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
      const res = await commitGoatsAction(rows, hash);
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
      width={820}
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
            <input id="bulk_file" type="file" accept=".csv,text/csv" onChange={onFile} />
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
                    const ident = r.normalized?.rfid || r.normalized?.old_tag || r.normalized?.temp_field_id || copy(pageContract, "label.placeholder");
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
              <button type="button" className="btn" onClick={() => downloadFailedRows(copy(pageContract, "action.download_failed_goat_rows"), csv, bulkColumns, view.rows, copy(pageContract, "field.failure_reason"))}>
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

// ---- Shed bulk import drawer (download template -> paste/upload CSV -> preview -> commit) ----
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
    updateCSV(await file.text());
  }

  function runPreview() {
    setError(null);
    setCommitted(null);
    startTransition(async () => {
      const hash = await stableCSVContentHash(csv);
      const res = await previewShedsAction(csv);
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
      width={820}
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
            <input id="shed_bulk_file" type="file" accept=".csv,text/csv" onChange={onFile} />
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
              <button type="button" className="btn" onClick={() => downloadFailedRows(copy(pageContract, "action.download_failed_shed_rows"), csv, shedColumns, view.rows, copy(pageContract, "field.failure_reason"))}>
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
