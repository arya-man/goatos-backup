import Form from "next/form";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";
import Alert from "@mui/material/Alert";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { PeopleFormSelect } from "./people-form-select";
import Link from "@/components/no-prefetch-link";
import MuiLink from "@mui/material/Link";
import { EmptyState } from "@/components/app/empty-state";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table/table-head-custom";
import { TablePaginationLinks } from "@/components/app/table/table-pagination-links";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import { COURSE_WIDGET_ICONS } from "@/lib/minimal-icons";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listAdminClockEntries, type ClockEntry } from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PaletteColorKey } from "@/theme/core";
import { ClockEntryDrawer } from "./clock-entry-drawer";
import { ThemedDatePicker } from "@/components/themed-date-picker";

const PAGE_SIZE = 25;

/** Flag colours only — the visible label is the backend flag label verbatim. */
function flagColor(key: string): LabelColor {
  switch (key) {
    case "offline":
      return "info";
    case "no_location":
      return "warning";
    case "not_clocked_out":
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
 * The Clock In / Out tab of /people (maintainer decisions 2026-08-27/28): one
 * row per active person for the selected IST day — clock-in, clock-out,
 * backend-owned hours, location, device, and honesty flags. Whole-filter
 * summary tiles come from the backend, never page math. Server component; the
 * selected window is fetched and rendered, never the whole roster's history.
 */
export async function ClockScreen({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const pathname = "/people";
  const sp = searchParams;

  const date = one(sp, "date") ?? "";
  const parkId = one(sp, "park_id") ?? "";
  const designation = one(sp, "designation") ?? "";
  const bucket = one(sp, "bucket") ?? "";
  const search = one(sp, "search") ?? "";
  const cursor = one(sp, "cursor") ?? "";
  const limit = boundedInt(one(sp, "limit"), PAGE_SIZE, 1, 100);

  const result = await listAdminClockEntries({
    date: date || undefined,
    park_id: parkId || undefined,
    designation: designation || undefined,
    bucket: bucket || undefined,
    q: search || undefined,
    cursor: cursor || undefined,
    limit,
  });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const items: ClockEntry[] = result.ok ? listOrEmpty(result.data.items) : [];
  const summary = result.ok ? result.data.summary : { working: 0, clocked_out: 0, not_clocked_in: 0, flagged: 0 };
  const nextCursor = result.ok ? result.data.next_cursor : "";
  const parks = result.ok ? listOrEmpty(result.data.parks) : [];
  const designations = result.ok ? listOrEmpty(result.data.designations) : [];
  const none = copy(pageContract, "clock.value.none");

  const tiles: { key: string; label: string; value: number; color: PaletteColorKey; icon: string }[] = [
    { key: "working", label: copy(pageContract, "clock.summary.working"), value: summary.working, color: "success", icon: COURSE_WIDGET_ICONS.progress },
    { key: "clocked_out", label: copy(pageContract, "clock.summary.clocked_out"), value: summary.clocked_out, color: "info", icon: COURSE_WIDGET_ICONS.completed },
    { key: "not_clocked_in", label: copy(pageContract, "clock.summary.not_clocked_in"), value: summary.not_clocked_in, color: "warning", icon: COURSE_WIDGET_ICONS.certificates },
    { key: "flagged", label: copy(pageContract, "clock.summary.flagged"), value: summary.flagged, color: "error", icon: COURSE_WIDGET_ICONS.certificates },
  ];

  return (
    <Stack spacing={3}>
      {!result.ok ? (
        <Alert severity="error">
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </Alert>
      ) : null}

      {/* Template course overview KPI row (CourseWidgetSummary); tapping a tile narrows the list
          to that bucket, tapping it again clears it. */}
      <Grid container spacing={3}>
        {tiles.map((tile) => {
          const selected = bucket === tile.key;
          return (
            <Grid key={tile.key} size={{ xs: 6, md: 3 }}>
              <MuiLink
                component={Link}
                href={hrefWithQuery(pathname, sp, { bucket: selected ? null : tile.key, cursor: null })}
                scroll={false}
                aria-pressed={selected}
                color="inherit"
                underline="none"
                sx={{ display: "block", height: 1 }}
              >
                <CourseWidgetSummary
                  title={tile.label}
                  total={tile.value}
                  color={tile.color}
                  icon={tile.icon}
                  sx={selected ? { height: 1, outline: 2, outlineColor: `${tile.color}.main` } : { height: 1 }}
                />
              </MuiLink>
            </Grid>
          );
        })}
      </Grid>

      {/* Template user list card: toolbar (next/form GET, soft navigation; filters round-trip through the URL and
          tab=clock keeps the tab) → TableHeadCustom table → pagination. */}
      <Card>
        <CardHeader
          title={copy(pageContract, "clock.tab.title")}
          action={
            <Label variant="soft" color={items.length ? "info" : "default"}>
              {items.length} {copy(pageContract, "summary.count")}
            </Label>
          }
          sx={{ "& .MuiCardHeader-action": { alignSelf: "center" } }}
        />
        <Form action={pathname} prefetch={false}>
        <Box
          sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center", flexDirection: { xs: "column", md: "row" }, "& > *": { width: { xs: 1, md: "auto" } } }}
        >
          <input type="hidden" name="tab" value="clock" />
          {bucket ? <input type="hidden" name="bucket" value={bucket} /> : null}
          <Box sx={{ flexShrink: 0 }}>
            <ThemedDatePicker
              name="date"
              label={copy(pageContract, "clock.filter.date")}
              defaultValue={date}
              previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
              nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
              invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
            />
          </Box>
          <PeopleFormSelect
            name="park_id"
            minWidth={200}
            label={copy(pageContract, "filter.park")}
            defaultValue={parkId}
            options={[
              { value: "", label: copy(pageContract, "filter.all") },
              ...parks.map((park) => ({ value: park.id, label: park.label })),
            ]}
          />
          <PeopleFormSelect
            name="designation"
            minWidth={200}
            label={copy(pageContract, "column.designation")}
            defaultValue={designation}
            options={[
              { value: "", label: copy(pageContract, "filter.all") },
              ...designations.map((option) => ({ value: option.id, label: option.label })),
            ]}
          />
          <TextField
            id="clock-search"
            name="search"
            defaultValue={search}
            placeholder={copy(pageContract, "filter.search_placeholder")}
            sx={{ flex: "1 1 240px", minWidth: 0 }}
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
          <Button type="submit" variant="outlined" color="inherit" size="large" sx={{ minHeight: 56, flexShrink: 0 }}>
            {copy(pageContract, "filter.apply", "Apply")}
          </Button>
        </Box>
        </Form>

        {items.length === 0 ? (
          <EmptyState title={copy(pageContract, "clock.empty")} />
        ) : (
          <Scrollbar>
            <Table sx={{ minWidth: 960 }} aria-label={copy(pageContract, "clock.tab.title")}>
              <TableHeadCustom
                headCells={[
                  { id: "person", label: copy(pageContract, "clock.column.person"), sortable: false },
                  { id: "park", label: copy(pageContract, "clock.column.park"), sortable: false },
                  { id: "designation", label: copy(pageContract, "clock.column.designation"), sortable: false },
                  { id: "in", label: copy(pageContract, "clock.column.clock_in"), sortable: false },
                  { id: "out", label: copy(pageContract, "clock.column.clock_out"), sortable: false },
                  { id: "hours", label: copy(pageContract, "clock.column.hours"), sortable: false },
                  { id: "location", label: copy(pageContract, "clock.column.location"), sortable: false },
                  { id: "device", label: copy(pageContract, "clock.column.device"), sortable: false },
                  { id: "flags", label: copy(pageContract, "clock.column.flags"), sortable: false },
                ]}
              />
              <TableBody>
                {items.map((entry) => {
                  // A not-clocked-in roster row has no entry id and nothing to
                  // drill into; a real clocking opens the detail drawer.
                  const drawerHref = entry.clock_entry_id
                    ? hrefWithQuery(pathname, sp, { clocking: entry.clock_entry_id })
                    : "";
                  const cell = (content: React.ReactNode) =>
                    drawerHref ? (
                      <MuiLink component={LocalOverlayLink} href={drawerHref} scroll={false} color="inherit" underline="none" sx={{ display: "flex", alignItems: "center", minHeight: "var(--tap-min)" }}>
                        {content}
                      </MuiLink>
                    ) : (
                      content
                    );
                  return (
                    <TableRow hover key={`${entry.workforce_member_id}:${entry.business_date}`}>
                      <TableCell>{cell(<Typography variant="subtitle2" component="span">{entry.person_name}</Typography>)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{cell(entry.park_label ?? none)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{cell(entry.designation || none)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{cell(entry.clock_in_label || none)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{cell(entry.clock_out_label ?? none)}</TableCell>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{cell(entry.hours_label || none)}</TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>{cell(entry.location_label || none)}</TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>{cell(entry.device_label || none)}</TableCell>
                      <TableCell>
                        {entry.flags.length === 0
                          ? cell(none)
                          : cell(
                              <Box component="span" sx={{ display: "inline-flex", gap: 0.5, flexWrap: "wrap" }}>
                                {entry.flags.map((flag) => (
                                  <Label key={flag.key} variant="soft" color={flagColor(flag.key)}>
                                    {flag.label}
                                  </Label>
                                ))}
                              </Box>,
                            )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </Scrollbar>
        )}

        {cursor || nextCursor ? (
          <TablePaginationLinks
            page={cursor ? 1 : 0}
            rowsPerPage={limit}
            count={-1}
            prevHref={cursor ? hrefWithQuery(pathname, sp, { cursor: null }) : null}
            nextHref={nextCursor ? hrefWithQuery(pathname, sp, { cursor: nextCursor }) : null}
            rangeLabel={`${items.length} ${copy(pageContract, "summary.count")}`}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
          />
        ) : null}
      </Card>

      {/* Always mounted (LocalOverlayLink changes the URL without an RSC request). */}
      <ClockEntryDrawer pageContract={pageContract} listHref={hrefWithQuery(pathname, sp, { clocking: null })} />
    </Stack>
  );
}
