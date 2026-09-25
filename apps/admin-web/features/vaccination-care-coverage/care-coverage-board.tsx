import Link from "@/components/no-prefetch-link";
import { Check, ChevronRight, ClipboardCheck, ShieldPlus } from "lucide-react";
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

function LegendSwatch({ varName, label }: { varName: string; label: string }) {
  return (
    <span>
      <i className="sw" style={{ background: `var(${varName})` }} aria-hidden="true" />
      {label}
    </span>
  );
}

function CardHeader({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <div className="hd">
      <ShieldPlus className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
      <h3>{copy(pageContract, "section.matrix.title")}</h3>
      <div className="sp" style={{ flex: 1 }} />
      <span className="legend">
        <LegendSwatch varName="--brand" label={copy(pageContract, "legend.done")} />
        <LegendSwatch varName="--line2" label={copy(pageContract, "legend.not_done")} />
      </span>
    </div>
  );
}

function PageHead({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <div className="phead">
      <div>
        <div className="crumb">{copy(pageContract, "crumb")}</div>
        <h1>{pageContract.title}</h1>
        <div className="sub">{pageContract.subtitle}</div>
      </div>
    </div>
  );
}

export function CareCoverageSkeleton({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <div className="screen on" aria-busy="true">
      <PageHead pageContract={pageContract} />
      <section className="card">
        <CardHeader pageContract={pageContract} />
        <div className="bd">
          {[0, 1, 2, 3, 4, 5].map((row) => (
            <div key={row} className="skel" style={{ display: "block", width: "100%", height: 34, marginBottom: 8 }} />
          ))}
        </div>
      </section>
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
    <div className="screen on lt-page">
      <PageHead pageContract={pageContract} />
      <CareCoverageFilters
        parkChoices={parkChoices}
        parkSelected={scope.parkId ?? ""}
        parkClearHref={allParksHref}
        penChoices={(data?.pen_options ?? []).map((option) => ({ value: option.value, label: option.label }))}
        penSelected={pens}
        clearAllHref={clearAllHref}
        pageContract={pageContract}
      />
      <section className="card" style={{ marginBottom: 16 }}>
        <CardHeader pageContract={pageContract} />
        {rows.length === 0 ? (
          <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
            <ClipboardCheck className="ic" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} aria-hidden="true" />
            <div style={{ minWidth: 0, flex: 1 }}>
              <b style={{ fontSize: 14 }}>{result.ok ? copy(pageContract, "section.matrix.empty") : copy(pageContract, "section.matrix.unavailable")}</b>
              <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                {result.ok ? copy(pageContract, "section.matrix.empty_body") : copy(pageContract, "section.matrix.unavailable_body")}
              </span>
            </div>
          </div>
        ) : (
          <div className="bd" style={{ padding: 0 }}>
            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.matrix.aria")}>
              <table className="vaccination-status-matrix-table care-coverage-table">
                <thead>
                  <tr>
                    <th>{labels[0] ?? copy(pageContract, "label.pen")}</th>
                    {categories.map((category) => (
                      <th key={category.key}>{category.label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => {
                    const cells = new Map(row.cells.map((cell) => [cell.category, cell]));
                    return (
                      <tr key={`${row.park_id}|${row.shed_id}|${row.partition_label}`}>
                        <td>
                          <b>{row.operational_location_display || row.shed_name}</b>
                          <div className="muted small">{row.park_name}</div>
                        </td>
                        {categories.map((category) => {
                          const cell = cells.get(category.key);
                          return (
                            <td key={category.key}>
                              <DoneCell done={Boolean(cell?.done)} date={cell?.last_done_business_date} label={category.label} pageContract={pageContract} />
                            </td>
                          );
                        })}
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            <div className="note" style={{ margin: "12px 14px" }}>
              {copy(pageContract, "section.matrix.note")}
            </div>
            <div className="pager2">
              <span className="muted small">
                {from + 1}-{from + rows.length} {copy(pageContract, "pager.of")} {total} {total === 1 ? copy(pageContract, "label.pen").toLowerCase() : copy(pageContract, "label.pens")}
              </span>
              <span className="sp" style={{ flex: 1 }} />
              <span className="muted small">{copy(pageContract, "pager.rows")}</span>
              <span className="chipset" style={{ gap: 4 }}>
                {pageSizes.map((size) => (
                  <Link key={size} href={href({ cc_limit: String(size) })} replace scroll={false} className={`chip pgsize${pageSize === size ? " on" : ""}`}>
                    {size}
                  </Link>
                ))}
              </span>
              {cursor ? (
                <Link href={href({})} replace scroll={false} className="btn sm">
                  {copy(pageContract, "action.first_page")}
                </Link>
              ) : null}
              {data?.next_cursor ? (
                <Link href={href({ cc_cursor: data.next_cursor, cc_from: String(from + rows.length) })} replace scroll={false} className="btn sm">
                  {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
                </Link>
              ) : null}
            </div>
          </div>
        )}
      </section>
    </div>
  );
}
