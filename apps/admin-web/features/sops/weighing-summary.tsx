"use client";

// WEIGHING SOP: the read-only summary of the rules a weighing task runs under, shown in the SOP
// drawer so the whole rule set is visible without opening the editor.
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { parseWeighing, type WeighingQuestionRow } from "./weighing-model";

function fill(template: string, vars: Record<string, string | number>): string {
  return Object.entries(vars).reduce((s, [k, v]) => s.split(`{${k}}`).join(String(v)), template);
}

function questionMeta(pc: AdminUiPageContract, q: WeighingQuestionRow, titleByKey: Record<string, string>): string {
  const parts: string[] = [copy(pc, `inspection.kind.${q.kind}`)];
  if (q.kind === "choice" || q.kind === "multi") parts.push(q.options.map((o) => o.label).join(" / "));
  if (q.kind === "number" && q.unit) parts.push(q.unit);
  parts.push(copy(pc, q.required ? "inspection.summary.required" : "inspection.summary.optional"));
  if (q.onlyIfQuestion) parts.push(fill(copy(pc, "inspection.summary.only_if"), { question: titleByKey[q.onlyIfQuestion] ?? q.onlyIfQuestion, value: q.onlyIfValue }));
  return parts.join(" · ");
}

export function WeighingSummary({ pageContract: pc, formDsl }: { pageContract: AdminUiPageContract; formDsl: unknown }) {
  const rows = parseWeighing(formDsl);
  if (!rows) return null;
  const titleByKey = Object.fromEntries(rows.removalQuestions.map((q) => [q.key, q.title]));
  return (
    <div className="inspection-summary">
      <div className="b700" style={{ margin: "14px 0 8px" }}>
        {copy(pc, "wsop.drawer.title")} <span className="muted small">— {copy(pc, "wsop.drawer.subtitle")}</span>
      </div>
      <div className="htl">
        <div className="hrow">
          <div className="htx">
            <b>{copy(pc, "wsop.section.planning")}</b>
            <div className="hmeta muted small">
              {rows.modes.map((m) => copy(pc, `wsop.planning.mode.${m}`)).join(" · ")} · {copy(pc, "wsop.planning.default_cap")}: {rows.defaultCapPerDay}
            </div>
          </div>
        </div>
        <div className="hrow">
          <div className="htx">
            <b>{copy(pc, "wsop.section.removal")}</b>
            <div className="hmeta muted small">
              {copy(pc, `wsop.removal.mode.${rows.removalMode}`)}
              {rows.removalMode !== "off"
                ? ` · ${rows.removalProofs.map((p) => `${p.title} (${copy(pc, `wsop.proof.kind.${p.kind}`)}${p.required ? "" : `, ${copy(pc, "inspection.summary.optional")}`})`).join(" + ")}`
                : ""}
            </div>
            {rows.removalMode !== "off" && rows.removalInstruction ? <div className="muted small">{rows.removalInstruction}</div> : null}
            {rows.removalMode !== "off" ? (
              <div className="muted small">
                {copy(pc, "wsop.removal.cutoff")}: {rows.removalCutoffTime || copy(pc, "wsop.removal.cutoff.farm")}
              </div>
            ) : null}
          </div>
        </div>
        <div className="hrow">
          <div className="htx">
            <b>{copy(pc, "wsop.section.capture")}</b>
            <div className="hmeta muted small">
              {copy(pc, "wsop.capture.individual.video")} · {fill(copy(pc, "wsop.summary.lump_sum_videos"), { min: rows.lumpSumVideoMin, max: rows.lumpSumVideoMax })}
            </div>
          </div>
        </div>
        <div className="hrow">
          <div className="htx">
            <b>{copy(pc, "wsop.section.weights")}</b>
            <div className="hmeta muted small">
              {rows.weightsFromMode === "rolling_weeks"
                ? fill(copy(pc, "wsop.summary.weights_rolling_weeks"), { weeks: rows.weightsFromWeeks })
                : rows.weightsFromMode === "rolling_days"
                ? fill(copy(pc, "wsop.summary.weights_rolling"), { days: rows.weightsFromDays })
                : fill(copy(pc, "wsop.summary.weights_fixed"), { date: rows.weightsFromDate })}
              {" · "}
              {fill(copy(pc, "wsop.summary.weights_earliest"), { date: rows.weightsEarliestDate })}
            </div>
          </div>
        </div>
      </div>
      {rows.removalMode !== "off" && rows.removalQuestions.length > 0 ? (
        <div>
          <div className="muted small b700" style={{ margin: "8px 0 4px" }}>
            {copy(pc, "wsop.removal.questions")}
          </div>
          <div className="htl">
            {rows.removalQuestions.map((q, qi) => (
              <div className="hrow" key={q.id}>
                <div className="htx">
                  <b>
                    {qi + 1}. {q.title}
                  </b>
                  <div className="hmeta muted small">{questionMeta(pc, q, titleByKey)}</div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}
    </div>
  );
}
