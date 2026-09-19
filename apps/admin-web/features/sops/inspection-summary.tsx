"use client";

// PROCUREMENT SOP: the read-only list of what the inspector answers, page by page, shown in the
// SOP drawer so the whole inspection is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseInspection, parseVendorForm, type InspectionQuestionRow } from "./inspection-model";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

function questionMeta(pc: AdminUiPageContract, q: InspectionQuestionRow, titleByKey: Record<string, string>): string {
  const parts: string[] = [copy(pc, `inspection.kind.${q.kind}`)];
  if (q.kind === "media") {
    parts.push(copy(pc, `inspection.accepts.${q.accepts}`));
    if (q.maxFiles > 1) parts.push(fill(copy(pc, "inspection.summary.files"), { n: q.maxFiles }));
  }
  if (q.catalog) parts.push(`${copy(pc, "vendor_form.notice.catalog_choices")} ${q.catalog}`);
  else if (q.kind === "choice" || q.kind === "multi") parts.push(q.options.map((o) => o.label).join(" / "));
  if (q.kind === "number" && q.unit) parts.push(q.unit);
  parts.push(copy(pc, q.required ? "inspection.summary.required" : "inspection.summary.optional"));
  if (q.onlyIfQuestion) parts.push(fill(copy(pc, "inspection.summary.only_if"), { question: titleByKey[q.onlyIfQuestion] ?? q.onlyIfQuestion, value: q.onlyIfValue }));
  return parts.join(" · ");
}

// profile selects the document read: the inspection (default) or the VENDOR FORM (2026-09-19),
// which shares the pages shape and this summary.
export function InspectionSummary({ pageContract, formDsl, profile = "inspection" }: { pageContract: AdminUiPageContract; formDsl: unknown; profile?: "inspection" | "vendor_form" }) {
  const rows = profile === "vendor_form" ? parseVendorForm(formDsl) : parseInspection(formDsl);
  if (!rows) return null;
  const prefix = profile === "vendor_form" ? "vendor_form" : "inspection";
  const titleByKey = Object.fromEntries(rows.pages.flatMap((p) => p.questions.map((q) => [q.key, q.title])));
  // Running question number across pages, computed up front (phone order).
  const startAt = rows.pages.map((_, i) => rows.pages.slice(0, i).reduce((n, p) => n + p.questions.length, 0));
  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pageContract, `${prefix}.drawer.title`)} <span className="muted small">— {copy(pageContract, `${prefix}.drawer.subtitle`)}</span>
      </div>
      {rows.loadForm.length > 0 ? (
        <div>
          <div className="muted small b700" style={{ margin: "8px 0 4px" }}>
            {copy(pageContract, "inspection.loadform.title")}
          </div>
          <div className="htl">
            {rows.loadForm.map((q, qi) => (
              <div className="hrow" key={q.id}>
                <div className="htx">
                  <b>
                    {qi + 1}. {q.title}
                  </b>
                  <div className="hmeta muted small">{questionMeta(pageContract, q, Object.fromEntries(rows.loadForm.map((x) => [x.key, x.title])))}</div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}
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
