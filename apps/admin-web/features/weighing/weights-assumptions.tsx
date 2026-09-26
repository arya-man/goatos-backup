"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { BodyPortal } from "@/components/app/body-portal";
import { Caption } from "@/components/app/caption";

import { Loader2, SlidersHorizontal, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  LocalOverlayLink,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { GrowthAssumptionsResponse, GrowthAssumptionsUpdate, GrowthAssumptionValue, GrowthSalePrice } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import { saveWeighingAssumptionsAction } from "./weights-assumptions-action";

const ASSUMPTIONS_PARAM = "wt_assumptions";

/** Reads the drawer flag from the address bar. "" means closed. */
function readParam(): string {
  return new URL(window.location.href).searchParams.get(ASSUMPTIONS_PARAM) ?? "";
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
 * The keyed figures, grouped the way the drawer lists them; labels are page copy keyed on the key.
 * The KIND comes from the served row, never from this list, so an input renders as the backend
 * says (a number, a comma list of edges, or a date).
 */
const SECTIONS = [
  { title: "drawer.assumptions.values.title", keys: ["sale_ready_lower_kg", "sale_ready_threshold_kg", "load_age_alert_days"] },
  { title: "drawer.assumptions.growth.title", keys: ["weight_band_edges_kg", "slow_growth_target_g_per_day", "bad_scan_loss_g_per_day"] },
  { title: "drawer.assumptions.window.title", keys: ["default_period_days"] },
] as const;
// The price grid's species rows and gender columns are Configuration's lists, compiled into the page
// contract (assumption_species / assumption_sexes; OPEN UP TO NEW SPECIES, 2026-09-25), so a species
// the farm adds can be priced at once. These are only the fallback for a contract without the groups.
const FALLBACK_SPECIES = ["goat", "sheep"];
const FALLBACK_SEXES = ["male", "female"];

/**
 * The draft key of one price row. A species DEFAULT is keyed by the species alone; a stage x sex
 * OVERRIDE (maintainer decision 2026-09-24) by all three, so the two can never collide.
 */
function priceKey(species: string, stage = "", sex = ""): string {
  return stage === "" ? species : `${species}|${stage}|${sex}`;
}

function priceRowKey(price: GrowthSalePrice): string {
  return priceKey(price.species, price.management_stage, price.sex);
}

type Draft = { prices: Record<string, string>; values: Record<string, string> };

/** The text the input shows for a served row: a number, "15, 20, 25" for a list, or the date. */
function textFor(value: GrowthAssumptionValue): string {
  if (value.kind === "number_list") return (value.values ?? []).map((v) => String(v)).join(", ");
  if (value.kind === "date") return value.date;
  return String(value.value);
}

function draftFrom(assumptions: GrowthAssumptionsResponse): Draft {
  const prices: Record<string, string> = {};
  for (const price of assumptions.sale_prices) prices[priceRowKey(price)] = String(price.price_per_kg_inr);
  const values: Record<string, string> = {};
  for (const value of assumptions.values) values[value.key] = textFor(value);
  return { prices, values };
}

/**
 * The Assumptions drawer on ADG Analytics (maintainer decision 2026-09-19): the ONE place the
 * assumed live-weight sale price, the sale-ready weight line and the load-age alert are changed.
 * Mounted when the page contract enables `edit_assumptions` -- the maintainer's ask was
 * "who have [the tick] should only see it", so a reader without the write sees no button at all;
 * the figures themselves stay visible in the FCR tab's own caption.
 *
 * Same overlay mechanics as the Download drawer: client state driven by the URL, SSR renders it
 * closed. Every figure is a controlled text input so a cleared field stays distinct from 0 and
 * the backend, not this drawer, decides the band a figure must land in.
 */
export function WeightsAssumptionsControl({
  pageContract,
  assumptions,
  openHref,
  closeHref,
}: {
  pageContract: AdminUiPageContract;
  assumptions: GrowthAssumptionsResponse;
  openHref: string;
  closeHref: string;
}) {
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readParam, () => "");
  const open = selection !== "";

  const [draft, setDraft] = useState<Draft>(() => draftFrom(assumptions));
  const [current, setCurrent] = useState(assumptions);
  const [notice, setNotice] = useState<{ tone: "ok" | "warn"; text: string } | null>(null);
  const [pending, startTransition] = useTransition();

  const close = useCallback(() => {
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  useEffect(() => {
    if (!open) return;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, close]);

  // A refusal lands at the foot of a long drawer (the stage x sex grids push it far down), so it is
  // brought into view -- otherwise Save looks like it did nothing.
  useEffect(() => {
    if (notice) noticeRef.current?.scrollIntoView({ block: "nearest" });
  }, [notice]);

  const speciesOptions = optionGroup(pageContract, "assumption_species");
  const sexOptions = optionGroup(pageContract, "assumption_sexes");
  const speciesKeys = speciesOptions.length ? speciesOptions.map((option) => option.key) : FALLBACK_SPECIES;
  const sexKeys = sexOptions.length ? sexOptions.map((option) => option.key) : FALLBACK_SEXES;
  const speciesLabel = (key: string) =>
    speciesOptions.find((option) => option.key === key)?.label ?? copy(pageContract, `assumption.${key}.label`);
  const sexLabel = (key: string) =>
    sexOptions.find((option) => option.key === key)?.label ?? copy(pageContract, `drawer.assumptions.prices.${key}`);

  const title = copy(pageContract, "drawer.assumptions.title");
  const setByFor = (key: string) => current.values.find((value) => value.key === key);

  function save(): void {
    setNotice(null);
    // Only fields that PARSE travel; a blank stays untouched on the backend (a row absent from the
    // request is left as it is), never coerced to 0 -- the validate-or-reject rule.
    type PriceUpdate = NonNullable<GrowthAssumptionsUpdate["sale_prices"]>[number];
    const loadedFor = (key: string) =>
      current.sale_prices.find((row) => priceRowKey(row) === key)?.price_per_kg_inr ?? null;
    // The species DEFAULTS: a blank stays untouched (a default can never be cleared).
    const defaults = speciesKeys.flatMap((species): PriceUpdate[] => {
      const raw = (draft.prices[species] ?? "").trim();
      if (raw === "") return [];
      // The fence: the price this drawer LOADED travels with the new one, so a save on a price
      // someone else moved meanwhile is refused rather than silently overwriting theirs.
      return [{ species, price_per_kg_inr: Number(raw), loaded_price_per_kg_inr: loadedFor(species) }];
    });
    // The stage x sex OVERRIDES: only the boxes that CHANGED travel. A box emptied that held a
    // price sends null -- that combination goes back to the all-stages price from today.
    const overrides = speciesKeys.flatMap((species) =>
      current.stages.flatMap((stage) =>
        sexKeys.flatMap((sex): PriceUpdate[] => {
          const key = priceKey(species, stage.code, sex);
          const raw = (draft.prices[key] ?? "").trim();
          const loaded = loadedFor(key);
          const next = raw === "" ? null : Number(raw);
          if (next === loaded) return [];
          return [{ species, management_stage: stage.code, sex, price_per_kg_inr: next, loaded_price_per_kg_inr: loaded }];
        }),
      ),
    );
    const salePrices = [...defaults, ...overrides];
    type ValueUpdate = NonNullable<GrowthAssumptionsUpdate["values"]>[number];
    const values = current.values.flatMap((row): ValueUpdate[] => {
      const raw = (draft.values[row.key] ?? "").trim();
      if (raw === "") return [];
      // Sent in the shape the row's KIND names; the backend validates the band, the order of the
      // edges and the date, and refuses rather than clamps.
      if (row.kind === "number_list") {
        return [{ key: row.key, values: raw.split(/[,\s]+/).filter(Boolean).map(Number), row_version: row.row_version }];
      }
      if (row.kind === "date") return [{ key: row.key, date: raw, row_version: row.row_version }];
      return [{ key: row.key, value: Number(raw), row_version: row.row_version }];
    });
    startTransition(async () => {
      const result = await saveWeighingAssumptionsAction({ sale_prices: salePrices, values });
      if (!result.ok) {
        const text =
          result.reason === "conflict"
            ? copy(pageContract, "drawer.assumptions.conflict")
            : result.reason === "invalid"
              ? result.message || copy(pageContract, "drawer.assumptions.error")
              : copy(pageContract, "drawer.assumptions.error");
        setNotice({ tone: "warn", text });
        return;
      }
      setCurrent(result.data);
      setDraft(draftFrom(result.data));
      setNotice({ tone: "ok", text: copy(pageContract, "drawer.assumptions.saved") });
      // The page's figures are server-rendered from the rows this save just changed, so the
      // honest thing is a full reload: the FCR tab, the Load-wise tab and the cards re-read.
      window.location.assign(closeHref);
    });
  }

  return (
    <div className="wt-assumptions-control">
      <LocalOverlayLink href={openHref} className="btn sm" scroll={false} aria-haspopup="dialog">
        <SlidersHorizontal className="ic" aria-hidden="true" /> {copy(pageContract, "action.assumptions")}
      </LocalOverlayLink>

      <BodyPortal>
      <div className={`scrim${open ? " on" : ""}`} aria-hidden={!open} tabIndex={open ? 0 : -1} onClick={close} style={{ paddingBottom: "env(safe-area-inset-bottom)" }} />
      <aside className={`drawer wt-assumptions-drawer${open ? " on" : ""}`} aria-label={title} aria-hidden={!open} inert={!open} style={{ paddingBottom: "env(safe-area-inset-bottom)" }}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <SlidersHorizontal className="ic" aria-hidden="true" />
          </span>
          <div>
            <h2>{title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={title} onClick={close}>
            <X className="ic" aria-hidden="true" />
          </button>
        </div>

        <div className="dc">
          <Caption>{copy(pageContract, "drawer.assumptions.caption")}</Caption>

          <h3 className="h" style={{ marginTop: 12 }}>{copy(pageContract, "drawer.assumptions.prices.title")}</h3>
          <Caption>{copy(pageContract, "drawer.assumptions.prices.hint")}</Caption>
          {speciesKeys.map((species) => {
            const price = current.sale_prices.find((row) => priceRowKey(row) === species);
            const defaultText = draft.prices[species] ?? "";
            const overrideCount = current.sale_prices.filter((row) => row.species === species && row.management_stage !== "").length;
            return (
              <div key={species} style={{ marginBottom: 16 }}>
                <div className="fld">
                  <label htmlFor={`wt-assume-${species}`}>
                    {speciesLabel(species)} · {copy(pageContract, "drawer.assumptions.prices.default")} ·{" "}
                    {copy(pageContract, "drawer.assumptions.rupees")}/kg
                  </label>
                  <input
                    id={`wt-assume-${species}`}
                    type="number"
                    inputMode="decimal"
                    step="1"
                    value={defaultText}
                    disabled={pending}
                    onChange={(event) => setDraft((d) => ({ ...d, prices: { ...d.prices, [species]: event.target.value } }))}
                  />
                  {price ? (
                    <span className="muted small">
                      {copy(pageContract, "drawer.assumptions.set_by")} {price.set_by} · {fmtDate(price.effective_from)}
                    </span>
                  ) : null}
                </div>
                {current.stages.length > 0 ? (
                  <details className="wt-assume-stages">
                    <summary className="small">
                      {copy(pageContract, "drawer.assumptions.prices.by_stage")}
                      {overrideCount > 0 ? (
                        <span className="muted"> · {overrideCount} {copy(pageContract, "drawer.assumptions.prices.overrides")}</span>
                      ) : null}
                    </summary>
                    <p className="muted small" style={{ margin: "6px 0" }}>
                      {copy(pageContract, "drawer.assumptions.prices.by_stage.hint")}
                    </p>
                    <Table className="wt-assume-grid">
                      <TableHead>
                        <TableRow>
                          <TableCell component="th" scope="col">{copy(pageContract, "drawer.assumptions.prices.stage")}</TableCell>
                          {sexKeys.map((sex) => (
                            <TableCell component="th" scope="col" key={sex}>
                              {sexLabel(sex)}
                            </TableCell>
                          ))}
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {current.stages.map((stage) => (
                          <TableRow key={stage.code}>
                            <TableCell component="th" scope="row">{stage.name || stage.code}</TableCell>
                            {sexKeys.map((sex) => {
                              const key = priceKey(species, stage.code, sex);
                              return (
                                <TableCell key={sex}>
                                  <input
                                    type="number"
                                    inputMode="decimal"
                                    step="1"
                                    aria-label={`${speciesLabel(species)} · ${stage.name || stage.code} · ${sexLabel(sex)}`}
                                    placeholder={defaultText}
                                    value={draft.prices[key] ?? ""}
                                    disabled={pending}
                                    onChange={(event) => setDraft((d) => ({ ...d, prices: { ...d.prices, [key]: event.target.value } }))}
                                  />
                                </TableCell>
                              );
                            })}
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </details>
                ) : null}
              </div>
            );
          })}

          {SECTIONS.map((section) => (
            <div key={section.title}>
              <h3 className="h" style={{ marginTop: 16 }}>{copy(pageContract, section.title)}</h3>
              {section.keys.map((key) => {
                const value = setByFor(key);
                if (!value) return null;
                const inputType = value.kind === "date" ? "date" : value.kind === "number_list" ? "text" : "number";
                return (
                  <div className="fld" key={key}>
                    <label htmlFor={`wt-assume-${key}`}>
                      {copy(pageContract, `assumption.${key}.label`)}
                      {value.unit ? ` · ${value.unit}` : ""}
                    </label>
                    <input
                      id={`wt-assume-${key}`}
                      type={inputType}
                      inputMode={inputType === "number" ? "decimal" : undefined}
                      step={inputType === "number" ? (value.unit === "kg" ? "0.5" : "1") : undefined}
                      value={draft.values[key] ?? ""}
                      disabled={pending}
                      onChange={(event) => setDraft((d) => ({ ...d, values: { ...d.values, [key]: event.target.value } }))}
                    />
                    <span className="muted small">{copy(pageContract, `assumption.${key}.hint`)}</span>
                    <span className="muted small" style={{ display: "block" }}>
                      {copy(pageContract, "drawer.assumptions.set_by")} {value.set_by}
                    </span>
                  </div>
                );
              })}
            </div>
          ))}

          {notice ? (
            <p ref={noticeRef} className={`small ${notice.tone === "warn" ? "warn" : "muted"}`} role="status">
              {notice.text}
            </p>
          ) : null}
        </div>

        <div className="df">
          <button type="button" className="btn" onClick={close} disabled={pending}>
            {copy(pageContract, "drawer.assumptions.cancel")}
          </button>
          <button type="button" className="btn primary" onClick={save} disabled={pending} aria-busy={pending}>
            {pending ? <Loader2 className="ic wt-export-spin" aria-hidden="true" /> : null}
            {copy(pageContract, "drawer.assumptions.save")}
          </button>
        </div>
      </aside>
      </BodyPortal>
    </div>
  );
}
