"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import { DATE_FROM_PARAM, DATE_TO_PARAM } from "./actions-date-params";

/**
 * The Actions board's capture-date filter: the shared calendar plus this screen's URL contract.
 *
 * It is a PAGE filter, not the top-bar scope control: the shared as-of picker was removed from the
 * shell on purpose (features/verification-review/local-drawer-navigation.test.mjs pins its absence)
 * and this one narrows only /actions. It sits beside Shed and the status pills for that reason.
 */

// Cleared on every date change for the same reason the module/status filters clear them: all five
// describe the board as it was BEFORE the change, and a keyset cursor especially so — it is a
// position in one filtered sequence and lands somewhere unrelated in another.
const RESET_ON_FILTER = ["vi_row", "vi_cursor", "vi_trail", "va_status", "va_code"];

export function ActionsDateFilter({
  labels,
  basePath,
  from,
  to,
  today,
}: {
  labels: DateRangePickerLabels;
  basePath: string;
  from: string;
  to: string;
  today: string;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [pending, startTransition] = useTransition();

  // The App Router refresh is async, so the props still describe the OLD selection while it runs.
  // Rendering the requested value immediately is what stops the label and the highlighted cells
  // from snapping back to the previous date for the length of the round trip.
  //
  // The optimistic value remembers WHICH server selection it was overriding, and is derived away
  // the moment the props move off that selection — rather than cleared by an effect, which fires
  // a second render pass after the new props have already painted the correct value.
  const serverSelection = `${from}|${to}`;
  const [optimistic, setOptimistic] = useState<{ from: string; to: string; overrides: string } | null>(null);
  const live = optimistic?.overrides === serverSelection ? optimistic : null;
  const selFrom = live?.from ?? from;
  const selTo = live?.to ?? to;

  function apply(nextFrom: string, nextTo: string): void {
    setOptimistic({ from: nextFrom, to: nextTo, overrides: serverSelection });

    const next = new URLSearchParams(searchParams?.toString() ?? "");
    for (const key of RESET_ON_FILTER) next.delete(key);
    // Today is the default the page falls back to, so it is expressed by ABSENCE. Writing it into
    // the URL would make a bookmark mean "12 Aug" forever instead of "today", which is the whole
    // point of a landing default.
    if (nextFrom === today && nextTo === today) {
      next.delete(DATE_FROM_PARAM);
      next.delete(DATE_TO_PARAM);
    } else {
      next.set(DATE_FROM_PARAM, nextFrom);
      next.set(DATE_TO_PARAM, nextTo);
    }
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false });
    });
  }

  return <DateRangePicker labels={labels} from={selFrom} to={selTo} today={today} busy={pending} onChange={apply} />;
}
