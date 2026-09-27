"use client";

import { useTransition } from "react";
import { useRouter } from "next/navigation";

import Chip from "@mui/material/Chip";

import { chipProps, FiltersBlock, FiltersResult } from "@/components/minimal/filters-result";

export type LinkFilterChip = {
  id: string;
  /** Block label, e.g. "Status:". */
  label: string;
  value: string;
  /** Where removing this one filter goes. */
  href: string;
};

/**
 * The template's `OrderTableFiltersResult` (FiltersResult + FiltersBlock + soft Chips) for a list
 * whose filters live in the URL: deleting a chip or Clear is a soft navigation to a prepared href
 * (router.push in a transition, scroll kept), so a server page can render it with hrefs it built.
 */
export function LinkFiltersResult({
  totalResults,
  chips,
  resetHref,
  placement = "card",
}: {
  totalResults: number;
  chips: LinkFilterChip[];
  resetHref: string;
  /** "card": inside a list card under its toolbar (template order list); "page": above a card, flush with it (template calendar view). */
  placement?: "card" | "page";
}) {
  const router = useRouter();
  const [, startTransition] = useTransition();
  const go = (href: string) => startTransition(() => router.push(href, { scroll: false }));
  if (chips.length === 0) return null;
  return (
    <FiltersResult totalResults={totalResults} onReset={() => go(resetHref)} sx={placement === "page" ? undefined : { p: 2.5, pt: 0 }}>
      {chips.map((chip) => (
        <FiltersBlock key={chip.id} label={chip.label} isShow>
          <Chip {...chipProps} label={chip.value} onDelete={() => go(chip.href)} />
        </FiltersBlock>
      ))}
    </FiltersResult>
  );
}
