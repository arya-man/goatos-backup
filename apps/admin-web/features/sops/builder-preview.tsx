"use client";

import { useMemo, useState } from "react";
import { Camera, Check, ChevronDown, RotateCcw, ScanLine, Video } from "lucide-react";
import { fieldConfigKind, type BuilderCondition, type BuilderStep } from "./sop-derive";
import { copy, optionalOptionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";

type Answer = string | string[] | undefined;

function isEmpty(v: Answer): boolean {
  return v === undefined || v === "" || (Array.isArray(v) && v.length === 0);
}

// Client mirror of the backend condition evaluator (sop/app/service.go conditionMatches), enough to make
// the preview behave: a conditionally-shown question appears/disappears as its referenced answer changes.
// Select/multiselect answers are the chosen labels, so the author's typed condition value matches by label.
function conditionMet(cond: BuilderCondition, steps: BuilderStep[], answers: Record<string, Answer>): boolean {
  const refIdx = steps.findIndex((s) => s.id === cond.refId);
  if (refIdx < 0) return true;
  const a = answers[cond.refId];
  switch (cond.operator) {
    case "answered":
      return !isEmpty(a);
    case "not_answered":
      return isEmpty(a);
    case "equals":
      return String(a ?? "") === cond.value;
    case "not_equals":
      return String(a ?? "") !== cond.value;
    case "is_one_of":
      return cond.value.split(",").map((x) => x.trim()).includes(String(a ?? ""));
    case "gt":
      return Number(a) > Number(cond.value);
    case "gte":
      return Number(a) >= Number(cond.value);
    case "lt":
      return Number(a) < Number(cond.value);
    case "lte":
      return Number(a) <= Number(cond.value);
    default:
      return true;
  }
}

// Interactive operator preview of the form being authored — updated live as questions change, and
// FILLABLE: type/select/toggle answers and conditional questions reveal or hide exactly as they will for
// a real operator. Purely local (no save, no backend); pickers/scan/proof render as disabled stubs.
export function BuilderPreview({ pc, steps }: { pc: AdminUiPageContract; steps: BuilderStep[] }) {
  const yesNo = optionalOptionGroup(pc, "yes_no");
  const [answers, setAnswers] = useState<Record<string, Answer>>({});
  const [files, setFiles] = useState<Record<string, string>>({});
  const setFile = (id: string, name: string | undefined) => setFiles((prev) => ({ ...prev, [id]: name ?? "" }));

  const setAnswer = (id: string, value: Answer) => setAnswers((prev) => ({ ...prev, [id]: value }));
  const toggleMulti = (id: string, value: string) =>
    setAnswers((prev) => {
      const cur = Array.isArray(prev[id]) ? (prev[id] as string[]) : [];
      return { ...prev, [id]: cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value] };
    });

  // A question shows when it has no condition, or its condition (against an EARLIER answer) is met.
  const visibleIds = useMemo(() => {
    const set = new Set<string>();
    steps.forEach((s, i) => {
      const cond = s.visibleWhen;
      const refIdx = cond ? steps.findIndex((x) => x.id === cond.refId) : -1;
      const show = !cond || refIdx < 0 || refIdx >= i ? true : conditionMet(cond, steps, answers);
      if (show) set.add(s.id);
    });
    return set;
  }, [steps, answers]);

  if (steps.length === 0) {
    return <div className="note">{copy(pc, "builder.preview.empty")}</div>;
  }

  const visibleSteps = steps.filter((s) => visibleIds.has(s.id));

  return (
    <div className="pvwrap">
      <div className="pvbar">
        <span className="muted small">{copy(pc, "builder.preview.subtitle")}</span>
        <span className="sp" style={{ flex: 1 }} />
        <button
          type="button"
          className="btn sm ghost"
          onClick={() => {
            setAnswers({});
            setFiles({});
          }}
          disabled={Object.keys(answers).length === 0 && Object.keys(files).length === 0}
        >
          <RotateCcw className="ic" style={{ width: 12 }} /> {copy(pc, "builder.preview.reset")}
        </button>
      </div>
      <div className="pvform">
        {visibleSteps.map((step) => {
          const kind = fieldConfigKind(step.type);
          const answer = answers[step.id];
          const num = steps.findIndex((s) => s.id === step.id) + 1;
          const opts = step.options.filter((o) => o.label.trim());
          return (
            <div className="pvfield" key={step.id}>
              <div className="pvlabel">
                <span className="pvnum">{num}</span>
                <span className="pvq">{step.label.trim() || `${copy(pc, "builder.question_label")} ${num}`}</span>
                {step.required ? <span className="tag t-warn">{copy(pc, "builder.preview.required_badge")}</span> : null}
                {step.visibleWhen ? <span className="tag t-info">{copy(pc, "builder.preview.conditional_badge")}</span> : null}
              </div>
              {step.helpText.trim() ? <div className="muted small pvhelp">{step.helpText}</div> : null}

              {kind === "text" ? (
                step.longText ? (
                  <textarea
                    className="pvctl"
                    rows={2}
                    placeholder={step.placeholder}
                    value={(answer as string) ?? ""}
                    onChange={(e) => setAnswer(step.id, e.target.value)}
                  />
                ) : (
                  <input
                    className="pvctl"
                    placeholder={step.placeholder}
                    value={(answer as string) ?? ""}
                    onChange={(e) => setAnswer(step.id, e.target.value)}
                  />
                )
              ) : null}

              {kind === "number" ? (
                <div className="pvnumrow">
                  <input
                    className="pvctl"
                    type="number"
                    value={(answer as string) ?? ""}
                    onChange={(e) => setAnswer(step.id, e.target.value)}
                  />
                  {step.unit.trim() ? <span className="pvunit">{step.unit}</span> : null}
                </div>
              ) : null}

              {kind === "boolean" ? (
                <div className="chipset">
                  {yesNo.map((o) => (
                    <button
                      key={o.key}
                      type="button"
                      className={`chip${answer === o.key ? " on" : ""}`}
                      aria-pressed={answer === o.key}
                      onClick={() => setAnswer(step.id, answer === o.key ? undefined : o.key)}
                    >
                      {o.label}
                    </button>
                  ))}
                </div>
              ) : null}

              {kind === "options" ? (
                opts.length === 0 ? (
                  <span className="muted small">{copy(pc, "builder.options.empty")}</span>
                ) : (
                  <div className="pvopts">
                    {opts.map((o) => {
                      const checked = step.type === "multiselect" ? Array.isArray(answer) && answer.includes(o.label) : answer === o.label;
                      const onClick = () =>
                        step.type === "multiselect"
                          ? toggleMulti(step.id, o.label)
                          : setAnswer(step.id, answer === o.label ? undefined : o.label);
                      return (
                        <button key={o.id} type="button" className={`pvopt pvopt-btn${checked ? " on" : ""}`} aria-pressed={checked} onClick={onClick}>
                          {step.type === "multiselect" ? (
                            <span className={`pvcheck${checked ? " on" : ""}`} aria-hidden="true">{checked ? <Check className="ic" /> : null}</span>
                          ) : (
                            <span className={`pvradio${checked ? " on" : ""}`} aria-hidden="true" />
                          )}
                          {o.label}
                        </button>
                      );
                    })}
                  </div>
                )
              ) : null}

              {/* Pickers render as a live-list dropdown stub, scan as a scan control, proof as an upload
                  dropzone — the real operator control's look (inert in preview; the operator does it live). */}
              {kind === "picker" ? (
                <div className="pvselectstub" aria-disabled="true">
                  <span>{copy(pc, "builder.picker.note")}</span>
                  <ChevronDown className="ic" aria-hidden="true" />
                </div>
              ) : null}
              {kind === "scan" ? (
                <div className="pvscanstub" aria-disabled="true">
                  <ScanLine className="ic" aria-hidden="true" />
                  <span>{copy(pc, "builder.scan.note")}</span>
                </div>
              ) : null}
              {kind === "proof" ? (
                <label className="pvdrop">
                  {/* Real native file picker so the author can verify the upload affordance. Preview only
                      captures the chosen filename — no backend upload / proof record is created here. */}
                  <input
                    type="file"
                    className="pvfileinput"
                    accept={step.type === "photo_proof" ? "image/*" : "video/*"}
                    onChange={(e) => setFile(step.id, e.target.files?.[0]?.name)}
                  />
                  {step.type === "photo_proof" ? <Camera className="ic" aria-hidden="true" /> : <Video className="ic" aria-hidden="true" />}
                  <span>{copy(pc, "builder.proof.note")}</span>
                  {files[step.id] ? <span className="pvfile">{files[step.id]}</span> : null}
                </label>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
