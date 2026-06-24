import Link from "next/link";
import { redirect } from "next/navigation";
import {
  ArrowLeft,
  ClipboardCheck,
  Flag,
  HeartPulse,
  PackageCheck,
  Truck,
  Warehouse,
} from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getProcurementLoad } from "@/lib/api/procurement-server";
import type {
  ProcurementArrivalReview,
  ProcurementDecision,
  ProcurementHoldingStay,
  ProcurementLoadDetail,
  ProcurementLoadGoat,
  ProcurementPHCHandoff,
  ProcurementSourceHealthCheck,
  ProcurementTimelineEvent,
  ProcurementTransitHandoff,
} from "@/lib/api/procurement";
import { fmtDate, fmtDateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { Tag } from "@/components/ui-primitives";
import type { Tone } from "@/components/ui-primitives";
import { LoadWriteActions } from "./load-forms";
import {
  PROC_ARRIVAL_META,
  PROC_GOAT_STATE_META,
  PROC_HEALTH_META,
  PROC_IDENTITY_META,
  PROC_LOAD_STATUS_META,
  PROC_OWNERSHIP_META,
  PROC_SELECTION_META,
  TONE_SWATCH,
  isAcceptedIntake,
  isProcurementHistoryOnly,
  warmupMeta,
  type IdentityReviewState,
} from "./work-state";

const DECISION_TONE: Record<string, Tone> = { accepted: "ok", rejected: "dng", deferred: "mut", blocked: "dng" };
const TRANSIT_STATUS_TONE: Record<string, Tone> = { planned: "info", in_transit: "info", arrived: "ok", canceled: "mut" };
const DISCREPANCY_TONE: Record<string, Tone> = {
  none: "ok",
  partial_load: "warn",
  accepted_not_loaded: "warn",
  missing: "dng",
  extra: "dng",
  mismatch: "dng",
  blocked: "dng",
};
const WARMUP_STATE_TONE: Record<string, Tone> = {
  not_started: "mut",
  in_progress: "info",
  completed: "ok",
  outside_normal_window: "warn",
};
const HANDOFF_TONE: Record<string, Tone> = { pending: "warn", emitted: "ok", canceled: "mut" };
const ARRIVAL_STATUS_TONE: Record<string, Tone> = {
  pending: "warn",
  mismatch: "dng",
  accepted: "ok",
  rejected: "dng",
  deferred: "mut",
  blocked: "dng",
};

function goatLabel(goat: ProcurementLoadGoat): string {
  return goat.source_tag || goat.source_rfid || goat.temporary_id || shortId(goat.goat_id);
}

// ---- Journey timeline ----
function TimelineCard({ events }: { events: ProcurementTimelineEvent[] }) {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <ClipboardCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>Journey timeline</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">purchase → holding → source SOP → pre-dispatch → transit → arrival → intake</span>
      </div>
      <div className="bd">
        {events.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            No journey events recorded for this load yet.
          </p>
        ) : (
          events.map((event, idx) => (
            <div key={`${event.ref_id ?? event.event_type}-${idx}`} style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
              <div style={{ display: "flex", flexDirection: "column", alignItems: "center", flexShrink: 0 }}>
                <span className="sw" style={{ width: 11, height: 11, borderRadius: 999, background: "var(--info)" }} />
                {idx < events.length - 1 ? <span style={{ width: 2, flex: 1, minHeight: 24, background: "var(--line)", marginTop: 2 }} /> : null}
              </div>
              <div style={{ paddingBottom: 14, minWidth: 0, flex: 1 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                  <b style={{ fontSize: 13 }}>{event.summary || event.event_type || "event"}</b>
                  {event.state ? <Tag tone="info">{event.state}</Tag> : null}
                  {event.occurred_at ? <span className="muted small">{fmtDateTime(event.occurred_at)}</span> : null}
                </div>
                {event.goat_id ? <div className="muted small" style={{ marginTop: 3 }}>goat {shortId(event.goat_id)}</div> : null}
              </div>
            </div>
          ))
        )}
      </div>
    </section>
  );
}

// ---- Per-goat rows ----
const GOAT_COLS = ["Goat / source tag", "Selection", "Current stage", "Identity", "Ownership", "Health", "Warmup", "Downstream"];

function GoatRows({ goats }: { goats: ProcurementLoadGoat[] }) {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Warehouse className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Goats in load</h3>
        <Tag tone={goats.length ? "info" : "mut"}>{goats.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">per-goat journey state — not load totals only</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Goats in load">
        <table>
          <thead>
            <tr>
              {GOAT_COLS.map((c) => (
                <th key={c}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {goats.length === 0 ? (
              <tr>
                <td colSpan={GOAT_COLS.length}>
                  <div className="muted small" style={{ padding: "16px 4px", textAlign: "center" }}>
                    No goats added to this load yet.
                  </div>
                </td>
              </tr>
            ) : (
              goats.map((goat) => {
                const warm = warmupMeta(goat.warmup_days);
                const accepted = isAcceptedIntake(goat.current_state);
                const historyOnly = isProcurementHistoryOnly(goat.current_state);
                return (
                  <tr key={goat.load_goat_id}>
                    <td>
                      <span className="gid">{goatLabel(goat)}</span>
                    </td>
                    <td>
                      <Tag tone={PROC_SELECTION_META[goat.selection_state].tone}>{PROC_SELECTION_META[goat.selection_state].label}</Tag>
                    </td>
                    <td>
                      <Tag tone={PROC_GOAT_STATE_META[goat.current_state].tone}>{PROC_GOAT_STATE_META[goat.current_state].label}</Tag>
                    </td>
                    <td>
                      <Tag tone={PROC_IDENTITY_META[goat.identity_review_state as IdentityReviewState].tone}>
                        {PROC_IDENTITY_META[goat.identity_review_state as IdentityReviewState].label}
                      </Tag>
                    </td>
                    <td>
                      <Tag tone={PROC_OWNERSHIP_META[goat.ownership_state].tone}>{PROC_OWNERSHIP_META[goat.ownership_state].label}</Tag>
                    </td>
                    <td>
                      <Tag tone={PROC_HEALTH_META[goat.health_state].tone}>{PROC_HEALTH_META[goat.health_state].label}</Tag>
                    </td>
                    <td title={warm.note}>
                      <Tag tone={warm.tone}>{warm.label}</Tag>
                    </td>
                    <td>
                      {accepted && goat.goat_id ? (
                        <Link href={`/goats/${encodeURIComponent(goat.goat_id)}`} className="lk small">
                          PHC passport →
                        </Link>
                      ) : historyOnly ? (
                        <span className="muted small">procurement history — no PHC work</span>
                      ) : (
                        <span className="muted small">in source entry</span>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
      <div className="bd">
        <p className="muted small" style={{ margin: 0, lineHeight: 1.6 }}>
          Only <b>accepted intake</b> goats link out to PHC. Rejected-before-truck, arrival-rejected, dead/sold/lost, and
          unresolved goats stay procurement history and are never shown as PHC vaccination work.
        </p>
      </div>
    </section>
  );
}

// ---- Pre-dispatch decisions ----
function DecisionCard({ decisions }: { decisions: ProcurementDecision[] }) {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Flag className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />
        <h3>Pre-dispatch decisions</h3>
        <Tag tone={decisions.length ? "warn" : "mut"}>{decisions.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">accept · reject before truck · defer · block — audited</span>
      </div>
      {decisions.length === 0 ? (
        <div className="bd">
          <p className="muted small" style={{ margin: 0 }}>
            No pre-dispatch decisions recorded yet. A decision (accept for truck, reject before truck, defer, or block)
            is recorded through the procurement backend with reason, proof, and audited actor/time.
          </p>
        </div>
      ) : (
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Pre-dispatch decisions">
          <table>
            <thead>
              <tr>
                <th>Goat</th>
                <th>Stage</th>
                <th>Decision</th>
                <th>Reason</th>
                <th>Decided</th>
              </tr>
            </thead>
            <tbody>
              {decisions.map((d, idx) => (
                <tr key={d.decision_id ?? idx}>
                  <td>
                    <span className="gid">{shortId(d.goat_id)}</span>
                  </td>
                  <td className="muted">{d.decision_stage ?? "pre_dispatch"}</td>
                  <td>{d.decision_type ? <Tag tone={DECISION_TONE[d.decision_type] ?? "mut"}>{d.decision_type}</Tag> : <span className="muted">—</span>}</td>
                  <td className="muted">{d.reason ?? "—"}</td>
                  <td className="muted">{fmtDateTime(d.decided_at) || "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

// ---- Arrival gate (distinct checkpoint) ----
function ArrivalGateCard({ reviews }: { reviews: ProcurementArrivalReview[] }) {
  return (
    <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--purple) 28%,var(--line))" }}>
      <div className="hd">
        <Flag className="ic" style={{ color: "var(--purple)" }} aria-hidden="true" />
        <h3>Arrival gate</h3>
        <Tag tone={reviews.length ? "pur" : "mut"}>{reviews.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">distinct checkpoint — reconciled before accepted herd intake</span>
      </div>
      <div className="bd">
        {reviews.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            No arrival review yet. The arrival gate reconciles expected vs arrived goats (matched / missing / extra,
            health and weight flags) at the park before any goat becomes clean herd truth.
          </p>
        ) : (
          reviews.map((review, idx) => {
            const goats = review.goats ?? [];
            const matched = goats.filter((g) => g.arrival_state === "matched" || g.arrival_state === "accepted").length;
            const missing = goats.filter((g) => g.arrival_state === "missing").length;
            const extra = goats.filter((g) => g.arrival_state === "extra_unresolved").length;
            return (
              <div key={review.review_id ?? idx} style={{ marginBottom: idx < reviews.length - 1 ? 14 : 0 }}>
                <div className="fchipsbar" style={{ marginBottom: 8, flexWrap: "wrap" }}>
                  {review.status ? <Tag tone={ARRIVAL_STATUS_TONE[review.status] ?? "mut"}>arrival: {review.status}</Tag> : null}
                  <span className="muted small">park {review.park_location_id ? shortId(review.park_location_id) : "—"}</span>
                  <div className="sp" style={{ flex: 1 }} />
                  <span className="muted small">
                    {matched} matched · {missing} missing · {extra} extra/unknown · {goats.length} reviewed
                  </span>
                </div>
                {goats.length > 0 ? (
                  <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Arrival goats">
                    <table>
                      <thead>
                        <tr>
                          <th>Goat</th>
                          <th>Arrival state</th>
                        </tr>
                      </thead>
                      <tbody>
                        {goats.map((g, gi) => (
                          <tr key={g.review_goat_id ?? gi}>
                            <td>
                              <span className="gid">{shortId(g.goat_id)}</span>
                            </td>
                            <td>{g.arrival_state ? <Tag tone={PROC_ARRIVAL_META[g.arrival_state].tone}>{PROC_ARRIVAL_META[g.arrival_state].label}</Tag> : <span className="muted">—</span>}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : null}
              </div>
            );
          })
        )}
      </div>
    </section>
  );
}

// ---- Supporting records (transit, holding, source health, PHC handoff) ----
function TransitCard({ handoffs }: { handoffs: ProcurementTransitHandoff[] }) {
  if (handoffs.length === 0) return null;
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Transit handoffs</h3>
        <Tag tone="info">{handoffs.length}</Tag>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Transit handoffs">
        <table>
          <thead>
            <tr>
              <th>Loaded</th>
              <th>Dispatched</th>
              <th>Status</th>
              <th>Discrepancy</th>
            </tr>
          </thead>
          <tbody>
            {handoffs.map((h, idx) => (
              <tr key={h.handoff_id ?? idx}>
                <td>{h.loaded_count ?? "—"}</td>
                <td className="muted">{fmtDateTime(h.dispatched_at) || "—"}</td>
                <td>{h.status ? <Tag tone={TRANSIT_STATUS_TONE[h.status] ?? "mut"}>{h.status}</Tag> : <span className="muted">—</span>}</td>
                <td>{h.discrepancy_state ? <Tag tone={DISCREPANCY_TONE[h.discrepancy_state] ?? "mut"}>{h.discrepancy_state}</Tag> : <span className="muted">—</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function HoldingCard({ stays }: { stays: ProcurementHoldingStay[] }) {
  if (stays.length === 0) return null;
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Warehouse className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Holding stays</h3>
        <Tag tone="info">{stays.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">source warmup 45–70 days is normal</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Holding stays">
        <table>
          <thead>
            <tr>
              <th>Goat</th>
              <th>Holding</th>
              <th>Started</th>
              <th>Ended</th>
              <th>Warmup</th>
              <th>State</th>
            </tr>
          </thead>
          <tbody>
            {stays.map((s, idx) => {
              const warm = warmupMeta(s.warmup_days);
              return (
                <tr key={s.stay_id ?? idx}>
                  <td>
                    <span className="gid">{shortId(s.goat_id)}</span>
                  </td>
                  <td className="muted">{s.holding_location_id ? shortId(s.holding_location_id) : "—"}</td>
                  <td className="muted">{fmtDate(s.started_at) || "—"}</td>
                  <td className="muted">{s.ended_at ? fmtDate(s.ended_at) : "ongoing"}</td>
                  <td title={warm.note}>
                    <Tag tone={warm.tone}>{warm.label}</Tag>
                  </td>
                  <td>{s.warmup_state ? <Tag tone={WARMUP_STATE_TONE[s.warmup_state] ?? "mut"}>{s.warmup_state.replace(/_/g, " ")}</Tag> : <span className="muted">—</span>}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function HealthCard({ checks }: { checks: ProcurementSourceHealthCheck[] }) {
  if (checks.length === 0) return null;
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <HeartPulse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>Source health checks</h3>
        <Tag tone="info">{checks.length}</Tag>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Source health checks">
        <table>
          <thead>
            <tr>
              <th>Goat</th>
              <th>Health</th>
              <th>Checked</th>
            </tr>
          </thead>
          <tbody>
            {checks.map((c, idx) => (
              <tr key={c.health_check_id ?? idx}>
                <td>
                  <span className="gid">{shortId(c.goat_id)}</span>
                </td>
                <td>{c.health_state ? <Tag tone={PROC_HEALTH_META[c.health_state].tone}>{PROC_HEALTH_META[c.health_state].label}</Tag> : <span className="muted">—</span>}</td>
                <td className="muted">{fmtDateTime(c.checked_at) || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function HandoffCard({ handoffs }: { handoffs: ProcurementPHCHandoff[] }) {
  if (handoffs.length === 0) return null;
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <PackageCheck className="ic" style={{ color: "var(--brand-d)" }} aria-hidden="true" />
        <h3>Accepted intake → PHC handoffs</h3>
        <Tag tone="ok">{handoffs.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">only accepted intake makes a goat eligible for post-arrival PHC</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="PHC handoffs">
        <table>
          <thead>
            <tr>
              <th>Goat</th>
              <th>Park</th>
              <th>Shed</th>
              <th>Entry date</th>
              <th>Accepted</th>
              <th>Event</th>
            </tr>
          </thead>
          <tbody>
            {handoffs.map((h, idx) => (
              <tr key={h.handoff_id ?? idx}>
                <td>
                  {h.goat_id ? (
                    <Link href={`/goats/${encodeURIComponent(h.goat_id)}`} className="gid">
                      {shortId(h.goat_id)}
                    </Link>
                  ) : (
                    <span className="gid">—</span>
                  )}
                </td>
                <td className="muted">{h.park_location_id ? shortId(h.park_location_id) : "—"}</td>
                <td className="muted">{h.shed_location_id ? shortId(h.shed_location_id) : "—"}</td>
                <td className="muted">{fmtDate(h.entry_date) || "—"}</td>
                <td className="muted">{fmtDateTime(h.accepted_at) || "—"}</td>
                <td>{h.event_status ? <Tag tone={HANDOFF_TONE[h.event_status] ?? "mut"}>{h.event_status}</Tag> : <span className="muted">—</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export async function ProcurementLoadDetailPage({ loadId, searchParams }: { loadId: string; searchParams?: RouteSearchParams }) {
  const result = await getProcurementLoad(loadId);
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const sp = searchParams ?? {};
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");
  const returnTo = `/procurement/source-entry/loads/${loadId}`;

  if (!result.ok) {
    return (
      <div className="screen on">
        <div className="phead">
          <div>
            <div className="crumb">
              <b>Procurement</b> · Source entry · Load
            </div>
            <h1>Load detail</h1>
          </div>
        </div>
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
        <Link href="/procurement/source-entry" className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> Back to Source Entry Board
        </Link>
      </div>
    );
  }

  const detail: ProcurementLoadDetail = result.data.detail;
  const { load } = detail;

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>Procurement</b> · Source entry · Load
          </div>
          <h1>Load {shortId(load.load_id)}</h1>
          <div className="sub">
            Source party {shortId(load.source_party_id)}
            {load.source_location_id ? ` · holding ${shortId(load.source_location_id)}` : ""} — full journey from purchase to
            accepted intake.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/procurement/source-entry" className="btn">
          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> Source Entry Board
        </Link>
      </div>

      <div className="fchipsbar" style={{ marginBottom: 16, flexWrap: "wrap" }}>
        <Tag tone={PROC_LOAD_STATUS_META[load.status].tone}>{PROC_LOAD_STATUS_META[load.status].label}</Tag>
        <span className="muted small">expected {load.expected_count}</span>
        <span className="muted small">purchase {fmtDate(load.purchase_date ?? undefined)}</span>
        <span className="muted small">planned dispatch {fmtDate(load.planned_dispatch_at ?? undefined)}</span>
        <div className="sp" style={{ flex: 1 }} />
        <span className="sw" style={{ background: TONE_SWATCH[PROC_LOAD_STATUS_META[load.status].tone], width: 10, height: 10, borderRadius: 999 }} />
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

      <GoatRows goats={detail.goats} />

      {/* Operator write surface — every control submits a real server action (idempotency-keyed). */}
      <section className="card" style={{ marginBottom: 16, background: "transparent", border: "none", padding: 0 }}>
        <div className="hd" style={{ paddingLeft: 0 }}>
          <h3>Source-entry actions</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">add goat · source health · pre-dispatch · dispatch · arrival · accept intake</span>
        </div>
        <LoadWriteActions loadId={load.load_id} goats={detail.goats} returnTo={returnTo} />
      </section>

      <DecisionCard decisions={detail.decisions} />
      <ArrivalGateCard reviews={detail.arrival_reviews} />
      <TransitCard handoffs={detail.transit_handoffs} />
      <HoldingCard stays={detail.holding_stays} />
      <HealthCard checks={detail.source_health_checks} />
      <HandoffCard handoffs={detail.phc_handoffs} />
      <TimelineCard events={detail.timeline} />
    </div>
  );
}
