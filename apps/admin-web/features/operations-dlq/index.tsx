import Form from "next/form";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { Iconify } from "@/components/minimal/iconify";
import { LinkButton } from "@/components/app/link-button";
import { EmptyContent } from "@/components/minimal/empty-content";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { SearchTextField } from "@/components/app/list/search-text-field";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import type { InvoiceAnalyticColor } from "./dlq-analytics";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { fPercent } from "@/components/minimal/_shared/format-number";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";

import { PageHeader } from "@/components/app/page-header";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listOutboxDLQ, type OutboxDLQMessage, type OutboxDLQStatus } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dash, fmtDateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { LinkFiltersResult, type LinkFilterChip } from "@/components/app/link-filters-result";
import { DLQAnalytics } from "./dlq-analytics";
import { DLQLocalDrawer, type DLQDrawerRecord } from "./dlq-local-drawer";
import Alert from "@mui/material/Alert";

const PATHNAME = "/operations/dlq";
const STATUS_KEYS = ["dead_letter", "failed", "discarded"] as const;
const DRAWER_PARAMS = { dlq_id: null, action_status: null, action_key: null, action_code: null, updated: null } as const;

// Template invoice list view (sections/invoice/view/invoice-list-view.tsx): the InvoiceAnalytic strip
// in its own Card, then the list Card — status Tabs with Label counts, the toolbar
// (sections/order/order-table-toolbar), the filters result, the Scrollbar table with TableHeadCustom
// and the pagination footer. Rows open the DLQ record drawer locally (#dlq_id).
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
  const rawQ = one(sp, "q")?.trim() ?? "";
  const q = rawQ.toLowerCase();
  const result = await listOutboxDLQ({ status, eventType, topic, limit: 100 });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const allRows = result.ok ? (result.data.items ?? []) : [];
  const rows = q ? allRows.filter((row) => matchesSearch(row, q)) : allRows;
  const initialSelectedOutboxId = one(sp, "dlq_id");
  const cols = tableLabels(pageContract, "dlq-events");
  const closeDrawerHref = hrefWithUpdates(sp, DRAWER_PARAMS);
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
  const total = allRows.length;
  const share = (n: number) => (total ? (n / total) * 100 : 0);
  const analytics: { key: string; label: string; count: number; color: InvoiceAnalyticColor; icon: "solar:danger-triangle-bold" | "solar:danger-bold" | "solar:trash-bin-trash-bold" | "solar:bill-list-bold" }[] = [
    { key: "dead_letter", label: copy(pageContract, "label.dead_letter_count"), count: countStatus(allRows, "dead_letter"), color: "error", icon: "solar:danger-triangle-bold" },
    { key: "failed", label: copy(pageContract, "label.failed_count"), count: countStatus(allRows, "failed"), color: "warning", icon: "solar:danger-bold" },
    { key: "discarded", label: copy(pageContract, "label.discarded_count"), count: countStatus(allRows, "discarded"), color: "secondary", icon: "solar:trash-bin-trash-bold" },
    { key: "rows", label: copy(pageContract, "label.rows_in_view"), count: rows.length, color: "info", icon: "solar:bill-list-bold" },
  ];
  const chips: LinkFilterChip[] = [
    ...(rawQ ? [{ id: "q", label: `${copy(pageContract, "action.search")}:`, value: rawQ, href: hrefWithUpdates(sp, { q: null, ...DRAWER_PARAMS }) }] : []),
    ...(eventType ? [{ id: "event_type", label: `${copy(pageContract, "filter.event_type_label")}:`, value: eventType, href: hrefWithUpdates(sp, { event_type: null, ...DRAWER_PARAMS }) }] : []),
    ...(topic ? [{ id: "topic", label: `${copy(pageContract, "filter.topic_label")}:`, value: topic, href: hrefWithUpdates(sp, { topic: null, ...DRAWER_PARAMS }) }] : []),
  ];

  return (
    <div className="screen on">
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={
          <LinkButton href="/operations/audit?domain=operations&module=dlq" variant="outlined" color="inherit" startIcon={<Iconify icon="solar:shield-check-bold" />}>
            {copy(pageContract, "action.open_audit")}
          </LinkButton>
        }
      />

      {result.ok ? null : (
        <Alert severity="error">
          <b>{result.error.code ?? copy(pageContract, "error.dlq_unavailable")}</b>&nbsp;{result.error.message}
        </Alert>
      )}

      {actionStatus && actionKey ? (
        <Alert severity={actionStatus === "success" ? "success" : "warning"}>
          <b>{copy(pageContract, actionKey)}</b>
          {one(sp, "action_code") ? <span>&nbsp;{one(sp, "action_code")}</span> : null}
        </Alert>
      ) : null}

      {/* Summary tiles and event rows swap to their skeleton on a tab / filter / search change (guard:
          url-keyed-panel); tabs and toolbar stay on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<StatStripSkeleton count={4} meta />}>
      <DLQAnalytics
        cells={analytics.map((cell) => ({
          key: cell.key,
          title: cell.label,
          // A share of nothing is not a figure: no "0%" caption repeated in every cell (TR1-#35).
          total: total ? fPercent(share(cell.count)) : "",
          price: cell.count,
          percent: share(cell.count),
          icon: cell.icon,
          color: cell.color,
        }))}
      />
      </UrlSuspense>

      <Card data-filter-scope>
        <TemplateTabs
          value={status}
          ariaLabel={copy(pageContract, "filter.search_label")}
          sx={{ px: { md: 2.5 } }}
          items={STATUS_KEYS.map((key) => ({
            value: key,
            label: optionLabel(pageContract, "dlq_status_tabs", key),
            // The count is the whole reason an operator picks one of these tabs over another.
            count: countStatus(allRows, key),
            href: hrefWithUpdates(sp, { status: key, ...DRAWER_PARAMS }),
          }))}
        />

        <OrderTableToolbar
          filters={
            <Form action={PATHNAME} prefetch={false} style={{ display: "contents" }}>
              {hiddenInputs(sp, ["event_type", "topic", "dlq_id", "action_status", "action_key", "action_code", "updated"])}
              <TextField id="dlq-event-type" name="event_type" label={copy(pageContract, "filter.event_type_label")} placeholder={copy(pageContract, "filter.event_type_placeholder")} defaultValue={eventType ?? ""} sx={{ width: { xs: 1, md: 200 }, flexShrink: 0 }} slotProps={{ inputLabel: { shrink: true } }} />
              <TextField id="dlq-topic" name="topic" label={copy(pageContract, "filter.topic_label")} placeholder={copy(pageContract, "filter.topic_placeholder")} defaultValue={topic ?? ""} sx={{ width: { xs: 1, md: 200 }, flexShrink: 0 }} slotProps={{ inputLabel: { shrink: true } }} />
              <Button type="submit" variant="contained" color="primary" sx={{ flexShrink: 0 }}>
                {copy(pageContract, "filter.apply")}
              </Button>
            </Form>
          }
          search={<Box sx={orderToolbarSearchSx}><Form action={PATHNAME} prefetch={false} title={copy(pageContract, "filter.search_label")}>
              {hiddenInputs(sp, ["q", "dlq_id", "action_status", "action_key", "action_code", "updated"])}
              <SearchTextField name="q" defaultValue={rawQ} placeholder={copy(pageContract, "filter.search_placeholder")} ariaLabel={copy(pageContract, "filter.search_label")} />
            </Form></Box>}
        />

        <LinkFiltersResult totalResults={rows.length} chips={chips} resetHref={PATHNAME} />

        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<TableSkeleton bare header={false} columns={cols.length || 6} rows={10} />}>
        <TabPanel tabKey={status}>
          <Scrollbar>
            <Table sx={{ minWidth: 960 }} aria-label={copy(pageContract, "section.events.aria")}>
              <TableHeadCustom headCells={cols.map((label, index) => ({ id: `c${index}`, label }))} />
              <TableBody>
                {rows.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={cols.length}>
                      <EmptyContent filled title={result.ok ? copy(pageContract, "empty.events") : copy(pageContract, "empty.events_unavailable")} sx={{ py: 10 }} />
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row) => <DLQTableRow key={row.outbox_id} row={row} closeHref={closeDrawerHref} pageContract={pageContract} />)
                )}
              </TableBody>
            </Table>
          </Scrollbar>
        </TabPanel>

        <TablePaginationLinks
          page={0}
          rowsPerPage={100}
          count={rows.length}
          hideActions
          rangeLabel={`${rows.length} / ${total}`}
          prevLabel={copy(pageContract, "action.previous")}
          nextLabel={copy(pageContract, "action.next")}
          left={
            <Typography variant="caption" sx={{ color: "text.secondary", px: 1 }}>
              {copy(pageContract, "pager.fixed_reason")}
            </Typography>
          }
        />
        </UrlSuspense>
      </Card>

      <DLQLocalDrawer
        rows={drawerRows}
        pageContract={pageContract}
        initialSelectedOutboxId={initialSelectedOutboxId}
        closeHref={closeDrawerHref}
      />
    </div>
  );
}

function DLQTableRow({ row, closeHref, pageContract }: { row: OutboxDLQMessage; closeHref: string; pageContract: AdminUiPageContract }) {
  const href = `${closeHref}#dlq_id=${encodeURIComponent(row.outbox_id)}`;
  return (
    <TableRow hover>
      <TableCell sx={{ whiteSpace: "nowrap" }}>
        <LocalOverlayLink href={href} scroll={false} style={{ color: "inherit", textDecoration: "none", display: "block" }}>
          <Box component="span" sx={{ display: "block", typography: "subtitle2" }} title={row.event_type}>
            {row.event_type}
          </Box>
          <Box component="span" sx={{ display: "block", typography: "caption", color: "text.disabled", mt: 0.5 }}>
            {shortId(row.event_id)}
          </Box>
        </LocalOverlayLink>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap" }}>
        <Box component="span" sx={{ display: "block", mb: 0.5 }} title={row.topic}>
          {row.topic}
        </Box>
        <Tag tone={toneForStatus(row.status, pageContract)}>{optionLabel(pageContract, "dlq_status_tabs", row.status)}</Tag>
      </TableCell>
      <TableCell>{row.attempt_count}</TableCell>
      <TableCell>{row.replay_count}</TableCell>
      <TableCell sx={{ minWidth: 260, maxWidth: 420 }}>
        <Box component="span" title={row.last_error} sx={{ display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden" }}>
          {dash(row.last_error)}
        </Box>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap", color: "text.secondary" }}>
        {fmtDateTime(row.updated_at)}
      </TableCell>
    </TableRow>
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

/** Params that never change the event list: the local record drawer and the action feedback banner. */
const PANEL_IGNORE = ["dlq_id", "action_status", "action_key", "action_code", "updated"] as const;
