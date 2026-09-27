"use client";

import type { ComponentProps } from "react";
import { EcommerceYearlySales } from "@/components/minimal/sections/overview/e-commerce/ecommerce-yearly-sales";
import { EmptyState } from "@/components/app/empty-state";
import { inr, inrAxisTick, num, numAxisTick } from "./sales-format";

type Series = Omit<ComponentProps<typeof EcommerceYearlySales>["chart"]["series"][number], "empty"> & { empty: string };

// Formatters live on the client side of the boundary: the Sold page is a server component and
// cannot hand functions to the template card. Rupees and counts keep the page's own formats (one
// unit per axis, Indian grouping in the tooltip).
const FORMATTERS: ComponentProps<typeof EcommerceYearlySales>["formatters"] = {
  inr: { axis: inrAxisTick, tooltip: (value) => inr(value) },
  number: { axis: numAxisTick, tooltip: (value) => num(value) },
};

// Template chart options (apexcharts `responsive`, deep-merged by useChart into the template's own
// sm entry): below 600px up to eighteen month ticks do not fit a phone card -- ApexCharts rotated them
// -45deg and the last one ran past the card edge (FJ3 P1-6). Flat labels, every other month
// hidden when they would overlap, a bounded tick count.
const PHONE_AXIS: ComponentProps<typeof EcommerceYearlySales>["chart"]["options"] = {
  responsive: [{ breakpoint: 600, options: { xaxis: { tickAmount: 6, labels: { rotate: 0, rotateAlways: false, hideOverlappingLabels: true, trim: false } } } }],
};

/**
 * Month by month on Sold: the template Yearly sales card, its select switching between revenue,
 * animals and manure. The three never share an axis -- one is drawn at a time, each with its own
 * scale -- which is the rule the three stacked column charts used to keep.
 */
export function SalesSoldMonthly({ title, series, ariaLabel }: { title: string; series: Series[]; ariaLabel: string }) {
  return (
    <EcommerceYearlySales
      component="section"
      aria-label={ariaLabel}
      title={title}
      formatters={FORMATTERS}
      chart={{ series: series.map((item) => ({ ...item, empty: <EmptyState title={item.empty} /> })), options: PHONE_AXIS }}
      sx={{ height: 1 }}
    />
  );
}
