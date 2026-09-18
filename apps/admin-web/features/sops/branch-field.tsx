"use client";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { ANSWER_OPS, NUMERIC_ANSWER_OPS, stepAnswerKind, type AnswerOp, type FollowUpStepRow } from "./followup-model";

/**
 * The answer-driven branch control of one step (SOP studio phase 2, 2026-09-18): which earlier
 * QUESTION the step hangs on, how its answer is compared, and the value(s). Blank question =
 * the step always runs. The comparison words and every label are page-contract copy; the
 * questions offered are the track's own earlier steps that record an answer.
 */
export function BranchField({
  pc,
  step,
  earlier,
  answerKinds,
  onChange,
}: {
  pc: AdminUiPageContract;
  step: FollowUpStepRow;
  earlier: FollowUpStepRow[];
  answerKinds: Record<string, string>;
  onChange: (patch: Partial<FollowUpStepRow>) => void;
}) {
  const questions = earlier.filter((s) => {
    const kind = stepAnswerKind(s, answerKinds);
    return kind && kind !== "none";
  });
  const question = questions.find((q) => q.key === step.whenStep);
  const kind = question ? stepAnswerKind(question, answerKinds) : "";
  const ops = ANSWER_OPS.filter((op) => (kind === "number" ? true : !NUMERIC_ANSWER_OPS.includes(op)));
  if (questions.length === 0) return null;
  return (
    <div className="studio-branch" data-testid="branch-field">
      <label>
        {copy(pc, "studio.branch.title")}
        <select
          value={step.whenStep}
          onChange={(e) => {
            const next = e.target.value;
            const q = questions.find((x) => x.key === next);
            const nextKind = q ? stepAnswerKind(q, answerKinds) : "";
            onChange({
              whenStep: next,
              whenOp: NUMERIC_ANSWER_OPS.includes(step.whenOp) && nextKind !== "number" ? "eq" : step.whenOp,
              whenValues: next ? (nextKind === "yes_no" ? ["yes"] : []) : [],
            });
          }}
          data-testid="branch-question"
        >
          <option value="">{copy(pc, "studio.branch.always")}</option>
          {questions.map((q) => (
            <option key={q.key} value={q.key}>
              {q.title || q.key}
            </option>
          ))}
        </select>
      </label>
      {question ? (
        <>
          <label>
            {copy(pc, "studio.branch.op")}
            <select value={step.whenOp} onChange={(e) => onChange({ whenOp: e.target.value as AnswerOp })} data-testid="branch-op">
              {ops.map((op) => (
                <option key={op} value={op}>
                  {copy(pc, `studio.branch.op.${op}`)}
                </option>
              ))}
            </select>
          </label>
          <label>
            {copy(pc, "studio.branch.value")}
            {kind === "yes_no" ? (
              <select value={step.whenValues[0] ?? "yes"} onChange={(e) => onChange({ whenValues: [e.target.value] })} data-testid="branch-value">
                <option value="yes">{copy(pc, "studio.branch.yes")}</option>
                <option value="no">{copy(pc, "studio.branch.no")}</option>
              </select>
            ) : kind === "select" || kind === "multiselect" ? (
              <span className="studio-branch-options">
                {question.options.map((o) => (
                  <label key={o} className="chkline">
                    <input
                      type="checkbox"
                      checked={step.whenValues.includes(o)}
                      onChange={(e) => onChange({ whenValues: e.target.checked ? [...step.whenValues, o] : step.whenValues.filter((v) => v !== o) })}
                    />{" "}
                    {o}
                  </label>
                ))}
              </span>
            ) : (
              <input
                value={step.whenValues.join(", ")}
                inputMode={kind === "number" ? "decimal" : "text"}
                placeholder={copy(pc, "studio.branch.values_hint")}
                onChange={(e) => onChange({ whenValues: e.target.value.split(",").map((v) => v.trim()).filter(Boolean) })}
                data-testid="branch-value"
              />
            )}
          </label>
        </>
      ) : null}
    </div>
  );
}

/** The farm sentence for a branch, mirroring tasks/domain.AnswerCondition.Phrase. */
export function branchPhrase(pc: AdminUiPageContract, step: FollowUpStepRow, questionTitle: string): string {
  const values = step.whenValues.join(", ");
  return `${copy(pc, "studio.branch.note")} “${questionTitle}” ${copy(pc, `studio.branch.op.${step.whenOp}`)} ${values}`;
}
