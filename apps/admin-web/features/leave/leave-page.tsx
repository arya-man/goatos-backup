import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { CalendarOff } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import { controlEnabled, control, copy, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
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

const PATHNAME = "/leave";
const PAGE_SIZE = 20;
const STATUS_FILTERS = ["", "pending", "approved", "rejected", "withdrawn"] as const;

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
  const feedback = { status: one(sp, "lv_status"), code: one(sp, "lv_code") };

  const mayDecide = controlEnabled(pageContract, "decide_leave", false);
  const mayList = controlEnabled(pageContract, "leave_list", false);
  const mayConfigure = controlEnabled(pageContract, "leave_config", false);

  const [queue, list, config] = await Promise.all([
    mayDecide ? listAdminWebLeaveApprovals({ limit: PAGE_SIZE, cursor: queueCursor || undefined }) : Promise.resolve(null),
    mayList ? listAdminLeaveRequests({ status: statusFilter || undefined, limit: PAGE_SIZE, cursor: listCursor || undefined }) : Promise.resolve(null),
    mayConfigure ? getLeaveApprovalConfig() : Promise.resolve(null),
  ]);
  const authError = firstAuthRequiredError(queue, list, config);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const queueRows: LeaveRequest[] = queue?.ok ? queue.data.items : [];
  const queueNext = queue?.ok ? queue.data.next_cursor : "";
  const listRows: LeaveRequest[] = list?.ok ? list.data.items : [];
  const listNext = list?.ok ? list.data.next_cursor : "";
  const queueTable = table(pageContract, "leave-approvals");
  const listTable = table(pageContract, "leave-requests");
  const queueLabels = tableLabels(pageContract, "leave-approvals");
  const listLabels = tableLabels(pageContract, "leave-requests");
  const returnTo = hrefWith(sp, { lv_status: null, lv_code: null });
  const feedbackText = feedbackCopy(pageContract, feedback.status, feedback.code);

  return (
    <div className="screen on">
      <LeaveActionTelemetry status={feedback.status} code={feedback.code} />
      <div className="phead">
        <div>
          <div className="crumb">
            <b>{t("page.title")}</b>
          </div>
          <h1>{t("page.title")}</h1>
          <div className="sub">{t("page.subtitle")}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {feedbackText ? (
        <div className={feedback.status === "error" ? "alert" : "alert ok"} data-testid="leave-feedback" style={{ marginBottom: 14 }}>
          {feedbackText}
        </div>
      ) : null}

      {mayConfigure && config?.ok ? <LeaveConfigPanel config={config.data.config} pageContract={pageContract} /> : null}

      {mayDecide ? (
        <section className="card" data-testid="leave-queue" style={{ marginBottom: 16 }}>
          <div className="chead">
            <h3>{queueTable.title}</h3>
          </div>
          {queue && !queue.ok ? (
            <div className="alert" style={{ margin: 12 }}>
              {queue.error.code ?? queue.error.kind} · {queue.error.message}
            </div>
          ) : null}
          {queueRows.length === 0 ? (
            <div className="empty" style={{ padding: 24 }}>
              <CalendarOff size={20} />
              <div className="small muted" style={{ marginTop: 8 }}>
                {t("queue.empty")}
              </div>
            </div>
          ) : (
            <div className="tablewrap" style={{ overflowX: "auto" }}>
              <table className="tbl">
                <thead>
                  <tr>
                    {queueLabels.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {queueRows.map((row) => (
                    <tr key={row.leave_request_id} data-testid="leave-queue-row">
                      <td>
                        <b>{row.person_name}</b>
                        {row.park_label ? <div className="small muted">{row.park_label}</div> : null}
                        {row.designation ? <div className="small muted">{row.designation}</div> : null}
                      </td>
                      <td>
                        {row.dates_label}
                        <div className="small muted">{row.raised_at_label}</div>
                      </td>
                      <td style={{ maxWidth: 320, whiteSpace: "normal" }}>{row.reason}</td>
                      <td>
                        {row.my_slot_label ? <Tag tone="info">{row.my_slot_label}</Tag> : null}
                        <div className="small muted" style={{ marginTop: 4 }}>
                          {row.status_line}
                        </div>
                      </td>
                      <td>
                        <div style={{ display: "flex", flexDirection: "column", gap: 6, minWidth: 220 }}>
                          <form action={approveLeaveAction}>
                            <input type="hidden" name="leave_request_id" value={row.leave_request_id} />
                            <input type="hidden" name="return_to" value={returnTo} />
                            <button type="submit" className="btn primary" data-testid="leave-approve">
                              {t("action.approve")}
                            </button>
                          </form>
                          <form action={rejectLeaveAction} style={{ display: "flex", gap: 6, alignItems: "flex-start" }}>
                            <input type="hidden" name="leave_request_id" value={row.leave_request_id} />
                            <input type="hidden" name="return_to" value={returnTo} />
                            <input
                              name="reason"
                              className="inp"
                              required
                              placeholder={t("reject.reason_hint")}
                              aria-label={t("reject.reason")}
                              data-testid="leave-reject-reason"
                              style={{ flex: 1 }}
                            />
                            <button type="submit" className="btn dng" data-testid="leave-reject">
                              {t("action.reject")}
                            </button>
                          </form>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {queueNext ? (
            <div className="pager" style={{ padding: 12 }}>
              <Link href={hrefWith(sp, { q_cursor: queueNext, lv_status: null, lv_code: null })} className="btn">
                {t("action.next")}
              </Link>
            </div>
          ) : null}
        </section>
      ) : null}

      {mayList ? (
        <section className="card" data-testid="leave-list">
          <div className="chead" style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <h3 style={{ marginRight: "auto" }}>{listTable.title}</h3>
            <div className="subtabs">
              {STATUS_FILTERS.map((key) => (
                <Link
                  key={key || "all"}
                  href={hrefWith(sp, { status: key || null, cursor: null, lv_status: null, lv_code: null })}
                  replace
                  scroll={false}
                  className={statusFilter === key ? "on" : ""}
                >
                  {key ? t(`filter.status.${key}`) : t("filter.status.all")}
                </Link>
              ))}
            </div>
          </div>
          {list && !list.ok ? (
            <div className="alert" style={{ margin: 12 }}>
              {list.error.code ?? list.error.kind} · {list.error.message}
            </div>
          ) : null}
          {listRows.length === 0 ? (
            <div className="empty" style={{ padding: 24 }}>
              <div className="small muted">{t("list.empty")}</div>
            </div>
          ) : (
            <div className="tablewrap" style={{ overflowX: "auto" }}>
              <table className="tbl">
                <thead>
                  <tr>
                    {listLabels.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {listRows.map((row) => (
                    <tr key={row.leave_request_id} data-testid="leave-list-row">
                      <td>
                        <b>{row.person_name}</b>
                        {row.designation ? <div className="small muted">{row.designation}</div> : null}
                      </td>
                      <td>{row.park_label ?? ""}</td>
                      <td>{row.dates_label}</td>
                      <td style={{ maxWidth: 320, whiteSpace: "normal" }}>{row.reason}</td>
                      <td>
                        <Tag tone={statusTone(row.status)}>{row.status_label}</Tag>
                        <div className="small muted" style={{ marginTop: 4 }}>
                          {row.status_line}
                        </div>
                      </td>
                      <td>{row.raised_at_label}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {listNext ? (
            <div className="pager" style={{ padding: 12 }}>
              <Link href={hrefWith(sp, { cursor: listNext, lv_status: null, lv_code: null })} className="btn">
                {t("action.next")}
              </Link>
            </div>
          ) : null}
        </section>
      ) : !mayDecide ? (
        <div className="alert">{control(pageContract, "leave_list").disabled_reason ?? ""}</div>
      ) : null}
    </div>
  );
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
