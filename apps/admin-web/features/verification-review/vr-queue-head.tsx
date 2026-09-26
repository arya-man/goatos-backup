"use client";

import { useRouter } from "next/navigation";

import { TableHeadCustom, type TableHeadCellProps } from "@/components/minimal/table";

/**
 * The queue's header row: the template TableHeadCustom, with the one sortable column (capture time)
 * navigating to the URL-owned sort the page computed. The sort is server-side (keyset order), so a
 * click is a replace navigation, never a client re-sort of the page on screen.
 */
export function VrQueueHead({
  headCells,
  orderBy,
  order,
  sortHref,
}: {
  headCells: TableHeadCellProps[];
  orderBy: string;
  order: "asc" | "desc";
  sortHref: string;
}) {
  const router = useRouter();
  return (
    <TableHeadCustom
      headCells={headCells}
      orderBy={orderBy}
      order={order}
      onSort={(id) => {
        if (id === orderBy) router.replace(sortHref, { scroll: false });
      }}
    />
  );
}
