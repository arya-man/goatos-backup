"use client";

import { useState } from "react";
import { AlertTriangle, ChevronLeft, ChevronRight, Pencil, Plus, Search } from "lucide-react";
import { RuleEditorModal } from "./rule-editor-modal";
import type { AnimalStageOption, SopVersionOption } from "./rule-dsl";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";

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
  loadError = null,
  stagesError = null,
  sopsError = null,
  canPublish = true,
  pageContract,
}: {
  rules: ConfigRuleRow[];
  initialCategory: string;
  sopVersions?: SopVersionOption[];
  animalStages?: AnimalStageOption[];
  loadError?: string | null;
  stagesError?: string | null;
  sopsError?: string | null;
  canPublish?: boolean;
  pageContract: AdminUiPageContract;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [requestedPage, setRequestedPage] = useState(1);
  const pageSizeOptions = tablePageSizes(pageContract, "protocol-rules");
  const defaultPageSize = pageSizeOptions.includes(10) ? 10 : (pageSizeOptions[0] ?? 10);
  const [pageSize, setPageSize] = useState<number>(defaultPageSize);
  const labels = tableLabels(pageContract, "protocol-rules");
  const filteredRules = rules.filter((rule) => {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    return [
      rule.categoryLabel,
      rule.version,
      rule.scope,
      rule.statusText,
      rule.effective,
      rule.linkedSop,
      rule.lastPublisher,
    ]
      .join(" ")
      .toLowerCase()
      .includes(q);
  });
  const totalPages = Math.max(1, Math.ceil(filteredRules.length / pageSize));
  const page = Math.min(requestedPage, totalPages);
  const start = filteredRules.length === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = filteredRules.length === 0 ? 0 : Math.min(filteredRules.length, page * pageSize);
  const pagedRules = filteredRules.slice((page - 1) * pageSize, page * pageSize);

  return (
    <>
	      <div className="phead">
	        <div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">
	            <b>{copy(pageContract, "page.lede")}</b> {copy(pageContract, "page.lede_detail")}
	          </div>
	        </div>
	        <div className="sp" />
	        <button type="button" className="btn p" onClick={() => setOpen(true)}>
	          <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.new_draft_rule")}
	        </button>
      </div>

      <div className="alert warn" style={{ marginBottom: 16 }}>
        <AlertTriangle className="ic" aria-hidden="true" />
        <div>
	          {copy(pageContract, "security.warning")}
        </div>
      </div>

      {loadError ? (
        <div className="alert" role="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
	          <div>
	            {copy(pageContract, "error.rules_load")} ({loadError})
	          </div>
        </div>
      ) : null}

      {stagesError ? (
        <div className="alert warn" role="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
	          <div>
	            {copy(pageContract, "error.stages_load")} ({stagesError})
	          </div>
        </div>
      ) : null}

      {sopsError ? (
        <div className="alert warn" role="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
	          <div>
	            {copy(pageContract, "error.sops_load")} ({sopsError})
	          </div>
        </div>
      ) : null}

      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Pencil className="ic" />
	          <h3>{copy(pageContract, "section.rules.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="tag t-mut">{rules.length}</span>
        </div>
        <div className="tbar">
          <div className="tsearch">
            <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
            <input
	              aria-label={copy(pageContract, "filter.search_label")}
	              placeholder={copy(pageContract, "filter.search_placeholder")}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value);
                setRequestedPage(1);
              }}
            />
          </div>
          <span className="muted small">
            {start}-{end} of {filteredRules.length} rows
          </span>
	          <span className="muted small">{copy(pageContract, "section.rules.note")}</span>
        </div>
        {/* Keyboard-accessible scroll region (WCAG scrollable-region-focusable): .card .bd overflow-x makes
            this scrollable on narrow widths, so it must be tab-focusable like the other data tables. */}
	        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.rules.aria")}>
          <table>
	            <thead>
	              <tr>
	                {labels.map((label) => (
	                  <th key={label}>{label}</th>
	                ))}
	              </tr>
	            </thead>
            <tbody>
              {filteredRules.length === 0 ? (
                <tr>
	                  <td colSpan={labels.length} className="muted small" style={{ padding: "18px 12px", textAlign: "center" }}>
	                    {rules.length === 0
	                      ? copy(pageContract, "empty.rules")
	                      : copy(pageContract, "empty.rules_search")}
	                  </td>
                </tr>
              ) : (
                pagedRules.map((r) => (
                  <tr key={r.id} style={r.statusTone !== "ok" ? { boxShadow: "inset 2px 0 0 var(--amber)" } : undefined}>
                    <td>
                      <b>{r.categoryLabel}</b>{" "}
                      <span className="muted small">
                        {r.ruleRows} {r.ruleRowLabel}
                      </span>
                    </td>
                    <td className="mono">{r.version}</td>
                    <td>{r.scope}</td>
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
        {filteredRules.length > 0 ? (
          <div className="pager2">
            <span className="muted small">
	              {start}-{end} of {filteredRules.length} {copy(pageContract, "pager.rules_noun")} · {copy(pageContract, "pager.page")} {page} of {totalPages}
	            </span>
	            <span className="sp" style={{ flex: 1 }} />
	            <span className="muted small">{copy(pageContract, "pager.rows")}</span>
	            <span className="chipset" style={{ gap: 4 }}>
	              {pageSizeOptions.map((size) => (
                <button
                  key={size}
                  type="button"
                  className={`chip pgsize${pageSize === size ? " on" : ""}`}
                  style={{ padding: "5px 8px", fontSize: 11 }}
                  onClick={() => {
                    setPageSize(size);
                    setRequestedPage(1);
                  }}
                >
                  {size}
                </button>
              ))}
            </span>
            <button
              type="button"
              className="btn sm"
              disabled={page <= 1}
              aria-disabled={page <= 1 ? "true" : undefined}
              style={page <= 1 ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
              onClick={() => setRequestedPage((p) => Math.max(1, p - 1))}
            >
	              <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
            </button>
            <button
              type="button"
              className="btn sm"
              disabled={page >= totalPages}
              aria-disabled={page >= totalPages ? "true" : undefined}
              style={page >= totalPages ? { opacity: 0.45, cursor: "not-allowed" } : undefined}
              onClick={() => setRequestedPage((p) => Math.min(totalPages, p + 1))}
            >
              {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
            </button>
          </div>
        ) : null}
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
        pageContract={pageContract}
      />
    </>
  );
}
