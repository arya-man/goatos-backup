"use client";

import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ChevronDown, Layers } from "lucide-react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PENS_PARAM, encodePens } from "./pen-param";

export type CareCoverageParkChoice = { value: string; label: string; href: string };
export type CareCoveragePenChoice = { value: string; label: string };


// Care Coverage's filter bar, in the Live Drive Tracker's .lt-fbar look. Park is a native single
// select whose destinations the SERVER computed (it writes the shared top-bar `park` key). Pen is
// a checkbox multi-select: ticks are STAGED in the open dropdown and nothing navigates until
// Apply, so ticking five pens is one page load, not five. Filtering itself happens server-side on
// the next render — never client-side row hiding.
export function CareCoverageFilters({
  parkChoices,
  parkSelected,
  parkClearHref,
  penChoices,
  penSelected,
  clearAllHref,
  pageContract,
}: {
  parkChoices: CareCoverageParkChoice[];
  parkSelected: string;
  parkClearHref: string;
  penChoices: CareCoveragePenChoice[];
  penSelected: string[];
  clearAllHref: string | null;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const [optimisticParkSelected, setOptimisticParkSelected] = useState<string | null>(null);
  const selectedParkValue = optimisticParkSelected ?? parkSelected;

  function go(href: string, nextParkSelected?: string) {
    if (nextParkSelected !== undefined) setOptimisticParkSelected(nextParkSelected);
    startTransition(() => router.push(href, { scroll: false }));
  }

  // Rewrites ONLY the pen key (and the page cursor, which belongs to the old result), keeping the
  // park scope, page size and every other parameter as they are.
  function hrefForPens(pens: string[]): string {
    const next = new URLSearchParams(searchParams?.toString() ?? "");
    next.delete(PENS_PARAM);
    next.delete("cc_cursor");
    next.delete("cc_from");
    if (pens.length) next.set(PENS_PARAM, encodePens(pens));
    const qs = next.toString();
    return qs ? `${pathname}?${qs}` : pathname;
  }

  const parkLabel = parkChoices.find((choice) => choice.value === parkSelected)?.label;
  const penLabels = new Map(penChoices.map((choice) => [choice.value, choice.label]));
  if (optimisticParkSelected !== null && optimisticParkSelected === parkSelected) setOptimisticParkSelected(null);

  return (
    <div className={`lt-fbar cc-fbar${isPending ? " wfbusy" : ""}`} aria-busy={isPending}>
      <span className="lt-fsel">
        <Layers className="ic" style={{ width: 13, height: 13 }} aria-hidden="true" />
        <select
          aria-label={copy(pageContract, "filter.park")}
          value={selectedParkValue}
          onChange={(event) => {
            const next = parkChoices.find((choice) => choice.value === event.target.value);
            go(next ? next.href : parkClearHref, next?.value ?? "");
          }}
        >
          <option value="">{copy(pageContract, "filter.all_parks")}</option>
          {parkChoices.map((choice) => (
            <option key={choice.value} value={choice.value}>
              {choice.label}
            </option>
          ))}
        </select>
      </span>

      <PenMultiSelect
        choices={penChoices}
        selected={penSelected}
        pageContract={pageContract}
        onApply={(pens) => go(hrefForPens(pens))}
      />

      <span className="lt-chips">
        {parkSelected ? (
          <span className="achip">
            {parkLabel ?? copy(pageContract, "filter.unlisted_selection")}
            <b
              role="button"
              tabIndex={0}
              aria-label={`${copy(pageContract, "filter.remove_one")} — ${copy(pageContract, "filter.park")}`}
              onClick={() => go(parkClearHref)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") go(parkClearHref);
              }}
            >
              ×
            </b>
          </span>
        ) : null}
        {penSelected.map((pen) => {
          const without = hrefForPens(penSelected.filter((candidate) => candidate !== pen));
          const label = penLabels.get(pen) ?? copy(pageContract, "filter.unlisted_selection");
          return (
            <span className="achip" key={pen}>
              {label}
              <b
                role="button"
                tabIndex={0}
                aria-label={`${copy(pageContract, "filter.remove_one")} — ${label}`}
                onClick={() => go(without)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") go(without);
                }}
              >
                ×
              </b>
            </span>
          );
        })}
        {clearAllHref ? (
          <button type="button" className="achip clr" onClick={() => go(clearAllHref)}>
            {copy(pageContract, "filter.clear_all")}
          </button>
        ) : null}
      </span>
      <span className="lt-fnote">{copy(pageContract, "filter.apply_note")}</span>
    </div>
  );
}

function PenMultiSelect({
  choices,
  selected,
  pageContract,
  onApply,
}: {
  choices: CareCoveragePenChoice[];
  selected: string[];
  pageContract: AdminUiPageContract;
  onApply: (pens: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [staged, setStaged] = useState<string[]>(selected);
  const [query, setQuery] = useState("");
  const rootRef = useRef<HTMLSpanElement | null>(null);

  // Outside click / Escape close without applying, wired only while open.
  useEffect(() => {
    if (!open) return;
    function onPointerDown(event: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false);
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle ? choices.filter((choice) => choice.label.toLowerCase().includes(needle)) : choices;
  }, [choices, query]);
  const stagedSet = new Set(staged);

  const summary =
    selected.length === 0
      ? copy(pageContract, "filter.all_pens")
      : selected.length === 1
        ? (choices.find((choice) => choice.value === selected[0])?.label ?? copy(pageContract, "filter.unlisted_selection"))
        : `${selected.length} ${copy(pageContract, "label.pens")}`;

  function toggle(value: string) {
    setStaged((current) => (current.includes(value) ? current.filter((candidate) => candidate !== value) : [...current, value]));
  }

  function apply() {
    // Keep the choice list's own order, so the URL (and the chips) read in pen order.
    const order = new Map(choices.map((choice, index) => [choice.value, index]));
    const next = [...staged].sort((a, b) => (order.get(a) ?? 1e9) - (order.get(b) ?? 1e9));
    setOpen(false);
    setQuery("");
    onApply(next);
  }

  return (
    <span className="lt-fsel cc-pensel" ref={rootRef}>
      <button
        type="button"
        className="cc-pensel-btn"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={copy(pageContract, "filter.pen")}
        onClick={() => {
          // Opening seeds the staged ticks from the APPLIED selection, so a chip removed or a
          // park switched since the last Apply is reflected in the boxes.
          if (!open) setStaged(selected);
          setOpen(!open);
        }}
      >
        <span className="cc-pensel-summary">{summary}</span>
        <ChevronDown className="cc-pensel-caret" aria-hidden="true" />
      </button>
      {open ? (
        <div className="cc-pensel-pop">
          <input
            type="search"
            className="cc-pensel-search"
            placeholder={copy(pageContract, "filter.search_pens")}
            aria-label={copy(pageContract, "filter.search_pens")}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <div role="listbox" aria-multiselectable="true" aria-label={copy(pageContract, "filter.pen")} className="cc-pensel-list">
            {visible.length === 0 ? <div className="muted small cc-pensel-empty">{copy(pageContract, "filter.no_pen_match")}</div> : null}
            {visible.map((choice) => {
              const checked = stagedSet.has(choice.value);
              return (
                <label key={choice.value} role="option" aria-selected={checked} className="cc-pensel-opt">
                  <input type="checkbox" checked={checked} onChange={() => toggle(choice.value)} />
                  <span>{choice.label}</span>
                </label>
              );
            })}
          </div>
          <div className="cc-pensel-foot">
            <span className="muted small">
              {staged.length} {copy(pageContract, "filter.selected")}
            </span>
            <span style={{ flex: 1 }} />
            <button type="button" className="btn sm" onClick={() => setStaged([])} disabled={staged.length === 0}>
              {copy(pageContract, "filter.clear")}
            </button>
            <button type="button" className="btn sm p" onClick={apply}>
              {copy(pageContract, "filter.apply")}
            </button>
          </div>
        </div>
      ) : null}
    </span>
  );
}
