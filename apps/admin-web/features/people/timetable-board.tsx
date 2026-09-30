"use client";

import { useState, useTransition } from "react";
import { CalendarOff, Clock3, UsersRound } from "lucide-react";
import { faro } from "@grafana/faro-web-sdk";

import Link from "@/components/no-prefetch-link";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { TimetablePerson, TimetableShift, WorkforceHoliday, WorkforceTimetable } from "@/lib/api/server";
import { addHolidayAction, removeHolidayAction, setPersonShiftAction, setShiftTimingAction, setWeekOffsAction } from "./timetable-actions";
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
function track(event: "timetable_person_shift" | "timetable_shift_timing" | "timetable_week_offs" | "timetable_holiday_add" | "timetable_holiday_remove", status: "success" | "error", code = "") {
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
                    weekdays={timetable.weekdays}
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

      <HolidaysCard pageContract={pageContract} timetable={timetable} canEdit={canEdit} editReason={editReason} />
    </>
  );
}

function PersonRow({
  pageContract,
  person,
  shifts,
  weekdays,
  canEdit,
  editReason,
  onSaved,
}: {
  pageContract: AdminUiPageContract;
  person: TimetablePerson;
  shifts: TimetableShift[];
  weekdays: WorkforceTimetable["weekdays"];
  canEdit: boolean;
  editReason: string;
  onSaved: (before: TimetablePerson, saved: TimetablePerson) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [pending, startTransition] = useTransition();

  // A weekly off is a fact about the person's shift: it saves the moment a day is ticked.
  const toggleOff = (day: number) => {
    const next = person.week_offs.includes(day) ? person.week_offs.filter((d) => d !== day) : [...person.week_offs, day].sort();
    setStatus({ kind: "saving" });
    startTransition(async () => {
      const result = await setWeekOffsAction({ personId: person.person_id, weekOffs: next, rowVersion: person.row_version });
      if (result.ok) {
        track("timetable_week_offs", "success");
        onSaved(person, result.row);
        setStatus({ kind: "saved" });
      } else {
        track("timetable_week_offs", "error", result.code);
        setStatus({ kind: "error", message: result.message || t("action.failed") });
      }
    });
  };

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
      <td>
        {person.shift_code ? (
          <div className="tt-offs" role="group" aria-label={t("weekoff.aria").replace("%s", person.display_name)} data-testid="timetable-week-offs">
            {weekdays.map((d) => (
              <label key={d.key} className={person.week_offs.includes(d.key) ? "tt-off on" : "tt-off"}>
                <input type="checkbox" checked={person.week_offs.includes(d.key)} disabled={!canEdit || pending} onChange={() => toggleOff(d.key)} />
                <span>{d.label}</span>
              </label>
            ))}
          </div>
        ) : (
          <span className="muted small">{t("weekoff.needs_shift")}</span>
        )}
      </td>
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

/**
 * Holidays (2026-09-30): dates HR enters for every park or one park. The clock-in check checks
 * nobody on them. None today -- the farm runs every day -- so an empty card is the normal state.
 */
function HolidaysCard({ pageContract, timetable, canEdit, editReason }: { pageContract: AdminUiPageContract; timetable: WorkforceTimetable; canEdit: boolean; editReason: string }) {
  const t = (key: string) => copy(pageContract, key);
  const [holidays, setHolidays] = useState<WorkforceHoliday[]>(timetable.holidays);
  const [date, setDate] = useState("");
  const [parkId, setParkId] = useState("");
  const [label, setLabel] = useState("");
  const [confirming, setConfirming] = useState("");
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const [pending, startTransition] = useTransition();

  const add = () =>
    startTransition(async () => {
      setStatus({ kind: "saving" });
      const result = await addHolidayAction({ holidayOn: date, parkId, label });
      if (result.ok) {
        track("timetable_holiday_add", "success");
        const saved = result.row;
        // Only a holiday for this park (or every park) belongs on this park's card.
        if (!saved.park_id || saved.park_id === timetable.park_id) {
          setHolidays((prev) => [...prev.filter((h) => h.holiday_id !== saved.holiday_id), saved].sort((a, b) => a.holiday_on.localeCompare(b.holiday_on)));
        }
        setDate("");
        setLabel("");
        setStatus({ kind: "saved" });
      } else {
        track("timetable_holiday_add", "error", result.code);
        setStatus({ kind: "error", message: result.message || t("action.failed") });
      }
    });
  const remove = (id: string) =>
    startTransition(async () => {
      const result = await removeHolidayAction(id);
      if (result.ok) {
        track("timetable_holiday_remove", "success");
        setHolidays((prev) => prev.filter((h) => h.holiday_id !== id));
        setConfirming("");
      } else {
        track("timetable_holiday_remove", "error", result.code);
        setStatus({ kind: "error", message: result.message || t("action.failed") });
      }
    });

  return (
    <section className="card" data-testid="timetable-holidays" style={{ marginTop: 16 }}>
      <div className="hd">
        <CalendarOff className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{t("holidays.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{t("holidays.hint")}</span>
      </div>
      <div className="bd">
        {holidays.length === 0 ? (
          <div className="small muted" data-testid="timetable-holidays-empty">
            {t("holidays.empty")}
          </div>
        ) : (
          <div className="tt-holidays">
            {holidays.map((h) => (
              <div className="tt-holiday" key={h.holiday_id} data-testid="timetable-holiday">
                <b>{h.date_label}</b>
                <span>{h.label}</span>
                <span className="muted small">{h.park_label}</span>
                <div className="sp" style={{ flex: 1 }} />
                {confirming === h.holiday_id ? (
                  <>
                    <button type="button" className="btn sm dng" disabled={pending} onClick={() => remove(h.holiday_id)}>
                      {t("holidays.remove_confirm")}
                    </button>
                    <button type="button" className="btn sm" disabled={pending} onClick={() => setConfirming("")}>
                      {t("action.cancel")}
                    </button>
                  </>
                ) : (
                  <button type="button" className="btn sm" disabled={!canEdit} title={canEdit ? undefined : editReason} onClick={() => setConfirming(h.holiday_id)}>
                    {t("holidays.remove")}
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
        {canEdit ? (
          <div className="tt-holiday-form" data-testid="timetable-holiday-form">
            <div className="fld">
              <span className="small muted">{t("holidays.date")}</span>
              <ThemedDatePicker
                name="holiday_on"
                label={t("holidays.date")}
                value={date}
                onChange={setDate}
                previousMonthLabel={t("holidays.prev_month")}
                nextMonthLabel={t("holidays.next_month")}
                invalidDateText={t("holidays.invalid_date")}
              />
            </div>
            <label className="fld">
              <span className="small muted">{t("holidays.park")}</span>
              <select className="inp" value={parkId} onChange={(e) => setParkId(e.target.value)} data-testid="timetable-holiday-park">
                <option value="">{t("holidays.every_park")}</option>
                {timetable.parks.map((p) => (
                  <option key={p.park_id} value={p.park_id}>
                    {p.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="fld tt-holiday-name">
              <span className="small muted">{t("holidays.name")}</span>
              <input className="inp" value={label} maxLength={80} placeholder={t("holidays.name_hint")} onChange={(e) => setLabel(e.target.value)} data-testid="timetable-holiday-name" />
            </label>
            <button type="button" className="btn primary" disabled={pending || !date || !label.trim()} onClick={add} data-testid="timetable-holiday-add">
              {t("holidays.add")}
            </button>
          </div>
        ) : null}
        <StatusLine pageContract={pageContract} status={status} />
      </div>
    </section>
  );
}
