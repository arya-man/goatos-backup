"use client";

// PROCUREMENT SOP: the read-only list of what the inspector answers, page by page, shown in the
// SOP drawer so the whole inspection is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseInspection, type InspectionQuestionRow } from "./inspection-model";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

function questionMeta(pc: AdminUiPageContract, q: InspectionQuestionRow, titleByKey: Record<string, string>): string {
  const parts: string[] = [copy(pc, `inspection.kind.${q.kind}`)];
  if (q.kind === "media") {
    parts.push(copy(pc, `inspection.accepts.${q.accepts}`));
    if (q.maxFiles > 1) parts.push(fill(copy(pc, "inspection.summary.files"), { n: q.maxFiles }));
  }
  if (q.kind === "choice" || q.kind === "multi") parts.push(q.options.map((o) => o.label).join(" / "));
  if (q.kind === "number" && q.unit) parts.push(q.unit);
  parts.push(copy(pc, q.required ? "inspection.summary.required" : "inspection.summary.optional"));
  if (q.onlyIfQuestion) parts.push(fill(copy(pc, "inspection.summary.only_if"), { question: titleByKey[q.onlyIfQuestion] ?? q.onlyIfQuestion, value: q.onlyIfValue }));
  return parts.join(" · ");
}

export function InspectionSummary({ pageContract, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parseInspection(formDsl);
  if (!rows) return null;
  const titleByKey = Object.fromEntries(rows.pages.flatMap((p) => p.questions.map((q) => [q.key, q.title])));
  // Running question number across pages, computed up front (phone order).
  const startAt = rows.pages.map((_, i) => rows.pages.slice(0, i).reduce((n, p) => n + p.questions.length, 0));
  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pageContract, "inspection.drawer.title")} <span className="muted small">— {copy(pageContract, "inspection.drawer.subtitle")}</span>
      </div>
      {rows.pages.map((page, pi) => (
        <div key={page.id}>
          <div className="muted small b700" style={{ margin: "8px 0 4px" }}>
            {copy(pageContract, "inspection.page")} {pi + 1}
            {page.title ? ` · ${page.title}` : ""}
          </div>
          <div className="htl">
            {page.questions.map((q, qi) => {
              return (
                <div className="hrow" key={q.id}>
                  <div className="htx">
                    <b>
                      {startAt[pi] + qi + 1}. {q.title}
                    </b>
                    <div className="hmeta muted small">{questionMeta(pageContract, q, titleByKey)}</div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      ))}
    </div>
  );
}
