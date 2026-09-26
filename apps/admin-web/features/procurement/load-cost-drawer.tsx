"use client";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import { Boxes } from "lucide-react";
import { useCallback, useId, useSyncExternalStore } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { DetailDrawer, DrawerBlock, DrawerMetaGrid, DrawerMetaItem, DrawerNote, DrawerTableScroll } from "@/components/app/detail-drawer";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LoadwiseLoad } from "@/lib/api/procurement";
import { humanDate, inr, num } from "./sales-format";
import { recordLoadCostAction } from "./sales-actions";
import Typography from "@mui/material/Typography";

/** Reads the selected load from the address bar. "" means the drawer is closed. */
function readCostLoadParam(): string {
  return new URL(window.location.href).searchParams.get("cost_load") ?? "";
}

function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

/**
 * The load-wise section's cost drawer: what one animal load cost to buy and bring in. Modeled on
 * the sales/feed drawers — CLIENT state driven by the URL (LocalOverlayLink changes history
 * WITHOUT an RSC request), rendered in the template temporary drawer (DetailDrawer: portal,
 * backdrop, header + close, Scrollbar body, footer actions).
 *
 * The form is a PUT of the full cost state: it opens prefilled with the load's recorded values, a
 * blank stays null (never coerced to 0), and clearing every box clears the recorded cost.
 */
export function LoadCostDrawer({
  loads,
  pageContract,
  listHref,
  canRecordCost,
}: {
  /** The rendered load-wise rows. The drawer opens from this data — it issues no fetch of its own. */
  loads: LoadwiseLoad[];
  pageContract: AdminUiPageContract;
  /** The list URL to restore on close (current tab/farm, without the cost_load param). */
  listHref: string;
  /** Backend-declared record_load_cost capability; without it the form never renders. */
  canRecordCost: boolean;
}) {
  const formId = useId();
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readCostLoadParam, () => "");
  const load = selection ? (loads.find((l) => l.load_id === selection) ?? null) : null;
  const open = load !== null;

  const close = useCallback(() => {
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(listHref);
  }, [listHref]);

  // Escape and the backdrop reach `close` through the template Drawer's onClose (focus is trapped in
  // the paper and returned to the opener); a second Escape listener would step history back twice.

  const none = copy(pageContract, "value.none");
  const field = (key: string) => copy(pageContract, `field.${key}`);
  const title = copy(pageContract, "drawer.load_cost.title");

  // One read-only cell pair of the record body.
  const cell = (label: string, value: string | number | null | undefined) => (
    <DrawerMetaItem key={label} label={label}>
      {value === null || value === undefined || value === "" ? none : value}
    </DrawerMetaItem>
  );

  const heading = load
    ? load.load_ref
      ? `${copy(pageContract, "column.load")} ${load.load_ref} · ${load.vendor_name || copy(pageContract, "value.none")}`
      : load.vendor_name.trim() === ""
        ? title
        : load.vendor_name
    : title;

  const costField = (name: "animal_cost" | "transport_cost" | "other_cost", value: number | null | undefined) => (
    <TextField
      fullWidth
      id={`lc-${name}`}
      name={name}
      type="number"
      label={field(name)}
      disabled={!canRecordCost}
      defaultValue={value ?? ""}
      slotProps={{ htmlInput: { min: 0, step: "0.01" }, inputLabel: { shrink: true } }}
    />
  );

  return (
    <DetailDrawer
      open={open}
      onClose={close}
      title={heading}
      eyebrow={copy(pageContract, "crumb")}
      icon={<Boxes aria-hidden="true" />}
      iconColors={{ bg: "var(--brand-soft)", fg: "var(--info)" }}
      subtitle={load?.purchase_date ? `${humanDate(load.purchase_date)}${load.farm ? ` · ${load.farm}` : ""}` : undefined}
      ariaLabel={title}
      closeLabel={copy(pageContract, "action.close")}
      footer={
        load ? (
          canRecordCost ? (
            <Button type="submit" form={formId} variant="contained" color="primary">
              {copy(pageContract, "action.record_load_cost.label")}
            </Button>
          ) : (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>{copy(pageContract, "disabled.load_cost")}</Typography>
          )
        ) : undefined
      }
    >
      {load ? (
        <Box component="form" id={formId} action={recordLoadCostAction} key={load.load_id} sx={{ display: "contents" }}>
          <input type="hidden" name="return_to" value={listHref} />
          <input type="hidden" name="load_id" value={load.load_id} />

          {/* The reconciliation the cost is being recorded against, read-only. */}
          <DrawerMetaGrid>
            {cell(copy(pageContract, "column.purchased"), num(load.purchased))}
            {cell(copy(pageContract, "column.sold"), num(load.sold))}
            {cell(copy(pageContract, "column.mortality"), num(load.mortality))}
            {cell(copy(pageContract, "column.remaining"), num(load.remaining))}
            {/* Culled / transferred / lost. The table has no column for it, so this is where
                a load that HAS other exits still shows them. */}
            {load.other_exits > 0 ? cell(copy(pageContract, "column.other_exits"), num(load.other_exits)) : null}
            {cell(
              copy(pageContract, "column.sold_value"),
              load.sold_value > 0 ? inr(Math.round(load.sold_value)) : none,
            )}
            <DrawerMetaItem label={copy(pageContract, "column.unaccounted")}>
              {load.unaccounted === 0 ? <Tag tone="mut">0</Tag> : <Tag tone="dng">{num(load.unaccounted)}</Tag>}
            </DrawerMetaItem>
          </DrawerMetaGrid>

          {/* The load's pre-system history, with the dates the old records span. */}
          {load.prior_sold || load.prior_dead ? (
            <DrawerBlock title={copy(pageContract, "loadwise.prior.title")}>
              {load.prior_sold ? (
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {copy(pageContract, "loadwise.prior.sold")}: <b>{num(load.prior_sold.count)}</b>{" "}
                  {copy(pageContract, "loadwise.prior.animals")}
                  {load.prior_sold.value ? <> · {inr(Math.round(load.prior_sold.value))}</> : null}
                  {load.prior_sold.first_on ? (
                    <>
                      {" "}· {humanDate(load.prior_sold.first_on)}
                      {load.prior_sold.last_on && load.prior_sold.last_on !== load.prior_sold.first_on
                        ? ` – ${humanDate(load.prior_sold.last_on)}`
                        : ""}
                    </>
                  ) : null}
                </Typography>
              ) : null}
              {load.prior_dead ? (
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {copy(pageContract, "loadwise.prior.died")}: <b>{num(load.prior_dead.count)}</b>{" "}
                  {copy(pageContract, "loadwise.prior.animals")}
                  {load.prior_dead.first_on ? (
                    <>
                      {" "}· {humanDate(load.prior_dead.first_on)}
                      {load.prior_dead.last_on && load.prior_dead.last_on !== load.prior_dead.first_on
                        ? ` – ${humanDate(load.prior_dead.last_on)}`
                        : ""}
                    </>
                  ) : null}
                </Typography>
              ) : null}
            </DrawerBlock>
          ) : null}

          {/* WHAT THE THREE FIGURES ARE MADE OF (maintainer decision 2026-09-01). The list
              stays three columns; the itemisation appears only here, on the opened load.
              Every label is backend-owned copy keyed on the line's KIND -- the kind string
              itself is never rendered. Absent for a load costed before the itemisation
              existed, which is why this block is conditional rather than an empty table:
              "no breakdown recorded" is a different fact from "no cost recorded", and the
              cost fields below already state the latter. */}
          {load.cost_lines && load.cost_lines.length > 0 ? (
            <DrawerBlock title={copy(pageContract, "loadwise.cost_breakdown.title")}>
              <DrawerTableScroll>
                <Table size="small" sx={{ minWidth: 320 }} aria-label={copy(pageContract, "loadwise.cost_breakdown.title")}>
                  <TableBody>
                    {load.cost_lines.map((line, index) => (
                      <TableRow key={`${line.kind}-${index}`}>
                        <TableCell>{copy(pageContract, `cost_kind.${line.kind}`, line.kind)}</TableCell>
                        <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                          {inr(Math.round(line.amount))}
                        </TableCell>
                      </TableRow>
                    ))}
                    {/* The total the reader is checking the parts against. Recomputed from the
                        lines rather than read from purchase_value so a breakdown that does not
                        add up is VISIBLE instead of hidden behind an authoritative-looking
                        figure. */}
                    <TableRow>
                      <TableCell sx={{ typography: "subtitle2" }}>{copy(pageContract, "loadwise.cost_breakdown.total")}</TableCell>
                      <TableCell align="right" sx={{ whiteSpace: "nowrap", typography: "subtitle2" }}>
                        {inr(Math.round(load.cost_lines.reduce((sum, line) => sum + line.amount, 0)))}
                      </TableCell>
                    </TableRow>
                  </TableBody>
                </Table>
              </DrawerTableScroll>
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                {copy(pageContract, "loadwise.cost_breakdown.hint")}
              </Typography>
            </DrawerBlock>
          ) : null}

          <DrawerNote>{copy(pageContract, "hint.load_cost")}</DrawerNote>

          {costField("animal_cost", load.animal_cost)}
          {costField("transport_cost", load.transport_cost)}
          {costField("other_cost", load.other_cost)}
        </Box>
      ) : null}
    </DetailDrawer>
  );
}
