"use client";

import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

/**
 * The Today table's routine filter: a URL-driven select (`?routine=`), the same shape as the
 * page's park segments -- a real navigation, no local filtering of the rows on screen, because
 * the summary strip is a whole-filter aggregate the backend computes for the SAME filter.
 * Renders no copy of its own: the blank option's label and the option names arrive resolved.
 */
export function RoutineFilter({
  label,
  current,
  options,
  hrefFor,
}: {
  label: string;
  current: string;
  options: { value: string; label: string }[];
  /** Precomputed hrefs per option value ("" = no filter), so the client composes no URL. */
  hrefFor: Record<string, string>;
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [optimistic, setOptimistic] = useState<string | null>(null);
  const selected = pending && optimistic !== null ? optimistic : current;
  return (
    <select
      aria-label={label}
      value={selected}
      aria-busy={pending}
      onChange={(event) => {
        const value = event.target.value;
        const href = hrefFor[value];
        if (!href) return;
        setOptimistic(value);
        startTransition(() => router.push(href, { scroll: false }));
      }}
    >
      <option value="">{label}</option>
      {options.map((option) => (
        <option key={option.value} value={option.value}>
          {option.label}
        </option>
      ))}
    </select>
  );
}
