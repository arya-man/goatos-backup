"use client";

// PC CARE SOP: the read-only summary of the cards the operators run -- the evening-before feed &
// water removal (whether it applies and to which work) and, per work category, the captures (kind
// and whether compulsory) and how many questions -- shown in the SOP drawer so the whole document
// is visible without opening the editor.
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PC_CARE_CATEGORIES, parsePcCare, type PcCareCategory } from "./pc-care-model";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

export function PcCareSummary({ pageContract: pc, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parsePcCare(formDsl);
  if (!rows) return null;
  const categoryOptions = optionGroup(pc, "pcsop_categories");
  const label = (category: PcCareCategory) => categoryOptions.find((o) => o.key === category)?.label ?? category;
  const captures = (n: number) => (n === 1 ? copy(pc, "pcsop.summary.captures_one") : fill(copy(pc, "pcsop.summary.captures_many"), { count: n }));
  const questions = (n: number) => (n === 1 ? copy(pc, "pcsop.summary.questions_one") : fill(copy(pc, "pcsop.summary.questions_many"), { count: n }));
  const removalMode = copy(pc, `pcsop.summary.removal.${rows.removal.mode}`);

  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pc, "pcsop.drawer.title")} <span className="muted small">— {copy(pc, "pcsop.drawer.subtitle")}</span>
      </div>
      <div className="htl">
        <div className="hrow">
          <div className="htx">
            <b>{copy(pc, "pcsop.section.removal")}</b>
            <div className="hmeta muted small">
              {removalMode}
              {rows.removal.mode === "off" ? null : ` · ${rows.removal.appliesTo.map(label).join(", ") || copy(pc, "pcsop.removal.applies_to.empty")}`}
              {rows.removal.mode === "off" ? null : ` · ${rows.removal.cutoffTime.trim() || copy(pc, "pcsop.removal.cutoff.farm")}`}
            </div>
            {rows.removal.mode === "off" ? null : (
              <div className="hmeta muted small">
                {rows.removal.proofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "pcsop.summary.optional")}`})`).join(" + ")}
                {rows.removal.questions.length > 0 ? ` · ${questions(rows.removal.questions.length)}` : ""}
              </div>
            )}
          </div>
        </div>
        {PC_CARE_CATEGORIES.map((category) => {
          const block = rows.categories[category];
          return (
            <div className="hrow" key={category}>
              <div className="htx">
                <b>{label(category)}</b>
                <div className="hmeta muted small">
                  {block.proofs.length === 0
                    ? captures(0)
                    : block.proofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "pcsop.summary.optional")}`})`).join(" + ")}
                  {block.questions.length > 0 ? ` · ${questions(block.questions.length)}` : ""}
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
