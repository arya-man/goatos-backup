import Link from "next/link";
import { Info, Search, Users, Video } from "lucide-react";
import { getVaccinationActionCenter, getVaccinationVerificationQueue } from "@/lib/api/server";
import type {
  ActionCenterObligation,
  ProcessIntegritySeverity,
  VaccinationQueueItem,
  WorkState,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { SEVERITY_META, SEVERITY_ORDER, WORK_STATE_ORDER } from "./process-integrity";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { WorkBoard } from "./work-board";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "—";
}

// ActionForm posts a per-row server action (Verify / Reject / Rework) against a real completion id.
function ActionForm({
  action,
  completionId,
  returnTo,
  reason,
  primary,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  completionId: string;
  returnTo: string;
  reason?: string;
  primary?: boolean;
  children: React.ReactNode;
}) {
  return (
    <form action={action} style={{ display: "inline" }}>
      <input type="hidden" name="completion_id" value={completionId} />
      <input type="hidden" name="return_to" value={returnTo} />
      {reason ? <input type="hidden" name="reason" value={reason} /> : null}
      <button type="submit" className={`btn sm${primary ? " p" : ""}`}>
        {children}
      </button>
    </form>
  );
}

export async function VaccinationActionCenterPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const view = one(sp, "bucket") === "verify" ? "verify" : "board";
  const stateFilter = (WORK_STATE_ORDER.find((s) => s === one(sp, "state")) ?? "all") as WorkState | "all";
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const { parkId, asOf } = backendScope(parseScope(sp));
  const scope = parseScope(sp);
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");

  // Board source = the real process-integrity Action Center contract (server-computed work state,
  // severity, owner, proof/verify state, next action). Verification queue = actionable completions.
  // Both honor the top-bar park scope (park_id) so the SOP/verification queue can't show other parks.
  const [actionCenter, queue] = await Promise.all([
    getVaccinationActionCenter({
      parkId,
      asOf,
      workState: stateFilter === "all" ? undefined : stateFilter,
      severity: severityFilter === "all" ? undefined : severityFilter,
      limit: 200,
    }),
    getVaccinationVerificationQueue({ parkId, limit: 200 }),
  ]);

  const items: ActionCenterObligation[] = actionCenter.ok ? actionCenter.data.items : [];
  const queueItems: VaccinationQueueItem[] = queue.ok ? queue.data.items : [];
  const nextCursor = actionCenter.ok ? actionCenter.data.next_cursor : undefined;

  // Server-authoritative counts per work state (not derived from the capped page).
  const stateCounts = new Map<WorkState, number>();
  if (actionCenter.ok) for (const c of actionCenter.data.counts_by_work_state) stateCounts.set(c.work_state, c.count);
  const totalCount = Array.from(stateCounts.values()).reduce((a, b) => a + b, 0);
  const overdueCount = stateCounts.get("overdue") ?? 0;
  const dueCount = stateCounts.get("due") ?? 0;

  // Filter links preserve the FULL top-bar scope (scopeHref) and layer the page filters on top — never
  // hand-rolled, so park/range/as_of/date_from/date_to are never dropped.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/action-center", scope, {}, {
      bucket: view === "verify" ? "verify" : undefined,
      severity: severityFilter,
      state: stateFilter,
      ...overrides,
    });
  }
  const verifyReturnTo = hrefWith({ bucket: "verify" });

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>Action Center</h1>
          <div className="sub">
            <b>Work</b> — every vaccination obligation grouped by computed work state. Execute, then verify, reject, or
            request rework (live). Each card carries its park, shed, owner chain, blocker, proof + verification state,
            and next action.
          </div>
        </div>
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            <Tag tone="ok">done</Tag> {actionMessage ?? "Action completed."}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>Action failed</b>&nbsp;{actionMessage ?? actionStatus}
          </div>
        )
      ) : null}

      {/* View switch — Status board vs the SOP/verification queue (mock #acView). */}
      <div className="subtabs">
        <Link href={hrefWith({ bucket: undefined })} className={view === "board" ? "on" : ""}>
          Status board
        </Link>
        <Link href={hrefWith({ bucket: "verify" })} className={view === "verify" ? "on" : ""}>
          SOP queues <span className="cbq">{queueItems.length}</span>
        </Link>
      </div>

      {!actionCenter.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{actionCenter.error.code ?? actionCenter.error.kind}</b>&nbsp;{actionCenter.error.message}
        </div>
      ) : null}
      {actionCenter.ok && !queue.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{queue.error.code ?? queue.error.kind}</b>&nbsp;{queue.error.message}
        </div>
      ) : null}

      {view === "verify" ? (
        // ===== SOP queues — verification surface (accept / reject / request rework) =====
        <section className="card">
          <div className="hd">
            <Video className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
            <h3>Awaiting verification</h3>
            <Tag tone={queueItems.length ? "warn" : "mut"}>{queueItems.length}</Tag>
            <div className="sp" style={{ flex: 1 }} />
            <span className="muted small">Recorded doses awaiting review</span>
          </div>
          {queueItems.length === 0 ? (
            <div className="bd">
              <p className="muted small" style={{ margin: 0 }}>
                Nothing awaiting verification. Recorded doses surface here once a published drive runs and proof is
                uploaded.
              </p>
            </div>
          ) : (
            <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Awaiting verification">
              <table>
                <thead>
                  <tr>
                    <th>Goat</th>
                    <th>Administered</th>
                    <th>Doses</th>
                    <th>Verify</th>
                  </tr>
                </thead>
                <tbody>
                  {queueItems.map((q) => (
                    <tr key={q.completion_id}>
                      <td>
                        <span className="gid">{shortId(q.goat_id)}</span>
                      </td>
                      <td>{fmtDate(q.administered_at)}</td>
                      <td>{q.doses}</td>
                      <td>
                        <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                          <ActionForm action={verifyCompletionAction} completionId={q.completion_id} returnTo={verifyReturnTo} primary>
                            Verify
                          </ActionForm>
                          <ActionForm action={rejectCompletionAction} completionId={q.completion_id} returnTo={verifyReturnTo} reason="rejected">
                            Reject
                          </ActionForm>
                          <ActionForm action={rejectCompletionAction} completionId={q.completion_id} returnTo={verifyReturnTo} reason="rework_requested">
                            Request rework
                          </ActionForm>
                          <Link href={`/goats/${q.goat_id}`} className="btn sm">
                            Passport
                          </Link>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      ) : (
        // ===== Status board — mock taskboard shell from the real Action Center process-integrity rows =====
        <>
          {/* Domain filter. Only the active vaccination module is visible in this slice. */}
          <div className="subtabs" id="acPillars" aria-label="Domains">
            <Link href={hrefWith({ severity: "all", state: "all" })} className="on">
              Vaccination <span className="cbq">{totalCount}</span>
            </Link>
          </div>

          {/* Quick tabs (mock #acQuickTabs) + My tasks + Filters. Counts are server-authoritative. */}
          <div id="acToggles" style={{ display: "flex", gap: 8, marginBottom: 14, flexWrap: "wrap", alignItems: "center" }}>
            <div className="subtabs" id="acQuickTabs" style={{ margin: 0 }}>
              <Link href={hrefWith({ state: "all" })} className={stateFilter === "all" ? "on" : ""}>
                All <span className="qc">{totalCount}</span>
              </Link>
              <Link href={hrefWith({ state: "overdue" })} className={stateFilter === "overdue" ? "on" : ""}>
                Overdue <span className="qc">{overdueCount}</span>
              </Link>
              <Link href={hrefWith({ state: "due" })} className={stateFilter === "due" ? "on" : ""}>
                Due <span className="qc">{dueCount}</span>
              </Link>
              <Link href="/action-center?bucket=verify" className="">
                Awaiting verification <span className="qc">{queueItems.length}</span>
              </Link>
            </div>
            <span className="sp" style={{ flex: 1 }} />
            <span
              className="btn sm"
              aria-disabled
              title="Owner-scoped filtering needs an owner filter in the Action Center contract — not yet available"
              style={{ opacity: 0.45, cursor: "not-allowed" }}
            >
              <Users className="ic" style={{ width: 13 }} aria-hidden="true" /> My tasks
            </span>
            <span
              className="btn sm"
              aria-disabled
              title="Advanced filter drawer is not built yet - use severity chips and top-bar park scope."
              style={{ opacity: 0.45, cursor: "not-allowed" }}
            >
              <Search className="ic" style={{ width: 13 }} aria-hidden="true" /> Filters
            </span>
          </div>

          {/* Filters — severity (server-side). Park scope is the top bar's single source of truth. */}
          <div className="chipset" style={{ marginBottom: 12 }}>
            <Link href={hrefWith({ severity: "all" })} className={`chip${severityFilter === "all" ? " on" : ""}`}>
              All severity
            </Link>
            {SEVERITY_ORDER.map((s) => (
              <Link key={s} href={hrefWith({ severity: s })} className={`chip${severityFilter === s ? " on" : ""}`}>
                {SEVERITY_META[s].label}
              </Link>
            ))}
          </div>

          {/* Explanatory band (mock). */}
          <div className="note" style={{ marginBottom: 12 }}>
            Every vaccination obligation, grouped by <b>computed work state</b> (expected vs actual — you don&apos;t set it
            by dragging). Act on a card: <b>execute · submit SOP · verify · reject · request rework · escalate</b>. Each
            links to its workflow, owner chain, and next action; every action writes the audit trail and ripples into{" "}
            <Link href="/protocol-adherence" className="lk">
              Protocol Adherence
            </Link>{" "}
            and the{" "}
            <Link href="/" className="lk">
              Control Tower
            </Link>
            .
          </div>

          {totalCount === 0 ? (
            <div className="note" style={{ marginBottom: 12, display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
              <Info className="ic" aria-hidden="true" style={{ color: "var(--brand)", flexShrink: 0 }} />
              <span>
                {actionCenter.ok
                  ? "No vaccination obligations yet — the board fills once a source-backed protocol is published and a vaccination SOP exists. Columns below show the work-state shell."
                  : "Action Center is unavailable — resolve the error above, then reload."}
              </span>
              {actionCenter.ok ? (
                <>
                  <Link href="/config?category=vaccination" className="btn sm">
                    Config
                  </Link>
                  <Link href="/sops" className="btn sm">
                    SOP Library
                  </Link>
                </>
              ) : null}
            </div>
          ) : nextCursor ? (
            <div className="muted small" style={{ marginBottom: 12 }}>
              Showing the first 200 rows for these filters; chip counts are server-authoritative totals. Narrow by park,
              severity, or work state to see the rest.
            </div>
          ) : null}

          {/* Board shell — always the full work-state column set (mock taskboard), "—" where empty. */}
          <WorkBoard rows={items} showAllColumns />
        </>
      )}
    </div>
  );
}
