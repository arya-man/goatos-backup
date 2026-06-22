"use client";

import type { BuilderField, BuilderState } from "./model";
import { flowFacts, type Outcome, type Scenario } from "./preview-engine";
import { titleCase } from "./templates";

const OUTCOME_PATH_HINT: Record<string, string> = {
  operator_execution: "step",
  approval: "approval",
  proof_verification: "proof",
  accepted: "accepted",
  rework: "rework",
  blocked: "review",
  rejected: "review",
};

export function AndroidPreview({
  state,
  scenario,
  onScenario,
  outcome,
}: {
  state: BuilderState;
  scenario: Scenario;
  onScenario: (patch: Partial<Scenario>) => void;
  outcome: Outcome;
}) {
  const facts = flowFacts(state);
  const shortName = state.meta.title.replace(/\s+SOP$/i, "");
  const banner = bannerFor(outcome);

  return (
    <aside className="preview">
      <div className="eyebrow" style={{ marginBottom: 4 }}>Live Android Preview</div>
      <p className="preview-note">This is what the operator sees. It updates from the saved form fields and active rules.</p>

      <div className="phone">
        <div className="notch" />
        <div className="pscreen">
          <div className="pbar">
            <span className="pt">{shortName} · <span className="mono" style={{ fontSize: 11 }}>TASK-4471</span></span>
            <span className="pill teal">{state.meta.typeLabel}</span>
          </div>
          {banner ? <div className={`banner ${banner.cls}`}>{banner.text}</div> : null}
          <div>
            {state.fields.length === 0 ? (
              <div className="octl">No fields yet — add some in Operator Form.</div>
            ) : (
              state.fields.map((field, i) => <OperatorControl key={`${field.key}-${i}`} field={field} proofCaptured={scenario.proofCaptured} />)
            )}
          </div>
          <button className="submit" disabled={!outcome.canSubmit}>{outcome.canSubmit ? state.meta.submitLabel : "Cannot submit yet"}</button>
        </div>
      </div>

      <div style={{ marginTop: 14 }}>
        <p className="sectionlabel">Try example situations</p>
        <p className="preview-note">Operators will not see these controls — they drive the preview only.</p>
        <div className="panel" style={{ padding: "8px 12px" }}>
          {facts.hasProofField ? (
            <div className="togglerow" style={{ borderTop: 0 }}>
              <span>Proof captured</span>
              <label className="sw"><input type="checkbox" checked={scenario.proofCaptured} onChange={(e) => onScenario({ proofCaptured: e.target.checked })} /><span /></label>
            </div>
          ) : null}
          {facts.hasApproval ? (
            <div className="togglerow">
              <span>Approval decision</span>
              <select style={{ width: 130 }} value={scenario.supervisor} onChange={(e) => onScenario({ supervisor: e.target.value as Scenario["supervisor"] })}>
                <option value="pending">pending</option>
                <option value="approved">approved</option>
                <option value="rejected">rejected</option>
              </select>
            </div>
          ) : null}
          {facts.hasProofVerification ? (
            <div className="togglerow">
              <span>Review decision</span>
              <select style={{ width: 130 }} value={scenario.verifier} onChange={(e) => onScenario({ verifier: e.target.value as Scenario["verifier"] })}>
                <option value="pending">pending</option>
                <option value="approved">approved</option>
                <option value="rework">rework</option>
              </select>
            </div>
          ) : null}
          {facts.hasBlockRule ? (
            <div className="togglerow">
              <span>Trigger a block rule</span>
              <label className="sw"><input type="checkbox" checked={scenario.triggerBlock} onChange={(e) => onScenario({ triggerBlock: e.target.checked })} /><span /></label>
            </div>
          ) : null}
        </div>
      </div>

      <div className="outcome">
        <div className="head">
          <span className="sectionlabel" style={{ margin: 0 }}>What will happen</span>
          <span className={`pill ${outcome.tone === "green" ? "green" : outcome.tone === "red" ? "red" : "amber"}`}>{outcome.canSubmit ? "submittable" : "blocked"}</span>
        </div>
        <div className="ostate" style={{ color: toneColor(outcome.tone) }}>{outcome.state}</div>
        <div className="kv">{outcome.kv}</div>
        <OutcomePath state={state} lit={outcome.lit} />
        <div className="hint">{outcome.hint}</div>
      </div>
    </aside>
  );
}

function OutcomePath({ state, lit }: { state: BuilderState; lit: Set<string> }) {
  const litNodes = state.nodes.filter((n) => lit.has(n.id));
  return (
    <div className="trace">
      {litNodes.map((n, i) => {
        let cls = "step on";
        if (n.type === "accepted") cls = "step end-accept";
        else if (n.type === "rework") cls = "step end-rework";
        else if (n.type === "blocked" || n.type === "rejected") cls = "step end-review";
        return (
          <span key={n.id} style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            {i > 0 ? <span className="sep">›</span> : null}
            <span className={cls} title={OUTCOME_PATH_HINT[n.type]}>{n.label}</span>
          </span>
        );
      })}
    </div>
  );
}

function OperatorControl({ field, proofCaptured }: { field: BuilderField; proofCaptured: boolean }) {
  const label = field.label || titleCase(field.key);
  const required = field.required;
  const lab = (
    <div className="olab">
      {label}
      {required ? <span className="req">*</span> : null}
      {field.cond ? <span className="pill amber" style={{ marginLeft: 4 }}>if {field.cond}</span> : null}
    </div>
  );
  const help = field.description ? <div className="hint">{field.description}</div> : null;
  const optionsHint = field.options ? ` · ${field.options}` : "";

  let control: React.ReactNode;
  switch (field.type) {
    case "goat_lookup":
      control = (
        <div className="octl scan">
          <span><span className="goatchip">G-000101</span><span className="goatchip">G-000102</span></span>
          <span style={{ color: "var(--sb-teal)" }}>＋ scan</span>
        </div>
      );
      break;
    case "rfid_scan":
      control = <div className="octl">tap to scan RFID</div>;
      break;
    case "location_picker":
      control = <div className="octl">choose shed / location</div>;
      break;
    case "number":
      control = <div className="octl">{field.defaultValue || field.placeholder || "0"}</div>;
      break;
    case "select":
      control = <div className="octl">{field.defaultValue || "select option"}{optionsHint}</div>;
      break;
    case "multiselect":
      control = <div className="octl">{field.defaultValue || "choose one or more"}{optionsHint}</div>;
      break;
    case "date_time":
      control = <div className="octl">date / time picker</div>;
      break;
    case "photo_proof":
      control = <div className={`proofbox ${proofCaptured ? "have" : ""}`}>{proofCaptured ? "📷 photo captured" : "tap to capture photo"}</div>;
      break;
    case "video_proof":
      control = <div className={`proofbox ${proofCaptured ? "have" : ""}`}>{proofCaptured ? "🎥 video captured" : "tap to record video"}</div>;
      break;
    default:
      control = <div className="octl">{field.defaultValue || field.placeholder || "text input"}</div>;
  }

  return (
    <div className="ofield">
      {lab}
      {control}
      {help}
    </div>
  );
}

function bannerFor(outcome: Outcome): { cls: string; text: string } | null {
  if (outcome.state.startsWith("BLOCKED")) return { cls: "block", text: "⛔ Submission blocked by a rule — routed to review." };
  if (outcome.state === "REQUEST REJECTED") return { cls: "block", text: "⛔ Supervisor rejected this request." };
  if (outcome.state === "CANNOT SUBMIT") return { cls: "warn", text: "⚠ Proof is required before this can be submitted." };
  return null;
}

function toneColor(tone: Outcome["tone"]): string {
  if (tone === "green") return "#86efac";
  if (tone === "red") return "#fca5a5";
  return "#fcd34d";
}
