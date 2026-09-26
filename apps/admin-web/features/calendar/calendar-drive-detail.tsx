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
import Button from "@mui/material/Button";
import CardHeader from "@mui/material/CardHeader";
import TextField from "@mui/material/TextField";
import InputAdornment from "@mui/material/InputAdornment";
import LinearProgress from "@mui/material/LinearProgress";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { LinkButton } from "@/components/minimal/link-button";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { PageHeader } from "@/components/app/page-header";
import { type AdminUiPageContract, copy, optionLabel } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { fmtDate as fmtIstDate, humanizeEnum, joinParts } from "@/lib/format";
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


  const cell = { whiteSpace: "nowrap" } as const;
  const headers = [
    "calendar.drive.display_id_header",
    "calendar.drive.shed_header",
    "calendar.drive.tag_1_header",
    "calendar.drive.tag_2_header",
    "calendar.drive.stage_header",
    "calendar.drive.lifecycle_header",
    "calendar.drive.health_header",
    "calendar.drive.reason_header",
    "calendar.drive.status_header",
  ];
  const statusText = (status: string) => optionLabel(pageContract, "calendar_status", status) || status;
  const pager = (
    <Stack direction="row" spacing={1.5} sx={{ p: 2, alignItems: "center", justifyContent: "flex-end", borderTop: 1, borderColor: "divider" }}>
      {prevHref ? (
        <LinkButton href={prevHref} variant="outlined" color="inherit" startIcon={<ChevronLeft className="ic" />} sx={{ minHeight: 44 }}>
          {copy(pageContract, "calendar.drive.previous_page")}
        </LinkButton>
      ) : (
        <Button disabled variant="outlined" color="inherit" startIcon={<ChevronLeft className="ic" />} sx={{ minHeight: 44 }}>
          {copy(pageContract, "calendar.drive.previous_page")}
        </Button>
      )}
      <Typography variant="body2" sx={{ color: "text.secondary", whiteSpace: "nowrap" }}>
        {copy(pageContract, "calendar.drive.page_label")} {page}
      </Typography>
      {nextHref ? (
        <LinkButton href={nextHref} variant="contained" endIcon={<ChevronRight className="ic" />} sx={{ minHeight: 44 }}>
          {copy(pageContract, "calendar.drive.next_page")}
        </LinkButton>
      ) : (
        <Button disabled variant="contained" endIcon={<ChevronRight className="ic" />} sx={{ minHeight: 44 }}>
          {copy(pageContract, "calendar.drive.next_page")}
        </Button>
      )}
    </Stack>
  );

  return (
    <div className="screen on">
      {crumb}
      <MuiGrid container spacing={3}>
        <MuiGrid size={{ xs: 12, md: 8 }}>
          <Card>
            <CardHeader title={`${event.title} · ${summary.park_name}`} />
            <Stack spacing={1.5} sx={{ p: 3 }}>
              <Stack direction="row" spacing={1} sx={{ alignItems: "baseline", flexWrap: "wrap" }}>
                <Typography variant="h3">{pct}%</Typography>
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {coverage.completed} {copy(pageContract, "calendar.drive.of")} {coverage.total}{" "}
                  {copy(pageContract, coverage.usesAnimals ? "calendar.drive.animals" : "calendar.drive.doses")}
                </Typography>
              </Stack>
              <LinearProgress variant="determinate" value={pct} color="primary" sx={{ height: 8 }} />
              <Typography variant="body2" sx={{ color: "text.secondary" }}>
                <Box component="b" sx={{ color: "text.primary" }}>{summary.sheds_completed}</Box> {copy(pageContract, "calendar.drive.of")} {summary.shed_count}{" "}
                {copy(pageContract, "calendar.drive.sheds_done_suffix")} · {copy(pageContract, "calendar.drive.owner")} {summary.owner_label}
              </Typography>
            </Stack>
          </Card>
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
                  <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
                    {summary.vaccine_labels.map((label) => (<Label key={label} variant="soft">{label}</Label>))}
                  </Stack>
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

        <MuiGrid size={12}>
          <Card>
            <CardHeader
              title={copy(pageContract, "calendar.drive.animal_roster")}
              action={
                <Typography variant="body2" sx={{ color: "text.secondary", whiteSpace: "nowrap" }}>
                  {copy(pageContract, "calendar.drive.page_label")} {page} · {rosterItems.length} {copy(pageContract, "calendar.drive.rows_label")}
                </Typography>
              }
              sx={{ "& .MuiCardHeader-action": { alignSelf: "center" } }}
            />
            <Box
              component="form"
              action={detailPath}
              sx={{ p: 2.5, gap: 1.5, display: "flex", flexWrap: "wrap", alignItems: "center" }}
            >
              {hiddenInputs(sp, new Set(["q", "cursor", "page", "cursor_stack"]))}
              <TextField
                name="q"
                defaultValue={targetSearch}
                placeholder={copy(pageContract, "calendar.drive.search_placeholder")}
                sx={{ flex: "1 1 260px" }}
                slotProps={{
                  input: {
                    startAdornment: (
                      <InputAdornment position="start">
                        <Search className="ic" style={{ width: 18 }} aria-hidden="true" />
                      </InputAdornment>
                    ),
                  },
                }}
              />
              <Button type="submit" variant="contained" size="large" sx={{ minHeight: 48 }}>
                {copy(pageContract, "calendar.drive.search_action")}
              </Button>
              {targetSearch ? (
                <LinkButton href={clearSearchHref} variant="outlined" color="inherit" size="large" sx={{ minHeight: 48 }}>
                  {copy(pageContract, "calendar.drive.clear_search")}
                </LinkButton>
              ) : null}
            </Box>
            {targetsError ? (
              <Alert severity="error" sx={{ mx: 2.5, mb: 2.5 }}>
                <b>{targetsError.code ?? targetsError.kind}</b>&nbsp;{targetsError.message}
              </Alert>
            ) : (
              <>
                {/* Laptop: the template table kit (Scrollbar + Table minWidth, nowrap identity cells). */}
                <Box sx={{ display: { xs: "none", md: "block" } }}>
                  <Scrollbar>
                    <Table sx={{ minWidth: 960, "& th, & td, & td .celllink": { overflowWrap: "normal", wordBreak: "normal" }, "& td .celllink": { whiteSpace: "nowrap" } }}>
                      <TableHead>
                        <TableRow>
                          {headers.map((key) => (
                            <TableCell key={key} component="th" sx={cell}>{copy(pageContract, key)}</TableCell>
                          ))}
                        </TableRow>
                      </TableHead>
                      <TableBody>
                        {rosterItems.length ? rosterItems.map((item) => {
                          const passportHref = hrefWithParam(detailPath, sp, "goat_passport", item.animal_id);
                          const linked = (value: React.ReactNode) => (
                            <LocalOverlayLink href={passportHref} className="celllink" scroll={false}>{value}</LocalOverlayLink>
                          );
                          return (
                            <TableRow key={item.animal_id} hover>
                              <TableCell sx={cell}>{linked(<span className="gid">{item.display_id || "—"}</span>)}</TableCell>
                              <TableCell sx={cell}>{linked(targetLocationLabel(item))}</TableCell>
                              <TableCell sx={{ ...cell, fontVariantNumeric: "tabular-nums" }}>{linked(item.animal_identifier_1 || "—")}</TableCell>
                              <TableCell sx={{ ...cell, fontVariantNumeric: "tabular-nums" }}>{linked(item.animal_identifier_2 || "—")}</TableCell>
                              <TableCell sx={cell}>{linked(stageLabel(item.stage) || "—")}</TableCell>
                              <TableCell sx={cell}>{linked(humanizeEnum(item.lifecycle_status) || "—")}</TableCell>
                              <TableCell sx={cell}>{linked(humanizeEnum(item.health_status) || "—")}</TableCell>
                              <TableCell>{linked(targetReason(item) || "—")}</TableCell>
                              <TableCell sx={cell}>{linked(<Label variant="soft">{statusText(item.status)}</Label>)}</TableCell>
                            </TableRow>
                          );
                        }) : (
                          <TableRow>
                            <TableCell colSpan={headers.length} sx={{ textAlign: "center", color: "text.secondary" }}>{copy(pageContract, "calendar.drive.no_animals")}</TableCell>
                          </TableRow>
                        )}
                      </TableBody>
                    </Table>
                  </Scrollbar>
                </Box>
                {/* Phone: stacked rows, one animal per row (identity line, location + stage, status). */}
                <Box sx={{ display: { xs: "block", md: "none" }, borderTop: 1, borderColor: "divider" }}>
                  {rosterItems.length ? rosterItems.map((item) => {
                    const passportHref = hrefWithParam(detailPath, sp, "goat_passport", item.animal_id);
                    return (
                      <LocalOverlayLink key={item.animal_id} href={passportHref} className="celllink" scroll={false}>
                        <Stack spacing={0.5} sx={{ px: 2.5, py: 1.5, minHeight: 44, borderBottom: 1, borderColor: "divider" }}>
                          <Stack direction="row" spacing={1} sx={{ alignItems: "center", justifyContent: "space-between" }}>
                            <span className="gid">{item.display_id || "—"}</span>
                            <Label variant="soft">{statusText(item.status)}</Label>
                          </Stack>
                          <Typography variant="body2" sx={{ fontVariantNumeric: "tabular-nums", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                            {item.animal_identifier_1 || "—"}{item.animal_identifier_2 ? ` · ${item.animal_identifier_2}` : ""}
                          </Typography>
                          <Typography variant="caption" sx={{ color: "text.secondary" }}>
                            {joinParts([item.shed_name ? targetLocationLabel(item) : null, stageLabel(item.stage), item.lifecycle_status ? humanizeEnum(item.lifecycle_status) : null, item.health_status ? humanizeEnum(item.health_status) : null, targetReason(item)])}
                          </Typography>
                        </Stack>
                      </LocalOverlayLink>
                    );
                  }) : (
                    <Typography variant="body2" sx={{ p: 2.5, textAlign: "center", color: "text.secondary" }}>{copy(pageContract, "calendar.drive.no_animals")}</Typography>
                  )}
                </Box>
                {pager}
              </>
            )}
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
