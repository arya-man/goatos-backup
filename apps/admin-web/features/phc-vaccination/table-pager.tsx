import { ChevronLeft, ChevronRight } from "lucide-react";

// Mock `.pager2` footer for the vaccination read-model tables (status matrix, per-cohort detail, shed events).
// HONEST pager: /vaccination/operations and /vaccination/execution are LIMIT-bounded and expose NO cursor or
// has_more — so we report exactly what came back ("N returned rows · no cursor exposed") and disable
// Previous/Next rather than implying the full set is shown or faking pagination. (Real cursor/has_more on
// these read-models is a backend follow-up; the wording must not over-claim until then.)
const NO_CURSOR = "Read model is limit-bounded and exposes no cursor / has_more — these are the returned rows.";
export function VaccinationTablePager({ rows, noun = "row" }: { rows: number; noun?: string }) {
  return (
    <div className="pager2">
      <span className="muted small">
        {rows} returned {noun}
        {rows === 1 ? "" : "s"} · no cursor exposed
      </span>
      <span className="btn sm" aria-disabled="true" title={NO_CURSOR} style={{ opacity: 0.45, cursor: "not-allowed" }}>
        <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
      </span>
      <span className="btn sm" aria-disabled="true" title={NO_CURSOR} style={{ opacity: 0.45, cursor: "not-allowed" }}>
        Next <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
      </span>
    </div>
  );
}
