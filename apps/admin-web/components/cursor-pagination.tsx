"use client";

import Link from "next/link";
import { ArrowLeft, ArrowRight } from "lucide-react";

export function CursorPagination({
  nextHref,
  previousHref,
  currentPage,
  pageSize,
  itemCount,
}: {
  nextHref: string | null;
  previousHref: string | null;
  currentPage?: number;
  pageSize?: number;
  itemCount?: number;
}) {
  const page = currentPage && currentPage > 0 ? currentPage : 1;
  const start = pageSize && itemCount && itemCount > 0 ? (page - 1) * pageSize + 1 : null;
  const end = start && itemCount ? start + itemCount - 1 : null;

  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-[#334155] bg-[#11151C] px-4 py-3">
      <div>
        <div className="text-xs font-semibold uppercase text-[#c7d1dc]">Page {page}</div>
        <div className="mt-1 text-sm text-[#93a4b8]">
          {start && end ? `Showing ${start.toLocaleString("en-IN")}-${end.toLocaleString("en-IN")}` : "Cursor-paged results"}
          {pageSize ? ` · ${pageSize} rows per page` : null}
        </div>
      </div>
      <div className="flex items-center gap-2">
        {previousHref ? (
          <Link
            href={previousHref}
            scroll={false}
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#f8fafc] hover:bg-[#22262E]"
          >
            <ArrowLeft className="h-4 w-4" aria-hidden="true" />
            Previous
          </Link>
        ) : (
          <button
            type="button"
            disabled
            className="inline-flex h-9 cursor-not-allowed items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#f8fafc] opacity-40"
          >
            <ArrowLeft className="h-4 w-4" aria-hidden="true" />
            Previous
          </button>
        )}
        {nextHref ? (
          <Link
            href={nextHref}
            scroll={false}
            className="inline-flex h-9 items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#f8fafc] hover:bg-[#22262E]"
          >
            Next
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </Link>
        ) : (
          <button
            type="button"
            disabled
            className="inline-flex h-9 cursor-not-allowed items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#f8fafc] opacity-40"
          >
            Next
            <ArrowRight className="h-4 w-4" aria-hidden="true" />
          </button>
        )}
      </div>
    </div>
  );
}
