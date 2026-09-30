import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, getWorkforceViolations } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { HrmsFrame } from "./frame";
import { hrefWith } from "./href";
import { MonthSelect } from "./month-select";
import { ViolationsBoard } from "./violations-board";

const PATHNAME = "/people/violations";

/**
 * People / HRMS > Violations (maintainer decisions 2026-09-30): one park (or all) and one month --
 * the whole-filter totals, each person's total, and every violation recorded, from a hand record
 * or an enquiry. HR and the CEO/CXO record one here and withdraw a mistaken one; the violation
 * types and their fines are the published HRMS SOP. Every word is backend copy or a
 * backend-composed field; one page of violations is fetched, never the whole history.
 */
export async function ViolationsPage({ searchParams, pageContract }: { searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams;
  const t = (key: string) => copy(pageContract, key);
  const park = one(sp, "park") ?? "";
  const month = one(sp, "month") ?? "";
  const period = one(sp, "period") ?? "";
  const status = one(sp, "status") ?? "";
  const cursor = one(sp, "cursor") ?? "";
  const result = await getWorkforceViolations({ parkId: park && park !== "all" ? park : undefined, month: month || undefined, period: period || undefined, status: status || undefined, cursor: cursor || undefined });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);
  if (!result.ok) {
    return (
      <HrmsFrame pageContract={pageContract}>
        <div className="alert">{result.error.message}</div>
      </HrmsFrame>
    );
  }
  const data = result.data;
  const canEdit = controlEnabled(pageContract, "record_violation", false);
  const editReason = control(pageContract, "record_violation").disabled_reason ?? t("disabled.violation_write");
  const statuses = [
    { key: "", label: t("filter.status.all") },
    { key: "pending", label: t("filter.status.pending") },
    { key: "recorded", label: t("filter.status.recorded") },
    { key: "closed", label: t("filter.status.closed") },
    { key: "withdrawn", label: t("filter.status.withdrawn") },
  ];
  return (
    <HrmsFrame pageContract={pageContract}>
      <div className="dsc-filters">
        <nav className="subtabs" aria-label={t("filter.park")} data-testid="violations-parks">
          <Link href={hrefWith(PATHNAME, sp, { park: null, cursor: null })} className={!park || park === "all" ? "on" : undefined} scroll={false}>
            {t("filter.park_all")}
          </Link>
          {data.parks.map((p) => (
            <Link key={p.park_id} href={hrefWith(PATHNAME, sp, { park: p.park_id, cursor: null })} className={park === p.park_id ? "on" : undefined} scroll={false}>
              {p.label}
            </Link>
          ))}
        </nav>
        <nav className="subtabs" aria-label={t("filter.period")} data-testid="violations-period">
          {data.periods.map((p) => (
            <Link key={p.key} href={hrefWith(PATHNAME, sp, { period: p.key === "month" ? null : p.key, cursor: null })} className={data.period === p.key ? "on" : undefined} replace scroll={false}>
              {p.label}
            </Link>
          ))}
        </nav>
        {data.period === "all" ? null : <MonthSelect label={t("filter.month")} months={data.months} value={data.month} pathname={PATHNAME} searchParams={sp} />}
        <nav className="subtabs" aria-label={t("filter.status")} data-testid="violations-status">
          {statuses.map((s) => (
            <Link key={s.key || "all"} href={hrefWith(PATHNAME, sp, { status: s.key || null, cursor: null })} className={status === s.key ? "on" : undefined} replace scroll={false}>
              {s.label}
            </Link>
          ))}
        </nav>
      </div>
      <ViolationsBoard
        key={`${park}:${data.month}:${data.period}:${status}:${cursor}`}
        pageContract={pageContract}
        page={data}
        canEdit={canEdit}
        editReason={editReason}
        firstPageHref={cursor ? hrefWith(PATHNAME, sp, { cursor: null }) : ""}
        nextPageHref={data.next_cursor ? hrefWith(PATHNAME, sp, { cursor: data.next_cursor }) : ""}
      />
    </HrmsFrame>
  );
}
