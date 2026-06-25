import Link from "next/link";
import { redirect } from "next/navigation";
import { AlertTriangle, ArrowLeft, ArrowRight, Clock, Filter, ScrollText, Search, ShieldCheck, Upload, UserRound, Zap } from "lucide-react";

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
import { dash, fmtDateTime, joinParts, shortId } from "@/lib/format";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";

const PATHNAME = "/operations/audit";
const PAGE_SIZE = 100;
const COLS = ["Time", "Operation", "Operator", "Action", "Target", "Result", "Proof"];
const ACTOR_TYPES = ["human", "system", "worker", "service", "user"] as const;

const VIEW_CHIPS: Array<{ label: string; actorType?: OperationsAuditActorType }> = [
  { label: "All actors" },
  { label: "Operators", actorType: "human" },
  { label: "System", actorType: "system" },
  { label: "Workers", actorType: "worker" },
  { label: "Services", actorType: "service" },
];

const OPERATION_CHIPS = [
  { label: "All operations", action: null },
  { label: "Goat created", action: "goat.created" },
  { label: "Obligation generated", action: "vaccination.obligation.generate" },
  { label: "SOP submitted", action: "sop.task.submit" },
  { label: "Verification accepted", action: "vaccination.verification.accept" },
];

export async function OperationsAuditPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const filters = parseFilters(sp);
  const actorQ = one(sp, "actor_q")?.trim().toLowerCase() ?? "";

  const [listResult, summaryResult] = await Promise.all([
    listOperationsAudit({ ...filters, limit: PAGE_SIZE, cursor: one(sp, "cursor") }),
    getOperationsAuditSummary(filters),
  ]);
  const authError = firstAuthRequiredError(listResult, summaryResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const rows = listResult.ok ? listResult.data.items : [];
  const summary = summaryResult.ok ? summaryResult.data : null;
  const actors = spanOfControl(rows, actorQ);
  const nextHref = listResult.ok ? hrefWithCursor(PATHNAME, sp, listResult.data.next_cursor ?? null) : null;
  const prevHref = hrefPreviousCursor(PATHNAME, sp);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            Admin · Data-Ops / <b>Audit log</b>
          </div>
          <h1>Audit Log</h1>
          <div className="sub">
            Every action by every operator <b>and admin</b> — organised by <b>operation</b>, not by area.
            Scoped to your hierarchy via the top-bar company/park and date controls. Append-only · tamper-proof.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <button type="button" className="btn" disabled aria-disabled="true" title="Export endpoint is outside this slice.">
          <Upload className="ic" aria-hidden="true" />
          Export
        </button>
      </div>

      {listResult.ok && summaryResult.ok ? null : (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{!listResult.ok ? listResult.error.code ?? listResult.error.kind : summaryResult.ok ? "audit_unavailable" : summaryResult.error.code ?? summaryResult.error.kind}</b>
          &nbsp;{!listResult.ok ? listResult.error.message : summaryResult.ok ? "Audit summary is unavailable." : summaryResult.error.message}
        </div>
      )}

      <div className="grid g4" style={{ marginBottom: 14 }}>
        <KPI label="Actions in view" value={summary ? String(summary.actions) : "—"} hint="tap to clear filters" tone="info" icon={Zap} href={PATHNAME} />
        <KPI label="Awaiting verification" value={summary ? String(summary.awaiting_verification) : "—"} hint="proof + sign-off" tone="warn" icon={Clock} href={hrefWithUpdates(sp, { status: "verification_pending", cursor: null, page: null })} />
        <KPI label="Proof coverage" value={summary ? `${summary.proof_coverage_percent}%` : "—"} hint="proof-bearing audit actions" tone="teal" icon={ShieldCheck} />
        <KPI label="Flagged anomalies" value={summary ? String(summary.anomalies) : "—"} hint="stock · deletes · skips" tone="dng" icon={AlertTriangle} href={hrefWithUpdates(sp, { anomalies_only: filters.anomaliesOnly ? null : "true", cursor: null, page: null })} />
      </div>

      <div className="subtabs" style={{ marginBottom: 12 }}>
        {VIEW_CHIPS.map((chip) => (
          <Link
            key={chip.label}
            href={hrefWithUpdates(sp, { actor_type: chip.actorType ?? null, cursor: null, page: null })}
            className={`tab ${filters.actorType === chip.actorType || (!filters.actorType && !chip.actorType) ? "on" : ""}`}
          >
            {chip.label}
          </Link>
        ))}
      </div>

      <div className="opf" style={{ marginBottom: 12 }}>
        {OPERATION_CHIPS.map((chip) => (
          <Link
            key={chip.label}
            href={hrefWithUpdates(sp, { action: chip.action, cursor: null, page: null })}
            className={`op ${filters.action === chip.action || (!filters.action && chip.action === null) ? "on" : ""}`}
          >
            {chip.label}
          </Link>
        ))}
      </div>

      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Search className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Search and filters</h3>
          <div className="sp" style={{ flex: 1 }} />
          <Link href={PATHNAME} className="lk small">
            Clear
          </Link>
        </div>
        <form className="bd" action={PATHNAME} style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
          <Field name="actor_id" label="Operator / actor" value={filters.actorId} placeholder="actor uuid" width={170} />
          <Field name="domain" label="Domain" value={filters.domain} placeholder="procurement" width={132} />
          <Field name="module" label="Module" value={filters.module} placeholder="source_entry" width={146} />
          <Field name="category" label="Category" value={filters.category} placeholder="accepted_intake" width={154} />
          <Field name="status" label="Status" value={filters.status} placeholder="queued" width={118} />
          <Field name="resource_type" label="Target type" value={filters.resourceType} placeholder="goat" width={120} />
          <Field name="resource_id" label="Target id" value={filters.resourceId} placeholder="uuid" width={170} />
          <label className="chip" style={{ display: "inline-flex", alignItems: "center", gap: 7, marginBottom: 2 }}>
            <input type="checkbox" name="anomalies_only" value="true" defaultChecked={filters.anomaliesOnly} />
            Anomalies only
          </label>
          <button type="submit" className="btn sm">
            <Filter className="ic" aria-hidden="true" />
            Apply
          </button>
        </form>
      </section>

      <div className="grid g2" style={{ gridTemplateColumns: "288px 1fr" }}>
        <section className="card">
          <div className="hd">
            <UserRound className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Span of control</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="pill">{actors.length}</span>
          </div>
          <form action={PATHNAME} className="spansearch">
            {preservedHiddenInputs(sp, ["actor_q", "cursor", "page", "cursor_stack"])}
            <input name="actor_q" defaultValue={one(sp, "actor_q") ?? ""} placeholder="Filter operators..." />
          </form>
          <div className="bd feed auditops">
            {actors.length === 0 ? (
              <div className="muted small" style={{ padding: "8px 2px" }}>
                {rows.length === 0 ? "No operators in the current audit page." : "No operators match this filter."}
              </div>
            ) : (
              actors.map((actor) => (
                <Link
                  key={actor.key}
                  href={hrefWithUpdates(sp, { actor_id: actor.actorId ?? null, actor_type: actor.actorId ? null : actor.actorType, cursor: null, page: null })}
                  className="fitem"
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

        <section className="card">
          <div className="hd">
            <ScrollText className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
            <h3>Activity trail</h3>
            <div className="sp" style={{ flex: 1 }} />
            <span className="small muted">
              Page {page} · {rows.length} entries
            </span>
          </div>
          <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label="Audit activity trail">
            <table data-enh="1">
              <thead>
                <tr>
                  {COLS.map((c) => (
                    <th key={c}>{c}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 ? (
                  <tr>
                    <td colSpan={COLS.length}>
                      <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                        {listResult.ok ? "No audit entries for the selected scope and filters." : "Audit rows are unavailable until the backend responds."}
                      </div>
                    </td>
                  </tr>
                ) : (
                  rows.map((row) => <AuditTableRow key={row.audit_id} row={row} />)
                )}
              </tbody>
            </table>
            <Pager prevHref={prevHref} nextHref={nextHref} page={page} count={rows.length} />
            <div className="note" style={{ margin: "12px 14px" }}>
              <b>Append-only · tamper-proof.</b> Stock-draw mismatches, record deletions, silent skips, and rework
              auto-raise anomalies into the operational trail.
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}

function AuditTableRow({ row }: { row: OperationsAuditRow }) {
  const result = metaString(row, "status") ?? metaString(row, "result") ?? (row.anomaly ? "flagged" : "recorded");
  const proof = metaString(row, "proof_id") ?? metaString(row, "proof_ref_id") ?? metaString(row, "media_proof_id") ?? (row.action.includes("proof") ? "proof event" : undefined);
  const operation = joinParts([metaString(row, "domain"), metaString(row, "module"), metaString(row, "category")]);
  return (
    <tr>
      <td className="muted">{fmtDateTime(row.recorded_at)}</td>
      <td>{operation}</td>
      <td>
        <span className="gid">{row.actor_id ? shortId(row.actor_id) : row.actor_type}</span>
      </td>
      <td>{row.action}</td>
      <td>{joinParts([row.resource_type, row.resource_id ? shortId(row.resource_id) : undefined])}</td>
      <td>
        <Tag tone={row.anomaly ? "dng" : toneForResult(result)} title={row.anomaly ? "Flagged anomaly" : undefined}>
          {result}
        </Tag>
      </td>
      <td>{dash(proof)}</td>
    </tr>
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
    <Link href={href} className="kpi" style={{ textDecoration: "none" }}>
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

function Pager({ prevHref, nextHref, page, count }: { prevHref: string | null; nextHref: string | null; page: number; count: number }) {
  return (
    <div className="pager2">
      <span className="muted small">
        Page {page} · {count} rows
      </span>
      {prevHref ? (
        <Link href={prevHref} scroll={false} className="btn sm">
          <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed" }}>
          <ArrowLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
        </span>
      )}
      {nextHref ? (
        <Link href={nextHref} scroll={false} className="btn sm">
          Next <ArrowRight className="ic" style={{ width: 13 }} aria-hidden="true" />
        </Link>
      ) : (
        <span className="btn sm" aria-disabled style={{ opacity: 0.45, cursor: "not-allowed" }}>
          Next <ArrowRight className="ic" style={{ width: 13 }} aria-hidden="true" />
        </span>
      )}
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
    domain: one(params, "domain"),
    module: one(params, "module"),
    category: one(params, "category"),
    result: one(params, "result"),
    status: one(params, "status"),
    anomaliesOnly: one(params, "anomalies_only") === "true",
  };
}

function actorType(value: string | undefined): OperationsAuditActorType | undefined {
  return ACTOR_TYPES.find((candidate) => candidate === value);
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
