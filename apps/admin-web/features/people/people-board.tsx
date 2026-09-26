import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { humanizeEnum } from "@/lib/format";
import { PeopleFormSelect } from "./people-form-select";
import { PeopleFilterFold } from "./people-filter-fold";
import pb from "./people-board.module.css";
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
import Alert from "@mui/material/Alert";
import { EmptyState } from "@/components/app/empty-state";

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

  const people: WorkforcePerson[] = result.ok ? listOrEmpty(result.data.items) : [];
  const nextCursor = result.ok ? result.data.next_cursor : "";
  const catalog = result.ok ? result.data.catalog : { parks: [], departments: [] };

  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const hasAnyFilter = Boolean(search || parkId || departmentId || status);
  const none = copy(pageContract, "value.none");

  const designation = (person: WorkforcePerson): string => {
    if (person.designation_grade) return humanizeEnum(person.designation_grade);
    return humanizeEnum(person.role_hint);
  };

  return (
    <>
      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <Alert severity="error" style={{ marginBottom: 14 }}>
            {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        )
      ) : null}

      {!result.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message || copy(pageContract, "error.load")}
        </Alert>
      ) : null}


      <section className="card">
        <div className="hd">
          <Users className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.people.title")}</h3>
          <Tag tone={people.length ? "info" : "mut"}>
            {people.length} {copy(pageContract, "summary.count")}
          </Tag>
          <div className="sp" style={{ flex: 1 }} />
          <LocalOverlayLink
            href={hrefWithQuery(pathname, sp, { person: "new" })}
            className="btn primary"
            scroll={false}
          >
            {copy(pageContract, "action.add_person")}
          </LocalOverlayLink>
        </div>

        {/* Native GET form: every filter round-trips through the URL, so the rendered
            page always matches the address bar and the server-read window. */}
        <form id="people-filter-form" method="get" action={pathname} className="people-filter-card people-filter-incard">
          <div className="people-filter-grid">
            <PeopleFilterFold
              filtersLabel={copy(pageContract, "filter.sheet_title")}
              closeLabel={copy(pageContract, "filter.sheet_close")}
              activeCount={[parkId, departmentId, status].filter(Boolean).length}
              search={
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
              }
            >
            <PeopleFormSelect
              className="fld"
              form="people-filter-form"
              name="park_id"
              label={copy(pageContract, "filter.park")}
              defaultValue={parkId}
              options={[
                { value: "", label: copy(pageContract, "filter.all") },
                ...catalog.parks.map((park) => ({ value: park.id, label: park.label })),
              ]}
            />
            <PeopleFormSelect
              className="fld"
              form="people-filter-form"
              name="department_id"
              label={copy(pageContract, "filter.department")}
              defaultValue={departmentId}
              options={[
                { value: "", label: copy(pageContract, "filter.all") },
                ...catalog.departments.map((department) => ({ value: department.id, label: department.label })),
              ]}
            />
            <PeopleFormSelect
              className="fld"
              form="people-filter-form"
              name="status"
              label={copy(pageContract, "filter.status")}
              defaultValue={status}
              options={[
                { value: "", label: copy(pageContract, "filter.all") },
                ...["candidate", "active", "inactive", "suspended", "left"].map((value) => ({ value, label: value })),
              ]}
            />
            {/* Wrapped in a .fld with a spacer label so the button top-aligns with the
                selects instead of hanging at the row baseline. */}
            <div className="fld">
              <label aria-hidden="true">&nbsp;</label>
              <button type="submit" form="people-filter-form" className="btn">
                {copy(pageContract, "filter.apply", "Apply")}
              </button>
            </div>
            </PeopleFilterFold>
          </div>
        </form>

        {people.length === 0 ? (
          <EmptyState title={hasAnyFilter ? copy(pageContract, "empty.people") : copy(pageContract, "empty.people.unset")} />
        ) : (
          <div className={`twrap tablewrap ${pb.scrollCue}`} tabIndex={0} role="region" aria-label={copy(pageContract, "section.people.aria")}>
            <Table className="people-table" aria-label={copy(pageContract, "section.people.aria")}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{copy(pageContract, "column.display_name")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.park")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.department")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.designation")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.email")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "column.status")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.clock_in_today")}</TableCell>
                  {/* Access opens its own overlay rather than the record drawer: it is a
                      different decision about the same person, and burying it inside the
                      record drawer hides the screen this rewrite exists to provide. */}
                  <TableCell component="th">{copy(pageContract, "access.title")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {people.map((person) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { person: person.person_id });
                  return (
                    <TableRow key={person.person_id}>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <b>{person.display_name}</b>
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.park_label ?? none}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.department_label ?? none}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {designation(person)}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.email ?? none}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={statusTone(person.status)}>{humanizeEnum(person.status)}</Tag>
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {person.clock_in_today_label ? (
                            <Tag tone="ok">
                              {copy(pageContract, "clock.chip.clocked_in").replace("%s", person.clock_in_today_label)}
                            </Tag>
                          ) : (
                            <Tag tone="mut">{copy(pageContract, "clock.chip.not_clocked_in")}</Tag>
                          )}
                        </LocalOverlayLink>
                      </TableCell>
                      <TableCell>
                        <PersonAccessLauncher
                          personId={person.person_id}
                          personName={person.display_name}
                          pageContract={pageContract}
                        />
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
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
