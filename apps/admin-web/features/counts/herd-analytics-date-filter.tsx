"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";

/**
 * Herd Analytics' window filter: the SHARED calendar plus this screen's URL contract.
 *
 * The calendar itself is `components/date-range-picker.tsx`, the same control the Verify
 * board and the video log use. That is the point — one calendar across the product rather
 * than a third one that drifts from the other two, which is the same reasoning behind the
 * shared operational-location helper.
 *
 * It is a PAGE filter, not top-bar scope. Park stays in the top bar (Scope Chrome Rule)
 * and is preserved untouched here: this component only ever writes `from`/`to`, so
 * changing the window can never reset which park the reader is looking at.
 */
export function HerdAnalyticsDateFilter({
  labels,
  basePath,
  from,
  to,
  today,
  minDate,
  defaultFrom,
  defaultTo,
}: {
  labels: DateRangePickerLabels;
  basePath: string;
  /** The window the BACKEND served, so the control shows what is on screen. */
  from: string;
  to: string;
  today: string;
  /** Earliest selectable day — the herd's history floor, mirrored from the backend. */
  minDate: string;
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
  //
  // The optimistic value remembers WHICH server selection it was overriding and is derived
  // away the moment the props move off it, rather than cleared by an effect that would fire
  // a second render pass after the correct value already painted.
  const serverSelection = `${from}|${to}`;
  const [optimistic, setOptimistic] = useState<{ from: string; to: string; overrides: string } | null>(null);
  const live = optimistic?.overrides === serverSelection ? optimistic : null;

  function apply(nextFrom: string, nextTo: string): void {
    setOptimistic({ from: nextFrom, to: nextTo, overrides: serverSelection });

    const next = new URLSearchParams(searchParams?.toString() ?? "");
    // The no-param default is the recent WINDOW, so only a selection equal to that window
    // is expressed by absence — a bookmark then keeps meaning "the last twelve months"
    // instead of freezing on the day it was taken. Every other selection, today included,
    // is written into the URL: deleting the params for one would silently fall back to the
    // whole default window, which reads as the filter not working.
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
      minDate={minDate}
      busy={pending}
      onChange={apply}
    />
  );
}
