"use client";

import { useRouter } from "next/navigation";
import { useTransition } from "react";
import type { SortingState } from "@tanstack/react-table";

/**
 * Whole-result table sorting through the URL ("sort all rows", 2026-09-25).
 *
 * A header click writes `sort` / `dir` (and drops the page offset, so the new order starts at its
 * first row) and asks for the page again as a TRANSITION: the chrome, the filters and every other
 * section stay on screen, and `pending` lets the table alone say it is busy. The default order is
 * written as ABSENCE, so the canonical URL stays the bare one.
 */
export function useUrlSort({
  sort,
  dir,
  defaultSort,
  pageParams,
}: {
  /** The column the served rows are ordered by, as the page read it from the URL ("" = default). */
  sort: string;
  dir: "asc" | "desc";
  defaultSort: { id: string; desc: boolean };
  /** Paging parameters a new order resets. */
  pageParams: string[];
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const sorting: SortingState = sort ? [{ id: sort, desc: dir === "desc" }] : [defaultSort];
  const onChange = (next: SortingState) => {
    const url = new URL(window.location.href);
    const first = next[0];
    if (!first || (first.id === defaultSort.id && first.desc === defaultSort.desc)) {
      url.searchParams.delete("sort");
      url.searchParams.delete("dir");
    } else {
      url.searchParams.set("sort", first.id);
      url.searchParams.set("dir", first.desc ? "desc" : "asc");
    }
    for (const key of pageParams) url.searchParams.delete(key);
    startTransition(() => router.replace(`${url.pathname}${url.search}`, { scroll: false }));
  };
  return { sorting, onChange, pending };
}
