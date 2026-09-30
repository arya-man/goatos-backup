import { Tag } from "@/components/ui-primitives";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Label, type LabelColor } from "@/components/minimal/label";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedDirectionLifecycle, FeedDirectionWorkflowLifecycle } from "@/lib/api/server";
import { fmtDate, fmtDateTime } from "@/lib/format";

import { isLifecycleEmpty } from "./feed-lifecycle-state";
import Alert, { type AlertColor as AlertSeverity } from "@mui/material/Alert";

// Re-exported so existing importers (feed-direction.tsx, feed-packing.tsx) keep a single import site;
// the pure predicate lives in feed-lifecycle-state.ts so it is unit-testable without JSX.
export { isLifecycleEmpty };

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

const STATE_ICON: Record<LifecycleState, IconifyName> = {
  issued: "solar:check-circle-bold",
  amended: "solar:pen-bold",
  locked: "solar:lock-password-outline",
  pending: "solar:clock-circle-bold",
  not_issued: "solar:danger-triangle-bold",
  // `preview` = the rows were GENERATED on demand for a day with no issued sheet. Informational, not
  // alarming — an eye, not a warning triangle.
  preview: "solar:eye-bold",
  // `beyond_horizon` = the day is outside the [today, tomorrow] projection window and has no issued
  // sheet, so no rows could be produced. A calendar-off marker: nothing wrong, just out of range.
  beyond_horizon: "solar:forbidden-circle-bold",
  draft: "solar:atom-bold-duotone",
};

// kit Alert severity. success = a clean issued sheet; warn = amended (corrections folded in) or the not_issued
// gap; info = the neutral locked / pending / draft states (nothing wrong, just not a frozen-and-ready
// sheet). Danger is reserved for the API-failure band the page renders separately.
function alertSeverity(state: LifecycleState): AlertSeverity {
  if (state === "issued") return "success";
  if (state === "amended" || state === "not_issued") return "warning";
  return "info";
}
function workflowChipColor(state: WorkflowState): LabelColor {
  switch (state) {
    case "issued":
      return "success";
    case "amended":
      return "warning";
    case "locked":
      return "info";
    case "not_issued":
      return "error";
    default:
      return "default";
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
  const icon = STATE_ICON[state];
  const instant = headlineInstant(lifecycle);
  // Not-yet-frozen states are anchored by WHICH feed day they cover and by the per-workflow expected
  // issue times, not by a frozen instant. `preview` (rows generated on demand) belongs here too — it
  // is a not-yet-issued day that now carries data. `beyond_horizon` is anchored by the feed day it
  // covers as well (the day is simply out of the projection window).
  const notFrozen =
    state === "pending" || state === "not_issued" || state === "preview" || state === "beyond_horizon";

  // Per-workflow breakdown is legible only when the workflows disagree (e.g. normal issued at 07:00
  // while experiment is still pending until 14:00), or when nothing is issued yet and the expected
  // issue times per workflow are the point. A uniform park-day says all it needs to in the headline.
  const statesDiffer = new Set(lifecycle.workflows.map((wf) => wf.state)).size > 1;
  const showBreakdown = lifecycle.workflows.length > 1 && (statesDiffer || notFrozen);

  return (
    <Alert
      severity={alertSeverity(state)}
      icon={<Iconify icon={icon} aria-hidden="true" />}
      role="status"
      aria-label={copy(pageContract, "lifecycle.aria")}
      sx={{ mb: 2 }}
    >
      <Box sx={{ display: "flex", flexDirection: "column", gap: 0.75, minWidth: 0 }}>
        <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "baseline", gap: 1 }}>
          <b>{copy(pageContract, `lifecycle.${state}.title`)}</b>
          {/* A frozen sheet is anchored by WHEN it was frozen; a not-yet-issued day by WHICH day it
              is. Both are client-formatted from a backend instant/date, never a literal. */}
          {instant ? (
            <Box component="span" sx={{ fontVariantNumeric: "tabular-nums" }}>{fmtDateTime(instant)}</Box>
          ) : notFrozen ? (
            <Box component="span" sx={{ fontVariantNumeric: "tabular-nums" }}>{fmtDate(feedDay)}</Box>
          ) : null}
          {state === "amended" ? (
            <Tag tone="warn">
              {lifecycle.amendment_count} {copy(pageContract, "lifecycle.corrections_noun")}
            </Tag>
          ) : null}
        </Box>

        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {copy(pageContract, `lifecycle.${state}.body`)}
        </Typography>

        {showBreakdown ? (
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
            {lifecycle.workflows.map((wf) => {
              const at = workflowInstant(wf);
              return (
                <Label key={wf.workflow} variant="soft" color={workflowChipColor(wf.state)}>
                  {copy(pageContract, `lifecycle.workflow.${wf.workflow}`)}{" · "}
                  {copy(pageContract, `lifecycle.state.${wf.state}`)}
                  {at ? <>{" · "}{fmtDateTime(at)}</> : null}
                </Label>
              );
            })}
          </Box>
        ) : null}
      </Box>
    </Alert>
  );
}
