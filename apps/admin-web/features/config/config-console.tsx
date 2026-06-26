"use client";

import { useState } from "react";
import { Pencil, Plus } from "lucide-react";
import { RuleEditorModal } from "./rule-editor-modal";
import type { AnimalStageOption, SopVersionOption } from "./rule-dsl";

export interface ConfigRuleRow {
  id: string;
  categoryLabel: string;
  ruleRows: number;
  ruleRowLabel: string;
  version: string;
  scope: string;
  statusText: string;
  statusTone: "warn" | "info" | "ok" | "mut";
  effective: string;
  linkedSop: string;
  lastPublisher: string;
}

const TONE_CLASS = { warn: "t-warn", info: "t-info", ok: "t-ok", mut: "t-mut" } as const;

// Client console for the generic Config surface: the protocol-rules table (all categories) plus the
// New-draft-rule editor modal. Rules are passed in from the server (backend list); until the list
// endpoint lands the table shows an honest empty state — rows are never fabricated client-side.
export function ConfigConsole({
  rules,
  initialCategory,
  sopVersions = [],
  animalStages = [],
  stagesError = null,
  sopsError = null,
  canPublish = true,
}: {
  rules: ConfigRuleRow[];
  initialCategory: string;
  sopVersions?: SopVersionOption[];
  animalStages?: AnimalStageOption[];
  stagesError?: string | null;
  sopsError?: string | null;
  canPublish?: boolean;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Pencil className="ic" />
          <h3>Protocol rules</h3>
          <span className="muted small">all categories · all versions</span>
          <div className="sp" style={{ flex: 1 }} />
          <span className="tag t-mut">{rules.length}</span>
          <button type="button" className="btn p sm" onClick={() => setOpen(true)}>
            <Plus className="ic" /> New draft rule
          </button>
        </div>
        {/* Keyboard-accessible scroll region (WCAG scrollable-region-focusable): .card .bd overflow-x makes
            this scrollable on narrow widths, so it must be tab-focusable like the other data tables. */}
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Protocol rules">
          <table>
            <thead>
              <tr>
                <th>Category</th>
                <th>Version</th>
                <th>Scope</th>
                <th>Status</th>
                <th>Effective</th>
                <th>Linked SOP</th>
                <th>Last publisher</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {rules.length === 0 ? (
                <tr>
                  <td colSpan={8} className="muted small" style={{ padding: "18px 12px", textAlign: "center" }}>
                    No protocol rules yet — the engine stands up empty. Author one with <b>New draft rule</b>; only
                    source-backed, approved versions can publish and generate obligations.
                  </td>
                </tr>
              ) : (
                rules.map((r) => (
                  <tr key={r.id} style={r.statusTone !== "ok" ? { boxShadow: "inset 2px 0 0 var(--amber)" } : undefined}>
                    <td>
                      <b>{r.categoryLabel}</b>{" "}
                      <span className="muted small">
                        {r.ruleRows} {r.ruleRowLabel}
                      </span>
                    </td>
                    <td className="mono">{r.version}</td>
                    <td>{r.scope === "tenant" ? "tenant" : <span className="tag t-info">{r.scope.replace(":", ": ")}</span>}</td>
                    <td>
                      <span className={`tag ${TONE_CLASS[r.statusTone]}`}>{r.statusText}</span>
                    </td>
                    <td className="muted">{r.effective}</td>
                    <td className="mono">{r.linkedSop}</td>
                    <td className="muted">{r.lastPublisher}</td>
                    <td style={{ whiteSpace: "nowrap" }}>
                      <span className="muted small">—</span>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      <RuleEditorModal
        key={initialCategory}
        open={open}
        onClose={() => setOpen(false)}
        initialCategory={initialCategory}
        sopVersions={sopVersions}
        animalStages={animalStages}
        stagesError={stagesError}
        sopsError={sopsError}
        canPublish={canPublish}
      />
    </>
  );
}
