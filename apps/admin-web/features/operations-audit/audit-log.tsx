import Form from "next/form";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { ListRowsSkeleton, StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import Grid from "@mui/material/Grid";
import Button from "@mui/material/Button";
import Avatar from "@mui/material/Avatar";
import Accordion from "@mui/material/Accordion";
import AccordionDetails from "@mui/material/AccordionDetails";
import AccordionSummary from "@mui/material/AccordionSummary";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import TextField from "@mui/material/TextField";
import { varAlpha } from "minimal-shared/utils";
import { Label } from "@/components/minimal/label";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { LinkButton } from "@/components/app/link-button";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { EmptyContent } from "@/components/minimal/empty-content";
import { SearchTextField } from "@/components/app/list/search-text-field";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { LinkSelect } from "@/components/app/link-select";
import { listOrEmpty } from "@/lib/list-or-empty";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";

import { Tag, type Tone } from "@/components/ui-primitives";
import { PageHeader } from "@/components/app/page-header";
import { TabPanel } from "@/components/app/template-tabs";
import { TemplateTabs } from "@/components/app/template-tabs";
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
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { LinkFiltersResult, type LinkFilterChip } from "@/components/app/link-filters-result";
import { AuditAnalytics } from "./audit-analytics";
import { AuditLogLocalDrawer, type AuditDrawerRecord } from "./audit-log-local-drawer";
import Alert from "@mui/material/Alert";
import { AUDIT_FAMILY_SELECT_WIDTH, AUDIT_OPERATOR_SKELETON_ROWS, AUDIT_SIDE_GRID, AUDIT_STATUS_TABS, AUDIT_STRIP_CELLS, PAGE_SIZE } from "./audit-layout";

const PATHNAME = "/operations/audit";
const ACTOR_TYPES = ["human", "system", "worker", "service", "user"] as const;

// Top-bar scope state to keep when a user clears the page filters.
const PRESERVE_ON_CLEAR = ["scope_mode", "park", "as_of", "range", "from", "to"];

// Business audit families are derived from durable audit metadata when available, with backend fallbacks for
// older rows that only captured action/resource names.
const OPERATION_FAMILIES: Array<{ key: string; domain: string | null; icon: IconifyName }> = [
  { key: "all", domain: null, icon: "ic:round-filter-list" },
  { key: "vaccination", domain: "vaccination", icon: "solar:medical-kit-bold" },
  { key: "procurement", domain: "procurement", icon: "carbon:delivery" },
  { key: "counts", domain: "counts", icon: "solar:bill-list-bold" },
  { key: "feed", domain: "feed", icon: "solar:box-minimalistic-bold" },
  { key: "weighing", domain: "weighing", icon: "solar:dumbbell-large-minimalistic-bold" },
  { key: "health", domain: "health", icon: "solar:heart-bold" },
  { key: "milk", domain: "milk", icon: "solar:tea-cup-bold" },
  { key: "admin", domain: "admin", icon: "solar:ssd-round-bold" },
  { key: "other", domain: "other", icon: "eva:more-horizontal-fill" },
];

// Result/status tabs map to real list filters.
const STATUS_TABS = AUDIT_STATUS_TABS;

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
  // One parallel round: the per-family summary reads do not depend on the list, so awaiting them
  // after it doubled server render latency (list+summary, THEN ten family summaries).
  // request-plan:ignore owner=ravi@mesha.sg issue=PR-350 expires=2026-12-31 reason=deliberate bounded fan-out (1 summary + a fixed 10-family list) in ONE parallel round; splitting it into two rounds is what doubled server render latency. The real fix is a per-domain breakdown on /operations/audit/summary so one request feeds every family tile.
  const [listResult, summaryResult, ...familySummaryResults] = await Promise.all([
    listOperationsAudit({ ...filters, limit: PAGE_SIZE, cursor: one(sp, "cursor") }),
    getOperationsAuditSummary(filters),
    ...OPERATION_FAMILIES.map((family) =>
      getOperationsAuditSummary({
        ...filters,
        domain: family.domain ?? undefined,
        module: undefined,
        category: undefined,
      }),
    ),
  ]);
  const authError = firstAuthRequiredError(listResult, summaryResult, ...familySummaryResults);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const rows = listResult.ok ? listOrEmpty(listResult.data.items) : [];
  const summary = summaryResult.ok ? summaryResult.data : null;
  const actors = spanOfControl(rows, actorQ);
  const operationCounts = countByOperation(familySummaryResults);
  const nextHref = listResult.ok ? hrefWithCursor(PATHNAME, sp, listResult.data.next_cursor ?? null) : null;
  const prevHref = hrefPreviousCursor(PATHNAME, sp);
  const clearedHref = clearHref(sp);
  const activeStatusTab = STATUS_TABS.find((tab) => statusTabActive(tab, filters)) ?? STATUS_TABS[0];
  const initialSelectedAuditId = one(sp, "audit_id");
  const cols = tableLabels(pageContract, "activity-trail");
  const closeDrawerHref = hrefWithUpdates(sp, { audit_id: null });
  const drawerRecords: AuditDrawerRecord[] = rows.map((row) => {
    const operation = operationLabel(row, pageContract);
    return {
      id: row.audit_id,
      anomaly: row.anomaly,
      actionLabel: humanAction(row.action),
      recordedAt: fmtDateTime(row.recorded_at),
      result: metaString(row, "status") ?? metaString(row, "result") ?? (row.anomaly ? "flagged" : "recorded"),
      proof: metaString(row, "proof_id") ?? metaString(row, "proof_ref_id") ?? metaString(row, "media_proof_id"),
      operation: { key: operation.key, label: operation.label, detail: operation.detail },
      operator: operatorLabel(row),
      target: targetLabel(row),
    };
  });

  const summaryPercent = (n: number) => (summary && summary.actions ? (n / summary.actions) * 100 : 0);
  const tabCount = (key: string): number | undefined => {
    if (!summary) return undefined;
    if (key === activeStatusTab.key) return summary.actions;
    if (activeStatusTab.key !== "all_results") return undefined;
    if (key === "awaiting") return summary.awaiting_verification;
    if (key === "rejected") return summary.rejected;
    return undefined;
  };
  const resetPage = { cursor: null, page: null } as const;
  const chips: LinkFilterChip[] = [
    ...(filters.q ? [{ id: "q", label: `${copy(pageContract, "action.search")}:`, value: filters.q, href: hrefWithUpdates(sp, { q: null, ...resetPage }) }] : []),
    ...(filters.domain
      ? [{ id: "domain", label: `${copy(pageContract, "filter.family_title_prefix")}:`, value: optionLabel(pageContract, "audit_operation_families", familyForDomain(filters.domain).key), href: hrefWithUpdates(sp, { domain: null, module: null, category: null, ...resetPage }) }]
      : []),
    ...(filters.anomaliesOnly ? [{ id: "anomalies", label: `${copy(pageContract, "filter.anomalies_only")}:`, value: "✓", href: hrefWithUpdates(sp, { anomalies_only: null, ...resetPage }) }] : []),
    ...(filters.actorId || filters.actorType
      ? [{ id: "actor", label: `${copy(pageContract, "field.actor_id")}:`, value: filters.actorId ? shortId(filters.actorId) : String(filters.actorType), href: hrefWithUpdates(sp, { actor_id: null, actor_type: null, ...resetPage }) }]
      : []),
    ...(filters.resourceType ? [{ id: "resource_type", label: `${copy(pageContract, "field.resource_type")}:`, value: filters.resourceType, href: hrefWithUpdates(sp, { resource_type: null, ...resetPage }) }] : []),
    ...(filters.resourceId ? [{ id: "resource_id", label: `${copy(pageContract, "field.resource_id")}:`, value: shortId(filters.resourceId), href: hrefWithUpdates(sp, { resource_id: null, ...resetPage }) }] : []),
    ...(filters.module ? [{ id: "module", label: `${copy(pageContract, "field.module")}:`, value: filters.module, href: hrefWithUpdates(sp, { module: null, ...resetPage }) }] : []),
    ...(filters.category ? [{ id: "category", label: `${copy(pageContract, "field.category")}:`, value: filters.category, href: hrefWithUpdates(sp, { category: null, ...resetPage }) }] : []),
  ];

  return (
    <Stack spacing={3}>
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
        actions={
          <Box component="span" title={copy(pageContract, "reason.export_pending")} sx={{ display: "inline-flex" }}>
            <Button variant="outlined" color="inherit" disabled aria-disabled="true" startIcon={<Iconify icon="solar:export-bold" />}>
              {copy(pageContract, "action.export")}
            </Button>
          </Box>
        }
      />

      {listResult.ok && summaryResult.ok ? null : (
        <Alert severity="error">
          <b>{!listResult.ok ? listResult.error.code ?? listResult.error.kind : summaryResult.ok ? copy(pageContract, "error.audit_unavailable") : summaryResult.error.code ?? summaryResult.error.kind}</b>
          &nbsp;{!listResult.ok ? listResult.error.message : summaryResult.ok ? copy(pageContract, "error.summary_unavailable") : summaryResult.error.message}
        </Alert>
      )}

      {/* Summary tiles, trail rows + pager and the operator list swap to their skeletons on a tab /
          filter / search / page click (guard: url-keyed-panel); tabs, toolbar and search stay. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<StatStripSkeleton count={AUDIT_STRIP_CELLS} meta wrapBelowMd />}>
      <AuditAnalytics
        cells={[
          { key: "actions", title: copy(pageContract, "label.actions_in_view"), total: copy(pageContract, "label.tap_clear_filters"), price: summary ? String(summary.actions) : "—", percent: summary ? 100 : 0, icon: "solar:bill-list-bold", color: "info", href: clearedHref },
          { key: "awaiting", title: copy(pageContract, "label.awaiting_verification"), total: copy(pageContract, "label.proof_signoff"), price: summary ? String(summary.awaiting_verification) : "—", percent: summaryPercent(summary?.awaiting_verification ?? 0), icon: "solar:clock-circle-bold", color: "warning", href: hrefWithUpdates(sp, { status: "verification_pending", result: null, proof_gaps: null, ...resetPage }) },
          { key: "proof", title: copy(pageContract, "label.proof_coverage"), total: copy(pageContract, "label.tap_proof_gaps"), price: summary ? `${summary.proof_coverage_percent}%` : "—", percent: summary?.proof_coverage_percent ?? 0, icon: "solar:shield-check-bold", color: "success", href: hrefWithUpdates(sp, { proof_gaps: filters.proofGaps ? null : "true", ...resetPage }) },
          { key: "anomalies", title: copy(pageContract, "label.flagged_anomalies"), total: copy(pageContract, "label.anomaly_sources"), price: summary ? String(summary.anomalies) : "—", percent: summaryPercent(summary?.anomalies ?? 0), icon: "solar:danger-triangle-bold", color: "error", href: hrefWithUpdates(sp, { anomalies_only: filters.anomaliesOnly ? null : "true", proof_gaps: null, ...resetPage }) },
        ]}
      />
      </UrlSuspense>

      {/* Template order list card: status Tabs with Label counts, the toolbar (operation family +
          search + Anomalies only), the filters result, the Scrollbar table and the pager footer. */}
      <Card>
        <TemplateTabs
          ariaLabel={copy(pageContract, "filter.search_label")}
          value={activeStatusTab.key}
          sx={{ px: { md: 2.5 } }}
          items={STATUS_TABS.map((tab) => ({
            value: tab.key,
            label: optionLabel(pageContract, "audit_status_tabs", tab.key),
            count: tabCount(tab.key),
            href: hrefWithUpdates(sp, { status: tab.status ?? null, result: tab.result ?? null, proof_gaps: tab.proofGaps ? "true" : null, ...resetPage }),
          }))}
        />

        <OrderTableToolbar
          filters={
            <LinkSelect
              label={copy(pageContract, "filter.family_title_prefix")}
              value={filters.domain ?? ""}
              minWidth={AUDIT_FAMILY_SELECT_WIDTH}
              options={[
                { value: "", label: copy(pageContract, "filter.all_option"), href: hrefWithUpdates(sp, { domain: null, module: null, category: null, ...resetPage }) },
                ...OPERATION_FAMILIES.filter((family) => family.domain).map((family) => ({
                  value: family.domain as string,
                  label: `${optionLabel(pageContract, "audit_operation_families", family.key)} · ${operationCounts.get(family.domain ?? "all") ?? 0}`,
                  href: hrefWithUpdates(sp, { domain: family.domain, module: null, category: null, ...resetPage }),
                })),
              ]}
            />
          }
          search={<Box sx={orderToolbarSearchSx}><Form action={PATHNAME} prefetch={false} title={copy(pageContract, "filter.search_label")}>
              {preservedHiddenInputs(sp, ["q", "cursor", "page", "cursor_stack", "audit_id"])}
              <SearchTextField name="q" defaultValue={filters.q ?? ""} placeholder={copy(pageContract, "filter.search_placeholder")} ariaLabel={copy(pageContract, "filter.search_label")} />
            </Form></Box>}
          trailing={
            <LinkButton
              href={hrefWithUpdates(sp, { anomalies_only: filters.anomaliesOnly ? null : "true", ...resetPage })}
              replace
              scroll={false}
              variant={filters.anomaliesOnly ? "contained" : "outlined"}
              color={filters.anomaliesOnly ? "primary" : "inherit"}
              startIcon={<Iconify icon="solar:danger-triangle-bold" />}
              sx={{ flexShrink: 0, whiteSpace: "nowrap" }}
            >
              {copy(pageContract, "filter.anomalies_only")}
            </LinkButton>
          }
        />

        <LinkFiltersResult totalResults={rows.length} chips={chips} resetHref={clearedHref} />

        {/* The status strip re-queries the trail; keyed on the tab + anomalies toggle so the panels
            cross-fade instead of snapping. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<TableSkeleton bare header={false} columns={cols.length || 6} rows={PAGE_SIZE} />}>
        <TabPanel tabKey={`${activeStatusTab.key}|${filters.anomaliesOnly ? "anom" : "all"}`}>
          <Scrollbar>
            <Table sx={{ minWidth: 960 }} aria-label={copy(pageContract, "table.activity.aria")}>
              <TableHeadCustom headCells={cols.map((label, index) => ({ id: `c${index}`, label }))} />
              <TableBody>
                {rows.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={cols.length}>
                      <EmptyContent filled title={listResult.ok ? copy(pageContract, "empty.activity") : copy(pageContract, "empty.activity_unavailable")} sx={{ py: 10 }} />
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row) => <AuditTableRow key={row.audit_id} row={row} searchParams={sp} pageContract={pageContract} />)
                )}
              </TableBody>
            </Table>
          </Scrollbar>
        </TabPanel>

        {/* Template pagination footer (links): rows per page is fixed on this cursor trail, so it
            reads as the value with its reason; Last stays disabled with its reason. */}
        <TablePaginationLinks
          page={Math.max(0, page - 1)}
          rowsPerPage={PAGE_SIZE}
          count={-1}
          prevHref={prevHref}
          nextHref={nextHref}
          hideActions={!prevHref && !nextHref && page <= 1}
          first={{ href: prevHref ? PATHNAME : null, label: copy(pageContract, "label.first") }}
          last={{ href: null, label: copy(pageContract, "label.last"), disabledReason: copy(pageContract, "reason.last_page_disabled") }}
          rangeLabel={pageTrailMeta(page, rows.length, Boolean(nextHref), pageContract)}
          prevLabel={copy(pageContract, "label.prev")}
          nextLabel={copy(pageContract, "label.next")}
          left={
            <Box component="span" title={copy(pageContract, "pager.fixed_reason")} sx={{ typography: "body2", display: "inline-flex", alignItems: "center", gap: 1, px: 1 }}>
              {copy(pageContract, "pager.rows")} <Label variant="soft">{PAGE_SIZE}</Label>
            </Box>
          }
        />
        </UrlSuspense>
      </Card>

      <Grid container spacing={3}>
        {/* Operators in this page of the trail: the template list-card anatomy (avatar, name,
            caption, count Label); a row narrows the trail to that operator. */}
        <Grid size={AUDIT_SIDE_GRID.operators}>
          <Card component="section" sx={{ height: 1 }}>
            <CardHeader
              title={copy(pageContract, "section.span.title")}
              action={
                <Label variant="soft" color="default">
                  {actors.length} {copy(pageContract, "label.operators")}
                </Label>
              }
            />
            <Form action={PATHNAME} prefetch={false}>
              <Box sx={{ px: 3, pt: 2.5 }}>
                {preservedHiddenInputs(sp, ["actor_q", "cursor", "page", "cursor_stack", "audit_id"])}
                <SearchTextField name="actor_q" defaultValue={one(sp, "actor_q") ?? ""} placeholder={copy(pageContract, "filter.actor_placeholder")} />
              </Box>
            </Form>
            <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PANEL_IGNORE} fallback={<Box sx={{ p: 3, pt: 2 }}><ListRowsSkeleton rows={AUDIT_OPERATOR_SKELETON_ROWS} /></Box>}>
            <Scrollbar sx={{ maxHeight: 420 }}>
              <Box sx={{ p: 3, pt: 2, display: "flex", flexDirection: "column", gap: 1 }}>
                {actors.length === 0 ? (
                  <Typography variant="body2" sx={{ color: "text.secondary", py: 1 }}>
                    {rows.length === 0 ? copy(pageContract, "empty.operators") : copy(pageContract, "empty.operators_filter")}
                  </Typography>
                ) : (
                  actors.map((actor) => {
                    const selected = filters.actorId === actor.actorId || (!filters.actorId && filters.actorType === actor.actorType);
                    const name = actor.actorId ? shortId(actor.actorId) : actor.actorType;
                    return (
                      <Box
                        component={Link}
                        key={actor.key}
                        href={hrefWithUpdates(sp, { actor_id: actor.actorId ?? null, actor_type: actor.actorId ? null : actor.actorType, cursor: null, page: null, audit_id: null })}
                        replace
                        scroll={false}
                        aria-current={selected ? "true" : undefined}
                        sx={{ color: "inherit", textDecoration: "none" }}
                      >
                        <Box sx={{ gap: 2, px: 1, py: 1, display: "flex", alignItems: "center", borderRadius: "var(--r-md)", minHeight: 44, bgcolor: selected ? "action.selected" : "transparent", "&:hover": { bgcolor: "action.hover" } }}>
                          <Avatar sx={{ width: 40, height: 40, typography: "subtitle2" }}>{name.slice(0, 1).toUpperCase()}</Avatar>
                          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
                            <Typography variant="subtitle2" noWrap>{name}</Typography>
                            <Typography variant="caption" noWrap component="div" sx={{ color: "text.secondary", mt: 0.25 }}>
                              {humanAction(actor.lastAction)}
                            </Typography>
                          </Box>
                          <Label variant="soft" color={selected ? "primary" : "default"}>{actor.count}</Label>
                        </Box>
                      </Box>
                    );
                  })
                )}
              </Box>
            </Scrollbar>
            </UrlSuspense>
          </Card>
        </Grid>

        {/* Raw developer fields are NOT the primary UX. They live here for entity-history deep links
            (resource_type / resource_id) and power-user filtering, preserving the selections above. */}
        <Grid size={AUDIT_SIDE_GRID.advanced}>
          <Card>
            <Accordion>
              <AccordionSummary expandIcon={<Iconify icon="eva:arrow-ios-downward-fill" />} sx={{ px: 3, py: 1.5 }}>
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="h6" component="h3">{copy(pageContract, "section.advanced.title")}</Typography>
                  <Typography variant="body2" sx={{ color: "text.secondary", mt: 0.5 }}>{copy(pageContract, "label.advanced_note")}</Typography>
                </Box>
              </AccordionSummary>
              <AccordionDetails sx={{ p: 0 }}>
                <Form action={PATHNAME} prefetch={false}>
                <Box sx={{ px: 3, pt: 1, pb: 3, display: "grid", gap: 2.5, gridTemplateColumns: { xs: "1fr", sm: "repeat(2, minmax(0, 1fr))" } }}>
                  {preservedHiddenInputs(sp, ["actor_id", "module", "category", "resource_type", "resource_id", "cursor", "page", "cursor_stack"])}
                  <Field name="actor_id" label={copy(pageContract, "field.actor_id")} value={filters.actorId} placeholder={copy(pageContract, "placeholder.actor_uuid")} />
                  <Field name="resource_type" label={copy(pageContract, "field.resource_type")} value={filters.resourceType} placeholder={copy(pageContract, "placeholder.goat")} />
                  <Field name="resource_id" label={copy(pageContract, "field.resource_id")} value={filters.resourceId} placeholder={copy(pageContract, "placeholder.uuid")} />
                  <Field name="module" label={copy(pageContract, "field.module")} value={filters.module} placeholder={copy(pageContract, "placeholder.source_entry")} />
                  <Field name="category" label={copy(pageContract, "field.category")} value={filters.category} placeholder={copy(pageContract, "placeholder.accepted_intake")} />
                  <Box sx={{ display: "flex", gap: 1.5, alignItems: "center", flexWrap: "wrap" }}>
                    <Button type="submit" variant="contained" color="primary">
                      {copy(pageContract, "filter.apply")}
                    </Button>
                    <LinkButton href={clearedHref} replace scroll={false} color="error" startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}>
                      {copy(pageContract, "filter.clear_all")}
                    </LinkButton>
                  </Box>
                </Box>
                </Form>
              </AccordionDetails>
            </Accordion>
          </Card>
        </Grid>
      </Grid>

      <AuditLogLocalDrawer
        records={drawerRecords}
        initialSelectedAuditId={initialSelectedAuditId}
        closeHref={closeDrawerHref}
        pageContract={pageContract}
      />
    </Stack>
  );
}

// Template order-table-row anatomy: two-line time cell, avatar + two-line operation cell, soft
// status Label; time and action open the record drawer locally (#audit_id).
function AuditTableRow({ row, searchParams, pageContract }: { row: OperationsAuditRow; searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const result = metaString(row, "status") ?? metaString(row, "result") ?? (row.anomaly ? "flagged" : "recorded");
  const proof = metaString(row, "proof_id") ?? metaString(row, "proof_ref_id") ?? metaString(row, "media_proof_id") ?? (row.action.includes("proof") ? "proof event" : undefined);
  const operation = operationLabel(row, pageContract);
  const operator = operatorLabel(row);
  const target = targetLabel(row);
  const detailHref = `${hrefWithUpdates(searchParams, { audit_id: null })}#audit_id=${encodeURIComponent(row.audit_id)}`;
  const [date, time] = fmtDateTime(row.recorded_at).split(" ");
  return (
    <TableRow hover sx={row.anomaly ? { bgcolor: varAlpha("var(--palette-error-mainChannel)", 0.08) } : undefined}>
      <TableCell sx={{ whiteSpace: "nowrap" }}>
        <Box component={LocalOverlayLink} href={detailHref} scroll={false} sx={{ color: "inherit", textDecoration: "none", display: "block" }}>
          <Box component="span" sx={{ display: "block", typography: "body2" }}>{date}</Box>
          <Box component="span" sx={{ display: "block", typography: "caption", color: "text.disabled", mt: 0.5 }}>{time}</Box>
        </Box>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap" }}>
        <Box sx={{ gap: 2, display: "flex", alignItems: "center" }}>
          <Avatar variant="rounded" sx={{ width: 36, height: 36, bgcolor: "background.neutral", color: "primary.main" }}>
            <Iconify icon={operation.icon} width={18} aria-hidden="true" />
          </Avatar>
          <Box sx={{ minWidth: 0 }}>
            <Box component="span" sx={{ display: "block", typography: "subtitle2" }}>{operation.label}</Box>
            {operation.detail ? <Box component="span" sx={{ display: "block", typography: "caption", color: "text.disabled", mt: 0.5 }}>{operation.detail}</Box> : null}
          </Box>
        </Box>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap" }}>
        <Box component="span" sx={{ display: "block", typography: "body2" }}>{operator.primary}</Box>
        <Box component="span" sx={{ display: "block", typography: "caption", color: "text.disabled", mt: 0.5 }}>{operator.secondary}</Box>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap" }}>
        <Box component={LocalOverlayLink} href={detailHref} scroll={false} sx={{ color: "inherit" }}>
          {humanAction(row.action)}
        </Box>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap" }}>{target.href ? <Box component={Link} href={target.href} sx={{ color: "inherit" }}>{target.label}</Box> : target.label}</TableCell>
      <TableCell>
        <Tag tone={row.anomaly ? "dng" : toneForResult(result)} title={row.anomaly ? copy(pageContract, "label.flagged_anomaly") : undefined}>
          {result}
        </Tag>
      </TableCell>
      <TableCell sx={{ whiteSpace: "nowrap", color: "text.secondary" }}>{dash(proof)}</TableCell>
    </TableRow>
  );
}

function Field({ name, label, value, placeholder }: { name: string; label: string; value?: string; placeholder: string }) {
  // Template outlined TextField with a floating label (the template filter form fields).
  return <TextField id={`audit-${name}`} name={name} label={label} defaultValue={value ?? ""} placeholder={placeholder} fullWidth slotProps={{ inputLabel: { shrink: true } }} />;
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

function countByOperation(summaryResults: Array<Awaited<ReturnType<typeof getOperationsAuditSummary>>>) {
  const counts = new Map<string, number>();
  OPERATION_FAMILIES.forEach((family, index) => {
    const result = summaryResults[index];
    counts.set(family.domain ?? "all", result?.ok ? result.data.actions : 0);
  });
  return counts;
}

function operationLabel(row: OperationsAuditRow, pageContract: AdminUiPageContract): { key: string; label: string; detail?: string; icon: IconifyName } {
  const domain = metaString(row, "domain") ?? "admin";
  const family = familyForDomain(domain);
  const rawDetail = joinParts([metaString(row, "module"), metaString(row, "category")]);
  const detail = rawDetail === "—" ? undefined : rawDetail;
  return { key: family.key, label: optionLabel(pageContract, "audit_operation_families", family.key), detail, icon: family.icon };
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
      return value.map((item) => <Box component="input" key={`${key}:${item}`} type="hidden" name={key} value={item} />);
    }
    return value ? [<Box component="input" key={key} type="hidden" name={key} value={value} />] : [];
  });
}

/** Params that never change the trail: the local record drawer. */
const PANEL_IGNORE = ["audit_id"] as const;
