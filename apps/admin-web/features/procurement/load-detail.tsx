import Link from "@/components/no-prefetch-link";
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
import { firstAuthRequiredError, listLocations, type LocationSummary } from "@/lib/api/server";
import { getProcurementLoad } from "@/lib/api/procurement-server";
import type {
  ProcurementArrivalReview,
  ProcurementDecision,
  ProcurementHoldingStay,
  ProcurementLoadDetail,
  ProcurementLoadGoat,
  ProcurementPCHandoff,
  ProcurementSourceHealthCheck,
  ProcurementTimelineEvent,
  ProcurementTransitHandoff,
} from "@/lib/api/procurement";
import { fmtDate, fmtDateTime, shortId } from "@/lib/format";
import { hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { actionFeedbackCopy, copy, optionLabel, optionTitle, optionTone, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Tag } from "@/components/ui-primitives";
import type { Tone } from "@/components/ui-primitives";
import { LoadWriteActions } from "./load-forms";
import type { ProcurementLocationOption, ProcurementLocations } from "./location-selects";
import {
  TONE_SWATCH,
  isAcceptedIntake,
  isProcurementHistoryOnly,
  warmupMeta,
} from "./work-state";

function goatLabel(goat: ProcurementLoadGoat): string {
	return goat.animal_identifier_1 || goat.animal_identifier_2 || shortId(goat.goat_id);
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function ContractTag({ pageContract, groupId, value }: { pageContract: AdminUiPageContract; groupId: string; value: string }) {
  return <Tag tone={contractTone(pageContract, groupId, value)}>{optionLabel(pageContract, groupId, value)}</Tag>;
}

function toLocationOption(location: LocationSummary): ProcurementLocationOption {
  return {
    id: location.location_id,
    code: location.location_code,
    name: location.name,
    parentId: location.parent_location_id,
  };
}

function addLocationOption(options: ProcurementLocationOption[], option: ProcurementLocationOption | null): ProcurementLocationOption[] {
  if (!option || options.some((item) => item.id === option.id)) return options;
  return [option, ...options];
}

function sourceLocationOption(load: ProcurementLoadDetail["load"]): ProcurementLocationOption | null {
  if (!load.source_location_id) return null;
  const name = load.source_location_name || load.source_location_code || shortId(load.source_location_id);
  return {
    id: load.source_location_id,
    code: load.source_location_code ?? null,
    name,
    parentId: null,
  };
}

function shedUsable(location: LocationSummary): boolean {
  return location.operational.usable_for_vaccination && !location.operational.is_holding;
}

async function getProcurementLocations(): Promise<ProcurementLocations> {
  // request-plan:ignore owner=procurement-platform issue=C35-016 expires=2026-09-30 reason=fixed three-call location taxonomy request; cardinality does not depend on returned rows
  const [parksResult, farmsResult, shedsResult] = await Promise.all([
    listLocations({ type: "park", status: "active" }),
    listLocations({ type: "farm", status: "active" }),
    listLocations({ type: "shed", status: "active" }),
  ]);
  const parks = parksResult.ok ? parksResult.data.items.map(toLocationOption) : [];
  const sheds = shedsResult.ok ? shedsResult.data.items.filter(shedUsable).map(toLocationOption) : [];
  const farms = farmsResult.ok ? farmsResult.data.items.map(toLocationOption) : [];
  return {
    parks,
    origins: [...farms, ...parks],
    sheds,
    available: parksResult.ok && shedsResult.ok,
  };
}

function warmupExpectationKey(purpose: string | null | undefined): string {
  if (purpose === "fattening" || purpose === "non_breeding" || purpose === "breeding") return purpose;
  return "unspecified";
}

function WarmupTag({ days, purpose, pageContract }: { days: number | null | undefined; purpose?: string | null; pageContract: AdminUiPageContract }) {
  const warm = warmupMeta(days, purpose);
  const expectationKey = warmupExpectationKey(purpose);
  const dynamicLabel = warm.label === "—" ? copy(pageContract, "label.placeholder") : warm.label;
  return (
    <span title={optionTitle(pageContract, "warmup_expectations", expectationKey)}>
      <Tag tone={warm.tone}>
        {dynamicLabel} / {optionLabel(pageContract, "warmup_expectations", expectationKey)}
      </Tag>
    </span>
  );
}

// ---- Journey timeline ----
function TimelineCard({ events, pageContract }: { events: ProcurementTimelineEvent[]; pageContract: AdminUiPageContract }) {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <ClipboardCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.timeline.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.timeline.note")}</span>
      </div>
      <div className="bd">
        {events.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "empty.timeline")}
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
                  <b style={{ fontSize: 13 }}>{event.summary || event.event_type || copy(pageContract, "label.event_fallback")}</b>
                  {event.state ? <Tag tone="info">{event.state}</Tag> : null}
                  {event.occurred_at ? <span className="muted small">{fmtDateTime(event.occurred_at)}</span> : null}
                </div>
                {event.goat_id ? <div className="muted small" style={{ marginTop: 3 }}>{copy(pageContract, "label.goat_prefix")} {shortId(event.goat_id)}</div> : null}
              </div>
            </div>
          ))
        )}
      </div>
    </section>
  );
}

// ---- Per-goat rows ----
function GoatRows({ goats, pageContract }: { goats: ProcurementLoadGoat[]; pageContract: AdminUiPageContract }) {
  const goatCols = tableLabels(pageContract, "load-goats");
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Warehouse className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.goats.title")}</h3>
        <Tag tone={goats.length ? "info" : "mut"}>{goats.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.goats.note")}</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.goats.title")}>
        <table>
          <thead>
            <tr>
              {goatCols.map((c) => (
                <th key={c}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {goats.length === 0 ? (
              <tr>
                <td colSpan={goatCols.length}>
                  <div className="muted small" style={{ padding: "16px 4px", textAlign: "center" }}>
                    {copy(pageContract, "empty.load_goats")}
                  </div>
                </td>
              </tr>
            ) : (
              goats.map((goat) => {
                const accepted = isAcceptedIntake(goat.current_state);
                const historyOnly = isProcurementHistoryOnly(goat.current_state);
                return (
                  <tr key={goat.load_goat_id}>
                    <td>
                      <span className="gid">{goatLabel(goat)}</span>
                    </td>
                    <td>
                      <ContractTag pageContract={pageContract} groupId="proc_selection_state" value={goat.selection_state} />
                    </td>
                    <td>
                      <ContractTag pageContract={pageContract} groupId="proc_goat_state" value={goat.current_state} />
                    </td>
                    <td>
                      <ContractTag pageContract={pageContract} groupId="proc_source_entry_state" value={goat.source_entry_state} />
                    </td>
                    <td>
                      <ContractTag pageContract={pageContract} groupId="proc_ownership_state" value={goat.ownership_state} />
                    </td>
                    <td>
                      <ContractTag pageContract={pageContract} groupId="proc_health_state" value={goat.health_state} />
                    </td>
                    <td>
                      <WarmupTag days={goat.warmup_days} purpose={goat.purpose} pageContract={pageContract} />
                    </td>
                    <td>
                      {accepted && goat.goat_id ? (
                        <Link href={`/goats/${encodeURIComponent(goat.goat_id)}`} className="lk small">
                          {copy(pageContract, "action.pc_passport")} →
                        </Link>
                      ) : historyOnly ? (
                        <span className="muted small">{copy(pageContract, "label.procurement_history_no_pc")}</span>
                      ) : (
                        <span className="muted small">{copy(pageContract, "label.in_source_entry")}</span>
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
          {copy(pageContract, "section.goats.boundary_note")}
        </p>
      </div>
    </section>
  );
}

// ---- Pre-dispatch decisions ----
function DecisionCard({ decisions, pageContract }: { decisions: ProcurementDecision[]; pageContract: AdminUiPageContract }) {
  const labels = tableLabels(pageContract, "pre-dispatch-decisions");
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Flag className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.pre_dispatch.title")}</h3>
        <Tag tone={decisions.length ? "warn" : "mut"}>{decisions.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.pre_dispatch.note")}</span>
      </div>
      {decisions.length === 0 ? (
        <div className="bd">
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "section.pre_dispatch.empty")}
          </p>
        </div>
      ) : (
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.pre_dispatch.title")}>
          <table>
            <thead>
              <tr>
                {labels.map((label) => (
                  <th key={label}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {decisions.map((d, idx) => (
                <tr key={d.decision_id ?? idx}>
                  <td>
                    <span className="gid">{shortId(d.goat_id)}</span>
                  </td>
                  <td className="muted">{d.decision_stage ?? "pre_dispatch"}</td>
                  <td>{d.decision_type ? <ContractTag pageContract={pageContract} groupId="proc_decision_type" value={d.decision_type} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
                  <td className="muted">{d.reason ?? copy(pageContract, "label.placeholder")}</td>
                  <td className="muted">{fmtDateTime(d.decided_at) || copy(pageContract, "label.placeholder")}</td>
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
function ArrivalGateCard({ reviews, pageContract }: { reviews: ProcurementArrivalReview[]; pageContract: AdminUiPageContract }) {
  const labels = tableLabels(pageContract, "arrival-goats");
  return (
    <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--purple) 28%,var(--line))" }}>
      <div className="hd">
        <Flag className="ic" style={{ color: "var(--purple)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.arrival_gate.title")}</h3>
        <Tag tone={reviews.length ? "pur" : "mut"}>{reviews.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.arrival_gate.note")}</span>
      </div>
      <div className="bd">
        {reviews.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "section.arrival_gate.empty")}
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
                  {review.status ? <Tag tone={contractTone(pageContract, "proc_arrival_status", review.status)}>{copy(pageContract, "label.arrival_prefix")}: {optionLabel(pageContract, "proc_arrival_status", review.status)}</Tag> : null}
                  <span className="muted small">{copy(pageContract, "label.park_prefix")} {review.park_location_label || (review.park_location_id ? shortId(review.park_location_id) : copy(pageContract, "label.placeholder"))}</span>
                  <div className="sp" style={{ flex: 1 }} />
                  <span className="muted small">
                    {matched} {copy(pageContract, "label.matched")} · {missing} {copy(pageContract, "label.missing")} · {extra} {copy(pageContract, "label.extra_unknown")} · {goats.length} {copy(pageContract, "label.reviewed")}
                  </span>
                </div>
                {goats.length > 0 ? (
                  <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={table(pageContract, "arrival-goats").title}>
                    <table>
                      <thead>
                        <tr>
                          {labels.map((label) => (
                            <th key={label}>{label}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {goats.map((g, gi) => (
                          <tr key={g.review_goat_id ?? gi}>
                            <td>
                              <span className="gid">{shortId(g.goat_id)}</span>
                            </td>
                            <td>{g.arrival_state ? <ContractTag pageContract={pageContract} groupId="proc_arrival_state" value={g.arrival_state} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
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

// ---- Supporting records (transit, holding, source health, Preventive Care (PC) handoff) ----
function TransitCard({ handoffs, pageContract }: { handoffs: ProcurementTransitHandoff[]; pageContract: AdminUiPageContract }) {
  if (handoffs.length === 0) return null;
  const labels = tableLabels(pageContract, "transit-handoffs");
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.transit.title")}</h3>
        <Tag tone="info">{handoffs.length}</Tag>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.transit.title")}>
        <table>
          <thead>
            <tr>
              {labels.map((label) => (
                <th key={label}>{label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {handoffs.map((h, idx) => (
              <tr key={h.handoff_id ?? idx}>
                <td>{h.loaded_count ?? copy(pageContract, "label.placeholder")}</td>
                <td className="muted">{h.from_location_label || (h.from_location_id ? shortId(h.from_location_id) : copy(pageContract, "label.placeholder"))}</td>
                <td className="muted">{h.to_location_label || (h.to_location_id ? shortId(h.to_location_id) : copy(pageContract, "label.placeholder"))}</td>
                <td className="muted">{fmtDateTime(h.dispatched_at) || copy(pageContract, "label.placeholder")}</td>
                <td>{h.status ? <ContractTag pageContract={pageContract} groupId="proc_transit_status" value={h.status} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
                <td>{h.discrepancy_state ? <ContractTag pageContract={pageContract} groupId="proc_discrepancy_state" value={h.discrepancy_state} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function HoldingCard({ stays, pageContract }: { stays: ProcurementHoldingStay[]; pageContract: AdminUiPageContract }) {
  if (stays.length === 0) return null;
  const labels = tableLabels(pageContract, "holding-stays");
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Warehouse className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.holding.title")}</h3>
        <Tag tone="info">{stays.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.holding.note")}</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.holding.title")}>
        <table>
          <thead>
            <tr>
              {labels.map((label) => (
                <th key={label}>{label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {stays.map((s, idx) => {
              return (
                <tr key={s.stay_id ?? idx}>
                  <td>
                    <span className="gid">{shortId(s.goat_id)}</span>
                  </td>
                  <td className="muted">{s.holding_location_id ? shortId(s.holding_location_id) : copy(pageContract, "label.placeholder")}</td>
                  <td className="muted">{fmtDate(s.started_at) || copy(pageContract, "label.placeholder")}</td>
                  <td className="muted">{s.ended_at ? fmtDate(s.ended_at) : copy(pageContract, "label.ongoing")}</td>
                  <td>
                    <WarmupTag days={s.warmup_days} purpose={s.purpose} pageContract={pageContract} />
                  </td>
                  <td>{s.warmup_state ? <ContractTag pageContract={pageContract} groupId="proc_warmup_state" value={s.warmup_state} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function HealthCard({ checks, pageContract }: { checks: ProcurementSourceHealthCheck[]; pageContract: AdminUiPageContract }) {
  if (checks.length === 0) return null;
  const labels = tableLabels(pageContract, "source-health-checks");
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <HeartPulse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.source_health.title")}</h3>
        <Tag tone="info">{checks.length}</Tag>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.source_health.title")}>
        <table>
          <thead>
            <tr>
              {labels.map((label) => (
                <th key={label}>{label}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {checks.map((c, idx) => (
              <tr key={c.health_check_id ?? idx}>
                <td>
                  <span className="gid">{shortId(c.goat_id)}</span>
                </td>
                <td>{c.health_state ? <ContractTag pageContract={pageContract} groupId="proc_health_state" value={c.health_state} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
                <td className="muted">{fmtDateTime(c.checked_at) || copy(pageContract, "label.placeholder")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

function HandoffCard({ handoffs, pageContract }: { handoffs: ProcurementPCHandoff[]; pageContract: AdminUiPageContract }) {
  if (handoffs.length === 0) return null;
  const labels = tableLabels(pageContract, "pc-handoffs");
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <PackageCheck className="ic" style={{ color: "var(--brand-d)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.pc_handoffs.title")}</h3>
        <Tag tone="ok">{handoffs.length}</Tag>
        <div className="sp" style={{ flex: 1 }} />
        <span className="muted small">{copy(pageContract, "section.pc_handoffs.note")}</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.pc_handoffs.title")}>
        <table>
          <thead>
            <tr>
              {labels.map((label) => (
                <th key={label}>{label}</th>
              ))}
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
                    <span className="gid">{copy(pageContract, "label.placeholder")}</span>
                  )}
                </td>
                <td className="muted">{h.park_location_label || (h.park_location_id ? shortId(h.park_location_id) : copy(pageContract, "label.placeholder"))}</td>
                <td className="muted">{h.shed_location_label || (h.shed_location_id ? shortId(h.shed_location_id) : copy(pageContract, "label.placeholder"))}</td>
                <td className="muted">{fmtDate(h.entry_date) || copy(pageContract, "label.placeholder")}</td>
                <td className="muted">{fmtDateTime(h.accepted_at) || copy(pageContract, "label.placeholder")}</td>
                <td>{h.event_status ? <ContractTag pageContract={pageContract} groupId="proc_handoff_status" value={h.event_status} /> : <span className="muted">{copy(pageContract, "label.placeholder")}</span>}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

export async function ProcurementLoadDetailPage({
  loadId,
  searchParams,
  pageContract,
}: {
  loadId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const [result, locations] = await Promise.all([getProcurementLoad(loadId), getProcurementLocations()]);
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const sp = searchParams ?? {};
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const returnTo = hrefWithoutAction(`/procurement/source-entry/loads/${encodeURIComponent(loadId)}`, sp);
  const backHref = hrefWithoutAction("/procurement/source-entry", sp);

  if (!result.ok) {
    return (
      <div className="screen on">
        <div className="phead">
          <div>
	            <div className="crumb">
	              <b>{copy(pageContract, "fallback.title")}</b>
	            </div>
	            <h1>{pageContract.title || copy(pageContract, "fallback.title")}</h1>
          </div>
        </div>
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
        <Link href={backHref} className="btn">
	          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "action.back_source_entry")}
        </Link>
      </div>
    );
  }

  const detail: ProcurementLoadDetail = result.data.detail;
  const { load } = detail;
  const goats = detail.goats ?? [];
  const hfEvidence = detail.hf_vaccination_evidence ?? [];
  const decisions = detail.decisions ?? [];
  const arrivalReviews = detail.arrival_reviews ?? [];
  const transitHandoffs = detail.transit_handoffs ?? [];
  const holdingStays = detail.holding_stays ?? [];
  const sourceHealthChecks = detail.source_health_checks ?? [];
  const pcHandoffs = detail.pc_handoffs ?? [];
  const timeline = detail.timeline ?? [];
  const sourceParty = load.source_party_name || shortId(load.source_party_id);
  const sourceLocation = load.source_location_name || load.source_location_code || null;
  const title = `${sourceLocation ?? copy(pageContract, "label.holding_farm")} · ${sourceParty}`;
  const loadLocations = {
    ...locations,
    origins: addLocationOption(locations.origins, sourceLocationOption(load)),
  };

  return (
    <div className="screen on">
      <div className="phead">
        <div>
	          <div className="crumb">
	            <b>{copy(pageContract, "fallback.title")}</b>
          </div>
          <h1>{title}</h1>
          <div className="sub">
            <b>{copy(pageContract, "label.load")} {shortId(load.load_id)}</b> · {optionLabel(pageContract, "source_load_status", load.status)} · {pageContract.subtitle}
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
	        <Link href={backHref} className="btn">
	          <ArrowLeft className="ic" style={{ width: 14 }} aria-hidden="true" /> {copy(pageContract, "action.back_source_entry")}
	        </Link>
      </div>

      <div className="fchipsbar" style={{ marginBottom: 16, flexWrap: "wrap" }}>
        <ContractTag pageContract={pageContract} groupId="source_load_status" value={load.status} />
        <span className="muted small">{copy(pageContract, "label.expected")} {load.expected_count}</span>
        <span className="muted small">{copy(pageContract, "label.purchase")} {fmtDate(load.purchase_date ?? undefined)}</span>
        <span className="muted small">{copy(pageContract, "label.planned_dispatch")} {fmtDate(load.planned_dispatch_at ?? undefined)}</span>
        <div className="sp" style={{ flex: 1 }} />
        <span className="sw" style={{ background: TONE_SWATCH[contractTone(pageContract, "source_load_status", load.status)], width: 10, height: 10, borderRadius: 999 }} />
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
	          <div className="note" style={{ marginBottom: 14 }}>
	            <Tag tone="ok">{copy(pageContract, "action.success_tag")}</Tag> {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
	          </div>
	        ) : (
	          <div className="alert" style={{ marginBottom: 14 }}>
	            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        )
      ) : null}

      <GoatRows goats={goats} pageContract={pageContract} />

      {/* Operator write surface — every control submits a real server action (idempotency-keyed). */}
      <section className="card" style={{ marginBottom: 16, background: "transparent", border: "none", padding: 0 }}>
        <div className="hd" style={{ paddingLeft: 0 }}>
          <h3>{copy(pageContract, "section.actions.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.actions.note")}</span>
        </div>
        <LoadWriteActions
          loadId={load.load_id}
          goats={goats}
          hfEvidence={hfEvidence}
          returnTo={returnTo}
          pageContract={pageContract}
          locations={loadLocations}
          defaultFromLocationId={load.source_location_id ?? ""}
        />
      </section>

      <DecisionCard decisions={decisions} pageContract={pageContract} />
      <ArrivalGateCard reviews={arrivalReviews} pageContract={pageContract} />
      <TransitCard handoffs={transitHandoffs} pageContract={pageContract} />
      <HoldingCard stays={holdingStays} pageContract={pageContract} />
      <HealthCard checks={sourceHealthChecks} pageContract={pageContract} />
      <HandoffCard handoffs={pcHandoffs} pageContract={pageContract} />
      <TimelineCard events={timeline} pageContract={pageContract} />
    </div>
  );
}
