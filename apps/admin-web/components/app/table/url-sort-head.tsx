"use client";

import { useRouter } from "next/navigation";

import { TableHeadCustom, type TableHeadCellProps } from "@/components/app/table/table-head-custom";

/**
 * The template TableHeadCustom for a SERVER-sorted table: each sortable column carries the URL of its
 * next sort (the page computed it, server-side keyset order), so a click is a replace navigation,
 * never a client re-sort of the rows on screen. Columns without an href are not sortable.
 */
export function UrlSortHead({
  headCells,
  orderBy,
  order,
  sortHrefs,
}: {
  headCells: TableHeadCellProps[];
  orderBy: string;
  order: "asc" | "desc";
  sortHrefs: Record<string, string>;
}) {
  const router = useRouter();
  return (
    <TableHeadCustom
      headCells={headCells.map((cell) => ({ ...cell, sortable: cell.id in sortHrefs }))}
      orderBy={orderBy}
      order={order}
      onSort={(id) => {
        const href = sortHrefs[id];
        if (href) router.replace(href, { scroll: false });
      }}
    />
  );
}
