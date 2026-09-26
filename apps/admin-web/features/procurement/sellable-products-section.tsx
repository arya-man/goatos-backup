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
import { useRouter } from "next/navigation";
import { Plus, Tag as TagIcon, Trash2 } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { SellableProduct, SellableProductPage } from "@/lib/api/procurement";
import { saveSellableProductAction, type SellableProductActionState } from "./sales-actions";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

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
  const router = useRouter();
  const [state, formAction, pending] = useActionState(
    async (previous: SellableProductActionState, formData: FormData) => {
      const next = await saveSellableProductAction(previous, formData);
      if (next.status === "success" && next.deletedCode) onDeleted(next.deletedCode);
      if (next.status === "success" && next.product) {
        onSaved({ ...(next.product as SellableProduct), priced_per_unit: next.product.priced_per_unit });
      }
      // The row lands instantly from the answer above; this then re-reads the page's own data so
      // the SERVER stays the source of truth. Without it the client list is the only record of an
      // edit, and it drifts: an item added here and then edited twice posted the first edit's
      // values the second time, because nothing ever told the page what was really stored. The
      // ACTION deliberately does not revalidate -- a returning action that also revalidates
      // re-renders the page on top of the row just applied -- so the refresh is asked for here,
      // after the row is in place.
      if (next.status === "success") router.refresh();
      return next;
    },
    INITIAL,
  );
  const adding = product === undefined;
  const id = product?.code ?? "new";
  // The kind decides what the row SHOWS -- whether the feeds it covers are listed, and what the
  // sale will ask for -- so it is held here rather than read off the DOM.
  //
  // Its correctness comes from the row's KEY, which carries the saved shape (see the list below):
  // a save that changes what the row is remounts it onto the stored values, so this state cannot
  // drift from them. An earlier version kept the state across the save and a renamed row came back
  // reading Animals -- the select said one thing and the saved row another, and the NEXT save
  // posted the select's lie.
  const [kind, setKind] = useState(product?.kind ?? page.kinds[0]?.key ?? "animal");
  const outcome = state.status === "idle" ? "" : copy(pageContract, `action.${state.code}`);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      <form
        action={formAction}
        className="sellable-product-row"
        aria-busy={pending}
        data-testid="sellable-product-row"
        // The row's stable handle: its registry code, or "new" for the blank one. A test (or a
        // person reading the DOM) can name a row without matching on a typed-in value, which
        // stops reflecting the field the moment somebody edits it.
        data-code={product?.code ?? "new"}
      >
        {/* The code is the row's identity: present on an edit, absent when adding. It is what
            makes a rename an EDIT rather than a second item. */}
        {product ? <input type="hidden" name="code" value={product.code} /> : null}
        <div className="fld">
          <label htmlFor={`sp-name-${id}`}>{copy(pageContract, "field.product_name")}</label>
          <input id={`sp-name-${id}`} name="name" required maxLength={60} defaultValue={product?.name ?? ""} disabled={!canWrite} />
        </div>
        <div className="fld">
          <label htmlFor={`sp-kind-${id}`}>{copy(pageContract, "field.product_kind")}</label>
          {/* UNCONTROLLED on purpose. What this select POSTS must be what the reader chose or what
              was saved -- nothing else. Held as controlled state it followed a re-render instead,
              and a row whose state had drifted posted the drift: an item edited twice saved the
              first edit's kind the second time. The state below is for DISPLAY only (which fields
              the row shows), and the key above re-seeds this default whenever the saved row
              changes. */}
          <select
            id={`sp-kind-${id}`}
            name="kind"
            required
            defaultValue={product?.kind ?? page.kinds[0]?.key}
            onChange={(event) => setKind(event.target.value)}
            disabled={!canWrite}
          >
            {page.kinds.map((choice) => (
              <option key={choice.key} value={choice.key} title={choice.hint}>
                {choice.label}
              </option>
            ))}
          </select>
        </div>
        <div className="fld">
          <label htmlFor={`sp-unit-${id}`}>{copy(pageContract, "field.product_unit")}</label>
          {/* Uncontrolled for the same reason as the kind above. */}
          <select id={`sp-unit-${id}`} name="unit" required defaultValue={product?.unit ?? page.units[0]?.key} disabled={!canWrite}>
            {page.units.map((choice) => (
              <option key={choice.key} value={choice.key} title={choice.hint}>
                {choice.label}
              </option>
            ))}
          </select>
        </div>
        {/* There is no Order field: the maintainer asked for it gone. An item keeps the place it
            already has, and a new one is appended by the backend, so the list stays stable
            without anybody being asked to number it. */}
        {product ? <input type="hidden" name="sort_order" value={product.sort_order} /> : null}
        {/* The console's own checkbox line -- NOT inside a .fld, whose label styling turned this
            into a small-caps field header with a bare box beside it. */}
        <FormControlLabel
          className="chkline sellable-product-inuse"
          disabled={!canWrite}
          control={
            <Checkbox
              id={`sp-inuse-${id}`}
              name="in_use"
              value="1"
              defaultChecked={product ? product.status === "active" : true}
              sx={{ p: { xs: 1.5, sm: 1 } }}
            />
          }
          label={<>{copy(pageContract, "status.product_active")}</>}
        />
        {/* A feed item is a BUCKET: which feed is chosen on the sale, from the farm's configured
            feed list. Naming them here makes that visible -- the reader can see the row covers
            their Feed config and invents nothing. */}
        {kind === "feed" && page.feed_items.length ? (
          <div className="note sellable-product-covers">
            {copy(pageContract, "hint.product_feed_covers")} {page.feed_items.join(", ")}
          </div>
        ) : null}
        {/* AN ANIMAL MUST SAY WHICH SPECIES IT IS, because the sale's breed list is that species'
            breeds -- an animal item naming none resolves to no breeds, and since the sale requires
            one, the item saves cleanly and can never be sold. The list is the breed register's own
            species, so the editor cannot offer one with nothing behind it. A species already saved
            rides along unchanged, so an edit cannot silently drop it. */}
        {kind === "animal" ? (
          <div className="fld">
            <label htmlFor={`sp-species-${id}`}>{copy(pageContract, "field.product_species")}</label>
            <select id={`sp-species-${id}`} name="species_code" required defaultValue={product?.species_code ?? ""} disabled={!canWrite}>
              <option value="">{copy(pageContract, "field.product_species.pick")}</option>
              {page.species.map((code) => (
                <option key={code} value={code}>
                  {code}
                </option>
              ))}
            </select>
          </div>
        ) : product?.species_code ? (
          <input type="hidden" name="species_code" value={product.species_code} />
        ) : null}
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
  // The server's list wins when it changes: a save applies its row here for speed and then asks
  // the page to re-read, and this is what lets that answer land. Reset during render, never in an
  // effect -- an effect would let one save's values flash into the next render.
  const serverList = JSON.stringify(page?.products ?? []);
  const [syncedServerList, setSyncedServerList] = useState(serverList);
  if (serverList !== syncedServerList) {
    setSyncedServerList(serverList);
    setRows(page?.products ?? []);
  }
  if (!page) return null;

  const publish = (next: SellableProduct[]) => {
    const ordered = [...next].sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name));
    setRows(ordered);
    onProductsChanged?.(ordered);
  };
  // A merge must never overwrite a field the row HAS with one the answer lacks: spreading an
  // object with an undefined key erases the value that was there, and a row missing its kind reads
  // as the first kind in the list rather than as unknown.
  const merge = (current: SellableProduct, saved: SellableProduct): SellableProduct => {
    const defined = Object.fromEntries(Object.entries(saved).filter(([, v]) => v !== undefined));
    return { ...current, ...defined } as SellableProduct;
  };
  const applySaved = (row: SellableProduct) =>
    publish(rows.some((r) => r.code === row.code) ? rows.map((r) => (r.code === row.code ? merge(r, row) : r)) : [...rows, row]);
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
              // The saved shape is part of the row's identity: a save that changes what the item
              // IS remounts the row onto the stored values rather than leaving half-stale state
              // behind it. The name is deliberately NOT in the key -- retyping a name would
              // remount the row under the reader's cursor.
              key={`${product.code}:${product.kind}:${product.unit}:${product.status}`}
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
