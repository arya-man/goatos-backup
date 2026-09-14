"use client";

// SOP-DRIVEN HERD OPERATIONS: the read-only list of what the phone runs after the event, shown in
// the SOP drawer beside the capture fields. A repeating round (the colostrum series) is expanded
// into every time it runs so the whole day's timings are visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { describeDue, describeProof, expandSeriesRows, formatAfter, parseFollowUp, type ExpandedRound, type FollowUpCopy } from "./followup-model";

export function followUpCopy(pc: AdminUiPageContract): FollowUpCopy {
  return (key, vars = {}) =>
    Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), copy(pc, key));
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
    <div className="followup-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pageContract, "followup.drawer.title")}{" "}
        <span className="muted small">— {copy(pageContract, "followup.drawer.subtitle")}</span>
      </div>
      {rows.tracks.map((track) => {
        const titleByKey = Object.fromEntries(track.steps.map((s) => [s.key, s.title || s.titlePattern || s.key]));
        let n = 0;
        return (
          <div key={track.key} className="followup-summary-track">
            <div className="muted small b700" style={{ margin: "6px 0" }}>
              {copy(pageContract, "followup.track")} · {track.label || track.key}
            </div>
            <div className="htl">
              {track.steps.flatMap((step) => {
                const proof = describeProof(step, c);
                const when = step.when ? ` · ${copy(pageContract, "followup.step.only_when")}` : "";
                if (step.scheduleKind === "series") {
                  return expandSeriesRows(step).map((r) => {
                    n += 1;
                    return (
                      <div className="hrow" key={`${step.key}-${r.dayOffset}-${r.time}-${r.afterMinutes ?? ""}`}>
                        <div className="htx">
                          <b>
                            {n}. {r.title}
                          </b>
                          <div className="hmeta muted small">
                            {roundWhen(pageContract, r)}
                            {proof ? ` · ${proof}` : ""}
                          </div>
                        </div>
                      </div>
                    );
                  });
                }
                n += 1;
                return [
                  <div className="hrow" key={step.key}>
                    <div className="htx">
                      <b>
                        {n}. {step.title || step.titlePattern || step.key}
                      </b>
                      <div className="hmeta muted small">
                        {describeDue(step, c, titleByKey)}
                        {proof ? ` · ${proof}` : ""}
                        {when}
                      </div>
                    </div>
                  </div>,
                ];
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}
