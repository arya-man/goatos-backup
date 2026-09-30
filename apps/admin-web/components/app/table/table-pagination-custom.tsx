"use client";

// The verbatim template TablePaginationCustom (components/minimal/table) with the phone fit, set
// through its own `sx` (the wrapper Box) and child selectors: at phone width the rows-per-page label
// + select hide and the toolbar wraps, so the range and arrows stay inside the card at 390/412; the
// theme pins the toolbar at 64px, which only grows on phone (no nested scroller).
import {
  TablePaginationCustom as TemplateTablePaginationCustom,
  type TablePaginationCustomProps,
} from "@/components/minimal/table/table-pagination-custom";

export type { TablePaginationCustomProps };

const PHONE_FIT = {
  "& .MuiTablePagination-root": { overflow: { xs: "visible", sm: "auto" } },
  "& .MuiTablePagination-toolbar": {
    flexWrap: { xs: "wrap", sm: "nowrap" },
    justifyContent: "flex-end",
    rowGap: 0.5,
    height: { xs: "auto", sm: "calc(8 * var(--spacing))" },
    minHeight: "calc(8 * var(--spacing))",
  },
  "& .MuiTablePagination-spacer": { display: { xs: "none", sm: "block" } },
  "& .MuiTablePagination-selectLabel": { display: { xs: "none", sm: "block" } },
  "& .MuiTablePagination-input": { display: { xs: "none", sm: "inline-flex" } },
  "& .MuiTablePagination-displayedRows": { whiteSpace: "nowrap" },
} as const;

export function TablePaginationCustom({ sx, ...other }: TablePaginationCustomProps) {
  return <TemplateTablePaginationCustom {...other} sx={[PHONE_FIT, ...(Array.isArray(sx) ? sx : [sx])]} />;
}
