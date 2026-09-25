"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";

/**
 * The Approvals calendar filter (maintainer request 2026-09-25): the India business day(s) a
 * request was RAISED on, applied by the server as `raised_from` / `raised_to`.
 *
 * It is the console's shared calendar (`DateRangePicker`) with an optional "any date" state,
 * because Approvals shows every date until a reader picks one. Choosing a span drops the page
 * cursor (the server binds a cursor to the filter it was minted under and refuses it under
 * another), the open drawer and any decision banner; Clear removes both dates.
 */
export function ApprovalsDateFilter({
  labels,
  from,
  to,
  today,
  anyLabel,
  clearLabel,
  basePath,
}: {
  labels: DateRangePickerLabels;
  /** The span the server applied, "" when none. */
  from: string;
  to: string;
  today: string;
  anyLabel: string;
  clearLabel: string;
  basePath: string;
}) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [pending, startTransition] = useTransition();
  // Optimistic selection, derived away once the server props move (the WindowDateFilter shape),
  // so the trigger never snaps back to the old dates for the length of the round trip.
  const served = `${from}|${to}`;
  const [optimistic, setOptimistic] = useState<{ from: string; to: string; overrides: string } | null>(null);
  const live = optimistic?.overrides === served ? optimistic : null;

  function go(nextFrom: string, nextTo: string): void {
    setOptimistic({ from: nextFrom, to: nextTo, overrides: served });
    const next = new URLSearchParams(searchParams?.toString() ?? "");
    for (const key of ["ap_cursor", "ap_row", "ap_status", "ap_code"]) next.delete(key);
    if (nextFrom) {
      next.set("raised_from", nextFrom);
      next.set("raised_to", nextTo || nextFrom);
    } else {
      next.delete("raised_from");
      next.delete("raised_to");
    }
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false });
    });
  }

  const shownFrom = live ? live.from : from;
  const shownTo = live ? live.to : to;
  return (
    <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
      <DateRangePicker
        labels={labels}
        from={shownFrom}
        to={shownTo}
        today={today}
        busy={pending}
        emptyLabel={anyLabel}
        onChange={(nextFrom, nextTo) => go(nextFrom, nextTo)}
      />
      {shownFrom ? (
        <button type="button" className="btn sm" onClick={() => go("", "")} disabled={pending}>
          {clearLabel}
        </button>
      ) : null}
    </div>
  );
}
