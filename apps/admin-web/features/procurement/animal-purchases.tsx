import Table from "@mui/material/Table";
import { DividedStack } from "@/components/app/divided-stack";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { KpiWidget } from "@/components/app/kpi-widget";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { Iconify } from "@/components/minimal/iconify";
import { TableHeadCustom } from "@/components/app/table";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarFilterSx, orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { JobItem } from "@/components/app/sections/job/job-item";
import { JobList } from "@/components/app/sections/job/job-list";
import { SegmentTabs } from "@/components/app/list/segment-tabs";
import { UrlTabs } from "@/components/app/url-tabs";
import { AnimalPurchaseRecordedRange } from "./animal-purchase-recorded-range";
import type { ReactNode } from "react";
import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getAnimalPurchaseDeskCounts, listAnimalPurchaseLoads, listAnimalPurchaseReview } from "@/lib/api/procurement-server";
import type { AnimalPurchaseAnimal, AnimalPurchaseLoad } from "@/lib/api/procurement";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import {
  control,
  controlEnabled,
  copy,
  optionGroup,
  table,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { num } from "./sales-format";
import { AnimalPurchaseDecisionForm } from "./animal-purchase-decision-form";
import { AnimalPurchaseTelemetry } from "./animal-purchase-telemetry";
import { AnimalPurchaseAnswers, AnimalPurchaseEmptyTile, AnimalPurchaseMedia, FieldVerdictChip, type SopCopy } from "./animal-purchase-sop";
import { MEDIA_TILES_SX } from "./animal-purchase-tile-sx";
import { AnimalPurchaseLightbox } from "./animal-purchase-lightbox";
import { FormSelect } from "./form-select";
import { ProcurementTableFooter } from "./table-footer-links";
import { listOptions } from "./option-utils";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import Box from "@mui/material/Box";
import { mergeSx } from "@/components/app/merge-sx";
import { HiddenField } from "@/components/app/hidden-field";
import Typography from "@mui/material/Typography";
import Form from "next/form";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { AnimalCardsSkeleton, AnimalLoadRowsSkeleton } from "./animal-purchases-skeletons";
import { ANIMAL_CARD_COLUMNS, ANIMAL_KPI_SIZE, ANIMAL_LOADS_DEFAULT_LIMIT } from "./animal-purchases-layout";
import { CELL_LINK, cellLinksSx, phoneLoadCardsSx } from "./procurement-sx";
import { PageRoot } from "@/components/app/page-root";

// Hook class for the phone load-card layout (procurement-sx targets `table.<hook>`); no stylesheet defines it.
const LOADS_TABLE_HOOK = "ap-loads-table";

const LOAD_CARDS_SX = phoneLoadCardsSx(LOADS_TABLE_HOOK, [{ nth: 1, column: "1", row: 1 }, { nth: 3, column: "2", row: 1, alignEnd: true }, { nth: 2, column: "1", row: 2, secondary: true }, { nth: 6, column: "2", row: 2, alignEnd: true }]);

const PATHNAME = "/procurement/animal-purchases";
const DEFAULT_DECISION = "pending";
const DEFAULT_LIMIT = ANIMAL_LOADS_DEFAULT_LIMIT;
const DECISION_CONTROL = "decide_animal_purchase";

function hrefWithQuery(sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === "") query.delete(key);
    else query.set(key, value);
  }
  const qs = query.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

// decision_tone is backend vocabulary (neutral | ok | bad); the mock palette names differ, so the
// mapping lives here once. An unknown tone renders muted rather than inventing a colour.
function decisionTone(tone: AnimalPurchaseAnimal["decision_tone"]): Tone {
  if (tone === "ok") return "ok";
  if (tone === "bad") return "dng";
  return "mut";
}

/**
 * Animal purchases — the CEO/CXO's review of the animals the buying desk filmed on the phone
 * (maintainer decision 2026-09-13).
 *
 * Three bounded reads, in parallel: one page of loads, one page of the review queue for the
 * selected load + decision filter, and the WHOLE-DESK counts for the header tiles. The page never
 * records a load or an animal; its only write is the decision, and that renders only behind the
 * backend-declared `decide_animal_purchase` control — there is deliberately no role check here.
 */
// The KPI cards have no series on this read; the sparkline stays hidden.

export async function AnimalPurchasesPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // The decision filter is validated against the SERVED option keys, never trusted raw.
  const decisionOptions = optionGroup(pageContract, "animal_purchase_decisions");
  const requestedDecision = one(sp, "decision");
  const decision = decisionOptions.some((option) => option.key === requestedDecision) && requestedDecision
    ? requestedDecision
    : DEFAULT_DECISION;
  const loadId = one(sp, "load_id") || undefined;
  // Recorded-on window: ISO dates only reach the read; anything else is dropped, not guessed.
  const isoDate = (raw: string | undefined) => (raw && /^\d{4}-\d{2}-\d{2}$/.test(raw) ? raw : "");
  const recordedFrom = isoDate(one(sp, "recorded_from"));
  const recordedTo = isoDate(one(sp, "recorded_to"));
  const animalCursor = one(sp, "ap_cursor") || undefined;
  const loadCursor = one(sp, "ld_cursor") || undefined;

  const loadsTable = table(pageContract, "animal-purchase-loads");
  const animalsTable = table(pageContract, "animal-purchase-animals");
  // Rows per page for the loads table: a contract page size chosen in the URL (`ld_limit`), else the
  // first. `ld_offset` is only the position shown in the footer range ("21–40 of …"); the read
  // itself stays keyset-paged by `ld_cursor`.
  const loadsPageSizes = loadsTable.page_size_options.length > 0 ? loadsTable.page_size_options : [DEFAULT_LIMIT];
  const requestedLoadsLimit = Number(one(sp, "ld_limit"));
  const loadsLimit = loadsPageSizes.includes(requestedLoadsLimit) ? requestedLoadsLimit : loadsPageSizes[0];
  const loadsOffsetRaw = Number(one(sp, "ld_offset"));
  const loadsOffset = loadCursor && Number.isInteger(loadsOffsetRaw) && loadsOffsetRaw > 0 ? loadsOffsetRaw : 0;
  const animalsLimit = animalsTable.page_size_options[0] ?? DEFAULT_LIMIT;

  const [loadsResult, reviewResult, totalsResult] = await Promise.all([
    listAnimalPurchaseLoads({ limit: loadsLimit, cursor: loadCursor }),
    listAnimalPurchaseReview({
      load_id: loadId,
      decision,
      recorded_from: recordedFrom || undefined,
      recorded_to: recordedTo || undefined,
      limit: animalsLimit,
      cursor: animalCursor,
    }),
    // The header tiles count the WHOLE desk ("across every load"), which the filtered queue read
    // cannot answer once a load is selected.
    getAnimalPurchaseDeskCounts(),
  ]);
  if (firstAuthRequiredError(loadsResult, reviewResult, totalsResult)) redirect(INTERNAL_LOGIN_PATH);

  const loads: AnimalPurchaseLoad[] = loadsResult.ok ? listOrEmpty(loadsResult.data.loads) : [];
  const loadsNextCursor = loadsResult.ok ? loadsResult.data.next_cursor : undefined;
  const animals: AnimalPurchaseAnimal[] = reviewResult.ok ? listOrEmpty(reviewResult.data.animals) : [];
  const animalsNextCursor = reviewResult.ok ? reviewResult.data.next_cursor : undefined;
  const filters = reviewResult.ok ? listOrEmpty(reviewResult.data.filters) : [];
  const totals = totalsResult.ok ? totalsResult.data : null;

  const none = copy(pageContract, "value.none");
  const canDecide = controlEnabled(pageContract, DECISION_CONTROL, false);
  const decideDisabledReason = pageContract.controls.find((item) => item.id === DECISION_CONTROL)
    ? control(pageContract, DECISION_CONTROL).disabled_reason
    : undefined;
  const loadColumns = tableLabels(pageContract, "animal-purchase-loads");
  const animalColumn = (key: string) => copy(pageContract, `column.${key}`);
  const selectedLoad = loadId ? loads.find((load) => load.load_id === loadId) : undefined;
  const selectedLoadRef = selectedLoad?.load_ref ?? animals.find((animal) => animal.load_id === loadId)?.load_ref;

  const feedback = { status: one(sp, "ap_status"), code: one(sp, "ap_code") };
  const feedbackText = feedback.status
    ? copy(pageContract, `action.${feedback.code ?? ""}`, "") || copy(pageContract, "action.error_form")
    : "";

  // Questionnaire copy. Every key is served by the backend map; a contract older than this
  // screen falls through to the route's COPY_FALLBACKS in admin-ui-contract.
  const sopCopy: SopCopy = {
    mediaTitle: copy(pageContract, "media.title"),
    mediaEmpty: copy(pageContract, "media.empty"),
    photoOpen: copy(pageContract, "photo.open"),
    close: copy(pageContract, "action.close"),
    answersTitle: copy(pageContract, "answers.title"),
    verdictSection: copy(pageContract, "verdict.section"),
    attentionHint: copy(pageContract, "attention.hint"),
    fieldVerdictHint: copy(pageContract, "field_verdict.hint"),
  };

  const decisionLabels = {
    title: copy(pageContract, "decision.title"),
    note: copy(pageContract, "decision.note"),
    noteHint: copy(pageContract, "decision.note_hint"),
    accept: copy(pageContract, "action.accept"),
    reject: copy(pageContract, "action.reject"),
    deciding: copy(pageContract, "action.deciding"),
    // The sentences the in-place decision shows beside the buttons, one per outcome code.
    outcomes: Object.fromEntries(
      ["decided_accepted", "decided_rejected", "decide_conflict", "decide_failed", "error_form"].map((code) => [code, copy(pageContract, `action.${code}`)]),
    ),
  };

  return (
    <PageRoot>
      <AnimalPurchaseTelemetry rows={animals.length} pending={totals?.pending ?? 0} feedback={feedback} />

      <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb"), href: "/procurement/source-entry" }, { label: pageContract.title }]} />

      {/* Decision feedback from the Server Action's redirect. Every code resolves to page copy;
          an unknown one falls back to the generic failure line rather than leaking the token. */}
      {feedback.status ? (
        <Alert severity={feedback.status === "success" ? "info" : "error"} role="status">
          {feedbackText}
        </Alert>
      ) : null}

      {!loadsResult.ok ? (
        <Alert severity="error">
          {loadsResult.error.message}
        </Alert>
      ) : null}
      {!reviewResult.ok ? (
        <Alert severity="error">
          {reviewResult.error.message}
        </Alert>
      ) : null}

      {/* Whole-desk figures from the backend counts, never sums over the rendered page. Template
          Ecommerce overview KPI row: EcommerceWidgetSummary cards on a Grid, spacing 3. */}
      <Grid container spacing={3}>
        <Grid size={ANIMAL_KPI_SIZE}>
          <KpiWidget
            title={copy(pageContract, "summary.loads")}
            // The loads read is one keyset page; a caption only when there are more than shown (the
            // trailing "+"). No caption repeating the title ("Loads / Loads", TR1-#34).
            total={loadsResult.ok ? loads.length : null}
            caption={loadsResult.ok && loadsNextCursor ? `${num(loads.length)}+` : undefined}
          />
        </Grid>
        <Grid size={ANIMAL_KPI_SIZE}>
          <KpiWidget title={copy(pageContract, "summary.pending")} total={totals ? totals.pending : null} />
        </Grid>
        <Grid size={ANIMAL_KPI_SIZE}>
          <KpiWidget title={copy(pageContract, "summary.accepted")} total={totals ? totals.accepted : null} />
        </Grid>
        <Grid size={ANIMAL_KPI_SIZE}>
          <KpiWidget title={copy(pageContract, "summary.rejected")} total={totals ? totals.rejected : null} />
        </Grid>
      </Grid>

      {/* Loads: the template order-list card — CardHeader with the load filter pill strip as its
          action, the selected load's record, the Scrollbar table under TableHeadCustom and the
          template table pagination. */}
      <Card>
        <CardHeader
          title={copy(pageContract, "section.loads.title")}
          slotProps={{ title: { component: "h3" } }}
          sx={{ mb: 3, "& .MuiCardHeader-action": { alignSelf: "center", m: 0 } }}
          action={
            /* The load FILTER as one pill strip: All loads · <selected>. Query-param links, no overlay. */
            <SegmentTabs
              keepScroll
              ariaLabel={copy(pageContract, "filter.load")}
              value={loadId ? "selected" : "all"}
              tabs={[
                { value: "all", label: copy(pageContract, "filter.load.all"), href: hrefWithQuery(sp, { load_id: null, ap_cursor: null, ap_status: null, ap_code: null }) },
                ...(loadId ? [{ value: "selected", label: selectedLoadRef ?? none, href: hrefWithQuery(sp, {}) }] : []),
              ]}
            />
          }
        />
        {/* The selected load's own record: what the buying desk typed when it opened the load on
            the phone (load number, vendor, farm, expected count, note), who recorded it and when,
            plus any EXTRA authored SOP answers (how it arrived, documents, ...). These sit with the
            load, not with any one animal, so the CEO reads them once here. A recorder whose roster
            name cannot be resolved is dropped, never shown as an id. */}
        {/* The loads body (guard: url-keyed-panel): a load pick / page / page-size click swaps it to
            its skeleton at once; the card header and its load strip stay on screen. */}
        <UrlSuspense searchParams={sp} watch={LOADS_WATCH} fallback={<AnimalLoadRowsSkeleton rows={loadsLimit} />}>
        {selectedLoad ? (
          <Box
            data-testid="ap-load-detail"
            role="group"
            aria-label={copy(pageContract, "label.load_answers")}
            sx={{ display: "flex", flexWrap: "wrap", columnGap: 2.25, rowGap: 0.75, alignItems: "baseline", px: 1.75, py: 1, borderTop: 1, borderColor: "divider" }}
          >
            <Typography component="span" variant="caption" sx={{ color: "text.secondary", fontWeight: "fontWeightBold" }}>
              {copy(pageContract, "label.load_answers")}
            </Typography>
            <LoadFact label={copy(pageContract, "column.load_ref")}>{selectedLoad.load_ref}</LoadFact>
            <LoadFact label={copy(pageContract, "column.vendor_name")}>{selectedLoad.vendor_name || none}</LoadFact>
            <LoadFact label={copy(pageContract, "column.farm")}>{selectedLoad.farm}</LoadFact>
            <LoadFact label={copy(pageContract, "column.expected_count")}>{num(selectedLoad.expected_count)}</LoadFact>
            <LoadFact label={copy(pageContract, "label.load.status")}>{copy(pageContract, `status.load.${selectedLoad.status}`, selectedLoad.status)}</LoadFact>
            {selectedLoad.recorded_by_name ? (
              <LoadFact label={copy(pageContract, "label.load.recorded_by")}>{selectedLoad.recorded_by_name}</LoadFact>
            ) : null}
            <LoadFact label={copy(pageContract, "label.load.added_on")}>{fmtDateTime(selectedLoad.created_at)}</LoadFact>
            {(selectedLoad.answer_rows ?? []).map((row) => (
              <LoadFact key={row.question_id} label={row.question}>{row.answer}</LoadFact>
            ))}
            <LoadFact label={copy(pageContract, "label.load.notes")} note>
              {selectedLoad.notes ? selectedLoad.notes : <Box component="span" sx={{ color: "text.secondary", fontWeight: "fontWeightRegular" }}>{copy(pageContract, "label.load.no_notes")}</Box>}
            </LoadFact>
          </Box>
        ) : null}

        {loads.length === 0 ? (
          <EmptyState title={copy(pageContract, "empty.loads")} />
        ) : (
          <Box id="animal-purchase-loads" tabIndex={0} role="region" aria-label={loadsTable.title} sx={mergeSx(cellLinksSx, LOAD_CARDS_SX)}>
            <Scrollbar>
              <Table className={LOADS_TABLE_HOOK} aria-label={loadsTable.title} sx={{ minWidth: 960 }}>
                {/* Header labels come from the page contract IN ITS ORDER; the body cells below
                    are written in that same order (load_ref, vendor_name, farm, expected_count,
                    total, pending, accepted, rejected, created_at). */}
                <TableHeadCustom headCells={loadColumns.map((label, index) => ({ id: `c${index}`, label, sortable: false }))} />
                <TableBody>
                  {loads.map((load) => {
                    const selected = load.load_id === loadId;
                    const filterHref = hrefWithQuery(sp, {
                      load_id: selected ? null : load.load_id,
                      ap_cursor: null,
                      ap_status: null,
                      ap_code: null,
                    });
                    const cellLink = (content: ReactNode) => (
                      <Link href={filterHref} className={CELL_LINK} scroll={false} aria-current={selected ? "true" : undefined}>
                        {content}
                      </Link>
                    );
                    return (
                      <TableRow key={load.load_id} hover selected={selected} aria-selected={selected ? "true" : undefined} sx={{ "& td": { whiteSpace: "nowrap" } }}>
                        <TableCell sx={{ typography: "subtitle2" }}>{cellLink(load.load_ref)}</TableCell>
                        <TableCell sx={{ "&&": { whiteSpace: "normal" } }}>{cellLink(load.vendor_name || none)}</TableCell>
                        <TableCell>{cellLink(load.farm)}</TableCell>
                        <TableCell>{cellLink(num(load.expected_count))}</TableCell>
                        <TableCell>{cellLink(num(load.counts.total))}</TableCell>
                        <TableCell>{cellLink(<Tag tone={load.counts.pending > 0 ? "warn" : "mut"}>{num(load.counts.pending)}</Tag>)}</TableCell>
                        <TableCell>{cellLink(num(load.counts.accepted))}</TableCell>
                        <TableCell>{cellLink(num(load.counts.rejected))}</TableCell>
                        <TableCell>{cellLink(fmtDate(load.created_at))}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </Scrollbar>
          </Box>
        )}

        {loads.length > 0 ? (
          /* Keyset cursors read forward only; "Back" returns to the first page rather than growing
             a trail — the loads list is expected to stay shallow. There is no row total, so the range
             reads "1–20 of more than 20" while a next page exists and "1–2 of 2" once it does not. */
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows_per_page")}
            rowsValue={loadsLimit}
            rowsOptions={loadsPageSizes.map((size) => ({ size, href: hrefWithQuery(sp, { ld_limit: String(size), ld_cursor: null, ld_offset: null }) }))}
            page={loadCursor ? 2 : 1}
            pageCount={loadsNextCursor ? (loadCursor ? 3 : 2) : loadCursor ? 2 : 1}
            prevHref={loadCursor ? hrefWithQuery(sp, { ld_cursor: null, ld_offset: null }) : null}
            nextHref={loadsNextCursor ? hrefWithQuery(sp, { ld_cursor: loadsNextCursor, ld_offset: String(loadsOffset + loads.length) }) : null}
            rangeLabel={`${num(loadsOffset + 1)}–${num(loadsOffset + loads.length)} ${copy(pageContract, "pager.of", "of")} ${
              loadsNextCursor ? `${copy(pageContract, "pager.more_than")} ${num(loadsOffset + loads.length)}` : num(loadsOffset + loads.length)
            }`}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="animal-purchase-loads"
          />
        ) : null}
        </UrlSuspense>
      </Card>

      {/* Animals: the template job list — a Card holding the decision Tabs (Label counts) and the
          filter toolbar, then the job-item card grid with centred MUI Pagination. */}
      <Card>
        <CardHeader title={copy(pageContract, "section.animals.title")} slotProps={{ title: { component: "h3" } }} sx={{ mb: 1 }} />

        {/* Decision chips are the response's own filters: label and WHOLE-FILTER count verbatim,
            selection as the backend reports it. A filter switch drops the cursor by construction. */}
        {filters.length > 0 ? (
          <UrlTabs
            ariaLabel={copy(pageContract, "filter.decision")}
            value={filters.find((filter) => filter.selected)?.key ?? decision}
            items={filters.map((filter) => ({
              value: filter.key,
              label: filter.label,
              count: num(filter.count),
              href: hrefWithQuery(sp, {
                decision: filter.key === DEFAULT_DECISION ? null : filter.key,
                ap_cursor: null,
                ap_status: null,
                ap_code: null,
              }),
            }))}
          />
        ) : null}

        {/* Load and recorded-on window: a plain GET form, so the filter lives in the URL like
            every other list filter and the whole-filter chip counts follow it. The chip and the
            page cursor are dropped on submit by construction (they are not form fields). The
            fields sit in the template OrderTableToolbar row. */}
        <Form action={PATHNAME} scroll={false} role="search" aria-label={copy(pageContract, "filter.load")}>
        <Box>
          {decision !== DEFAULT_DECISION ? <HiddenField name="decision" value={decision} /> : null}
          <OrderTableToolbar
            filters={
              <Box sx={orderToolbarFilterSx}>
              <FormSelect
                label={copy(pageContract, "filter.load")}
                name="load_id"
                defaultValue={loadId ?? ""}
                fullWidth
                options={listOptions(
                  loads,
                  (load) => load.load_id,
                  (load) => `${load.load_ref} · ${load.vendor_name}`,
                  copy(pageContract, "filter.load.all"),
                )}
              />
              </Box>
            }
            search={<Box sx={orderToolbarSearchSx}><Box sx={{ display: "flex" }}>
              <AnimalPurchaseRecordedRange
                from={recordedFrom}
                to={recordedTo}
                label={copy(pageContract, "filter.recorded_from")}
                fromLabel={copy(pageContract, "filter.from")}
                toLabel={copy(pageContract, "filter.recorded_to")}
                previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
                nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
              />
              </Box></Box>}
            trailing={
              <Box sx={{ display: "flex", gap: 1, flexShrink: 0, pr: { md: 1.5 } }}>
                <Button type="submit" variant="contained" color="primary">
                  {copy(pageContract, "filter.apply")}
                </Button>
                {loadId || recordedFrom || recordedTo ? (
                  <Button component={Link} href={hrefWithQuery(sp, { load_id: null, recorded_from: null, recorded_to: null, ap_cursor: null, ap_status: null, ap_code: null })} scroll={false} color="inherit" variant="outlined">
                    {copy(pageContract, "filter.clear")}
                  </Button>
                ) : null}
              </Box>
            }
          />
        </Box>
        </Form>
      </Card>

      {/* The decision tabs drive a server navigation. The kit `TabPanel` content transition is
          NOT wrapped around this body: it branches on `useReducedMotion()`, which is false on the
          server and true on a reduced-motion client, so it renders a different element tree on
          each side and hydration fails on every load under that setting. Put it back once the kit
          renders one tree and only zeroes the durations. */}
      {/* The animals (guard: url-keyed-panel): a decision tab / filter / page change swaps them to
          the job-list skeleton at once; the tabs and toolbar above stay on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={ANIMALS_IGNORE} fallback={<AnimalCardsSkeleton />}>
      {animals.length === 0 ? (
        <Card>
          <EmptyState title={copy(pageContract, decision === DEFAULT_DECISION ? "empty.pending" : "empty.animals")} />
        </Card>
      ) : (
        <JobList
          columns={{ xs: `repeat(${ANIMAL_CARD_COLUMNS.xs}, minmax(0, 1fr))`, lg: `repeat(${ANIMAL_CARD_COLUMNS.lg}, minmax(0, 1fr))` }}
          pagination={{
            page: animalCursor ? 2 : 1,
            ariaLabel: copy(pageContract, "section.animals.title"),
            hrefs: [
              ...(animalCursor ? [hrefWithQuery(sp, { ap_cursor: null, ap_status: null, ap_code: null })] : []),
              hrefWithQuery(sp, { ap_status: null, ap_code: null }),
              ...(animalsNextCursor ? [hrefWithQuery(sp, { ap_cursor: animalsNextCursor, ap_status: null, ap_code: null })] : []),
            ],
          }}
        >
          {animals.map((animal) => {
            // The decision block is the same on both card shapes: who decided and when, the form
            // for a pending row behind the backend control, or the backend's reason.
            const decisionBlock =
              animal.decision !== "pending" ? (
                <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, typography: "body2" }}>
                  <Box component="span" sx={{ color: "text.secondary" }}>
                    {copy(pageContract, "decision.by")} {animal.decided_by_name || none}
                    {animal.decided_at ? ` ${copy(pageContract, "decision.on")} ${fmtDateTime(animal.decided_at)}` : ""}
                  </Box>
                  {animal.decision_note ? <span>{animal.decision_note}</span> : null}
                </Box>
              ) : canDecide ? (
                <AnimalPurchaseDecisionForm candidateId={animal.candidate_id} rowVersion={animal.row_version} labels={decisionLabels} />
              ) : (
                // A principal who can open the page but not decide sees the backend's reason,
                // never a button that would 403.
                <Box sx={{ color: "text.secondary", typography: "body2" }}>{decideDisabledReason || copy(pageContract, "verdict.disabled_no_access")}</Box>
              );

            // The CEO's decision chip, the buying desk's own field verdict beside it, and the load:
            // the template job-item meta line under the backend-owned row title.
            const heading = (
              <>
                <Tag tone={decisionTone(animal.decision_tone)}>{animal.decision_label}</Tag>
                <FieldVerdictChip animal={animal} hint={sopCopy.fieldVerdictHint} />
                <Box component="span" sx={{ color: "text.disabled", ml: 0.5 }}>
                  {animalColumn("load_ref")} {animal.load_ref}
                </Box>
              </>
            );

            if (animal.questionnaire_version > 0) {
              // A row recorded under the Procurement SOP questionnaire: the captures as one strip
              // in recorded order, the answers beneath in compact columns, the decision last.
              return (
                <article key={animal.candidate_id} className="ap-animal" aria-label={animal.title}>
                  <JobItem
                    title={animal.title}
                    meta={heading}
                  slotProps={{ meta: { flexWrap: "wrap" } }}
                  wrapFacts
                    avatar={<Iconify icon="solar:videocamera-record-bold" />}
                    media={<Box sx={{ mt: 2 }}><AnimalPurchaseMedia slots={animal.media_slots ?? []} copy={sopCopy} /></Box>}
                    sx={{ height: 1 }}
                  >
                    <DividedStack flexItem={false} spacing={2.5}>
                      <AnimalPurchaseAnswers rows={animal.answer_rows ?? []} copy={sopCopy} />
                      {decisionBlock}
                    </DividedStack>
                  </JobItem>
                </article>
              );
            }

            // A legacy row recorded before the questionnaire: one video and the few facts, in
            // the same card shape as an SOP row (a tile strip, then the facts, then the decision).
            return (
              <article key={animal.candidate_id} className="ap-animal" aria-label={animal.title}>
                <JobItem
                  title={animal.title}
                  meta={heading}
                  slotProps={{ meta: { flexWrap: "wrap" } }}
                  wrapFacts
                  avatar={<Iconify icon="solar:videocamera-record-bold" />}
                  sx={{ height: 1 }}
                  media={
                    <Box sx={{ mt: 2, ...MEDIA_TILES_SX }}>
                      {animal.media_url ? (
                        <AnimalPurchaseLightbox
                          items={[{ proofRef: animal.video_proof_ref || animal.candidate_id, url: animal.media_url, kind: "video", title: copy(pageContract, "video.title") }]}
                          openLabel={sopCopy.photoOpen}
                          closeLabel={sopCopy.close}
                        />
                      ) : (
                        <AnimalPurchaseEmptyTile text={copy(pageContract, "video.empty")} caption={copy(pageContract, "video.title")} />
                      )}
                    </Box>
                  }
                  // Template job facts: one caption per cell (label · value); the free-text note
                  // reads in full in the foot section above the decision.
                  facts={[
                    { key: "breed", label: `${animalColumn("breed")} · ${animal.breed || none}` },
                    { key: "age", label: `${animalColumn("age_months")} · ${animal.age_months == null ? none : num(animal.age_months)}` },
                    { key: "weight", label: `${animalColumn("weight_kg")} · ${animal.weight_kg == null ? none : num(animal.weight_kg, 1)}` },
                    { key: "condition", label: `${animalColumn("condition")} · ${animal.condition_label || none}` },
                    { key: "temp_tag", label: `${animalColumn("temp_tag")} · ${animal.temp_tag || none}` },
                  ]}
                >
                  {animal.notes ? (
                    <Typography variant="body2" sx={{ mb: 2, color: "text.secondary", overflowWrap: "anywhere" }}>
                      {animalColumn("notes")}: {animal.notes}
                    </Typography>
                  ) : null}
                  {decisionBlock}
                </JobItem>
              </article>
            );
          })}
        </JobList>
      )}
      </UrlSuspense>
    </PageRoot>
  );
}

/** The params the loads read (and the selected-load record) take. */
const LOADS_WATCH = ["load_id", "ld_cursor", "ld_limit", "ld_offset"] as const;
/** Params that never change the animals: the loads pager and the decision feedback. */
const ANIMALS_IGNORE = ["ld_cursor", "ld_limit", "ld_offset", "ap_status", "ap_code"] as const;

/** One typed fact of the selected load: the caption label, then the value in the subtitle weight. */
function LoadFact({ label, note, children }: { label: string; note?: boolean; children: ReactNode }) {
  return (
    <Box
      component="span"
      sx={{ display: "inline-flex", gap: 0.75, alignItems: "baseline", typography: "body2", ...(note ? { flexBasis: "100%", whiteSpace: "pre-wrap", overflowWrap: "anywhere" } : {}) }}
    >
      <Typography component="span" variant="caption" sx={{ color: "text.secondary" }}>
        {label}
      </Typography>{" "}
      <Box component="span" sx={{ fontWeight: "fontWeightSemiBold" }}>
        {children}
      </Box>
    </Box>
  );
}
