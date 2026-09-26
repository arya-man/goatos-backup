"use client";

import { LinkSelect } from "@/components/app/link-select";

/**
 * A register filter: a URL-driven select (`?f.<column>=`), the same shape as the routines page
 * filter -- a real navigation, no local filtering of the rows on screen, because the total and
 * the page are whole-filter answers the backend computes for the SAME filter.
 *
 * The control is the kit `LinkSelect` (floating label + listbox), not a native <select>; it
 * navigates to the precomputed href of the chosen option exactly as the select did. Renders no
 * copy of its own: the blank option's label and the option names arrive resolved.
 */
export function RegisterFilter({
  label,
  allLabel,
  current,
  options,
  hrefFor,
}: {
  label: string;
  /** The blank option's own word ("All"), so the control never shows its label as its value. */
  allLabel?: string;
  current: string;
  options: { value: string; label: string }[];
  /** Precomputed hrefs per option value ("" = no filter), so the client composes no URL. */
  hrefFor: Record<string, string>;
}) {
  return (
    <LinkSelect
      label={label}
      value={current}
      minWidth={150}
      options={[{ value: "", label: allLabel ?? label, href: hrefFor[""] ?? "" }, ...options.map((option) => ({ value: option.value, label: option.label, href: hrefFor[option.value] ?? "" }))]}
    />
  );
}
