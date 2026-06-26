"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { Search, X } from "lucide-react";
import { parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";

// Herd Register filter modal — ports the mock's "Filter — Counts / Herd" dialog. Styling uses the shared
// theme classes (.modal / .chip / .fld / .btn) and CSS variables ONLY — no hardcoded hex, so the light/dark
// theme toggle and the Mesha palette stay correct.
//
// Backed facets wire to /goats/search (Gender→sex, Breed→breed, Search→q). Park scope is owned by the top
// bar (Scope Chrome Rule), shown read-only here. Additional facets preserve their chosen values in the URL
// and become active automatically when the goat-search contract starts consuming them.

const BREEDS = ["Malai", "Beetal", "Sojat", "Osmanabadi", "Boer", "Anantapur", "Kenguri"];
const GENDERS = ["Male", "Female"];

const EXTRA_FACETS = [
  ["age_cohort", "Age Cohort", "K0 / K1 / K2"],
  ["shed", "Shed", "ICU-1 / K3 / Mother"],
  ["repro_stage", "Pregnancy / Lactation Stage", "pregnant / milking / open"],
  ["lifecycle", "Status", "active / under_treatment / sold"],
  ["identity_review", "Identity Review Status", "needs review / clean"],
  ["origin_farm", "Origin Farm", "source farm"],
  ["days_in_stage", "Days in Stage (min/max)", "e.g. 7-30"],
  ["weight_kg", "Weight (kg) (min/max)", "e.g. 20-35"],
  ["adg", "ADG (g/day) (min/max)", "e.g. 80-140"],
] as const;

const groupLabelStyle: React.CSSProperties = {
  display: "block",
  fontSize: 11,
  fontWeight: 700,
  textTransform: "uppercase",
  letterSpacing: ".06em",
  color: "var(--muted)",
  marginBottom: 8,
};

interface HerdFiltersModalProps {
  open: boolean;
  searchParams?: RouteSearchParams;
  onClose: () => void;
}

export function HerdFiltersModal({ open, searchParams = {}, onClose }: HerdFiltersModalProps) {
  const router = useRouter();
  const pathname = "/counts/herd";
  const scope = parseScope(searchParams);
  const q = one(searchParams, "q");
  const breed = one(searchParams, "breed");
  const sex = one(searchParams, "sex");

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  // Preserve the top-bar scope (scope_mode/park/as_of) on every navigation so the bar never disagrees.
  const scopeParams = () => {
    const params = new URLSearchParams();
    if (scope.mode === "park" && scope.parkId) {
      params.set("scope_mode", "park");
      params.set("park", scope.parkId);
    } else {
      params.set("scope_mode", "company");
    }
    if (scope.asOf) params.set("as_of", scope.asOf);
    return params;
  };

  const handleApplyFilters = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const params = scopeParams();

    const searchVal = (form.querySelector("#filter-search") as HTMLInputElement | null)?.value?.trim();
    if (searchVal) params.set("q", searchVal);

    const selectedBreed = form.querySelector(".chip[data-facet='breed'][data-on='true']") as HTMLElement | null;
    if (selectedBreed?.dataset.value) params.set("breed", selectedBreed.dataset.value);

    const selectedSex = form.querySelector(".chip[data-facet='sex'][data-on='true']") as HTMLElement | null;
    if (selectedSex?.dataset.value) params.set("sex", selectedSex.dataset.value);

    for (const [name] of EXTRA_FACETS) {
      const value = (form.querySelector(`input[name='${name}']`) as HTMLInputElement | null)?.value?.trim();
      if (value) params.set(name, value);
    }

    const qs = params.toString();
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    onClose();
  };

  const clearAllUrl = (() => {
    const qs = scopeParams().toString();
    return qs ? `${pathname}?${qs}` : pathname;
  })();

  return (
    <>
      <div
        onClick={onClose}
        aria-hidden="true"
        style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,.6)", zIndex: 210 }}
      />
      {/* `card` gives the header/body the shared .card .hd / .card .bd styling; `modal on` provides the
          centered overlay positioning + animation. Flex column so the header + footer are flex-none and
          ALWAYS visible while only the facet list scrolls — the primary actions never fall below the fold. */}
      <div
        className="modal on card"
        role="dialog"
        aria-modal="true"
        aria-label="Filter — Counts / Herd"
        style={{ display: "flex", flexDirection: "column", overflow: "hidden" }}
      >
        <div className="hd" style={{ borderBottom: "1px solid var(--line2)", flex: "0 0 auto" }}>
          <Search className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>Filter — Counts / Herd</h3>
          <div className="sp" style={{ flex: 1 }} />
          <button type="button" className="iconbtn" onClick={onClose} aria-label="Close filters">
            <X className="ic" />
          </button>
        </div>

        <form onSubmit={handleApplyFilters} className="bd" style={{ display: "flex", flexDirection: "column", flex: "1 1 auto", minHeight: 0, overflow: "hidden", gap: 0 }}>
          <div style={{ display: "flex", flexDirection: "column", gap: 18, overflow: "auto", flex: "1 1 auto", minHeight: 0 }}>
          {/* Park is owned by the top-bar scope (Scope Chrome Rule) — read-only here, not a duplicate filter. */}
          <div>
            <span style={groupLabelStyle}>Park</span>
            <div className="muted small">Park scope is set in the top bar — this filter is read-only.</div>
          </div>

          <div className="fld" style={{ marginBottom: 0 }}>
            <label htmlFor="filter-search">Search (RFID / old tag / id)</label>
            <input id="filter-search" name="q" defaultValue={q ?? ""} placeholder="e.g. RF-9001 or CB-201" />
          </div>

          <div>
            <span style={groupLabelStyle}>Gender</span>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              {GENDERS.map((g) => {
                const value = g === "Male" ? "male" : "female";
                return <ChipButton key={g} label={g} value={value} facet="sex" selected={sex === value} />;
              })}
            </div>
          </div>

          <div>
            <span style={groupLabelStyle}>Breed</span>
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              {BREEDS.map((b) => (
                <ChipButton key={b} label={b} value={b} facet="breed" selected={breed === b} />
              ))}
            </div>
          </div>

          {EXTRA_FACETS.map(([name, label, placeholder]) => (
            <div key={name}>
              <span style={groupLabelStyle}>{label}</span>
              <div className="fld" style={{ marginBottom: 0 }}>
                <input
                  name={name}
                  defaultValue={one(searchParams, name) ?? ""}
                  placeholder={placeholder}
                  aria-label={label}
                />
              </div>
            </div>
          ))}
          </div>

          {/* Footer is OUTSIDE the scrolling list (flex-none) so Apply / Clear all stay pinned + visible at
              any viewport height; the negative margins bleed it to the body edges. */}
          <div
            style={{
              flex: "0 0 auto",
              display: "flex",
              alignItems: "center",
              gap: 10,
              borderTop: "1px solid var(--line2)",
              background: "var(--panel)",
              margin: "12px -16px -14px",
              padding: "14px 16px",
            }}
          >
            <Link href={clearAllUrl} replace scroll={false} className="btn" onClick={onClose}>
              Clear all
            </Link>
            <div className="sp" style={{ flex: 1 }} />
            <button type="submit" className="btn p">
              Apply filters
            </button>
          </div>
        </form>
      </div>
    </>
  );
}

function ChipButton({ label, value, facet, selected }: { label: string; value: string; facet: string; selected: boolean }) {
  return (
    <button
      type="button"
      data-facet={facet}
      data-value={value}
      data-on={selected ? "true" : "false"}
      className={`chip ${selected ? "on" : ""}`}
      onClick={(e) => {
        const form = (e.target as HTMLElement).closest("form");
        if (!form) return;
        const chips = form.querySelectorAll<HTMLElement>(`.chip[data-facet='${facet}']`);
        chips.forEach((c) => {
          if (c === e.currentTarget) {
            const on = c.getAttribute("data-on") === "true";
            c.setAttribute("data-on", on ? "false" : "true");
            c.classList.toggle("on", !on);
          } else {
            c.setAttribute("data-on", "false");
            c.classList.remove("on");
          }
        });
      }}
    >
      {label}
    </button>
  );
}
