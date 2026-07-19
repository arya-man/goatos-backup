import Link from "@/components/no-prefetch-link";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Prev/next pager for the feed reads.
//
// Deliberately NOT the numbered pager the Counts Breakdown uses. Every feed endpoint returns
// `has_more` and no total, because counting the filtered set on each request is compute-on-read; the
// page genuinely does not know how many pages exist, so it does not draw page numbers it would have
// to invent. It shows the offset window it is actually on, and whether more exists.
//
// The mock's `.pager2` footer bar: meta pushed left by `margin-right:auto`, controls right.

export function FeedPager({
  pageContract,
  offset,
  limit,
  rowCount,
  hasMore,
  noun,
  pageSizeOptions,
  hrefForOffset,
  hrefForLimit,
}: {
  pageContract: AdminUiPageContract;
  offset: number;
  limit: number;
  /** Rows actually returned on this page — the window's real end, never an assumed full page. */
  rowCount: number;
  hasMore: boolean;
  noun: string;
  pageSizeOptions: readonly number[];
  hrefForOffset: (offset: number) => string;
  hrefForLimit: (limit: number) => string;
}) {
  const start = rowCount === 0 ? 0 : offset + 1;
  const end = rowCount === 0 ? 0 : offset + rowCount;
  const pluralNoun = rowCount === 1 ? noun : `${noun}s`;
  const hasPrevious = offset > 0;
  const previousOffset = Math.max(0, offset - limit);

  return (
    <div className="pager2">
      <span className="small muted" style={{ marginRight: "auto" }}>
        {rowCount === 0
          ? `0 ${pluralNoun}`
          : `${start}-${end} ${pluralNoun} · ${copy(pageContract, "pager.page")} ${Math.floor(offset / limit) + 1}`}
      </span>

      <label style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}>
        <span className="muted">{copy(pageContract, "pager.rows")}</span>
        {pageSizeOptions.map((size) => (
          <Link
            key={size}
            href={hrefForLimit(size)}
            className={size === limit ? "btn sm p" : "btn sm"}
            aria-current={size === limit ? "true" : undefined}
          >
            {String(size)}
          </Link>
        ))}
      </label>

      {hasPrevious ? (
        <Link className="btn sm" href={hrefForOffset(previousOffset)}>
          {copy(pageContract, "action.previous")}
        </Link>
      ) : (
        <span className="btn sm" aria-disabled="true">
          {copy(pageContract, "action.previous")}
        </span>
      )}
      {hasMore ? (
        <Link className="btn sm" href={hrefForOffset(offset + limit)}>
          {copy(pageContract, "action.next")}
        </Link>
      ) : (
        <span className="btn sm" aria-disabled="true">
          {copy(pageContract, "action.next")}
        </span>
      )}
    </div>
  );
}
