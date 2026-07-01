"use client";

import { useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import { createPortal } from "react-dom";
import { Search, X } from "lucide-react";
import { copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export interface VaccinationFilterModalProps {
  pageContract: AdminUiPageContract;
  title: string;
  searchReason: string;
  filterReason: string;
  rowsLabel: string;
  actionHref?: string;
  actionLabel?: string;
  quickTerms?: AdminUiOption[];
  facets: string[];
}

function filterRoot(node: HTMLElement | null): ParentNode {
  return node?.closest("[data-filter-scope]") ?? node?.closest(".card") ?? document;
}

function filterableRows(root: ParentNode): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>("[data-filter-row], tbody tr, .pexr, .wfrow, .task"));
}

export function VisibleTableSearch({
  pageContract,
  label,
  placeholder,
}: {
  pageContract: AdminUiPageContract;
  label: string;
  placeholder?: string;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  function apply(query: string) {
    const rows = filterableRows(filterRoot(inputRef.current));
    const q = query.trim().toLowerCase();
    for (const row of rows) {
      const haystack = (row.textContent ?? "").toLowerCase();
      row.style.display = !q || haystack.includes(q) ? "" : "none";
    }
  }

  return (
    <div className="tsearch" title={copy(pageContract, "filter.search_visible_rows")}>
      <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
      <input
        ref={inputRef}
        placeholder={placeholder ?? copy(pageContract, "filter.search_placeholder")}
        aria-label={label}
        onChange={(event) => apply(event.target.value)}
      />
    </div>
  );
}

export function VaccinationFilterButton({
  pageContract,
  title,
  searchReason,
  filterReason,
  rowsLabel,
  actionHref,
  actionLabel,
  quickTerms,
  facets,
}: VaccinationFilterModalProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeFacet, setActiveFacet] = useState("all");
  const [filteredCount, setFilteredCount] = useState<number | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const modalRef = useRef<HTMLDivElement>(null);
  const searchId = useId();
  const facetTerms = quickTerms ?? optionGroup(pageContract, "filter_quick_terms");

  useEffect(() => {
    if (!open) return;
    const node = modalRef.current;
    const first = node?.querySelector<HTMLElement>('a[href],button:not([disabled]),input:not([disabled])');
    (first ?? node)?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open]);

  function applyVisibleTableFilter(nextQuery = query, nextFacet = activeFacet) {
    const rows = filterableRows(filterRoot(buttonRef.current));
    const q = nextQuery.trim().toLowerCase();
    const facet = nextFacet === "all" ? "" : nextFacet;
    let shown = 0;
    for (const row of rows) {
      const haystack = (row.textContent ?? "").toLowerCase();
      const match = (!q || haystack.includes(q)) && (!facet || haystack.includes(facet));
      row.style.display = match ? "" : "none";
      if (match) shown += 1;
    }
    setFilteredCount(shown);
  }

  function clearVisibleTableFilter() {
    for (const row of filterableRows(filterRoot(buttonRef.current))) {
      row.style.display = "";
    }
    setQuery("");
    setActiveFacet("all");
    setFilteredCount(null);
  }

  const modal = open ? (
    <>
      <div
        onClick={() => setOpen(false)}
        aria-hidden="true"
        style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,.6)", zIndex: 260 }}
      />
      <div
        ref={modalRef}
        className="modal on card"
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
        style={{ display: "flex", flexDirection: "column", overflow: "hidden", zIndex: 261 }}
      >
        <div className="hd" style={{ borderBottom: "1px solid var(--line2)", flex: "0 0 auto" }}>
          <Search className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{title}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="iconbtn" onClick={() => setOpen(false)} aria-label={copy(pageContract, "filter.close_label")}>
            <X className="ic" />
          </button>
        </div>
        <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 14, overflow: "auto" }}>
          <div className="fld" style={{ marginBottom: 0 }}>
            <label htmlFor={searchId}>{copy(pageContract, "filter.search_rows_label")}</label>
            <input
              id={searchId}
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={searchReason || copy(pageContract, "filter.search_visible_rows")}
            />
          </div>
          <div>
            <div className="muted small" style={{ marginBottom: 8 }}>
              {filteredCount === null ? rowsLabel : `${filteredCount} ${copy(pageContract, "pager.matching_rows")}`}
            </div>
            <div className="note">{copy(pageContract, "filter.apply_immediately")}</div>
          </div>
          <div>
            <div className="muted small" style={{ marginBottom: 8 }}>
              {copy(pageContract, "filter.facets_label")}
            </div>
            <div className="chips" role="list" aria-label={`${title} ${copy(pageContract, "filter.quick_filters_aria")}`}>
              {facetTerms.map((facet) => (
                <button
                  key={facet.key}
                  type="button"
                  className={`chip ${activeFacet === facet.key ? "on" : ""}`}
                  onClick={() => {
                    setActiveFacet(facet.key);
                    applyVisibleTableFilter(query, facet.key);
                  }}
                >
                  {facet.label}
                </button>
              ))}
            </div>
            <div className="muted small" style={{ marginTop: 8 }}>
              {copy(pageContract, "filter.available_columns")}: {facets.join(copy(pageContract, "filter.column_separator"))}
            </div>
          </div>
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 10,
            borderTop: "1px solid var(--line2)",
            background: "var(--panel)",
            padding: "14px 16px",
          }}
        >
          <button type="button" className="btn" onClick={clearVisibleTableFilter}>
            {copy(pageContract, "action.clear")}
          </button>
          <div className="sp" style={{ flex: 1 }} />
          <button
            type="button"
            className="btn"
            title={filterReason}
            onClick={() => {
              applyVisibleTableFilter();
              setOpen(false);
            }}
          >
            {copy(pageContract, "filter.apply_filters")}
          </button>
          {actionHref && actionLabel ? (
            <Link href={actionHref} className="btn p" onClick={() => setOpen(false)}>
              {actionLabel}
            </Link>
          ) : null}
        </div>
      </div>
    </>
  ) : null;

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        className="btn sm"
        onClick={() => setOpen(true)}
        title={filterReason}
        aria-haspopup="dialog"
      >
        <Search className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.filters")}
      </button>
      {modal ? createPortal(modal, document.body) : null}
    </>
  );
}
