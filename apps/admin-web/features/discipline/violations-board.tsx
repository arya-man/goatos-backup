"use client";

import { useMemo, useState, useTransition } from "react";
import { Plus, ShieldAlert } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { Violation, ViolationsPage } from "@/lib/api/server";
import { recordViolationAction, withdrawViolationAction } from "./actions";
import { rupeesLabel, todayKey } from "./format";
import { trackDiscipline } from "./telemetry";

/**
 * People / HRMS > Violations, the interactive half (maintainer decisions 2026-09-30). A recorded or
 * withdrawn violation comes back from the server and is put in place -- the list, the person's
 * total and the page totals move together, with no page reload. Every word is backend copy or a
 * backend-composed field.
 */
export function ViolationsBoard({
  pageContract,
  page,
  canEdit,
  editReason,
  firstPageHref,
  nextPageHref,
}: {
  pageContract: AdminUiPageContract;
  page: ViolationsPage;
  canEdit: boolean;
  editReason: string;
  firstPageHref: string;
  nextPageHref: string;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [items, setItems] = useState<Violation[]>(page.items);
  const [byPerson, setByPerson] = useState(page.by_person);
  const [summary, setSummary] = useState(page.summary);
  const [formOpen, setFormOpen] = useState(false);

  const applyDelta = (v: Violation, sign: 1 | -1) => {
    const next = nextByPerson(byPerson, v, sign);
    setByPerson(next);
    // People = the distinct persons still owing in this filter, the backend's own definition.
    const fine = summary.fine_rupees + sign * v.fine_rupees;
    setSummary({ ...summary, count: summary.count + sign, fine_rupees: fine, fine_label: rupeesLabel(fine), people: next.length });
  };
  const nextByPerson = (rows: ViolationsPage["by_person"], v: Violation, sign: 1 | -1): ViolationsPage["by_person"] => {
    const existing = rows.find((r) => r.person_id === v.person_id);
    if (!existing) {
      return sign > 0
        ? [...rows, { person_id: v.person_id, person_name: v.person_name, designation: v.designation, park_label: v.park_label, count: 1, fine_rupees: v.fine_rupees, fine_label: v.fine_label }]
        : rows;
    }
    return rows
      .map((r) => {
        if (r.person_id !== v.person_id) return r;
        const fine = r.fine_rupees + sign * v.fine_rupees;
        return { ...r, count: r.count + sign, fine_rupees: fine, fine_label: rupeesLabel(fine) };
      })
      .filter((r) => r.count > 0)
      .sort((a, b) => b.fine_rupees - a.fine_rupees);
  };

  const onRecorded = (v: Violation) => {
    // Only a violation inside this page's month joins it; the totals follow the list.
    if (v.occurred_on.startsWith(page.month) && page.status !== "withdrawn") {
      setItems((prev) => [v, ...prev]);
      applyDelta(v, 1);
    }
    setFormOpen(false);
  };
  const onWithdrawn = (v: Violation) => {
    setItems((prev) => (page.status === "recorded" ? prev.filter((p) => p.violation_id !== v.violation_id) : prev.map((p) => (p.violation_id === v.violation_id ? v : p))));
    applyDelta(v, -1);
  };

  const listLabels = tableLabels(pageContract, "violations");
  const personLabels = tableLabels(pageContract, "violation-people");

  return (
    <>
      <div className="dsc-tiles">
        {[
          { key: "count", label: t("summary.count"), value: String(summary.count) },
          { key: "fines", label: t("summary.fines"), value: summary.fine_label },
          { key: "people", label: t("summary.people"), value: String(summary.people) },
        ].map((tile) => (
          <div className="card dsc-tile" key={tile.key} data-testid={`violations-tile-${tile.key}`}>
            <div className="muted small">{tile.label}</div>
            <div className="dsc-tile-value">{tile.value}</div>
          </div>
        ))}
        <div className="dsc-tiles-action">
          <button type="button" className="btn primary" disabled={!canEdit} title={canEdit ? undefined : editReason} onClick={() => setFormOpen((o) => !o)} data-testid="violations-record-open">
            <Plus className="ic" /> {t("action.record")}
          </button>
        </div>
      </div>

      {formOpen ? <RecordForm pageContract={pageContract} page={page} onDone={onRecorded} onCancel={() => setFormOpen(false)} /> : null}

      <section className="card" data-testid="violations-by-person">
        <div className="hd">
          <ShieldAlert className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{t("people.title")}</h3>
        </div>
        {byPerson.length === 0 ? (
          <div className="empty">{t("people.empty")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={t("people.title")}>
            <table className="people-table">
              <thead>
                <tr>
                  {personLabels.map((l) => (
                    <th key={l}>{l}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {byPerson.map((p) => (
                  <tr key={p.person_id} data-testid="violations-person-row">
                    <td>
                      <b>{p.person_name}</b>
                      {p.designation ? <div className="small muted">{p.designation}</div> : null}
                    </td>
                    <td>{p.park_label}</td>
                    <td>{p.count}</td>
                    <td>
                      <b>{p.fine_label}</b>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="card" data-testid="violations-list">
        <div className="hd">
          <h3>{t("list.title")}</h3>
        </div>
        {items.length === 0 ? (
          <div className="empty">{t("list.empty")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={t("list.title")}>
            <table className="people-table">
              <thead>
                <tr>
                  {listLabels.map((l) => (
                    <th key={l}>{l}</th>
                  ))}
                  <th aria-hidden="true" />
                </tr>
              </thead>
              <tbody>
                {items.map((v) => (
                  <ViolationRow key={v.violation_id} pageContract={pageContract} violation={v} canEdit={canEdit} editReason={editReason} onWithdrawn={onWithdrawn} />
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

function ViolationRow({
  pageContract,
  violation: v,
  canEdit,
  editReason,
  onWithdrawn,
}: {
  pageContract: AdminUiPageContract;
  violation: Violation;
  canEdit: boolean;
  editReason: string;
  onWithdrawn: (v: Violation) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [asking, setAsking] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const withdrawn = v.status === "withdrawn";

  const confirm = () =>
    startTransition(async () => {
      const res = await withdrawViolationAction(v.violation_id, reason, v.row_version);
      if (res.ok) {
        trackDiscipline("hrms_violation_withdraw", "success");
        setAsking(false);
        onWithdrawn(res.row);
      } else {
        trackDiscipline("hrms_violation_withdraw", "error", res.code);
        setError(res.message || t("action.failed"));
      }
    });

  return (
    <tr className={withdrawn ? "dsc-withdrawn" : undefined} data-testid="violation-row">
      <td>{v.occurred_on_label}</td>
      <td>
        <b>{v.person_name}</b>
        {v.designation ? <div className="small muted">{v.designation}</div> : null}
      </td>
      <td>
        {v.type_label}
        <div className="small muted">{v.source_label}</div>
      </td>
      <td>
        <b>{v.fine_label}</b>
      </td>
      <td className="dsc-note">{v.note}</td>
      <td>
        {v.recorded_by_name}
        <div className="small muted">{v.recorded_at_label}</div>
      </td>
      <td>
        <span className={withdrawn ? "tag t-mut" : "tag t-warn"}>{v.status_label}</span>
        {withdrawn && v.withdraw_reason ? <div className="small muted">{v.withdraw_reason}</div> : null}
      </td>
      <td>
        {withdrawn ? null : asking ? (
          <div className="dsc-withdraw">
            <input className="inp" value={reason} maxLength={500} placeholder={t("action.withdraw_reason")} aria-label={t("action.withdraw_reason")} onChange={(e) => setReason(e.target.value)} data-testid="violation-withdraw-reason" />
            <button type="button" className="btn sm dng" disabled={pending || !reason.trim()} onClick={confirm} data-testid="violation-withdraw-confirm">
              {t("action.withdraw_confirm")}
            </button>
            <button type="button" className="btn sm" disabled={pending} onClick={() => setAsking(false)}>
              {t("action.cancel")}
            </button>
            {error ? <div className="small tt-status dng">{error}</div> : null}
          </div>
        ) : (
          <button type="button" className="btn sm" disabled={!canEdit} title={canEdit ? undefined : editReason} onClick={() => setAsking(true)} data-testid="violation-withdraw">
            {t("action.withdraw")}
          </button>
        )}
      </td>
    </tr>
  );
}

function RecordForm({ pageContract, page, onDone, onCancel }: { pageContract: AdminUiPageContract; page: ViolationsPage; onDone: (v: Violation) => void; onCancel: () => void }) {
  const t = (key: string) => copy(pageContract, key);
  const [personId, setPersonId] = useState("");
  const [typeKey, setTypeKey] = useState("");
  const [date, setDate] = useState(todayKey());
  const [fine, setFine] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  // One retry key per form: a double click or a lost response replays, never double-fines.
  const idempotencyKey = useMemo(() => `web-violation-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`, []);

  if (page.types.length === 0) {
    return (
      <div className="card dsc-form" data-testid="violations-no-types">
        <div className="bd small muted">{t("form.no_types")}</div>
      </div>
    );
  }

  const pickType = (key: string) => {
    setTypeKey(key);
    const type = page.types.find((x) => x.key === key);
    if (type) setFine(String(type.default_fine));
  };
  const save = () => {
    if (!personId || !typeKey || !date) {
      setError(t("action.required"));
      return;
    }
    setError("");
    startTransition(async () => {
      const amount = Number.parseInt(fine, 10);
      const res = await recordViolationAction({
        person_id: personId,
        type_key: typeKey,
        occurred_on: date,
        fine_rupees: Number.isFinite(amount) ? amount : null,
        note,
        idempotency_key: idempotencyKey,
      });
      if (res.ok) {
        trackDiscipline("hrms_violation_record", "success");
        onDone(res.row);
      } else {
        trackDiscipline("hrms_violation_record", "error", res.code);
        setError(res.message || t("action.failed"));
      }
    });
  };

  return (
    <section className="card dsc-form" data-testid="violations-form">
      <div className="hd">
        <h3>{t("form.title")}</h3>
      </div>
      <div className="bd dsc-form-grid">
        <label className="fld">
          <span className="small muted">{t("form.person")}</span>
          <select className="inp" value={personId} onChange={(e) => setPersonId(e.target.value)} data-testid="violations-form-person">
            <option value="">{t("form.person_choose")}</option>
            {page.people.map((p) => (
              <option key={p.person_id} value={p.person_id}>
                {[p.name, p.designation, p.park_label].filter(Boolean).join(" · ")}
              </option>
            ))}
          </select>
        </label>
        <label className="fld">
          <span className="small muted">{t("form.type")}</span>
          <select className="inp" value={typeKey} onChange={(e) => pickType(e.target.value)} data-testid="violations-form-type">
            <option value="">{t("form.type_choose")}</option>
            {page.types.map((x) => (
              <option key={x.key} value={x.key}>
                {`${x.title} · ${x.default_fine_label}`}
              </option>
            ))}
          </select>
        </label>
        <div className="fld">
          <ThemedDatePicker
            name="occurred_on"
            label={t("form.date")}
            max={todayKey()}
            value={date}
            onChange={setDate}
            previousMonthLabel={t("form.prev_month")}
            nextMonthLabel={t("form.next_month")}
            invalidDateText={t("form.invalid_date")}
          />
        </div>
        <label className="fld">
          <span className="small muted">{t("form.fine")}</span>
          <input className="inp" inputMode="numeric" value={fine} onChange={(e) => setFine(e.target.value.replace(/[^0-9]/g, ""))} data-testid="violations-form-fine" />
        </label>
        <label className="fld dsc-form-note">
          <span className="small muted">{t("form.note")}</span>
          <textarea className="inp" rows={2} maxLength={2000} value={note} placeholder={t("form.note_hint")} onChange={(e) => setNote(e.target.value)} data-testid="violations-form-note" />
        </label>
        <div className="dsc-form-actions">
          {error ? <div className="small tt-status dng" role="alert">{error}</div> : null}
          <button type="button" className="btn" disabled={pending} onClick={onCancel}>
            {t("action.cancel")}
          </button>
          <button type="button" className="btn primary" disabled={pending} onClick={save} data-testid="violations-form-save">
            {pending ? t("action.saving") : t("action.save")}
          </button>
        </div>
      </div>
    </section>
  );
}
