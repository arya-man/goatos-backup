"use client";

// VENDOR FORM IS AUTHORED (maintainer instruction 2026-09-19, docs/decisions/sales-sop.md -> "The
// vendor form"). The add / edit vendor body rendered from the published sales.vendor form: one
// section per page, one control per question, by kind. Typed questions (the register's own
// columns) are named by their column so the server action's typed read still works; every
// question also travels as an answer under its id, with the form version, so the backend checks
// the submission against exactly the form this drawer rendered.
import { useState } from "react";
import MenuItem from "@mui/material/MenuItem";
import TextField from "@mui/material/TextField";
import { visibleQuestionIds } from "./authored-form-visibility";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ProcurementVendor, ProcurementVendorForm, ProcurementVendorQuestion } from "@/lib/api/server";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

type Answers = Record<string, string>;

// Typed columns the register carries that a form may not ask. On EDIT their current values ride
// as hidden inputs so the REPLACE write cannot clear a column the form never showed.
const TYPED_CARRY: Array<[string, (v: ProcurementVendor) => string]> = [
  ["business_name", (v) => v.business_name ?? ""],
  ["record_type", (v) => v.record_type ?? ""],
  ["contact_person_name", (v) => v.contact_person_name ?? ""],
  ["phone_number", (v) => v.phone_number ?? ""],
  ["state", (v) => v.state ?? ""],
  ["city", (v) => v.city ?? ""],
  ["status", (v) => v.status ?? ""],
  ["breed", (v) => v.breed ?? ""],
  ["feed", (v) => v.feed ?? ""],
  ["filtered_stock", (v) => (v.filtered_stock ?? "").toString()],
  ["price_per_goat", (v) => v.price_per_goat ?? ""],
  ["ready_to_filtered", (v) => v.ready_to_filtered ?? ""],
  ["eta_after_order_days", (v) => (v.eta_after_order_days ?? "").toString()],
  ["details", (v) => v.details ?? ""],
  ["comments", (v) => v.comments ?? ""],
  ["capacity_quantity", (v) => v.capacity_quantity ?? ""],
  ["capacity_unit", (v) => v.capacity_unit ?? ""],
  ["supply_frequency", (v) => v.supply_frequency ?? ""],
  ["average_animal_weight_kg", (v) => v.average_animal_weight_kg ?? ""],
];

// currentAnswers: what the vendor already carries, keyed by question id -- typed values from the
// row, extras from `answers`.
function currentAnswers(vendor: ProcurementVendor | null): Answers {
  if (!vendor) return { status: "active" };
  const out: Answers = {};
  for (const [id, read] of TYPED_CARRY) out[id] = read(vendor);
  for (const [k, v] of Object.entries(vendor.answers ?? {})) out[k] = v;
  return out;
}

function questionsOf(form: ProcurementVendorForm): ProcurementVendorQuestion[] {
  return form.pages.flatMap((p) => p.questions);
}

export function VendorFormFields({ form, vendor, pageContract }: { form: ProcurementVendorForm; vendor: ProcurementVendor | null; pageContract: AdminUiPageContract }) {
  // Only pick-one answers are tracked live, for "ask only when" visibility; everything else is
  // an uncontrolled input the server action reads off the form.
  const [picked, setPicked] = useState<Answers>(() => {
    const init = currentAnswers(vendor);
    const out: Answers = {};
    for (const q of questionsOf(form)) if (q.kind === "choice") out[q.id] = init[q.id] ?? "";
    return out;
  });
  const initial = currentAnswers(vendor);
  const asked = new Set(questionsOf(form).map((q) => q.id));
  const visibleIds = visibleQuestionIds(questionsOf(form), picked);
  const visible = (q: ProcurementVendorQuestion) => visibleIds.has(q.id);

  return (
    <>
      <div className="note">{copy(pageContract, "required.hint.form")}</div>
      <input type="hidden" name="questionnaire_version" value={form.version} />
      <input type="hidden" name="questionnaire_sop_code" value={form.sop_code ?? "sales.vendor"} />
      <input type="hidden" name="form_question_ids" value={[...visibleIds].join(",")} />
      {vendor
        ? TYPED_CARRY.filter(([id]) => !asked.has(id)).map(([id, read]) => <input key={id} type="hidden" name={id} value={read(vendor)} />)
        : null}
      {form.pages.map((page, pi) => (
        <div key={page.key} className="vendor-form-page">
          {page.title || pi > 0 ? <div className="dgrp">{page.title}</div> : null}
          {page.hint ? <div className="muted small">{page.hint}</div> : null}
          {page.questions.map((q) => {
            if (!visible(q)) return null;
            const id = `vq-${q.id}`;
            const labelText = `${q.title}${q.required ? " *" : ""}`;
            const hint = q.hint ? <div className="muted small">{q.hint}</div> : null;
            const value = initial[q.id] ?? "";
            switch (q.kind) {
              case "choice": {
                const options = q.options ?? [];
                // A stored value no longer offered (retired catalog entry) stays selectable so an
                // edit cannot silently re-save the row with a different value.
                const stale = value && !options.some((o) => o.value === value);
                return (
                  <div className="fld" key={q.id}>
                    <TextField
                      select
                      fullWidth
                      id={id}
                      name={q.id}
                      label={labelText}
                      required={q.required}
                      value={picked[q.id] ?? ""}
                      onChange={(e) => setPicked((s) => ({ ...s, [q.id]: e.target.value }))}
                      slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
                    >
                      <MenuItem value="" disabled={q.required}>
                        —
                      </MenuItem>
                      {stale ? <MenuItem value={value}>{value}</MenuItem> : null}
                      {options.map((o) => (
                        <MenuItem key={o.value} value={o.value}>
                          {o.label}
                        </MenuItem>
                      ))}
                    </TextField>
                    {q.allow_other && picked[q.id] === "other" ? (
                      <TextField
                        fullWidth
                        name={`${q.id}_other`}
                        placeholder={copy(pageContract, "hint.other")}
                        defaultValue={initial[`${q.id}_other`] ?? ""}
                        required
                        slotProps={{ htmlInput: { maxLength: 160 } }}
                      />
                    ) : null}
                    {hint}
                  </div>
                );
              }
              case "multi": {
                const chosen = new Set(value.split("|").map((x) => x.trim()).filter(Boolean));
                return (
                  <div className="fld" key={q.id}>
                    <label htmlFor={id}>{labelText}</label>
                    <div className="vendor-form-multi">
                      {(q.options ?? []).map((o) => (
                        <FormControlLabel
                          key={o.value}
                          className="chkline"
                          control={<Checkbox name={q.id} value={o.value} defaultChecked={chosen.has(o.value)} sx={{ p: { xs: 1.5, sm: 1 } }} />}
                          label={<>{o.label}</>}
                        />
                      ))}
                    </div>
                    {hint}
                  </div>
                );
              }
              case "number":
                return (
                  <div className="fld" key={q.id}>
                    <TextField
                      fullWidth
                      id={id}
                      name={q.id}
                      type="number"
                      label={labelText}
                      required={q.required}
                      defaultValue={value}
                      placeholder={q.unit ?? ""}
                      slotProps={{
                        htmlInput: { inputMode: "decimal", step: "any", min: q.min, max: q.max },
                        inputLabel: { shrink: true },
                      }}
                    />
                    {hint}
                  </div>
                );
              default:
                return (
                  <div className="fld" key={q.id}>
                    <TextField
                      fullWidth
                      id={id}
                      name={q.id}
                      label={labelText}
                      required={q.required}
                      defaultValue={value}
                      multiline={q.id === "comments" || q.id === "details"}
                      rows={q.id === "comments" || q.id === "details" ? 2 : undefined}
                      slotProps={{
                        htmlInput: { maxLength: q.id === "comments" || q.id === "details" ? 2000 : 160 },
                        inputLabel: { shrink: true },
                      }}
                    />
                    {hint}
                  </div>
                );
            }
          })}
        </div>
      ))}
    </>
  );
}

// vendorAnswerRows labels a vendor's extra answers (beyond the register's columns) by the form
// the drawer holds, in form order; an answer whose question the form no longer carries is listed
// under its key rather than dropped. Choice values render their labels.
export function vendorAnswerRows(form: ProcurementVendorForm | null, vendor: ProcurementVendor): Array<{ label: string; value: string }> {
  const answers = vendor.answers ?? {};
  const typed = new Set(TYPED_CARRY.map(([id]) => id));
  const out: Array<{ label: string; value: string }> = [];
  const seen = new Set<string>();
  for (const q of form ? questionsOf(form) : []) {
    if (typed.has(q.id)) continue;
    const raw = answers[q.id];
    if (!raw) continue;
    seen.add(q.id);
    let value = raw;
    if (q.kind === "choice" || q.kind === "multi") {
      value = raw
        .split("|")
        .map((v) => v.trim())
        .map((v) => (v === "other" && answers[`${q.id}_other`] ? answers[`${q.id}_other`] : (q.options ?? []).find((o) => o.value === v)?.label ?? v))
        .join(", ");
    } else if (q.kind === "number" && q.unit) {
      value = `${raw} ${q.unit}`;
    }
    out.push({ label: q.title, value });
  }
  for (const [k, v] of Object.entries(answers)) {
    if (seen.has(k) || typed.has(k) || k.endsWith("_other") || !v) continue;
    out.push({ label: k, value: v });
  }
  return out;
}
