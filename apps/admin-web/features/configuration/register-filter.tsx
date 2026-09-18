"use client";

import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

/**
 * A register filter: a URL-driven select (`?f.<column>=`), the same shape as the routines page
 * filter -- a real navigation, no local filtering of the rows on screen, because the total and
 * the page are whole-filter answers the backend computes for the SAME filter.
 * Renders no copy of its own: the blank option's label and the option names arrive resolved.
 */
export function RegisterFilter({
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
