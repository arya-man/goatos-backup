"use client";

// HRMS SOP editor (maintainer instruction 2026-09-30: "every violation type, everything is SOP
// driven; in future every list should be changeable"). HR and the CEO add, rename and retire
// violation types and change their default fines, and change what each enquiry asks and its
// deadline. The backend validates the document at save and names any problem; publishing makes it
// the list new violations and new enquiries use. Every word is page-contract copy.

import { useState, useTransition } from "react";
import Link from "@/components/no-prefetch-link";
import { useRouter } from "next/navigation";
import { AlertTriangle, ChevronLeft, Plus, X } from "lucide-react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { publishHrmsVersion, saveHrmsVersion, type HrmsSaveResult } from "./sop-actions";
import { publishedHref } from "./published-href";
import { emitHrms, freeTriggers, type HrmsAttendanceRow, type HrmsEnquiryRow, type HrmsQuestionRow, type HrmsRows, type HrmsTypeRow } from "./hrms-model";

export function HrmsEditor({
  pageContract: pc,
  basePath,
  sopId,
  sopName,
  versionLabel,
  initial,
}: {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  versionLabel: string;
  initial: HrmsRows;
}) {
  const router = useRouter();
  const t = (key: string) => copy(pc, key);
  const [rows, setRows] = useState<HrmsRows>(initial);
  const [result, setResult] = useState<HrmsSaveResult | null>(null);
  const [pending, startTransition] = useTransition();

  const setType = (i: number, patch: Partial<HrmsTypeRow>) =>
    setRows((r) => ({ ...r, types: r.types.map((row, j) => (j === i ? { ...row, ...patch } : row)) }));
  const setEnquiry = (i: number, patch: Partial<HrmsEnquiryRow>) =>
    setRows((r) => ({ ...r, enquiries: r.enquiries.map((row, j) => (j === i ? { ...row, ...patch } : row)) }));
  const setQuestion = (e: number, q: number, patch: Partial<HrmsQuestionRow>) =>
    setEnquiry(e, { questions: rows.enquiries[e].questions.map((row, j) => (j === q ? { ...row, ...patch } : row)) });

  function submit(publish: boolean) {
    const doc = emitHrms(rows);
    startTransition(async () => {
      const res = publish ? await publishHrmsVersion(sopId, doc) : await saveHrmsVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  const spare = freeTriggers(rows);
  const setAttendance = (patch: Partial<HrmsAttendanceRow>) =>
    setRows((r) => ({ ...r, attendance: { ...(r.attendance ?? { graceMinutes: "15", lateType: "", absentType: "" }), ...patch } }));
  // Only types already saved can be named by the check: a new row gets its key when it is saved.
  const namedTypes = rows.types.filter((x) => x.stored && x.key);

  return (
    <div className="screen on sop-inspection hsop">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pc, "crumb")} · {pc.title} · <b>{sopName}</b>
          </div>
          <h1>{t("hsop.title")}</h1>
          <div className="sub">{t("hsop.subtitle")}</div>
          <div className="muted small" style={{ marginTop: 4 }}>
            {versionLabel}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link className="btn" href={basePath}>
          <ChevronLeft className="ic" /> {t("builder.back")}
        </Link>
      </div>

      {result ? (
        <div className={result.ok ? "note" : "alert warn"} style={{ marginBottom: 12 }} data-testid="hsop-result">
          {result.ok ? <span className="tag t-ok">{t("modal.builder.notice_ok")}</span> : <AlertTriangle className="ic" />} {result.message}
          {result.report && !result.report.valid ? (
            <ul className="small" style={{ margin: "6px 0 0 16px" }}>
              {result.report.errors.slice(0, 8).map((e, i) => (
                <li key={i}>{e.message}</li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}

      <section className="card" data-testid="hsop-types">
        <div className="hd">
          <h3>{t("hsop.types.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{t("hsop.types.hint")}</span>
        </div>
        <div className="bd hsop-list">
          {rows.types.length === 0 ? <div className="empty">{t("hsop.types.empty")}</div> : null}
          {rows.types.map((row, i) => (
            <div className={row.active ? "hsop-row" : "hsop-row retired"} key={`${row.key || "new"}-${i}`} data-testid="hsop-type-row">
              <label className="fld hsop-grow">
                <span className="small muted">{t("hsop.types.name")}</span>
                <input className="inp" value={row.title} maxLength={80} onChange={(e) => setType(i, { title: e.target.value })} />
              </label>
              <label className="hsop-check">
                <input type="checkbox" checked={row.active} onChange={(e) => setType(i, { active: e.target.checked })} />
                <span>{row.active ? t("hsop.types.active") : t("hsop.types.retired")}</span>
              </label>
              {row.stored ? null : (
                <button type="button" className="btn sm" onClick={() => setRows((r) => ({ ...r, types: r.types.filter((_, j) => j !== i) }))} aria-label={t("hsop.types.remove")}>
                  <X className="ic" /> {t("hsop.types.remove")}
                </button>
              )}
            </div>
          ))}
          <div>
            <button
              type="button"
              className="btn"
              data-testid="hsop-add-type"
              onClick={() => setRows((r) => ({ ...r, types: [...r.types, { key: "", title: "", active: true, stored: false }] }))}
            >
              <Plus className="ic" /> {t("hsop.types.add")}
            </button>
          </div>
        </div>
      </section>

      <section className="card" data-testid="hsop-enquiries">
        <div className="hd">
          <h3>{t("hsop.enquiries.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{t("hsop.enquiries.hint")}</span>
        </div>
        <div className="bd hsop-list">
          {rows.enquiries.map((e, ei) => (
            <div className="hsop-enquiry" key={e.trigger} data-testid="hsop-enquiry">
              <div className="hsop-row">
                <div className="fld hsop-grow">
                  <span className="small muted">{t("hsop.enquiries.event")}</span>
                  <b>{copy(pc, `hsop.event.${e.trigger}`, e.trigger)}</b>
                </div>
                <label className="fld hsop-grow">
                  <span className="small muted">{t("hsop.enquiries.name")}</span>
                  <input className="inp" value={e.title} maxLength={80} onChange={(ev) => setEnquiry(ei, { title: ev.target.value })} />
                </label>
                <label className="fld hsop-num">
                  <span className="small muted">{t("hsop.enquiries.due")}</span>
                  <input className="inp" inputMode="numeric" value={e.dueHours} data-testid="hsop-due" onChange={(ev) => setEnquiry(ei, { dueHours: ev.target.value.replace(/[^0-9]/g, "") })} />
                </label>
                <button type="button" className="btn sm" onClick={() => setRows((r) => ({ ...r, enquiries: r.enquiries.filter((_, j) => j !== ei) }))}>
                  <X className="ic" /> {t("hsop.enquiries.remove")}
                </button>
              </div>
              <div className="small muted hsop-sub">{t("hsop.questions.title")}</div>
              {e.questions.map((q, qi) => (
                <div className="hsop-row hsop-question" key={`${q.id || "new"}-${qi}`} data-testid="hsop-question">
                  <label className="fld hsop-grow">
                    <span className="small muted">{t("hsop.questions.text")}</span>
                    <input className="inp" value={q.title} maxLength={200} onChange={(ev) => setQuestion(ei, qi, { title: ev.target.value })} />
                  </label>
                  <label className="fld hsop-num">
                    <span className="small muted">{t("hsop.questions.kind")}</span>
                    <select className="inp" value={q.kind} onChange={(ev) => setQuestion(ei, qi, { kind: ev.target.value === "yes_no" ? "yes_no" : "text" })}>
                      <option value="text">{t("hsop.questions.kind.text")}</option>
                      <option value="yes_no">{t("hsop.questions.kind.yes_no")}</option>
                    </select>
                  </label>
                  <label className="hsop-check">
                    <input type="checkbox" checked={q.required} onChange={(ev) => setQuestion(ei, qi, { required: ev.target.checked })} />
                    <span>{t("hsop.questions.required")}</span>
                  </label>
                  <button type="button" className="btn sm" onClick={() => setEnquiry(ei, { questions: e.questions.filter((_, j) => j !== qi) })}>
                    <X className="ic" /> {t("hsop.questions.remove")}
                  </button>
                </div>
              ))}
              <div>
                <button
                  type="button"
                  className="btn sm"
                  data-testid="hsop-add-question"
                  onClick={() => setEnquiry(ei, { questions: [...e.questions, { id: "", kind: "text", title: "", required: false, stored: false }] })}
                >
                  <Plus className="ic" /> {t("hsop.questions.add")}
                </button>
              </div>
            </div>
          ))}
          {spare.length > 0 ? (
            <div>
              <button
                type="button"
                className="btn"
                onClick={() => setRows((r) => ({ ...r, enquiries: [...r.enquiries, { trigger: spare[0], title: "", dueHours: "48", questions: [] }] }))}
              >
                <Plus className="ic" /> {t("hsop.enquiries.add")}
              </button>
            </div>
          ) : null}
        </div>
      </section>

      <section className="card" data-testid="hsop-attendance">
        <div className="hd">
          <h3>{t("hsop.attendance.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{t("hsop.attendance.hint")}</span>
        </div>
        <div className="bd hsop-list">
          <div className="hsop-row">
            <label className="fld hsop-num">
              <span className="small muted">{t("hsop.attendance.grace")}</span>
              <input className="inp" inputMode="numeric" value={rows.attendance?.graceMinutes ?? ""} onChange={(e) => setAttendance({ graceMinutes: e.target.value.replace(/[^0-9]/g, "") })} data-testid="hsop-grace" />
            </label>
            <label className="fld hsop-grow">
              <span className="small muted">{t("hsop.attendance.late")}</span>
              <select className="inp" value={rows.attendance?.lateType ?? ""} onChange={(e) => setAttendance({ lateType: e.target.value })} data-testid="hsop-late-type">
                <option value="">{t("hsop.attendance.off")}</option>
                {namedTypes.map((x) => (
                  <option key={x.key} value={x.key}>
                    {x.title}
                  </option>
                ))}
              </select>
            </label>
            <label className="fld hsop-grow">
              <span className="small muted">{t("hsop.attendance.absent")}</span>
              <select className="inp" value={rows.attendance?.absentType ?? ""} onChange={(e) => setAttendance({ absentType: e.target.value })} data-testid="hsop-absent-type">
                <option value="">{t("hsop.attendance.off")}</option>
                {namedTypes.map((x) => (
                  <option key={x.key} value={x.key}>
                    {x.title}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
      </section>

      <div className="hsop-actions">
        <button type="button" className="btn" disabled={pending} onClick={() => submit(false)} data-testid="hsop-save">
          {pending ? t("hsop.saving") : t("hsop.save")}
        </button>
        <button type="button" className="btn primary" disabled={pending} onClick={() => submit(true)} data-testid="hsop-publish">
          {pending ? t("hsop.saving") : t("hsop.publish")}
        </button>
      </div>
    </div>
  );
}
