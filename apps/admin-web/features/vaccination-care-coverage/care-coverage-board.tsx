import Box from "@mui/material/Box";
import { visuallyHidden } from "@mui/utils";
import { PageRoot } from "@/components/app/page-root";
import { UrlSuspense } from "@/components/app/url-suspense";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import { TableSkeleton } from "@/components/app/skeletons";
import Card from "@mui/material/Card";
import MuiCardHeader from "@mui/material/CardHeader";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";
import { PageHeader } from "@/components/app/page-header";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { getPCCarePenCoverage, type PCCarePenCoverage } from "@/lib/api/server";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { parseScope, scopeHref, type Scope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { PENS_PARAM, decodePens, encodePens } from "./pen-param";
import { CareCoverageFilters } from "./care-coverage-filters";

const ROUTE = "/vaccination/care-coverage";

// Care Coverage renders the vaccination Status matrix's layout for the five PC Care jobs: pens
// down the left, one column per job, a tick where the job is done. Every column, label and
// done-state comes from GET /app/pc-care/pen-coverage; this file owns layout only.

function pageSizeFrom(sp: RouteSearchParams, options: number[]): number {
  const raw = Number(one(sp, "cc_limit"));
  return options.includes(raw) ? raw : (options[0] ?? 25);
}

// Template chart legend dot (12px circle + caption), palette colours only.
function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, typography: "caption", color: "text.secondary" }}>
      <Box component="span" aria-hidden="true" sx={{ width: "calc(1.5 * var(--spacing))", height: "calc(1.5 * var(--spacing))", flexShrink: 0, borderRadius: "50%", bgcolor: color }} />
      {label}
    </Box>
  );
}

// Template card header (title, chart-legend dots in the action slot).
function CardHeader({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <MuiCardHeader
      title={copy(pageContract, "section.matrix.title")}
      action={
        <Box component="span" sx={{ display: "inline-flex", flexWrap: "wrap", alignItems: "center", gap: 2 }}>
          <LegendDot color="primary.main" label={copy(pageContract, "legend.done")} />
          <LegendDot color="grey.500" label={copy(pageContract, "legend.not_done")} />
        </Box>
      }
      sx={{ alignItems: "center", flexWrap: "wrap", gap: 1 }}
    />
  );
}

function PageHead({ pageContract }: { pageContract: AdminUiPageContract }) {
  return <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]} />;
}

// A done job: the template soft success circle with a check, the done date as a caption under it.
// Not done: a disabled dash. Each cell keeps its accessible "done / not done" text.
function DoneCell({ done, date, label, pageContract }: { done: boolean; date?: string; label: string; pageContract: AdminUiPageContract }) {
  if (!done) {
    return (
      <Box component="span" sx={{ color: "text.disabled" }} title={`${label} — ${copy(pageContract, "label.not_done")}`}>
        <span aria-hidden="true">—</span>
        <Box component="span" sx={visuallyHidden}>{copy(pageContract, "label.not_done")}</Box>
      </Box>
    );
  }
  const dateText = date ? fmtDate(date) : "";
  return (
    <Box
      component="span"
      sx={{ display: "inline-flex", flexDirection: "column", alignItems: "center", gap: 0.5 }}
      title={dateText ? `${label} — ${copy(pageContract, "label.done_on")} ${dateText}` : `${label} — ${copy(pageContract, "label.done")}`}
    >
      <Iconify icon="solar:check-circle-bold" width={24} sx={{ color: "success.main" }} />
      <Box component="span" sx={visuallyHidden}>{copy(pageContract, "label.done")}</Box>
      {dateText ? (
        <Typography variant="caption" sx={{ color: "text.secondary" }}>
          {dateText}
        </Typography>
      ) : null}
    </Box>
  );
}

// The MUI visually-hidden style (1px box). A hand-rolled `{ height: 1, width: 1, margin: -1 }` in sx
// reads 1 as 100% and -1 as a -8px spacing step: every "Not done" label was a full-cell absolute box
// hanging 19px under the last row, so the table's sideways scroller also scrolled 19px vertically
// and ate the first vertical swipe (J3B P2-1; r2 `table-scroll|table-scroll-trap`).

export async function CareCoverageBoard({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope: Scope = parseScope(sp);
  const pageSizes = tablePageSizes(pageContract, "care-coverage");
  const pageSize = pageSizeFrom(sp, pageSizes);
  const cursor = one(sp, "cc_cursor");
  const pens = decodePens(one(sp, PENS_PARAM));
  const from = Math.max(0, Number(one(sp, "cc_from")) || 0);
  const result = await getPCCarePenCoverage({ parkId: scope.parkId, pens, cursor, limit: pageSize });
  const data: PCCarePenCoverage | null = result.ok ? result.data : null;
  const labels = tableLabels(pageContract, "care-coverage");
  const categories = data?.categories ?? [];
  const rows = data?.rows ?? [];
  const total = data?.total ?? 0;

  function href(extra: Record<string, string>): string {
    return scopeHref(ROUTE, scope, {}, { cc_limit: String(pageSize), [PENS_PARAM]: pens.length ? encodePens(pens) : undefined, ...extra });
  }

  // Park writes the SAME `park` URL key as the top-bar picker, so the two controls always agree.
  // Changing park drops the pens (they belonged to the old park) and the page cursor.
  const allParksHref = scopeHref(ROUTE, scope, { park: null, mode: "company" }, { cc_limit: String(pageSize) });
  const parkChoices = (data?.park_options ?? []).map((option) => ({
    value: option.value,
    label: option.label,
    href: scopeHref(ROUTE, scope, { park: option.value, mode: "park" }, { cc_limit: String(pageSize) }),
  }));
  const clearAllHref = scope.parkId || pens.length > 0 ? allParksHref : null;

  return (
    <PageRoot>
      <PageHead pageContract={pageContract} />
      <Card>
        <CardHeader pageContract={pageContract} />
        <CareCoverageFilters
          parkChoices={parkChoices}
          parkSelected={scope.parkId ?? ""}
          parkClearHref={allParksHref}
          penChoices={(data?.pen_options ?? []).map((option) => ({ value: option.value, label: option.label }))}
          penSelected={pens}
          clearAllHref={clearAllHref}
          pageContract={pageContract}
        />
        {/* The matrix (guard: url-keyed-panel): a park / pen / page change swaps it to its skeleton at once. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<TableSkeleton bare header={false} columns={6} rows={10} />}>
        {rows.length === 0 ? (
          <EmptyContent
            filled
            title={result.ok ? copy(pageContract, "section.matrix.empty") : copy(pageContract, "section.matrix.unavailable")}
            description={result.ok ? copy(pageContract, "section.matrix.empty_body") : copy(pageContract, "section.matrix.unavailable_body")}
            sx={{ m: 3, mt: 0 }}
          />
        ) : (
          <>
            <Scrollbar>
              <Table sx={{ minWidth: 760 }} aria-label={copy(pageContract, "section.matrix.aria")}>
                <TableHeadCustom
                  headCells={[
                    { id: "pen", label: labels[0] ?? copy(pageContract, "label.pen") },
                    ...categories.map((category) => ({ id: category.key, label: category.label, align: "center" as const, width: 140 })),
                  ]}
                />
                <TableBody>
                  {rows.map((row) => {
                    const cells = new Map(row.cells.map((cell) => [cell.category, cell]));
                    return (
                      <TableRow hover key={`${row.park_id}|${row.shed_id}|${row.partition_label}`}>
                        <TableCell sx={{ minWidth: 180 }}>
                          <Typography variant="subtitle2" component="span" sx={{ display: "block" }}>
                            {row.operational_location_display || row.shed_name}
                          </Typography>
                          <Typography variant="caption" sx={{ color: "text.secondary" }}>
                            {row.park_name}
                          </Typography>
                        </TableCell>
                        {categories.map((category) => {
                          const cell = cells.get(category.key);
                          return (
                            <TableCell key={category.key} align="center" sx={{ position: "relative" }}>
                              <DoneCell done={Boolean(cell?.done)} date={cell?.last_done_business_date} label={category.label} pageContract={pageContract} />
                            </TableCell>
                          );
                        })}
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </Scrollbar>
            <Typography variant="caption" component="p" sx={{ px: 3, py: 2, color: "text.secondary" }}>
              {copy(pageContract, "section.matrix.note")}
            </Typography>
            {/* Cursor-paged: the backend never counts pages, so the arrows are "first page" and
                "next page", and the page size is a link like every other server-paged table. */}
            <TablePaginationLinks
              page={cursor ? 1 : 0}
              rowsPerPage={pageSize}
              count={-1}
              rowsPerPageHrefs={pageSizes.map((size) => ({ value: size, href: href({ cc_limit: String(size) }) }))}
              prevHref={cursor ? href({}) : null}
              nextHref={data?.next_cursor ? href({ cc_cursor: data.next_cursor, cc_from: String(from + rows.length) }) : null}
              labelRowsPerPage={copy(pageContract, "pager.rows")}
              rangeLabel={`${from + 1}-${from + rows.length} ${copy(pageContract, "pager.of")} ${total} ${total === 1 ? copy(pageContract, "label.pen").toLowerCase() : copy(pageContract, "label.pens")}`}
              prevLabel={copy(pageContract, "action.first_page")}
              nextLabel={copy(pageContract, "action.next")}
              replace
            />
          </>
        )}
        </UrlSuspense>
      </Card>
    </PageRoot>
  );
}
