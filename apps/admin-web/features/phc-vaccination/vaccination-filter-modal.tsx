"use client";

import { useEffect, useId, useRef, useState } from "react";
import Link from "next/link";
import { Search, X } from "lucide-react";

export interface VaccinationFilterModalProps {
  title: string;
  searchReason: string;
  filterReason: string;
  rowsLabel: string;
  actionHref?: string;
  actionLabel?: string;
  facets: string[];
}

export function VisibleTableSearch({
  label,
  placeholder = "Search rows...",
}: {
  label: string;
  placeholder?: string;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  function apply(query: string) {
    const card = inputRef.current?.closest(".card");
    const rows = Array.from(card?.querySelectorAll<HTMLTableRowElement>("tbody tr") ?? []);
    const q = query.trim().toLowerCase();
    for (const row of rows) {
      const haystack = (row.textContent ?? "").toLowerCase();
      row.style.display = !q || haystack.includes(q) ? "" : "none";
    }
  }

  return (
    <div className="tsearch" title="Search visible rows">
      <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
      <input
        ref={inputRef}
        placeholder={placeholder}
        aria-label={label}
        onChange={(event) => apply(event.target.value)}
      />
    </div>
  );
}

export function VaccinationFilterButton({
  title,
  searchReason,
  filterReason,
  rowsLabel,
  actionHref,
  actionLabel,
  facets,
}: VaccinationFilterModalProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeFacet, setActiveFacet] = useState("all");
  const [filteredCount, setFilteredCount] = useState<number | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const modalRef = useRef<HTMLDivElement>(null);
  const searchId = useId();
  const facetTerms = [
    { label: "All", value: "all" },
    { label: "Due", value: "due" },
    { label: "Done", value: "done" },
    { label: "Pending", value: "pending" },
    { label: "Overdue", value: "overdue" },
    { label: "Review", value: "review" },
  ];

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
    const card = buttonRef.current?.closest(".card");
    const rows = Array.from(card?.querySelectorAll<HTMLTableRowElement>("tbody tr") ?? []);
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
    const card = buttonRef.current?.closest(".card");
    for (const row of Array.from(card?.querySelectorAll<HTMLTableRowElement>("tbody tr") ?? [])) {
      row.style.display = "";
    }
    setQuery("");
    setActiveFacet("all");
    setFilteredCount(null);
  }

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
        <Search className="ic" style={{ width: 13 }} aria-hidden="true" /> Filters
      </button>
      {open ? (
        <>
          <div
            onClick={() => setOpen(false)}
            aria-hidden="true"
            style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,.6)", zIndex: 210 }}
          />
          <div
            ref={modalRef}
            className="modal on card"
            role="dialog"
            aria-modal="true"
            aria-label={title}
            tabIndex={-1}
            style={{ display: "flex", flexDirection: "column", overflow: "hidden" }}
          >
            <div className="hd" style={{ borderBottom: "1px solid var(--line2)", flex: "0 0 auto" }}>
              <Search className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
              <h3>{title}</h3>
              <div className="sp" style={{ flex: 1 }} />
              <button type="button" className="iconbtn" onClick={() => setOpen(false)} aria-label="Close filters">
                <X className="ic" />
              </button>
            </div>
            <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 14, overflow: "auto" }}>
              <div className="fld" style={{ marginBottom: 0 }}>
                <label htmlFor={searchId}>Search rows</label>
                <input
                  id={searchId}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder={searchReason || "Search visible rows..."}
                />
              </div>
              <div>
                <div className="muted small" style={{ marginBottom: 8 }}>
                  {filteredCount === null ? rowsLabel : `${filteredCount} matching rows`}
                </div>
                <div className="note">Filters apply to the visible table immediately; deeper backend filters stay on the linked source surface.</div>
              </div>
              <div>
                <div className="muted small" style={{ marginBottom: 8 }}>
                  Facets
                </div>
                <div className="chips" role="list" aria-label={`${title} quick filters`}>
                  {facetTerms.map((facet) => (
                    <button
                      key={facet.value}
                      type="button"
                      className={`chip ${activeFacet === facet.value ? "on" : ""}`}
                      onClick={() => {
                        setActiveFacet(facet.value);
                        applyVisibleTableFilter(query, facet.value);
                      }}
                    >
                      {facet.label}
                    </button>
                  ))}
                </div>
                <div className="muted small" style={{ marginTop: 8 }}>
                  Available columns: {facets.join(" · ")}
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
                Clear
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
                Apply filters
              </button>
              {actionHref ? (
                <Link href={actionHref} className="btn p" onClick={() => setOpen(false)}>
                  {actionLabel ?? "Open Action Center"}
                </Link>
              ) : null}
            </div>
          </div>
        </>
      ) : null}
    </>
  );
}
