"use client";

// SHIFTING SOP: the read-only summary of the three cards -- per card, the captures (kind and
// whether compulsory) and how many questions -- shown in the SOP drawer so the whole document is
// visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { SHIFTING_SECTIONS, parseShifting } from "./shifting-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryList, SummaryRow, SummaryTitle, SummaryMeta, SummaryNote } from "./sop-summary";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

export function ShiftingSummary({ pageContract: pc, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parseShifting(formDsl);
  if (!rows) return null;
  return (
    <SummaryRoot>
      <SummaryHeading>
        {copy(pc, "ssop.drawer.title")} <SummarySub>— {copy(pc, "ssop.drawer.subtitle")}</SummarySub>
      </SummaryHeading>
      <SummaryList>
        {SHIFTING_SECTIONS.map((section) => {
          const block = rows[section];
          const questions = block.questions.length === 1 ? copy(pc, "ssop.summary.questions_one") : fill(copy(pc, "ssop.summary.questions_many"), { n: block.questions.length });
          const captures = block.proofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "ssop.summary.optional")}`})`).join(" + ");
          const line = [captures, block.questions.length > 0 ? questions : ""].filter(Boolean).join(" · ");
          return (
            <SummaryRow key={section}>
              <SummaryTitle>{copy(pc, `ssop.section.${section}`)}</SummaryTitle>
              <SummaryMeta>{line || copy(pc, "ssop.summary.empty")}</SummaryMeta>
              {block.instruction ? <SummaryNote>{block.instruction}</SummaryNote> : null}
            </SummaryRow>
          );
        })}
      </SummaryList>
    </SummaryRoot>
  );
}
