"use client";

import { Loader2, SlidersHorizontal, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore, useTransition } from "react";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  LocalOverlayLink,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { GrowthAssumptionsResponse, GrowthAssumptionsUpdate, GrowthAssumptionValue } from "@/lib/api/server";
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
const SPECIES = ["goat", "sheep"] as const;

type Draft = { prices: Record<string, string>; values: Record<string, string> };

/** The text the input shows for a served row: a number, "15, 20, 25" for a list, or the date. */
function textFor(value: GrowthAssumptionValue): string {
  if (value.kind === "number_list") return (value.values ?? []).map((v) => String(v)).join(", ");
  if (value.kind === "date") return value.date;
  return String(value.value);
}

function draftFrom(assumptions: GrowthAssumptionsResponse): Draft {
  const prices: Record<string, string> = {};
  for (const price of assumptions.sale_prices) prices[price.species] = String(price.price_per_kg_inr);
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

  const title = copy(pageContract, "drawer.assumptions.title");
  const setByFor = (key: string) => current.values.find((value) => value.key === key);

  function save(): void {
    setNotice(null);
    // Only fields that PARSE travel; a blank stays untouched on the backend (a row absent from the
    // request is left as it is), never coerced to 0 -- the validate-or-reject rule.
    const salePrices = SPECIES.flatMap((species) => {
      const raw = (draft.prices[species] ?? "").trim();
      if (raw === "") return [];
      // The fence: the price this drawer LOADED travels with the new one, so a save on a price
      // someone else moved meanwhile is refused rather than silently overwriting theirs.
      const loaded = current.sale_prices.find((row) => row.species === species)?.price_per_kg_inr ?? null;
      return [{ species, price_per_kg_inr: Number(raw), loaded_price_per_kg_inr: loaded }];
    });
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
    <>
      <LocalOverlayLink href={openHref} className="btn sm" scroll={false} aria-haspopup="dialog">
        <SlidersHorizontal className="ic" aria-hidden="true" /> {copy(pageContract, "action.assumptions")}
      </LocalOverlayLink>

      <div className={`scrim${open ? " on" : ""}`} aria-hidden={!open} tabIndex={open ? 0 : -1} onClick={close} />
      <aside className={`drawer${open ? " on" : ""}`} aria-label={title} aria-hidden={!open} inert={!open}>
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
          <p className="muted small" style={{ marginTop: 0 }}>
            {copy(pageContract, "drawer.assumptions.caption")}
          </p>

          <h3 className="h" style={{ marginTop: 12 }}>{copy(pageContract, "drawer.assumptions.prices.title")}</h3>
          <p className="muted small">{copy(pageContract, "drawer.assumptions.prices.hint")}</p>
          {SPECIES.map((species) => {
            const price = current.sale_prices.find((row) => row.species === species);
            return (
              <div className="fld" key={species}>
                <label htmlFor={`wt-assume-${species}`}>
                  {copy(pageContract, `assumption.${species}.label`)} · {copy(pageContract, "drawer.assumptions.rupees")}/kg
                </label>
                <input
                  id={`wt-assume-${species}`}
                  type="number"
                  inputMode="decimal"
                  step="1"
                  value={draft.prices[species] ?? ""}
                  disabled={pending}
                  onChange={(event) => setDraft((d) => ({ ...d, prices: { ...d.prices, [species]: event.target.value } }))}
                />
                {price ? (
                  <span className="muted small">
                    {copy(pageContract, "drawer.assumptions.set_by")} {price.set_by} · {price.effective_from}
                  </span>
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
            <p className={`small ${notice.tone === "warn" ? "warn" : "muted"}`} role="status">
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
    </>
  );
}
