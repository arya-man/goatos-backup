"use client";

import { useState } from "react";
import type { BuilderState } from "../model";
import { CAPABILITIES, COVERAGE_MAP, DOMAINS, MENTAL_MODEL, TRIGGERS } from "../templates";
import { CATALOG_ORDER, SOP_TEMPLATES } from "../sop-templates";

export interface CustomDraftInput {
  name: string;
  code: string;
  domain: string;
  trigger: string;
}

export function CatalogTab({
  state,
  onLoadTemplate,
  onCreateCustom,
  creating,
}: {
  state: BuilderState;
  onLoadTemplate: (key: string) => void;
  onCreateCustom: (input: CustomDraftInput) => void;
  creating: boolean;
}) {
  const [name, setName] = useState("Custom inspection SOP");
  const [code, setCode] = useState("ops.custom_inspection");
  const [domain, setDomain] = useState(DOMAINS[0]);
  const [trigger, setTrigger] = useState(TRIGGERS[0]);

  return (
    <div>
      <div className="flow-note">
        <h3>Start here: choose the SOP you want to build.</h3>
        <p>The builder has two jobs: decide what the operator fills, then decide what happens after submit.</p>
        <div className="mental-model">
          {MENTAL_MODEL.map((m, i) => (
            <div key={m.title} className={`guide-card${i === 0 ? " active" : ""}`}>
              <div className="label">{m.label}</div>
              <div className="title">{m.title}</div>
              <div className="copy">{m.copy}</div>
            </div>
          ))}
        </div>
      </div>

      <div className="panel">
        <h3>Create Custom SOP Draft</h3>
        <div className="desc">Blank-slate path. Creates the versioned SOP shell, then moves you to the form builder.</div>
        <div className="builder-form">
          <div className="inputgroup wide">
            <label htmlFor="customSopName">SOP name</label>
            <input id="customSopName" type="text" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="inputgroup">
            <label htmlFor="customSopCode">Code</label>
            <input id="customSopCode" type="text" value={code} onChange={(e) => setCode(e.target.value)} />
          </div>
          <div className="inputgroup">
            <label htmlFor="customSopDomain">Domain</label>
            <select id="customSopDomain" value={domain} onChange={(e) => setDomain(e.target.value)}>
              {DOMAINS.map((d) => <option key={d}>{d}</option>)}
            </select>
          </div>
          <div className="inputgroup">
            <label htmlFor="customSopTrigger">Trigger</label>
            <select id="customSopTrigger" value={trigger} onChange={(e) => setTrigger(e.target.value)}>
              {TRIGGERS.map((t) => <option key={t}>{t}</option>)}
            </select>
          </div>
          <button className="btn primary" type="button" disabled={creating} onClick={() => onCreateCustom({ name, code, domain, trigger })}>
            {creating ? "Creating…" : "Create draft and build fields"}
          </button>
        </div>
        <div className="builder-help">Creating a draft does not publish anything. Publish becomes available only after validation.</div>
      </div>

      <div className="panel">
        <h3>Existing SOP Catalog — migration view</h3>
        <div className="desc">Click a card to load its operator form, form logic, task flow, and Android preview. Migration candidates from known patterns plus a custom path.</div>
        <div className="catalog-grid">
          {CATALOG_ORDER.map((key) => {
            const t = SOP_TEMPLATES[key];
            const active = state.activeTemplate === key;
            return (
              <button key={key} type="button" className={`sop-card${active ? " active" : ""}`} onClick={() => onLoadTemplate(key)}>
                <div className="sop-head">
                  <div>
                    <div className="sop-title">{t.meta.title}</div>
                    <div className="sop-meta">{t.meta.domain.toLowerCase()} · {t.meta.trigger.toLowerCase()}</div>
                  </div>
                  <span className={`pill ${active ? "teal" : ""}`}>{active ? "editing" : t.badge}</span>
                </div>
                <p>{t.summary}</p>
                <div className="tagrow">{t.tags.map((tag) => <span key={tag} className="pill">{tag}</span>)}</div>
              </button>
            );
          })}
        </div>
        <div className="builder-help">Selected template loads immediately. You can still add custom fields, rules, and workflow nodes after loading any catalog SOP.</div>
      </div>

      <div className="panel">
        <h3>Known SOP coverage map</h3>
        <div className="desc">This covers the SOP families explicitly named so far. Final coverage still depends on the source inventory check.</div>
        <div className="mini-grid">
          {COVERAGE_MAP.map((c, i) => (
            <div key={i} className="mini-card">
              <h4>{c.title}</h4>
              <ul>{c.items.map((it) => <li key={it}>{it}</li>)}</ul>
            </div>
          ))}
        </div>
      </div>

      <div className="panel">
        <h3>Capabilities required by existing SOPs</h3>
        <div className="desc">Builder checklist. If an old SOP needs one of these, the screen should make it visible and easy to configure.</div>
        <div className="mini-grid">
          {CAPABILITIES.map((c) => (
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
