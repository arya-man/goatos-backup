/** Shared realistic Goat OS fixtures for the kit stories. No lorem ipsum anywhere. */
import * as React from "react";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

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
  <Box sx={{ display: "flex", flexWrap: wrap ? "wrap" : "nowrap", gap: 1.5, alignItems: "center" }}>{children}</Box>
);

export const Stack = ({ children, title }: { children: React.ReactNode; title?: string }) => (
  <Box sx={{ display: "grid", gridTemplateColumns: "minmax(0,1fr)", gap: 1.25, minWidth: 0 }}>
    {title ? (
      <Typography variant="overline" sx={{ color: "text.secondary" }}>{title}</Typography>
    ) : null}
    {children}
  </Box>
);

/** Page-like padded canvas so stories sit on the app background (theme palette), not on white. */
export const Canvas = ({ children }: { children: React.ReactNode }) => (
  <Box sx={{ p: 3, display: "grid", gridTemplateColumns: "minmax(0,1fr)", gap: 3, alignContent: "start", minWidth: 0, overflowX: "clip", bgcolor: "background.default", minHeight: "100vh", color: "text.primary" }}>
    {children}
  </Box>
);

export const MOBILE = { globals: { viewport: { value: "mobile", isRotated: false } } };
