"use client";

// WEIGHING SOP: the read-only summary of the rules a weighing task runs under, shown in the SOP
// drawer so the whole rule set is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { CAPTURE_DEFAULTS_COPY_KEY, parseCaptureDefaults, parseWeighing, type WeighingQuestionRow } from "./weighing-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryGroup, SummaryList, SummaryRow, SummaryTitle, SummaryMeta, SummaryNote } from "./sop-summary";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

function questionMeta(pc: AdminUiPageContract, q: WeighingQuestionRow, titleByKey: Record<string, string>): string {
  const parts: string[] = [copy(pc, `inspection.kind.${q.kind}`)];
  if (q.kind === "choice" || q.kind === "multi") parts.push(q.options.map((o) => o.label).join(" / "));
  if (q.kind === "number" && q.unit) parts.push(q.unit);
  parts.push(copy(pc, q.required ? "inspection.summary.required" : "inspection.summary.optional"));
  if (q.onlyIfQuestion) parts.push(fill(copy(pc, "inspection.summary.only_if"), { question: titleByKey[q.onlyIfQuestion] ?? q.onlyIfQuestion, value: q.onlyIfValue }));
  return parts.join(" · ");
}

export function WeighingSummary({ pageContract: pc, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  // The sections a document leaves implicit read as the contract's seeded slot document.
  const rows = parseWeighing(formDsl, parseCaptureDefaults(copy(pc, CAPTURE_DEFAULTS_COPY_KEY, "")));
  if (!rows) return null;
  const questionsCount = (n: number) => (n === 1 ? copy(pc, "wsop.summary.questions_one") : fill(copy(pc, "wsop.summary.questions_many"), { n }));
  const lumpTotal = rows.lumpSumProofs.reduce((sum, p) => sum + (Number(p.max) || 0), 0);
  const titleByKey = Object.fromEntries(rows.removalQuestions.map((q) => [q.key, q.title]));
  return (
    <SummaryRoot>
      <SummaryHeading>
        {copy(pc, "wsop.drawer.title")} <SummarySub>— {copy(pc, "wsop.drawer.subtitle")}</SummarySub>
      </SummaryHeading>
      <SummaryList>
        <SummaryRow>
          <SummaryTitle>{copy(pc, "wsop.section.planning")}</SummaryTitle>
          <SummaryMeta>
            {rows.modes.map((m) => copy(pc, `wsop.planning.mode.${m}`)).join(" · ")} · {copy(pc, "wsop.planning.default_cap")}: {rows.defaultCapPerDay}
          </SummaryMeta>
        </SummaryRow>
        <SummaryRow>
          <SummaryTitle>{copy(pc, "wsop.section.removal")}</SummaryTitle>
          <SummaryMeta>
            {copy(pc, `wsop.removal.mode.${rows.removalMode}`)}
            {rows.removalMode !== "off"
              ? ` · ${rows.removalProofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "inspection.summary.optional")}`})`).join(" + ")}`
              : ""}
          </SummaryMeta>
          {rows.removalMode !== "off" && rows.removalInstruction ? <SummaryNote>{rows.removalInstruction}</SummaryNote> : null}
          {rows.removalMode !== "off" ? (
            <SummaryNote>
              {copy(pc, "wsop.removal.cutoff")}: {rows.removalCutoffTime || copy(pc, "wsop.removal.cutoff.farm")}
            </SummaryNote>
          ) : null}
        </SummaryRow>
        <SummaryRow>
          <SummaryTitle>{copy(pc, "wsop.section.capture")}</SummaryTitle>
          {/* Two sections, never one shared list (maintainer decision 2026-09-16). */}
          <SummaryMeta>
            <b>{copy(pc, "wsop.capture.individual.title")}</b> ·{" "}
            {fill(copy(pc, "wsop.summary.individual_captures"), { n: rows.individualProofs.length, required: rows.individualProofs.filter((p) => p.required).length })} ·{" "}
            {rows.individualProofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "inspection.summary.optional")}`})`).join(" + ")} ·{" "}
            {questionsCount(rows.individualQuestions.length)}
          </SummaryMeta>
          <SummaryMeta>
            <b>{copy(pc, "wsop.capture.lump_sum.title")}</b> · {fill(copy(pc, "wsop.summary.lump_sum_captures"), { n: rows.lumpSumProofs.length, max: lumpTotal })} ·{" "}
            {rows.lumpSumProofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)} ${p.min}–${p.max})`).join(" + ")} · {questionsCount(rows.lumpSumQuestions.length)}
          </SummaryMeta>
        </SummaryRow>
      </SummaryList>
      {rows.removalMode !== "off" && rows.removalQuestions.length > 0 ? (
        <div>
          <SummaryGroup>{copy(pc, "wsop.removal.questions")}</SummaryGroup>
          <SummaryList>
            {rows.removalQuestions.map((q, qi) => (
              <SummaryRow key={q.id}>
                <SummaryTitle>
                  {qi + 1}. {q.title}
                </SummaryTitle>
                <SummaryMeta>{questionMeta(pc, q, titleByKey)}</SummaryMeta>
              </SummaryRow>
            ))}
          </SummaryList>
        </div>
      ) : null}
      {rows.individualQuestions.length > 0 ? questionBlock(pc, "wsop.capture.individual.questions", rows.individualQuestions) : null}
      {rows.lumpSumQuestions.length > 0 ? questionBlock(pc, "wsop.capture.lump_sum.questions", rows.lumpSumQuestions) : null}
    </SummaryRoot>
  );
}

function questionBlock(pc: AdminUiPageContract, titleKey: string, questions: WeighingQuestionRow[]) {
  const titleByKey = Object.fromEntries(questions.map((q) => [q.key, q.title]));
  return (
    <div>
      <SummaryGroup>{copy(pc, titleKey)}</SummaryGroup>
      <SummaryList>
        {questions.map((q, qi) => (
          <SummaryRow key={q.id}>
            <SummaryTitle>
              {qi + 1}. {q.title}
            </SummaryTitle>
            <SummaryMeta>{questionMeta(pc, q, titleByKey)}</SummaryMeta>
          </SummaryRow>
        ))}
      </SummaryList>
    </div>
  );
}
