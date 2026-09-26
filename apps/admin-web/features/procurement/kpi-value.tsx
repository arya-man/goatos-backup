"use client";

import Box from "@mui/material/Box";

import { CountUp } from "@/components/app/count-up";
import { inr, num } from "./sales-format";

/**
 * A KPI figure that counts up on first view.
 *
 * Presentation only: it renders exactly the string the page rendered before, through the same
 * `sales-format` helpers. The formatter is chosen by a serializable `kind` rather than passed as a
 * function, so a server page can drop this client leaf straight into its existing `.kpi .val`
 * without becoming a client component itself. Reduced motion prints the final value at once
 * (handled inside `CountUp`).
 */
export type KpiValueKind = "inr" | "num";

export function KpiValue({
  value,
  kind = "num",
  digits = 0,
  suffix,
}: {
  value: number;
  kind?: KpiValueKind;
  digits?: number;
  /** Printed verbatim after the number, e.g. a contract-owned "kg" or "per kg". */
  suffix?: string;
}) {
  const format = kind === "inr" ? (n: number) => inr(n, digits) : (n: number) => num(n, digits);
  return (
    // Figure + unit wrap as one baseline group inside the widget, never past its edge.
    <Box
      component="span"
      className="proc-kpi-value"
      sx={{
        display: "inline-flex",
        alignItems: "baseline",
        flexWrap: "wrap",
        columnGap: "0.22em",
        minWidth: 0,
        maxWidth: "100%",
        "& .proc-kpi-figure": { minWidth: 0, maxWidth: "100%", overflowWrap: { xs: "normal", sm: "anywhere" } },
      }}
    >
      <CountUp value={value} format={format} className="proc-kpi-figure" />
      {/* Same unit treatment as KpiCard's `unit` (template widget: subtitle2, secondary text). */}
      {suffix ? (
        <Box component="span" className="proc-kpi-suffix" sx={{ ml: 0.5, typography: "subtitle2", color: "text.secondary", whiteSpace: "nowrap" }}>
          {suffix}
        </Box>
      ) : null}
    </Box>
  );
}
