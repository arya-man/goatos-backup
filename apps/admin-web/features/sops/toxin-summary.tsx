import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseToxin } from "./toxin-model";

// THE TOXIN PROCEDURE IS AUTHORED (2026-09-20): what the library drawer shows for the aflatoxin
// SOP -- the steps in order, each with its kind, its wait, and the step it waits on. Read-only;
// "Change procedure" opens the editor.
export function ToxinSummary({ pageContract: pc, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parseToxin(formDsl);
  if (!rows) return null;
  const kindLabel = (kind: string) => copy(pc, `tsop.summary.kind.${kind}`);
  const waiting = rows.steps.reduce((sum, s) => sum + (s.kind === "wait" ? s.waitMinutes : 0), 0);
  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pc, "tsop.steps")}{" "}
        <span className="muted small">
          — {rows.steps.length} {copy(pc, "tsop.steps.unit")}
          {waiting > 0 ? `, ${copy(pc, "tsop.steps.waiting")} ${waiting} ${copy(pc, "tsop.unit.minutes")}` : ""}
        </span>
      </div>
      <div className="htl">
        {rows.steps.map((step, i) => {
          const gate =
            step.kind !== "wait" && step.gateAfterStep > 0 && step.gateMinutes > 0
              ? ` · ${copy(pc, "tsop.flow.after")} ${step.gateMinutes} ${copy(pc, "tsop.unit.minutes")} (${rows.steps[step.gateAfterStep - 1]?.title ?? `#${step.gateAfterStep}`})`
              : "";
          const wait = step.kind === "wait" ? ` · ${step.waitMinutes} ${copy(pc, "tsop.unit.minutes")}` : "";
          return (
            <div className="hrow" key={step.id}>
              <div className="htx">
                <b>
                  {i + 1}. {step.title || copy(pc, "tsop.step.untitled")}
                </b>
                <div className="hmeta muted small">
                  {kindLabel(step.kind)}
                  {wait}
                  {gate}
                </div>
                {step.instruction ? <div className="muted small">{step.instruction}</div> : null}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
