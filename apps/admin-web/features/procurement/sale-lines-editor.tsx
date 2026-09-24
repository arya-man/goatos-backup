"use client";

import { Plus, Trash2 } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesProductOption } from "@/lib/api/procurement";
import { newSaleLine, saleLinesTotals, saleLineValue, type SaleLineDraft } from "./sale-lines";
import { inr, MAX_SALE_LINES, num } from "./sales-format";

/**
 * The "What was sold" block of the record-sale drawer (maintainer decision 2026-09-12): one card
 * per product line, each with its own figures, and a running total underneath.
 *
 * WHAT A LINE ASKS FOR IS DECIDED BY THE PRODUCT'S KIND (migration 000422), never by its name.
 * An animal line asks for a breed, a head count, a weight and a value; a FEED line asks which
 * feed, how many kilograms and at what rate, and works the value out. So the farm adding "Hay"
 * to its registry as another `other` product, or a new feed to its catalogue, changes this screen
 * with no edit here at all.
 *
 * The products and their variants come from /sales/options -- the same answer the phone's
 * record-sale form reads -- rather than from this page's contract, because two copies of one
 * vocabulary are two vocabularies waiting to disagree.
 *
 * Posts as indexed hidden-name fields (`line_product_type_0`...), read back in order by
 * readSaleForm. Every label is backend copy; this component owns layout and local edit state only.
 */
export function SaleLinesEditor({
  lines,
  onChange,
  pageContract,
  products,
  variants,
}: {
  lines: SaleLineDraft[];
  onChange: (next: SaleLineDraft[]) => void;
  pageContract: AdminUiPageContract;
  products: SalesProductOption[];
  variants: Record<string, string[]>;
}) {
  // What each product measures, for the footer: kilograms of feed and counted pieces are different
  // facts and must not be added together.
  const productUnits = Object.fromEntries(products.map((p) => [p.name, { kind: p.kind, unit: p.unit }]));
  const totals = saleLinesTotals(lines, productUnits);
  const canAdd = lines.length < MAX_SALE_LINES;
  const nextId = lines.reduce((max, line) => Math.max(max, line.id), 0) + 1;

  const update = (id: number, patch: Partial<SaleLineDraft>) =>
    onChange(lines.map((line) => (line.id === id ? { ...line, ...patch } : line)));
  const remove = (id: number) => onChange(lines.filter((line) => line.id !== id));
  const add = () => {
    if (!canAdd) return;
    // A new line starts on the LAST line's product: a mixed sale is usually two breeds of one
    // product before it is two products, so the common case needs one fewer click.
    onChange([...lines, newSaleLine(nextId, lines[lines.length - 1]?.product ?? products[0]?.name ?? "")]);
  };

  return (
    <div className="sales-lines">
      <div className="dgrp">{copy(pageContract, "section.lines.title")}</div>
      <div className="note">{copy(pageContract, "hint.lines")}</div>

      {lines.map((line, index) => {
        const product = products.find((candidate) => candidate.name === line.product);
        const isFeed = product?.kind === "feed";
        // What the line ASKS is the backend's answer, not this component's reading of the kind:
        // an item the farm adds itself -- sheep tags by number -- asks the same quantity and rate
        // that feed does, and only animals are sold as a negotiated lot.
        const pricedPerUnit = product?.priced_per_unit ?? false;
        const variantOptions = variants[line.product] ?? [];
        // An item with no second dimension -- manure, tags -- has exactly one "variant" and it is
        // its own name. Offering a dropdown of one is a question with no answer to give, so the
        // value rides as a hidden field and the reader is not asked it.
        const variantIsItself = variantOptions.length === 1 && variantOptions[0] === line.product;
        const variantValid = variantOptions.includes(line.breed);
        const computed = saleLineValue(line);
        return (
          <fieldset className="sales-line" key={line.id} data-testid="sale-line" data-kind={product?.kind ?? ""}>
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
                  // Changing the product empties everything it decided the shape of: a breed from
                  // the previous product's vocabulary, or kilograms typed against a feed, must
                  // never ride along into the submit of a different product.
                  onChange={(event) =>
                    update(line.id, {
                      product: event.target.value,
                      breed: "",
                      quantity: "",
                      rate: "",
                      animals: "",
                      weightKg: "",
                      value: "",
                    })
                  }
                >
                  {products.map((option) => (
                    <option key={option.code} value={option.name}>
                      {option.name}
                    </option>
                  ))}
                </select>
              </div>
              {variantIsItself ? (
                <input type="hidden" name={`line_breed_${index}`} value={line.product} />
              ) : (
              <div className="fld">
                <label htmlFor={`s-line-breed-${line.id}`}>
                  {copy(pageContract, isFeed ? "field.line_feed_item" : "field.breed")}
                </label>
                <select
                  id={`s-line-breed-${line.id}`}
                  name={`line_breed_${index}`}
                  required
                  value={variantValid ? line.breed : ""}
                  onChange={(event) => update(line.id, { breed: event.target.value })}
                >
                  <option value="" disabled>
                    —
                  </option>
                  {variantOptions.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
              </div>
              )}
            </div>
            {pricedPerUnit ? (
              // A feed sale is kilograms at a rate. The value is a READOUT, not an input: it is
              // computed here exactly as the backend computes it, so the figure the operator
              // watches is the figure that gets recorded.
              <div className="sales-line-grid sales-line-grid-3">
                <div className="fld">
                  <label htmlFor={`s-line-quantity-${line.id}`}>
                    {copy(pageContract, product?.unit === "number" ? "field.line_count" : "field.line_quantity")}
                  </label>
                  <input
                    id={`s-line-quantity-${line.id}`}
                    name={`line_quantity_${index}`}
                    type="number"
                    inputMode="decimal"
                    min={0}
                    step={product?.unit === "number" ? 1 : 0.001}
                    required
                    value={line.quantity}
                    onChange={(event) => update(line.id, { quantity: event.target.value })}
                  />
                </div>
                <div className="fld">
                  <label htmlFor={`s-line-rate-${line.id}`}>
                    {copy(pageContract, product?.unit === "number" ? "field.line_rate_each" : "field.line_rate_per_unit")}
                  </label>
                  <input
                    id={`s-line-rate-${line.id}`}
                    name={`line_rate_per_unit_${index}`}
                    type="number"
                    inputMode="decimal"
                    min={0}
                    step="0.01"
                    required
                    value={line.rate}
                    onChange={(event) => update(line.id, { rate: event.target.value })}
                  />
                </div>
                <div className="fld">
                  <label htmlFor={`s-line-value-readout-${line.id}`}>
                    {copy(pageContract, "field.line_sales_value")}
                  </label>
                  <output
                    id={`s-line-value-readout-${line.id}`}
                    className="sales-line-value"
                    data-testid="sale-line-computed-value"
                  >
                    {inr(computed)}
                  </output>
                  <div className="note">{copy(pageContract, "hint.line_feed_value")}</div>
                </div>
              </div>
            ) : (
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
                  <label htmlFor={`s-line-weight-${line.id}`}>
                    {copy(pageContract, "field.line_total_weight_kg")}
                  </label>
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
            )}
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
            {num(totals.animals)} {copy(pageContract, "summary.lines.animals")}
            {/* Non-feed weight is dropped only when there is feed weight or counted items to show: an
                animal-only sale keeps the "0.0 kg" it has always shown, while a feed-only sale
                stops claiming zero kilograms of animal beside the kilograms it actually sold. */}
            {totals.weightKg > 0 || (totals.feedKg === 0 && totals.pieces === 0) ? (
              <>
                {" · "}
                {num(totals.weightKg, 1)} {copy(pageContract, "summary.lines.weight")}
              </>
            ) : null}
            {/* Sold feed is kilograms too, but NOT live weight -- shown as its own term, and only
                when there is any, so an animal-only sale reads exactly as it did before. */}
            {totals.feedKg > 0 ? (
              <>
                {" · "}
                {num(totals.feedKg, 1)} {copy(pageContract, "summary.lines.feed_kg")}
              </>
            ) : null}
            {/* Counted items -- tags, and anything else the farm sells by the piece. */}
            {totals.pieces > 0 ? (
              <>
                {" · "}
                {num(totals.pieces)} {copy(pageContract, "summary.lines.pieces")}
              </>
            ) : null}{" "}
            · {lines.length} {copy(pageContract, "summary.lines.lines")}
          </span>
        </div>
      </div>
    </div>
  );
}
