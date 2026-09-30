import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, getWorkforceTimetable } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { HrmsPageFrame } from "./people-page";
import { TimetableBoard } from "./timetable-board";

const PATHNAME = "/people/timetable";
const PAGE_SIZE = 50;

function hrefWith(sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    if (value === undefined || key in patch) continue;
    for (const v of Array.isArray(value) ? value : [value]) query.append(key, v);
  }
  for (const [key, value] of Object.entries(patch)) if (value) query.set(key, value);
  const qs = query.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

/**
 * People / HRMS > Timetable (maintainer request 2026-09-30). One park at a time: the park's
 * shifts with their working hours, then everyone whose home park it is and the shift each works.
 * A park added in Configuration appears in the park strip on its own, with every shift "Not set"
 * and a Set time button. The two edit controls come from the page contract
 * (workforce.timetable.write: HR and the CEO/CXO); a reader sees them disabled with the reason.
 * Every word is backend copy or a backend-composed field; the selected park's page of people is
 * fetched and rendered, never the whole roster.
 */
export async function TimetablePage({ searchParams, pageContract }: { searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams;
  const t = (key: string) => copy(pageContract, key);
  const parkId = one(sp, "park") ?? "";
  const shift = one(sp, "shift") ?? "";
  const cursor = one(sp, "cursor") ?? "";

  const result = await getWorkforceTimetable({ parkId: parkId || undefined, shift: shift || undefined, cursor: cursor || undefined, limit: PAGE_SIZE });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const canEdit = controlEnabled(pageContract, "edit_person_shift", false) && controlEnabled(pageContract, "edit_shift_timing", false);
  const editReason = control(pageContract, "edit_person_shift")?.disabled_reason ?? t("disabled.timetable_write");

  if (!result.ok) {
    return (
      <HrmsPageFrame pageContract={pageContract}>
        <div className="alert" data-testid="timetable-error">
          {result.error.message}
        </div>
      </HrmsPageFrame>
    );
  }
  const data = result.data;
  if (data.parks.length === 0) {
    return (
      <HrmsPageFrame pageContract={pageContract}>
        <div className="card empty" style={{ padding: 24 }} data-testid="timetable-no-parks">
          <div className="small muted">{t("parks.empty")}</div>
        </div>
      </HrmsPageFrame>
    );
  }

  const shiftFilters = [
    { key: "", label: t("filter.shift_all"), count: data.total_people },
    ...data.shifts.map((s) => ({ key: s.shift_code, label: s.label, count: s.people_count })),
    { key: "unassigned", label: t("filter.shift_unassigned"), count: data.unassigned_count },
  ];

  return (
    <HrmsPageFrame pageContract={pageContract}>
      <nav className="subtabs" aria-label={t("filter.park")} data-testid="timetable-parks" style={{ marginBottom: 14 }}>
        {data.parks.map((park) => (
          <Link
            key={park.park_id}
            href={hrefWith(sp, { park: park.park_id, shift: null, cursor: null })}
            className={park.park_id === data.park_id ? "on" : undefined}
            aria-current={park.park_id === data.park_id ? "page" : undefined}
            scroll={false}
          >
            {park.label}
          </Link>
        ))}
      </nav>

      <TimetableBoard
        key={`${data.park_id}:${data.shift_filter}:${cursor}`}
        pageContract={pageContract}
        timetable={data}
        canEdit={canEdit}
        editReason={editReason}
        filters={
          <nav className="subtabs" aria-label={t("filter.shift")} data-testid="timetable-shift-filter">
            {shiftFilters.map((f) => (
              <Link
                key={f.key || "all"}
                href={hrefWith(sp, { park: data.park_id, shift: f.key || null, cursor: null })}
                className={data.shift_filter === f.key ? "on" : undefined}
                aria-current={data.shift_filter === f.key ? "page" : undefined}
                replace
                scroll={false}
              >
                {f.label} · {f.count}
              </Link>
            ))}
          </nav>
        }
        pager={
          cursor || data.next_cursor ? (
            <div className="pager" style={{ padding: 12, display: "flex", gap: 8 }}>
              {cursor ? (
                <Link href={hrefWith(sp, { park: data.park_id, cursor: null })} className="btn" scroll={false}>
                  {t("pager.first")}
                </Link>
              ) : null}
              {data.next_cursor ? (
                <Link href={hrefWith(sp, { park: data.park_id, cursor: data.next_cursor })} className="btn" scroll={false}>
                  {t("pager.next")}
                </Link>
              ) : null}
            </div>
          ) : null
        }
      />
    </HrmsPageFrame>
  );
}
