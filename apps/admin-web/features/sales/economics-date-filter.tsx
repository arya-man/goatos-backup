"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";

/**
 * Economics' window filter: the SHARED calendar plus this screen's URL contract.
 *
 * The calendar itself is `components/date-range-picker.tsx`, the same control the Verify
 * board, the video log and Herd Analytics use. That is the point — one calendar across
 * the product rather than a fourth that drifts from the other three, the same reasoning
 * behind the shared operational-location helper.
 *
 * It is a PAGE filter and writes only `from`/`to`, so changing the window can never reset
 * which park the reader has selected. It DOES clear the shed pager: the page a reader was
 * on in one window is a different set of pens in another, so carrying it over would land
 * them on a page that no longer exists.
 */
export function EconomicsDateFilter({
  labels,
  basePath,
  from,
  to,
  today,
  defaultFrom,
  defaultTo,
}: {
  labels: DateRangePickerLabels;
  basePath: string;
  /** The window the BACKEND served, so the control shows what is on screen. */
  from: string;
  to: string;
  today: string;
  /** The backend's no-param default window, expressed by ABSENCE in the URL. */
  defaultFrom: string;
  defaultTo: string;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [pending, startTransition] = useTransition();

  // The App Router refresh is async, so the props still describe the OLD selection while
  // it runs. Rendering the requested value immediately is what stops the label and the
  // highlighted cells from snapping back to the previous dates for the length of the
  // round trip — the URL-Driven Filter Responsiveness rule.
  const serverSelection = `${from}|${to}`;
  const [optimistic, setOptimistic] = useState<{ from: string; to: string; overrides: string } | null>(null);
  const live = optimistic?.overrides === serverSelection ? optimistic : null;

  function apply(nextFrom: string, nextTo: string): void {
    setOptimistic({ from: nextFrom, to: nextTo, overrides: serverSelection });

    const next = new URLSearchParams(searchParams?.toString() ?? "");
    next.delete("spage");
    // The no-param default is a WINDOW ending today, so only a selection equal to that
    // window is expressed by absence — a bookmark then keeps meaning "the last 90 days"
    // instead of freezing on the day it was taken. Every other selection is written into
    // the URL, because deleting the params for one would silently fall back to the whole
    // default window, which reads as the filter not working.
    if (nextFrom === defaultFrom && nextTo === defaultTo) {
      next.delete("from");
      next.delete("to");
    } else {
      next.set("from", nextFrom);
      next.set("to", nextTo);
    }
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false });
    });
  }

  return (
    <DateRangePicker
      labels={labels}
      from={live?.from ?? from}
      to={live?.to ?? to}
      today={today}
      busy={pending}
      onChange={apply}
    />
  );
}
