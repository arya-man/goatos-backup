import Link from "next/link";
import { redirect } from "next/navigation";
import {
  AlertTriangle,
  ClipboardList,
  Clock,
  Database,
  Eye,
  Filter,
  ScrollText,
  Search,
  ShieldCheck,
  Syringe,
  Truck,
  Upload,
  UserRound,
  X,
  Zap,
} from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import {
  firstAuthRequiredError,
  getOperationsAuditSummary,
  listOperationsAudit,
  type OperationsAuditActorType,
  type OperationsAuditListParams,
  type OperationsAuditRow,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { copy, optionLabel, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash, fmtDateTime, joinParts, shortId } from "@/lib/format";
import { ROLE_LENSES, roleLensById } from "@/lib/role-lens";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";

const PATHNAME = "/operations/audit";
const PAGE_SIZE = 25;
const ACTOR_TYPES = ["human", "system", "worker", "service", "user"] as const;

// Top-bar scope + role-preview state to keep when a user clears the page filters.
const PRESERVE_ON_CLEAR = ["viewing_as", "scope_mode", "park", "as_of", "range", "from", "to"];

// Visible audit families stay locked to the current vaccination slice. Source Entry, Herd Register, and
// Admin/SOP are shown only because they feed the vaccination evidence/config chain.
const OPERATION_FAMILIES: Array<{ key: string; domain: string | null; icon: typeof Zap }> = [
  { key: "vaccination", domain: "vaccination", icon: Syringe },
  { key: "procurement", domain: "procurement", icon: Truck },
  { key: "counts", domain: "counts", icon: ClipboardList },
  { key: "admin", domain: "admin", icon: Database },
];

// Result/status tabs map to real list filters.
const STATUS_TABS: Array<{ key: string; status?: string; result?: string; proofGaps?: boolean }> = [
  { key: "all_results" },
  { key: "awaiting", status: "verification_pending" },
  { key: "rejected", result: "rejected" },
  { key: "proof_gaps", proofGaps: true },
];

export async function OperationsAuditPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const filters = parseFilters(sp);
  const actorQ = one(sp, "actor_q")?.trim().toLowerCase() ?? "";
  // `viewing_as` is a CEO/admin role-PREVIEW lens, synced to the shared role-lens model used by the top bar.
  // It is label-only and never becomes a backend filter — backend RBAC governs the real audit span.
  const lens = roleLensById(one(sp, "viewing_as"));

  const [listResult, summaryResult] = await Promise.all([
    listOperationsAudit({ ...filters, limit: PAGE_SIZE, cursor: one(sp, "cursor") }),
    getOperationsAuditSummary(filters),
  ]);
  const authError = firstAuthRequiredError(listResult, summaryResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const rows = listResult.ok ? listResult.data.items : [];
  const summary = summaryResult.ok ? summaryResult.data : null;
  const actors = spanOfControl(rows, actorQ);
  const operationCounts = countByOperation(rows, summary?.actions ?? rows.length, filters.domain ?? "vaccination");
  const nextHref = listResult.ok ? hrefWithCursor(PATHNAME, sp, listResult.data.next_cursor ?? null) : null;
  const prevHref = hrefPreviousCursor(PATHNAME, sp);
  const clearedHref = clearHref(sp);
  const activeStatusTab = STATUS_TABS.find((tab) => statusTabActive(tab, filters)) ?? STATUS_TABS[0];
  const selectedRow = rows.find((row) => row.audit_id === one(sp, "audit_id"));
  const cols = tableLabels(pageContract, "activity-trail");

  return (
    <div className="screen on">
      <div className="phead">
        <div>
	          <div className="crumb">
	            {copy(pageContract, "crumb")} / <b>{pageContract.title}</b>
	          </div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
	        </div>
	        <div className="sp" style={{ flex: 1 }} />
	        <button type="button" className="btn" disabled aria-disabled="true" title={copy(pageContract, "reason.export_pending")}>
	          <Upload className="ic" aria-hidden="true" />
	          {copy(pageContract, "action.export")}
	        </button>
      </div>

      {listResult.ok && summaryResult.ok ? null : (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{!listResult.ok ? listResult.error.code ?? listResult.error.kind : summaryResult.ok ? copy(pageContract, "error.audit_unavailable") : summaryResult.error.code ?? summaryResult.error.kind}</b>
          &nbsp;{!listResult.ok ? listResult.error.message : summaryResult.ok ? copy(pageContract, "error.summary_unavailable") : summaryResult.error.message}
        </div>
      )}

      <div className="grid g4" style={{ marginBottom: 14 }}>
	        <KPI label={copy(pageContract, "label.actions_in_view")} value={summary ? String(summary.actions) : "—"} hint={copy(pageContract, "label.tap_clear_filters")} tone="info" icon={Zap} href={clearedHref} />
	        <KPI label={copy(pageContract, "label.awaiting_verification")} value={summary ? String(summary.awaiting_verification) : "—"} hint={copy(pageContract, "label.proof_signoff")} tone="warn" icon={Clock} href={hrefWithUpdates(sp, { status: "verification_pending", result: null, proof_gaps: null, cursor: null, page: null })} />
	        <KPI label={copy(pageContract, "label.proof_coverage")} value={summary ? `${summary.proof_coverage_percent}%` : "—"} hint={copy(pageContract, "label.tap_proof_gaps")} tone="teal" icon={ShieldCheck} href={hrefWithUpdates(sp, { proof_gaps: filters.proofGaps ? null : "true", cursor: null, page: null })} />
	        <KPI label={copy(pageContract, "label.flagged_anomalies")} value={summary ? String(summary.anomalies) : "—"} hint={copy(pageContract, "label.anomaly_sources")} tone="dng" icon={AlertTriangle} href={hrefWithUpdates(sp, { anomalies_only: filters.anomaliesOnly ? null : "true", proof_gaps: null, cursor: null, page: null })} />
      </div>

      {/* Viewing as — CEO/admin role-preview lens, mirrors the shared top-bar role lens. Preview only. */}
      <div className="subtabs" style={{ marginBottom: 8 }}>
        <span className="muted small" style={{ padding: "7px 8px 7px 4px", display: "inline-flex", alignItems: "center", gap: 6 }}>
	          <Eye className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "label.viewing_as")}
        </span>
        {ROLE_LENSES.map((role) => (
          <Link
            key={role.id}
            href={hrefWithUpdates(sp, { viewing_as: role.superadmin ? null : role.id, cursor: null, page: null })}
            replace
            scroll={false}
            className={`${lens.id === role.id ? "on" : ""}`}
            title={`${role.name} · ${role.scope}`}
          >
            {role.auditShort}
          </Link>
        ))}
      </div>
      <div className="note" style={{ marginBottom: 12 }}>
	        {copy(pageContract, "label.previewing_as")} <b>{lens.name}</b> · {lens.scope}. {copy(pageContract, "label.role_preview_note")}
      </div>

      {/* Operation families — real backend `domain` filter for vaccination-supporting surfaces only. */}
      <div className="opf" style={{ marginBottom: 12 }}>
        {OPERATION_FAMILIES.map((family) => {
          const Icon = family.icon;
          const active = (filters.domain ?? null) === family.domain;
          const familyLabel = optionLabel(pageContract, "audit_operation_families", family.key);
          return (
            <Link
              key={family.key}
              href={hrefWithUpdates(sp, { domain: family.domain, cursor: null, page: null })}
              replace
              scroll={false}
              className={active ? "on" : ""}
              title={`${copy(pageContract, "filter.family_title_prefix")} ${familyLabel}`}
            >
              <span className="oc">
                <Icon className="ic" aria-hidden="true" />
              </span>
              {familyLabel}
              <span className="cbq">{operationCounts.get(family.domain ?? "all") ?? 0}</span>
            </Link>
          );
        })}
      </div>

      <div className="wftoolbar" style={{ marginBottom: 14 }}>
	        <form className="tsearch" action={PATHNAME} style={{ maxWidth: 300 }} title={copy(pageContract, "filter.search_label")}>
          {preservedHiddenInputs(sp, ["q", "cursor", "page", "cursor_stack", "audit_id"])}
          <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
          <input
            name="q"
            defaultValue={filters.q ?? ""}
	            placeholder={copy(pageContract, "filter.search_placeholder")}
	            aria-label={copy(pageContract, "filter.search_label")}
          />
        </form>
        <div className="subtabs" style={{ margin: 0 }}>
          {STATUS_TABS.map((tab) => (
            <Link
              key={tab.key}
              href={hrefWithUpdates(sp, { status: tab.status ?? null, result: tab.result ?? null, proof_gaps: tab.proofGaps ? "true" : null, cursor: null, page: null })}
              replace
              scroll={false}
              className={activeStatusTab.key === tab.key ? "on" : ""}
            >
              {optionLabel(pageContract, "audit_status_tabs", tab.key)}
            </Link>
          ))}
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <Link
          href={hrefWithUpdates(sp, { anomalies_only: filters.anomaliesOnly ? null : "true", cursor: null, page: null })}
          replace
          scroll={false}
          className={`btn sm ${filters.anomaliesOnly ? "p" : ""}`}
        >
          <AlertTriangle className="ic" style={{ width: 13 }} aria-hidden="true" />
	          {copy(pageContract, "filter.anomalies_only")}
        </Link>
      </div>

      <div className="gridside">
        <section className="card">
          <div className="hd">
            <UserRound className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.span.title")}</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="pill">{actors.length} {copy(pageContract, "label.operators")}</span>
          </div>
          <form action={PATHNAME} className="spansearch">
            {preservedHiddenInputs(sp, ["actor_q", "cursor", "page", "cursor_stack", "audit_id"])}
	            <input name="actor_q" defaultValue={one(sp, "actor_q") ?? ""} placeholder={copy(pageContract, "filter.actor_placeholder")} />
          </form>
          <div className="bd feed auditops">
            {actors.length === 0 ? (
              <div className="muted small" style={{ padding: "8px 2px" }}>
	                {rows.length === 0 ? copy(pageContract, "empty.operators") : copy(pageContract, "empty.operators_filter")}
              </div>
            ) : (
              actors.map((actor) => (
                <Link
                  key={actor.key}
                  href={hrefWithUpdates(sp, { actor_id: actor.actorId ?? null, actor_type: actor.actorId ? null : actor.actorType, cursor: null, page: null, audit_id: null })}
                  replace
                  scroll={false}
                  className={`fitem${filters.actorId === actor.actorId || (!filters.actorId && filters.actorType === actor.actorType) ? " on" : ""}`}
                  style={{ textDecoration: "none" }}
                >
                  <div className="tx">
                    <b>{actor.actorId ? shortId(actor.actorId) : actor.actorType}</b>
                    <div className="mt">{actor.lastAction}</div>
                  </div>
                  <Tag tone="mut">{actor.count}</Tag>
                </Link>
              ))
            )}
          </div>
        </section>

        {/* min-width:0 lets the 1fr grid track shrink so the wide audit table scrolls inside its own
            overflow-x container instead of blowing the section past the viewport edge. */}
        <section className="card" style={{ minWidth: 0 }}>
          <div className="hd">
            <ScrollText className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
	            <h3>{copy(pageContract, "section.activity.title")}</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="small muted">
              {pageTrailMeta(page, rows.length, Boolean(nextHref), pageContract)}
            </span>
          </div>
	          <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.activity.aria")}>
	            <Pager prevHref={prevHref} nextHref={nextHref} page={page} count={rows.length} pageContract={pageContract} top />
            <table data-enh="1">
              <thead>
                <tr>
	                  {cols.map((c) => (
                    <th key={c}>{c}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 ? (
                  <tr>
	                    <td colSpan={cols.length}>
                      <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
	                        {listResult.ok ? copy(pageContract, "empty.activity") : copy(pageContract, "empty.activity_unavailable")}
                      </div>
                    </td>
                  </tr>
                ) : (
                  rows.map((row) => <AuditTableRow key={row.audit_id} row={row} searchParams={sp} pageContract={pageContract} />)
                )}
              </tbody>
            </table>
	            <Pager prevHref={prevHref} nextHref={nextHref} page={page} count={rows.length} pageContract={pageContract} />
            <div className="note" style={{ margin: "12px 14px" }}>
              <b>{copy(pageContract, "label.append_only_title")}</b> {copy(pageContract, "label.append_only_body")}
            </div>
          </div>
        </section>
      </div>

      {/* Raw developer fields are NOT the primary UX. They live here for entity-history deep links
          (resource_type / resource_id) and power-user filtering, preserving the business selections above. */}
      <details className="card" style={{ marginTop: 14 }}>
        <summary className="hd" style={{ cursor: "pointer", listStyle: "revert" }}>
          <Filter className="ic" style={{ color: "var(--muted)" }} aria-hidden="true" />
	          <h3>{copy(pageContract, "section.advanced.title")}</h3>
          <span className="muted small" style={{ marginLeft: 8 }}>{copy(pageContract, "label.advanced_note")}</span>
        </summary>
        <form className="bd" action={PATHNAME} style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
          {preservedHiddenInputs(sp, ["actor_id", "module", "category", "resource_type", "resource_id", "cursor", "page", "cursor_stack"])}
          <Field name="actor_id" label={copy(pageContract, "field.actor_id")} value={filters.actorId} placeholder={copy(pageContract, "placeholder.actor_uuid")} width={184} />
          <Field name="resource_type" label={copy(pageContract, "field.resource_type")} value={filters.resourceType} placeholder={copy(pageContract, "placeholder.goat")} width={120} />
          <Field name="resource_id" label={copy(pageContract, "field.resource_id")} value={filters.resourceId} placeholder={copy(pageContract, "placeholder.uuid")} width={184} />
          <Field name="module" label={copy(pageContract, "field.module")} value={filters.module} placeholder={copy(pageContract, "placeholder.source_entry")} width={150} />
          <Field name="category" label={copy(pageContract, "field.category")} value={filters.category} placeholder={copy(pageContract, "placeholder.accepted_intake")} width={158} />
          <button type="submit" className="btn sm">
            <Filter className="ic" aria-hidden="true" />
	            {copy(pageContract, "filter.apply")}
          </button>
          <Link href={clearedHref} replace scroll={false} className="lk small" style={{ marginBottom: 8 }}>
	            {copy(pageContract, "filter.clear_all")}
          </Link>
        </form>
      </details>
	      {selectedRow ? <AuditDetailDrawer row={selectedRow} searchParams={sp} pageContract={pageContract} /> : null}
    </div>
  );
}

function AuditTableRow({ row, searchParams, pageContract }: { row: OperationsAuditRow; searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const result = metaString(row, "status") ?? metaString(row, "result") ?? (row.anomaly ? "flagged" : "recorded");
  const proof = metaString(row, "proof_id") ?? metaString(row, "proof_ref_id") ?? metaString(row, "media_proof_id") ?? (row.action.includes("proof") ? "proof event" : undefined);
  const operation = operationLabel(row, pageContract);
  const operator = operatorLabel(row);
  const target = targetLabel(row);
  const detailHref = hrefWithUpdates(searchParams, { audit_id: row.audit_id });
  const OperationIcon = operation.icon;
  return (
    <tr className={row.anomaly ? "audit-anomaly" : undefined}>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        <Link href={detailHref} className="lk small" scroll={false}>
          {fmtDateTime(row.recorded_at)}
        </Link>
      </td>
      <td>
        <span className="opcell">
          <span className="oc">
            <OperationIcon className="ic" aria-hidden="true" />
          </span>
          {operation.label}
        </span>
        {operation.detail ? <div className="mt">{operation.detail}</div> : null}
      </td>
      <td>
        <b className="trc opn">{operator.primary}</b>
        <div className="mt trc opn">{operator.secondary}</div>
      </td>
      <td>
        <Link href={detailHref} className="lk" scroll={false}>
          {humanAction(row.action)}
        </Link>
      </td>
      <td>{target.href ? <Link href={target.href} className="gid">{target.label}</Link> : target.label}</td>
      <td>
        <Tag tone={row.anomaly ? "dng" : toneForResult(result)} title={row.anomaly ? copy(pageContract, "label.flagged_anomaly") : undefined}>
          {result}
        </Tag>
      </td>
      <td>{dash(proof)}</td>
    </tr>
  );
}

function AuditDetailDrawer({ row, searchParams, pageContract }: { row: OperationsAuditRow; searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const result = metaString(row, "status") ?? metaString(row, "result") ?? (row.anomaly ? "flagged" : "recorded");
  const proof = metaString(row, "proof_id") ?? metaString(row, "proof_ref_id") ?? metaString(row, "media_proof_id");
  const operation = operationLabel(row, pageContract);
  const target = targetLabel(row);
  const operator = operatorLabel(row);
  const closeHref = hrefWithUpdates(searchParams, { audit_id: null });
  const OperationIcon = operation.icon;
  return (
	    <>
	      <Link
	        href={closeHref}
        replace
        className="veil"
	        aria-label={copy(pageContract, "drawer.record.close_label")}
        scroll={false}
        style={{ opacity: 1, pointerEvents: "auto" }}
      />
	      <aside className="drawer on" aria-label={copy(pageContract, "drawer.record.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <OperationIcon className="ic" aria-hidden="true" />
          </span>
          <div>
	            <div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div>
            <h2>{humanAction(row.action)}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
	          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="helpgrid">
	            <div className="hk">{tableLabels(pageContract, "activity-trail")[0]}</div>
            <div>{fmtDateTime(row.recorded_at)}</div>
	            <div className="hk">{tableLabels(pageContract, "activity-trail")[1]}</div>
            <div>
              <b>{operation.label}</b>
              {operation.detail ? <div className="mt">{operation.detail}</div> : null}
            </div>
	            <div className="hk">{tableLabels(pageContract, "activity-trail")[2]}</div>
            <div>
              <b>{operator.primary}</b>
              <div className="mt">{operator.secondary}</div>
            </div>
	            <div className="hk">{tableLabels(pageContract, "activity-trail")[4]}</div>
            <div>{target.href ? <Link href={target.href} className="gid">{target.label}</Link> : target.label}</div>
	            <div className="hk">{tableLabels(pageContract, "activity-trail")[5]}</div>
            <div>
              <Tag tone={row.anomaly ? "dng" : toneForResult(result)}>{result}</Tag>
            </div>
	            <div className="hk">{tableLabels(pageContract, "activity-trail")[6]}</div>
            <div>{dash(proof)}</div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>
	            {copy(pageContract, "drawer.record.note")}
          </div>
        </div>
        <div className="df">
          <Link href={target.href ?? closeHref} className={`btn p${target.href ? "" : " disabled"}`} aria-disabled={!target.href} scroll={false}>
	            {copy(pageContract, "drawer.open_target")}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
	            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}

function KPI({
  label,
  value,
  hint,
  tone,
  icon: Icon,
  href,
}: {
  label: string;
  value: string;
  hint: string;
  tone: Tone;
  icon: typeof Zap;
  href?: string | null;
}) {
  const content = (
    <>
      <span className="acc" style={{ background: accentForTone(tone) }} aria-hidden="true" />
      <div className="lab">
        <Icon className="ic" style={{ width: 14 }} aria-hidden="true" />
        {label}
      </div>
      <div className="val">{value}</div>
      <div className="dl">
        <span className="muted">{hint}</span>
      </div>
    </>
  );
  return href ? (
    <Link href={href} replace scroll={false} className="kpi" style={{ textDecoration: "none" }}>
      {content}
    </Link>
  ) : (
    <div className="kpi">{content}</div>
  );
}

function Field({
  name,
  label,
  value,
  placeholder,
  width,
}: {
  name: string;
  label: string;
  value?: string;
  placeholder: string;
  width: number;
}) {
  const id = `audit-${name}`;
  return (
    <div className="fld" style={{ width, marginBottom: 0 }}>
      <label htmlFor={id}>{label}</label>
      <input id={id} name={name} defaultValue={value ?? ""} placeholder={placeholder} />
    </div>
  );
}

function Pager({
  prevHref,
  nextHref,
  page,
  count,
  pageContract,
  top = false,
}: {
  prevHref: string | null;
  nextHref: string | null;
  page: number;
  count: number;
  pageContract: AdminUiPageContract;
  top?: boolean;
}) {
  return (
    <div className="pager2" style={top ? { borderTop: 0, borderBottom: "1px solid var(--line2)" } : undefined}>
      <span className="muted small">
        {pageTrailMeta(page, count, Boolean(nextHref), pageContract)}
      </span>
      <label className="pgmeta" style={{ marginRight: 0, fontWeight: 600 }}>
        {copy(pageContract, "pager.rows")}{" "}
        <select disabled title={copy(pageContract, "pager.fixed_reason")}>
          <option>25</option>
        </select>
      </label>
      {prevHref ? (
        <Link href={PATHNAME} scroll={false} className="btn sm">
          {copy(pageContract, "label.first")}
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed" }}>
          {copy(pageContract, "label.first")}
        </span>
      )}
      {prevHref ? (
        <Link href={prevHref} scroll={false} className="btn sm">
          {copy(pageContract, "label.prev")}
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed" }}>
          {copy(pageContract, "label.prev")}
        </span>
      )}
      {nextHref ? (
        <Link href={nextHref} scroll={false} className="btn sm">
          {copy(pageContract, "label.next")}
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed" }}>
          {copy(pageContract, "label.next")}
        </span>
      )}
      <span className="btn sm" aria-disabled title={copy(pageContract, "reason.last_page_disabled")} style={{ opacity: 0.45, cursor: "not-allowed" }}>
        {copy(pageContract, "label.last")}
      </span>
    </div>
  );
}

function parseFilters(params: RouteSearchParams): OperationsAuditListParams {
  return {
    from: one(params, "from"),
    to: one(params, "to"),
    actorType: actorType(one(params, "actor_type")),
    actorId: one(params, "actor_id"),
    action: one(params, "action"),
    resourceType: one(params, "resource_type"),
    resourceId: one(params, "resource_id"),
    scopeType: one(params, "scope_type"),
    scopeId: one(params, "scope_id"),
    domain: one(params, "domain") ?? "vaccination",
    module: one(params, "module"),
    category: one(params, "category"),
    result: one(params, "result"),
    status: one(params, "status"),
    q: one(params, "q"),
    anomaliesOnly: one(params, "anomalies_only") === "true",
    proofGaps: one(params, "proof_gaps") === "true",
  };
}

function actorType(value: string | undefined): OperationsAuditActorType | undefined {
  return ACTOR_TYPES.find((candidate) => candidate === value);
}

function statusTabActive(tab: { status?: string; result?: string; proofGaps?: boolean }, filters: OperationsAuditListParams): boolean {
  if (tab.proofGaps) return Boolean(filters.proofGaps);
  if (!tab.status && !tab.result) return !filters.status && !filters.result && !filters.proofGaps;
  if (tab.status) return filters.status === tab.status;
  return filters.result === tab.result;
}

function spanOfControl(rows: OperationsAuditRow[], actorQ: string) {
  const groups = new Map<string, { key: string; actorId?: string; actorType: string; count: number; lastAction: string }>();
  for (const row of rows) {
    const key = row.actor_id ?? row.actor_type;
    const existing = groups.get(key);
    if (existing) {
      existing.count += 1;
      continue;
    }
    groups.set(key, { key, actorId: row.actor_id, actorType: row.actor_type, count: 1, lastAction: row.action });
  }
  return [...groups.values()]
    .filter((actor) => !actorQ || actor.key.toLowerCase().includes(actorQ) || actor.actorType.toLowerCase().includes(actorQ))
    .sort((a, b) => b.count - a.count);
}

function pageTrailMeta(page: number, count: number, hasNext: boolean, pageContract: AdminUiPageContract): string {
  if (count === 0) return `0 ${copy(pageContract, "label.results")}`;
  const start = (page - 1) * PAGE_SIZE + 1;
  const end = start + count - 1;
  return `${start}–${end}${hasNext ? "+" : ""} · ${copy(pageContract, "label.page")} ${page}`;
}

function familyForDomain(domain?: string | null) {
  return OPERATION_FAMILIES.find((family) => family.domain === (domain ?? null)) ?? OPERATION_FAMILIES[0];
}

function countByOperation(rows: OperationsAuditRow[], activeCount: number, activeDomain: string) {
  const counts = new Map<string, number>([[activeDomain, activeCount]]);
  for (const row of rows) {
    const key = metaString(row, "domain") ?? "admin";
    if (key === activeDomain) continue;
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  return counts;
}

function operationLabel(row: OperationsAuditRow, pageContract: AdminUiPageContract): { label: string; detail?: string; icon: typeof Zap } {
  const domain = metaString(row, "domain") ?? "admin";
  const family = familyForDomain(domain);
  const rawDetail = joinParts([metaString(row, "module"), metaString(row, "category")]);
  const detail = rawDetail === "—" ? undefined : rawDetail;
  return { label: optionLabel(pageContract, "audit_operation_families", family.key), detail, icon: family.icon };
}

function operatorLabel(row: OperationsAuditRow): { primary: string; secondary: string } {
  const name = metaString(row, "operator_name") ?? metaString(row, "actor_name");
  const role = metaString(row, "operator_role") ?? metaString(row, "role") ?? row.actor_type;
  if (name) return { primary: name, secondary: role };
  if (row.actor_id) return { primary: shortId(row.actor_id), secondary: role };
  return { primary: row.actor_type, secondary: "system event" };
}

function targetLabel(row: OperationsAuditRow): { label: string; href?: string } {
  const label =
    metaString(row, "target_label") ??
    metaString(row, "goat_id") ??
    metaString(row, "goat_code") ??
    metaString(row, "load_code") ??
    joinParts([row.resource_type, row.resource_id ? shortId(row.resource_id) : undefined]);
  const goatId = metaString(row, "goat_id") ?? (row.resource_type === "goat" ? row.resource_id : undefined);
  if (goatId) return { label, href: `/goats/${encodeURIComponent(goatId)}` };
  if (row.resource_type === "source_load" && row.resource_id) {
    return { label, href: `/procurement/source-entry/loads/${encodeURIComponent(row.resource_id)}` };
  }
  return { label };
}

function humanAction(action: string): string {
  return action
    .split(/[._:]+/)
    .filter(Boolean)
    .map((part) => (part.length <= 3 ? part.toUpperCase() : part[0]?.toUpperCase() + part.slice(1)))
    .join(" ");
}

function metaString(row: OperationsAuditRow, key: string): string | undefined {
  const value = row.metadata[key];
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function toneForResult(result: string): Tone {
  const normalized = result.toLowerCase();
  if (["failed", "rejected", "rollback", "deleted", "skipped", "mismatch", "flagged"].includes(normalized)) return "dng";
  if (["queued", "pending", "awaiting", "awaiting_verification", "verification_pending", "rework"].includes(normalized)) return "warn";
  if (["accepted", "success", "succeeded", "completed", "recorded"].includes(normalized)) return "ok";
  return "info";
}

function accentForTone(tone: Tone): string {
  if (tone === "warn") return "var(--amber)";
  if (tone === "dng") return "var(--danger)";
  if (tone === "teal") return "var(--teal)";
  return "var(--brand)";
}

function hrefWithUpdates(params: RouteSearchParams, updates: Record<string, string | boolean | null | undefined>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === "cursor_stack") continue;
    if (Object.prototype.hasOwnProperty.call(updates, key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  for (const [key, value] of Object.entries(updates)) {
    next.delete(key);
    if (value === null || value === undefined || value === false || value === "") continue;
    next.set(key, String(value));
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

// Clearing filters resets the trail to its base while keeping the role-preview lens and top-bar scope.
function clearHref(params: RouteSearchParams): string {
  const next = new URLSearchParams();
  for (const key of PRESERVE_ON_CLEAR) {
    const value = one(params, key);
    if (value) next.set(key, value);
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

function preservedHiddenInputs(params: RouteSearchParams, exclude: string[]) {
  const excluded = new Set(exclude);
  return Object.entries(params).flatMap(([key, value]) => {
    if (excluded.has(key)) return [];
    if (Array.isArray(value)) {
      return value.map((item) => <input key={`${key}:${item}`} type="hidden" name={key} value={item} />);
    }
    return value ? [<input key={key} type="hidden" name={key} value={value} />] : [];
  });
}
