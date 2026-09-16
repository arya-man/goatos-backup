"use client";

// FEED SOP: the read-only summary of the cards the crew runs -- per stage, the captures (kind and
// whether compulsory) and how many questions -- shown in the SOP drawer so the whole card is
// visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FEED_STAGES_BY_CODE, parseFeed } from "./feed-model";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

export function FeedSummary({ pageContract: pc, sopCode, formDsl }: { pageContract: AdminUiPageContract; sopCode: string; formDsl: unknown }) {
  const rows = parseFeed(sopCode, formDsl);
  if (!rows) return null;
  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pc, "fsop.drawer.title")} <span className="muted small">— {copy(pc, "fsop.drawer.subtitle")}</span>
      </div>
      <div className="htl">
        {(FEED_STAGES_BY_CODE[sopCode] ?? []).map((stage) => {
          const block = rows.stages[stage];
          if (!block) return null;
          const questions = block.questions.length === 1 ? copy(pc, "fsop.summary.questions_one") : fill(copy(pc, "fsop.summary.questions_many"), { n: block.questions.length });
          return (
            <div className="hrow" key={stage}>
              <div className="htx">
                <b>{copy(pc, `fsop.stage.${stage}`)}</b>
                <div className="hmeta muted small">
                  {block.proofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "fsop.summary.optional")}`})`).join(" + ")}
                  {block.questions.length > 0 ? ` · ${questions}` : ""}
                </div>
                {block.instruction ? <div className="muted small">{block.instruction}</div> : null}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
