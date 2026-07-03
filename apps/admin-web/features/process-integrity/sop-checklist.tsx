import { Check, Video } from "lucide-react";
import { Tag } from "@/components/ui-primitives";
import type { SopStep } from "@/features/preventive-care-vaccination";

// SopChecklist renders an SOP's procedure steps as the mock's drawer checklist (numbered/checked circles,
// step description, "video proof required" pill) — NOT the obligation lifecycle chain (that is the
// Workflow record's stepper). `doneThrough` is derived from the obligation's computed states: steps below
// it are done, the step at it is current. Same `.stepper` anatomy as the mock #taskDrawer checklist.
export function SopChecklist({ steps, doneThrough }: { steps: SopStep[]; doneThrough: number }) {
  return (
    <div className="stepper">
      {steps.map((s, i) => {
        const done = i < doneThrough;
        const cur = i === doneThrough;
        return (
          <div key={s.title} className={`step${done ? " done" : ""}${cur ? " cur" : ""}`}>
            <div className="ln" />
            <div className="no">
              {done ? <Check className="ic" style={{ width: 14, strokeWidth: 2.4 }} aria-hidden="true" /> : i + 1}
            </div>
            <div className="ct">
              <b>{s.title}</b>
              <div className="d">{s.detail}</div>
              {s.videoProof ? (
                <div className="vp">
                  <Tag tone="pur">
                    <Video className="ic" style={{ width: 12 }} aria-hidden="true" /> video proof required
                  </Tag>
                </div>
              ) : null}
            </div>
          </div>
        );
      })}
    </div>
  );
}
