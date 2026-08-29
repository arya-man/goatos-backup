import { redirect } from "next/navigation";
import { Clock } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, listAdminClockEntries, type ClockEntry } from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { ClockEntryDrawer } from "./clock-entry-drawer";

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

  const items: ClockEntry[] = result.ok ? result.data.items : [];
  const summary = result.ok ? result.data.summary : { working: 0, clocked_out: 0, not_clocked_in: 0, flagged: 0 };
  const nextCursor = result.ok ? result.data.next_cursor : "";
  const parks = result.ok ? result.data.parks : [];
  const designations = result.ok ? result.data.designations : [];
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
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Whole-filter summary tiles; tapping one narrows the list to that bucket. */}
      <div className="kpis" style={{ display: "flex", gap: 10, flexWrap: "wrap", marginBottom: 14 }}>
        {tiles.map((tile) => (
          <Link
            key={tile.key}
            href={hrefWithQuery(pathname, sp, { bucket: bucket === tile.key ? null : tile.key, cursor: null })}
            scroll={false}
            className="card"
            style={{
              padding: "10px 16px",
              minWidth: 130,
              textDecoration: "none",
              outline: bucket === tile.key ? "2px solid var(--info)" : undefined,
            }}
          >
            <div className="muted small">{tile.label}</div>
            <div style={{ fontSize: 22, fontWeight: 700 }}>{tile.value}</div>
          </Link>
        ))}
      </div>

      {/* Native GET form: filters round-trip through the URL. tab=clock is
          preserved so submitting stays on this tab. */}
      <form method="get" action={pathname} className="card" style={{ padding: 12, marginBottom: 14 }}>
        <input type="hidden" name="tab" value="clock" />
        {bucket ? <input type="hidden" name="bucket" value={bucket} /> : null}
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
          <div className="fld">
            <label htmlFor="clock-date">{copy(pageContract, "clock.filter.date")}</label>
            <input id="clock-date" name="date" type="date" defaultValue={date} />
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
          <div className="fld">
            <label htmlFor="clock-park">{copy(pageContract, "filter.park")}</label>
            <select id="clock-park" name="park_id" defaultValue={parkId}>
              <option value="">{copy(pageContract, "filter.all")}</option>
              {parks.map((park) => (
                <option key={park.id} value={park.id}>
                  {park.label}
                </option>
              ))}
            </select>
          </div>
          <div className="fld">
            <label htmlFor="clock-designation">{copy(pageContract, "column.designation")}</label>
            <select id="clock-designation" name="designation" defaultValue={designation}>
              <option value="">{copy(pageContract, "filter.all")}</option>
              {designations.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </select>
          </div>
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
          <div className="empty">{copy(pageContract, "clock.empty")}</div>
        ) : (
          <div className="twrap">
            <table className="people-table" aria-label={copy(pageContract, "clock.tab.title")}>
              <thead>
                <tr>
                  <th>{copy(pageContract, "clock.column.person")}</th>
                  <th>{copy(pageContract, "clock.column.park")}</th>
                  <th>{copy(pageContract, "clock.column.designation")}</th>
                  <th>{copy(pageContract, "clock.column.clock_in")}</th>
                  <th>{copy(pageContract, "clock.column.clock_out")}</th>
                  <th>{copy(pageContract, "clock.column.hours")}</th>
                  <th>{copy(pageContract, "clock.column.location")}</th>
                  <th>{copy(pageContract, "clock.column.device")}</th>
                  <th>{copy(pageContract, "clock.column.flags")}</th>
                </tr>
              </thead>
              <tbody>
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
                    <tr key={`${entry.workforce_member_id}:${entry.business_date}`}>
                      <td>{cell(<b>{entry.person_name}</b>)}</td>
                      <td>{cell(entry.park_label ?? none)}</td>
                      <td>{cell(entry.designation || none)}</td>
                      <td>{cell(entry.clock_in_label || none)}</td>
                      <td>{cell(entry.clock_out_label ?? none)}</td>
                      <td>{cell(entry.hours_label || none)}</td>
                      <td className="muted">{cell(entry.location_label || none)}</td>
                      <td className="muted">{cell(entry.device_label || none)}</td>
                      <td>
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
            ) : null}
            {nextCursor ? (
              <Link href={hrefWithQuery(pathname, sp, { cursor: nextCursor })} scroll={false} className="btn">
                {copy(pageContract, "action.next_page")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>

      {/* Always mounted (LocalOverlayLink changes the URL without an RSC request). */}
      <ClockEntryDrawer pageContract={pageContract} listHref={hrefWithQuery(pathname, sp, { clocking: null })} />
    </>
  );
}
