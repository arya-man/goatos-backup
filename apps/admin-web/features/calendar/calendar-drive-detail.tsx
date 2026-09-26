import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { randomUUID } from "node:crypto";
import { ChevronLeft, ChevronRight, Search } from "lucide-react";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import MuiGrid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { PageHeader } from "@/components/app/page-header";
import { type AdminUiPageContract, copy, optionLabel } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { fmtDate as fmtIstDate } from "@/lib/format";
import { HerdPassportLocalDrawer, type HerdPassportDrawerItem } from "@/features/counts";
import { getCalendarVaccinationEventDetail, getCalendarDriveTargets } from "./calendar-server";
import { driveSummaryOf, type CalendarDriveTarget } from "./calendar-contract";
import { driveVisibleProgress, drivePctFor, driveStatusChips, driveStatusClass } from "./drive-card-metrics";
import { operationalLocationLabel } from "@/lib/operational-location";
import { stageLabel } from "@/lib/stage-labels";
import Alert from "@mui/material/Alert";

function targetLocationLabel(item: CalendarDriveTarget): string {
  if (!item.shed_name) return "—";
  return (
    item.operational_location_display ||
    operationalLocationLabel({
      shedName: item.shed_name,
      partitionLabel: item.partition_label,
      sourceShedName: item.source_shed_name,
    })
  );
}

// Full-screen drive detail (owner-directed replacement for the calendar drive drawer, 2026-07-14).
// New route with no backend page contract yet, so its structural labels (breadcrumb crumbs, roster
// column headers) are local literals — the same documented exception as /verification (see
// context/frontend/admin-web-backend-ui-contract.md, allow-listed in check-ui-contract-literals.mjs).

function targetReason(item: CalendarDriveTarget): string {
  if (item.status === "deferred" && item.defer_reason) return item.defer_reason;
  if (item.exit_reason) return item.exit_reason;
  return "";
}

function hiddenInputs(params: RouteSearchParams, exclude: Set<string>) {
  return Object.entries(params).flatMap(([key, value]) => {
    if (exclude.has(key)) return [];
    const values = Array.isArray(value) ? value : value ? [value] : [];
    return values.map((item, index) => <input key={`${key}-${index}`} type="hidden" name={key} value={item} />);
  });
}

function hrefWithoutKeys(pathname: string, params: RouteSearchParams, keys: Set<string>) {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (keys.has(key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item) next.append(key, item);
      }
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function hrefWithParam(pathname: string, params: RouteSearchParams, key: string, value: string | null): string {
  const next = new URLSearchParams();
  for (const [paramKey, paramValue] of Object.entries(params)) {
    if (paramKey === key) continue;
    if (Array.isArray(paramValue)) {
      for (const item of paramValue) if (item) next.append(paramKey, item);
    } else if (paramValue) {
      next.set(paramKey, paramValue);
    }
  }
  if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export async function VaccinationDriveDetail({
  eventId,
  searchParams,
  pageContract,
}: {
  eventId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const backHref = scopeHref("/calendar", scope);
  const cursor = one(sp, "cursor");
  const targetSearch = (one(sp, "q") ?? "").trim();
  const page = boundedInt(one(sp, "page"), 1, 1, 1000000);
  const selectedGoatId = one(sp, "goat_passport");
  const detailPath = `/calendar/drive/${eventId}`;

  const [detail, targets] = await Promise.all([
    getCalendarVaccinationEventDetail(eventId),
    getCalendarDriveTargets(eventId, { cursor, limit: 25, q: targetSearch || undefined }),
  ]);

  if (!detail.ok) {
    return (
      <div className="screen on">
        <PageHeader
          title={pageContract.title}
          backHref={backHref}
          crumbs={[{ label: copy(pageContract, "calendar.breadcrumb.vaccination"), href: backHref }, { label: pageContract.title }]}
        />
        <Alert severity="error" role="alert">
          <b>{detail.error.code ?? detail.error.kind}</b>&nbsp;{detail.error.message}
        </Alert>
      </div>
    );
  }

  const event = detail.data.event;
  const summary = driveSummaryOf(event);
  const currentLabel = summary ? `${summary.park_name} · ${fmtIstDate(summary.due_date)}` : event.title;
  const crumb = (
    <PageHeader
      title={currentLabel}
      backHref={backHref}
      crumbs={[
        { label: copy(pageContract, "calendar.breadcrumb.vaccination"), href: scopeHref("/vaccination", scope) },
        { label: copy(pageContract, "calendar.breadcrumb.calendar"), href: backHref },
        { label: currentLabel },
      ]}
    />
  );

  if (!summary) {
    return (
      <div className="screen on">
        {crumb}
        <div className="note">{copy(pageContract, "calendar.drive.summary_pending")}</div>
      </div>
    );
  }

  // Same drive, same number: the detail ring renders the BACKEND-OWNED progress contract verbatim,
  // exactly as DriveProgressCard does. It previously derived its own numerator via the legacy
  // per-client coverage helper and its own percentage via the closed-cap helper, which caps at 99
  // until event.status is
  // "completed" -- so a fully covered drive read 100% on the card and 99% on its own detail page.
  // Do NOT reintroduce a locally derived numerator or a local cap here.
  const coverage = driveVisibleProgress(summary);
  const pct = drivePctFor(summary, coverage);
  const chips = driveStatusChips(summary);
  const ringRadius = 29;
  const ringCircumference = 2 * Math.PI * ringRadius;
  const ringOffset = ringCircumference * (1 - pct / 100);

  const rosterItems = targets && targets.ok ? listOrEmpty(targets.data.items) : [];
  const nextCursor = targets && targets.ok ? targets.data.next_cursor : undefined;
  const targetsError = targets && !targets.ok ? targets.error : null;
  const nextHref = nextCursor ? hrefWithPagedCursor(detailPath, sp, "cursor", nextCursor, "page", "cursor_stack") : null;
  const prevHref = hrefPreviousPagedCursor(detailPath, sp, "cursor", "page", "cursor_stack");
  const clearSearchHref = hrefWithoutKeys(detailPath, sp, new Set(["q", "cursor", "page", "cursor_stack"]));
  const closePassportHref = hrefWithParam(detailPath, sp, "goat_passport", null);
  const reproductiveIdempotencyKey = randomUUID();
  const drawerItems: HerdPassportDrawerItem[] = rosterItems.map((item) => ({
    goatId: item.animal_id,
    displayId: item.display_id,
    tag1: item.animal_identifier_1,
    tag2: item.animal_identifier_2,
    park: summary.park_name,
    shed: targetLocationLabel(item),
    breed: null,
    sex: null,
    lifecycleStatus: item.lifecycle_status,
    healthStatus: item.health_status,
    reproductiveStatus: item.stage,
  }));

  return (
    <div className="kit-enter screen on">
      {crumb}
      <MuiGrid container spacing={3}>
        <MuiGrid size={{ xs: 12, md: 8 }}>
          <Box sx={{ gap: 3, display: "flex", flexDirection: { xs: "column-reverse", md: "column" } }}>
            <section className="card">
              <div className="hd"><h3>{event.title} · {summary.park_name}</h3></div>
              <div className="bd">
                <div className="ddhero">
                  <svg className="dring" viewBox="0 0 70 70" width="92" height="92" aria-hidden="true">
                    <circle className="rbg" cx="35" cy="35" r={ringRadius} />
                    <circle className="rfg" cx="35" cy="35" r={ringRadius} strokeDasharray={ringCircumference.toFixed(1)} strokeDashoffset={ringOffset.toFixed(1)} transform="rotate(-90 35 35)" />
                    <text x="35" y="35" className="rtx" textAnchor="middle" dominantBaseline="central">{pct}%</text>
                  </svg>
                  <div>
                    <div style={{ fontSize: 22, fontWeight: 700 }}>
                      <span className="mono">{coverage.completed}</span>{" "}
                      <span style={{ fontSize: 15, color: "var(--muted)" }}>
                        {copy(pageContract, "calendar.drive.of")} {coverage.total} {copy(pageContract, coverage.usesAnimals ? "calendar.drive.animals" : "calendar.drive.doses")}
                      </span>
                    </div>
                    <div className="metric" style={{ marginTop: 6 }}>
                      <span><b style={{ color: "var(--ink)" }}>{summary.sheds_completed}</b> {copy(pageContract, "calendar.drive.of")} {summary.shed_count} {copy(pageContract, "calendar.drive.sheds_done_suffix")} · {copy(pageContract, "calendar.drive.owner")} {summary.owner_label}</span>
                    </div>
                  </div>
                </div>
              </div>
            </section>

            <section className="card">
        <div className="bd">
          <div className="lt">{copy(pageContract, "calendar.drive.animal_roster")}</div>
          <div style={{ display: "flex", alignItems: "center", gap: 12, justifyContent: "space-between", flexWrap: "wrap", margin: "0 0 14px" }}>
            <form action={detailPath} style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 320, flex: "1 1 420px" }}>
              {hiddenInputs(sp, new Set(["q", "cursor", "page", "cursor_stack"]))}
              <label className="searchbox" style={{ flex: "1 1 280px", display: "flex", alignItems: "center", gap: 8 }}>
                <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
                <input
                  name="q"
                  defaultValue={targetSearch}
                  placeholder={copy(pageContract, "calendar.drive.search_placeholder")}
                  style={{ width: "100%", background: "transparent", border: 0, outline: 0, color: "inherit" }}
                />
              </label>
              <button type="submit" className="btn btn-primary">{copy(pageContract, "calendar.drive.search_action")}</button>
              {targetSearch ? <Link href={clearSearchHref} className="btn">{copy(pageContract, "calendar.drive.clear_search")}</Link> : null}
            </form>
            <div className="sub" style={{ whiteSpace: "nowrap" }}>
              {copy(pageContract, "calendar.drive.page_label")} {page} · {rosterItems.length} {copy(pageContract, "calendar.drive.rows_label")}
            </div>
          </div>
          {targetsError ? (
            <div style={{ padding: 16, textAlign: "center", color: "var(--danger)" }}>
              <b>{targetsError.code ?? targetsError.kind}</b>&nbsp;{targetsError.message}
            </div>
          ) : (
            <>
              <div className="tablewrap" style={{ overflowX: "auto" }}>
                <Table className="rostertbl">
                  <TableHead><TableRow><TableCell component="th">{copy(pageContract, "calendar.drive.display_id_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.shed_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.tag_1_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.tag_2_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.stage_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.lifecycle_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.health_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.reason_header")}</TableCell><TableCell component="th">{copy(pageContract, "calendar.drive.status_header")}</TableCell></TableRow></TableHead>
                  <TableBody className="mono">
                    {rosterItems.length ? rosterItems.map((item) => {
                      const passportHref = hrefWithParam(detailPath, sp, "goat_passport", item.animal_id);
                      return (
                        <TableRow key={item.animal_id}>
                          <TableCell>
                            <LocalOverlayLink href={passportHref} className="celllink" scroll={false}>
                              <span className="gid">{item.display_id || "—"}</span>
                            </LocalOverlayLink>
                          </TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{targetLocationLabel(item)}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{item.animal_identifier_1 || "—"}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{item.animal_identifier_2 || "—"}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{stageLabel(item.stage) || "—"}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{item.lifecycle_status || "—"}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{item.health_status || "—"}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{targetReason(item) || "—"}</LocalOverlayLink></TableCell>
                          <TableCell><LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{optionLabel(pageContract, "calendar_status", item.status).toLowerCase() || item.status}</LocalOverlayLink></TableCell>
                        </TableRow>
                      );
                    }) : (
                      <TableRow><TableCell colSpan={9} style={{ padding: 10, textAlign: "center", color: "var(--muted)" }}>{copy(pageContract, "calendar.drive.no_animals")}</TableCell></TableRow>
                    )}
                  </TableBody>
                </Table>
              </div>
              <div style={{ padding: 12, display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 10, borderTop: "1px solid var(--border)" }}>
                {prevHref ? (
                  <Link href={prevHref} className="btn">
                    <ChevronLeft className="ic" /> {copy(pageContract, "calendar.drive.previous_page")}
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true" style={{ opacity: 0.45, pointerEvents: "none" }}>
                    <ChevronLeft className="ic" /> {copy(pageContract, "calendar.drive.previous_page")}
                  </span>
                )}
                <span className="sub">{copy(pageContract, "calendar.drive.page_label")} {page}</span>
                {nextHref ? (
                  <Link href={nextHref} className="btn btn-primary">
                    {copy(pageContract, "calendar.drive.next_page")} <ChevronRight className="ic" />
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true" style={{ opacity: 0.45, pointerEvents: "none" }}>
                    {copy(pageContract, "calendar.drive.next_page")} <ChevronRight className="ic" />
                  </span>
                )}
              </div>
            </>
          )}
        </div>
            </section>
          </Box>
        </MuiGrid>

        <MuiGrid size={{ xs: 12, md: 4 }}>
          <Card>
            <Box sx={{ p: 3 }}>
              <Stack spacing={2}>
                <Stack spacing={0.5}>
                  <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4 }}>
                    {copy(pageContract, "calendar.breadcrumb.vaccination")}
                  </Typography>
                  <Typography variant="body2" sx={{ color: "text.primary" }}>{summary.park_name}</Typography>
                </Stack>
                <Stack spacing={0.5}>
                  <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4 }}>
                    {copy(pageContract, "calendar.drive.owner")}
                  </Typography>
                  <Typography variant="body2" sx={{ color: "text.primary" }}>{summary.owner_label}</Typography>
                </Stack>
              </Stack>
            </Box>

            {summary.vaccine_labels.length ? (
              <>
                <Divider sx={{ borderStyle: "dashed" }} />
                <Box sx={{ p: 3 }}>
                  <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4, mb: 1, display: "block" }}>
                    Vaccines
                  </Typography>
                  <div className="vchips">
                    {summary.vaccine_labels.map((label) => (<span key={label} className="tag t-mut">{label}</span>))}
                  </div>
                </Box>
              </>
            ) : null}

            {chips.length ? (
              <>
                <Divider sx={{ borderStyle: "dashed" }} />
                <Box sx={{ p: 3 }}>
                  <Typography variant="caption" sx={{ color: "text.disabled", textTransform: "uppercase", letterSpacing: 0.4, mb: 1, display: "block" }}>
                    Coverage
                  </Typography>
                  <div className="chips" style={{ gap: "var(--sp-1h)" }}>
                    {chips.map((chip) => (
                      <span key={chip.key} className={`sc ${driveStatusClass(chip.key)}`}>
                        <span className={`d c-${driveStatusClass(chip.key)}`} />
                        {chip.count} {copy(pageContract, "calendar.drive.doses").toLowerCase()} {optionLabel(pageContract, "calendar_status", chip.key).toLowerCase()}
                      </span>
                    ))}
                  </div>
                </Box>
              </>
            ) : null}
          </Card>
        </MuiGrid>
      </MuiGrid>

      <HerdPassportLocalDrawer
        items={drawerItems}
        initialSelectedId={selectedGoatId}
        closeHref={closePassportHref}
        reproductiveIdempotencyKey={reproductiveIdempotencyKey}
        returnTo={closePassportHref}
        pageContract={pageContract}
      />
    </div>
  );
}
