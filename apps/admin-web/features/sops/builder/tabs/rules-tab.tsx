"use client";

import { useRef, useState } from "react";
import type { Dispatch } from "react";
import type { BuilderRule, BuilderState, RuleOperator } from "../model";
import type { Action } from "../state";
import { RULE_ACTIONS, RULE_LIBRARY } from "../templates";

const OP_LABELS: Record<RuleOperator, string> = {
  equals: "equals",
  not_equals: "does not equal",
  in: "is one of",
  empty: "is empty",
  not_empty: "is not empty",
};

export function RulesTab({ state, dispatch }: { state: BuilderState; dispatch: Dispatch<Action> }) {
  const ruleSeq = useRef(0);
  const fieldKeys = state.fields.map((f) => f.key);
  const [whenField, setWhenField] = useState(fieldKeys[0] ?? "");
  const [operator, setOperator] = useState<RuleOperator>("equals");
  const [value, setValue] = useState("high");
  const [actionValue, setActionValue] = useState(RULE_ACTIONS[1].value);
  const [targetField, setTargetField] = useState(fieldKeys[0] ?? "");
  const [message, setMessage] = useState("");

  const needsValue = operator === "equals" || operator === "not_equals" || operator === "in";

  const addRule = () => {
    const spec = RULE_ACTIONS.find((a) => a.value === actionValue) ?? RULE_ACTIONS[1];
    ruleSeq.current += 1;
    const rule: BuilderRule = {
      id: `custom_${ruleSeq.current}_${state.rules.length}`,
      type: spec.type,
      whenField: whenField || fieldKeys[0] || "",
      operator,
      value: needsValue ? value : "",
      targetField: spec.type === "block_submission_if" ? "" : targetField,
      message: message.trim() || defaultMessage(spec.label),
      on: true,
      custom: true,
    };
    dispatch({ kind: "addRule", rule });
    setMessage("");
  };

  return (
    <div>
      <div className="flow-note">
        <h3>Form Logic decides how the form behaves.</h3>
        <p>Plain if/then rules: when something happens, show a field, make it required, block submit, or require proof.</p>
        <div className="info-line">
          <span className="i">i</span>
          <span>Example: if source shed does not match the goat&apos;s location, show Exception Reason and make it required.</span>
        </div>
      </div>

      <div className="panel">
        <h3>Add Form Logic</h3>
        <div className="desc">Create one simple rule at a time. The Android preview and backend validation follow the same rule.</div>
        <div className="builder-form">
          <div className="inputgroup">
            <label>When this field</label>
            <select value={whenField} onChange={(e) => setWhenField(e.target.value)}>
              {fieldKeys.map((k) => <option key={k} value={k}>{k}</option>)}
            </select>
          </div>
          <div className="inputgroup">
            <label>Has condition</label>
            <select value={operator} onChange={(e) => setOperator(e.target.value as RuleOperator)}>
              {(Object.keys(OP_LABELS) as RuleOperator[]).map((op) => <option key={op} value={op}>{OP_LABELS[op]}</option>)}
            </select>
          </div>
          <div className="inputgroup">
            <label>This value</label>
            <input value={value} disabled={!needsValue} placeholder={operator === "in" ? "comma,separated" : "value"} onChange={(e) => setValue(e.target.value)} />
          </div>
          <div className="inputgroup">
            <label>Do this</label>
            <select value={actionValue} onChange={(e) => setActionValue(e.target.value)}>
              {RULE_ACTIONS.map((a) => <option key={a.value} value={a.value}>{a.label}</option>)}
            </select>
          </div>
          <div className="inputgroup">
            <label>Target field</label>
            <select value={targetField} onChange={(e) => setTargetField(e.target.value)} disabled={actionValue === "block_submission"}>
              {fieldKeys.map((k) => <option key={k} value={k}>{k}</option>)}
            </select>
          </div>
          <div className="inputgroup wide">
            <label>Message</label>
            <input value={message} placeholder="shown when the rule fires" onChange={(e) => setMessage(e.target.value)} />
          </div>
          <button className="btn small" type="button" onClick={addRule} disabled={fieldKeys.length === 0}>Add rule</button>
        </div>
      </div>

      <div className="panel">
        <h3>Active Form Logic</h3>
        <div className="desc">Turn rules on/off to test the form. These are the rules the backend re-checks before saving.</div>
        {state.rules.length === 0 ? (
          <div className="octl">No rules yet.</div>
        ) : (
          state.rules.map((r) => (
            <div key={r.id} className="rule">
              <label className="sw" title={r.on ? "Active" : "Off"}>
                <input type="checkbox" checked={r.on} onChange={(e) => dispatch({ kind: "updateRule", id: r.id, patch: { on: e.target.checked } })} />
                <span />
              </label>
              <div className="rtxt">
                <div className="rule-title">
                  {r.custom ? <span className="pill teal" style={{ marginRight: 6 }}>custom</span> : null}
                  {ruleText(r)}
                </div>
                <div className="rule-help">{r.message || helpText(r.type)}</div>
              </div>
              <button className="mini-btn danger" type="button" onClick={() => dispatch({ kind: "removeRule", id: r.id })}>Remove</button>
            </div>
          ))
        )}
      </div>

      <div className="panel">
        <h3>Rule Type Library</h3>
        <div className="desc">Admins build these as visual condition/action blocks; preview and backend dry-run must show the same outcome.</div>
        <div className="mini-grid">
          {RULE_LIBRARY.map((c) => (
            <div key={c.title} className="mini-card">
              <h4>{c.title}</h4>
              <ul>{c.items.map((it) => <li key={it}>{it}</li>)}</ul>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function ruleText(r: BuilderRule): string {
  const needsValue = r.operator === "equals" || r.operator === "not_equals" || r.operator === "in";
  const valuePart = needsValue && r.value ? ` ${r.value}` : "";
  const actionLabel = RULE_ACTIONS.find((a) => a.type === r.type)?.label ?? r.type;
  const targetPart = r.targetField ? ` ${r.targetField}` : "";
  return `If ${r.whenField || "field"} ${OP_LABELS[r.operator]}${valuePart} → ${actionLabel}${targetPart}`;
}

function helpText(type: BuilderRule["type"]): string {
  switch (type) {
    case "block_submission_if":
      return "Blocks bad submissions before they become records.";
    case "proof_required_if":
      return "Controls when photo/video proof is required.";
    case "required_if":
      return "Changes which fields operators must fill.";
    case "visible_if":
      return "Shows or hides a field based on another answer.";
    default:
      return "Backend re-checks this before saving.";
  }
}

function defaultMessage(actionLabel: string): string {
  return `Rule: ${actionLabel}.`;
}
