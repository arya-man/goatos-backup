import { redirect } from "next/navigation";
import { Users } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listWorkforcePeople, type WorkforcePerson } from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import { actionFeedbackCopy, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PersonAccessLauncher } from "./person-access-launcher";
import { PersonAddDrawer } from "./person-add-drawer";

const PAGE_SIZE = 25;

/** Status tone only — the visible label is the backend value rendered verbatim. */
function statusTone(status: string): Tone {
  switch (status) {
    case "active":
      return "ok";
    case "candidate":
      return "info";
    case "suspended":
      return "warn";
    case "left":
      return "dng";
    default:
      return "mut";
  }
}

function hrefWithQuery(pathname: string, sp: RouteSearchParams, patch: Record<string, string | null>): string {
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
  return qs ? `${pathname}?${qs}` : pathname;
}

/**
 * The ALL-PEOPLE directory: one keyset page of workforce members with park,
 * department, and designation, plus the Add Person drawer. Server component —
 * the selected window is fetched and rendered, never the whole roster.
 */
export async function PeopleBoard({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const pathname = "/people";
  const sp = searchParams;

  const search = one(sp, "search") ?? "";
  const parkId = one(sp, "park_id") ?? "";
  const departmentId = one(sp, "department_id") ?? "";
  const status = one(sp, "status") ?? "";
  const cursor = one(sp, "cursor") ?? "";
  const limit = boundedInt(one(sp, "limit"), PAGE_SIZE, 1, 100);

  const result = await listWorkforcePeople({
    q: search || undefined,
    park_id: parkId || undefined,
    department_id: departmentId || undefined,
    status: status || undefined,
    cursor: cursor || undefined,
    limit,
  });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const people: WorkforcePerson[] = result.ok ? result.data.items : [];
  const nextCursor = result.ok ? result.data.next_cursor : "";
  const catalog = result.ok ? result.data.catalog : { parks: [], departments: [] };

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const hasAnyFilter = Boolean(search || parkId || departmentId || status);
  const none = copy(pageContract, "value.none");

  const designation = (person: WorkforcePerson): string => {
    if (person.designation_grade) return person.designation_grade.replace(/_/g, " ");
    return person.role_hint.replace(/_/g, " ");
  };

  return (
    <>
      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        )
      ) : null}

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      {/* Native GET form: every filter round-trips through the URL, so the rendered
          page always matches the address bar and the server-read window. */}
      <form method="get" action={pathname} className="card" style={{ padding: 12, marginBottom: 14 }}>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
          <div className="fld" style={{ minWidth: 220, flex: 1 }}>
            <label htmlFor="people-search">{copy(pageContract, "filter.search_label")}</label>
            <input
              id="people-search"
              name="search"
              defaultValue={search}
              placeholder={copy(pageContract, "filter.search_placeholder")}
              maxLength={200}
            />
          </div>
          <div className="fld">
            <label htmlFor="people-park">{copy(pageContract, "filter.park")}</label>
            <select id="people-park" name="park_id" defaultValue={parkId}>
              <option value="">{copy(pageContract, "filter.all")}</option>
              {catalog.parks.map((park) => (
                <option key={park.id} value={park.id}>
                  {park.label}
                </option>
              ))}
            </select>
          </div>
          <div className="fld">
            <label htmlFor="people-department">{copy(pageContract, "filter.department")}</label>
            <select id="people-department" name="department_id" defaultValue={departmentId}>
              <option value="">{copy(pageContract, "filter.all")}</option>
              {catalog.departments.map((department) => (
                <option key={department.id} value={department.id}>
                  {department.label}
                </option>
              ))}
            </select>
          </div>
          <div className="fld">
            <label htmlFor="people-status">{copy(pageContract, "filter.status")}</label>
            <select id="people-status" name="status" defaultValue={status}>
              <option value="">{copy(pageContract, "filter.all")}</option>
              {["candidate", "active", "inactive", "suspended", "left"].map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
          </div>
          {/* Wrapped in a .fld with a spacer label so the button top-aligns with the
              selects instead of hanging at the row baseline. */}
          <div className="fld">
            <label aria-hidden="true">&nbsp;</label>
            <button type="submit" className="btn">
              {copy(pageContract, "filter.apply", "Apply")}
            </button>
          </div>
        </div>
      </form>

      <section className="card">
        <div className="hd">
          <Users className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.people.title")}</h3>
          <Tag tone={people.length ? "info" : "mut"}>
            {people.length} {copy(pageContract, "summary.count")}
          </Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.people.row_hint")}</span>
          <LocalOverlayLink
            href={hrefWithQuery(pathname, sp, { person: "new" })}
            className="btn primary"
            scroll={false}
          >
            {copy(pageContract, "action.add_person")}
          </LocalOverlayLink>
        </div>

        {people.length === 0 ? (
          <div className="empty">
            {hasAnyFilter ? copy(pageContract, "empty.people") : copy(pageContract, "empty.people.unset")}
          </div>
        ) : (
          <div className="twrap">
            <table className="people-table" aria-label={copy(pageContract, "section.people.aria")}>
              <thead>
                <tr>
                  <th>{copy(pageContract, "column.display_name")}</th>
                  <th>{copy(pageContract, "column.park")}</th>
                  <th>{copy(pageContract, "column.department")}</th>
                  <th>{copy(pageContract, "column.designation")}</th>
                  <th>{copy(pageContract, "column.email")}</th>
                  <th>{copy(pageContract, "column.status")}</th>
                  <th>{copy(pageContract, "clock.column.clock_in_today")}</th>
                  {/* Access opens its own overlay rather than the record drawer: it is a
                      different decision about the same person, and burying it inside the
                      record drawer hides the screen this rewrite exists to provide. */}
                  <th>{copy(pageContract, "access.title")}</th>
                </tr>
              </thead>
              <tbody>
                {people.map((person) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { person: person.person_id });
                  return (
                    <tr key={person.person_id}>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <b>{person.display_name}</b>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.park_label ?? none}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.department_label ?? none}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {designation(person)}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.email ?? none}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={statusTone(person.status)}>{person.status}</Tag>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.clock_in_today_label ? (
                            <Tag tone="ok">
                              {copy(pageContract, "clock.chip.clocked_in").replace("%s", person.clock_in_today_label)}
                            </Tag>
                          ) : (
                            <Tag tone="mut">{copy(pageContract, "clock.chip.not_clocked_in")}</Tag>
                          )}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <PersonAccessLauncher
                          personId={person.person_id}
                          personName={person.display_name}
                          pageContract={pageContract}
                        />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {cursor || nextCursor ? (
          <div className="pager2" style={{ paddingRight: 56 }}>
            {cursor ? (
              <Link href={hrefWithQuery(pathname, sp, { cursor: null })} scroll={false} className="btn">
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.prev_page")}
              </span>
            )}
            {nextCursor ? (
              <Link href={hrefWithQuery(pathname, sp, { cursor: nextCursor })} scroll={false} className="btn">
                {copy(pageContract, "action.next_page")}
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.next_page")}
              </span>
            )}
          </div>
        ) : null}
      </section>

      {/* Always mounted: LocalOverlayLink changes the URL without an RSC request,
          so an overlay gated on a server-read search param would never appear. */}
      <PersonAddDrawer
        people={people}
        catalog={catalog}
        pageContract={pageContract}
        listHref={hrefWithQuery(pathname, sp, { person: null })}
      />
    </>
  );
}
