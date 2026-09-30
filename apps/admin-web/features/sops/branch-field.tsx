"use client";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FieldSelect } from "./editor-chrome";
import { ANSWER_OPS, NUMERIC_ANSWER_OPS, stepAnswerKind, type AnswerOp, type FollowUpStepRow } from "./followup-model";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import MuiTextField from "@mui/material/TextField";
import type { InputHTMLAttributes } from "react";
import { CheckLine, Hint } from "./editor-parts";

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
    <Box
      data-testid="branch-field"
      sx={(theme) => ({
        display: "grid",
        gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))",
        columnGap: 1,
        rowGap: 1,
        p: 1.25,
        border: `dashed 1px ${theme.vars.palette.divider}`,
        borderRadius: "var(--r-md)",
      })}
    >
      <FieldSelect
        label={copy(pc, "studio.branch.title")}
        value={step.whenStep}
        minWidth={200}
        options={[{ value: "", label: copy(pc, "studio.branch.always") }, ...questions.map((q) => ({ value: q.key, label: q.title || q.key }))]}
        onChange={(next) => {
          const q = questions.find((x) => x.key === next);
          const nextKind = q ? stepAnswerKind(q, answerKinds) : "";
          onChange({
            whenStep: next,
            whenOp: NUMERIC_ANSWER_OPS.includes(step.whenOp) && nextKind !== "number" ? "eq" : step.whenOp,
            whenValues: next ? (nextKind === "yes_no" ? ["yes"] : []) : [],
          });
        }}
      />
      {question ? (
        <>
          <FieldSelect
            label={copy(pc, "studio.branch.op")}
            value={step.whenOp}
            minWidth={150}
            options={ops.map((op) => ({ value: op, label: copy(pc, `studio.branch.op.${op}`) }))}
            onChange={(next) => onChange({ whenOp: next as AnswerOp })}
          />
          {kind === "yes_no" ? (
            <FieldSelect
              label={copy(pc, "studio.branch.value")}
              value={step.whenValues[0] ?? "yes"}
              minWidth={120}
              options={[
                { value: "yes", label: copy(pc, "studio.branch.yes") },
                { value: "no", label: copy(pc, "studio.branch.no") },
              ]}
              onChange={(next) => onChange({ whenValues: [next] })}
            />
          ) : (
          kind === "select" || kind === "multiselect" ? (
            <Stack sx={{ gridColumn: "1 / -1" }}>
              <Hint caption>{copy(pc, "studio.branch.value")}</Hint>
              <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
                {question.options.map((o) => (
                  <CheckLine key={o} checked={step.whenValues.includes(o)} onChange={(on) => onChange({ whenValues: on ? [...step.whenValues, o] : step.whenValues.filter((v) => v !== o) })} label={o} />
                ))}
              </Stack>
            </Stack>
          ) : (
            <MuiTextField
              label={copy(pc, "studio.branch.value")}
              size="small"
              value={step.whenValues.join(", ")}
              placeholder={copy(pc, "studio.branch.values_hint")}
              slotProps={{ inputLabel: { shrink: true }, htmlInput: { inputMode: kind === "number" ? "decimal" : "text", "data-testid": "branch-value" } as InputHTMLAttributes<HTMLInputElement> }}
              onChange={(e) => onChange({ whenValues: e.target.value.split(",").map((v) => v.trim()).filter(Boolean) })}
              sx={{ mt: 1 }}
            />
          )
          )}
        </>
      ) : null}
    </Box>
  );
}

/** The farm sentence for a branch, mirroring tasks/domain.AnswerCondition.Phrase. */
export function branchPhrase(pc: AdminUiPageContract, step: FollowUpStepRow, questionTitle: string): string {
  const values = step.whenValues.join(", ");
  return `${copy(pc, "studio.branch.note")} “${questionTitle}” ${copy(pc, `studio.branch.op.${step.whenOp}`)} ${values}`;
}
