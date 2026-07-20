import type { ComponentType, SVGProps } from "react";
import { AlertTriangle, CheckCircle2, Clock, FlaskConical, Lock, PencilLine } from "lucide-react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedDirectionLifecycle, FeedDirectionWorkflowLifecycle } from "@/lib/api/server";
import { fmtDate, fmtDateTime } from "@/lib/format";

// The issue -> amend -> lock lifecycle banner shared by Feed Direction and Feed Packing.
//
// The served park-day is either a FROZEN sheet (issued/amended/locked, with rows) or a state where
// nothing was issued (pending/not_issued, no rows). The default feed day is TOMORROW, which is
// normally `pending` until its scheduled issue time, so WITHOUT this banner the page shows a blank
// table that reads as "nothing to feed" — the opposite of the truth. This banner replaces that blank
// with an explanation, and above a real sheet it states the frozen/amended/locked status.
//
// Aggregate `lifecycle.state` is the LEAST-ADVANCED workflow state, so a park-day can be `pending`
// while its `normal` workflow is already issued (its rows ARE returned). That is why the caller keys
// the "hide the table" decision off `rows.length`, not off the aggregate state — and why the
// per-workflow breakdown is surfaced whenever the workflows disagree.

type LifecycleState = FeedDirectionLifecycle["state"];
type WorkflowState = FeedDirectionWorkflowLifecycle["state"];

type IconType = ComponentType<SVGProps<SVGSVGElement>>;

const STATE_ICON: Record<LifecycleState, IconType> = {
  issued: CheckCircle2,
  amended: PencilLine,
  locked: Lock,
  pending: Clock,
  not_issued: AlertTriangle,
  draft: FlaskConical,
};

// `.alert` tone. ok = a clean issued sheet; warn = amended (corrections folded in) or the not_issued
// gap; info = the neutral locked / pending / draft states (nothing wrong, just not a frozen-and-ready
// sheet). Danger is reserved for the API-failure band the page renders separately.
function alertClass(state: LifecycleState): string {
  if (state === "issued") return "alert ok";
  if (state === "amended" || state === "not_issued") return "alert warn";
  return "alert info";
}

function workflowChipTone(state: WorkflowState): string {
  switch (state) {
    case "issued":
      return "tag t-ok";
    case "amended":
      return "tag t-warn";
    case "locked":
      return "tag t-info";
    case "not_issued":
      return "tag t-dng";
    default:
      return "tag t-mut";
  }
}

// The single date that anchors a workflow chip: when it was frozen (issued/amended/locked) or, for a
// state where nothing is frozen, when it is DUE to be issued.
function workflowInstant(wf: FeedDirectionWorkflowLifecycle): string | undefined {
  switch (wf.state) {
    case "issued":
      return wf.issued_at;
    case "amended":
      return wf.amended_at;
    case "locked":
      return wf.locked_at;
    default:
      return wf.expected_issue_at;
  }
}

// The instant shown next to the aggregate title for a frozen sheet.
function headlineInstant(lifecycle: FeedDirectionLifecycle): string | undefined {
  switch (lifecycle.state) {
    case "issued":
      return lifecycle.issued_at;
    case "amended":
      return lifecycle.amended_at;
    case "locked":
      return lifecycle.locked_at;
    default:
      return undefined;
  }
}

export function FeedLifecycleBanner({
  lifecycle,
  feedDay,
  pageContract,
}: {
  lifecycle: FeedDirectionLifecycle;
  feedDay: string;
  pageContract: AdminUiPageContract;
}) {
  const state = lifecycle.state;
  const Icon = STATE_ICON[state];
  const instant = headlineInstant(lifecycle);
  const nothingIssued = state === "pending" || state === "not_issued";

  // Per-workflow breakdown is legible only when the workflows disagree (e.g. normal issued at 07:00
  // while experiment is still pending until 14:00), or when nothing is issued yet and the expected
  // issue times per workflow are the point. A uniform park-day says all it needs to in the headline.
  const statesDiffer = new Set(lifecycle.workflows.map((wf) => wf.state)).size > 1;
  const showBreakdown = lifecycle.workflows.length > 1 && (statesDiffer || nothingIssued);

  return (
    <div
      className={alertClass(state)}
      role="status"
      aria-label={copy(pageContract, "lifecycle.aria")}
      style={{ marginBottom: 16, alignItems: "flex-start" }}
    >
      <Icon className="ic" aria-hidden="true" />
      <div style={{ display: "flex", flexDirection: "column", gap: 6, minWidth: 0 }}>
        <div style={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", gap: 8 }}>
          <b>{copy(pageContract, `lifecycle.${state}.title`)}</b>
          {/* A frozen sheet is anchored by WHEN it was frozen; a not-yet-issued day by WHICH day it
              is. Both are client-formatted from a backend instant/date, never a literal. */}
          {instant ? (
            <span style={{ fontVariantNumeric: "tabular-nums" }}>{fmtDateTime(instant)}</span>
          ) : nothingIssued ? (
            <span style={{ fontVariantNumeric: "tabular-nums" }}>{fmtDate(feedDay)}</span>
          ) : null}
          {state === "amended" ? (
            <span className="tag t-warn">
              {lifecycle.amendment_count} {copy(pageContract, "lifecycle.corrections_noun")}
            </span>
          ) : null}
        </div>

        <div className="small muted">{copy(pageContract, `lifecycle.${state}.body`)}</div>

        {showBreakdown ? (
          <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
            {lifecycle.workflows.map((wf) => {
              const at = workflowInstant(wf);
              return (
                <span key={wf.workflow} className={workflowChipTone(wf.state)}>
                  {copy(pageContract, `lifecycle.workflow.${wf.workflow}`)}{" · "}
                  {copy(pageContract, `lifecycle.state.${wf.state}`)}
                  {at ? <>{" · "}{fmtDateTime(at)}</> : null}
                </span>
              );
            })}
          </div>
        ) : null}
      </div>
    </div>
  );
}

/**
 * A served park-day whose empty table is EXPLAINED by the lifecycle (nothing was issued), as opposed
 * to a filter excluding rows from a sheet that does exist. Only the former replaces the table with
 * the banner; the latter keeps the normal "no rows match these filters" empty state.
 */
export function isLifecycleEmpty(lifecycle: FeedDirectionLifecycle, rowCount: number): boolean {
  return rowCount === 0 && (lifecycle.state === "pending" || lifecycle.state === "not_issued");
}
