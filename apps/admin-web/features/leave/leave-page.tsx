import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import Alert from "@mui/material/Alert";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import MuiCard from "@mui/material/Card";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { PageHeader } from "@/components/app/page-header";
import { EmptyState } from "@/components/app/empty-state";
import { StatStrip } from "@/components/minimal/widgets/stat-strip";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { redirect } from "next/navigation";
import { CalendarCheck2, CalendarOff, CircleAlert, CircleCheck, CircleDot, CircleSlash, Undo2 } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import { controlEnabled, control, copy, table, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getLeaveApprovalConfig,
  listAdminLeaveRequests,
  listAdminWebLeaveApprovals,
  type LeaveRequest,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { approveLeaveAction, rejectLeaveAction } from "./actions";
import { LeaveConfigPanel } from "./leave-config-panel";
import { LeaveActionTelemetry } from "./leave-telemetry";
import { LeaveRejectDialog } from "./leave-reject-dialog";
import { LeaveTableChrome } from "./leave-toolbar";

const PATHNAME = "/leave";
const DEFAULT_PAGE_SIZE = 20;
const STATUS_FILTERS = ["", "pending", "approved", "rejected", "withdrawn"] as const;

// Table cell presentation (template user-table-row: Avatar + name over muted secondary lines).
const PERSON_SX = { display: "flex", alignItems: "center", gap: 1.5, minWidth: 0 } as const;
const AVATAR_SX = { bgcolor: "primary.lighter", color: "primary.dark", typography: "caption", fontWeight: "fontWeightBold" } as const;
const SUB_SX = { display: "block", typography: "caption", color: "text.secondary" } as const;
const REASON_SX = { maxWidth: { xs: "22ch", md: "36ch" }, whiteSpace: "normal" } as const;
const CLAMP_SX = { display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden" } as const;

/**
 * /leave (maintainer decisions 2026-09-10, docs/features/leave-requests/plan.md). Three parts,
 * each gated by its own compiled control off the page contract, never a role string:
 *
 *  - "Waiting for you": the caller's open queue (decide_leave / leave.approve) -- the park head
 *    sees their park's park-head line, HR the HR line, the CEO both; approve and reject post to
 *    the SAME routes the phone's outbox drains through.
 *  - "All leave requests": every request, any status (leave_list / leave.read: CEO + HR).
 *  - "Who approves leave": the two routing ticks (leave_config / leave.approval.configure, CEO).
 *
 * Every visible word is backend copy from the page contract or a backend-composed field on the
 * row (dates_label, status_line, status_label, my_slot_label). Server component: the selected
 * window is fetched and rendered, never the whole history.
 */
export async function LeavePage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const t = (key: string) => copy(pageContract, key);
  const statusFilter = STATUS_FILTERS.find((s) => s === (one(sp, "status") ?? "")) ?? "";
  const listCursor = one(sp, "cursor") ?? "";
  const queueCursor = one(sp, "q_cursor") ?? "";
  // Rows-per-page comes from the contract, like every other table on the admin; the URL picks one.
  const queueSizes = tablePageSizes(pageContract, "leave-approvals");
  const listSizes = tablePageSizes(pageContract, "leave-requests");
  const queueSize = pickSize(one(sp, "q_limit"), queueSizes);
  const listSize = pickSize(one(sp, "limit"), listSizes);
  // Toolbar filters. The backend list takes only status/limit/cursor, so these narrow the window
  // the route has already fetched — never a second request, never a mutation.
  const filters = {
    q: (one(sp, "q") ?? "").trim(),
    park: one(sp, "park") ?? "",
    designation: one(sp, "designation") ?? "",
    from: one(sp, "from") ?? "",
    to: one(sp, "to") ?? "",
  };
  const feedback = { status: one(sp, "lv_status"), code: one(sp, "lv_code") };

  const mayDecide = controlEnabled(pageContract, "decide_leave", false);
  const mayList = controlEnabled(pageContract, "leave_list", false);
  const mayConfigure = controlEnabled(pageContract, "leave_config", false);

  const [queue, list, config] = await Promise.all([
    mayDecide ? listAdminWebLeaveApprovals({ limit: queueSize, cursor: queueCursor || undefined }) : Promise.resolve(null),
    mayList ? listAdminLeaveRequests({ status: statusFilter || undefined, limit: listSize, cursor: listCursor || undefined }) : Promise.resolve(null),
    mayConfigure ? getLeaveApprovalConfig() : Promise.resolve(null),
  ]);
  const authError = firstAuthRequiredError(queue, list, config);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const queueAll: LeaveRequest[] = queue?.ok ? listOrEmpty(queue.data.items) : [];
  const queueRows = applyFilters(queueAll, filters);
  const queueNext = queue?.ok ? queue.data.next_cursor : "";
  const listAll: LeaveRequest[] = list?.ok ? listOrEmpty(list.data.items) : [];
  const listRows = applyFilters(listAll, filters);
  const listNext = list?.ok ? list.data.next_cursor : "";
  const queueTable = table(pageContract, "leave-approvals");
  const listTable = table(pageContract, "leave-requests");
  const queueLabels = tableLabels(pageContract, "leave-approvals");
  const listLabels = tableLabels(pageContract, "leave-requests");
  const returnTo = hrefWith(sp, { lv_status: null, lv_code: null });
  const currentQuery = queryOf(sp);
  const parkOptions = uniqueOf([...queueAll, ...listAll], (r) => r.park_label ?? "");
  const designationOptions = uniqueOf([...queueAll, ...listAll], (r) => r.designation ?? "");
  // The status tabs already carry the taxonomy; the strip counts the window in hand.
  const statusCounts = {
    pending: listAll.filter((r) => r.status === "pending").length,
    approved: listAll.filter((r) => r.status === "approved").length,
    rejected: listAll.filter((r) => r.status === "rejected").length,
    withdrawn: listAll.filter((r) => r.status === "withdrawn").length,
  };
  const feedbackText = feedbackCopy(pageContract, feedback.status, feedback.code);

  return (
    <div className="screen on leave-page">
      <LeaveActionTelemetry status={feedback.status} code={feedback.code} />
      <PageHeader
        title={t("page.title")}
        crumbs={[{ label: copy(pageContract, "crumb", "Operations") }, { label: copy(pageContract, "crumb.section", "People") }, { label: t("page.title") }]}
      />

      <div className="kit-enter">
        {feedbackText ? (
          <Alert
            severity={feedback.status === "error" ? "error" : "success"}
            icon={feedback.status === "error" ? <CircleAlert size={18} aria-hidden="true" /> : <CircleCheck size={18} aria-hidden="true" />}
            data-testid="leave-feedback"
            role="status"
            sx={{ mb: 2 }}
          >
            {feedbackText}
          </Alert>
        ) : null}

        {mayList && Object.values(statusCounts).some((n) => Number(n) > 0) ? (
          // Spec §5 strip over the status taxonomy the tabs already use; hidden while every count is 0.
          // Template invoice list: the InvoiceAnalytic row inside its own Card.
          <MuiCard sx={{ mb: 2 }}>
            <StatStrip
              cells={[
                { key: "pending", icon: <CircleDot aria-hidden="true" />, label: t("filter.status.pending"), value: String(statusCounts.pending), tone: "warning" },
                { key: "approved", icon: <CircleCheck aria-hidden="true" />, label: t("filter.status.approved"), value: String(statusCounts.approved), tone: "success" },
                { key: "rejected", icon: <CircleSlash aria-hidden="true" />, label: t("filter.status.rejected"), value: String(statusCounts.rejected), tone: "error" },
                { key: "withdrawn", icon: <Undo2 aria-hidden="true" />, label: t("filter.status.withdrawn"), value: String(statusCounts.withdrawn), tone: "neutral" },
              ]}
            />
          </MuiCard>
        ) : null}

        {mayConfigure && config?.ok ? (
          <div>
            <LeaveConfigPanel config={config.data.config} pageContract={pageContract} />
          </div>
        ) : null}

        {mayDecide ? (
          <div>
            <Card className="kit-tablecard" data-testid="leave-queue" sx={{ mb: 2 }}>
              <CardHeader
                sx={{ pt: 2.5, px: 3, pb: 1.5, mb: 2, alignItems: "center" }}
                title={
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
                    <CalendarCheck2 className="ic" style={{ width: 18, color: "var(--primary)" }} aria-hidden="true" />
                    {queueTable.title}
                  </span>
                }
                action={queueRows.length ? <Tag tone="info">{queueRows.length}</Tag> : null}
              />
              {queue && !queue.ok ? (
                <Alert severity="error" icon={<CircleAlert size={18} aria-hidden="true" />} role="alert" sx={{ mx: 3, mb: 2 }}>
                  <b>{queue.error.code ?? queue.error.kind}</b> · {queue.error.message}
                </Alert>
              ) : null}
              {queueRows.length === 0 ? (
                <EmptyState icon={<CalendarOff className="ic" aria-hidden="true" />} title={t("queue.empty")} sx={{ mx: 3, mb: 3 }} />
              ) : (
                <LeaveTableChrome
                  tableAriaLabel={queueTable.title}
                  toolbar={{
                    value: filters,
                    parkOptions,
                    designationOptions,
                    basePath: PATHNAME,
                    currentQuery,
                    copyFor: (key) => copy(pageContract, key),
                    shown: queueRows.length,
                    total: queueAll.length,
                    csv: toCsv(queueRows),
                    csvName: "leave-approvals.csv",
                  }}
                  footer={{
                    shown: queueRows.length,
                    nextHref: queueNext ? hrefWith(sp, { q_cursor: queueNext, lv_status: null, lv_code: null }) : "",
                    hasPrevious: Boolean(queueCursor),
                    pageSizeOptions: queueSizes,
                    pageSize: queueSize,
                    basePath: PATHNAME,
                    currentQuery,
                    limitParam: "q_limit",
                    cursorParam: "q_cursor",
                    copyFor: (key) => copy(pageContract, key),
                  }}
                >
                  <Table stickyHeader>
                    <TableHead>
                      <TableRow>
                        {queueLabels.map((label) => (
                          <TableCell component="th" key={label}>{label}</TableCell>
                        ))}
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {queueRows.map((row) => (
                        <TableRow key={row.leave_request_id} data-testid="leave-queue-row">
                          <TableCell>
                            <Box component="span" sx={PERSON_SX}>
                              <Avatar aria-hidden="true" sx={AVATAR_SX}>{initials(row.person_name)}</Avatar>
                              <Box component="span" sx={{ minWidth: 0 }}>
                                <b>{row.person_name}</b>
                                {row.park_label ? <Box component="span" sx={SUB_SX}>{row.park_label}</Box> : null}
                                {row.designation ? <Box component="span" sx={SUB_SX}>{row.designation}</Box> : null}
                              </Box>
                            </Box>
                          </TableCell>
                          <TableCell>
                            {row.dates_label}
                            <Box component="span" sx={SUB_SX}>{row.raised_at_label}</Box>
                          </TableCell>
                          <TableCell sx={REASON_SX}>
                            {/* Two lines, then ellipsis; the full text is the cell's own tooltip. */}
                            <Box component="span" sx={CLAMP_SX} title={row.reason}>{row.reason}</Box>
                          </TableCell>
                          <TableCell>
                            {row.my_slot_label ? <Tag tone="info">{row.my_slot_label}</Tag> : null}
                            <Box component="span" sx={{ ...SUB_SX, mt: 0.5 }}>
                              {row.status_line}
                            </Box>
                          </TableCell>
                          <TableCell>
                            <Box sx={{ display: "flex", flexDirection: "column", gap: 1 }}>
                              <form action={approveLeaveAction}>
                                <input type="hidden" name="leave_request_id" value={row.leave_request_id} />
                                <input type="hidden" name="return_to" value={returnTo} />
                                <Button variant="contained" color="primary" type="submit" size="small" data-testid="leave-approve">
                                  {t("action.approve")}
                                </Button>
                              </form>
                              {/* Rejecting is destructive: it happens in a dialog with a labelled,
                                  required reason, never from a bare input in the row. */}
                              <LeaveRejectDialog
                                leaveRequestId={row.leave_request_id}
                                personName={row.person_name}
                                datesLabel={row.dates_label}
                                returnTo={returnTo}
                                action={rejectLeaveAction}
                                labels={{
                                  trigger: t("action.reject"),
                                  title: copy(pageContract, "reject.title", t("action.reject")),
                                  body: copy(pageContract, "reject.body", t("reject.reason_hint")),
                                  reason: t("reject.reason"),
                                  reasonHint: t("reject.reason_hint"),
                                  cancel: copy(pageContract, "action.cancel", "Cancel"),
                                  confirm: t("action.reject"),
                                }}
                              />
                            </Box>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </LeaveTableChrome>
              )}
            </Card>
          </div>
        ) : null}

        {mayList ? (
          <div>
            <Card className="kit-tablecard" data-testid="leave-list">
              <CardHeader
                sx={{
                  pt: 2.5,
                  px: 3,
                  pb: 1.5,
                  mb: 2,
                  alignItems: "center",
                  gap: 1.5,
                  flexWrap: "wrap",
                  [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 0 auto" }, minWidth: 0, maxWidth: "100%" },
                }}
                title={listTable.title}
                action={
                  <AnimatedTabs
                    variant="pill"
                    ariaLabel={t("filter.status.all")}
                    value={statusFilter || "all"}
                    items={STATUS_FILTERS.map((key) => ({
                      value: key || "all",
                      label: key ? t(`filter.status.${key}`) : t("filter.status.all"),
                      // Counts are of the window in hand; the backend returns no per-status totals.
                      count: key ? listAll.filter((r) => r.status === key).length : listAll.length,
                      href: hrefWith(sp, { status: key || null, cursor: null, lv_status: null, lv_code: null }),
                    }))}
                  />
                }
              />
              {list && !list.ok ? (
                <Alert severity="error" icon={<CircleAlert size={18} aria-hidden="true" />} role="alert" sx={{ mx: 3, mb: 2 }}>
                  <b>{list.error.code ?? list.error.kind}</b> · {list.error.message}
                </Alert>
              ) : null}
              {listRows.length === 0 ? (
                <EmptyState icon={<CalendarOff className="ic" aria-hidden="true" />} title={t("list.empty")} sx={{ mx: 3, mb: 3 }} />
              ) : (
                <LeaveTableChrome
                  tableAriaLabel={listTable.title}
                  toolbar={{
                    value: filters,
                    parkOptions,
                    designationOptions,
                    basePath: PATHNAME,
                    currentQuery,
                    copyFor: (key) => copy(pageContract, key),
                    shown: listRows.length,
                    total: listAll.length,
                    csv: toCsv(listRows),
                    csvName: "leave-requests.csv",
                  }}
                  footer={{
                    shown: listRows.length,
                    nextHref: listNext ? hrefWith(sp, { cursor: listNext, lv_status: null, lv_code: null }) : "",
                    hasPrevious: Boolean(listCursor),
                    pageSizeOptions: listSizes,
                    pageSize: listSize,
                    basePath: PATHNAME,
                    currentQuery,
                    limitParam: "limit",
                    cursorParam: "cursor",
                    copyFor: (key) => copy(pageContract, key),
                  }}
                >
                  <Table stickyHeader>
                    <TableHead>
                      <TableRow>
                        {listLabels.map((label) => (
                          <TableCell component="th" key={label}>{label}</TableCell>
                        ))}
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {listRows.map((row) => (
                        <TableRow key={row.leave_request_id} data-testid="leave-list-row">
                          <TableCell>
                            <Box component="span" sx={PERSON_SX}>
                              <Avatar aria-hidden="true" sx={AVATAR_SX}>{initials(row.person_name)}</Avatar>
                              <Box component="span" sx={{ minWidth: 0 }}>
                                <b>{row.person_name}</b>
                                {row.designation ? <Box component="span" sx={SUB_SX}>{row.designation}</Box> : null}
                              </Box>
                            </Box>
                          </TableCell>
                          <TableCell>{row.park_label ?? ""}</TableCell>
                          <TableCell>{row.dates_label}</TableCell>
                          <TableCell sx={REASON_SX}>
                            <Box component="span" sx={CLAMP_SX} title={row.reason}>{row.reason}</Box>
                          </TableCell>
                          <TableCell>
                            <Tag tone={statusTone(row.status)}>{row.status_label}</Tag>
                            <Box component="span" sx={{ ...SUB_SX, mt: 0.5 }}>
                              {row.status_line}
                            </Box>
                          </TableCell>
                          <TableCell>{row.raised_at_label}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </LeaveTableChrome>
              )}
            </Card>
          </div>
        ) : !mayDecide ? (
          <div>
            <Alert severity="error">{control(pageContract, "leave_list").disabled_reason ?? ""}</Alert>
          </div>
        ) : null}
      </div>
    </div>
  );
}

/** The chosen rows-per-page: the URL's value when the contract offers it, else the first option. */
function pickSize(raw: string | undefined, options: number[]): number {
  const n = Number.parseInt(raw ?? "", 10);
  if (Number.isFinite(n) && options.includes(n)) return n;
  return options[0] ?? DEFAULT_PAGE_SIZE;
}

/** Distinct, sorted, non-empty values for a toolbar select. */
function uniqueOf(rows: LeaveRequest[], pick: (row: LeaveRequest) => string): string[] {
  return Array.from(new Set(rows.map(pick).filter(Boolean))).sort();
}

/**
 * Toolbar filters over the window already fetched. The backend list endpoint takes only
 * status/limit/cursor, so person, park, designation and the date range are applied here — they
 * narrow what is on screen and never trigger a second request or a mutation.
 */
function applyFilters(
  rows: LeaveRequest[],
  f: { q: string; park: string; designation: string; from: string; to: string },
): LeaveRequest[] {
  const q = f.q.toLowerCase();
  const fromMs = f.from ? Date.parse(f.from) : null;
  const toMs = f.to ? Date.parse(f.to) : null;
  const stamp = (label: string): number | null => {
    const m = /(\d{2})\/(\d{2})\/(\d{4})/.exec(label);
    return m ? Date.parse(`${m[3]}-${m[2]}-${m[1]}`) : null;
  };
  return rows.filter((row) => {
    if (q && !`${row.person_name} ${row.designation ?? ""} ${row.park_label ?? ""}`.toLowerCase().includes(q)) return false;
    if (f.park && (row.park_label ?? "") !== f.park) return false;
    if (f.designation && (row.designation ?? "") !== f.designation) return false;
    if (fromMs !== null || toMs !== null) {
      const d = stamp(row.dates_label);
      if (d === null) return false;
      if (fromMs !== null && d < fromMs) return false;
      if (toMs !== null && d > toMs) return false;
    }
    return true;
  });
}

/** Read-only CSV of exactly the rows on screen, handed to the toolbar's Export. */
function toCsv(rows: LeaveRequest[]): string {
  const head = ["person", "park", "designation", "dates", "reason", "status", "raised_at"];
  const body = rows.map((r) => [r.person_name, r.park_label ?? "", r.designation ?? "", r.dates_label, r.reason, r.status_label, r.raised_at_label]);
  return [head, ...body].map((r) => r.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(",")).join("\r\n");
}

/** The current query string, so a client control can patch one parameter and keep the rest. */
function queryOf(sp: RouteSearchParams): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  return query.toString();
}

/** Two letters for the row avatar; presentation only, it composes no copy. */
function initials(name: string): string {
  return name
    .split(/\s+/)
    .map((part) => part.charAt(0))
    .join("")
    .slice(0, 2)
    .toUpperCase();
}


function statusTone(status: LeaveRequest["status"]): Tone {
  switch (status) {
    case "approved":
      return "ok";
    case "rejected":
      return "dng";
    case "withdrawn":
      return "mut";
    default:
      return "warn";
  }
}

/** The action-outcome sentence: backend copy per outcome; an unknown error code shows the code. */
function feedbackCopy(pageContract: AdminUiPageContract, status?: string, code?: string): string {
  if (!status) return "";
  if (status === "success") {
    return code === "rejected" ? copy(pageContract, "feedback.rejected") : copy(pageContract, "feedback.approved");
  }
  if (code === "leave_slot_decided" || code === "leave_not_pending") return copy(pageContract, "feedback.already_decided");
  if (code === "reason_required") return copy(pageContract, "reject.reason_hint");
  return code ?? "";
}

function hrefWith(sp: RouteSearchParams, patch: Record<string, string | null>): string {
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
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}
