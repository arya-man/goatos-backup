import { ChevronDown, Flag, HeartPulse, PackageCheck, Plus, Truck } from "lucide-react";
import type { ProcurementLoadGoat } from "@/lib/api/procurement";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { isAcceptedIntake, isProcurementHistoryOnly } from "./work-state";
import {
  acceptIntakeAction,
  addSourceGoatAction,
  arrivalReviewAction,
  createLoadAction,
  dispatchLoadAction,
  preDispatchDecisionAction,
  recordSourceHealthAction,
} from "./actions";

// Operator write surface for a load. Every control submits a real server action against a generated
// backend endpoint (idempotency-keyed) — none are display-only. Media capture is not built in this slice,
// so proof is an optional ref-id field and the upload affordance is visibly disabled with a reason; proof
// is optional on these contracts, so the actions still run without it.

const SELECTION_OPTIONS = ["source_only", "candidate", "purchased"];
const HEALTH_OPTIONS = ["pending", "passed", "failed", "deferred"];
const OWNERSHIP_OPTIONS = ["pending", "shared_pending", "mesha_owned", "not_owned", "settled", "blocked"];
const DECISION_OPTIONS = ["accepted", "rejected", "deferred", "blocked"];
const HEALTH_RESULT_OPTIONS = ["passed", "failed", "deferred"];
const ARRIVAL_STATUS_OPTIONS = ["pending", "mismatch", "accepted", "rejected", "deferred", "blocked"];
const INTAKE_SIGNAL_OPTIONS = ["clear", "defer", "quarantine", "review"];

function goatLabel(goat: ProcurementLoadGoat): string {
  return goat.source_tag || goat.source_rfid || goat.temporary_id || (goat.goat_id ? goat.goat_id.slice(0, 8) : "—");
}

// A native disclosure that reads as a mock card header; no client JS needed in a server component.
function Disclosure({ icon, title, children }: { icon: React.ReactNode; title: string; children: React.ReactNode }) {
  return (
    <details className="card" style={{ marginBottom: 12 }}>
      <summary className="hd" style={{ cursor: "pointer", listStyle: "none" }}>
        {icon}
        <h3>{title}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <ChevronDown className="ic" aria-hidden="true" />
      </summary>
      <div className="bd">{children}</div>
    </details>
  );
}

function DisabledMediaProof() {
  return (
    <span
      className="btn sm"
      aria-disabled
      title="Media capture is not built in this frontend slice — enter a known proof ref id, or upload via the field app / proof API"
      style={{ opacity: 0.45, cursor: "not-allowed" }}
    >
      Upload media proof (not in this slice)
    </span>
  );
}

export function NewLoadForm({ returnTo }: { returnTo: string }) {
  return (
    <Disclosure icon={<Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />} title="New load">
      <form action={createLoadAction} style={{ maxWidth: 620 }}>
        <input type="hidden" name="return_to" value={returnTo} />
        <div className="fld">
          <label>Source party id (required)</label>
          <input name="source_party_id" required placeholder="uuid of supplier / source party" />
        </div>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label>Source / holding location id</label>
            <input name="source_location_id" placeholder="holding farm location uuid (optional)" />
          </div>
          <div className="fld" style={{ width: 140 }}>
            <label>Expected count</label>
            <input name="expected_count" type="number" min={0} placeholder="0" />
          </div>
        </div>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          <div className="fld" style={{ flex: 1, minWidth: 180 }}>
            <label>Purchase date</label>
            <input name="purchase_date" type="date" />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 180 }}>
            <label>Planned dispatch</label>
            <input name="planned_dispatch_at" type="datetime-local" />
          </div>
        </div>
        <div className="fld">
          <label>Notes</label>
          <input name="notes" placeholder="optional" />
        </div>
        <button type="submit" className="btn p">
          Create load
        </button>
      </form>
    </Disclosure>
  );
}

export function LoadWriteActions({ loadId, goats, returnTo }: { loadId: string; goats: ProcurementLoadGoat[]; returnTo: string }) {
  // Per-goat source health + pre-dispatch decision belong to goats still inside source entry — not to
  // terminal (rejected/dead/sold/lost) or already-accepted-intake goats.
  const actionableGoats = goats.filter((g) => !isProcurementHistoryOnly(g.current_state) && !isAcceptedIntake(g.current_state));

  return (
    <>
      <Disclosure icon={<Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />} title="Add source goat">
        <form action={addSourceGoatAction} style={{ maxWidth: 620 }}>
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>Source tag</label>
              <input name="source_tag" placeholder="supplier tag" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>Source RFID</label>
              <input name="source_rfid" placeholder="rfid" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>Temporary id</label>
              <input name="temporary_id" placeholder="temp id" />
            </div>
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>Selection state</label>
              <select name="selection_state" defaultValue="source_only">
                {SELECTION_OPTIONS.map((o) => (
                  <option key={o} value={o}>{o.replace(/_/g, " ")}</option>
                ))}
              </select>
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>Health state</label>
              <select name="health_state" defaultValue="pending">
                {HEALTH_OPTIONS.map((o) => (
                  <option key={o} value={o}>{o}</option>
                ))}
              </select>
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>Ownership</label>
              <select name="ownership_state" defaultValue="pending">
                {OWNERSHIP_OPTIONS.map((o) => (
                  <option key={o} value={o}>{o.replace(/_/g, " ")}</option>
                ))}
              </select>
            </div>
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ width: 140 }}>
              <label>Warmup days</label>
              <input name="warmup_days" type="number" min={0} placeholder="e.g. 52" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>Holding location id</label>
              <input name="holding_location_id" placeholder="holding location uuid (optional)" />
            </div>
          </div>
          <div className="muted small" style={{ marginBottom: 8 }}>A source goat is procurement-only identity — not clean park/herd truth until accepted intake.</div>
          <button type="submit" className="btn p">
            Add source goat
          </button>
        </form>
      </Disclosure>

      {/* Pre-dispatch section — per-goat source health + accept/reject-before-truck/defer/block. */}
      <Disclosure icon={<Flag className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />} title="Pre-dispatch decisions (per goat)">
        {actionableGoats.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            No goats currently awaiting source health or a pre-dispatch decision. Accepted-intake and terminal goats are
            not shown here.
          </p>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            {actionableGoats.map((goat) => (
              <div key={goat.load_goat_id} style={{ border: "1px solid var(--line)", borderRadius: 10, padding: 12 }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
                  <HeartPulse className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
                  <b style={{ fontSize: 13 }}>{goatLabel(goat)}</b>
                </div>
                <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
                  {/* Source health */}
                  <form action={recordSourceHealthAction} style={{ display: "flex", gap: 8, alignItems: "flex-end", flexWrap: "wrap" }}>
                    <input type="hidden" name="return_to" value={returnTo} />
                    <input type="hidden" name="load_id" value={loadId} />
                    <input type="hidden" name="goat_id" value={goat.goat_id} />
                    <div className="fld" style={{ width: 130, marginBottom: 0 }}>
                      <label>Source health</label>
                      <select name="health_state" defaultValue="passed">
                        {HEALTH_RESULT_OPTIONS.map((o) => (
                          <option key={o} value={o}>{o}</option>
                        ))}
                      </select>
                    </div>
                    <div className="fld" style={{ width: 160, marginBottom: 0 }}>
                      <label>Reason</label>
                      <input name="reason" placeholder="optional" />
                    </div>
                    <button type="submit" className="btn sm">Record health</button>
                  </form>
                  {/* Pre-dispatch decision */}
                  <form action={preDispatchDecisionAction} style={{ display: "flex", gap: 8, alignItems: "flex-end", flexWrap: "wrap" }}>
                    <input type="hidden" name="return_to" value={returnTo} />
                    <input type="hidden" name="load_id" value={loadId} />
                    <input type="hidden" name="goat_id" value={goat.goat_id} />
                    <div className="fld" style={{ width: 150, marginBottom: 0 }}>
                      <label>Pre-dispatch</label>
                      <select name="decision_type" defaultValue="accepted">
                        {DECISION_OPTIONS.map((o) => (
                          <option key={o} value={o}>{o === "accepted" ? "accept for truck" : o === "rejected" ? "reject before truck" : o}</option>
                        ))}
                      </select>
                    </div>
                    <div className="fld" style={{ width: 160, marginBottom: 0 }}>
                      <label>Reason</label>
                      <input name="reason" placeholder="optional" />
                    </div>
                    <ConfirmSubmitButton className="btn sm" message={`Record pre-dispatch decision for ${goatLabel(goat)}? Rejection before truck keeps the goat in procurement history (no PHC work).`}>
                      Record decision
                    </ConfirmSubmitButton>
                  </form>
                </div>
              </div>
            ))}
          </div>
        )}
      </Disclosure>

      {/* Dispatch / transit */}
      <Disclosure icon={<Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />} title="Record dispatch / transit">
        <form action={dispatchLoadAction} style={{ maxWidth: 620 }}>
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>To location id (required)</label>
              <input name="to_location_id" required placeholder="destination park location uuid" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>From location id</label>
              <input name="from_location_id" placeholder="source location uuid (optional)" />
            </div>
          </div>
          <div className="fld">
            <label>Goat ids (comma separated)</label>
            <input name="goat_ids" placeholder="only accepted-for-truck goats" />
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ maxWidth: 260 }}>
              <label>Dispatched at</label>
              <input name="dispatched_at" type="datetime-local" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 220 }}>
              <label>Dispatch proof ref id (required for real transit)</label>
              <input name="proof_ref_id" placeholder="proof artifact uuid" />
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <button type="submit" className="btn p">Record dispatch</button>
            <DisabledMediaProof />
          </div>
        </form>
      </Disclosure>

      {/* Arrival gate review */}
      <Disclosure icon={<Flag className="ic" style={{ color: "var(--purple)" }} aria-hidden="true" />} title="Record arrival review">
        <form action={arrivalReviewAction} style={{ maxWidth: 720 }}>
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div className="fld">
            <label>Park location id (required)</label>
            <input name="park_location_id" required placeholder="arrival park location uuid" />
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {[
              ["expected_count", "Expected"],
              ["loaded_count", "Loaded"],
              ["arrived_count", "Arrived"],
              ["matched_count", "Matched"],
              ["missing_count", "Missing"],
              ["extra_count", "Extra"],
              ["rejected_count", "Rejected"],
            ].map(([name, label]) => (
              <div className="fld" key={name} style={{ width: 96, marginBottom: 8 }}>
                <label>{label}</label>
                <input name={name} type="number" min={0} placeholder="0" />
              </div>
            ))}
          </div>
          <div className="fld" style={{ maxWidth: 220 }}>
            <label>Review status</label>
            <select name="status" defaultValue="pending">
              {ARRIVAL_STATUS_OPTIONS.map((o) => (
                <option key={o} value={o}>{o}</option>
              ))}
            </select>
          </div>
          <div className="fld">
            <label>Per-goat arrival rows (one per line: goat_id, arrival_state)</label>
            <textarea
              name="goats"
              rows={4}
              placeholder={"goat_id, accepted\ngoat_id, rejected"}
              style={{ fontFamily: "var(--mono, monospace)" }}
            />
            <span className="muted small">
              Only goats listed as <code>accepted</code> advance to arrival-accepted and become eligible for intake.
            </span>
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <button type="submit" className="btn p">Record arrival review</button>
            <DisabledMediaProof />
          </div>
        </form>
      </Disclosure>

      {/* Accept intake (load-level) */}
      <Disclosure icon={<PackageCheck className="ic" style={{ color: "var(--brand-d)" }} aria-hidden="true" />} title="Accept intake">
        <form action={acceptIntakeAction} style={{ maxWidth: 620 }}>
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div className="fld">
            <label>Goat ids (comma separated)</label>
            <input name="goat_ids" placeholder="only arrival-accepted, eligible goats" />
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>Park location id (required)</label>
              <input name="park_location_id" required placeholder="park location uuid" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>Shed location id (required)</label>
              <input name="shed_location_id" required placeholder="shed location uuid" />
            </div>
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ width: 180 }}>
              <label>Entry date</label>
              <input name="entry_date" type="date" />
            </div>
            <div className="fld" style={{ width: 180 }}>
              <label>Intake health signal</label>
              <select name="intake_health_signal" defaultValue="clear">
                {INTAKE_SIGNAL_OPTIONS.map((o) => (
                  <option key={o} value={o}>{o}</option>
                ))}
              </select>
            </div>
          </div>
          <div className="muted small" style={{ marginBottom: 8 }}>
            Accepting intake is the only handoff that makes a procured goat eligible for post-arrival PHC. It runs once
            and is idempotent.
          </div>
          <ConfirmSubmitButton className="btn p" message="Accept these goats into the herd and create PHC handoffs? Only run after arrival reconciliation.">
            Accept intake
          </ConfirmSubmitButton>
        </form>
      </Disclosure>
    </>
  );
}
