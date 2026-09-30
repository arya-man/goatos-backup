"use client";

import { useActionState, useEffect, useMemo, useRef, useState } from "react";
import Button from "@mui/material/Button";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import Box from "@mui/material/Box";
import Alert from "@mui/material/Alert";

import { currentHistoryEntryIsLocalOverlay, replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ConfigurationColumn, ConfigurationRefOption, ConfigurationRegister, ConfigurationRow } from "@/lib/api/configuration-server";
import { createRowAction, deleteRowAction, setRowStatusAction, updateRowAction, type ConfigurationActionState } from "./configuration-actions";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

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
      <Stack component="form" action={formAction} aria-busy={pending} className="cfg-form" spacing={3}>
        <Box component="input" type="hidden" name="register" value={register.key} />
        {row ? <Box component="input" type="hidden" name="row_id" value={row.id} /> : null}
        {row ? <Box component="input" type="hidden" name="row_version" value={row.row_version} /> : null}
        <Box component="input" type="hidden" name="fields_json" value={fieldsJson} />

        {row?.is_builtin ? <Alert severity="info">{c("drawer.builtin_hint")}</Alert> : null}
        {editElsewhere ? (
          <Box>
            <Button href={editElsewhere.href} size="small" variant="outlined" color="inherit">
              {editElsewhere.label}
            </Button>
          </Box>
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
          // MUI adds the one required asterisk from `required`; the label is the column label only.
          const label = column.label;
          let control: React.ReactNode;
          switch (column.type) {
            case "bool":
              control = (
                <FormControlLabel disabled={disabled} control={<Checkbox id={id} checked={value === true} onChange={(e) => update(column.key, e.target.checked)} disabled={disabled} sx={{ p: { xs: 1.5, sm: 1 } }} />} label={column.label} />
              );
              break;
            case "enum":
              control = (
                <TextField
                  select
                  fullWidth
                  id={id}
                  label={label}
                  value={String(value ?? "")}
                  onChange={(e) => update(column.key, e.target.value)}
                  disabled={disabled}
                  required={column.required}
                  slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
                >
                  <MenuItem value="">—</MenuItem>
                  {(column.options ?? []).map((option) => (
                    <MenuItem key={option.value} value={option.value}>
                      {option.label}
                    </MenuItem>
                  ))}
                </TextField>
              );
              break;
            case "ref":
              control = (
                <TextField
                  select
                  fullWidth
                  id={id}
                  label={label}
                  value={String(value ?? "")}
                  onChange={(e) => {
                    update(column.key, e.target.value);
                    // Changing a park clears a pen that belonged to the old one.
                    if (column.key === "park_id" && register.columns.some((other) => other.key === "pen_id")) update("pen_id", "");
                  }}
                  disabled={disabled}
                  required={column.required}
                  slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
                >
                  <MenuItem value="">—</MenuItem>
                  {refOptions(column).map((option) => (
                    <MenuItem key={option.id} value={option.id}>
                      {option.label}
                    </MenuItem>
                  ))}
                </TextField>
              );
              break;
            case "notes":
              control = (
                <TextField
                  fullWidth
                  multiline
                  rows={3}
                  id={id}
                  label={label}
                  value={String(value ?? "")}
                  onChange={(e) => update(column.key, e.target.value)}
                  slotProps={{ htmlInput: { readOnly: disabled }, inputLabel: { shrink: true } }}
                />
              );
              break;
            case "number":
              control = (
                <TextField
                  fullWidth
                  type="number"
                  id={id}
                  label={label}
                  value={String(value ?? "")}
                  onChange={(e) => update(column.key, e.target.value)}
                  required={column.required}
                  slotProps={{
                    htmlInput: {
                      inputMode: column.integer ? "numeric" : "decimal",
                      step: column.integer ? 1 : "any",
                      min: column.min ?? undefined,
                      readOnly: disabled,
                    },
                    inputLabel: { shrink: true },
                  }}
                />
              );
              break;
            default:
              // An immutable column (a code) is DISABLED on edit, not merely read-only, so it reads
              // as fixed; the draft still carries its stored value and changedFields never sends it.
              control = (
                <TextField
                  fullWidth
                  id={id}
                  label={label}
                  value={String(value ?? "")}
                  onChange={(e) => update(column.key, e.target.value)}
                  disabled={column.immutable && isEdit}
                  required={column.required}
                  slotProps={{
                    htmlInput: { readOnly, maxLength: 500 },
                    inputLabel: { shrink: true },
                  }}
                />
              );
          }
          return (
            <Stack key={column.key} spacing={0.75}>
              {control}
              {column.hint ? (
                <Typography variant="caption" sx={{ color: "text.secondary", px: 1.75 }}>
                  {column.hint}
                </Typography>
              ) : null}
              {error ? (
                <Typography variant="caption" sx={{ color: "error.main", px: 1.75 }}>
                  {error}
                </Typography>
              ) : null}
            </Stack>
          );
        })}

        {message ? (
          <Alert severity={state.status === "error" ? "error" : "success"} role="status" aria-live="polite" key={`m-${state.ticket}`}>
            {message}
          </Alert>
        ) : null}

        {canEdit ? (
          <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
            <Button type="submit" variant="contained" color="primary" loading={pending}>
              {c("action.save")}
            </Button>
            <Button type="button" variant="outlined" onClick={() => closeOverlay(listHref)} disabled={pending}>
              {c("action.cancel")}
            </Button>
          </Stack>
        ) : null}
      </Stack>

      {row && (canSetStatus || canDelete) ? (
        <Stack spacing={1.25} sx={{ mt: 2.25, pt: 1.75, borderTop: (theme) => `1px solid ${theme.vars.palette.divider}` }}>
          {row.counts && Object.values(row.counts).some((n) => n > 0) ? (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {c("drawer.usage_title")}:{" "}
              {Object.entries(row.counts)
                .filter(([, n]) => n > 0)
                .map(([noun, n]) => `${n} ${noun.replace(/_/g, " ")}`)
                .join(", ")}
            </Typography>
          ) : null}
          <Stack direction="row" sx={{ gap: 1, flexWrap: "wrap", alignItems: "center" }}>
            {canSetStatus && !row.is_builtin ? (
              <form action={statusFormAction} aria-busy={statusPending}>
                <Box component="input" type="hidden" name="register" value={register.key} />
                <Box component="input" type="hidden" name="row_id" value={row.id} />
                <Box component="input" type="hidden" name="row_version" value={row.row_version} />
                <Box component="input" type="hidden" name="status" value={row.status === "archived" ? "active" : "archived"} />
                <Button type="submit" size="small" variant="outlined" loading={statusPending} title={c("drawer.archive_hint")}>
                  {row.status === "archived" ? c("action.restore") : c("action.archive")}
                </Button>
              </form>
            ) : null}
            {canDelete && !row.is_builtin ? (
              confirmDelete ? (
                <Stack component="form" direction="row" action={deleteFormAction} aria-busy={deletePending} sx={{ display: "inline-flex", gap: 1, alignItems: "center", flexWrap: "wrap" }}>
                  <Box component="input" type="hidden" name="register" value={register.key} />
                  <Box component="input" type="hidden" name="row_id" value={row.id} />
                  <Box component="input" type="hidden" name="row_version" value={row.row_version} />
                  <Typography component="span" variant="body2">{c("drawer.delete_confirm")}</Typography>
                  <Button type="submit" size="small" variant="outlined" color="error" loading={deletePending}>
                    {c("action.delete")}
                  </Button>
                  <Button type="button" size="small" variant="text" onClick={() => setConfirmDelete(false)}>
                    {c("action.cancel")}
                  </Button>
                </Stack>
              ) : (
                <Button type="button" size="small" variant="text" color="error" onClick={() => setConfirmDelete(true)}>
                  {c("action.delete")}
                </Button>
              )
            ) : null}
          </Stack>
          {statusMessage ? (
            <Alert severity={statusState.status === "error" ? "error" : "success"} role="status" aria-live="polite" key={`s-${statusState.ticket}`}>
              {statusMessage}
            </Alert>
          ) : null}
          {deleteMessage ? (
            <Alert severity={deleteState.status === "error" ? "error" : "success"} role="status" aria-live="polite" key={`d-${deleteState.ticket}`}>
              {deleteMessage}
            </Alert>
          ) : null}
        </Stack>
      ) : null}
    </>
  );
}
