import Box from "@mui/material/Box";
import { PageRoot } from "@/components/app/page-root";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { DetailCardSkeleton, KpiRowSkeleton, StackSkeleton, TableSkeleton } from "@/components/app/skeletons";
import Card from "@mui/material/Card";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import LinearProgress from "@mui/material/LinearProgress";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { LinkButton } from "@/components/app/link-button";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TemplateTabs } from "@/components/app/template-tabs";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { OrderDetailsHistory, type OrderHistoryItem, type OrderHistoryTone } from "@/components/app/sections/order/order-details-history";
import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { PageHeader, type PageCrumb } from "@/components/app/page-header";
import { getVaccinationActionCenter, getVaccinationActionCenterCounts, listAnimalStages } from "@/lib/api/server";
import { stageNameMap } from "@/lib/stage-display";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import { copy, optionalCopy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { listOrEmpty } from "@/lib/list-or-empty";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { TONE_SWATCH, type Tone } from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { operationalLocationLabel } from "@/lib/operational-location.ts";
import { actionDriveLabel, actionWorkTitle, stageWords } from "./work-board";
import { VaccinationFilterButton, VisibleTableSearch, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { WorkflowsKpis } from "./workflows-kpis";
import Alert from "@mui/material/Alert";

// Top-level Workflows screen — vaccination-only, ported from the mock orchestration layout: KPI tiles,
// module pill row, a left workflow catalog, and a right chain-reaction map. Each catalog row drills into
// /workflows/{row_id}.

type ChainStep = { key: string; stage: string; title: string; detail: string };

function chainSteps(pageContract: AdminUiPageContract): ChainStep[] {
  return optionGroup(pageContract, "chain_steps").map((step) => ({
    key: step.key,
    stage: copy(pageContract, `stage.${step.key}.short`),
    title: step.label,
    detail: step.title,
  }));
}

type NodeState = "done" | "cur" | "next" | "pending" | "blocked";

// Per-step chain state computed (read-only) from the active obligation's lifecycle. Steps are marked done
// up to the leading run of satisfied stages; the first unsatisfied stage is the current step (or blocked
// when the obligation is gated), the next is "next", the rest pending — exactly the mock's onode states.
function chainStates(w: ActionCenterObligation | undefined, stepCount: number): NodeState[] {
  const pending = Array.from({ length: stepCount }, () => "pending" as NodeState);
  if (!w) return pending;
  const sop = w.sop_task_state;
  const proof = w.proof_state;
  const done = [
    true, // config rule published — a live workflow exists
    true, // obligation generated — it exists
    sop !== "not_started" || proof === "uploaded" || proof === "accepted" || w.completed_count > 0, // drive opened
    sop === "submitted" || sop === "accepted", // SOP task
    proof === "uploaded" || proof === "accepted", // proof uploaded
    w.verification_state === "accepted", // verification
    w.completion_state === "completed", // completion posted
  ];
  let doneThrough = 0;
  while (doneThrough < done.length && done[doneThrough]) doneThrough++;
  const blocked = Boolean(w.blocker_reason) || w.owner_state === "missing" || ["blocked", "rejected"].includes(w.work_state);
  return pending.map((_, i) => {
    if (i < doneThrough) return "done";
    if (i === doneThrough) return blocked ? "blocked" : "cur";
    if (i === doneThrough + 1) return "next";
    return "pending";
  });
}

function hrefPreservingWorkflowPage(
  pathname: string,
  params: RouteSearchParams,
  workflowId?: string,
  overrides: Record<string, string | string[] | null | undefined> = {},
): string {
  return hrefWithParams(pathname, params, workflowId ? { ...overrides, workflow: workflowId } : overrides, ["workflow"]);
}

const ACTIVE_STATES: WorkState[] = ["in_progress", "scheduled"];
const BLOCKED_STATES: WorkState[] = ["blocked", "proof_pending", "rejected"];
const PATH = "/workflows";

export async function VaccinationWorkflowsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const selectedWorkflowId = one(sp, "workflow");
  const workflowCursor = one(sp, "wf_cursor");
  const hasWorkflowCursorStack = Boolean(sp.wf_cursor_stack);
  const workflowCursorStackValues = Array.isArray(sp.wf_cursor_stack) ? sp.wf_cursor_stack.filter(Boolean) : sp.wf_cursor_stack ? [sp.wf_cursor_stack] : undefined;

  const pageSizeOptions = tablePageSizes(pageContract, "workflow-catalog");
  const requestedPageSize = pageSizeOptions.find((size) => size === boundedInt(one(sp, "wf_limit"), 10, 1, 100)) ?? 10;
  const requestedPage = boundedInt(one(sp, "wf_page"), 1, 1, 1000000);
  const [countsResult, result, stages] = await Promise.all([
    getVaccinationActionCenterCounts({ parkId, asOf }),
    getVaccinationActionCenter({ parkId, asOf, cursor: workflowCursor, limit: requestedPageSize }),
    listAnimalStages(),
  ]);
  const stageNames = stageNameMap(stages.ok ? stages.data.items : undefined);
  const totalFromBackend = countsResult.ok ? countsResult.data.total_count : 0;
  const rows: ActionCenterObligation[] = result.ok ? listOrEmpty(result.data.items) : [];
  const nextCursor = result.ok ? result.data.next_cursor : undefined;
  if (requestedPage > 1 && !workflowCursor && !hasWorkflowCursorStack) {
    redirect(hrefWithPagedCursor(PATH, sp, "wf_cursor", null, "wf_page", "wf_cursor_stack") || scopeHref(PATH, scope, {}, { wf_page: "1", wf_cursor: undefined, wf_cursor_stack: undefined }));
  }
  const start = totalFromBackend === 0 ? 0 : (requestedPage - 1) * requestedPageSize + 1;
  const end = totalFromBackend === 0 ? 0 : Math.min(totalFromBackend, start + rows.length - 1);
  const paged = {
    page: requestedPage,
    pageSize: requestedPageSize,
    total: totalFromBackend,
    start,
    end,
  };
  const selectedWorkflow = selectedWorkflowId ? rows.find((row) => row.row_id === selectedWorkflowId) : undefined;
  const selectedWorkflowMissing = Boolean(selectedWorkflowId && !selectedWorkflow);
  const activeWorkflow = selectedWorkflowId ? selectedWorkflow : rows[0];
  const steps = chainSteps(pageContract);
  const catalogLabels = tableLabels(pageContract, "workflow-catalog");
  const chainNodeStates = chainStates(activeWorkflow, steps.length);
  const listHref = hrefPreservingWorkflowPage(PATH, sp);
  const actionCenterHref = activeWorkflow
    ? scopeHref("/action-center", scope, {}, { ac_row: activeWorkflow.row_id })
    : scopeHref("/action-center", scope);

  // Server-authoritative counts for the KPI tiles (not the capped page).
  const counts = new Map<WorkState, number>();
  if (countsResult.ok) for (const c of listOrEmpty(countsResult.data.counts_by_work_state)) counts.set(c.work_state, c.count);
  const sum = (states: WorkState[]) => states.reduce((n, s) => n + (counts.get(s) ?? 0), 0);
  const totalWorkflows = totalFromBackend;
  const activeRuns = sum(ACTIVE_STATES);
  const blocked = sum(BLOCKED_STATES);
  const withExpected = rows.filter((r) => r.expected_count > 0);
  const avgProgress = withExpected.length
    ? Math.round(withExpected.reduce((a, r) => a + (r.completed_count / r.expected_count) * 100, 0) / withExpected.length)
    : 0;
  function pagerHref(page: number): string {
    if (page > paged.page && nextCursor) {
      return hrefWithPagedCursor(PATH, sp, "wf_cursor", nextCursor, "wf_page", "wf_cursor_stack") ?? scopeHref(PATH, scope);
    }
    if (page < paged.page) {
      return hrefPreviousPagedCursor(PATH, sp, "wf_cursor", "wf_page", "wf_cursor_stack") ?? scopeHref(PATH, scope);
    }
    return hrefPreservingWorkflowPage(PATH, sp, selectedWorkflowId, {
      wf_page: String(page),
      wf_limit: String(paged.pageSize),
      wf_cursor: workflowCursor,
      wf_cursor_stack: workflowCursorStackValues,
    });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return scopeHref(PATH, scope, {}, { wf_page: "1", wf_limit: String(pageSize), wf_cursor: undefined, wf_cursor_stack: undefined });
  }


  // Breadcrumb trail. The parent segment is the contract's own `crumb` copy -- rendered only when
  // the backend actually supplies one AND it is not just the page title again, which is what the
  // old `<div className="crumb"><b>{title}</b></div>` rendered on every route in this module. No
  // section name is invented here: a missing key means a single muted current segment, and the
  // real two-level trail lands the moment the contract carries the section label.
  const crumbSection = optionalCopy(pageContract, "crumb");
  const crumbItems: PageCrumb[] = [
    ...(crumbSection && crumbSection !== pageContract.title ? [{ label: crumbSection, href: "/" }] : []),
    { label: pageContract.title },
  ];

  const locationOf = (row: ActionCenterObligation) =>
    row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label });
  const timeline: OrderHistoryItem[] = steps.map((step, index) => {
    const state = chainNodeStates[index];
    return {
      key: step.key,
      title: step.title,
      body: step.detail,
      tone: NODE_TONE[state],
      time: state === "cur" ? copy(pageContract, "label.you_are_here") : state === "next" ? copy(pageContract, "label.next_upper") : undefined,
    };
  });
  const activePct = activeWorkflow && activeWorkflow.expected_count > 0 ? Math.round((activeWorkflow.completed_count / activeWorkflow.expected_count) * 100) : 0;

  return (
    <PageRoot>
      <PageHeader title={pageContract.title} crumbs={crumbItems} />

      {!countsResult.ok || !result.ok ? (
        <Alert severity="error">
          {(() => {
            const error = !countsResult.ok ? countsResult.error : !result.ok ? result.error : undefined;
            return error ? <><b>{error.code ?? error.kind}</b>&nbsp;{error.message}</> : null;
          })()}
        </Alert>
      ) : null}

      <UrlSuspense searchParams={sp} watch={SCOPE_WATCH} fallback={<KpiRowSkeleton count={4} />}>
      <WorkflowsKpis
        items={[
          { key: "workflows", title: copy(pageContract, "label.workflows"), total: totalWorkflows, caption: copy(pageContract, "label.vaccination_chain") },
          { key: "active", title: copy(pageContract, "label.active_runs"), total: activeRuns, caption: copy(pageContract, "label.scheduled_in_progress") },
          { key: "blocked", title: copy(pageContract, "label.blocked_gated"), total: blocked, caption: copy(pageContract, "label.awaiting_proof_owner") },
          { key: "progress", title: copy(pageContract, "label.avg_progress"), total: avgProgress, caption: `% · ${copy(pageContract, "label.across_shown")}` },
        ]}
      />
      </UrlSuspense>

      <Grid container spacing={3}>
        {/* Workflow catalog on the template order-list anatomy: Tabs with the Label count, the
            toolbar (visible-row search + Filters drawer), the Scrollbar table, the pager footer. */}
        <Grid size={{ xs: 12, lg: 8 }}>
          <Card className="wfcat" id="wfcat" data-filter-scope>
            <TemplateTabs
              ariaLabel={copy(pageContract, "filter.domains.aria")}
              value="all"
              sx={{ px: { md: 2.5 } }}
              items={[{ value: "all", label: copy(pageContract, "label.all_domains"), count: totalWorkflows, href: scopeHref(PATH, scope) }]}
            />
            <OrderTableToolbar
              search={<Box sx={orderToolbarSearchSx}><Box sx={{ display: "flex" }}><VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_label")} /></Box></Box>}
              trailing={
                <VaccinationFilterButton
                  pageContract={pageContract}
                  title={copy(pageContract, "filter.drawer.title")}
                  searchReason={copy(pageContract, "filter.search_reason")}
                  filterReason={copy(pageContract, "filter.reason")}
                  rowsLabel={`${paged.start}-${paged.end} of ${totalWorkflows} rows · ${copy(pageContract, "filter.rows_suffix")}`}
                  actionHref={scopeHref("/action-center", scope)}
                  actionLabel={copy(pageContract, "action.open_action_center")}
                  facets={catalogLabels}
                />
              }
            />
            {/* Catalog rows + pager (guard: url-keyed-panel): a page / rows-per-page click swaps them to
                their skeleton at once; picking a row only refreshes the right rail. */}
            <UrlSuspense searchParams={sp} watch={CATALOG_WATCH} fallback={<TableSkeleton bare header={false} columns={catalogLabels.length || 5} rows={requestedPageSize} />}>
            <Scrollbar>
              <Table sx={{ minWidth: 680 }} aria-label={copy(pageContract, "section.catalog.title")}>
                <TableHeadCustom headCells={catalogLabels.map((label, index) => ({ id: `c${index}`, label }))} />
                <TableBody>
                  {rows.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={catalogLabels.length}>
                        <EmptyContent filled title={result.ok ? copy(pageContract, "empty.catalog") : copy(pageContract, "empty.unavailable")} sx={{ py: 10 }} />
                      </TableCell>
                    </TableRow>
                  ) : (
                    rows.map((row) => {
                      const title = actionWorkTitle(pageContract, row);
                      const pct = row.expected_count > 0 ? Math.round((row.completed_count / row.expected_count) * 100) : 0;
                      const isActive = activeWorkflow?.row_id === row.row_id;
                      // The drive leads the second line (A7, pr294): two "Assign operator — Godel 2 - Part 2"
                      // rows are two different drives' work in one pen, and read as duplicates without it.
                      const where = [actionDriveLabel(pageContract, row), row.park_name, locationOf(row)].filter(Boolean).join(" · ");
                      const progressText = row.expected_count > 0 ? `${row.completed_count}/${row.expected_count} ${copy(pageContract, "label.done_suffix")}` : null;
                      return (
                        <TableRow key={row.row_id} hover selected={isActive}>
                          <TableCell sx={{ minWidth: 220 }}>
                            <Box
                              component={Link}
                              href={hrefPreservingWorkflowPage(PATH, sp, row.row_id)}
                              scroll={false}
                              className="wfrow"
                              aria-current={isActive ? "true" : undefined}
                              aria-label={`${copy(pageContract, "action.open_record")} ${title}`}
                              title={`${title} · ${where} · ${optionLabel(pageContract, "work_state_filter_chips", row.work_state)}`}
                              sx={{ color: "inherit", textDecoration: "none", display: "block" }}
                            >
                              <Box component="span" sx={{ display: "flex", alignItems: "center", gap: 2 }}>
                              <Box component="span" sx={{ width: 10, height: 10, borderRadius: "50%", flexShrink: 0, bgcolor: TONE_SWATCH[optionTone(pageContract, "severity_chips", row.severity) as Tone] }} />
                              <Box component="span" sx={{ minWidth: 0, flexGrow: 1 }}>
                                <Box component="span" sx={{ display: "block", typography: "subtitle2" }}>{title}</Box>
                                <Box component="span" sx={{ display: "block", typography: "caption", color: "text.disabled", mt: 0.5 }}>{where}</Box>
                                {/* The bar carries its value beside it (A7): an all-grey 0% bar read as "no data". */}
                                {progressText ? (
                                  <Box component="span" sx={{ mt: 1, display: "flex", alignItems: "center", gap: 1 }}>
                                    <LinearProgress variant="determinate" value={pct} sx={{ flex: "1 1 auto", height: 4, maxWidth: 200 }} aria-hidden="true" />
                                    <Box component="span" sx={{ typography: "caption", color: "text.secondary", whiteSpace: "nowrap", fontVariantNumeric: "tabular-nums" }}>{progressText}</Box>
                                  </Box>
                                ) : null}
                              </Box>
                              </Box>
                            </Box>
                          </TableCell>
                          <TableCell sx={{ whiteSpace: "nowrap" }}>{stageWords(row.animal_stage, stageNames)}</TableCell>
                          <TableCell sx={{ whiteSpace: "nowrap" }}>{row.owner?.operator_name || "—"}</TableCell>
                          <TableCell sx={{ minWidth: 140, color: "text.secondary", typography: "body2" }}>{row.next_action}</TableCell>
                          <TableCell>
                            <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", row.work_state)}</Tag>
                          </TableCell>
                        </TableRow>
                      );
                    })
                  )}
                </TableBody>
              </Table>
            </Scrollbar>
            {rows.length > 0 ? (
              <VaccinationTablePager
                pageContract={pageContract}
                pageSizeOptions={pageSizeOptions}
                page={paged.page}
                pageSize={paged.pageSize}
                total={paged.total}
                start={paged.start}
                end={paged.end}
                noun={copy(pageContract, "label.workflows")}
                hrefForPage={pagerHref}
                hrefForPageSize={pageSizeHref}
              />
            ) : null}
            </UrlSuspense>
          </Card>
        </Grid>

        {/* Right rail (template order details): the selected workflow's chain on the
            OrderDetailsHistory timeline, then its state card with the next steps. */}
        <Grid size={{ xs: 12, lg: 4 }}>
          <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<StackSkeleton spacing={3}><DetailCardSkeleton rows={6} /><DetailCardSkeleton rows={4} /></StackSkeleton>}>
          <Stack spacing={3}>
            <OrderDetailsHistory
              title={copy(pageContract, "section.chain.title")}
              aria-label={copy(pageContract, "section.chain.aria")}
              action={
                activeWorkflow ? (
                  <Tag tone={optionTone(pageContract, "work_state_filter_chips", activeWorkflow.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", activeWorkflow.work_state)}</Tag>
                ) : undefined
              }
              timeline={timeline}
            />

            <Card>
              <CardHeader
                title={activeWorkflow ? actionDriveLabel(pageContract, activeWorkflow) : pageContract.title}
                subheader={activeWorkflow ? `${activeWorkflow.park_name} · ${locationOf(activeWorkflow)}` : pageContract.subtitle}
                action={
                  selectedWorkflowId ? (
                    <LinkButton href={listHref} replace scroll={false} size="small" color="inherit" startIcon={<Iconify icon="eva:arrow-ios-back-fill" />}>
                      {copy(pageContract, "action.back_to_list")}
                    </LinkButton>
                  ) : undefined
                }
              />
              <Stack spacing={2} sx={{ p: 3 }}>
                {selectedWorkflowMissing ? (
                  <Alert severity="warning">{copy(pageContract, "empty.unavailable")}</Alert>
                ) : activeWorkflow ? (
                  <>
                    <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
                      <Tag tone={optionTone(pageContract, "severity_chips", activeWorkflow.severity) as Tone}>{optionLabel(pageContract, "severity_chips", activeWorkflow.severity)}</Tag>
                      <Tag tone={optionTone(pageContract, "sop_state_chips", activeWorkflow.sop_task_state) as Tone}>{optionLabel(pageContract, "sop_state_chips", activeWorkflow.sop_task_state)}</Tag>
                      <Tag tone={optionTone(pageContract, "proof_state_chips", activeWorkflow.proof_state) as Tone}>{optionLabel(pageContract, "proof_state_chips", activeWorkflow.proof_state)}</Tag>
                      <Tag tone={optionTone(pageContract, "verification_state_chips", activeWorkflow.verification_state) as Tone}>{optionLabel(pageContract, "verification_state_chips", activeWorkflow.verification_state)}</Tag>
                    </Box>
                    <Box>
                      <Box sx={{ display: "flex", justifyContent: "space-between", typography: "body2", mb: 1 }}>
                        <Box component="span" sx={{ color: "text.secondary" }}>{activeWorkflow.completed_count}/{activeWorkflow.expected_count} {copy(pageContract, "label.done")}</Box>
                        <Box component="span" sx={{ typography: "subtitle2" }}>{activePct}%</Box>
                      </Box>
                      <LinearProgress variant="determinate" value={activePct} />
                    </Box>
                  </>
                ) : null}
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {activeWorkflow ? (
                    <>
                      {copy(pageContract, "label.chain_note_selected")} <b>{locationOf(activeWorkflow)}</b>.
                    </>
                  ) : rows.length ? (
                    copy(pageContract, "label.chain_note_open")
                  ) : (
                    copy(pageContract, "label.chain_note_empty")
                  )}
                </Typography>
                {activeWorkflow ? (
                  <Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap" }}>
                    <LinkButton href={actionCenterHref} scroll={false} variant="contained" endIcon={<Iconify icon="eva:arrow-ios-forward-fill" />}>
                      {copy(pageContract, "action.open_action_center")}
                    </LinkButton>
                    <LinkButton href={scopeHref(`/workflows/${encodeURIComponent(activeWorkflow.row_id)}`, scope)} variant="outlined" color="inherit">
                      {copy(pageContract, "action.open_record")}
                    </LinkButton>
                  </Box>
                ) : null}
              </Stack>
            </Card>
          </Stack>
          </UrlSuspense>
        </Grid>
      </Grid>
    </PageRoot>
  );
}

/** The top-bar scope the counts read. */
const SCOPE_WATCH = ["park", "scope_mode", "as_of"] as const;
/** The catalog read's params: scope + the keyset pager (not the selected workflow). */
const CATALOG_WATCH = [...SCOPE_WATCH, "wf_cursor", "wf_cursor_stack", "wf_page", "wf_limit"] as const;

// Chain node state -> template timeline dot colour.
const NODE_TONE: Record<NodeState, OrderHistoryTone> = {
  done: "success",
  cur: "primary",
  next: "info",
  pending: "grey",
  blocked: "error",
};
