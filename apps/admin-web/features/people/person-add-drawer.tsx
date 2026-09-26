"use client";

import { UserPlus, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import MenuItem from "@mui/material/MenuItem";
import { Tag, type Tone } from "@/components/ui-primitives";
import { PeopleFormSelect } from "./people-form-select";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkforcePeopleCatalog, WorkforcePerson } from "@/lib/api/server";
import { changePersonStatusAction, createPersonAction, setPersonTitleAction } from "./people-actions";

/** Reads the selected person from the address bar. "" means the drawer is closed. */
function readPersonParam(): string {
  return new URL(window.location.href).searchParams.get("person") ?? "";
}

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
    case "candidate":
      return "info";
    case "suspended":
      return "warn";
    case "left":
      return "dng";
    default:
      return "mut";
  }
}

/**
 * The directory's record / add overlay. Client state driven by the URL (the
 * LocalOverlayLink contract) with the mock's drawer anatomy: `.scrim`, `.dh`,
 * `.dc`, `.df`, and a `.metagrid` RECORD body for an existing person.
 */
export function PersonAddDrawer({
  people,
  catalog,
  pageContract,
  listHref,
}: {
  /** The rendered page of people. The drawer opens from this data — no fetch of its own. */
  people: WorkforcePerson[];
  catalog: WorkforcePeopleCatalog;
  pageContract: AdminUiPageContract;
  listHref: string;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readPersonParam, () => "");

  const isAdding = selection === "new";
  const person = isAdding ? null : (people.find((p) => p.person_id === selection) ?? null);
  const open = isAdding || person !== null;

  const roles = optionGroup(pageContract, "people_roles");
  const grades = optionGroup(pageContract, "people_designation_grades");

  // Role selection drives whether the park select is REQUIRED — the option's
  // title carries the grant's scope shape ("park"/"tenant") from the backend.
  const [role, setRole] = useState("");
  const [parkID, setParkID] = useState("");
  // Two-step confirm for the activate/deactivate action: the first click arms
  // the confirm block, the second submits. Reset whenever the selection moves.
  const [confirmingStatus, setConfirmingStatus] = useState(false);
  const parkRequired = roles.find((r) => r.key === role)?.title === "park";
  const incomplete = !role || (parkRequired && !parkID);

  // Reset the armed confirm during render when the selection moves (never in an
  // effect — same pattern as the vendor drawer's mode reset).
  const [syncedSelection, setSyncedSelection] = useState(selection);
  if (syncedSelection !== selection) {
    setSyncedSelection(selection);
    setConfirmingStatus(false);
  }

  // Minted once per drawer OPEN so a double-submit or retry converges on one
  // person server-side; re-opening the drawer starts a fresh create.
  const idempotencyKey = useMemo(() => {
    if (!isAdding) return "";
    return crypto.randomUUID();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAdding, selection]);

  const close = useCallback(() => {
    setRole("");
    setParkID("");
    setConfirmingStatus(false);
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
  const title = isAdding ? copy(pageContract, "drawer.add.title") : (person?.display_name ?? "");

  const cell = (label: string, value: string | null | undefined) => (
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
            <UserPlus className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "crumb")}</div>
            <h2>{title}</h2>
            {isAdding ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {copy(pageContract, "drawer.add.subtitle")}
              </div>
            ) : person ? (
              <div className="muted small" style={{ marginTop: 3 }}>
                {person.park_label ?? none} · {person.department_label ?? none}
              </div>
            ) : null}
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <IconButton
            ref={closeButtonRef}
            type="button"
            aria-label={copy(pageContract, "action.close")}
            onClick={close}
          >
            <X className="ic" aria-hidden="true" />
          </IconButton>
        </div>

        {isAdding ? (
          <form action={createPersonAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <input type="hidden" name="idempotency_key" value={idempotencyKey} />

              <div className="note">{copy(pageContract, "required.hint")}</div>

              <div className="fld">
                <TextField
                  fullWidth
                  id="p-first_name"
                  name="first_name"
                  label={field("first_name")}
                  required
                  slotProps={{ htmlInput: { maxLength: 120 }, inputLabel: { shrink: true } }}
                />
              </div>
              <div className="fld">
                <TextField
                  fullWidth
                  id="p-last_name"
                  name="last_name"
                  label={`${field("last_name")} (${field("optional")})`}
                  slotProps={{ htmlInput: { maxLength: 120 }, inputLabel: { shrink: true } }}
                />
              </div>
              <div className="fld">
                <TextField
                  fullWidth
                  id="p-email"
                  name="email"
                  type="email"
                  label={field("email")}
                  required
                  slotProps={{ htmlInput: { maxLength: 254 }, inputLabel: { shrink: true } }}
                />
              </div>
              {/* Role and Park were `required` native selects. A hidden input is barred from
                  constraint validation, so the required-ness now lives on the Save button
                  (`incomplete` below) -- the same "you cannot submit without these" rule, stated
                  in the control the operator actually clicks. The server action is unchanged. */}
              <div className="fld">
                <input type="hidden" name="role" value={role} />
                <TextField
                  select
                  label={field("role")}
                  value={role}
                  onChange={(event) => setRole(event.target.value)}
                  sx={{ flexShrink: 0, maxWidth: 1 }}
                  slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                >
                  <MenuItem value="">—</MenuItem>
                  {roles.map((option) => (
                    <MenuItem key={option.key} value={option.key}>
                      {option.label}
                    </MenuItem>
                  ))}
                </TextField>
              </div>
              <div className="fld">
                <input type="hidden" name="park_id" value={parkID} />
                <TextField
                  select
                  label={parkRequired ? field("park") : `${field("park")} (${field("optional")})`}
                  value={parkID}
                  onChange={(event) => setParkID(event.target.value)}
                  sx={{ flexShrink: 0, maxWidth: 1 }}
                  slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
                >
                  <MenuItem value="">—</MenuItem>
                  {catalog.parks.map((park) => (
                    <MenuItem key={park.id} value={park.id}>
                      {park.label}
                    </MenuItem>
                  ))}
                </TextField>
              </div>
              <PeopleFormSelect
                className="fld"
                name="department_id"
                minWidth={0}
                label={`${field("department")} (${field("optional")})`}
                options={[{ value: "", label: "—" }, ...catalog.departments.map((department) => ({ value: department.id, label: department.label }))]}
              />
              <PeopleFormSelect
                className="fld"
                name="designation_grade"
                minWidth={0}
                label={`${field("designation")} (${field("optional")})`}
                options={[{ value: "", label: "—" }, ...grades.map((grade) => ({ value: grade.key, label: grade.label }))]}
              />

              <div className="note">{copy(pageContract, "password.note")}</div>
            </div>
            <div className="df">
              <Button type="submit" variant="contained" disabled={incomplete}>
                {copy(pageContract, "action.save")}
              </Button>
              <Button type="button" variant="outlined" onClick={close}>
                {copy(pageContract, "action.cancel")}
              </Button>
            </div>
          </form>
        ) : person ? (
          <>
            <div className="dc">
              <div className="metagrid">
                {cell(field("first_name"), person.first_name)}
                {cell(field("last_name"), person.last_name)}
                {cell(field("email"), person.email)}
                {cell(field("park"), person.park_label)}
                {cell(field("department"), person.department_label)}
                {cell(field("designation"), person.designation_grade ?? person.role_hint)}
                {cell(copy(pageContract, "column.title"), person.title)}
                <div>
                  <div className="k">{copy(pageContract, "column.status")}</div>
                  <div className="v">
                    <Tag tone={statusTone(person.status)}>{person.status}</Tag>
                  </div>
                </div>
              </div>

              {/* Business title (maintainer request 2026-09-11): what the Tasks assignee picker
                  shows in place of the name. Edited here, on the person, never on the picker. */}
              <form action={setPersonTitleAction} className="fld" style={{ marginTop: 16 }}>
                <input type="hidden" name="return_to" value={listHref} />
                <input type="hidden" name="person_id" value={person.person_id} />
                <input type="hidden" name="row_version" value={person.row_version} />
                <div style={{ display: "flex", gap: 8 }}>
                  <TextField
                    id="p-title"
                    name="title"
                    label={copy(pageContract, "column.title")}
                    defaultValue={person.title ?? ""}
                    placeholder={copy(pageContract, "title.placeholder")}
                    slotProps={{ htmlInput: { maxLength: 80 }, inputLabel: { shrink: true } }}
                    sx={{ flex: 1 }}
                  />
                  <Button type="submit" variant="outlined">
                    {copy(pageContract, "action.save_title")}
                  </Button>
                </div>
                <div className="muted small" style={{ marginTop: 4 }}>
                  {copy(pageContract, "title.hint")}
                </div>
              </form>

              {/* Proof-work statistics: one verification item = one submitted
                  proof set; withdrawn/superseded items are excluded backend-side. */}
              <div className="hd" style={{ marginTop: 16 }}>
                <h3>{copy(pageContract, "stats.title")}</h3>
              </div>
              {person.proof_uploads > 0 ? (
                <div className="metagrid">
                  {cell(copy(pageContract, "stats.uploaded"), String(person.proof_uploads))}
                  {cell(copy(pageContract, "stats.approved"), String(person.proof_approved))}
                  {cell(copy(pageContract, "stats.rejected"), String(person.proof_rejected))}
                  {cell(copy(pageContract, "stats.pending"), String(person.proof_pending))}
                  {/* Settled by the randomization policy, never watched. Shown whenever it is
                      non-zero so "approved" cannot be read as "checked" -- the two stopped meaning
                      the same thing when sampling arrived (maintainer decision 2026-08-26). */}
                  {person.proof_not_reviewed > 0
                    ? cell(copy(pageContract, "stats.not_reviewed"), String(person.proof_not_reviewed))
                    : null}
                  <div>
                    <div className="k">{copy(pageContract, "stats.rejection_rate")}</div>
                    <div className="v">
                      {person.proof_rejection_pct === null || person.proof_rejection_pct === undefined ? (
                        <span className="muted">{copy(pageContract, "stats.no_reviews")}</span>
                      ) : (
                        <Tag tone={person.proof_rejection_pct >= 20 ? "dng" : person.proof_rejection_pct > 0 ? "warn" : "ok"}>
                          {person.proof_rejection_pct}%
                        </Tag>
                      )}
                    </div>
                  </div>
                </div>
              ) : (
                <div className="muted small">{copy(pageContract, "stats.none")}</div>
              )}

              {confirmingStatus ? (
                <div className="note" style={{ marginTop: 16 }}>
                  <b>
                    {copy(
                      pageContract,
                      person.status === "active" ? "confirm.deactivate.title" : "confirm.activate.title",
                    )}
                  </b>
                  <div style={{ marginTop: 4 }}>
                    {copy(
                      pageContract,
                      person.status === "active" ? "confirm.deactivate.body" : "confirm.activate.body",
                    )}
                  </div>
                </div>
              ) : null}
            </div>
            <div className="df">
              {confirmingStatus ? (
                <form action={changePersonStatusAction} style={{ display: "contents" }}>
                  <input type="hidden" name="return_to" value={listHref} />
                  <input type="hidden" name="person_id" value={person.person_id} />
                  <input type="hidden" name="row_version" value={person.row_version} />
                  <input type="hidden" name="target_status" value={person.status === "active" ? "deactivate" : "activate"} />
                  <Button type="submit" variant="contained" color={person.status === "active" ? "error" : "primary"}>
                    {copy(pageContract, "action.confirm")}
                  </Button>
                  <Button type="button" variant="outlined" onClick={() => setConfirmingStatus(false)}>
                    {copy(pageContract, "action.cancel")}
                  </Button>
                </form>
              ) : (
                <Button
                  type="button"
                  variant="contained"
                  color={person.status === "active" ? "error" : "primary"}
                  onClick={() => setConfirmingStatus(true)}
                >
                  {copy(pageContract, person.status === "active" ? "action.deactivate" : "action.activate")}
                </Button>
              )}
            </div>
          </>
        ) : null}
      </aside>
    </>
  );
}
