"use client";

// PROCUREMENT SOP: the read-only list of what the inspector answers, page by page, shown in the
// SOP drawer so the whole inspection is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseInspection, parseVendorForm, type InspectionQuestionRow } from "./inspection-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryGroup, SummaryList, SummaryRow, SummaryTitle, SummaryMeta } from "./sop-summary";

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
    <SummaryRoot>
      <SummaryHeading>
        {copy(pageContract, `${prefix}.drawer.title`)} <SummarySub>— {copy(pageContract, `${prefix}.drawer.subtitle`)}</SummarySub>
      </SummaryHeading>
      {rows.loadForm.length > 0 ? (
        <div>
          <SummaryGroup>{copy(pageContract, "inspection.loadform.title")}</SummaryGroup>
          <SummaryList>
            {rows.loadForm.map((q, qi) => (
              <SummaryRow key={q.id}>
                <SummaryTitle>
                  {qi + 1}. {q.title}
                </SummaryTitle>
                <SummaryMeta>{questionMeta(pageContract, q, Object.fromEntries(rows.loadForm.map((x) => [x.key, x.title])))}</SummaryMeta>
              </SummaryRow>
            ))}
          </SummaryList>
        </div>
      ) : null}
      {rows.pages.map((page, pi) => (
        <div key={page.id}>
          <SummaryGroup>
            {copy(pageContract, "inspection.page")} {pi + 1}
            {page.title ? ` · ${page.title}` : ""}
          </SummaryGroup>
          <SummaryList>
            {page.questions.map((q, qi) => {
              return (
                <SummaryRow key={q.id}>
                  <SummaryTitle>
                    {startAt[pi] + qi + 1}. {q.title}
                  </SummaryTitle>
                  <SummaryMeta>{questionMeta(pageContract, q, titleByKey)}</SummaryMeta>
                </SummaryRow>
              );
            })}
          </SummaryList>
        </div>
      ))}
    </SummaryRoot>
  );
}
