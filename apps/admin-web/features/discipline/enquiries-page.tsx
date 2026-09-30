import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, getWorkforceEnquiries } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { HrmsFrame } from "./frame";
import { hrefWith } from "./href";

const PATHNAME = "/people/enquiries";
const STATUSES = ["", "open", "overdue", "submitted"] as const;

/**
 * People / HRMS > Enquiries (maintainer decisions 2026-09-30): every enquiry a farm event opened
 * (first: an approved death). The park head fills each on the phone; HR and the CEO/CXO open one
 * here and fill it on the web. Whole-filter counts; one page of enquiries per request.
 */
export async function EnquiriesPage({ searchParams, pageContract }: { searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams;
  const t = (key: string) => copy(pageContract, key);
  const park = one(sp, "park") ?? "";
  const status = STATUSES.find((s) => s === (one(sp, "status") ?? "")) ?? "";
  const cursor = one(sp, "cursor") ?? "";
  const result = await getWorkforceEnquiries({ parkId: park && park !== "all" ? park : undefined, status: status || undefined, cursor: cursor || undefined });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);
  if (!result.ok) {
    return (
      <HrmsFrame pageContract={pageContract}>
        <div className="alert">{result.error.message}</div>
      </HrmsFrame>
    );
  }
  const data = result.data;
  const labels = tableLabels(pageContract, "enquiries");
  return (
    <HrmsFrame pageContract={pageContract}>
      <div className="dsc-tiles">
        {[
          { key: "open", label: t("summary.open"), value: data.summary.open },
          { key: "overdue", label: t("summary.overdue"), value: data.summary.overdue },
          { key: "submitted", label: t("summary.submitted"), value: data.summary.submitted },
        ].map((tile) => (
          <Link key={tile.key} href={hrefWith(PATHNAME, sp, { status: status === tile.key ? null : tile.key, cursor: null })} className={`card dsc-tile${status === tile.key ? " on" : ""}`} scroll={false} data-testid={`enquiries-tile-${tile.key}`}>
            <div className="muted small">{tile.label}</div>
            <div className="dsc-tile-value">{tile.value}</div>
          </Link>
        ))}
      </div>
      <div className="dsc-filters">
        <nav className="subtabs" aria-label={t("filter.park")}>
          <Link href={hrefWith(PATHNAME, sp, { park: null, cursor: null })} className={!park || park === "all" ? "on" : undefined} scroll={false}>
            {t("filter.park_all")}
          </Link>
          {data.parks.map((p) => (
            <Link key={p.park_id} href={hrefWith(PATHNAME, sp, { park: p.park_id, cursor: null })} className={park === p.park_id ? "on" : undefined} scroll={false}>
              {p.label}
            </Link>
          ))}
        </nav>
        <nav className="subtabs" aria-label={t("filter.status")} data-testid="enquiries-status">
          {STATUSES.map((s) => (
            <Link key={s || "all"} href={hrefWith(PATHNAME, sp, { status: s || null, cursor: null })} className={status === s ? "on" : undefined} replace scroll={false}>
              {t(`filter.status.${s || "all"}`)}
            </Link>
          ))}
        </nav>
      </div>
      <section className="card" data-testid="enquiries-list">
        <div className="hd">
          <h3>{t("list.title")}</h3>
        </div>
        {data.items.length === 0 ? (
          <div className="empty">{t("list.empty")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={t("list.title")}>
            <table className="people-table">
              <thead>
                <tr>
                  {labels.map((l) => (
                    <th key={l}>{l}</th>
                  ))}
                  <th aria-hidden="true" />
                </tr>
              </thead>
              <tbody>
                {data.items.map((e) => (
                  <tr key={e.enquiry_id} data-testid="enquiry-row">
                    <td>
                      <b>{e.title}</b>
                      {e.subject_label ? <div className="small muted">{e.subject_label}</div> : null}
                    </td>
                    <td>{e.park_label}</td>
                    <td>{e.opened_at_label}</td>
                    <td className={e.overdue ? "dsc-overdue" : undefined}>{e.due_at_label}</td>
                    <td>
                      <span className={e.status === "submitted" ? "tag t-ok" : e.overdue ? "tag t-dng" : "tag t-warn"}>{e.status_label}</span>
                      {e.penalty_label ? <div className="small muted">{e.penalty_label}</div> : null}
                    </td>
                    <td>
                      <Link className="btn sm" href={`${PATHNAME}/${e.enquiry_id}`} data-testid="enquiry-open">
                        {e.status === "open" ? t("action.fill") : t("action.open")}
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {cursor || data.next_cursor ? (
          <div className="pager" style={{ padding: 12, display: "flex", gap: 8 }}>
            {cursor ? (
              <Link href={hrefWith(PATHNAME, sp, { cursor: null })} className="btn" scroll={false}>
                {t("pager.first")}
              </Link>
            ) : null}
            {data.next_cursor ? (
              <Link href={hrefWith(PATHNAME, sp, { cursor: data.next_cursor })} className="btn" scroll={false}>
                {t("pager.next")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>
    </HrmsFrame>
  );
}
