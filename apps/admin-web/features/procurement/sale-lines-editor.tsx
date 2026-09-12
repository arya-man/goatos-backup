"use client";

import { Plus, Trash2 } from "lucide-react";

import { copy, optionalOptionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { newSaleLine, saleLinesTotals, type SaleLineDraft } from "./sale-lines";
import { inr, MAX_SALE_LINES, num } from "./sales-format";

/**
 * The "What was sold" block of the record-sale drawer (maintainer decision 2026-09-12): one card
 * per product/breed, each with its own animals, weight and value, and a running total underneath.
 *
 * Posts as indexed hidden-name fields (`line_product_type_0`...), read back in order by
 * readSaleForm. Every label is backend copy; this component owns layout and local edit state only.
 */
export function SaleLinesEditor({
  lines,
  onChange,
  pageContract,
  productOptions,
}: {
  lines: SaleLineDraft[];
  onChange: (next: SaleLineDraft[]) => void;
  pageContract: AdminUiPageContract;
  productOptions: { key: string; label: string }[];
}) {
  const totals = saleLinesTotals(lines);
  const canAdd = lines.length < MAX_SALE_LINES;
  const nextId = lines.reduce((max, line) => Math.max(max, line.id), 0) + 1;

  const update = (id: number, patch: Partial<SaleLineDraft>) =>
    onChange(lines.map((line) => (line.id === id ? { ...line, ...patch } : line)));
  const remove = (id: number) => onChange(lines.filter((line) => line.id !== id));
  const add = () => {
    if (!canAdd) return;
    // A new line starts on the LAST line's product: a mixed sale is usually two breeds of one
    // product before it is two products, so the common case needs one fewer click.
    onChange([...lines, newSaleLine(nextId, lines[lines.length - 1]?.product ?? productOptions[0]?.key ?? "")]);
  };

  return (
    <div className="sales-lines">
      <div className="dgrp">{copy(pageContract, "section.lines.title")}</div>
      <div className="note">{copy(pageContract, "hint.lines")}</div>

      {lines.map((line, index) => {
        const breedOptions = optionalOptionGroup(pageContract, `sales_breeds_${line.product.toLowerCase()}`);
        const breedValid = breedOptions.some((option) => option.key === line.breed);
        return (
          <fieldset className="sales-line" key={line.id} data-testid="sale-line">
            <div className="sales-line-hd">
              <span className="mt">
                {copy(pageContract, "label.line")} {index + 1}
              </span>
              <span className="sp" style={{ flex: 1 }} />
              {lines.length > 1 ? (
                <button
                  type="button"
                  className="btn sm"
                  onClick={() => remove(line.id)}
                  aria-label={`${copy(pageContract, "action.remove_line")} ${index + 1}`}
                >
                  <Trash2 className="ic" aria-hidden="true" />
                  {copy(pageContract, "action.remove_line")}
                </button>
              ) : null}
            </div>
            <div className="sales-line-grid">
              <div className="fld">
                <label htmlFor={`s-line-product-${line.id}`}>{copy(pageContract, "field.product_type")}</label>
                <select
                  id={`s-line-product-${line.id}`}
                  name={`line_product_type_${index}`}
                  required
                  value={line.product}
                  // Changing the product empties the breed: a breed from the previous product's
                  // vocabulary must never ride along into the submit.
                  onChange={(event) => update(line.id, { product: event.target.value, breed: "" })}
                >
                  {productOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <div className="fld">
                <label htmlFor={`s-line-breed-${line.id}`}>{copy(pageContract, "field.breed")}</label>
                <select
                  id={`s-line-breed-${line.id}`}
                  name={`line_breed_${index}`}
                  required
                  value={breedValid ? line.breed : ""}
                  onChange={(event) => update(line.id, { breed: event.target.value })}
                >
                  <option value="" disabled>
                    —
                  </option>
                  {breedOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
            </div>
            <div className="sales-line-grid sales-line-grid-3">
              <div className="fld">
                <label htmlFor={`s-line-animals-${line.id}`}>{copy(pageContract, "field.line_animal_count")}</label>
                <input
                  id={`s-line-animals-${line.id}`}
                  name={`line_animal_count_${index}`}
                  type="number"
                  inputMode="numeric"
                  min={0}
                  step={1}
                  value={line.animals}
                  onChange={(event) => update(line.id, { animals: event.target.value })}
                />
              </div>
              <div className="fld">
                <label htmlFor={`s-line-weight-${line.id}`}>{copy(pageContract, "field.line_total_weight_kg")}</label>
                <input
                  id={`s-line-weight-${line.id}`}
                  name={`line_total_weight_kg_${index}`}
                  type="number"
                  inputMode="decimal"
                  min={0}
                  step="0.01"
                  value={line.weightKg}
                  onChange={(event) => update(line.id, { weightKg: event.target.value })}
                />
              </div>
              <div className="fld">
                <label htmlFor={`s-line-value-${line.id}`}>{copy(pageContract, "field.line_sales_value")}</label>
                <input
                  id={`s-line-value-${line.id}`}
                  name={`line_sales_value_${index}`}
                  type="number"
                  inputMode="decimal"
                  min={1}
                  step="0.01"
                  required
                  value={line.value}
                  onChange={(event) => update(line.id, { value: event.target.value })}
                />
              </div>
            </div>
          </fieldset>
        );
      })}

      <div className="sales-line-foot">
        <button type="button" className="btn sm" onClick={add} disabled={!canAdd} aria-disabled={!canAdd}>
          <Plus className="ic" aria-hidden="true" />
          {copy(pageContract, "action.add_line")}
        </button>
        <span className="sp" style={{ flex: 1 }} />
        <div className="sales-line-total" data-testid="sale-lines-total" aria-live="polite">
          <span className="k">{copy(pageContract, "summary.lines.total")}</span>
          <strong>{inr(totals.value)}</strong>
          <span className="muted small">
            {num(totals.animals)} {copy(pageContract, "summary.lines.animals")} · {num(totals.weightKg, 1)}{" "}
            {copy(pageContract, "summary.lines.weight")} · {lines.length} {copy(pageContract, "summary.lines.lines")}
          </span>
        </div>
      </div>
    </div>
  );
}
