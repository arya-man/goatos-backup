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

import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Divider from "@mui/material/Divider";
import MenuItem from "@mui/material/MenuItem";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { EmptyState } from "@/components/app/empty-state";
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
    <Stack spacing={0.5}>
      <Box
        component="form"
        action={formAction}
        className="sellable-product-row"
        aria-busy={pending}
        // Template form row: TextFields on a wrapping grid (one column on a phone), never native
        // selects squeezed to their content (FJ3 P1-3 "Feed from the store" overflowed its select).
        sx={{
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
          gap: 2,
          "& > .MuiTextField-root": { flex: { xs: "1 1 100%", sm: "1 1 160px" }, minWidth: 0 },
          "& > .sellable-product-actions": { ml: { sm: "auto" } },
        }}
        data-testid="sellable-product-row"
        // The row's stable handle: its registry code, or "new" for the blank one. A test (or a
        // person reading the DOM) can name a row without matching on a typed-in value, which
        // stops reflecting the field the moment somebody edits it.
        data-code={product?.code ?? "new"}
      >
        {/* The code is the row's identity: present on an edit, absent when adding. It is what
            makes a rename an EDIT rather than a second item. */}
        {product ? <input type="hidden" name="code" value={product.code} /> : null}
        <TextField
          id={`sp-name-${id}`}
          size="small"
          name="name"
          required
          label={copy(pageContract, "field.product_name")}
          defaultValue={product?.name ?? ""}
          disabled={!canWrite}
          slotProps={{ htmlInput: { maxLength: 60 } }}
        />
          {/* UNCONTROLLED on purpose. What this select POSTS must be what the reader chose or what
              was saved -- nothing else. Held as controlled state it followed a re-render instead,
              and a row whose state had drifted posted the drift: an item edited twice saved the
              first edit's kind the second time. The state below is for DISPLAY only (which fields
              the row shows), and the key above re-seeds this default whenever the saved row
              changes. */}
        <TextField
          select
          id={`sp-kind-${id}`}
          size="small"
          name="kind"
          required
          label={copy(pageContract, "field.product_kind")}
          defaultValue={product?.kind ?? page.kinds[0]?.key}
          onChange={(event) => setKind(event.target.value)}
          disabled={!canWrite}
        >
          {page.kinds.map((choice) => (
            <MenuItem key={choice.key} value={choice.key} title={choice.hint}>
              {choice.label}
            </MenuItem>
          ))}
        </TextField>
        {/* Uncontrolled for the same reason as the kind above. */}
        <TextField
          select
          id={`sp-unit-${id}`}
          size="small"
          name="unit"
          required
          label={copy(pageContract, "field.product_unit")}
          defaultValue={product?.unit ?? page.units[0]?.key}
          disabled={!canWrite}
        >
          {page.units.map((choice) => (
            <MenuItem key={choice.key} value={choice.key} title={choice.hint}>
              {choice.label}
            </MenuItem>
          ))}
        </TextField>
        {/* There is no Order field: the maintainer asked for it gone. An item keeps the place it
            already has, and a new one is appended by the backend, so the list stays stable
            without anybody being asked to number it. */}
        {product ? <input type="hidden" name="sort_order" value={product.sort_order} /> : null}
        {/* The console's own checkbox line -- NOT inside a .fld, whose label styling turned this
            into a small-caps field header with a bare box beside it. */}
        <FormControlLabel
          className="sellable-product-inuse"
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
          <Typography variant="caption" className="sellable-product-covers" sx={{ flexBasis: "100%", color: "text.secondary" }}>
            {copy(pageContract, "hint.product_feed_covers")} {page.feed_items.join(", ")}
          </Typography>
        ) : null}
        {/* AN ANIMAL MUST SAY WHICH SPECIES IT IS, because the sale's breed list is that species'
            breeds -- an animal item naming none resolves to no breeds, and since the sale requires
            one, the item saves cleanly and can never be sold. The list is the breed register's own
            species, so the editor cannot offer one with nothing behind it. A species already saved
            rides along unchanged, so an edit cannot silently drop it. */}
        {kind === "animal" ? (
          <TextField
            select
            id={`sp-species-${id}`}
            size="small"
            name="species_code"
            required
            label={copy(pageContract, "field.product_species")}
            defaultValue={product?.species_code ?? ""}
            disabled={!canWrite}
            slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
          >
            <MenuItem value="">{copy(pageContract, "field.product_species.pick")}</MenuItem>
            {page.species.map((code) => (
              <MenuItem key={code} value={code}>
                {code}
              </MenuItem>
            ))}
          </TextField>
        ) : product?.species_code ? (
          <input type="hidden" name="species_code" value={product.species_code} />
        ) : null}
        <Stack direction="row" className="sellable-product-actions" sx={{ gap: 1, flexWrap: "wrap", justifyContent: { sm: "flex-end" } }}>
          <Button type="submit" variant="contained" color="primary" disabled={!canWrite || pending} startIcon={adding ? <Iconify icon="mingcute:add-line" /> : undefined}>
            {copy(pageContract, "action.save_sellable_product")}
          </Button>
          {/* Deleting is offered on every saved row. The backend refuses when the farm has SOLD
              any of it, and its refusal says to switch the item off instead -- so the button is
              never a trap, and the sentence explaining that is backend copy like every other. */}
          {product ? (
            <Button
              type="submit"
              name="intent"
              value="delete"
              variant="outlined"
              color="error"
              disabled={!canWrite || pending}
              aria-label={`${copy(pageContract, "action.delete_sellable_product")} ${product.name}`}
              startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}
            >
              {copy(pageContract, "action.delete_sellable_product")}
            </Button>
          ) : null}
        </Stack>
      </Box>
      {outcome ? (
        <Typography role="status" variant="caption" sx={{ color: state.status === "success" ? "success.main" : "error.main" }}>
          {outcome}
        </Typography>
      ) : null}
    </Stack>
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
    <Card component="section" aria-label={copy(pageContract, "section.sellable_products.aria")}>
      <CardHeader
        title={
          <Stack direction="row" sx={{ alignItems: "center", gap: 1 }}>
            {copy(pageContract, "section.sellable_products.title")}
            <Label variant="soft" color={active ? "info" : "default"}>{active}</Label>
          </Stack>
        }
        subheader={copy(pageContract, "section.sellable_products.caption")}
      />
      <Stack spacing={2.5} sx={{ p: 3 }} divider={<Divider flexItem sx={{ borderStyle: "dashed" }} />}>
        {canWrite ? null : <Alert severity="info">{disabledReason}</Alert>}
        {rows.length === 0 ? (
          <EmptyState title={copy(pageContract, "empty.sellable_products")} />
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
          <Stack spacing={2}>
            <Typography variant="subtitle2">{copy(pageContract, "drawer.sellable_product.title")}</Typography>
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
            <Typography variant="caption" sx={{ color: "text.secondary" }}>{copy(pageContract, "hint.product_code")}</Typography>
          </Stack>
        ) : null}
      </Stack>
    </Card>
  );
}
