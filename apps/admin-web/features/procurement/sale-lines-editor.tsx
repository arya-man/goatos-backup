"use client";

import Box from "@mui/material/Box";
import Paper from "@mui/material/Paper";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import IconButton from "@mui/material/IconButton";
import Typography from "@mui/material/Typography";
import FormHelperText from "@mui/material/FormHelperText";
import { Plus, Trash2 } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SalesProductOption } from "@/lib/api/procurement";
import { newSaleLine, saleLinesTotals, saleLineValue, type SaleLineDraft } from "./sale-lines";
import { countKey, inr, MAX_SALE_LINES, num } from "./sales-format";
import { FormSelect } from "./form-select";
import { listOptions } from "./option-utils";

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

  // Template form grid (user-quick-edit): one column on a phone, two/three from sm up.
  const grid2 = { display: "grid", gap: 2, gridTemplateColumns: { xs: "minmax(0,1fr)", sm: "repeat(2, minmax(0,1fr))" } } as const;
  const grid3 = { display: "grid", gap: 2, gridTemplateColumns: { xs: "minmax(0,1fr)", sm: "repeat(3, minmax(0,1fr))" } } as const;

  return (
    <Stack spacing={2} sx={{ minWidth: 0 }}>
      <Typography variant="subtitle2">{copy(pageContract, "section.lines.title")}</Typography>

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
          <Paper
            variant="outlined"
            component="fieldset"
            key={line.id}
            data-testid="sale-line"
            data-kind={product?.kind ?? ""}
            sx={{ m: 0, p: 2, minWidth: 0 }}
          >
            <Stack spacing={2}>
              <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                <Typography variant="subtitle2" sx={{ flex: 1, minWidth: 0 }}>
                  {copy(pageContract, "label.line")} {index + 1}
                </Typography>
                {lines.length > 1 ? (
                  <IconButton
                    size="small"
                    onClick={() => remove(line.id)}
                    aria-label={`${copy(pageContract, "action.remove_line")} ${index + 1}`}
                    title={copy(pageContract, "action.remove_line")}
                    sx={{ minWidth: { xs: "var(--tap-min)", sm: 0 }, minHeight: { xs: "var(--tap-min)", sm: 0 } }}
                  >
                    <Trash2 size={18} aria-hidden="true" />
                  </IconButton>
                ) : null}
              </Box>
              <Box sx={grid2}>
                <FormSelect
                  label={copy(pageContract, "field.product_type")}
                  id={`s-line-product-${line.id}`}
                  name={`line_product_type_${index}`}
                  required
                  fullWidth
                  value={line.product}
                  // Changing the product empties everything it decided the shape of: a breed from
                  // the previous product's vocabulary, or kilograms typed against a feed, must
                  // never ride along into the submit of a different product.
                  onValueChange={(next) =>
                    update(line.id, {
                      product: next,
                      breed: "",
                      quantity: "",
                      rate: "",
                      animals: "",
                      weightKg: "",
                      value: "",
                    })
                  }
                  options={listOptions(products, (option) => option.name, (option) => option.name)}
                />
                {variantIsItself ? (
                  <input type="hidden" name={`line_breed_${index}`} value={line.product} />
                ) : (
                  <FormSelect
                    label={copy(pageContract, isFeed ? "field.line_feed_item" : "field.breed")}
                    id={`s-line-breed-${line.id}`}
                    name={`line_breed_${index}`}
                    required
                    fullWidth
                    value={variantValid ? line.breed : ""}
                    onValueChange={(next) => update(line.id, { breed: next })}
                    options={listOptions(variantOptions, (option) => option, (option) => option, "—")}
                  />
                )}
              </Box>
              {pricedPerUnit ? (
                // A feed sale is kilograms at a rate. The value is a READOUT, not an input: it is
                // computed here exactly as the backend computes it, so the figure the operator
                // watches is the figure that gets recorded.
                <Box sx={grid3}>
                  <TextField
                    fullWidth
                    id={`s-line-quantity-${line.id}`}
                    name={`line_quantity_${index}`}
                    label={copy(pageContract, product?.unit === "number" ? "field.line_count" : "field.line_quantity")}
                    type="number"
                    required
                    value={line.quantity}
                    onChange={(event) => update(line.id, { quantity: event.target.value })}
                    slotProps={{ htmlInput: { inputMode: "decimal", min: 0, step: product?.unit === "number" ? 1 : 0.001 } }}
                  />
                  <TextField
                    fullWidth
                    id={`s-line-rate-${line.id}`}
                    name={`line_rate_per_unit_${index}`}
                    label={copy(pageContract, product?.unit === "number" ? "field.line_rate_each" : "field.line_rate_per_unit")}
                    type="number"
                    required
                    value={line.rate}
                    onChange={(event) => update(line.id, { rate: event.target.value })}
                    slotProps={{ htmlInput: { inputMode: "decimal", min: 0, step: "0.01" } }}
                  />
                  <Box sx={{ minWidth: 0 }}>
                    <Typography variant="caption" component="label" htmlFor={`s-line-value-readout-${line.id}`} sx={{ color: "text.secondary", display: "block" }}>
                      {copy(pageContract, "field.line_sales_value")}
                    </Typography>
                    <Typography variant="subtitle1" component="output" id={`s-line-value-readout-${line.id}`} data-testid="sale-line-computed-value" sx={{ display: "block" }}>
                      {inr(computed)}
                    </Typography>
                    <FormHelperText sx={{ mx: 0 }}>{copy(pageContract, "hint.line_feed_value")}</FormHelperText>
                  </Box>
                </Box>
              ) : (
                <Box sx={grid3}>
                  <TextField
                    fullWidth
                    id={`s-line-animals-${line.id}`}
                    name={`line_animal_count_${index}`}
                    label={copy(pageContract, "field.line_animal_count")}
                    type="number"
                    value={line.animals}
                    onChange={(event) => update(line.id, { animals: event.target.value })}
                    slotProps={{ htmlInput: { inputMode: "numeric", min: 0, step: 1 } }}
                  />
                  <TextField
                    fullWidth
                    id={`s-line-weight-${line.id}`}
                    name={`line_total_weight_kg_${index}`}
                    label={copy(pageContract, "field.line_total_weight_kg")}
                    type="number"
                    value={line.weightKg}
                    onChange={(event) => update(line.id, { weightKg: event.target.value })}
                    slotProps={{ htmlInput: { inputMode: "decimal", min: 0, step: "0.01" } }}
                  />
                  <TextField
                    fullWidth
                    id={`s-line-value-${line.id}`}
                    name={`line_sales_value_${index}`}
                    label={copy(pageContract, "field.line_sales_value")}
                    type="number"
                    required
                    value={line.value}
                    onChange={(event) => update(line.id, { value: event.target.value })}
                    slotProps={{ htmlInput: { inputMode: "decimal", min: 1, step: "0.01" } }}
                  />
                </Box>
              )}
            </Stack>
          </Paper>
        );
      })}

      <Box sx={{ display: "flex", alignItems: "center", gap: 2, flexWrap: "wrap" }}>
        <Button
          variant="outlined"
          color="inherit"
          size="small"
          startIcon={<Plus size={16} aria-hidden="true" />}
          onClick={add}
          disabled={!canAdd}
          aria-disabled={!canAdd}
        >
          {copy(pageContract, "action.add_line")}
        </Button>
        <Box
          data-testid="sale-lines-total"
          aria-live="polite"
          sx={{ ml: "auto", display: "flex", flexDirection: "column", alignItems: "flex-end", textAlign: "right", minWidth: 0 }}
        >
          <Typography variant="overline" sx={{ color: "text.secondary" }}>{copy(pageContract, "summary.lines.total")}</Typography>
          <Typography variant="h6" component="strong">{inr(totals.value)}</Typography>
          <Typography variant="caption" sx={{ color: "text.secondary" }}>
            {num(totals.animals)} {copy(pageContract, countKey(totals.animals, "summary.lines.animal", "summary.lines.animals"))}
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
                {num(totals.pieces)} {copy(pageContract, countKey(totals.pieces, "summary.lines.piece", "summary.lines.pieces"))}
              </>
            ) : null}{" "}
            · {lines.length} {copy(pageContract, countKey(lines.length, "summary.lines.line", "summary.lines.lines"))}
          </Typography>
        </Box>
      </Box>
    </Stack>
  );
}
