import type { ReactNode } from "react";

/** Realistic Goat OS fixture data shared by the kit stories. */
export const PENS = [
  { pen: "Pen A-12", park: "Kranji Park", head: 148, adg: 182, kids: 26, vet: "Dr. Suriya", status: "Healthy" },
  { pen: "Pen A-13", park: "Kranji Park", head: 132, adg: 164, kids: 18, vet: "Dr. Suriya", status: "Healthy" },
  { pen: "Pen B-04", park: "Lim Chu Kang Park", head: 96, adg: 121, kids: 9, vet: "Dr. Nadia", status: "Watch" },
  { pen: "Pen B-05", park: "Lim Chu Kang Park", head: 104, adg: 98, kids: 11, vet: "Dr. Nadia", status: "Watch" },
  { pen: "Pen C-01", park: "Sungei Tengah Park", head: 210, adg: 205, kids: 41, vet: "Dr. Arun", status: "Healthy" },
  { pen: "Pen C-02", park: "Sungei Tengah Park", head: 187, adg: 176, kids: 33, vet: "Dr. Arun", status: "Healthy" },
  { pen: "Pen D-07", park: "Mandai Quarantine", head: 54, adg: 64, kids: 0, vet: "Dr. Arun", status: "Quarantine" },
  { pen: "Pen D-08", park: "Mandai Quarantine", head: 61, adg: 71, kids: 2, vet: "Dr. Nadia", status: "Quarantine" },
  { pen: "Pen E-11", park: "Kranji Park", head: 143, adg: 158, kids: 22, vet: "Dr. Suriya", status: "Healthy" },
  { pen: "Pen E-12", park: "Kranji Park", head: 119, adg: 149, kids: 17, vet: "Dr. Suriya", status: "Healthy" },
];

export const VENDORS = [
  "Selvam Livestock Traders",
  "Tanjung Karang Goat Supply Cooperative (Selangor)",
  "Bukit Mertajam Farms",
  "Ipoh Highland Breeders",
];

export const ADG_WEEKS = [128, 141, 133, 152, 148, 161, 158, 172, 165, 181, 176, 190];
export const LOADS_PER_DAY = [4, 6, 3, 8, 7, 9, 5, 11, 6, 8, 12, 7];
export const FLAT_SERIES = [140, 140, 140, 140, 140, 140];

/** Page-ish padding so a story is not flush against the iframe edge. */
export function Frame({ children, width, pad = 24 }: { children: ReactNode; width?: number | string; pad?: number }) {
  return (
    <div style={{ padding: pad, background: "var(--bg)", minHeight: "100%", boxSizing: "border-box" }}>
      <div style={{ maxWidth: width ?? "100%", margin: "0 auto" }}>{children}</div>
    </div>
  );
}

/** Labelled state stack so one story can show default / hover / disabled side by side. */
export function States({ children }: { children: ReactNode }) {
  return <div style={{ display: "grid", gap: 20 }}>{children}</div>;
}

export function StateBlock({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section style={{ display: "grid", gap: 8 }}>
      <h4 style={{ margin: 0, font: "600 11px/1.4 var(--font-sans)", letterSpacing: ".08em", textTransform: "uppercase", color: "var(--fg-muted)" }}>
        {label}
      </h4>
      {children}
    </section>
  );
}

export const MOBILE = { viewport: { value: "mobile", isRotated: false } } as const;
