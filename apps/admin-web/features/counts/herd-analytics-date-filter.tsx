"use client";

import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { WindowDateFilter } from "@/components/window-date-filter";

/**
 * Herd Analytics' window filter.
 *
 * The mechanism moved to `components/window-date-filter.tsx` when Health Analytics needed the
 * same control: the optimistic-selection handling is subtle enough that a second hand-written
 * copy would drift from this one. This stays as the named entry point so the page reads as it
 * did, and so a future Counts-only change has somewhere to live.
 */
export function HerdAnalyticsDateFilter(props: {
  labels: DateRangePickerLabels;
  basePath: string;
  from: string;
  to: string;
  today: string;
  defaultFrom: string;
  defaultTo: string;
}) {
  return <WindowDateFilter {...props} />;
}
