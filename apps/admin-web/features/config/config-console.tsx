"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { AlertTriangle, Calculator, ChevronLeft, ChevronRight, Database, Pencil, Plus, Search, X } from "lucide-react";
import type { AppApiComponents } from "@goatos/api-client";
import { RuleEditorModal } from "./rule-editor-modal";
import type { AnimalStageOption, SopVersionOption } from "./rule-dsl";
import { copy, optionGroup, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";

type ProtocolVersionDetail = AppApiComponents["schemas"]["ProtocolVersionResponse"];

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

function configHref(params: RouteSearchParams | undefined, category: string, selectedRuleId?: string, newRule = false): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params ?? {})) {
    if (key === "config_rule" || key === "category" || key === "new_rule") continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  next.set("category", category);
  if (selectedRuleId) next.set("config_rule", selectedRuleId);
  if (newRule) next.set("new_rule", "1");
  const qs = next.toString();
  return qs ? `/config?${qs}` : "/config";
}

function jsonText(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function ProtocolRuleDrawer({
  row,
  detail,
  detailError,
  closeHref,
  pageContract,
}: {
  row?: ConfigRuleRow;
  detail: ProtocolVersionDetail | null;
  detailError?: string | null;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  if (!row && !detailError) return null;
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.record.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Pencil className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div>
            <h2>{row?.categoryLabel ?? copy(pageContract, "drawer.record.detail_unavailable")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          {detailError ? (
            <div className="alert warn" role="alert" style={{ marginBottom: 14 }}>
              <AlertTriangle className="ic" aria-hidden="true" />
              <div>{copy(pageContract, "drawer.record.detail_unavailable")} ({detailError})</div>
            </div>
          ) : null}
          <div className="note">{copy(pageContract, "drawer.record.note")}</div>
          {row ? (
            <div className="metagrid" style={{ marginTop: 14 }}>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.version")}</div>
                <div className="v mono">{row.version}</div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.status")}</div>
                <div className="v"><span className={`tag ${TONE_CLASS[row.statusTone]}`}>{row.statusText}</span></div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.scope")}</div>
                <div className="v">{row.scope}</div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.effective")}</div>
                <div className="v">{row.effective}</div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.linked_sop")}</div>
                <div className="v mono">{row.linkedSop}</div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.publisher")}</div>
                <div className="v mono">{row.lastPublisher}</div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.row_version")}</div>
                <div className="v mono">{detail?.row_version ?? "-"}</div>
              </div>
              <div>
                <div className="k">{copy(pageContract, "drawer.record.version_id")}</div>
                <div className="v mono">{row.id.slice(0, 8)}...</div>
              </div>
            </div>
          ) : null}
          {detail ? (
            <>
              <div className="k" style={{ marginTop: 16 }}>{copy(pageContract, "drawer.record.rule_dsl")}</div>
              <pre className="cfgjson" style={{ marginTop: 8, maxHeight: 300, overflow: "auto" }}>
                {jsonText(detail.rule_dsl)}
              </pre>
              <div className="k" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.record.proof_policy")}</div>
              <pre className="cfgjson" style={{ marginTop: 8, maxHeight: 160, overflow: "auto" }}>
                {jsonText(detail.proof_policy)}
              </pre>
            </>
          ) : null}
        </div>
        <div className="df">
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.cancel")}
          </Link>
        </div>
      </aside>
    </>
  );
}

function FeedDirectionTemplatePanel({ pageContract }: { pageContract: AdminUiPageContract }) {
  const sourceTables = optionGroup(pageContract, "feed_source_tables");
  const parameterFamilies = optionGroup(pageContract, "feed_parameter_families");
  const dimensions = optionGroup(pageContract, "feed_dimension_keys");
  const validations = optionGroup(pageContract, "feed_validation_checks");
  const outputs = optionGroup(pageContract, "feed_calculation_outputs");
  const labels = tableLabels(pageContract, "feed-config-evidence");
  const rowCount = Math.max(sourceTables.length, parameterFamilies.length, validations.length, outputs.length);
  const evidenceReady = sourceTables.length > 0 && parameterFamilies.length > 0 && validations.length > 0 && outputs.length > 0;

  return (
    <section className="card" style={{ marginBottom: 14 }}>
      <div className="hd" style={{ alignItems: "flex-start", flexWrap: "wrap", gap: 8 }}>
        <Database className="ic" />
        <h3>{copy(pageContract, "feed_config.section.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="tag t-info" style={{ maxWidth: "100%", whiteSpace: "normal", lineHeight: 1.3 }}>
          {copy(pageContract, "feed_config.section.note")}
        </span>
      </div>
      <div className="bd">
        <div className="alert warn" style={{ marginBottom: 12 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>{copy(pageContract, "feed_config.alert")}</div>
        </div>
        <div className="grid g4" style={{ marginBottom: 12 }}>
          {[
            [copy(pageContract, "feed_config.kpi.sources"), sourceTables.length],
            [copy(pageContract, "feed_config.kpi.families"), parameterFamilies.length],
            [copy(pageContract, "feed_config.kpi.dimensions"), dimensions.length],
            [copy(pageContract, "feed_config.kpi.validations"), validations.length],
          ].map(([label, value]) => (
            <div key={String(label)} className="kpi">
              <div className="lab">{label}</div>
              <div className="val">{String(value)}</div>
            </div>
          ))}
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "feed_config.section.title")}>
          <table>
            <thead>
              <tr>
                {labels.map((label) => (
                  <th key={label}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {evidenceReady
                ? Array.from({ length: rowCount }).map((_, i) => {
                    const source = sourceTables[i % sourceTables.length];
                    const family = parameterFamilies[i % parameterFamilies.length];
                    const validation = validations[i % validations.length];
                    const output = outputs[i % outputs.length];
                    return (
                      <tr key={String(source.key) + "-" + String(family.key) + "-" + String(validation.key) + "-" + String(output.key)}>
                        <td>
                          <b>{source.label}</b>
                          <div className="muted small">{source.title}</div>
                        </td>
                        <td>{family.label}</td>
                        <td>
                          <span className={"tag " + (validation.tone === "warn" ? "t-warn" : "t-mut")}>{validation.label}</span>
                        </td>
                        <td>{output.label}</td>
                      </tr>
                    );
                  })
                : null}
            </tbody>
          </table>
        </div>
        <div className="pager2">
          <span className="muted small">{copy(pageContract, "feed_config.action.preview_disabled")}</span>
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn sm" disabled title={copy(pageContract, "feed_config.action.preview_disabled")} style={{ opacity: 0.45, cursor: "not-allowed" }}>
            <Calculator className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "feed_config.action.preview")}
          </button>
        </div>
      </div>
    </section>
  );
}

// Client console for the generic Config surface: the protocol-rules table (all categories) plus the
// New-draft-rule editor modal. Rules are passed in from the server (backend list); until the list
// endpoint lands the table shows an honest empty state — rows are never fabricated client-side.
export function ConfigConsole({
  rules,
  initialCategory,
  searchParams,
  selectedRuleId,
  selectedRuleDetail,
  selectedRuleError,
  sopVersions = [],
  animalStages = [],
  loadError = null,
  stagesError = null,
  sopsError = null,
  canPublish,
  publishDisabledReason,
  pageContract,
}: {
  rules: ConfigRuleRow[];
  initialCategory: string;
  searchParams?: RouteSearchParams;
  selectedRuleId?: string;
  selectedRuleDetail?: ProtocolVersionDetail | null;
  selectedRuleError?: string | null;
  sopVersions?: SopVersionOption[];
  animalStages?: AnimalStageOption[];
  loadError?: string | null;
  stagesError?: string | null;
  sopsError?: string | null;
  canPublish: boolean;
  publishDisabledReason: string;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [requestedPage, setRequestedPage] = useState(1);
  const pageSizeOptions = tablePageSizes(pageContract, "protocol-rules");
  const defaultPageSize = pageSizeOptions.includes(10) ? 10 : (pageSizeOptions[0] ?? 10);
  const [pageSize, setPageSize] = useState<number>(defaultPageSize);
  const labels = tableLabels(pageContract, "protocol-rules");
  const categoryOptions = optionGroup(pageContract, "rule_categories");
  const authoringDisabled = categoryOptions.length === 0;
  const authoringDisabledReason = authoringDisabled ? copy(pageContract, "modal.rule_editor.categories_empty") : undefined;
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
  const selectedRule = selectedRuleId ? rules.find((rule) => rule.id === selectedRuleId) : undefined;
  const closeRecordHref = configHref(searchParams, initialCategory);
  const newRule = searchParams ? one(searchParams, "new_rule") === "1" : false;
  const newRuleHref = configHref(searchParams, initialCategory, undefined, true);

  if (newRule) {
    return (
      <>
        <nav className="navback" aria-label={copy(pageContract, "breadcrumb.config_rule_editor")}>
          <Link href={closeRecordHref} replace className="nbback" scroll={false}>
            <ChevronLeft className="ic" aria-hidden="true" /> {pageContract.title}
          </Link>
          <div className="nbtrail">
            <span className="nbc">{copy(pageContract, "crumb")}</span>
            <ChevronRight className="nbsep" aria-hidden="true" />
            <Link href={closeRecordHref} replace className="nbc" scroll={false}>
              {pageContract.title}
            </Link>
            <ChevronRight className="nbsep" aria-hidden="true" />
            <span className="nbc cur">{copy(pageContract, "action.new_draft_rule")}</span>
          </div>
        </nav>
        <RuleEditorModal
          key={initialCategory}
          open
          presentation="page"
          onClose={() => router.push(closeRecordHref, { scroll: false })}
          initialCategory={initialCategory}
          sopVersions={sopVersions}
          animalStages={animalStages}
          stagesError={stagesError}
          sopsError={sopsError}
          canPublish={canPublish}
          publishDisabledReason={publishDisabledReason}
          pageContract={pageContract}
        />
      </>
    );
  }

  return (
    <>
      <div className="phead">
	        <div>
	          <div className="crumb">
	            <b>{copy(pageContract, "crumb")}</b> / {pageContract.title}
	          </div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">
	            <b>{copy(pageContract, "page.lede")}</b> {copy(pageContract, "page.lede_detail")}
	          </div>
	        </div>
	        <div className="sp" />
	        {authoringDisabled ? (
	          <button
	            type="button"
	            className="btn p"
	            disabled
	            title={authoringDisabledReason}
	            style={{ opacity: 0.45, cursor: "not-allowed" }}
	          >
	            <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.new_draft_rule")}
	          </button>
	        ) : (
	          <Link href={newRuleHref} className="btn p" scroll={false}>
	            <Plus className="ic" aria-hidden="true" /> {copy(pageContract, "action.new_draft_rule")}
	          </Link>
	        )}
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

      {authoringDisabledReason ? (
        <div className="alert warn" role="alert" style={{ marginBottom: 14 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>{authoringDisabledReason}</div>
        </div>
      ) : null}

      {initialCategory === "feed_direction" ? <FeedDirectionTemplatePanel pageContract={pageContract} /> : null}

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
                pagedRules.map((r) => {
                  const recordHref = configHref(searchParams, initialCategory, r.id);
                  const selected = selectedRuleId === r.id;
                  return (
                  <tr
                    key={r.id}
                    role="link"
                    tabIndex={0}
                    aria-label={`${copy(pageContract, "action.open_protocol_record")} ${r.categoryLabel}`}
                    onClick={() => router.push(recordHref, { scroll: false })}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        router.push(recordHref, { scroll: false });
                      }
                    }}
                    style={{
                      cursor: "pointer",
                      ...(r.statusTone !== "ok" ? { boxShadow: "inset 2px 0 0 var(--amber)" } : {}),
                      ...(selected ? { background: "color-mix(in srgb,var(--brand-soft) 55%,transparent)" } : {}),
                    }}
                  >
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
                      <Link href={recordHref} className="lk small" scroll={false} onClick={(event) => event.stopPropagation()}>
                        {copy(pageContract, "drawer.record.eyebrow")}
                      </Link>
                    </td>
                  </tr>
                  );
                })
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

      {selectedRuleId ? (
        <ProtocolRuleDrawer
          row={selectedRule}
          detail={selectedRuleDetail ?? null}
          detailError={selectedRuleError}
          closeHref={closeRecordHref}
          pageContract={pageContract}
        />
      ) : null}
    </>
  );
}
