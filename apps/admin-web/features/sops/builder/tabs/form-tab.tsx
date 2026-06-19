"use client";

import { useState } from "react";
import type { Dispatch } from "react";
import type { BuilderField, BuilderState, FieldType } from "../model";
import type { Action } from "../state";
import { FIELD_TYPES } from "../model";
import { BUILD_STEPS, OPTION_SOURCES, PALETTE, titleCase } from "../templates";

export function FormTab({ state, dispatch }: { state: BuilderState; dispatch: Dispatch<Action> }) {
  const [dropActive, setDropActive] = useState(false);
  const isDraft = !!state.draftField;
  const selected = !isDraft && state.selectedFieldIndex != null ? state.fields[state.selectedFieldIndex] : null;
  const editing: BuilderField | null = state.draftField ?? selected;

  const patch = (p: Partial<BuilderField>) => {
    if (isDraft) dispatch({ kind: "editDraft", patch: p });
    else if (state.selectedFieldIndex != null) dispatch({ kind: "editField", index: state.selectedFieldIndex, patch: p });
  };

  return (
    <div>
      <div className="flow-note">
        <h3>Build the Android form the operator will fill.</h3>
        <p>Pick fields, rename them in plain language, choose whether they are required, and watch the phone preview update.</p>
        <div className="build-steps">
          {BUILD_STEPS.map((s, i) => (
            <div key={s.name} className={`build-step${i === 1 ? " active" : ""}`}>
              <div className="num">{s.num}</div>
              <div className="name">{s.name}</div>
              <div className="why">{s.why}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="panel">
        <h3>SOP Definition Header</h3>
        <div className="desc">A published SOP is a versioned product object, not a loose form. Edit ownership, trigger type, and code here.</div>
        <div className="definition-grid">
          <Def label="Code"><input value={state.meta.code} onChange={(e) => dispatch({ kind: "updateMeta", patch: { code: e.target.value } })} /></Def>
          <Def label="Title"><input value={state.meta.title} onChange={(e) => dispatch({ kind: "updateMeta", patch: { title: e.target.value } })} /></Def>
          <Def label="Domain"><input value={state.meta.domain} onChange={(e) => dispatch({ kind: "updateMeta", patch: { domain: e.target.value } })} /></Def>
          <Def label="Trigger"><input value={state.meta.trigger} onChange={(e) => dispatch({ kind: "updateMeta", patch: { trigger: e.target.value } })} /></Def>
          <Def label="Runner"><input value={state.meta.runner} onChange={(e) => dispatch({ kind: "updateMeta", patch: { runner: e.target.value } })} /></Def>
          <Def label="Type tag"><input value={state.meta.typeLabel} onChange={(e) => dispatch({ kind: "updateMeta", patch: { typeLabel: e.target.value } })} /></Def>
        </div>
        <label className="inline-check" style={{ marginTop: 10 }}>
          <input type="checkbox" checked={state.meta.repeatForEachGoat} onChange={(e) => dispatch({ kind: "updateMeta", patch: { repeatForEachGoat: e.target.checked } })} />
          repeat submission for each goat
        </label>
      </div>

      <div className="panel">
        <h3>Operator Form Fields — {state.meta.title.replace(/\s+SOP$/i, "")}</h3>
        <div className="desc">Pick a field type, then set the operator-facing title, help text, choices, and required flag.</div>

        <div className="toolbox">
          <h4>1. Add a field</h4>
          <div className="desc">Choose what kind of answer the operator must give. Backend-valid field types only.</div>
          <div className="palette">
            {PALETTE.map((p) => (
              <div
                key={p.type}
                className="chip"
                draggable
                onDragStart={(e) => e.dataTransfer.setData("fieldtype", p.type)}
                onClick={() => dispatch({ kind: "startDraft", fieldType: p.type })}
              >
                <span className="ic">{p.icon}</span>
                <span>{p.label}</span>
              </div>
            ))}
          </div>
        </div>

        <div className={`settings-panel${isDraft ? " draft" : ""}`}>
          <h4>2. {isDraft ? "New Field Draft" : "Field Details"}</h4>
          <div className="desc">
            {editing
              ? isDraft
                ? `Drafting ${editing.key}. Name it the way an operator should see it, then add it.`
                : `Editing ${editing.key}. Changes update the field list and Android preview live.`
              : "Pick a field type or select a row to edit its details."}
          </div>
          <div className="builder-form">
            <Group label="Field key"><input disabled={!editing} value={editing?.key ?? ""} onChange={(e) => patch({ key: e.target.value })} /></Group>
            <Group label="Operator title" wide><input disabled={!editing} value={editing?.label ?? ""} placeholder="e.g. Destination count" onChange={(e) => patch({ label: e.target.value })} /></Group>
            <Group label="Type">
              <select disabled={!editing} value={editing?.type ?? "text"} onChange={(e) => patch({ type: e.target.value as FieldType })}>
                {FIELD_TYPES.map((t) => <option key={t} value={t}>{paletteLabel(t)}</option>)}
              </select>
            </Group>
            <Group label="Required">
              <label className="inline-check"><input type="checkbox" disabled={!editing} checked={!!editing?.required} onChange={(e) => patch({ required: e.target.checked })} /> required</label>
            </Group>
            <Group label="Help text" wide><input disabled={!editing} value={editing?.description ?? ""} placeholder="shown below field on Android" onChange={(e) => patch({ description: e.target.value })} /></Group>
            <Group label="Placeholder"><input disabled={!editing} value={editing?.placeholder ?? ""} placeholder="tap to enter…" onChange={(e) => patch({ placeholder: e.target.value })} /></Group>
            <Group label="Default value"><input disabled={!editing} value={editing?.defaultValue ?? ""} placeholder="optional" onChange={(e) => patch({ defaultValue: e.target.value })} /></Group>
            <Group label="Options" wide><input disabled={!editing} value={editing?.options ?? ""} placeholder="comma separated for select/multiselect" onChange={(e) => patch({ options: e.target.value })} /></Group>
            <Group label="Option source"><input disabled={!editing} value={editing?.optionSource ?? ""} placeholder="e.g. locations.active" onChange={(e) => patch({ optionSource: e.target.value })} /></Group>
            <Group label="Badge note"><input disabled={!editing} value={editing?.note ?? ""} placeholder="e.g. repeat for each" onChange={(e) => patch({ note: e.target.value })} /></Group>
            {isDraft ? (
              <>
                <button className="btn small primary" type="button" onClick={() => dispatch({ kind: "commitDraft" })}>Add field to form</button>
                <button className="btn small" type="button" onClick={() => dispatch({ kind: "discardDraft" })}>Discard draft</button>
              </>
            ) : (
              <button className="btn small" type="button" disabled={!editing} onClick={() => dispatch({ kind: "selectField", index: -1 })}>Clear selection</button>
            )}
          </div>
        </div>

        <div className="canvas-title">
          <h4>Fields in this operator form</h4>
          <span className="hint">Drag rows to reorder. Use Edit to change the selected field.</span>
        </div>
        <div
          className={`canvas${dropActive ? " drop" : ""}`}
          onDragOver={(e) => { e.preventDefault(); setDropActive(true); }}
          onDragLeave={() => setDropActive(false)}
          onDrop={(e) => {
            setDropActive(false);
            const t = e.dataTransfer.getData("fieldtype");
            if (t) dispatch({ kind: "startDraft", fieldType: t as FieldType });
          }}
        >
          {state.fields.map((field, i) => (
            <FieldRow key={`${field.key}-${i}`} field={field} index={i} selected={i === state.selectedFieldIndex} dispatch={dispatch} />
          ))}
          {state.fields.length === 0 ? <div className="octl">No fields yet. Click a field type above to add one.</div> : null}
        </div>
      </div>

      <div className="panel">
        <h3>Dynamic Option Sources</h3>
        <div className="desc">Reference-only. These dropdowns/pickers come from backend APIs/offline caches, not hardcoded menus.</div>
        <div className="source-grid">
          {OPTION_SOURCES.map((s) => (
            <div key={s.name} className="source-card">
              <div className="name">{s.name}</div>
              <p className="desc" style={{ marginBottom: 0 }}>{s.desc}</p>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function FieldRow({ field, index, selected, dispatch }: { field: BuilderField; index: number; selected: boolean; dispatch: Dispatch<Action> }) {
  return (
    <div
      className={`frow${selected ? " selected" : ""}`}
      draggable
      onDragStart={(e) => e.dataTransfer.setData("reorder", String(index))}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => {
        e.preventDefault();
        e.stopPropagation();
        const from = e.dataTransfer.getData("reorder");
        if (from === "") return;
        dispatch({ kind: "reorderField", from: Number(from), to: index });
      }}
      onClick={() => dispatch({ kind: "selectField", index })}
    >
      <span className="grip">⠿</span>
      <div>
        <div className="fname">{field.label || titleCase(field.key)}</div>
        <div className="ftype">{field.type} · {field.key}</div>
        {field.description ? <div className="fdesc">{field.description}</div> : null}
      </div>
      <div className="badges">
        <span className={`pill ${field.required ? "teal" : ""}`}>{field.required ? "required" : "optional"}</span>
        {field.cond ? <span className="pill amber">if {field.cond}</span> : null}
        {field.note ? <span className="pill">{field.note}</span> : null}
      </div>
      <div className="row-actions">
        <button className="mini-btn" type="button" onClick={(e) => { e.stopPropagation(); dispatch({ kind: "selectField", index }); }}>Edit</button>
        <button className="mini-btn" type="button" onClick={(e) => { e.stopPropagation(); dispatch({ kind: "toggleRequired", index }); }}>{field.required ? "Make optional" : "Make required"}</button>
        <button className="mini-btn danger" type="button" onClick={(e) => { e.stopPropagation(); dispatch({ kind: "removeField", index }); }}>Remove</button>
      </div>
    </div>
  );
}

function Def({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="defitem">
      <div className="k">{label}</div>
      <div className="v">{children}</div>
    </div>
  );
}

function Group({ label, wide, children }: { label: string; wide?: boolean; children: React.ReactNode }) {
  return (
    <div className={`inputgroup${wide ? " wide" : ""}`}>
      <label>{label}</label>
      {children}
    </div>
  );
}

function paletteLabel(type: FieldType): string {
  return PALETTE.find((p) => p.type === type)?.label ?? type;
}
