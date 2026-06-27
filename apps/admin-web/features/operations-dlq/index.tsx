import Link from "next/link";
import { redirect } from "next/navigation";
import { AlertTriangle, CheckCircle2, DatabaseZap, Filter, RotateCcw, Search, ShieldAlert, Trash2, X } from "lucide-react";
import type { ReactNode } from "react";

import { ClipText, Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listOutboxDLQ, type OutboxDLQMessage, type OutboxDLQStatus } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dash, fmtDateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { discardDLQAction, replayDLQAction } from "./actions";

const PATHNAME = "/operations/dlq";
const STATUS_KEYS = ["dead_letter", "failed", "discarded"] as const;

export async function OperationsDLQPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const status = parseStatus(one(sp, "status"));
  const eventType = one(sp, "event_type")?.trim();
  const topic = one(sp, "topic")?.trim();
  const q = one(sp, "q")?.trim().toLowerCase() ?? "";
  const result = await listOutboxDLQ({ status, eventType, topic, limit: 100 });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const allRows = result.ok ? (result.data.items ?? []) : [];
  const rows = q ? allRows.filter((row) => matchesSearch(row, q)) : allRows;
  const selected = rows.find((row) => row.outbox_id === one(sp, "dlq_id")) ?? allRows.find((row) => row.outbox_id === one(sp, "dlq_id"));
  const cols = tableLabels(pageContract, "dlq-events");
  const returnTo = hrefWithUpdates(sp, {});
  const actionKey = one(sp, "action_key");
  const actionStatus = one(sp, "action_status");

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
        <Link href="/operations/audit?domain=operations&module=dlq" className="btn">
          <ShieldAlert className="ic" aria-hidden="true" />
          {copy(pageContract, "action.open_audit")}
        </Link>
      </div>

      {result.ok ? null : (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? copy(pageContract, "error.dlq_unavailable")}</b>&nbsp;{result.error.message}
        </div>
      )}

      {actionStatus && actionKey ? (
        <div className={actionStatus === "success" ? "alert ok" : "alert warn"} style={{ marginBottom: 14 }}>
          <b>{copy(pageContract, actionKey)}</b>
          {one(sp, "action_code") ? <span>&nbsp;{one(sp, "action_code")}</span> : null}
        </div>
      ) : null}

      <div className="grid g4" style={{ marginBottom: 14 }}>
        <KPI label={copy(pageContract, "label.dead_letter_count")} value={String(countStatus(allRows, "dead_letter"))} tone="dng" icon={ShieldAlert} />
        <KPI label={copy(pageContract, "label.failed_count")} value={String(countStatus(allRows, "failed"))} tone="warn" icon={AlertTriangle} />
        <KPI label={copy(pageContract, "label.discarded_count")} value={String(countStatus(allRows, "discarded"))} tone="mut" icon={Trash2} />
        <KPI label={copy(pageContract, "label.rows_in_view")} value={String(rows.length)} tone="info" icon={DatabaseZap} />
      </div>

      <div className="subtabs" style={{ marginBottom: 12 }}>
        {STATUS_KEYS.map((key) => (
          <Link key={key} href={hrefWithUpdates(sp, { status: key, dlq_id: null, action_status: null, action_key: null, action_code: null, updated: null })} replace scroll={false} className={status === key ? "on" : ""}>
            {optionLabel(pageContract, "dlq_status_tabs", key)}
          </Link>
        ))}
      </div>

      <div className="wftoolbar" style={{ marginBottom: 14 }}>
        <form className="tsearch" action={PATHNAME} style={{ maxWidth: 320 }} title={copy(pageContract, "filter.search_label")}>
          {hiddenInputs(sp, ["q", "dlq_id", "action_status", "action_key", "action_code", "updated"])}
          <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
          <input name="q" defaultValue={one(sp, "q") ?? ""} placeholder={copy(pageContract, "filter.search_placeholder")} aria-label={copy(pageContract, "filter.search_label")} />
        </form>
        <form action={PATHNAME} style={{ display: "contents" }}>
          {hiddenInputs(sp, ["event_type", "topic", "dlq_id", "action_status", "action_key", "action_code", "updated"])}
          <div className="fld" style={{ width: 170, marginBottom: 0 }}>
            <label htmlFor="dlq-event-type">{copy(pageContract, "filter.event_type_label")}</label>
            <input id="dlq-event-type" name="event_type" defaultValue={eventType ?? ""} placeholder={copy(pageContract, "filter.event_type_placeholder")} />
          </div>
          <div className="fld" style={{ width: 170, marginBottom: 0 }}>
            <label htmlFor="dlq-topic">{copy(pageContract, "filter.topic_label")}</label>
            <input id="dlq-topic" name="topic" defaultValue={topic ?? ""} placeholder={copy(pageContract, "filter.topic_placeholder")} />
          </div>
          <button type="submit" className="btn sm">
            <Filter className="ic" aria-hidden="true" />
            {copy(pageContract, "filter.apply")}
          </button>
        </form>
        <Link href={PATHNAME} replace scroll={false} className="lk small">
          {copy(pageContract, "filter.clear_all")}
        </Link>
      </div>

      <section className="card" style={{ minWidth: 0 }}>
        <div className="hd">
          <DatabaseZap className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.events.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="pill">{copy(pageContract, "pager.fixed_reason")}</span>
        </div>
        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.events.aria")}>
          <table data-enh="1">
            <thead>
              <tr>
                {cols.map((label) => (
                  <th key={label}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={cols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok ? copy(pageContract, "empty.events") : copy(pageContract, "empty.events_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                rows.map((row) => <DLQTableRow key={row.outbox_id} row={row} searchParams={sp} pageContract={pageContract} />)
              )}
            </tbody>
          </table>
          <div className="note" style={{ margin: "12px 14px" }}>
            <b>{copy(pageContract, "label.replay_safe")}</b> · {copy(pageContract, "reason.replay")}
          </div>
        </div>
      </section>

      {selected ? <DLQDrawer row={selected} searchParams={sp} pageContract={pageContract} returnTo={returnTo} /> : null}
    </div>
  );
}

function DLQTableRow({ row, searchParams, pageContract }: { row: OutboxDLQMessage; searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const href = hrefWithUpdates(searchParams, { dlq_id: row.outbox_id, action_status: null, action_key: null, action_code: null, updated: null });
  return (
    <tr>
      <td>
        <Link href={href} className="celllink" scroll={false}>
          <ClipText title={row.event_type} className="strong">
            {row.event_type}
          </ClipText>
          <span className="mt">{shortId(row.event_id)}</span>
        </Link>
      </td>
      <td>
        <ClipText title={row.topic}>{row.topic}</ClipText>
        <div className="mt">
          <Tag tone={toneForStatus(row.status, pageContract)}>{optionLabel(pageContract, "dlq_status_tabs", row.status)}</Tag>
        </div>
      </td>
      <td>{row.attempt_count}</td>
      <td>{row.replay_count}</td>
      <td>
        <ClipText title={row.last_error}>{dash(row.last_error)}</ClipText>
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {fmtDateTime(row.updated_at)}
      </td>
    </tr>
  );
}

function DLQDrawer({
  row,
  searchParams,
  pageContract,
  returnTo,
}: {
  row: OutboxDLQMessage;
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
  returnTo: string;
}) {
  const closeHref = hrefWithUpdates(searchParams, { dlq_id: null, action_status: null, action_key: null, action_code: null, updated: null });
  const statusTone = toneForStatus(row.status, pageContract);
  const replayAction = repairAction(pageContract, "replay");
  const discardAction = repairAction(pageContract, "discard");
  const replayDisabledReason = row.status === "discarded" ? copy(pageContract, "reason.repair_discarded") : replayAction.disabled_reason;
  const discardDisabledReason = row.status === "discarded" ? copy(pageContract, "reason.repair_discarded") : discardAction.disabled_reason;
  const replayDisabled = row.status === "discarded" || !replayAction.enabled;
  const discardDisabled = row.status === "discarded" || !discardAction.enabled;
  const repairDisabledReason = replayDisabledReason || discardDisabledReason;
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false} style={{ opacity: 1, pointerEvents: "auto" }} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.record.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <DatabaseZap className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div>
            <h2>{row.event_type}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <Meta label={copy(pageContract, "label.status")}>
              <Tag tone={statusTone}>{optionLabel(pageContract, "dlq_status_tabs", row.status)}</Tag>
            </Meta>
            <Meta label={copy(pageContract, "label.event_id")}>{row.event_id}</Meta>
            <Meta label={copy(pageContract, "label.aggregate")}>{row.aggregate_type} · {shortId(row.aggregate_id)}</Meta>
            <Meta label={copy(pageContract, "label.idempotency_key")}>{row.idempotency_key}</Meta>
            <Meta label={copy(pageContract, "label.trace")}>{dash(row.trace_id)}</Meta>
            <Meta label={copy(pageContract, "label.created")}>{fmtDateTime(row.created_at)}</Meta>
            <Meta label={copy(pageContract, "label.updated")}>{fmtDateTime(row.updated_at)}</Meta>
            <Meta label={copy(pageContract, "label.attempts")}>{row.attempt_count}</Meta>
            <Meta label={copy(pageContract, "label.replays")}>{row.replay_count}</Meta>
          </div>
          <div className="note" style={{ marginTop: 14 }}>
            {copy(pageContract, "drawer.record.note")}
          </div>
          <div className="helpgrid" style={{ marginTop: 14 }}>
            <div className="hk">{copy(pageContract, "label.last_error")}</div>
            <div>{dash(row.last_error)}</div>
            <div className="hk">{copy(pageContract, "label.headers")}</div>
            <pre style={preStyle}>{pretty(row.headers)}</pre>
            <div className="hk">{copy(pageContract, "label.payload")}</div>
            <pre style={preStyle}>{pretty(row.payload)}</pre>
          </div>
          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <RotateCcw className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
              <h3>{copy(pageContract, "section.repair.title")}</h3>
            </div>
            <div className="bd">
              <form action={replayDLQAction} style={{ display: "grid", gap: 8, marginBottom: 12 }}>
                <input type="hidden" name="outbox_id" value={row.outbox_id} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{copy(pageContract, "form.reason_label")}</span>
                  <textarea name="reason" rows={2} placeholder={copy(pageContract, "form.reason_placeholder")} disabled={replayDisabled} />
                </label>
                <div className="note">{replayDisabledReason || copy(pageContract, "reason.replay")}</div>
                <button type="submit" className="btn p" disabled={replayDisabled} aria-disabled={replayDisabled} title={replayDisabledReason}>
                  <CheckCircle2 className="ic" aria-hidden="true" />
                  {replayAction.label}
                </button>
              </form>
              <form action={discardDLQAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="outbox_id" value={row.outbox_id} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{copy(pageContract, "form.reason_label")}</span>
                  <textarea name="reason" rows={2} placeholder={copy(pageContract, "form.reason_placeholder")} disabled={discardDisabled} />
                </label>
                <div className="note">{discardDisabledReason || copy(pageContract, "reason.discard")}</div>
                <button type="submit" className="btn" disabled={discardDisabled} aria-disabled={discardDisabled} title={discardDisabledReason}>
                  <Trash2 className="ic" aria-hidden="true" />
                  {discardAction.label}
                </button>
              </form>
              {repairDisabledReason ? <div className="note" style={{ marginTop: 10 }}>{repairDisabledReason}</div> : null}
            </div>
          </section>
        </div>
        <div className="df">
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}

function repairAction(pageContract: AdminUiPageContract, key: string): AdminUiOption {
  const action = optionGroup(pageContract, "dlq_repair_actions").find((item) => item.key === key);
  if (!action) {
    throw new Error(`Admin-web page contract ${pageContract.route_id} missing option dlq_repair_actions.${key}`);
  }
  return action;
}

function KPI({ label, value, tone, icon: Icon }: { label: string; value: string; tone: Tone; icon: typeof DatabaseZap }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentForTone(tone) }} aria-hidden="true" />
      <div className="lab">
        <Icon className="ic" style={{ width: 14 }} aria-hidden="true" />
        {label}
      </div>
      <div className="val">{value}</div>
    </div>
  );
}

function Meta({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <span className="muted small">{label}</span>
      <b style={{ display: "block", marginTop: 3, overflowWrap: "anywhere" }}>{children}</b>
    </div>
  );
}

function parseStatus(raw: string | undefined): OutboxDLQStatus {
  return STATUS_KEYS.includes(raw as OutboxDLQStatus) ? (raw as OutboxDLQStatus) : "dead_letter";
}

function countStatus(rows: OutboxDLQMessage[], status: OutboxDLQStatus) {
  return rows.filter((row) => row.status === status).length;
}

function toneForStatus(status: string, pageContract: AdminUiPageContract): Tone {
  return tone(optionTone(pageContract, "dlq_status_tabs", status));
}

function tone(value: string): Tone {
  return ["ok", "warn", "dng", "info", "mut", "pur", "teal"].includes(value) ? (value as Tone) : "mut";
}

function matchesSearch(row: OutboxDLQMessage, q: string) {
  const haystack = [row.event_type, row.topic, row.status, row.aggregate_type, row.aggregate_id, row.event_id, row.outbox_id, row.last_error, row.idempotency_key, row.trace_id ?? ""]
    .join(" ")
    .toLowerCase();
  return haystack.includes(q);
}

function hrefWithUpdates(params: RouteSearchParams, updates: Record<string, string | null | undefined>) {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null || value === undefined || value === "") next.delete(key);
    else next.set(key, value);
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

function hiddenInputs(params: RouteSearchParams, exclude: string[]) {
  return Object.entries(params).flatMap(([key, value]) => {
    if (exclude.includes(key)) return [];
    if (Array.isArray(value)) {
      return value.filter(Boolean).map((item) => <input key={`${key}:${item}`} type="hidden" name={key} value={item} />);
    }
    return value ? [<input key={key} type="hidden" name={key} value={value} />] : [];
  });
}

function pretty(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function accentForTone(toneValue: Tone) {
  switch (toneValue) {
    case "ok":
      return "#6fd043";
    case "warn":
      return "#f7c948";
    case "dng":
      return "#ff6b6b";
    case "pur":
      return "#a77cff";
    case "teal":
      return "#35d2c6";
    case "info":
      return "#5da8ff";
    default:
      return "#7a8b78";
  }
}

const preStyle = {
  margin: 0,
  whiteSpace: "pre-wrap",
  overflowWrap: "anywhere",
  fontSize: 11,
  lineHeight: 1.5,
} as const;
