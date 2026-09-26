import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import MuiCardHeader from "@mui/material/CardHeader";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import { Check, ShieldPlus } from "lucide-react";
import { PageHeader, PageHeaderSkeleton } from "@/components/app/page-header";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TablePaginationLinks } from "@/components/minimal/table/table-pagination-links";
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

// Template card header (icon + title, legend in the action slot), as on the vaccination matrix.
function CardHeader({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <MuiCardHeader
      title={
        <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
          <ShieldPlus className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          {copy(pageContract, "section.matrix.title")}
        </Box>
      }
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

export function CareCoverageSkeleton({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <div className="screen on" aria-busy="true">
      <PageHeaderSkeleton />
      <Card>
        <CardHeader pageContract={pageContract} />
        <Box sx={{ px: 3, pb: 3, display: "grid", gap: 1 }}>
          {[0, 1, 2, 3, 4, 5].map((row) => (
            <Box key={row} className="skel" sx={{ display: "block", width: 1, height: "var(--table-row-h, 44px)" }} />
          ))}
        </Box>
      </Card>
    </div>
  );
}

function DoneCell({ done, date, label, pageContract }: { done: boolean; date?: string; label: string; pageContract: AdminUiPageContract }) {
  if (!done) {
    return (
      <span className="cc-miss" title={`${label} — ${copy(pageContract, "label.not_done")}`}>
        <span aria-hidden="true">—</span>
        <span className="sr-only">{copy(pageContract, "label.not_done")}</span>
      </span>
    );
  }
  const dateText = date ? fmtDate(date) : "";
  return (
    <span className="cc-done" title={dateText ? `${label} — ${copy(pageContract, "label.done_on")} ${dateText}` : `${label} — ${copy(pageContract, "label.done")}`}>
      <span className="cc-tick">
        <Check className="ic" aria-hidden="true" />
        <span className="sr-only">{copy(pageContract, "label.done")}</span>
      </span>
      {dateText ? <span className="muted small cc-date">{dateText}</span> : null}
    </span>
  );
}

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
    <div className="screen on">
      <PageHead pageContract={pageContract} />
      <Card sx={{ mb: 2 }}>
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
        {rows.length === 0 ? (
          <EmptyContent
            filled
            title={result.ok ? copy(pageContract, "section.matrix.empty") : copy(pageContract, "section.matrix.unavailable")}
            description={result.ok ? copy(pageContract, "section.matrix.empty_body") : copy(pageContract, "section.matrix.unavailable_body")}
            sx={{ m: 3, mt: 0 }}
          />
        ) : (
          <>
            <Box sx={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.matrix.aria")}>
              <Table className="vaccination-status-matrix-table care-coverage-table">
                <TableHead>
                  <TableRow>
                    <TableCell component="th">{labels[0] ?? copy(pageContract, "label.pen")}</TableCell>
                    {categories.map((category) => (
                      <TableCell component="th" key={category.key}>{category.label}</TableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {rows.map((row) => {
                    const cells = new Map(row.cells.map((cell) => [cell.category, cell]));
                    return (
                      <TableRow key={`${row.park_id}|${row.shed_id}|${row.partition_label}`}>
                        <TableCell>
                          <b>{row.operational_location_display || row.shed_name}</b>
                          <div className="muted small">{row.park_name}</div>
                        </TableCell>
                        {categories.map((category) => {
                          const cell = cells.get(category.key);
                          return (
                            <TableCell key={category.key}>
                              <DoneCell done={Boolean(cell?.done)} date={cell?.last_done_business_date} label={category.label} pageContract={pageContract} />
                            </TableCell>
                          );
                        })}
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </Box>
            <Box className="note" sx={{ mx: 1.75, my: 1.5 }}>
              {copy(pageContract, "section.matrix.note")}
            </Box>
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
      </Card>
    </div>
  );
}
