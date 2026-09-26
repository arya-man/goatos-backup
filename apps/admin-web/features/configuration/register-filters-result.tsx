"use client";

import { useTransition } from "react";
import { useRouter } from "next/navigation";

import Chip from "@mui/material/Chip";

import { chipProps, FiltersBlock, FiltersResult } from "@/components/minimal/filters-result";

export type RegisterFilterChip = { key: string; label: string; value: string; href: string };

/**
 * The template list's filters-result row (user/order `*-table-filters-result.tsx` anatomy: "N results
 * found", one dashed FiltersBlock per applied filter with a soft deletable Chip, and Clear) for a
 * URL-driven register. Removing a chip or clearing navigates to a precomputed href in a transition,
 * so the page stays on screen; nothing is filtered locally.
 */
export function RegisterFiltersResult({ total, chips, resetHref }: { total: number; chips: RegisterFilterChip[]; resetHref: string }) {
  const router = useRouter();
  const [, startTransition] = useTransition();
  const go = (href: string) => startTransition(() => router.push(href, { scroll: false }));
  if (!chips.length) return null;
  return (
    <FiltersResult totalResults={total} onReset={() => go(resetHref)} sx={{ p: 2.5, pt: 0 }}>
      {chips.map((chip) => (
        <FiltersBlock key={chip.key} label={`${chip.label}:`} isShow>
          <Chip {...chipProps} label={chip.value} onDelete={() => go(chip.href)} />
        </FiltersBlock>
      ))}
    </FiltersResult>
  );
}
