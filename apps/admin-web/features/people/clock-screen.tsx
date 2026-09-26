import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { PeopleFormSelect } from "./people-form-select";
import { Clock } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { EmptyState } from "@/components/app/empty-state";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listAdminClockEntries, type ClockEntry } from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { ClockEntryDrawer } from "./clock-entry-drawer";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import Alert from "@mui/material/Alert";

const PAGE_SIZE = 25;

/** Flag tones only — the visible label is the backend flag label verbatim. */
function flagTone(key: string): Tone {
  switch (key) {
    case "offline":
      return "info";
    case "no_location":
      return "warn";
    case "not_clocked_out":
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

  const tiles: { key: string; label: string; value: number }[] = [
    { key: "working", label: copy(pageContract, "clock.summary.working"), value: summary.working },
    { key: "clocked_out", label: copy(pageContract, "clock.summary.clocked_out"), value: summary.clocked_out },
    { key: "not_clocked_in", label: copy(pageContract, "clock.summary.not_clocked_in"), value: summary.not_clocked_in },
    { key: "flagged", label: copy(pageContract, "clock.summary.flagged"), value: summary.flagged },
  ];

  return (
    <>
      {!result.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </Alert>
      ) : null}

      {/* Whole-filter summary tiles; tapping one narrows the list to that bucket. */}
      <KpiGrid min={180}>
        {tiles.map((tile) => (
          <KpiCard
            key={tile.key}
            label={tile.label}
            value={tile.value}
            tone={bucket === tile.key ? "info" : "neutral"}
            href={hrefWithQuery(pathname, sp, { bucket: bucket === tile.key ? null : tile.key, cursor: null })}
            hint={bucket === tile.key ? "Filtering \u00b7 click to clear" : undefined}
          />
        ))}
      </KpiGrid>

      {/* Native GET form: filters round-trip through the URL. tab=clock is
          preserved so submitting stays on this tab. */}
      <form method="get" action={pathname} className="card people-filter-card">
        <input type="hidden" name="tab" value="clock" />
        {bucket ? <input type="hidden" name="bucket" value={bucket} /> : null}
        <div className="people-filter-grid">
          <div className="fld">
            <label>{copy(pageContract, "clock.filter.date")}</label>
            <ThemedDatePicker
              name="date"
              label={copy(pageContract, "clock.filter.date")}
              defaultValue={date}
              previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
              nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
              invalidDateText={copy(pageContract, "date.invalid", "Pick a valid date")}
            />
          </div>
          <div className="fld" style={{ minWidth: 200, flex: 1 }}>
            <label htmlFor="clock-search">{copy(pageContract, "filter.search_label")}</label>
            <input
              id="clock-search"
              name="search"
              defaultValue={search}
              placeholder={copy(pageContract, "filter.search_placeholder")}
              maxLength={200}
            />
          </div>
          <PeopleFormSelect
            className="fld"
            name="park_id"
            label={copy(pageContract, "filter.park")}
            defaultValue={parkId}
            options={[
              { value: "", label: copy(pageContract, "filter.all") },
              ...parks.map((park) => ({ value: park.id, label: park.label })),
            ]}
          />
          <PeopleFormSelect
            className="fld"
            name="designation"
            label={copy(pageContract, "column.designation")}
            defaultValue={designation}
            options={[
              { value: "", label: copy(pageContract, "filter.all") },
              ...designations.map((option) => ({ value: option.id, label: option.label })),
            ]}
          />
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
          <Clock className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "clock.tab.title")}</h3>
          <Tag tone={items.length ? "info" : "mut"}>
            {items.length} {copy(pageContract, "summary.count")}
          </Tag>
        </div>

        {items.length === 0 ? (
          <EmptyState title={copy(pageContract, "clock.empty")} />
        ) : (
          <div className="twrap tablewrap" tabIndex={0} role="region" aria-label={copy(pageContract, "clock.tab.title")}>
            <Table className="people-table" aria-label={copy(pageContract, "clock.tab.title")}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{copy(pageContract, "clock.column.person")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.park")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.designation")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.clock_in")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.clock_out")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.hours")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.location")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.device")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "clock.column.flags")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {items.map((entry) => {
                  // A not-clocked-in roster row has no entry id and nothing to
                  // drill into; a real clocking opens the detail drawer.
                  const drawerHref = entry.clock_entry_id
                    ? hrefWithQuery(pathname, sp, { clocking: entry.clock_entry_id })
                    : "";
                  const cell = (content: React.ReactNode) =>
                    drawerHref ? (
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        {content}
                      </LocalOverlayLink>
                    ) : (
                      content
                    );
                  return (
                    <TableRow key={`${entry.workforce_member_id}:${entry.business_date}`}>
                      <TableCell>{cell(<b>{entry.person_name}</b>)}</TableCell>
                      <TableCell>{cell(entry.park_label ?? none)}</TableCell>
                      <TableCell>{cell(entry.designation || none)}</TableCell>
                      <TableCell>{cell(entry.clock_in_label || none)}</TableCell>
                      <TableCell>{cell(entry.clock_out_label ?? none)}</TableCell>
                      <TableCell>{cell(entry.hours_label || none)}</TableCell>
                      <TableCell className="muted">{cell(entry.location_label || none)}</TableCell>
                      <TableCell className="muted">{cell(entry.device_label || none)}</TableCell>
                      <TableCell>
                        {entry.flags.length === 0
                          ? cell(none)
                          : cell(
                              <span style={{ display: "inline-flex", gap: 4, flexWrap: "wrap" }}>
                                {entry.flags.map((flag) => (
                                  <Tag key={flag.key} tone={flagTone(flag.key)}>
                                    {flag.label}
                                  </Tag>
                                ))}
                              </span>,
                            )}
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

      {/* Always mounted (LocalOverlayLink changes the URL without an RSC request). */}
      <ClockEntryDrawer pageContract={pageContract} listHref={hrefWithQuery(pathname, sp, { clocking: null })} />
    </>
  );
}
