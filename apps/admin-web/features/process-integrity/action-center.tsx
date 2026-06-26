import Link from "next/link";
import { Ban, GitBranch, Info, ShieldCheck, Syringe, Video, X } from "lucide-react";
import { getVaccinationActionCenter, getVaccinationVerificationQueue } from "@/lib/api/server";
import type {
  ActionCenterObligation,
  ProcessIntegritySeverity,
  VaccinationQueueItem,
  WorkState,
} from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import {
  PROOF_META,
  SEVERITY_META,
  SEVERITY_ORDER,
  SOP_META,
  VERIFICATION_META,
  WORK_STATE_META,
  WORK_STATE_ORDER,
} from "./process-integrity";
import { SopChecklist } from "./sop-checklist";
import { VACCINATION_DRIVE_SOP_STEPS } from "@/features/phc-vaccination/vaccination-sop-steps";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { ActionCenterFiltersButton } from "./action-center-filters";
import { WorkBoard, actionWorkTitle } from "./work-board";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

// Backend has no manual priority field — it is derived from computed severity. The drawer shows the
// mock's High/Med/Low chips read-only (disabled-with-reason), reflecting severity, never editable.
const PRIORITY_BY_SEVERITY: Record<ProcessIntegritySeverity, "High" | "Med" | "Low"> = {
  broken: "High",
  at_risk: "Med",
  watch: "Low",
  ok: "Low",
};

// How far the vaccination drive SOP has progressed for this obligation, derived (read-only) from the
// computed lifecycle states — steps below the index are done, the step at it is current. Honest mapping:
// the obligation existing = "Drive scheduled" done; SOP/proof activity advances administration; an
// accepted+verified proof advances the ledger consume; a completed obligation finishes the flow.
function sopDoneThrough(row: ActionCenterObligation): number {
  let n = 1; // obligation generated → "Drive scheduled" done
  if (
    row.sop_task_state === "submitted" ||
    row.sop_task_state === "accepted" ||
    row.proof_state === "uploaded" ||
    row.proof_state === "accepted"
  ) {
    n = 2;
  }
  if (row.proof_state === "accepted" && row.verification_state === "accepted") n = 3;
  if (row.completion_state === "completed") n = 4;
  return n;
}

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
  const selectedActionRowId = one(sp, "ac_row");
  const selectedActionRow = selectedActionRowId ? items.find((row) => row.row_id === selectedActionRowId) : undefined;

  // Server-authoritative counts per work state (not derived from the capped page).
  const stateCounts = new Map<WorkState, number>();
  if (actionCenter.ok) for (const c of actionCenter.data.counts_by_work_state) stateCounts.set(c.work_state, c.count);
  const totalCount = Array.from(stateCounts.values()).reduce((a, b) => a + b, 0);
  const overdueCount = stateCounts.get("overdue") ?? 0;
  const dueCount = stateCounts.get("due") ?? 0;
  const severityCounts = new Map<ProcessIntegritySeverity, number>();
  for (const item of items) severityCounts.set(item.severity, (severityCounts.get(item.severity) ?? 0) + 1);

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
  const stateFilterLinks = [
    { label: "All", href: hrefWith({ state: "all" }), active: stateFilter === "all", count: totalCount },
    ...WORK_STATE_ORDER.map((state) => ({
      label: WORK_STATE_META[state].label,
      href: hrefWith({ state }),
      active: stateFilter === state,
      count: stateCounts.get(state) ?? 0,
    })),
  ];
  const severityFilterLinks = [
    { label: "All severity", href: hrefWith({ severity: "all" }), active: severityFilter === "all", count: items.length },
    ...SEVERITY_ORDER.map((severity) => ({
      label: SEVERITY_META[severity].label,
      href: hrefWith({ severity }),
      active: severityFilter === severity,
      count: severityCounts.get(severity) ?? 0,
    })),
  ];
  const clearFiltersHref = hrefWith({ severity: "all", state: "all" });

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
        <Link href={hrefWith({ bucket: undefined })} replace scroll={false} className={view === "board" ? "on" : ""}>
          Status board
        </Link>
        <Link href={hrefWith({ bucket: "verify" })} replace scroll={false} className={view === "verify" ? "on" : ""}>
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
            <Link href={hrefWith({ severity: "all", state: "all" })} replace scroll={false} className="on">
              Vaccination <span className="cbq">{totalCount}</span>
            </Link>
          </div>

          {/* Quick tabs (mock #acQuickTabs) + My tasks + Filters. Counts are server-authoritative. */}
          <div id="acToggles" style={{ display: "flex", gap: 8, marginBottom: 14, flexWrap: "wrap", alignItems: "center" }}>
            <div className="subtabs" id="acQuickTabs" style={{ margin: 0 }}>
              <Link href={hrefWith({ state: "all" })} replace scroll={false} className={stateFilter === "all" ? "on" : ""}>
                All <span className="qc">{totalCount}</span>
              </Link>
              <Link href={hrefWith({ state: "overdue" })} replace scroll={false} className={stateFilter === "overdue" ? "on" : ""}>
                Overdue <span className="qc">{overdueCount}</span>
              </Link>
              <Link href={hrefWith({ state: "due" })} replace scroll={false} className={stateFilter === "due" ? "on" : ""}>
                Due <span className="qc">{dueCount}</span>
              </Link>
              <Link href={hrefWith({ bucket: "verify" })} replace scroll={false} className="">
                Awaiting verification <span className="qc">{queueItems.length}</span>
              </Link>
            </div>
            <span className="sp" style={{ flex: 1 }} />
            <ActionCenterFiltersButton
              label="My tasks"
              mode="my"
              rowsLabel={`${items.length} rows shown · local owner filter applies to visible cards`}
              clearHref={clearFiltersHref}
              stateLinks={stateFilterLinks}
              severityLinks={severityFilterLinks}
            />
            <ActionCenterFiltersButton
              rowsLabel={`${items.length} rows shown · ${totalCount} total in current work-state counts`}
              clearHref={clearFiltersHref}
              stateLinks={stateFilterLinks}
              severityLinks={severityFilterLinks}
            />
          </div>

          {/* Filters — severity (server-side). Park scope is the top bar's single source of truth. */}
          <div className="chipset" style={{ marginBottom: 12 }}>
            <Link href={hrefWith({ severity: "all" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
              All severity
            </Link>
            {SEVERITY_ORDER.map((s) => (
              <Link key={s} href={hrefWith({ severity: s })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
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
          <WorkBoard rows={items} showAllColumns drawerHrefForRow={(row) => hrefWith({ ac_row: row.row_id })} />
          {selectedActionRow ? (
            <ActionCenterRowDrawer
              row={selectedActionRow}
              closeHref={hrefWith({ ac_row: undefined })}
              returnTo={hrefWith({ ac_row: selectedActionRow.row_id })}
              workflowHref={scopeHref(`/workflows/${encodeURIComponent(selectedActionRow.row_id)}`, scope, {}, { from: "action-center" })}
              passportHref={selectedActionRow.goat_id ? `/goats/${encodeURIComponent(selectedActionRow.goat_id)}` : undefined}
            />
          ) : null}
        </>
      )}
    </div>
  );
}

// Action runs in the field app / workflow record, not as a direct admin mutation here. Such buttons keep
// the mock's look but are disabled-with-reason (never dropped) per the mock-fidelity rule (disable ≠ simplify).
const FIELD_ACTION_NOTE =
  "Recorded against the obligation from the workflow record / field app — not a direct admin action here.";

function ActionCenterRowDrawer({
  row,
  closeHref,
  returnTo,
  workflowHref,
  passportHref,
}: {
  row: ActionCenterObligation;
  closeHref: string;
  returnTo: string;
  workflowHref: string;
  passportHref?: string;
}) {
  const work = WORK_STATE_META[row.work_state];
  const blocker = row.blocker_reason;
  const ownerMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const ownerName = row.owner?.operator_name ?? row.owner?.park_head_name ?? "Unassigned";
  const title = actionWorkTitle(row);
  const priority = PRIORITY_BY_SEVERITY[row.severity];
  const sopProgress = sopDoneThrough(row);
  const hasCompletion = !!row.completion_id;
  const completionId = row.completion_id ?? "";
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close Action Center drawer" scroll={false} />
      <aside className="drawer on" aria-label="Action Center work item">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">ACTION</div>
            <h2>{title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close Action Center drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          {/* Computed adherence status — never a manual label (mock). */}
          <div className="fld">
            <label>Adherence status (computed)</label>
            <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <Tag tone={work.tone}>{work.label}</Tag>
              <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
              <span className="muted small">
                computed from obligation + SOP submission + proof + timing — not manually editable
              </span>
            </div>
          </div>

          {/* Owner chain + Due. Owner is assigned via the workflow record; due comes from the protocol schedule. */}
          <div className="fld" style={{ display: "flex", gap: 10 }}>
            <div style={{ flex: 1 }}>
              <label>Owner chain</label>
              {ownerMissing ? (
                <div style={{ paddingTop: 4 }}>
                  <Tag tone="dng">owner chain missing</Tag>
                </div>
              ) : (
                <select disabled title="Owner chain is assigned from the workflow record, not edited here" defaultValue={ownerName}>
                  <option>{ownerName}</option>
                </select>
              )}
            </div>
            <div style={{ flex: 1 }}>
              <label>Due</label>
              <input disabled title="Due date is set by the published protocol schedule" defaultValue={fmtDate(row.due_at)} />
            </div>
          </div>

          {/* Priority — derived from computed severity, shown read-only (disabled-with-reason). */}
          <div className="fld">
            <label>Priority</label>
            <div className="chipset" title="Priority is derived from computed severity — not manually set">
              {(["High", "Med", "Low"] as const).map((p) => (
                <span key={p} className={`chip${p === priority ? " on" : ""}`} aria-disabled="true">
                  {p}
                </span>
              ))}
            </div>
          </div>

          {/* Obligation facts — compact metagrid (mock density), not a flat full-record dump. */}
          <div className="metagrid">
            <div>
              <div className="k">Protocol</div>
              <div className="v">{row.protocol_name}</div>
            </div>
            <div>
              <div className="k">Dose</div>
              <div className="v">{row.dose_code}</div>
            </div>
            <div>
              <div className="k">Park · Shed</div>
              <div className="v">
                {row.park_name} · {row.shed_name}
              </div>
            </div>
            <div>
              <div className="k">Cohort · Progress</div>
              <div className="v">
                {row.animal_stage} · {row.completed_count}/{row.expected_count} done
              </div>
            </div>
          </div>

          {blocker ? (
            <div className="alert" style={{ marginTop: 14, marginBottom: 0 }}>
              <Ban className="ic" aria-hidden="true" />
              <span>{blocker}</span>
            </div>
          ) : null}

          {/* SOP checklist — the vaccination drive SOP's procedure STEPS with per-step video-proof pills
              (mock #taskDrawer checklist anatomy), NOT the obligation lifecycle chain (that is the Workflow
              record). Step done/current is derived read-only from the computed obligation states. */}
          <div style={{ marginTop: 16 }}>
            <div className="b700" style={{ margin: "4px 0 10px" }}>
              Checklist · SOP — Vaccination Drive SOP
            </div>
            <SopChecklist steps={VACCINATION_DRIVE_SOP_STEPS} doneThrough={sopProgress} />
          </div>

          {/* Computed SOP / proof / verification state chips. */}
          <div className="chipset" style={{ marginTop: 14 }}>
            <Tag tone={SOP_META[row.sop_task_state].tone}>{SOP_META[row.sop_task_state].label}</Tag>
            <Tag tone={PROOF_META[row.proof_state].tone}>{PROOF_META[row.proof_state].label}</Tag>
            <Tag tone={VERIFICATION_META[row.verification_state].tone}>{VERIFICATION_META[row.verification_state].label}</Tag>
          </div>

          {/* Linked surfaces (mock "Linked"). lucide icons — never emoji. */}
          <div style={{ marginTop: 16 }}>
            <div className="b700" style={{ margin: "2px 0 8px" }}>
              Linked
            </div>
            <div style={{ display: "flex", gap: 7, flexWrap: "wrap" }}>
              <Link href={workflowHref} className="tag t-info" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <GitBranch className="ic" style={{ width: 12 }} aria-hidden="true" />
                workflow record
              </Link>
              <Link href="/protocol-adherence" className="tag t-warn" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <ShieldCheck className="ic" style={{ width: 12 }} aria-hidden="true" />
                adherence
              </Link>
              <Link href="/vaccination" className="tag t-teal" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
                <Syringe className="ic" style={{ width: 12 }} aria-hidden="true" />
                vaccination
              </Link>
            </div>
          </div>
        </div>

        {/* Action row (mock #tdFoot). Verify / Request rework are wired to the real completion when one
            exists; un-backed steps keep the mock look but are disabled-with-reason. */}
        <div className="df">
          <Link href={workflowHref} className="btn p">
            {row.next_action}
          </Link>
          <button type="button" className="btn" disabled aria-disabled="true" title={FIELD_ACTION_NOTE}>
            Start SOP
          </button>
          <button type="button" className="btn" disabled aria-disabled="true" title={FIELD_ACTION_NOTE}>
            Submit / proof
          </button>
          {hasCompletion ? (
            <ActionForm action={verifyCompletionAction} completionId={completionId} returnTo={returnTo}>
              Verify
            </ActionForm>
          ) : (
            <button type="button" className="btn" disabled aria-disabled="true" title="No recorded dose to verify yet.">
              Verify
            </button>
          )}
          {hasCompletion ? (
            <ActionForm action={rejectCompletionAction} completionId={completionId} returnTo={returnTo} reason="rework_requested">
              Request rework
            </ActionForm>
          ) : (
            <button type="button" className="btn" disabled aria-disabled="true" title="No recorded dose to rework yet.">
              Request rework
            </button>
          )}
          <button type="button" className="btn" disabled aria-disabled="true" title={FIELD_ACTION_NOTE}>
            Escalate
          </button>
          <Link href={workflowHref} className="btn">
            Workflow record
          </Link>
          {passportHref ? (
            <Link href={passportHref} className="btn">
              Goat Passport
            </Link>
          ) : null}
          <Link href={closeHref} replace className="btn" scroll={false}>
            Close
          </Link>
        </div>
      </aside>
    </>
  );
}
