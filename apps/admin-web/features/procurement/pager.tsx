import Link from "next/link";
import { ArrowLeft, ArrowRight } from "lucide-react";

// Mock-styled cursor pager shared by the procurement row surfaces (Source Entry Board, Action Center,
// Protocol Adherence). Prev/Next are real server-navigation links built from the backend's next_cursor +
// the cursor-stack helpers in lib/search-params; disabled affordance when there is no page in a direction.
export function ProcurementPager({
  prevHref,
  nextHref,
  page,
  count,
  noun,
}: {
  prevHref: string | null;
  nextHref: string | null;
  page: number;
  count: number;
  noun: string;
}) {
  if (!prevHref && !nextHref && page <= 1) return null;

  return (
    <div className="pager2">
      <span className="muted small">
        Page {page} · {count} {noun}
        {count === 1 ? "" : "s"} on this page
      </span>
      <div className="sp" style={{ flex: 1 }} />
      {prevHref ? (
        <Link href={prevHref} scroll={false} className="btn sm" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed", display: "inline-flex", alignItems: "center", gap: 4 }}>
          <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
        </span>
      )}
      {nextHref ? (
        <Link href={nextHref} scroll={false} className="btn sm" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          Next <ArrowRight className="ic" style={{ width: 13 }} aria-hidden="true" />
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed", display: "inline-flex", alignItems: "center", gap: 4 }}>
          Next <ArrowRight className="ic" style={{ width: 13 }} aria-hidden="true" />
        </span>
      )}
    </div>
  );
}
