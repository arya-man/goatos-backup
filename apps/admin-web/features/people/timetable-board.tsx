"use client";

import { useState, useTransition } from "react";
import { Clock3, UsersRound } from "lucide-react";
import { faro } from "@grafana/faro-web-sdk";

import Link from "@/components/no-prefetch-link";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { TimetablePerson, TimetableShift, WorkforceTimetable } from "@/lib/api/server";
import { setPersonShiftAction, setShiftTimingAction } from "./timetable-actions";
import {
  HOURS,
  draftFromTiming,
  hourLabel,
  minuteLabel,
  minuteOptions,
  timingRequestFromDraft,
  type TimeDraft,
  type TimingDraft,
} from "./timetable-form";

// Faro RUM for the page's two writes (TELEMETRY GUARDRAIL): the outcome only, never a name.
function track(event: "timetable_person_shift" | "timetable_shift_timing", status: "success" | "error", code = "") {
  try {
    faro.api?.pushEvent(event, { status, code });
  } catch {
    // Faro must never break the page.
  }
}

type Status = { kind: "idle" } | { kind: "saving" } | { kind: "saved" } | { kind: "error"; message: string };

/**
 * People / HRMS > Timetable, the interactive half (maintainer request 2026-09-30). The server
 * page reads one park and hands it here; a save returns the saved row and it is put in place --
 * the shift's hours on its card and on every person working it, a person's shift on their row and
 * in the per-shift counts -- with no page reload. Every word is backend copy or a
 * backend-composed field (timing_label, shift_label, label).
 */
export function TimetableBoard({
  pageContract,
  timetable,
  canEdit,
  editReason,
  filterLinks,
  firstPageHref,
  nextPageHref,
}: {
  pageContract: AdminUiPageContract;
  timetable: WorkforceTimetable;
  canEdit: boolean;
  editReason: string;
  /** The shift filter strip, composed by the server page (labels carry whole-park counts). */
  filterLinks: { key: string; label: string; href: string; active: boolean }[];
  /** Keyset pager targets; "" when there is no such page. */
  firstPageHref: string;
  nextPageHref: string;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [shifts, setShifts] = useState<TimetableShift[]>(timetable.shifts);
  const [people, setPeople] = useState<TimetablePerson[]>(timetable.people);
  const labels = tableLabels(pageContract, "timetable-people");

  const onShiftSaved = (saved: TimetableShift) => {
    setShifts((prev) => prev.map((s) => (s.shift_code === saved.shift_code ? { ...saved, people_count: s.people_count } : s)));
    setPeople((prev) => prev.map((p) => (p.shift_code === saved.shift_code ? { ...p, timing_label: saved.timing_label } : p)));
  };
  const onPersonSaved = (before: TimetablePerson, saved: TimetablePerson) => {
    setPeople((prev) => prev.map((p) => (p.person_id === saved.person_id ? saved : p)));
    if (before.shift_code === saved.shift_code) return;
    setShifts((prev) =>
      prev.map((s) =>
        s.shift_code === before.shift_code
          ? { ...s, people_count: Math.max(0, s.people_count - 1) }
          : s.shift_code === saved.shift_code
            ? { ...s, people_count: s.people_count + 1 }
            : s,
      ),
    );
  };

  const peopleCount = (n: number) => (n === 1 ? t("shifts.person_count") : t("shifts.people_count").replace("%d", String(n)));

  return (
    <>
      <section className="card" data-testid="timetable-shifts" style={{ marginBottom: 16 }}>
        <div className="hd">
          <Clock3 className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>
            {t("shifts.title")} · {timetable.park_label}
          </h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{t("shifts.hint")}</span>
        </div>
        <div className="tt-shifts">
          {shifts.map((shift) => (
            <ShiftCard
              key={shift.shift_code}
              pageContract={pageContract}
              parkId={timetable.park_id}
              shift={shift}
              peopleLabel={peopleCount(shift.people_count)}
              canEdit={canEdit}
              editReason={editReason}
              onSaved={onShiftSaved}
            />
          ))}
        </div>
      </section>

      <section className="card" data-testid="timetable-people">
        <div className="hd">
          <UsersRound className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{t("people.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{t("people.hint")}</span>
        </div>
        <div className="tt-filter">
          <nav className="subtabs" aria-label={t("filter.shift")} data-testid="timetable-shift-filter">
            {filterLinks.map((f) => (
              <Link key={f.key} href={f.href} className={f.active ? "on" : undefined} aria-current={f.active ? "page" : undefined} replace scroll={false}>
                {f.label}
              </Link>
            ))}
          </nav>
        </div>
        {people.length === 0 ? (
          <div className="empty">{timetable.shift_filter ? t("people.empty_filtered") : t("people.empty")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={t("people.title")}>
            <table className="people-table" aria-label={t("people.title")}>
              <thead>
                <tr>
                  {labels.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {people.map((person) => (
                  <PersonRow
                    key={person.person_id}
                    pageContract={pageContract}
                    person={person}
                    shifts={shifts}
                    canEdit={canEdit}
                    editReason={editReason}
                    onSaved={onPersonSaved}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
        {firstPageHref || nextPageHref ? (
          <div className="pager" style={{ padding: 12, display: "flex", gap: 8 }}>
            {firstPageHref ? (
              <Link href={firstPageHref} className="btn" scroll={false}>
                {t("pager.first")}
              </Link>
            ) : null}
            {nextPageHref ? (
              <Link href={nextPageHref} className="btn" scroll={false}>
                {t("pager.next")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>
    </>
  );
}

function PersonRow({
  pageContract,
  person,
  shifts,
  canEdit,
  editReason,
  onSaved,
}: {
  pageContract: AdminUiPageContract;
  person: TimetablePerson;
  shifts: TimetableShift[];
  canEdit: boolean;
  editReason: string;
  onSaved: (before: TimetablePerson, saved: TimetablePerson) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [pending, startTransition] = useTransition();

  const change = (shiftCode: string) => {
    if (shiftCode === person.shift_code) return;
    setStatus({ kind: "saving" });
    startTransition(async () => {
      const result = await setPersonShiftAction({ personId: person.person_id, shiftCode, rowVersion: person.row_version });
      if (result.ok) {
        track("timetable_person_shift", "success");
        onSaved(person, result.row);
        setStatus({ kind: "saved" });
      } else {
        track("timetable_person_shift", "error", result.code);
        setStatus({ kind: "error", message: result.message || t("action.failed") });
      }
    });
  };

  return (
    <tr data-testid="timetable-person-row">
      <td>
        <b>{person.display_name}</b>
      </td>
      <td>{person.designation}</td>
      <td>{person.department}</td>
      <td>
        <select
          className="inp"
          value={person.shift_code}
          disabled={!canEdit || pending}
          title={canEdit ? undefined : editReason}
          aria-label={t("shift.aria").replace("%s", person.display_name)}
          onChange={(event) => change(event.target.value)}
          data-testid="timetable-person-shift"
          style={{ minWidth: 150 }}
        >
          <option value="">{t("shift.none")}</option>
          {shifts.map((s) => (
            <option key={s.shift_code} value={s.shift_code}>
              {s.label}
            </option>
          ))}
        </select>
        <StatusLine pageContract={pageContract} status={status} />
      </td>
      <td className={person.timing_label ? undefined : "muted"}>{person.timing_label || "—"}</td>
    </tr>
  );
}

function ShiftCard({
  pageContract,
  parkId,
  shift,
  peopleLabel,
  canEdit,
  editReason,
  onSaved,
}: {
  pageContract: AdminUiPageContract;
  parkId: string;
  shift: TimetableShift;
  peopleLabel: string;
  canEdit: boolean;
  editReason: string;
  onSaved: (saved: TimetableShift) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState<TimingDraft>(() => draftFromTiming(shift.start_minute, shift.end_minute));
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [pending, startTransition] = useTransition();

  const open = () => {
    setDraft(draftFromTiming(shift.start_minute, shift.end_minute));
    setStatus({ kind: "idle" });
    setEditing(true);
  };
  const save = () => {
    const request = timingRequestFromDraft(draft);
    if (!request.ok) {
      setStatus({ kind: "error", message: t("action.start_required") });
      return;
    }
    setStatus({ kind: "saving" });
    startTransition(async () => {
      const result = await setShiftTimingAction({
        parkId,
        shiftCode: shift.shift_code,
        startMinute: request.body.start_minute,
        endMinute: request.body.end_minute,
        rowVersion: shift.row_version,
      });
      if (result.ok) {
        track("timetable_shift_timing", "success");
        onSaved(result.row);
        setEditing(false);
        setStatus({ kind: "saved" });
      } else {
        track("timetable_shift_timing", "error", result.code);
        setStatus({ kind: "error", message: result.message || t("action.failed") });
      }
    });
  };

  return (
    <div className="tt-shift" data-testid="timetable-shift" data-shift={shift.shift_code}>
      <div className="tt-shift-head">
        <Clock3 size={16} aria-hidden="true" />
        <b>{shift.label}</b>
      </div>
      <div className={shift.is_set ? "tt-shift-time" : "tt-shift-time muted"} data-testid="timetable-shift-timing">
        {shift.timing_label}
      </div>
      <div className="small muted">{peopleLabel}</div>

      {editing ? (
        <div className="tt-shift-form">
          <TimeField
            pageContract={pageContract}
            label={t("field.start")}
            idPrefix={`tt-${shift.shift_code}-start`}
            value={draft.start}
            onChange={(start) => setDraft({ ...draft, start })}
          />
          <TimeField
            pageContract={pageContract}
            label={t("field.end")}
            idPrefix={`tt-${shift.shift_code}-end`}
            value={draft.end}
            allowNone
            onChange={(end) => setDraft({ ...draft, end })}
          />
          <div className="tt-shift-actions">
            <button type="button" className="btn primary sm" onClick={save} disabled={pending} data-testid="timetable-shift-save">
              {pending ? t("action.saving") : t("action.save")}
            </button>
            <button type="button" className="btn sm" onClick={() => setEditing(false)} disabled={pending}>
              {t("action.cancel")}
            </button>
          </div>
        </div>
      ) : (
        <button
          type="button"
          className="btn sm"
          onClick={open}
          disabled={!canEdit}
          title={canEdit ? undefined : editReason}
          data-testid="timetable-shift-edit"
        >
          {shift.is_set ? t("action.edit_timing") : t("action.set_timing")}
        </button>
      )}
      <StatusLine pageContract={pageContract} status={status} />
    </div>
  );
}

function TimeField({
  pageContract,
  label,
  idPrefix,
  value,
  allowNone = false,
  onChange,
}: {
  pageContract: AdminUiPageContract;
  label: string;
  idPrefix: string;
  value: TimeDraft;
  allowNone?: boolean;
  onChange: (next: TimeDraft) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  return (
    <fieldset className="tt-time">
      <legend className="small">{label}</legend>
      <div className="tt-time-row">
        <label className="sr-only" htmlFor={`${idPrefix}-hour`}>
          {label} · {t("field.hour")}
        </label>
        <select
          id={`${idPrefix}-hour`}
          className="inp"
          value={value.hour}
          onChange={(event) => onChange({ hour: event.target.value, minute: event.target.value === "" ? "" : value.minute || "0" })}
        >
          <option value="">{allowNone ? t("field.end_none") : t("field.hour")}</option>
          {HOURS.map((h) => (
            <option key={h} value={String(h)}>
              {hourLabel(h)}
            </option>
          ))}
        </select>
        <label className="sr-only" htmlFor={`${idPrefix}-minute`}>
          {label} · {t("field.minute")}
        </label>
        <select
          id={`${idPrefix}-minute`}
          className="inp"
          value={value.minute}
          disabled={value.hour === ""}
          onChange={(event) => onChange({ ...value, minute: event.target.value })}
        >
          {value.hour === "" ? <option value="">{t("field.minute")}</option> : null}
          {minuteOptions(value.minute).map((m) => (
            <option key={m} value={String(m)}>
              {minuteLabel(m)}
            </option>
          ))}
        </select>
      </div>
    </fieldset>
  );
}

function StatusLine({ pageContract, status }: { pageContract: AdminUiPageContract; status: Status }) {
  if (status.kind === "idle") return null;
  const text =
    status.kind === "saving" ? copy(pageContract, "action.saving") : status.kind === "saved" ? copy(pageContract, "action.saved") : status.message;
  return (
    <div className={status.kind === "error" ? "small tt-status dng" : "small tt-status muted"} role={status.kind === "error" ? "alert" : "status"}>
      {text}
    </div>
  );
}
