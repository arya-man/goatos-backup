/** Shared realistic Goat OS fixtures for the kit stories. No lorem ipsum anywhere. */
import * as React from "react";

export const PENS = [
  { value: "", label: "All pens" },
  { value: "pen-a1", label: "Pen A1 · Kids 0–3 mo" },
  { value: "pen-a2", label: "Pen A2 · Kids 3–6 mo" },
  { value: "pen-b1", label: "Pen B1 · Growers" },
  { value: "pen-b2", label: "Pen B2 · Growers" },
  { value: "pen-c1", label: "Pen C1 · Breeding does" },
  { value: "pen-c2", label: "Pen C2 · Bucks" },
  { value: "pen-q1", label: "Pen Q1 · Quarantine" },
] as const;

export const PARKS = [
  { value: "seletar", label: "Seletar Park" },
  { value: "lim-chu-kang", label: "Lim Chu Kang Park" },
  { value: "kranji", label: "Kranji Park" },
] as const;

export const LONG_OPTIONS = [
  { value: "vendor-1", label: "Kranji Livestock Supply Cooperative (Batch 2026-04, quarantine cleared)" },
  { value: "vendor-2", label: "Seletar Feed & Forage Pte Ltd" },
  { value: "vendor-3", label: "Lim Chu Kang Goat Breeders Association — Northern Chapter" },
] as const;

export const MANY_OPTIONS = Array.from({ length: 40 }, (_, i) => ({
  value: `shed-${i + 1}`,
  label: `Pen ${String(i + 1).padStart(2, "0")} · ${120 + i * 3} head`,
}));

export const PEN_ROWS = [
  { pen: "Pen A1", kids: 46, adg: 168, vendor: "Kranji Livestock", status: "On target" },
  { pen: "Pen A2", kids: 38, adg: 141, vendor: "Seletar Feed", status: "Watch" },
  { pen: "Pen B1", kids: 52, adg: 194, vendor: "Kranji Livestock", status: "On target" },
  { pen: "Pen B2", kids: 49, adg: 122, vendor: "LCK Breeders", status: "Below target" },
  { pen: "Pen C1", kids: 31, adg: 88, vendor: "Seletar Feed", status: "Below target" },
];

export const Row = ({ children, wrap = true }: { children: React.ReactNode; wrap?: boolean }) => (
  <div style={{ display: "flex", flexWrap: wrap ? "wrap" : "nowrap", gap: 12, alignItems: "center" }}>{children}</div>
);

export const Stack = ({ children, title }: { children: React.ReactNode; title?: string }) => (
  <div style={{ display: "grid", gridTemplateColumns: "minmax(0,1fr)", gap: 10, minWidth: 0 }}>
    {title ? (
      <div style={{ font: "700 11px/1.4 var(--font-sans)", letterSpacing: ".06em", textTransform: "uppercase", opacity: 0.6 }}>{title}</div>
    ) : null}
    {children}
  </div>
);

/** Page-like padded canvas so stories sit on the app background, not on white. */
export const Canvas = ({ children }: { children: React.ReactNode }) => (
  <div className="main" style={{ padding: 24, display: "grid", gridTemplateColumns: "minmax(0,1fr)", gap: 24, alignContent: "start", minWidth: 0, overflowX: "clip", background: "var(--bg)", minHeight: "100vh", color: "var(--fg, var(--ink))" }}>
    {children}
  </div>
);

export const MOBILE = { globals: { viewport: { value: "mobile", isRotated: false } } };
