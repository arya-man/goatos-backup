"use client";

// THE FEED PURCHASE FORM IS AUTHORED (maintainer decision 2026-09-20,
// docs/decisions/procurement-sop-driven.md).
//
// The Record purchase drawer keeps its purpose-built inputs for the LEDGER's own columns -- the
// date picker, the stepped numbers, the catalog selects -- because those columns are read by the
// stock cards, the landed rate and the aflatoxin task, and a generic renderer would be a worse
// version of controls that already exist.
//
// This renders everything the farm authored BEYOND them, from the published document, in the
// pages and order it declares. Each answer travels as `sop.<question id>`; the server action
// collects those alongside the typed values and sends them with the form version, so the backend
// checks the submission against exactly the form this drawer rendered.
import { useEffect, useRef, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ProcurementVendorForm, ProcurementVendorQuestion } from "@/lib/api/server";

/** authoredQuestions is every question the ledger does NOT store in a column of its own. */
function authoredQuestions(form: ProcurementVendorForm): Array<{ page: string; questions: ProcurementVendorQuestion[] }> {
  return form.pages
    .map((p) => ({ page: p.title ?? "", questions: p.questions.filter((q) => !q.typed) }))
    .filter((p) => p.questions.length > 0);
}

const typedFeedPurchaseQuestions: Array<[questionId: string, fieldName: string]> = [
  ["purchase_date", "purchase_date"],
  ["farm_label", "farm"],
  ["feed_item_label", "feed_item"],
  ["quantity_kg", "quantity_kg"],
  ["vendor", "vendor"],
  ["feed_cost", "feed_cost"],
  ["transport_cost", "transport_cost"],
  ["loading_cost", "loading_cost"],
  ["unloading_cost", "unloading_cost"],
  ["total_cost", "total_cost"],
  ["payment_released", "payment_released"],
  ["payment_status", "payment_status"],
  ["reached_on", "reached_on"],
  ["reached_weight_kg", "reached_weight_kg"],
];

function currentTypedAnswers(root: HTMLElement | null): Record<string, string> {
  const form = root?.closest("form");
  if (!form) return {};
  const data = new FormData(form);
  return Object.fromEntries(typedFeedPurchaseQuestions.map(([questionId, fieldName]) => [questionId, String(data.get(fieldName) ?? "").trim()]));
}

export function FeedPurchaseExtraFields({ form, pageContract }: { form: ProcurementVendorForm | null; pageContract: AdminUiPageContract }) {
  const rootRef = useRef<HTMLDivElement | null>(null);
  // Pick-one extras and typed ledger fields are tracked live for "ask only when" visibility;
  // everything else is still an uncontrolled input the server action reads off the form.
  const [picked, setPicked] = useState<Record<string, string>>({});
  const [typed, setTyped] = useState<Record<string, string>>({});
  useEffect(() => {
    const root = rootRef.current;
    const parentForm = root?.closest("form");
    const refresh = () => setTyped(currentTypedAnswers(root));
    refresh();
    parentForm?.addEventListener("input", refresh);
    parentForm?.addEventListener("change", refresh);
    return () => {
      parentForm?.removeEventListener("input", refresh);
      parentForm?.removeEventListener("change", refresh);
    };
  }, [form]);
  if (!form) return null;
  const pages = authoredQuestions(form);
  if (pages.length === 0) return null;

  const answers = { ...typed, ...picked };
  const visible = (q: ProcurementVendorQuestion) => !q.only_if || (answers[q.only_if.question_id] ?? "") === q.only_if.value;

  return (
    <div ref={rootRef} style={{ display: "contents" }}>
      {pages.map((page, pi) => (
        <div key={`${page.page}-${pi}`} style={{ display: "contents" }}>
          {page.page ? <div className="dgrp">{page.page}</div> : null}
          {page.questions.filter(visible).map((q) => {
            const id = `fp-sop-${q.id}`;
            const name = `sop.${q.id}`;
            return (
              <div className="fld" key={q.id}>
                <label htmlFor={id}>
                  {q.title}
                  {q.required ? " *" : ""}
                </label>
                {q.kind === "choice" ? (
                  <>
                    <select
                      id={id}
                      name={name}
                      required={q.required}
                      defaultValue=""
                      onChange={(e) => setPicked((prev) => ({ ...prev, [q.id]: e.target.value }))}
                    >
                      <option value="" disabled>
                        —
                      </option>
                      {(q.options ?? []).map((o) => (
                        <option key={o.value} value={o.value}>
                          {o.label}
                        </option>
                      ))}
                    </select>
                    {q.allow_other && picked[q.id] === "other" ? (
                      <input name={`${name}_other`} placeholder={copy(pageContract, "hint.other")} required maxLength={160} />
                    ) : null}
                  </>
                ) : q.kind === "multi" ? (
                  <div className="vendor-form-multi">
                    {(q.options ?? []).map((o) => (
                      <label key={o.value} className="chkline">
                        <input type="checkbox" name={name} value={o.value} /> {o.label}
                      </label>
                    ))}
                  </div>
                ) : q.kind === "number" ? (
                  <input
                    id={id}
                    name={name}
                    type="number"
                    required={q.required}
                    min={q.min ?? undefined}
                    max={q.max ?? undefined}
                    step="any"
                  />
                ) : (
                  <input id={id} name={name} type="text" required={q.required} maxLength={500} />
                )}
                {q.hint ? <div className="muted small">{q.hint}</div> : null}
                {q.unit ? <div className="muted small">{q.unit}</div> : null}
              </div>
            );
          })}
        </div>
      ))}
      <div className="note">{copy(pageContract, "hint.authored_questions")}</div>
    </div>
  );
}
