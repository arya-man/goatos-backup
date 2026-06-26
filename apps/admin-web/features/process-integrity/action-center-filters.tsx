"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { Search, Users, X } from "lucide-react";

type FilterLink = {
  label: string;
  href: string;
  active?: boolean;
  count?: number;
};

export function ActionCenterFiltersButton({
  label = "Filters",
  mode = "filters",
  rowsLabel,
  clearHref,
  stateLinks,
  severityLinks,
}: {
  label?: string;
  mode?: "filters" | "my";
  rowsLabel: string;
  clearHref: string;
  stateLinks: FilterLink[];
  severityLinks: FilterLink[];
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [owner, setOwner] = useState("");
  const modalRef = useRef<HTMLDivElement>(null);
  const Icon = mode === "my" ? Users : Search;

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

  function applyLocalFilters(nextQuery = query, nextOwner = owner) {
    const q = nextQuery.trim().toLowerCase();
    const o = nextOwner.trim().toLowerCase();
    for (const card of Array.from(document.querySelectorAll<HTMLElement>(".taskboard .task"))) {
      const text = (card.textContent ?? "").toLowerCase();
      const match = (!q || text.includes(q)) && (!o || text.includes(o));
      card.style.display = match ? "" : "none";
    }
  }

  function clearLocalFilters() {
    setQuery("");
    setOwner("");
    for (const card of Array.from(document.querySelectorAll<HTMLElement>(".taskboard .task"))) {
      card.style.display = "";
    }
  }

  return (
    <>
      <button type="button" className="btn sm" onClick={() => setOpen(true)} aria-haspopup="dialog">
        <Icon className="ic" style={{ width: 13 }} aria-hidden="true" /> {label}
      </button>
      {open ? (
        <>
          <button
            type="button"
            aria-label="Close Action Center filters"
            onClick={() => setOpen(false)}
            style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,.6)", zIndex: 210, border: 0 }}
          />
          <div
            ref={modalRef}
            className="modal on card"
            role="dialog"
            aria-modal="true"
            aria-label="Action Center filters"
            tabIndex={-1}
            style={{ display: "flex", flexDirection: "column", overflow: "hidden" }}
          >
            <div className="hd" style={{ borderBottom: "1px solid var(--line2)", flex: "0 0 auto" }}>
              <Icon className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
              <h3>{mode === "my" ? "My tasks" : "Action Center filters"}</h3>
              <div className="sp" style={{ flex: 1 }} />
              <button type="button" className="iconbtn" onClick={() => setOpen(false)} aria-label="Close filters">
                <X className="ic" />
              </button>
            </div>
            <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 14, overflow: "auto" }}>
              <div className="muted small">{rowsLabel}</div>
              <div className="fld" style={{ marginBottom: 0 }}>
                <label>Search action, operator, ID</label>
                <input
                  value={query}
                  placeholder="Search visible cards..."
                  onChange={(event) => {
                    setQuery(event.target.value);
                    applyLocalFilters(event.target.value, owner);
                  }}
                />
              </div>
              <Facet title="Work state" links={stateLinks} onPick={() => setOpen(false)} />
              <Facet title="Severity" links={severityLinks} onPick={() => setOpen(false)} />
              <div className="fld" style={{ marginBottom: 0 }}>
                <label>Owner / operator</label>
                <input
                  value={owner}
                  placeholder="Filter visible cards by owner..."
                  onChange={(event) => {
                    setOwner(event.target.value);
                    applyLocalFilters(query, event.target.value);
                  }}
                />
              </div>
              <div className="note">
                Park and date scope come from the top bar. Work-state and severity apply immediately because those
                filters are backed by the Action Center API.
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
              <Link href={clearHref} replace scroll={false} className="btn" onClick={() => setOpen(false)}>
                Clear all
              </Link>
              <button type="button" className="btn" onClick={clearLocalFilters}>
                Clear visible
              </button>
              <div className="sp" style={{ flex: 1 }} />
              <button
                type="button"
                className="btn p"
                onClick={() => {
                  applyLocalFilters();
                  setOpen(false);
                }}
              >
                Done
              </button>
            </div>
          </div>
        </>
      ) : null}
    </>
  );
}

function Facet({ title, links, onPick }: { title: string; links: FilterLink[]; onPick: () => void }) {
  return (
    <div>
      <div className="muted small" style={{ marginBottom: 8, fontWeight: 700 }}>
        {title}
      </div>
      <div className="chipset">
        {links.map((link) => (
          <Link key={link.label} href={link.href} replace scroll={false} className={`chip${link.active ? " on" : ""}`} onClick={onPick}>
            {link.label}
            {typeof link.count === "number" ? <span className="cbq">{link.count}</span> : null}
          </Link>
        ))}
      </div>
    </div>
  );
}
