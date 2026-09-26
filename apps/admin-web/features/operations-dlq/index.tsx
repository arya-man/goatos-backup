import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import TextField from "@mui/material/TextField";
import { Iconify } from "@/components/minimal/iconify";
import { LinkButton } from "@/components/minimal/link-button";
import { SearchTextField } from "@/components/minimal/list/search-text-field";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { AlertTriangle, DatabaseZap, ShieldAlert, Trash2 } from "lucide-react";

import type { KitTone } from "@/lib/tone";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { AnimatedTabs, TabPanel } from "@/components/minimal/list/animated-tabs";
import { ClipText, Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listOutboxDLQ, type OutboxDLQMessage, type OutboxDLQStatus } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dash, fmtDateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { DLQLocalDrawer, type DLQDrawerRecord } from "./dlq-local-drawer";
import Alert from "@mui/material/Alert";

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
  const initialSelectedOutboxId = one(sp, "dlq_id");
  const cols = tableLabels(pageContract, "dlq-events");
  const closeDrawerHref = hrefWithUpdates(sp, { dlq_id: null, action_status: null, action_key: null, action_code: null, updated: null });
  const drawerRows: DLQDrawerRecord[] = allRows.map((row) => ({
    outbox_id: row.outbox_id,
    event_type: row.event_type,
    status: row.status,
    event_id: row.event_id,
    aggregate_type: row.aggregate_type,
    aggregate_id: row.aggregate_id,
    idempotency_key: row.idempotency_key,
    trace_id: row.trace_id,
    created_at: row.created_at,
    updated_at: row.updated_at,
    attempt_count: row.attempt_count,
    replay_count: row.replay_count,
    last_error: row.last_error,
    headers: row.headers,
    payload: row.payload,
  }));
  const actionKey = one(sp, "action_key");
  const actionStatus = one(sp, "action_status");

  return (
    <div className="kit-enter screen on">
      <div>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
          actions={
            <Link href="/operations/audit?domain=operations&module=dlq" className="btn">
              <ShieldAlert className="ic" aria-hidden="true" />
              {copy(pageContract, "action.open_audit")}
            </Link>
          }
        />
      </div>

      {result.ok ? null : (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? copy(pageContract, "error.dlq_unavailable")}</b>&nbsp;{result.error.message}
        </Alert>
      )}

      {actionStatus && actionKey ? (
        <Alert severity={actionStatus === "success" ? "success" : "warning"} style={{ marginBottom: 14 }}>
          <b>{copy(pageContract, actionKey)}</b>
          {one(sp, "action_code") ? <span>&nbsp;{one(sp, "action_code")}</span> : null}
        </Alert>
      ) : null}

      <div>
      <KpiGrid min={210}>
        <KPI label={copy(pageContract, "label.dead_letter_count")} value={String(countStatus(allRows, "dead_letter"))} tone="dng" icon={ShieldAlert} />
        <KPI label={copy(pageContract, "label.failed_count")} value={String(countStatus(allRows, "failed"))} tone="warn" icon={AlertTriangle} />
        <KPI label={copy(pageContract, "label.discarded_count")} value={String(countStatus(allRows, "discarded"))} tone="mut" icon={Trash2} />
        <KPI label={copy(pageContract, "label.rows_in_view")} value={String(rows.length)} tone="info" icon={DatabaseZap} />
      </KpiGrid>
      </div>

      <div style={{ margin: "14px 0 12px" }}>
        <AnimatedTabs
          value={status}
          ariaLabel={copy(pageContract, "filter.search_label")}
          items={STATUS_KEYS.map((key) => ({
            value: key,
            label: optionLabel(pageContract, "dlq_status_tabs", key),
            // The count is the whole reason an operator picks one of these tabs over another.
            count: countStatus(allRows, key),
            href: hrefWithUpdates(sp, { status: key, dlq_id: null, action_status: null, action_key: null, action_code: null, updated: null }),
          }))}
        />
      </div>

      {/* Template list toolbar on a Card: keyword search, then the event-type / topic filters. */}
      <div style={{ marginBottom: 14 }}>
        <Card sx={{ p: 2.5, display: "flex", gap: 2, flexWrap: "wrap", alignItems: "center", overflow: "visible" }}>
          <Box component="form" action={PATHNAME} title={copy(pageContract, "filter.search_label")} sx={{ flex: "1 1 280px", minWidth: 0 }}>
            {hiddenInputs(sp, ["q", "dlq_id", "action_status", "action_key", "action_code", "updated"])}
            <SearchTextField name="q" defaultValue={one(sp, "q") ?? ""} placeholder={copy(pageContract, "filter.search_placeholder")} ariaLabel={copy(pageContract, "filter.search_label")} />
          </Box>
          <Box component="form" action={PATHNAME} sx={{ display: "flex", gap: 2, flexWrap: "wrap", alignItems: "center" }}>
            {hiddenInputs(sp, ["event_type", "topic", "dlq_id", "action_status", "action_key", "action_code", "updated"])}
            <TextField id="dlq-event-type" name="event_type" label={copy(pageContract, "filter.event_type_label")} defaultValue={eventType ?? ""} placeholder={copy(pageContract, "filter.event_type_placeholder")} sx={{ width: { xs: 1, sm: 210 } }} slotProps={{ inputLabel: { shrink: true } }} />
            <TextField id="dlq-topic" name="topic" label={copy(pageContract, "filter.topic_label")} defaultValue={topic ?? ""} placeholder={copy(pageContract, "filter.topic_placeholder")} sx={{ width: { xs: 1, sm: 210 } }} slotProps={{ inputLabel: { shrink: true } }} />
            <Button type="submit" variant="contained">
              {copy(pageContract, "filter.apply")}
            </Button>
          </Box>
          <LinkButton href={PATHNAME} replace scroll={false} color="error" startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}>
            {copy(pageContract, "filter.clear_all")}
          </LinkButton>
        </Card>
      </div>

      <div>
      <TabPanel tabKey={status}>
      <section className="card kit-tablecard" style={{ minWidth: 0 }}>
        <div className="hd">
          <DatabaseZap className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.events.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="pill">{copy(pageContract, "pager.fixed_reason")}</span>
        </div>
        <div className="bd twrap tablewrap" style={{ padding: 0 }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.events.aria")}>
          <Table data-enh="1" className="operations-dlq-table">
            <TableHead>
              <TableRow>
                {cols.map((label) => (
                  <TableCell component="th" key={label}>{label}</TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={cols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok ? copy(pageContract, "empty.events") : copy(pageContract, "empty.events_unavailable")}
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => <DLQTableRow key={row.outbox_id} row={row} searchParams={sp} pageContract={pageContract} />)
              )}
            </TableBody>
          </Table>
        </div>
      </section>
      </TabPanel>
      </div>

      <DLQLocalDrawer
        rows={drawerRows}
        pageContract={pageContract}
        initialSelectedOutboxId={initialSelectedOutboxId}
        closeHref={closeDrawerHref}
      />
    </div>
  );
}

function DLQTableRow({ row, searchParams, pageContract }: { row: OutboxDLQMessage; searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const closeHref = hrefWithUpdates(searchParams, { dlq_id: null, action_status: null, action_key: null, action_code: null, updated: null });
  const href = `${closeHref}#dlq_id=${encodeURIComponent(row.outbox_id)}`;
  return (
    <TableRow>
      <TableCell>
        <LocalOverlayLink href={href} className="celllink" scroll={false}>
          <ClipText title={row.event_type} className="strong">
            {row.event_type}
          </ClipText>
          <span className="mt">{shortId(row.event_id)}</span>
        </LocalOverlayLink>
      </TableCell>
      <TableCell>
        <ClipText title={row.topic}>{row.topic}</ClipText>
        <div className="mt">
          <Tag tone={toneForStatus(row.status, pageContract)}>{optionLabel(pageContract, "dlq_status_tabs", row.status)}</Tag>
        </div>
      </TableCell>
      <TableCell>{row.attempt_count}</TableCell>
      <TableCell>{row.replay_count}</TableCell>
      <TableCell>
        <ClipText title={row.last_error}>{dash(row.last_error)}</ClipText>
      </TableCell>
      <TableCell className="muted" style={{ whiteSpace: "nowrap" }}>
        {fmtDateTime(row.updated_at)}
      </TableCell>
    </TableRow>
  );
}

const KPI_TONE: Record<Tone, KitTone> = { ok: "success", warn: "warning", dng: "error", info: "info", mut: "neutral", pur: "violet", teal: "info" };

function KPI({ label, value, tone, icon: Icon }: { label: string; value: string; tone: Tone; icon: typeof DatabaseZap }) {
  const numeric = /^\d+$/.test(value) ? Number(value) : null;
  return <KpiCard label={label} value={numeric ?? value} tone={KPI_TONE[tone]} icon={<Icon />} />;
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

