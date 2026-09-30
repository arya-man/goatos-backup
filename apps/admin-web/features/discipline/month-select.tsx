"use client";

import { useRouter } from "next/navigation";

import { hrefWith } from "./href";

/** The month filter: picking a month loads that month (a data change, so a navigation). */
export function MonthSelect({
  label,
  months,
  value,
  pathname,
  searchParams,
}: {
  label: string;
  months: { key: string; label: string }[];
  value: string;
  pathname: string;
  searchParams: Record<string, string | string[] | undefined>;
}) {
  const router = useRouter();
  return (
    <label className="dsc-month">
      <span className="small muted">{label}</span>
      <select
        className="inp"
        aria-label={label}
        value={value}
        data-testid="violations-month"
        onChange={(e) => router.replace(hrefWith(pathname, searchParams, { month: e.target.value, cursor: null }), { scroll: false })}
      >
        {months.map((m) => (
          <option key={m.key} value={m.key}>
            {m.label}
          </option>
        ))}
      </select>
    </label>
  );
}
