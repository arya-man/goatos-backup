"use client";

import { useState } from "react";

import { DateRangeField } from "@/components/app/date-range-field";

/**
 * The "Recorded from / to" window of the Animals filter as the kit DateRangeField, inside the
 * page's plain GET form: the field posts `recorded_from` / `recorded_to` through its hidden inputs
 * (name prefix "recorded"), exactly the two params the route already reads.
 */
export function AnimalPurchaseRecordedRange({
  from,
  to,
  label,
  fromLabel,
  toLabel,
  previousMonthLabel,
  nextMonthLabel,
}: {
  from: string;
  to: string;
  label: string;
  fromLabel: string;
  toLabel: string;
  previousMonthLabel: string;
  nextMonthLabel: string;
}) {
  const [range, setRange] = useState({ from, to });
  return (
    <DateRangeField
      name="recorded"
      label={label}
      from={range.from}
      to={range.to}
      fromLabel={fromLabel}
      toLabel={toLabel}
      previousMonthLabel={previousMonthLabel}
      nextMonthLabel={nextMonthLabel}
      onChange={setRange}
      className="ap-filter ap-filter-date"
    />
  );
}
