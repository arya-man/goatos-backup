"use client";

// WHAT THE FARM SELLS, authored on Sales Config (maintainer instruction 2026-09-23).
//
// "In future i will sell tags of sheep also so it should be configurable in sales config -- i will
// add item and how i will sell, whether in kg's or item numbers -- while registering sales i need
// to see all this in dropdown." So this is the list the record-sale form offers, maintained by a
// person: add "Sheep tags", sold by number, and it is in that dropdown the same minute.
//
// EVERY ROW IS ITS OWN FORM, always editable and ALWAYS SELECTABLE. An earlier pass greyed the
// three built-in rows out of changing kind and being switched off; the maintainer asked why, and
// the fear was overstated -- a sale line is stamped with the code, name and kind it was sold
// under, so editing the list changes what the NEXT sale asks for and nothing that is recorded.
//
// The list is CLIENT STATE seeded from the server: each save returns its row and is applied in
// place, so the table updates without the page re-rendering under the reader. `onProductsChanged`
// hands the same list up, which is how the record-sale drawer's dropdown learns about a new item
// without a reload.
//
// This file composes NO copy: every word arrives resolved from the page contract, and the two
// vocabularies (what an item is, how it is sold) arrive from the backend with the rows.
import { useState } from "react";
import { useActionState } from "react";
import { Plus, Tag as TagIcon, Trash2 } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SellableProduct, SellableProductPage } from "@/lib/api/procurement";
import { saveSellableProductAction, type SellableProductActionState } from "./sales-actions";

const INITIAL: SellableProductActionState = { status: "idle", code: "", ticket: 0 };

/** One row of the list, or the blank row that adds a new item. */
function ProductRow({
  product,
  page,
  pageContract,
  canWrite,
  onSaved,
  onDeleted,
}: {
  product?: SellableProduct;
  page: SellableProductPage;
  pageContract: AdminUiPageContract;
  canWrite: boolean;
  onSaved: (row: SellableProduct) => void;
  onDeleted: (code: string) => void;
}) {
  const [state, formAction, pending] = useActionState(
    async (previous: SellableProductActionState, formData: FormData) => {
      const next = await saveSellableProductAction(previous, formData);
      if (next.status === "success" && next.deletedCode) onDeleted(next.deletedCode);
      if (next.status === "success" && next.product) {
        onSaved({ ...(next.product as SellableProduct), priced_per_unit: next.product.priced_per_unit });
      }
      return next;
    },
    INITIAL,
  );
  const adding = product === undefined;
  const id = product?.code ?? "new";
  const outcome = state.status === "idle" ? "" : copy(pageContract, `action.${state.code}`);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <form action={formAction} className="sellable-product-row" aria-busy={pending} data-testid="sellable-product-row">
        {/* The code is the row's identity: present on an edit, absent when adding. It is what
            makes a rename an EDIT rather than a second item. */}
        {product ? <input type="hidden" name="code" value={product.code} /> : null}
        <div className="fld">
          <label htmlFor={`sp-name-${id}`}>{copy(pageContract, "field.product_name")}</label>
          <input id={`sp-name-${id}`} name="name" required maxLength={60} defaultValue={product?.name ?? ""} disabled={!canWrite} />
        </div>
        <div className="fld">
          <label htmlFor={`sp-kind-${id}`}>{copy(pageContract, "field.product_kind")}</label>
          <select id={`sp-kind-${id}`} name="kind" required defaultValue={product?.kind ?? page.kinds[0]?.key} disabled={!canWrite}>
            {page.kinds.map((choice) => (
              <option key={choice.key} value={choice.key} title={choice.hint}>
                {choice.label}
              </option>
            ))}
          </select>
        </div>
        <div className="fld">
          <label htmlFor={`sp-unit-${id}`}>{copy(pageContract, "field.product_unit")}</label>
          <select id={`sp-unit-${id}`} name="unit" required defaultValue={product?.unit ?? page.units[0]?.key} disabled={!canWrite}>
            {page.units.map((choice) => (
              <option key={choice.key} value={choice.key} title={choice.hint}>
                {choice.label}
              </option>
            ))}
          </select>
        </div>
        <div className="fld sellable-product-order">
          <label htmlFor={`sp-order-${id}`}>{copy(pageContract, "field.product_sort_order")}</label>
          <input id={`sp-order-${id}`} name="sort_order" type="number" min={0} step={1} defaultValue={product?.sort_order ?? 100} disabled={!canWrite} />
        </div>
        {/* The console's own checkbox line -- NOT inside a .fld, whose label styling turned this
            into a small-caps field header with a bare box beside it. */}
        <label className="chkline sellable-product-inuse" htmlFor={`sp-inuse-${id}`}>
          <input
            id={`sp-inuse-${id}`}
            name="in_use"
            type="checkbox"
            value="1"
            defaultChecked={product ? product.status === "active" : true}
            disabled={!canWrite}
          />
          {copy(pageContract, "status.product_active")}
        </label>
        {/* An animal item's breeds are its species'; the field rides along unchanged so an edit
            cannot silently drop it. */}
        {product?.species_code ? <input type="hidden" name="species_code" value={product.species_code} /> : null}
        <div className="sellable-product-actions">
          <button type="submit" className="btn sm primary" disabled={!canWrite || pending}>
            {adding ? <Plus className="ic" aria-hidden="true" /> : null}
            {copy(pageContract, "action.save_sellable_product")}
          </button>
          {/* Deleting is offered on every saved row. The backend refuses when the farm has SOLD
              any of it, and its refusal says to switch the item off instead -- so the button is
              never a trap, and the sentence explaining that is backend copy like every other. */}
          {product ? (
            <button
              type="submit"
              name="intent"
              value="delete"
              className="btn sm"
              disabled={!canWrite || pending}
              aria-label={`${copy(pageContract, "action.delete_sellable_product")} ${product.name}`}
            >
              <Trash2 className="ic" aria-hidden="true" />
              {copy(pageContract, "action.delete_sellable_product")}
            </button>
          ) : null}
        </div>
      </form>
      {outcome ? (
        <div role="status" style={{ fontSize: 12, color: state.status === "success" ? "var(--ok)" : "var(--danger)" }}>
          {outcome}
        </div>
      ) : null}
    </div>
  );
}

export function SellableProductsSection({
  page,
  pageContract,
  canWrite,
  disabledReason,
  onProductsChanged,
}: {
  /** The registry, archived rows included. `null` when the read failed. */
  page: SellableProductPage | null;
  pageContract: AdminUiPageContract;
  canWrite: boolean;
  disabledReason: string;
  /** Hands the current list up so the record-sale dropdown sees an edit without a reload. */
  onProductsChanged?: (products: SellableProduct[]) => void;
}) {
  const [rows, setRows] = useState<SellableProduct[]>(page?.products ?? []);
  if (!page) return null;

  const publish = (next: SellableProduct[]) => {
    const ordered = [...next].sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name));
    setRows(ordered);
    onProductsChanged?.(ordered);
  };
  const applySaved = (row: SellableProduct) =>
    publish(rows.some((r) => r.code === row.code) ? rows.map((r) => (r.code === row.code ? { ...r, ...row } : r)) : [...rows, row]);
  const applyDeleted = (code: string) => publish(rows.filter((r) => r.code !== code));

  const active = rows.filter((p) => p.status === "active").length;
  return (
    <section className="card" aria-label={copy(pageContract, "section.sellable_products.aria")}>
      <div className="hd">
        <TagIcon className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.sellable_products.title")}</h3>
        <Tag tone={active ? "info" : "mut"}>{active}</Tag>
      </div>
      <div className="bd">
        <div className="note">{copy(pageContract, "section.sellable_products.caption")}</div>
        {canWrite ? null : <div className="note warn">{disabledReason}</div>}
        {rows.length === 0 ? (
          <div className="note">{copy(pageContract, "empty.sellable_products")}</div>
        ) : (
          rows.map((product) => (
            <ProductRow
              key={product.code}
              product={product}
              page={page}
              pageContract={pageContract}
              canWrite={canWrite}
              onSaved={applySaved}
              onDeleted={applyDeleted}
            />
          ))
        )}
        {canWrite ? (
          <>
            <div className="dgrp">{copy(pageContract, "drawer.sellable_product.title")}</div>
            {/* Keyed by how many rows exist, so a successful add gives the blank row a fresh
                identity and empties itself instead of keeping the item just added. */}
            <ProductRow
              key={`new-${rows.length}`}
              page={page}
              pageContract={pageContract}
              canWrite
              onSaved={applySaved}
              onDeleted={applyDeleted}
            />
            <div className="note">{copy(pageContract, "hint.product_code")}</div>
          </>
        ) : null}
      </div>
    </section>
  );
}
