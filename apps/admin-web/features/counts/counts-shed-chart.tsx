"use client";

import { useState } from "react";

import { AnalyticsConversionRates } from "@/components/minimal/sections/overview/analytics/analytics-conversion-rates";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TablePaginationCustom } from "@/components/minimal/table";

type Bar = { key: string; label: string; value: number };

/**
 * Counts Breakdown's pen/shed occupancy: template AnalyticsConversionRates (horizontal bars), paged
 * ten bars at a time with the template pagination footer. The series is never truncated (it
 * PARTITIONS the herd, so the bars sum to the KPI above): every pen stays reachable a page away,
 * and a 130-pen herd no longer draws a 5,000px card or a nested scroller.
 */
export function CountsShedChart({
  title,
  bars,
  unit,
  emptyLabel,
  pageSize = 10,
}: {
  title: string;
  bars: Bar[];
  unit: string;
  emptyLabel: string;
  pageSize?: number;
}) {
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(pageSize);
  const shown = bars.slice(page * rowsPerPage, page * rowsPerPage + rowsPerPage);
  return (
    <AnalyticsConversionRates
      title={title}
      aria-label={title}
      empty={<EmptyContent title={emptyLabel} />}
      chart={{
        categories: shown.map((bar) => bar.label),
        series: [{ name: unit, data: shown.map((bar) => bar.value) }],
      }}
    >
      {bars.length > rowsPerPage || rowsPerPage !== pageSize ? (
        <TablePaginationCustom
          count={bars.length}
          page={page}
          rowsPerPage={rowsPerPage}
          rowsPerPageOptions={[10, 25, 50]}
          onPageChange={(_, next) => setPage(next)}
          onRowsPerPageChange={(event) => {
            setRowsPerPage(Number(event.target.value));
            setPage(0);
          }}
        />
      ) : null}
    </AnalyticsConversionRates>
  );
}
