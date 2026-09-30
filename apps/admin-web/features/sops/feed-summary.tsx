"use client";

// FEED SOP: the read-only summary of the cards the crew runs -- per stage, the captures (kind and
// whether compulsory) and how many questions -- shown in the SOP drawer so the whole card is
// visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FEED_STAGES_BY_CODE, parseFeed } from "./feed-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryList, SummaryRow, SummaryTitle, SummaryMeta, SummaryNote } from "./sop-summary";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

export function FeedSummary({ pageContract: pc, sopCode, formDsl }: { pageContract: AdminUiPageContract; sopCode: string; formDsl: unknown }) {
  const rows = parseFeed(sopCode, formDsl);
  if (!rows) return null;
  return (
    <SummaryRoot>
      <SummaryHeading>
        {copy(pc, "fsop.drawer.title")} <SummarySub>— {copy(pc, "fsop.drawer.subtitle")}</SummarySub>
      </SummaryHeading>
      <SummaryList>
        {(FEED_STAGES_BY_CODE[sopCode] ?? []).map((stage) => {
          const block = rows.stages[stage];
          if (!block) return null;
          const questions = block.questions.length === 1 ? copy(pc, "fsop.summary.questions_one") : fill(copy(pc, "fsop.summary.questions_many"), { n: block.questions.length });
          return (
            <SummaryRow key={stage}>
              <SummaryTitle>{copy(pc, `fsop.stage.${stage}`)}</SummaryTitle>
              <SummaryMeta>
                {block.proofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "fsop.summary.optional")}`})`).join(" + ")}
                {block.questions.length > 0 ? ` · ${questions}` : ""}
              </SummaryMeta>
              {block.instruction ? <SummaryNote>{block.instruction}</SummaryNote> : null}
            </SummaryRow>
          );
        })}
      </SummaryList>
    </SummaryRoot>
  );
}
