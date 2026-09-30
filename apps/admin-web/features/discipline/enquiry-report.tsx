"use client";

import { useEffect, useState, useTransition } from "react";
import { Plus, X } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { EnquiryDetail } from "@/lib/api/server";
import { submitEnquiryAction } from "./actions";
import { trackDiscipline } from "./telemetry";

type Penalty = { personId: string; typeKey: string; fine: string; note: string };

/**
 * An enquiry's report (maintainer decisions 2026-09-30). Open: answer the questions its PINNED HRMS
 * SOP version asks, add each person responsible with a violation (the fine starts at the type's
 * default), or nobody, and submit -- the violations are recorded with the report. Submitted: what
 * was answered and who was penalised, read only. Every word is backend copy or a composed field.
 */
export function EnquiryReport({
  pageContract,
  initial,
  canSubmit,
  submitReason,
}: {
  pageContract: AdminUiPageContract;
  initial: EnquiryDetail;
  canSubmit: boolean;
  submitReason: string;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [detail, setDetail] = useState<EnquiryDetail>(initial);
  const [answers, setAnswers] = useState<Record<string, string | boolean>>({});
  const [penalties, setPenalties] = useState<Penalty[]>([]);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [pending, startTransition] = useTransition();
  // A refusal is about the report as it was sent; editing the report clears it.
  useEffect(() => setError(""), [answers, penalties]);
  const e = detail.enquiry;
  const open = detail.can_submit;

  const setPenalty = (i: number, patch: Partial<Penalty>) => setPenalties((ps) => ps.map((p, j) => (j === i ? { ...p, ...patch } : p)));
  // The violation and its fine are separate choices (maintainer, 2026-09-30): picking one never
  // fills in the other.
  const pickType = (i: number, key: string) => setPenalty(i, { typeKey: key });

  const submit = () => {
    setError("");
    startTransition(async () => {
      const res = await submitEnquiryAction(e.enquiry_id, {
        answers,
        penalties: penalties.map((p) => {
          const n = Number.parseInt(p.fine, 10);
          return { person_id: p.personId, type_key: p.typeKey, fine_rupees: Number.isFinite(n) ? n : null, note: p.note };
        }),
        row_version: e.row_version,
      });
      if (res.ok) {
        trackDiscipline("hrms_enquiry_submit", "success");
        setDetail(res.row);
        setDone(true);
      } else {
        trackDiscipline("hrms_enquiry_submit", "error", res.code);
        setError(res.message || t("action.failed"));
      }
    });
  };

  return (
    <div className="dsc-enquiry">
      <section className="card" data-testid="enquiry-head">
        <div className="hd">
          <h3>{e.title}</h3>
          <span className={e.status === "submitted" ? "tag t-ok" : e.overdue ? "tag t-dng" : "tag t-warn"} data-testid="enquiry-status">
            {e.status_label}
          </span>
        </div>
        <div className="bd dsc-facts">
          {e.subject_label ? (
            <div>
              <b>{e.subject_label}</b>
            </div>
          ) : null}
          <div className="small muted">{e.park_label}</div>
          <div className="dsc-fact-row">
            <span>
              <span className="muted small">{t("detail.happened")}</span> {e.occurred_at_label}
            </span>
            <span>
              <span className="muted small">{t("detail.opened")}</span> {e.opened_at_label}
            </span>
            <span className={e.overdue ? "dsc-overdue" : undefined}>
              <span className="muted small">{t("detail.due")}</span> {e.due_at_label}
            </span>
          </div>
          {e.status === "submitted" ? (
            <div className="small" data-testid="enquiry-submitted-by">
              {t("detail.submitted").replace("%s", e.submitted_by_name).replace("%s", e.submitted_at_label)}
            </div>
          ) : null}
        </div>
      </section>

      {done ? (
        <div className="note" role="status" data-testid="enquiry-done">
          <span className="tag t-ok">{t("action.submitted")}</span> {e.penalty_label}
        </div>
      ) : null}

      <section className="card" data-testid="enquiry-questions">
        <div className="hd">
          <h3>{t("detail.report")}</h3>
        </div>
        <div className="bd dsc-questions">
          {detail.questions.map((q) => {
            const stored = detail.answers[q.id];
            return (
              <div className="fld" key={q.id} data-testid="enquiry-question">
                <span className="dsc-q-title">
                  {q.title} {q.required ? <span className="small muted">· {t("detail.required")}</span> : null}
                </span>
                {!open ? (
                  <div className="dsc-answer">{typeof stored === "boolean" ? (stored ? t("detail.yes") : t("detail.no")) : String(stored ?? "—")}</div>
                ) : q.kind === "yes_no" ? (
                  <div className="dsc-yesno" role="radiogroup" aria-label={q.title}>
                    {[true, false].map((v) => (
                      <label key={String(v)} className="dsc-radio">
                        <input type="radio" name={`q-${q.id}`} checked={answers[q.id] === v} onChange={() => setAnswers((a) => ({ ...a, [q.id]: v }))} />
                        <span>{v ? t("detail.yes") : t("detail.no")}</span>
                      </label>
                    ))}
                  </div>
                ) : (
                  <textarea
                    className="inp"
                    rows={3}
                    maxLength={2000}
                    value={typeof answers[q.id] === "string" ? (answers[q.id] as string) : ""}
                    onChange={(ev) => setAnswers((a) => ({ ...a, [q.id]: ev.target.value }))}
                    aria-label={q.title}
                    data-testid={`enquiry-answer-${q.id}`}
                  />
                )}
              </div>
            );
          })}
        </div>
      </section>

      <section className="card" data-testid="enquiry-responsible">
        <div className="hd">
          <h3>{open ? t("detail.responsible") : t("detail.recorded")}</h3>
          {open ? (
            <>
              <div className="sp" style={{ flex: 1 }} />
              <span className="muted small">{t("detail.responsible_hint")}</span>
            </>
          ) : null}
        </div>
        <div className="bd dsc-penalties">
          {!open ? (
            detail.violations.length === 0 ? (
              <div className="small muted">{t("detail.nobody")}</div>
            ) : (
              detail.violations.map((v) => (
                <div className="dsc-penalty-read" key={v.violation_id} data-testid="enquiry-violation">
                  <b>{v.person_name}</b> · {v.type_label} · <b>{v.fine_label}</b>
                  {v.note ? <div className="small muted">{v.note}</div> : null}
                </div>
              ))
            )
          ) : detail.types.length === 0 ? (
            <div className="small muted">{t("detail.no_types")}</div>
          ) : (
            <>
              {penalties.map((p, i) => (
                <div className="dsc-penalty" key={i} data-testid="enquiry-penalty">
                  <label className="fld">
                    <span className="small muted">{t("detail.person")}</span>
                    <select className="inp" value={p.personId} onChange={(ev) => setPenalty(i, { personId: ev.target.value })} data-testid="enquiry-penalty-person">
                      <option value="">{t("detail.person_choose")}</option>
                      {detail.people.map((person) => (
                        <option key={person.person_id} value={person.person_id}>
                          {[person.name, person.designation].filter(Boolean).join(" · ")}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="fld">
                    <span className="small muted">{t("detail.type")}</span>
                    <select className="inp" value={p.typeKey} onChange={(ev) => pickType(i, ev.target.value)} data-testid="enquiry-penalty-type">
                      <option value="">{t("detail.type_choose")}</option>
                      {detail.types.map((x) => (
                        <option key={x.key} value={x.key}>
                          {x.title}
                        </option>
                      ))}
                    </select>
                  </label>
                  <label className="fld dsc-fine">
                    <span className="small muted">{t("detail.fine")}</span>
                    <input className="inp" inputMode="numeric" value={p.fine} placeholder={t("detail.fine_none")} onChange={(ev) => setPenalty(i, { fine: ev.target.value.replace(/[^0-9]/g, "") })} />
                  </label>
                  <label className="fld dsc-grow">
                    <span className="small muted">{t("detail.note")}</span>
                    <input className="inp" maxLength={2000} value={p.note} onChange={(ev) => setPenalty(i, { note: ev.target.value })} />
                  </label>
                  <button type="button" className="btn sm" onClick={() => setPenalties((ps) => ps.filter((_, j) => j !== i))}>
                    <X className="ic" /> {t("detail.remove")}
                  </button>
                </div>
              ))}
              <div>
                <button type="button" className="btn" onClick={() => setPenalties((ps) => [...ps, { personId: "", typeKey: "", fine: "", note: "" }])} data-testid="enquiry-add-person">
                  <Plus className="ic" /> {t("detail.add_person")}
                </button>
              </div>
            </>
          )}
        </div>
      </section>

      {open ? (
        <div className="dsc-submit">
          {error ? (
            <div className="small tt-status dng" role="alert" data-testid="enquiry-error">
              {error}
            </div>
          ) : null}
          <button type="button" className="btn primary" disabled={!canSubmit || pending} title={canSubmit ? undefined : submitReason} onClick={submit} data-testid="enquiry-submit">
            {pending ? t("action.submitting") : t("action.submit")}
          </button>
        </div>
      ) : null}
    </div>
  );
}
