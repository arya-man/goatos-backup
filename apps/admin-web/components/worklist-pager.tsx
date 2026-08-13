import Link from "@/components/no-prefetch-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export function WorklistPager({
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
          <Link key={size} href={hrefForLimit(size)} className={size === limit ? "btn sm p" : "btn sm"} aria-current={size === limit ? "true" : undefined}>
            {String(size)}
          </Link>
        ))}
      </label>
      {offset > 0 ? (
        <Link className="btn sm" href={hrefForOffset(previousOffset)}>{copy(pageContract, "action.previous")}</Link>
      ) : (
        <span className="btn sm" aria-disabled="true">{copy(pageContract, "action.previous")}</span>
      )}
      {hasMore ? (
        <Link className="btn sm" href={hrefForOffset(offset + limit)}>{copy(pageContract, "action.next")}</Link>
      ) : (
        <span className="btn sm" aria-disabled="true">{copy(pageContract, "action.next")}</span>
      )}
    </div>
  );
}
