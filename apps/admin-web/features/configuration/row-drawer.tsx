"use client";

import { useActionState, useEffect, useMemo, useRef, useState } from "react";

import { currentHistoryEntryIsLocalOverlay, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ConfigurationColumn, ConfigurationRefOption, ConfigurationRegister, ConfigurationRow } from "@/lib/api/configuration-server";
import { createRowAction, deleteRowAction, setRowStatusAction, updateRowAction, type ConfigurationActionState } from "./configuration-actions";

/**
 * The register row drawer: ONE form rendered from the register DEFINITION -- an input per column,
 * typed by the column's type (text / code / number / bool / enum / ref / notes) -- that creates a
 * row or saves the changed fields of one, plus, on edit, archive / restore and delete. It lives
 * inside the page's LocalOverlayDrawer, so opening and closing never navigate; a write lands in
 * place through `useActionState` -- the backend's outcome sentence appears beside the form, a
 * per-field refusal under the input it names, and a success closes the drawer after the page's
 * revalidation has re-read the table.
 *
 * Every visible word arrives resolved: column labels and hints from the definition, ref choices
 * from the options the backend served, the sentences from the page contract. This file composes
 * none. The draft travels to the action as ONE JSON field so the decoder never re-derives types.
 */

type Draft = Record<string, string | boolean>;

// A "use server" module may export only async functions, so the idle state lives here.
const INITIAL_ACTION_STATE: ConfigurationActionState = { status: "idle", code: "", detail: "", fields: {}, ticket: 0 };

/** Closes the overlay the same way the drawer's own X does: Back when the entry is local, else replace. */
function closeOverlay(listHref: string): void {
  if (currentHistoryEntryIsLocalOverlay()) {
    window.history.back();
    return;
  }
  replaceLocalOverlayUrl(listHref);
}

function draftFrom(register: ConfigurationRegister, row: ConfigurationRow | undefined): Draft {
  const draft: Draft = {};
  for (const column of register.columns) {
    const value = row?.fields[column.key];
    if (column.type === "bool") draft[column.key] = value === true;
    else draft[column.key] = value === null || value === undefined ? "" : String(value);
  }
  return draft;
}

/** The kind of item the draft describes: the chosen category's kind, so kind-scoped columns show. */
function draftKind(register: ConfigurationRegister, draft: Draft, options: Record<string, ConfigurationRefOption[]>, row: ConfigurationRow | undefined): string {
  const categoryColumn = register.columns.find((column) => column.key === "category_id");
  if (!categoryColumn || !categoryColumn.ref) return "";
  const chosen = String(draft.category_id ?? "");
  const option = (options[categoryColumn.ref] ?? []).find((item) => item.id === chosen);
  if (option?.kind) return option.kind;
  return row ? String(row.fields.kind ?? "") : "";
}

/** The fields to send: on create everything typed; on edit only what differs from the stored row. */
function changedFields(register: ConfigurationRegister, draft: Draft, row: ConfigurationRow | undefined, kind: string): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const column of register.columns) {
    if (column.kinds && column.kinds.length && kind && !column.kinds.includes(kind)) continue;
    const value = draft[column.key];
    if (row) {
      const stored = row.fields[column.key];
      if (column.type === "bool") {
        if ((stored === true) === (value === true)) continue;
      } else if ((stored === null || stored === undefined ? "" : String(stored)) === String(value ?? "")) continue;
      if (column.immutable) continue;
    }
    if (column.type === "bool") out[column.key] = value === true;
    else out[column.key] = String(value ?? "");
  }
  return out;
}

export function RowDrawerForm({
  pageContract,
  register,
  row,
  options,
  canEdit,
  canSetStatus,
  canDelete,
  listHref,
  editElsewhere,
  defaults,
}: {
  pageContract: AdminUiPageContract;
  register: ConfigurationRegister;
  row?: ConfigurationRow;
  options: Record<string, ConfigurationRefOption[]>;
  canEdit: boolean;
  canSetStatus: boolean;
  canDelete: boolean;
  listHref: string;
  /** A row owned by another screen (a feed item): read-only here, with the link to where it is edited. */
  editElsewhere?: { href: string; label: string };
  /** Prefilled fields on create (a new sub-list's parent). */
  defaults?: Record<string, string>;
}) {
  const isEdit = !!row;
  const [draft, setDraft] = useState<Draft>(() => ({ ...draftFrom(register, row), ...(row ? {} : defaults ?? {}) }));
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [state, formAction, pending] = useActionState(isEdit ? updateRowAction : createRowAction, INITIAL_ACTION_STATE);
  const [statusState, statusFormAction, statusPending] = useActionState(setRowStatusAction, INITIAL_ACTION_STATE);
  const [deleteState, deleteFormAction, deletePending] = useActionState(deleteRowAction, INITIAL_ACTION_STATE);
  const lastClosedTicket = useRef<number>(0);

  useEffect(() => {
    const latest = [state, statusState, deleteState].find((item) => item.status === "success" && item.ticket > 0);
    if (!latest) return;
    const ticket = state.ticket * 1000000 + statusState.ticket * 1000 + deleteState.ticket;
    if (lastClosedTicket.current === ticket) return;
    lastClosedTicket.current = ticket;
    closeOverlay(listHref);
  }, [state, statusState, deleteState, listHref]);

  const c = (key: string) => copy(pageContract, key);
  const kind = useMemo(() => draftKind(register, draft, options, row), [register, draft, options, row]);
  const update = (key: string, value: string | boolean) => setDraft((current) => ({ ...current, [key]: value }));
  const readOnly = !canEdit;
  const outcome = (item: ConfigurationActionState) => (item.status === "idle" ? "" : item.detail || copy(pageContract, `action.${item.code}`, copy(pageContract, "action.failed_message")));
  const message = outcome(state);
  const statusMessage = outcome(statusState);
  const deleteMessage = outcome(deleteState);
  const fieldErrors = state.fields ?? {};
  const fieldsJson = JSON.stringify(changedFields(register, draft, row, kind));

  // Ref selects narrow to the parent already chosen in this form (a pen to its park), and the
  // parent select clears a child that no longer belongs to it.
  const parentKeyFor = (column: ConfigurationColumn): string | null => {
    if (column.key === "pen_id" && register.columns.some((other) => other.key === "park_id")) return "park_id";
    return null;
  };
  const refOptions = (column: ConfigurationColumn): ConfigurationRefOption[] => {
    const all = options[column.ref ?? ""] ?? [];
    const parentKey = parentKeyFor(column);
    const parent = parentKey ? String(draft[parentKey] ?? "") : "";
    let list = parent ? all.filter((option) => option.parent_id === parent) : all;
    // A category cannot sit under itself.
    if (register.key === "categories" && column.key === "parent_id" && row) list = list.filter((option) => option.id !== row.id);
    return list;
  };
  const inputId = (column: ConfigurationColumn) => `cfg-${register.key}-${column.key}`;

  const visibleColumns = register.columns.filter((column) => !column.derived && (!column.kinds || !column.kinds.length || !kind || column.kinds.includes(kind)));

  return (
    <>
      <form action={formAction} aria-busy={pending} className="cfg-form" style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <input type="hidden" name="register" value={register.key} />
        {row ? <input type="hidden" name="row_id" value={row.id} /> : null}
        {row ? <input type="hidden" name="row_version" value={row.row_version} /> : null}
        <input type="hidden" name="fields_json" value={fieldsJson} />

        {row?.is_builtin ? <div className="note">{c("drawer.builtin_hint")}</div> : null}
        {editElsewhere ? (
          <div className="note">
            <a href={editElsewhere.href} className="btn sm">
              {editElsewhere.label}
            </a>
          </div>
        ) : null}
        {row && row.status === "archived" ? (
          <div>
            <Tag tone="mut">{c("status.archived")}</Tag>
          </div>
        ) : null}

        {visibleColumns.map((column) => {
          const id = inputId(column);
          const value = draft[column.key];
          const error = fieldErrors[column.key];
          const disabled = readOnly || (column.immutable && isEdit);
          let control: React.ReactNode;
          switch (column.type) {
            case "bool":
              control = (
                <label className="pen-routine-check" htmlFor={id}>
                  <input id={id} type="checkbox" checked={value === true} onChange={(e) => update(column.key, e.target.checked)} disabled={disabled} />
                  <span>{column.label}</span>
                </label>
              );
              break;
            case "enum":
              control = (
                <select id={id} value={String(value ?? "")} onChange={(e) => update(column.key, e.target.value)} disabled={disabled} required={column.required}>
                  <option value="">—</option>
                  {(column.options ?? []).map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              );
              break;
            case "ref":
              control = (
                <select
                  id={id}
                  value={String(value ?? "")}
                  onChange={(e) => {
                    update(column.key, e.target.value);
                    // Changing a park clears a pen that belonged to the old one.
                    if (column.key === "park_id" && register.columns.some((other) => other.key === "pen_id")) update("pen_id", "");
                  }}
                  disabled={disabled}
                  required={column.required}
                >
                  <option value="">—</option>
                  {refOptions(column).map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.label}
                    </option>
                  ))}
                </select>
              );
              break;
            case "notes":
              control = <textarea id={id} rows={3} value={String(value ?? "")} onChange={(e) => update(column.key, e.target.value)} readOnly={disabled} />;
              break;
            case "number":
              control = <input id={id} type="number" inputMode={column.integer ? "numeric" : "decimal"} step={column.integer ? 1 : "any"} min={column.min ?? undefined} value={String(value ?? "")} onChange={(e) => update(column.key, e.target.value)} readOnly={disabled} required={column.required} />;
              break;
            default:
              // An immutable column (a code) is DISABLED on edit, not merely read-only, so it reads
              // as fixed; the draft still carries its stored value and changedFields never sends it.
              control = <input id={id} type="text" value={String(value ?? "")} onChange={(e) => update(column.key, e.target.value)} readOnly={readOnly} disabled={column.immutable && isEdit} required={column.required} maxLength={500} />;
          }
          return (
            <div className="fld" key={column.key}>
              {column.type !== "bool" ? (
                <label htmlFor={id}>
                  {column.label}
                  {column.required ? " *" : ""}
                </label>
              ) : null}
              {control}
              {column.hint ? <div className="muted small">{column.hint}</div> : null}
              {error ? <div className="cfg-ferr">{error}</div> : null}
            </div>
          );
        })}

        {message ? (
          <div className={state.status === "error" ? "cfg-ferr" : "note"} role="status" aria-live="polite" key={`m-${state.ticket}`}>
            {message}
          </div>
        ) : null}

        {canEdit ? (
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <button type="submit" className="btn b" disabled={pending}>
              {c("action.save")}
            </button>
            <button type="button" className="btn" onClick={() => closeOverlay(listHref)} disabled={pending}>
              {c("action.cancel")}
            </button>
          </div>
        ) : null}
      </form>

      {row && (canSetStatus || canDelete) ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 18, paddingTop: 14, borderTop: "1px solid var(--line)" }}>
          {row.counts && Object.values(row.counts).some((n) => n > 0) ? (
            <div className="muted small">
              {c("drawer.usage_title")}:{" "}
              {Object.entries(row.counts)
                .filter(([, n]) => n > 0)
                .map(([noun, n]) => `${n} ${noun.replace(/_/g, " ")}`)
                .join(", ")}
            </div>
          ) : null}
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
            {canSetStatus && !row.is_builtin ? (
              <form action={statusFormAction} aria-busy={statusPending}>
                <input type="hidden" name="register" value={register.key} />
                <input type="hidden" name="row_id" value={row.id} />
                <input type="hidden" name="row_version" value={row.row_version} />
                <input type="hidden" name="status" value={row.status === "archived" ? "active" : "archived"} />
                <button type="submit" className="btn sm" disabled={statusPending} title={c("drawer.archive_hint")}>
                  {row.status === "archived" ? c("action.restore") : c("action.archive")}
                </button>
              </form>
            ) : null}
            {canDelete && !row.is_builtin ? (
              confirmDelete ? (
                <form action={deleteFormAction} aria-busy={deletePending} style={{ display: "inline-flex", gap: 8, alignItems: "center" }}>
                  <input type="hidden" name="register" value={register.key} />
                  <input type="hidden" name="row_id" value={row.id} />
                  <input type="hidden" name="row_version" value={row.row_version} />
                  <span className="small">{c("drawer.delete_confirm")}</span>
                  <button type="submit" className="btn sm" style={{ color: "var(--danger)" }} disabled={deletePending}>
                    {c("action.delete")}
                  </button>
                  <button type="button" className="btn sm ghost" onClick={() => setConfirmDelete(false)}>
                    {c("action.cancel")}
                  </button>
                </form>
              ) : (
                <button type="button" className="btn sm ghost" style={{ color: "var(--danger)" }} onClick={() => setConfirmDelete(true)}>
                  {c("action.delete")}
                </button>
              )
            ) : null}
          </div>
          {statusMessage ? (
            <div className={statusState.status === "error" ? "cfg-ferr" : "note"} role="status" aria-live="polite" key={`s-${statusState.ticket}`}>
              {statusMessage}
            </div>
          ) : null}
          {deleteMessage ? (
            <div className={deleteState.status === "error" ? "cfg-ferr" : "note"} role="status" aria-live="polite" key={`d-${deleteState.ticket}`}>
              {deleteMessage}
            </div>
          ) : null}
        </div>
      ) : null}
    </>
  );
}
