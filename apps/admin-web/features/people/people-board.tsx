import { UrlSuspense } from "@/components/app/url-suspense";
import { TableSkeleton } from "@/components/app/skeletons";
import Form from "next/form";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { humanizeEnum, joinParts } from "@/lib/format";
import { PeopleFormSelect } from "./people-form-select";
import { PeopleFilterFold } from "./people-filter-fold";
import Link from "@mui/material/Link";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { UserTableRow } from "@/components/app/sections/user/user-table-row";
import { TemplateTabs } from "@/components/app/template-tabs";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listWorkforcePeople, type WorkforcePerson } from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { actionFeedbackCopy, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PersonAccessLauncher } from "./person-access-launcher";
import { PersonAddDrawer } from "./person-add-drawer";
import Alert from "@mui/material/Alert";
import { EmptyState } from "@/components/app/empty-state";
import { PAGE_SIZE } from "./people-layout";


const PERSON_STATUSES = ["active", "candidate", "inactive", "suspended", "left"] as const;

/** Status colour only — the visible label is the backend value rendered verbatim. */
function statusColor(status: string): LabelColor {
  switch (status) {
    case "active":
      return "success";
    case "candidate":
      return "info";
    case "suspended":
      return "warning";
    case "left":
      return "error";
    default:
      return "default";
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

  const clockLabel = (person: WorkforcePerson) =>
    person.clock_in_today_label ? (
      <Label variant="soft" color="success">{copy(pageContract, "clock.chip.clocked_in").replace("%s", person.clock_in_today_label)}</Label>
    ) : (
      <Label variant="soft" color="default">{copy(pageContract, "clock.chip.not_clocked_in")}</Label>
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


      {/* Template user list (sections/user/view/user-list-view): Card → status Tabs with Label
          counts → toolbar → TableHeadCustom table of UserTableRow → TablePagination. The Add person
          action sits in the page breadcrumbs row (PeoplePage), as the template's "Add user". */}
      <Card>
        <TemplateTabs
          scrollButtons="auto"
          sx={{ px: { md: 2.5 } }}
          ariaLabel={copy(pageContract, "filter.status")}
          value={status || "all"}
          items={["", ...PERSON_STATUSES].map((key) => ({
            value: key || "all",
            label: key ? humanizeEnum(key) : copy(pageContract, "filter.all"),
            // The server returns no per-status totals: only the selected tab counts the page in hand.
            count: (key || "") === status ? `${people.length}${nextCursor ? "+" : ""}` : undefined,
            href: hrefWithQuery(pathname, sp, { status: key || null, cursor: null, action_status: null, action_key: null }),
          }))}
        />

        {/* GET form (next/form, a soft navigation): every filter round-trips through the URL, so the rendered page always
            matches the address bar and the server-read window. Template UserTableToolbar: selects
            (200px at md) then the full-width search, one 56px row; stacks on a phone. */}
        <Form id="people-filter-form" action={pathname} prefetch={false}>
        <Box
          sx={{
            p: 2.5,
            gap: 2,
            display: "flex",
            pr: { xs: 2.5, md: 1 },
            flexDirection: { xs: "column", md: "row" },
            alignItems: { xs: "stretch", md: "center" },
          }}
        >
          {status ? <input type="hidden" name="status" value={status} /> : null}
          <PeopleFilterFold
            filtersLabel={copy(pageContract, "filter.sheet_title")}
            closeLabel={copy(pageContract, "filter.sheet_close")}
            activeCount={[parkId, departmentId].filter(Boolean).length}
            search={
              <TextField
                id="people-search"
                name="search"
                fullWidth
                defaultValue={search}
                placeholder={copy(pageContract, "filter.search_placeholder")}
                sx={{ flex: "1 1 auto", minWidth: 0, order: { md: 2 } }}
                slotProps={{
                  htmlInput: { maxLength: 200, "aria-label": copy(pageContract, "filter.search_label") },
                  input: {
                    startAdornment: (
                      <InputAdornment position="start">
                        <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
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
              autoSubmit
              minWidth={200}
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
              autoSubmit
              minWidth={200}
              label={copy(pageContract, "filter.department")}
              defaultValue={departmentId}
              options={[
                { value: "", label: copy(pageContract, "filter.all") },
                ...catalog.departments.map((department) => ({ value: department.id, label: department.label })),
              ]}
            />
            {/* The selects apply on change (template toolbar: no Apply button). The button only shows
                in the phone filters drawer, where it closes the sheet on a deliberate tap. */}
            <Button type="submit" form="people-filter-form" variant="outlined" color="inherit" size="large" sx={{ minHeight: 56, flexShrink: 0, display: { xs: "inline-flex", sm: "none" } }}>
              {copy(pageContract, "filter.apply", "Apply")}
            </Button>
          </PeopleFilterFold>
        </Box>
        </Form>

        {/* The directory rows (guard: url-keyed-panel): a status / filter / search / page change
            swaps them to their skeleton at once; the tabs and toolbar stay on screen. */}
        <UrlSuspense searchParams={sp} watch={DIRECTORY_WATCH} fallback={<TableSkeleton bare header={false} pager={false} columns={6} rows={limit} />}>
        {people.length === 0 ? (
          <EmptyState title={hasAnyFilter ? copy(pageContract, "empty.people") : copy(pageContract, "empty.people.unset")} />
        ) : (
          <>
            {/* Laptop/tablet: the template table inside its Scrollbar (min width 960, as the template). */}
            <Box sx={{ display: { xs: "none", sm: "block" } }}>
              <Scrollbar>
                <Table sx={{ minWidth: 960 }} aria-label={copy(pageContract, "section.people.aria")}>
                  <TableHeadCustom
                    headCells={[
                      { id: "name", label: copy(pageContract, "column.display_name"), sortable: false },
                      { id: "park", label: copy(pageContract, "column.park"), sortable: false },
                      { id: "department", label: copy(pageContract, "column.department"), sortable: false },
                      { id: "designation", label: copy(pageContract, "column.designation"), sortable: false },
                      { id: "status", label: copy(pageContract, "column.status"), sortable: false, width: 110 },
                      { id: "clock", label: copy(pageContract, "clock.column.clock_in_today"), sortable: false },
                      // Access opens its own overlay rather than the record drawer: a different
                      // decision about the same person (the template row's trailing action cell).
                      { id: "access", label: copy(pageContract, "access.title"), sortable: false, width: 88 },
                    ]}
                  />
                  <TableBody>
                    {people.map((person) => {
                      const drawerHref = hrefWithQuery(pathname, sp, { person: person.person_id });
                      return (
                        <UserTableRow
                          key={person.person_id}
                          id={person.person_id}
                          name={person.display_name}
                          nameHref={drawerHref}
                          nameLinkComponent={LocalOverlayLink}
                          nameLinkProps={{ scroll: false }}
                          secondary={person.email ?? none}
                          cells={[person.park_label ?? none, person.department_label ?? none, designation(person)]}
                          status={{ label: humanizeEnum(person.status), color: statusColor(person.status) }}
                          extra={[clockLabel(person)]}
                          actions={<PersonAccessLauncher personId={person.person_id} personName={person.display_name} pageContract={pageContract} compact />}
                        />
                      );
                    })}
                  </TableBody>
                </Table>
              </Scrollbar>
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
                          <Label variant="soft" color={statusColor(person.status)}>{humanizeEnum(person.status)}</Label>
                        </Stack>
                        <Typography variant="body2" sx={{ color: "text.secondary" }}>
                          {joinParts([person.park_label ?? none, person.department_label, designation(person)])}
                        </Typography>
                        <Box>{clockLabel(person)}</Box>
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
        </UrlSuspense>
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

/** The params the directory read takes. */
const DIRECTORY_WATCH = ["search", "park_id", "department_id", "status", "cursor", "limit"] as const;
