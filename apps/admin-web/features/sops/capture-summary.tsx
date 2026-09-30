"use client";

// HERD OPERATIONS CAPTURE CARD: the read-only summary of what the Add birth / Add death form asks
// beside its fixed fields -- the captures (kind, compulsory or not) and the questions -- shown in
// the SOP drawer so the card is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseCaptureCard } from "./capture-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryList, SummaryRow, SummaryTitle, SummaryMeta, SummaryEmpty } from "./sop-summary";

export function CaptureCardSummary({ pageContract: pc, sopCode, formDsl }: { pageContract: AdminUiPageContract; sopCode: string; formDsl: unknown }) {
  const rows = parseCaptureCard(sopCode, formDsl);
  if (!rows) return null;
  const empty = rows.proofs.length === 0 && rows.questions.length === 0;
  return (
    <SummaryRoot>
      <SummaryHeading>
        {copy(pc, "capture.drawer.title")} <SummarySub>— {copy(pc, "capture.drawer.subtitle")}</SummarySub>
      </SummaryHeading>
      {empty ? (
        <SummaryEmpty>{copy(pc, "capture.drawer.empty")}</SummaryEmpty>
      ) : (
        <SummaryList>
          {rows.proofs.map((p) => (
            <SummaryRow key={p.id}>
              <SummaryTitle>{p.title}</SummaryTitle>
              <SummaryMeta>
                {copy(pc, `wsop.proof.kind.${p.kind}`)}
                {p.required ? "" : ` · ${copy(pc, "capture.summary.optional")}`}
              </SummaryMeta>
            </SummaryRow>
          ))}
          {rows.questions.map((q) => (
            <SummaryRow key={q.id}>
              <SummaryTitle>{q.title}</SummaryTitle>
              <SummaryMeta>
                {copy(pc, "capture.question")}
                {q.required ? "" : ` · ${copy(pc, "capture.summary.optional")}`}
              </SummaryMeta>
            </SummaryRow>
          ))}
        </SummaryList>
      )}
    </SummaryRoot>
  );
}
