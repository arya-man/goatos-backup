"use client";

import { fieldConfigKind, type BuilderStep } from "./sop-derive";
import { copy, optionalOptionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Read-only operator preview of the form being authored — updated live as questions change. Purely
// presentational (every control is disabled); it mirrors what an operator submits in the field.
export function BuilderPreview({ pc, steps }: { pc: AdminUiPageContract; steps: BuilderStep[] }) {
  const yesNo = optionalOptionGroup(pc, "yes_no");

  if (steps.length === 0) {
    return <div className="note">{copy(pc, "builder.preview.empty")}</div>;
  }

  return (
    <div className="pvform">
      {steps.map((step, i) => {
        const kind = fieldConfigKind(step.type);
        return (
          <div className="pvfield" key={step.id}>
            <div className="pvlabel">
              <span className="pvnum">{i + 1}</span>
              <span className="pvq">{step.label.trim() || `${copy(pc, "builder.question_label")} ${i + 1}`}</span>
              {step.required ? <span className="tag t-warn">{copy(pc, "builder.preview.required_badge")}</span> : null}
              {step.visibleWhen ? <span className="tag t-info">{copy(pc, "builder.preview.conditional_badge")}</span> : null}
            </div>
            {step.helpText.trim() ? <div className="muted small pvhelp">{step.helpText}</div> : null}

            {kind === "text" ? (
              step.longText ? (
                <textarea className="pvctl" rows={2} disabled placeholder={step.placeholder} />
              ) : (
                <input className="pvctl" disabled placeholder={step.placeholder} />
              )
            ) : null}

            {kind === "number" ? (
              <div className="pvnumrow">
                <input className="pvctl" type="number" disabled />
                {step.unit.trim() ? <span className="pvunit">{step.unit}</span> : null}
              </div>
            ) : null}

            {kind === "boolean" ? (
              <div className="chipset">
                {yesNo.map((o) => (
                  <span key={o.key} className="chip">
                    {o.label}
                  </span>
                ))}
              </div>
            ) : null}

            {kind === "options" ? (
              <div className={step.type === "multiselect" ? "pvopts multi" : "pvopts"}>
                {step.options.filter((o) => o.label.trim()).length === 0 ? (
                  <span className="muted small">{copy(pc, "builder.options.empty")}</span>
                ) : (
                  step.options
                    .filter((o) => o.label.trim())
                    .map((o) => (
                      <span className="pvopt" key={o.id}>
                        <span className={step.type === "multiselect" ? "optbox" : "optdot"} aria-hidden="true" />
                        {o.label}
                      </span>
                    ))
                )}
              </div>
            ) : null}

            {kind === "picker" ? <div className="pvpick muted small">{copy(pc, "builder.picker.note")}</div> : null}
            {kind === "scan" ? <div className="pvpick muted small">{copy(pc, "builder.scan.note")}</div> : null}
            {kind === "proof" ? <div className="pvproof muted small">{copy(pc, "builder.proof.note")}</div> : null}
          </div>
        );
      })}
    </div>
  );
}
