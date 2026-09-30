"use client";

import { useMemo, useState, useTransition } from "react";
import { Plus, ShieldAlert } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { Violation, ViolationsPage } from "@/lib/api/server";
import { closeViolationAction, keepViolationAction, recordViolationAction, withdrawViolationAction } from "./actions";
import { applyViolationChange, type ViolationChange } from "./totals";
import { todayKey } from "./format";
import { trackDiscipline, type DisciplineEvent } from "./telemetry";

/**
 * People / HRMS > Violations, the interactive half (maintainer decisions 2026-09-30). A recorded,
 * withdrawn, kept or closed violation comes back from the server and is put in place -- the list,
 * the person's totals and the page totals move together, with no page reload. An automatic
 * clock-in violation WAITS for HR, who keeps it (with a fine, or none) or closes it with a reason.
 * Every word is backend copy or a backend-composed field.
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
  const [totals, setTotals] = useState({ summary: page.summary, byPerson: page.by_person });
  const [formOpen, setFormOpen] = useState(false);

  // The tiles and the per-person row move on every tab: the tiles count recorded violations
  // whatever tab is open, and the person row carries every status in its own column.
  const apply = (v: Violation, change: ViolationChange) => setTotals((cur) => applyViolationChange(cur, v, change));

  const replaceRow = (v: Violation) =>
    setItems((prev) => (page.status && page.status !== v.status ? prev.filter((p) => p.violation_id !== v.violation_id) : prev.map((p) => (p.violation_id === v.violation_id ? v : p))));

  const onRecorded = (v: Violation) => {
    // Only a violation inside this page's period joins it; the totals follow the list.
    const inPeriod = page.period === "all" || v.occurred_on.startsWith(page.period === "year" ? page.month.slice(0, 4) : page.month);
    if (inPeriod && (page.status === "" || page.status === "recorded")) {
      setItems((prev) => [v, ...prev]);
      apply(v, "recorded");
    }
    setFormOpen(false);
  };

  const listLabels = tableLabels(pageContract, "violations");
  const personLabels = tableLabels(pageContract, "violation-people");
  const { summary, byPerson } = totals;

  return (
    <>
      <div className="dsc-tiles">
        {[
          { key: "count", label: t("summary.count"), value: String(summary.count) },
          { key: "fines", label: t("summary.fines"), value: summary.fine_label },
          { key: "people", label: t("summary.people"), value: String(summary.people) },
          { key: "pending", label: t("summary.pending"), value: String(summary.pending) },
        ].map((tile) => (
          <div className={tile.key === "pending" && summary.pending > 0 ? "card dsc-tile dsc-tile-warn" : "card dsc-tile"} key={tile.key} data-testid={`violations-tile-${tile.key}`}>
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
          <h3>
            {t("people.title")} · {page.period_label}
          </h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small dsc-hint">{t("people.hint")}</span>
        </div>
        {byPerson.length === 0 ? (
          <div className="empty">{t("people.empty")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={t("people.title")}>
            <table className="people-table dsc-rtable">
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
                    <td className="dsc-rt-head">
                      <b>{p.person_name}</b>
                      {p.designation ? <div className="small muted">{p.designation}</div> : null}
                    </td>
                    <td data-label={personLabels[1]}>{p.park_label}</td>
                    <td data-label={personLabels[2]}>{p.count}</td>
                    <td data-label={personLabels[3]}>
                      <b>{p.fine_label}</b>
                    </td>
                    <td data-label={personLabels[4]}>{p.pending > 0 ? <span className="tag t-warn">{p.pending}</span> : <span className="muted">0</span>}</td>
                    <td data-label={personLabels[5]}>{p.closed}</td>
                    <td data-label={personLabels[6]}>{p.leave_label || <span className="muted">—</span>}</td>
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
            <table className="people-table dsc-rtable dsc-list-table">
              <thead>
                <tr>
                  {listLabels.map((l) => (
                    <th key={l}>{l}</th>
                  ))}
                  <th>{t("column.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {items.map((v) => (
                  <ViolationRow
                    key={v.violation_id}
                    pageContract={pageContract}
                    violation={v}
                    labels={listLabels}
                    canEdit={canEdit}
                    editReason={editReason}
                    onChanged={(row, change) => {
                      replaceRow(row);
                      apply(row, change);
                    }}
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

type Asking = "" | "withdraw" | "keep" | "close";

function ViolationRow({
  pageContract,
  violation: v,
  labels,
  canEdit,
  editReason,
  onChanged,
}: {
  pageContract: AdminUiPageContract;
  violation: Violation;
  labels: string[];
  canEdit: boolean;
  editReason: string;
  onChanged: (v: Violation, change: ViolationChange) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [asking, setAsking] = useState<Asking>("");
  const [text, setText] = useState("");
  const [fine, setFine] = useState("");
  const [error, setError] = useState("");
  const [pending, startTransition] = useTransition();
  const muted = v.status === "withdrawn" || v.status === "closed";
  const waiting = v.status === "pending";

  const open = (a: Asking) => {
    setAsking(a);
    setText("");
    setFine("");
    setError("");
  };
  const run = (event: DisciplineEvent, call: () => Promise<{ ok: true; row: Violation } | { ok: false; code: string; message: string }>, change: ViolationChange) =>
    startTransition(async () => {
      const res = await call();
      if (res.ok) {
        trackDiscipline(event, "success");
        setAsking("");
        onChanged(res.row, change);
      } else {
        trackDiscipline(event, "error", res.code);
        setError(res.message || t("action.failed"));
      }
    });
  const confirm = () => {
    if (asking === "withdraw") run("hrms_violation_withdraw", () => withdrawViolationAction(v.violation_id, text, v.row_version), "withdrawn");
    if (asking === "close") run("hrms_violation_close", () => closeViolationAction(v.violation_id, text, v.row_version), "closed");
    if (asking === "keep") {
      const n = Number.parseInt(fine, 10);
      run("hrms_violation_keep", () => keepViolationAction(v.violation_id, Number.isFinite(n) ? n : null, text, v.row_version), "kept");
    }
  };
  const decided = v.decided_by_name ? t("action.decided_by").replace("%s", v.decided_by_name).replace("%s", v.decided_at_label) : "";

  const cols = labels.length + 1;
  return (
    <>
      <tr className={muted ? "dsc-withdrawn" : undefined} data-testid="violation-row">
        <td data-label={labels[0]}>{v.occurred_on_label}</td>
        <td className="dsc-rt-head">
          <b>{v.person_name}</b>
          {v.designation ? <div className="small muted">{v.designation}</div> : null}
        </td>
        <td data-label={labels[2]}>
          {v.type_label}
          {/* An automatic one says what happened; "Clock-in check" already reads in Recorded by. */}
          {v.detail ? <div className="small dsc-detail">{v.detail}</div> : <div className="small muted">{v.source_label}</div>}
          {v.note ? <div className="small dsc-note">{v.note}</div> : null}
        </td>
        <td data-label={labels[3]}>
          <b>{v.fine_label}</b>
        </td>
        <td data-label={labels[4]}>
          {v.recorded_by_name || <span className="muted">{v.source_label}</span>}
          <div className="small muted">{v.recorded_at_label}</div>
        </td>
        <td data-label={labels[5]}>
          <span className={muted ? "tag t-mut" : waiting ? "tag t-info" : "tag t-warn"} data-testid="violation-status">
            {v.status_label}
          </span>
          {v.withdraw_reason ? <div className="small muted">{v.withdraw_reason}</div> : null}
          {decided ? <div className="small muted">{decided}</div> : null}
          {v.status === "closed" && v.decision_note ? <div className="small muted">{v.decision_note}</div> : null}
        </td>
        <td className="dsc-rt-actions">
          {muted || asking ? null : waiting ? (
            <div className="dsc-actions">
              <button type="button" className="btn sm primary" disabled={!canEdit} title={canEdit ? undefined : editReason} onClick={() => open("keep")} data-testid="violation-keep">
                {t("action.keep")}
              </button>
              <button type="button" className="btn sm" disabled={!canEdit} title={canEdit ? undefined : editReason} onClick={() => open("close")} data-testid="violation-close">
                {t("action.close")}
              </button>
            </div>
          ) : (
            <button type="button" className="btn sm" disabled={!canEdit} title={canEdit ? undefined : editReason} onClick={() => open("withdraw")} data-testid="violation-withdraw">
              {t("action.withdraw")}
            </button>
          )}
        </td>
      </tr>
      {asking ? (
        <tr className="dsc-editor-row">
          <td colSpan={cols}>
            {/* The decision opens as a full-width row under the violation: nothing is squeezed into a cell. */}
            <div className="dsc-decide" data-testid={`violation-${asking}-form`}>
              {asking === "keep" ? (
                <label className="fld dsc-f-fine">
                  <span className="small muted">{t("form.fine")}</span>
                  <input className="inp" inputMode="numeric" value={fine} placeholder={t("form.fine_none")} onChange={(e) => setFine(e.target.value.replace(/[^0-9]/g, ""))} data-testid="violation-keep-fine" />
                </label>
              ) : null}
              <label className="fld dsc-f-note">
                <span className="small muted">{asking === "keep" ? t("form.note") : asking === "close" ? t("action.close_reason") : t("action.withdraw_reason")}</span>
                <input className="inp" value={text} maxLength={asking === "keep" ? 2000 : 500} onChange={(e) => setText(e.target.value)} data-testid={`violation-${asking}-text`} />
              </label>
              <div className="dsc-f-actions">
                <button
                  type="button"
                  className={asking === "keep" ? "btn primary" : "btn dng"}
                  disabled={pending || (asking !== "keep" && !text.trim())}
                  onClick={confirm}
                  data-testid={`violation-${asking}-confirm`}
                >
                  {asking === "keep" ? t("action.keep_confirm") : asking === "close" ? t("action.close_confirm") : t("action.withdraw_confirm")}
                </button>
                <button type="button" className="btn" disabled={pending} onClick={() => open("")}>
                  {t("action.cancel")}
                </button>
              </div>
              {error ? <div className="small tt-status dng">{error}</div> : null}
            </div>
          </td>
        </tr>
      ) : null}
    </>
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

  // One line: who, what, when, how much, why, and the two buttons. The violation and the fine are
  // separate choices (maintainer, 2026-09-30) -- picking a violation never fills in money.
  return (
    <section className="card dsc-form" data-testid="violations-form" aria-label={t("form.title")}>
      <div className="bd">
        <div className="dsc-form-row">
          <label className="fld dsc-f-person">
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
          <label className="fld dsc-f-type">
            <span className="small muted">{t("form.type")}</span>
            <select className="inp" value={typeKey} onChange={(e) => setTypeKey(e.target.value)} data-testid="violations-form-type">
              <option value="">{t("form.type_choose")}</option>
              {page.types.map((x) => (
                <option key={x.key} value={x.key}>
                  {x.title}
                </option>
              ))}
            </select>
          </label>
          <div className="fld dsc-f-date">
            <span className="small muted">{t("form.date")}</span>
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
          <label className="fld dsc-f-fine">
            <span className="small muted">{t("form.fine")}</span>
            <input className="inp" inputMode="numeric" value={fine} placeholder={t("form.fine_none")} onChange={(e) => setFine(e.target.value.replace(/[^0-9]/g, ""))} data-testid="violations-form-fine" />
          </label>
          <label className="fld dsc-f-note">
            <span className="small muted">{t("form.note")}</span>
            <input className="inp" maxLength={2000} value={note} placeholder={t("form.note_hint")} onChange={(e) => setNote(e.target.value)} data-testid="violations-form-note" />
          </label>
          <div className="dsc-f-actions">
            <button type="button" className="btn" disabled={pending} onClick={onCancel}>
              {t("action.cancel")}
            </button>
            <button type="button" className="btn primary" disabled={pending} onClick={save} data-testid="violations-form-save">
              {pending ? t("action.saving") : t("action.save")}
            </button>
          </div>
        </div>
        {error ? (
          <div className="small tt-status dng" role="alert">
            {error}
          </div>
        ) : null}
      </div>
    </section>
  );
}
