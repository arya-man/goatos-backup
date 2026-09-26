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
import { visibleQuestionIds } from "./authored-form-visibility";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ProcurementVendorForm, ProcurementVendorQuestion } from "@/lib/api/server";
import Box from "@mui/material/Box";
import Checkbox from "@mui/material/Checkbox";
import FormControl from "@mui/material/FormControl";
import FormControlLabel from "@mui/material/FormControlLabel";
import FormGroup from "@mui/material/FormGroup";
import FormHelperText from "@mui/material/FormHelperText";
import FormLabel from "@mui/material/FormLabel";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { FormSelect } from "./form-select";
import { listOptions } from "./option-utils";

type ProcurementVendorFormPage = ProcurementVendorForm["pages"][number];
type ProcurementVendorQuestionOption = NonNullable<ProcurementVendorQuestion["options"]>[number];

/** authoredQuestions is every question the ledger does NOT store in a column of its own. */
function authoredQuestions(form: ProcurementVendorForm): Array<{ page: string; questions: ProcurementVendorQuestion[] }> {
  return form.pages
    .map((p: ProcurementVendorFormPage) => ({ page: p.title ?? "", questions: p.questions.filter((q: ProcurementVendorQuestion) => !q.typed) }))
    .filter((p: { page: string; questions: ProcurementVendorQuestion[] }) => p.questions.length > 0);
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

  const visibleIds = visibleQuestionIds(form.pages.flatMap((page) => page.questions), { ...typed, ...picked });
  const visiblePages = pages.map((page) => ({ ...page, questions: page.questions.filter((q) => visibleIds.has(q.id)) }));

  return (
    <Box ref={rootRef} sx={{ display: "contents" }}>
      {visiblePages.map((page, pi) => (
        <Box key={`${page.page}-${pi}`} sx={{ display: "contents" }}>
          {page.page ? (
            <Typography variant="subtitle2" component="h3" sx={{ pt: 1 }}>
              {page.page}
            </Typography>
          ) : null}
          {page.questions.map((q) => {
            const id = `fp-sop-${q.id}`;
            const name = `sop.${q.id}`;
            const pickedValue = picked[q.id] ?? "";
            const hint = [q.hint, q.unit].filter(Boolean).join(" · ");
            return (
              <Box key={q.id} sx={{ display: "flex", flexDirection: "column", gap: 1, minWidth: 0 }}>
                {q.kind === "choice" ? (
                  <>
                    <FormSelect
                      label={q.title}
                      id={id}
                      name={name}
                      required={q.required}
                      value={pickedValue}
                      onValueChange={(next) => setPicked((prev) => ({ ...prev, [q.id]: next }))}
                      options={listOptions(q.options ?? [], (o: ProcurementVendorQuestionOption) => o.value, (o: ProcurementVendorQuestionOption) => o.label, "—")}
                    />
                    {q.allow_other && pickedValue === "other" ? (
                      <TextField
                        fullWidth
                        name={`${name}_other`}
                        placeholder={copy(pageContract, "hint.other")}
                        required
                        slotProps={{ htmlInput: { maxLength: 160, "aria-label": `${q.title} ${copy(pageContract, "hint.other")}` } }}
                      />
                    ) : null}
                  </>
                ) : q.kind === "multi" ? (
                  <FormControl component="fieldset" required={q.required}>
                    <FormLabel component="legend">{q.title}</FormLabel>
                    <FormGroup>
                      {(q.options ?? []).map((o: ProcurementVendorQuestionOption) => (
                        <FormControlLabel
                          key={o.value}
                          control={<Checkbox name={name} value={o.value} sx={{ p: { xs: 1.5, sm: 1 } }} />}
                          label={<>{o.label}</>}
                        />
                      ))}
                    </FormGroup>
                  </FormControl>
                ) : (
                  <TextField
                    fullWidth
                    id={id}
                    name={name}
                    type={q.kind === "number" ? "number" : "text"}
                    label={q.title}
                    required={q.required}
                    slotProps={{
                      htmlInput: q.kind === "number" ? { min: q.min ?? undefined, max: q.max ?? undefined, step: "any" } : { maxLength: 500 },
                      inputLabel: { shrink: true },
                    }}
                  />
                )}
                {hint ? <FormHelperText sx={{ mx: 0 }}>{hint}</FormHelperText> : null}
              </Box>
            );
          })}
        </Box>
      ))}
      <Typography variant="body2" component="div" sx={{ color: "text.secondary" }}>
        {copy(pageContract, "hint.authored_questions")}
      </Typography>
    </Box>
  );
}
