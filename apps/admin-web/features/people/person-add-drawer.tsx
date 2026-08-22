"use client";

import { UserPlus, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkforcePeopleCatalog, WorkforcePerson } from "@/lib/api/server";
import { changePersonStatusAction, createPersonAction } from "./people-actions";

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
  // Two-step confirm for the activate/deactivate action: the first click arms
  // the confirm block, the second submits. Reset whenever the selection moves.
  const [confirmingStatus, setConfirmingStatus] = useState(false);
  const parkRequired = roles.find((r) => r.key === role)?.title === "park";

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
          <form action={createPersonAction} style={{ display: "contents" }}>
            <div className="dc">
              <input type="hidden" name="return_to" value={listHref} />
              <input type="hidden" name="idempotency_key" value={idempotencyKey} />

              <div className="note">{copy(pageContract, "required.hint")}</div>

              <div className="fld">
                <label htmlFor="p-first_name">{field("first_name")}</label>
                <input id="p-first_name" name="first_name" required maxLength={120} />
              </div>
              <div className="fld">
                <label htmlFor="p-last_name">
                  {field("last_name")} <span className="muted small">({field("optional")})</span>
                </label>
                <input id="p-last_name" name="last_name" maxLength={120} />
              </div>
              <div className="fld">
                <label htmlFor="p-email">{field("email")}</label>
                <input id="p-email" name="email" type="email" required maxLength={254} />
              </div>
              <div className="fld">
                <label htmlFor="p-role">{field("role")}</label>
                <select
                  id="p-role"
                  name="role"
                  required
                  value={role}
                  onChange={(event) => setRole(event.target.value)}
                >
                  <option value="" disabled>
                    —
                  </option>
                  {roles.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="p-park">
                  {field("park")}{" "}
                  {!parkRequired ? <span className="muted small">({field("optional")})</span> : null}
                </label>
                <select id="p-park" name="park_id" required={parkRequired} defaultValue="">
                  <option value="" disabled={parkRequired}>
                    —
                  </option>
                  {catalog.parks.map((park) => (
                    <option key={park.id} value={park.id}>
                      {park.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="p-department">
                  {field("department")} <span className="muted small">({field("optional")})</span>
                </label>
                <select id="p-department" name="department_id" defaultValue="">
                  <option value="">—</option>
                  {catalog.departments.map((department) => (
                    <option key={department.id} value={department.id}>
                      {department.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor="p-designation">
                  {field("designation")} <span className="muted small">({field("optional")})</span>
                </label>
                <select id="p-designation" name="designation_grade" defaultValue="">
                  <option value="">—</option>
                  {grades.map((grade) => (
                    <option key={grade.key} value={grade.key}>
                      {grade.label}
                    </option>
                  ))}
                </select>
              </div>

              <div className="note">{copy(pageContract, "password.note")}</div>
            </div>
            <div className="df">
              <button type="submit" className="btn primary">
                {copy(pageContract, "action.save")}
              </button>
              <button type="button" className="btn" onClick={close}>
                {copy(pageContract, "action.cancel")}
              </button>
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
                <div>
                  <div className="k">{copy(pageContract, "column.status")}</div>
                  <div className="v">
                    <Tag tone={statusTone(person.status)}>{person.status}</Tag>
                  </div>
                </div>
              </div>

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
                  <input
                    type="hidden"
                    name="target_status"
                    value={person.status === "active" ? "deactivate" : "activate"}
                  />
                  <button type="submit" className={person.status === "active" ? "btn dng" : "btn primary"}>
                    {copy(pageContract, "action.confirm")}
                  </button>
                  <button type="button" className="btn" onClick={() => setConfirmingStatus(false)}>
                    {copy(pageContract, "action.cancel")}
                  </button>
                </form>
              ) : (
                <button
                  type="button"
                  className={person.status === "active" ? "btn dng" : "btn primary"}
                  onClick={() => setConfirmingStatus(true)}
                >
                  {copy(pageContract, person.status === "active" ? "action.deactivate" : "action.activate")}
                </button>
              )}
            </div>
          </>
        ) : null}
      </aside>
    </>
  );
}
