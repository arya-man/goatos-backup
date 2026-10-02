import Box from "@mui/material/Box";
import { FEED_DEFAULT_PAGE_SIZE as DEFAULT_PAGE_SIZE, FEED_KPI_SIZE, FEED_TABLE_COLUMNS } from "./feed-layout";
import { UrlSuspense } from "@/components/app/url-suspense";
import { KpiRowSkeleton } from "@/components/app/skeletons";
import { TableSkeleton } from "@/components/app/skeletons";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { Iconify } from "@/components/minimal/iconify";

import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { InfoHint } from "@/components/app/info-hint";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import AlertTitle from "@mui/material/AlertTitle";
import TableContainer from "@mui/material/TableContainer";
import { Label } from "@/components/minimal/label";
import { EmptyContent } from "@/components/minimal/empty-content";
import { KpiWidget } from "@/components/app/kpi-widget";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getFeedDirectionPreview,
  listFeedConfigSessionTemplates,
  type FeedDirectionPreviewPage,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { operationalLocationLabel } from "@/lib/operational-location";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedLifecycleBanner, isLifecycleEmpty } from "./feed-lifecycle";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import {
  FeedItemStatusTag,
  FeedOverdueShiftingChip,
  FeedQuantityCell,
  FeedWorkflowTag,
} from "./feed-quantity";
import { isNothingToFeed, visibleOperationalFeedItems } from "./feed-quantity-state";
import { feedHref, feedLimit, feedOffset, resolveFeedScope } from "./feed-scope";
import { stageLabel } from "@/lib/stage-labels";
import { fmtKg } from "@/lib/format";
import Alert from "@mui/material/Alert";

// Feed -> Feed Direction. The generated feed sheet for ONE park and ONE Asia/Kolkata business day:
// projected head count x authored grams per head x shed factor, split across the park's sessions.
//
// Three things on this screen are easy to get wrong and expensive when wrong:
//
//  1. BLOCKED IS NOT ZERO. Handled once, in feed-quantity.tsx — read the header there before
//     touching any quantity rendering. A blocked cell has no number and must never acquire one.
//     This screen also HIDES configured-zero item lines (`visibleOperationalFeedItems`): a line
//     reading "0.000 kg RGS Concentrate" is an instruction to do nothing, printed among the
//     instructions to do something. Blocked lines are never hidden — the filter keys off the
//     `configured_zero` class, not off "there is no number to show", which is the mistake that would
//     take blocked with it. The omission is disclosed under the table so an absent line can only be
//     read as a deliberate zero.
//
//  2. THE HEAD COUNT IS A PROJECTION, NOT A CENSUS. It is the live herd PLUS approved movements that
//     are already feed-effective for the selected day. It is not how many animals are standing in the
//     shed right now. The column is therefore labelled as projected, and where the projection has
//     drifted from reality (an approved movement whose feed-effective date passed with the animals
//     still not moved) the row carries the overdue-movement chip instead of quietly absorbing it.
//
//  3. EXPERIMENT SHEDS DO NOT MULTIPLY BY HEAD COUNT. An experiment row's quantity is hand-entered
//     absolute kg for the whole shed, and `head_count_informational` says so. Presenting that head
//     count the same way as a normal row's invites someone to multiply it and overfeed the shed by a
//     factor of its whole population.
//
// Pagination is by SHED, so a shed's sessions never straddle a page boundary and every
// `session_total_kg` on screen is complete.

const PAGE_PATH = "/feed/direction";
// KPI tiles with no day series: the template widget draws no sparkline under two points.

/**
 * One table line per VISIBLE (row, feed item). Shed-level cells span the shed's visible item lines.
 *
 * Counted from the visible items, not `row.items` — spanning the unfiltered count would leave the
 * shed/breed/session cells stretching over rows that are no longer rendered, pulling the table apart.
 */
function itemLineCount(visibleItems: readonly unknown[]): number {
  return Math.max(1, visibleItems.length);
}

export async function FeedDirectionPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};

  const locations = await getCensusLocations();
  const scope = resolveFeedScope(sp, "fd_park", "fd_date", locations.parks);
  const shedId = (sp.fd_shed as string | undefined) || "";
  const sessionRaw = (sp.fd_session as string | undefined) || "";

  const pageSizeOptions = tablePageSizes(pageContract, "direction-rows");
  const limit = feedLimit(sp, "fd_limit", pageSizeOptions, DEFAULT_PAGE_SIZE);
  const offset = feedOffset(sp, "fd_offset");

  // The park's own session template supplies the session filter vocabulary. Sourcing it from the
  // returned rows instead would collapse the dropdown to whatever is already selected — pick Morning
  // and Evening disappears, leaving no way back.
  const [previewResult, sessionTemplates] = await Promise.all([
    scope.parkId
      ? getFeedDirectionPreview({
          park_id: scope.parkId,
          target_date: scope.targetDate,
          shed_id: shedId || undefined,
          session: sessionRaw ? Number(sessionRaw) : undefined,
          limit,
          offset,
        })
      : Promise.resolve(null),
    scope.parkId ? listFeedConfigSessionTemplates({ park_id: scope.parkId, limit: 50 }) : Promise.resolve(null),
  ]);

  const authError = firstAuthRequiredError(previewResult, sessionTemplates);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const preview: FeedDirectionPreviewPage | null =
    previewResult && previewResult.ok ? previewResult.data : null;
  const rows = preview?.items ?? [];
  const summary = preview?.summary;
  const lifecycle = preview?.lifecycle;
  const hasFilter = Boolean(shedId || sessionRaw);
  // A served day whose empty table is EXPLAINED by the lifecycle (nothing was issued) shows the
  // banner AS the content instead of an empty grid. A filter that merely excludes rows from a sheet
  // that DOES exist keeps the normal "no rows match these filters" state, so the filter guard stands.
  const lifecycleEmpty = lifecycle ? isLifecycleEmpty(lifecycle, rows.length) && !hasFilter : false;

  const cols = tableLabels(pageContract, "direction-rows");

  const filterFields: FeedFilterField[] = [
    {
      kind: "date",
      param: "fd_date",
      label: copy(pageContract, "filter.date_label"),
      value: scope.targetDate,
      // Bound to [today, tomorrow]: beyond that the projected counts are unknown, so the backend
      // refuses to generate a sheet. Preventing the pick is the honest, no-fabrication guard.
      min: scope.minDate,
      max: scope.maxDate,
    },
    {
      kind: "select",
      param: "fd_park",
      label: copy(pageContract, "filter.park_label"),
      value: scope.parkId,
      // A park is mandatory for this endpoint, so there is no "All" option. Disabled (not hidden)
      // when the top bar already owns park scope: the mock's rule is disable-with-reason, and hiding
      // it would make the control appear to come and go.
      allowAll: false,
      disabledReason: scope.parkLockedByTopBar ? copy(pageContract, "filter.scope_readonly") : undefined,
      // The park CODE, as every row and chip on this page names the park ("CPT", not "Channapatna").
    options: locations.parks.map((park) => ({ value: park.id, label: park.code || park.name })),
    },
    {
      kind: "select",
      param: "fd_shed",
      label: copy(pageContract, "filter.shed_label"),
      value: shedId,
      options: locations.sheds
        .filter((shed) => !scope.parkId || shed.parentId === scope.parkId)
        .map((shed) => ({ value: shed.id, label: shed.name })),
    },
    {
      kind: "select",
      param: "fd_session",
      label: copy(pageContract, "filter.session_label"),
      value: sessionRaw,
      options: (sessionTemplates && sessionTemplates.ok ? listOrEmpty(sessionTemplates.data.items) : []).map((template) => ({
        value: String(template.session_no),
        label: template.session_label,
      })),
    },
  ];

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <FeedFaroView
        routeId={pageContract.route_id}
        parkId={scope.parkId}
        targetDate={scope.targetDate}
        blockedCells={summary?.blocked_count}
      />

      <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "section.direction.title") }]} />

      {/* An API failure surfaces as a visible error band. Swallowing it into an empty table would
          read to an operator as "nothing to feed today", which is the worst possible misreading. */}
      {previewResult && !previewResult.ok ? (
        <Alert severity="error">
          <AlertTitle>{copy(pageContract, "state.direction_unavailable")}</AlertTitle>
          {previewResult.error.code ?? previewResult.error.kind}&nbsp;{previewResult.error.message}
        </Alert>
      ) : null}

      {/* Always rendered. The API summary is WHOLE-SCOPE (`summary.scope === "filtered"`) and
          invariant to limit/offset, so these figures are the day's real totals on every page.
          Template CourseWidgetSummary (KpiWidget) tiles above the list card (invoice-list analytic row). */}
      {/* Shown for a not-yet-issued day too (zero counts are a reading; the tiles no longer vanish
          and pull the card up under a skeleton that drew them; guard: feed-direction-loading-mirror). */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={PAGER_PARAMS} fallback={<KpiRowSkeleton count={2} hint size={FEED_KPI_SIZE} />}>
      {summary ? (
        <Grid container spacing={3}>
          <Grid size={FEED_KPI_SIZE}>
            <KpiWidget
              title={copy(pageContract, "kpi.sheds.label")}
              total={summary.shed_count}
              caption={copy(pageContract, "kpi.sheds.sub")}
              sx={{ height: 1 }}
            />
          </Grid>
          <Grid size={FEED_KPI_SIZE}>
            <KpiWidget
              title={copy(pageContract, "kpi.blocked.label")}
              total={summary.blocked_count}
              caption={summary.blocked_count > 0 ? copy(pageContract, "kpi.blocked.sub") : copy(pageContract, "empty.blocked")}
              sx={{ height: 1 }}
            />
          </Grid>
        </Grid>
      ) : null}
      </UrlSuspense>

      {/* Template order-list anatomy: one Card holding the header, the filter toolbar, the day's
          lifecycle banner, the per-item day totals, the sheet table and its pager. overflow visible:
          the filter bar's pickers must not be clipped by the card edge. */}
      <Card sx={{ overflow: "visible" }}>
        <CardHeader
          title={copy(pageContract, "section.direction.title")}
          subheader={copy(pageContract, "section.direction.caption")}
          action={summary && !lifecycleEmpty ? <InfoHint text={copy(pageContract, "section.summary.note")} /> : null}
        />
        <Box sx={{ px: 1, pt: 1 }}>
          <FeedFilters
            basePath={PAGE_PATH}
            pageParam="fd_offset"
            fields={filterFields}
            pageContract={pageContract}
          />
        </Box>

        {/* The sheet (guard: url-keyed-panel): a filter / page change swaps it to its skeleton at
            once; header and filters stay on screen. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<TableSkeleton bare header={false} columns={FEED_TABLE_COLUMNS} rows={limit} />}>
        {/* The issue -> amend -> lock status of the served park-day. For a not-yet-issued day this
            banner IS the content: the summary/table below are suppressed so the operator sees the
            explanation, not a blank grid that reads as "nothing to feed". */}
        {lifecycle ? (
          <Box sx={{ px: 3, pb: 2 }}>
            <FeedLifecycleBanner lifecycle={lifecycle} feedDay={scope.targetDate} pageContract={pageContract} />
          </Box>
        ) : null}

        {summary && !lifecycleEmpty && summary.total_kg_by_feed_item.length > 0 ? (
          <Stack direction="row" sx={{ flexWrap: "wrap", gap: 1, px: 3, pb: 2 }}>
            {summary.total_kg_by_feed_item.map((total) => (
              <Label
                key={total.feed_item}
                variant="soft"
                color={total.blocked_cells > 0 ? "warning" : "default"}
                title={total.blocked_cells > 0 ? copy(pageContract, "label.blocked_note") : undefined}
              >
                {total.feed_item} · {fmtKg(total.quantity_kg)} {copy(pageContract, "label.kg_noun")}
              </Label>
            ))}
          </Stack>
        ) : null}

        {lifecycleEmpty ? (
          <Box sx={{ px: 3, pb: 3 }}>
            {/* One glyph + one line: the card's own caption already names the grain above. */}
            <EmptyState title={copy(pageContract, "empty.direction")} icon={<Iconify icon="solar:bill-list-bold" />} />
          </Box>
        ) : null}

        {!lifecycleEmpty ? (
        <>
        <TableContainer
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.direction.aria")}
        >
          <Table sx={{ minWidth: 1000, "& td": { verticalAlign: "top" } }} aria-label={copy(pageContract, "table.direction.aria")}>
            <TableHead>
              <TableRow>
                {cols.map((col) => (
                  <TableCell component="th" key={col}>{col}</TableCell>
                ))}
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={cols.length} sx={{ p: 0 }}>
                    <EmptyContent
                      sx={{ py: 5 }}
                      title={
                        !previewResult || previewResult.ok
                          ? hasFilter
                            ? copy(pageContract, "empty.direction_filtered")
                            : copy(pageContract, "empty.direction")
                          : copy(pageContract, "state.direction_unavailable")
                      }
                    />
                  </TableCell>
                </TableRow>
              ) : (
                rows.flatMap((row) => {
                  // Configured zeros drop out here. Blocked items are NOT touched by this filter.
                  const visibleItems = visibleOperationalFeedItems(row.items);
                  // Every item this shed has was authored at 0 — a real, explainable state that must
                  // render as a stated "nothing to feed", never as an empty hole in the table.
                  const nothingToFeed = isNothingToFeed(row.items);
                  const span = itemLineCount(visibleItems);
                  // The row's real identity: one operational location, one session, one workflow.
                  // Keyed on the descriptive columns instead (`ration_group|breed`), two partitions of
                  // one shed holding the same mix produced the SAME React key, and those columns are
                  // now backend-joined summaries of a pen rather than the thing that separates rows.
                  const rowKey = `${row.shed_id}|${row.partition_label ?? ""}|${row.workflow}|${row.session_no}`;
                  const items = visibleItems.length > 0 ? visibleItems : [null];

                  return items.map((item, index) => (
                    <TableRow key={`${rowKey}|${item ? item.feed_item : "none"}`}>
                      {index === 0 ? (
                        <>
                          {/* No park cell: this endpoint is single-park by contract (park_id is
                              required), so the value is constant for the whole page and is named
                              once by the Park filter above. See the contract comment in
                              adminui/app/service.go pages(). */}
                          <TableCell rowSpan={span}>
                            <Stack spacing={0.5}>
                              <Typography variant="subtitle2">
                                {row.operational_location_display || operationalLocationLabel({ shedName: row.shed_label, partitionLabel: row.partition_label })}
                              </Typography>
                              <Stack direction="row" sx={{ gap: 0.625, flexWrap: "wrap" }}>
                                <FeedWorkflowTag workflow={row.workflow} pageContract={pageContract} />
                                {row.overdue_pending ? <FeedOverdueShiftingChip pageContract={pageContract} /> : null}
                              </Stack>
                            </Stack>
                          </TableCell>
                          {/* Always the ANIMALS' management stage, on every workflow — the experiment
                              arm has its own column now. Multi-stage sheds arrive pre-joined. */}
                          <TableCell sx={{ color: "text.secondary" }} rowSpan={span}>
                            {stageLabel(row.shed_tag) || copy(pageContract, "label.placeholder")}
                          </TableCell>
                          {/* Breed only. The ration group used to print as a sub-line here, but on a
                              feed sheet it is derivable noise: the operator reads the breed at the
                              shed door, and the group is an internal lookup key that is empty on
                              every experiment row anyway. It stays visible on /feed/config, which is
                              where the breed → group merge is actually authored. A multi-breed shed
                              arrives already joined into one label by the backend; the frontend does
                              not decide how a mixed shed is named. */}
                          <TableCell rowSpan={span}>{row.breed || copy(pageContract, "label.placeholder")}</TableCell>
                          <TableCell sx={{ color: "text.secondary" }} rowSpan={span}>
                            {row.session_label}
                          </TableCell>
                          {/* Projected, not census. `head_count_informational` marks the experiment
                              case, whose kg is already a shed total and must not be multiplied. */}
                          <TableCell rowSpan={span}>
                            <Stack spacing={0.25}>
                              <Typography
                                variant="subtitle2"
                                component="span"
                                sx={{ fontVariantNumeric: "tabular-nums" }}
                                title={copy(pageContract, "label.projected_count_note")}
                              >
                                {row.head_count}
                              </Typography>
                              <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>
                                {row.head_count_informational
                                  ? copy(pageContract, "label.workflow_experiment")
                                  : copy(pageContract, "label.projected_count")}
                              </Typography>
                            </Stack>
                          </TableCell>
                        </>
                      ) : null}

                      {item ? (
                        <>
                          {/* `feed-wrap` is layout only (frontend-owned): the feed item is a long
                              multi-word LABEL, and holding it on one line is what pushed the
                              session total off the right edge of the card. It word-wraps; it is
                              never broken mid-token. */}
                          <TableCell sx={{ minWidth: 150 }}>{item.feed_item}</TableCell>
                          <TableCell>
                            <FeedQuantityCell item={item} pageContract={pageContract} />
                          </TableCell>
                        </>
                      ) : nothingToFeed ? (
                        /* Every item was a configured zero. Say so across the item + quantity columns
                           rather than leaving two placeholder dashes, which would read as missing
                           data — the one meaning this shed's state is NOT. */
                        <TableCell sx={{ color: "text.secondary", minWidth: 150 }} colSpan={2}>
                          {copy(pageContract, "empty.nothing_to_feed")}
                        </TableCell>
                      ) : (
                        <>
                          <TableCell sx={{ color: "text.secondary", minWidth: 150 }}>{copy(pageContract, "label.placeholder")}</TableCell>
                          <TableCell sx={{ color: "text.secondary" }}>{copy(pageContract, "label.placeholder")}</TableCell>
                        </>
                      )}

                      {index === 0 ? (
                        <TableCell rowSpan={span} align="right" sx={{ minWidth: 132 }}>
                          {/* Sum of the RESOLVED items only. When the row is blocked this total is
                              partial by construction, and saying so is the whole point — the number
                              is not what the shed needs, it is what we know how to give it. */}
                          <Stack spacing={0.5} sx={{ alignItems: "flex-end" }}>
                            <Typography
                              variant="subtitle2"
                              component="span"
                              sx={{ fontVariantNumeric: "tabular-nums", color: "primary.main" }}
                              title={copy(pageContract, "label.session_split_note")}
                            >
                              {fmtKg(row.session_total_kg)} {copy(pageContract, "label.kg_noun")}
                            </Typography>
                            {row.blocked ? (
                              // One row now covers a whole pen, so "blocked" alone no longer says WHICH
                              // part of it is unauthored — the backend names each gap and they are shown
                              // here. The operator feeds what IS configured; this is how they see what
                              // is missing and where to close it. Backend-owned wording, joined only.
                              <Label
                                variant="soft"
                                color="error"
                                title={
                                  row.blocked_reasons?.length
                                    ? row.blocked_reasons.map((reason) => reason?.detail ?? "").filter(Boolean).join("\n")
                                    : copy(pageContract, "label.blocked_note")
                                }
                              >
                                {copy(pageContract, "label.blocked_short")}
                              </Label>
                            ) : null}
                          </Stack>
                        </TableCell>
                      ) : null}

                      <TableCell>
                        {item ? (
                          <FeedItemStatusTag item={item} pageContract={pageContract} />
                        ) : (
                          <Box component="span" sx={{ color: "text.secondary" }}>{copy(pageContract, "label.placeholder")}</Box>
                        )}
                      </TableCell>
                    </TableRow>
                  ));
                })
              )}
            </TableBody>
          </Table>
        </TableContainer>

        {/* The omission is disclosed once, under the table it applies to — not per row. Without it a
            reader who expects an item and cannot find it has no way to tell "authored as 0" from
            "dropped", and those have opposite consequences. */}
        <Box sx={{ display: "flex", justifyContent: "flex-end", px: 2.5, pt: 1.5 }}>
          <InfoHint text={copy(pageContract, "label.zero_items_omitted")} />
        </Box>

        <FeedPager
          pageContract={pageContract}
          offset={offset}
          limit={limit}
          rowCount={rows.length}
          // items are a page of PENS (a pen's grains never straddle a page): the range counts pens.
          pageUnits={new Set(rows.map((row) => row.shed_id)).size}
          hasMore={preview?.has_more ?? false}
          noun={copy(pageContract, "table.direction.noun")}
          pageSizeOptions={pageSizeOptions}
          hrefForOffset={(next) => feedHref(PAGE_PATH, sp, "fd_offset", String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, sp, "fd_limit", String(next))}
        />
        </>
        ) : null}
        </UrlSuspense>
      </Card>
    </Stack>
  );
}

/** The sheet's pager: paging never changes the whole-scope KPI tiles. */
const PAGER_PARAMS = ["fd_offset", "fd_limit"] as const;
