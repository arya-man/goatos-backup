import Form from "next/form";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { humanizeEnum, joinParts } from "@/lib/format";
import { PeopleFormSelect } from "./people-form-select";
import { PeopleFilterFold } from "./people-filter-fold";
import pb from "./people-board.module.css";
import { Search } from "lucide-react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";
import { Label } from "@/components/minimal/label";
import { TablePaginationLinks } from "@/components/minimal/table/table-pagination-links";
import { PeopleAddButton } from "./people-add-button";
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

  const clockTag = (person: WorkforcePerson) =>
    person.clock_in_today_label ? (
      <Tag tone="ok">{copy(pageContract, "clock.chip.clocked_in").replace("%s", person.clock_in_today_label)}</Tag>
    ) : (
      <Tag tone="mut">{copy(pageContract, "clock.chip.not_clocked_in")}</Tag>
    );

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


      <Card>
        <CardHeader
          title={
            <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
              <span>{copy(pageContract, "section.people.title")}</span>
              <Label variant="soft" color={people.length ? "info" : "default"}>
                {people.length} {copy(pageContract, "summary.count")}
              </Label>
            </Stack>
          }
          action={<PeopleAddButton href={hrefWithQuery(pathname, sp, { person: "new" })} label={copy(pageContract, "action.add_person")} />}
          sx={{ "& .MuiCardHeader-action": { alignSelf: "center" } }}
        />

        {/* GET form (next/form, a soft navigation): every filter round-trips through the URL, so the rendered
            page always matches the address bar and the server-read window. Template
            UserTableToolbar: search + selects + Apply, one height (56px). */}
        <Form id="people-filter-form" action={pathname} prefetch={false}>
        <Box
          sx={{
            p: 2.5,
            gap: 2,
            display: "flex",
            flexWrap: "wrap",
            alignItems: "center",
            "& > *": { flexShrink: 0 },
          }}
        >
          <PeopleFilterFold
            filtersLabel={copy(pageContract, "filter.sheet_title")}
            closeLabel={copy(pageContract, "filter.sheet_close")}
            activeCount={[parkId, departmentId, status].filter(Boolean).length}
            search={
              <TextField
                id="people-search"
                name="search"
                defaultValue={search}
                placeholder={copy(pageContract, "filter.search_placeholder")}
                sx={{ flex: "1 1 240px", minWidth: 0 }}
                slotProps={{
                  htmlInput: { maxLength: 200, "aria-label": copy(pageContract, "filter.search_label") },
                  input: {
                    startAdornment: (
                      <InputAdornment position="start">
                        <Search size={18} aria-hidden="true" />
                      </InputAdornment>
                    ),
                  },
                }}
              />
            }
          >
            <PeopleFormSelect
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
              form="people-filter-form"
              name="status"
              label={copy(pageContract, "filter.status")}
              defaultValue={status}
              options={[
                { value: "", label: copy(pageContract, "filter.all") },
                ...["candidate", "active", "inactive", "suspended", "left"].map((value) => ({ value, label: humanizeEnum(value) })),
              ]}
            />
            <Button type="submit" form="people-filter-form" variant="outlined" color="inherit" size="large" sx={{ minHeight: 56, minWidth: 96 }}>
              {copy(pageContract, "filter.apply", "Apply")}
            </Button>
          </PeopleFilterFold>
        </Box>
        </Form>

        {people.length === 0 ? (
          <EmptyState title={hasAnyFilter ? copy(pageContract, "empty.people") : copy(pageContract, "empty.people.unset")} />
        ) : (
          <>
            {/* Laptop/tablet: the template table; the long free-text columns truncate
                (people-board.module.css) and the full value stays in the row drawer. */}
            <Box sx={{ display: { xs: "none", sm: "block" } }}>
              <div className={`twrap ${pb.scrollCue}`} tabIndex={0} role="region" aria-label={copy(pageContract, "section.people.aria")}>
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
                        <TableRow key={person.person_id} hover>
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
                              {clockTag(person)}
                            </LocalOverlayLink>
                          </TableCell>
                          <TableCell>
                            <PersonAccessLauncher personId={person.person_id} personName={person.display_name} pageContract={pageContract} />
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>
            </Box>

            {/* Phone: stacked rows (name + status, park · department, designation) with a
                44px Access icon button, instead of a table clipped at the card edge. */}
            <Box component="ul" sx={{ display: { xs: "block", sm: "none" }, m: 0, p: 0, listStyle: "none", borderTop: 1, borderColor: "divider" }} aria-label={copy(pageContract, "section.people.aria")}>
              {people.map((person) => {
                const drawerHref = hrefWithQuery(pathname, sp, { person: person.person_id });
                return (
                  <Box component="li" key={person.person_id} sx={{ display: "flex", alignItems: "center", gap: 1, pl: 2, pr: 1, py: 1.25, borderBottom: 1, borderColor: "divider" }}>
                    <LocalOverlayLink href={drawerHref} className="celllink" scroll={false} style={{ flex: "1 1 auto", minWidth: 0 }}>
                      <Stack spacing={0.5} sx={{ minWidth: 0 }}>
                        <Stack direction="row" spacing={1} sx={{ alignItems: "center", minWidth: 0 }}>
                          <Typography variant="subtitle2" noWrap sx={{ minWidth: 0 }}>{person.display_name}</Typography>
                          <Tag tone={statusTone(person.status)}>{humanizeEnum(person.status)}</Tag>
                        </Stack>
                        <Typography variant="body2" sx={{ color: "text.secondary" }}>
                          {joinParts([person.park_label ?? none, person.department_label, designation(person)])}
                        </Typography>
                        <Box>{clockTag(person)}</Box>
                      </Stack>
                    </LocalOverlayLink>
                    <PersonAccessLauncher personId={person.person_id} personName={person.display_name} pageContract={pageContract} compact />
                  </Box>
                );
              })}
            </Box>
          </>
        )}

        {cursor || nextCursor ? (
          <TablePaginationLinks
            page={cursor ? 1 : 0}
            rowsPerPage={limit}
            count={-1}
            prevHref={cursor ? hrefWithQuery(pathname, sp, { cursor: null }) : null}
            nextHref={nextCursor ? hrefWithQuery(pathname, sp, { cursor: nextCursor }) : null}
            rangeLabel={`${people.length} ${copy(pageContract, "summary.count")}`}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
          />
        ) : null}
      </Card>

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
