"use client";

// HERD OPERATIONS CAPTURE CARD: the read-only summary of what the Add birth / Add death form asks
// beside its fixed fields -- the captures (kind, compulsory or not) and the questions -- shown in
// the SOP drawer so the card is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseCaptureCard } from "./capture-model";

export function CaptureCardSummary({ pageContract: pc, sopCode, formDsl }: { pageContract: AdminUiPageContract; sopCode: string; formDsl: unknown }) {
  const rows = parseCaptureCard(sopCode, formDsl);
  if (!rows) return null;
  const empty = rows.proofs.length === 0 && rows.questions.length === 0;
  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pc, "capture.drawer.title")} <span className="muted small">— {copy(pc, "capture.drawer.subtitle")}</span>
      </div>
      {empty ? (
        <div className="note">{copy(pc, "capture.drawer.empty")}</div>
      ) : (
        <div className="htl">
          {rows.proofs.map((p) => (
            <div className="hrow" key={p.id}>
              <div className="htx">
                <b>{p.title}</b>
                <div className="hmeta muted small">
                  {copy(pc, `wsop.proof.kind.${p.kind}`)}
                  {p.required ? "" : ` · ${copy(pc, "capture.summary.optional")}`}
                </div>
              </div>
            </div>
          ))}
          {rows.questions.map((q) => (
            <div className="hrow" key={q.id}>
              <div className="htx">
                <b>{q.title}</b>
                <div className="hmeta muted small">
                  {copy(pc, "capture.question")}
                  {q.required ? "" : ` · ${copy(pc, "capture.summary.optional")}`}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
