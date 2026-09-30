"use client";

// SOP-DRIVEN HERD OPERATIONS: the read-only list of what the phone runs after the event, shown in
// the SOP drawer beside the capture fields. A repeating round (the colostrum series) is expanded
// into every time it runs so the whole day's timings are visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { branchPhrase } from "./branch-field";
import { describeDue, describeProof, expandSeriesRows, formatAfter, parseFollowUp, type ExpandedRound, type FollowUpCopy } from "./followup-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryGroup, SummaryList, SummaryRow, SummaryTitle, SummaryMeta } from "./sop-summary";

export function followUpCopy(pc: AdminUiPageContract): FollowUpCopy {
  return (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), copy(pc, key));
}

export function dayLabel(pc: AdminUiPageContract, dayOffset: number): string {
  return dayOffset === 0 ? copy(pc, "followup.day.event") : followUpCopy(pc)("followup.day.after", { d: dayOffset });
}

// roundWhen renders one expanded round's timing: "event day · 07:00" for a fixed-times round,
// "4 h after the event" for a from-event round.
export function roundWhen(pc: AdminUiPageContract, r: ExpandedRound): string {
  if (r.afterMinutes !== undefined) return followUpCopy(pc)("followup.round.after", { after: formatAfter(r.afterMinutes) });
  return `${dayLabel(pc, r.dayOffset)} · ${r.time}`;
}

export function FollowUpStepsSummary({ pageContract, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parseFollowUp(formDsl);
  if (!rows) return null;
  const c = followUpCopy(pageContract);
  return (
    <SummaryRoot>
      <SummaryHeading>
        {copy(pageContract, "followup.drawer.title")} <SummarySub>— {copy(pageContract, "followup.drawer.subtitle")}</SummarySub>
      </SummaryHeading>
      {rows.tracks.map((track) => {
        const titleByKey = Object.fromEntries(track.steps.map((s) => [s.key, s.title || s.titlePattern || s.key]));
        let n = 0;
        return (
          <div key={track.key} className="followup-summary-track">
            <SummaryGroup dense>
              {copy(pageContract, "followup.track")} · {track.label || track.key}
            </SummaryGroup>
            <SummaryList>
              {track.steps.flatMap((step) => {
                const proof = describeProof(step, c);
                // The legacy `when` (kid pen unresolved) and an answer-driven branch both read as
                // conditions here; a branch step was rendered as unconditional before (PR 308 review).
                const when = [step.when ? copy(pageContract, "followup.step.only_when") : "", step.whenStep ? branchPhrase(pageContract, step, titleByKey[step.whenStep] ?? step.whenStep) : ""]
                  .filter(Boolean)
                  .map((t) => ` · ${t}`)
                  .join("");
                if (step.scheduleKind === "series") {
                  return expandSeriesRows(step).map((r) => {
                    n += 1;
                    return (
                      <SummaryRow key={`${step.key}-${r.dayOffset}-${r.time}-${r.afterMinutes ?? ""}`}>
                        <SummaryTitle>
                          {n}. {r.title}
                        </SummaryTitle>
                        <SummaryMeta>
                          {roundWhen(pageContract, r)}
                          {proof ? ` · ${proof}` : ""}
                        </SummaryMeta>
                      </SummaryRow>
                    );
                  });
                }
                n += 1;
                return [
                  <SummaryRow key={step.key}>
                    <SummaryTitle>
                      {n}. {step.title || step.titlePattern || step.key}
                    </SummaryTitle>
                    <SummaryMeta>
                      {describeDue(step, c, titleByKey)}
                      {proof ? ` · ${proof}` : ""}
                      {when}
                    </SummaryMeta>
                  </SummaryRow>,
                ];
              })}
            </SummaryList>
          </div>
        );
      })}
    </SummaryRoot>
  );
}
