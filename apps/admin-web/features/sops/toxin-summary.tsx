import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseToxin } from "./toxin-model";
import { SummaryRoot, SummaryHeading, SummarySub, SummaryList, SummaryRow, SummaryTitle, SummaryMeta, SummaryNote } from "./sop-summary";

// THE TOXIN PROCEDURE IS AUTHORED (2026-09-20): what the library drawer shows for the aflatoxin
// SOP -- the steps in order, each with its kind, its wait, and the step it waits on. Read-only;
// "Change procedure" opens the editor.
export function ToxinSummary({ pageContract: pc, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parseToxin(formDsl);
  if (!rows) return null;
  const kindLabel = (kind: string) => copy(pc, `tsop.summary.kind.${kind}`);
  const waiting = rows.steps.reduce((sum, s) => sum + (s.kind === "wait" ? s.waitMinutes : 0), 0);
  return (
    <SummaryRoot>
      <SummaryHeading>
        {copy(pc, "tsop.steps")}{" "}
        <SummarySub>
          — {rows.steps.length} {copy(pc, "tsop.steps.unit")}
          {waiting > 0 ? `, ${copy(pc, "tsop.steps.waiting")} ${waiting} ${copy(pc, "tsop.unit.minutes")}` : ""}
        </SummarySub>
      </SummaryHeading>
      <SummaryList>
        {rows.steps.map((step, i) => {
          const gate =
            step.kind !== "wait" && step.gateAfterStep > 0 && step.gateMinutes > 0
              ? ` · ${copy(pc, "tsop.flow.after")} ${step.gateMinutes} ${copy(pc, "tsop.unit.minutes")} (${rows.steps[step.gateAfterStep - 1]?.title ?? `#${step.gateAfterStep}`})`
              : "";
          const wait = step.kind === "wait" ? ` · ${step.waitMinutes} ${copy(pc, "tsop.unit.minutes")}` : "";
          return (
            <SummaryRow key={step.id}>
              <SummaryTitle>
                {i + 1}. {step.title || copy(pc, "tsop.step.untitled")}
              </SummaryTitle>
              <SummaryMeta>
                {kindLabel(step.kind)}
                {wait}
                {gate}
              </SummaryMeta>
              {step.instruction ? <SummaryNote>{step.instruction}</SummaryNote> : null}
            </SummaryRow>
          );
        })}
      </SummaryList>
    </SummaryRoot>
  );
}
