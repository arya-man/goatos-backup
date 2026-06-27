import { randomUUID } from "node:crypto";
import { ChevronDown, Flag, HeartPulse, PackageCheck, Plus, Truck } from "lucide-react";
import type { ProcurementHFVaccinationEvidence, ProcurementLoadGoat } from "@/lib/api/procurement";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { fmtDate } from "@/lib/format";
import { copy, optionGroup, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { isAcceptedIntake, isProcurementHistoryOnly } from "./work-state";
import {
  acceptIntakeAction,
  addSourceGoatAction,
  arrivalReviewAction,
  createLoadAction,
  dispatchLoadAction,
  preDispatchDecisionAction,
  recordHFVaccinationEvidenceAction,
  recordSourceHealthAction,
  reviewHFVaccinationEvidenceAction,
} from "./actions";
import { OptionalLocationSelect, ParkLocationSelect, ParkShedLocationSelects, type ProcurementLocations } from "./location-selects";

// Operator write surface for a load. Every control submits a real server action against a generated
// backend endpoint (idempotency-keyed) — none are display-only. Media capture is not built in this slice,
// so proof is an optional ref-id field and the upload affordance is visibly disabled with a reason; proof
// is optional on these contracts, so the actions still run without it.

function goatLabel(goat: ProcurementLoadGoat): string {
  return goat.source_tag || goat.source_rfid || goat.temporary_id || (goat.goat_id ? goat.goat_id.slice(0, 8) : "—");
}

function SelectOptions({ pageContract, groupId }: { pageContract: AdminUiPageContract; groupId: string }) {
  return (
    <>
      {optionGroup(pageContract, groupId).map((option) => (
        <option key={option.key} value={option.key}>{option.label}</option>
      ))}
    </>
  );
}

// A native disclosure that reads as a mock card header; no client JS needed in a server component.
function Disclosure({
  icon,
  title,
  children,
  id,
  defaultOpen,
}: {
  icon: React.ReactNode;
  title: string;
  children: React.ReactNode;
  id?: string;
  defaultOpen?: boolean;
}) {
  return (
    <details id={id} className="card" open={defaultOpen} style={{ marginBottom: 12, scrollMarginTop: 82 }}>
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

function DisabledMediaProof({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <span
      className="btn sm"
      aria-disabled
      title={copy(pageContract, "reason.media_disabled")}
      style={{ opacity: 0.45, cursor: "not-allowed" }}
    >
      {copy(pageContract, "action.upload_media_disabled")}
    </span>
  );
}

// A stable idempotency key, minted once when the form is server-rendered and submitted as a hidden field.
// A double-submit/retry of the same rendered form replays the same key, so the backend returns the original
// result instead of writing twice (e.g. no duplicate source goat); a fresh render = a new key = a new
// logical request. The server action reads this via formIdempotencyKey rather than minting per call.
function IdempotencyKeyField() {
  return <input type="hidden" name="idempotency_key" value={randomUUID()} />;
}

export function NewLoadForm({ returnTo, pageContract }: { returnTo: string; pageContract: AdminUiPageContract }) {
  return (
    <Disclosure icon={<Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />} title={copy(pageContract, "form.new_load.title")}>
      <form action={createLoadAction} style={{ maxWidth: 620 }}>
        <IdempotencyKeyField />
        <input type="hidden" name="return_to" value={returnTo} />
        <div className="fld">
          <label>{copy(pageContract, "field.source_party_id")}</label>
          <input name="source_party_id" required placeholder={copy(pageContract, "placeholder.source_party_id")} />
        </div>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          <div className="fld" style={{ flex: 1, minWidth: 200 }}>
            <label>{copy(pageContract, "field.source_location_id")}</label>
            <input name="source_location_id" placeholder={copy(pageContract, "placeholder.source_location")} />
          </div>
          <div className="fld" style={{ width: 140 }}>
            <label>{copy(pageContract, "field.expected_count")}</label>
            <input name="expected_count" type="number" min={0} placeholder={copy(pageContract, "placeholder.zero")} />
          </div>
        </div>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
          <div className="fld" style={{ flex: 1, minWidth: 180 }}>
            <label>{copy(pageContract, "field.purchase_date")}</label>
            <input name="purchase_date" type="date" />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 180 }}>
            <label>{copy(pageContract, "field.planned_dispatch")}</label>
            <input name="planned_dispatch_at" type="datetime-local" />
          </div>
        </div>
        <div className="fld">
          <label>{copy(pageContract, "field.notes")}</label>
          <input name="notes" placeholder={copy(pageContract, "placeholder.optional")} />
        </div>
        <button type="submit" className="btn p">
          {copy(pageContract, "action.create_load")}
        </button>
      </form>
    </Disclosure>
  );
}

export function LoadWriteActions({
  loadId,
  goats,
  hfEvidence = [],
  returnTo,
  pageContract,
  locations,
  defaultFromLocationId = "",
}: {
  loadId: string;
  goats: ProcurementLoadGoat[];
  hfEvidence?: ProcurementHFVaccinationEvidence[];
  returnTo: string;
  pageContract: AdminUiPageContract;
  locations: ProcurementLocations;
  defaultFromLocationId?: string;
}) {
  // Per-goat source health + pre-dispatch decision belong to goats still inside source entry — not to
  // terminal (rejected/dead/sold/lost) or already-accepted-intake goats.
  const actionableGoats = goats.filter((g) => !isProcurementHistoryOnly(g.current_state) && !isAcceptedIntake(g.current_state));
  const locationBlockReason = !locations.available
    ? copy(pageContract, "location.locations_unavailable")
    : locations.parks.length === 0
      ? copy(pageContract, "location.no_parks")
      : "";
  const parkActionDisabled = locationBlockReason !== "";
  const intakeBlockReason = locationBlockReason || (locations.sheds.length === 0 ? copy(pageContract, "location.no_sheds") : "");
  const intakeDisabled = intakeBlockReason !== "";

  return (
    <>
      {locationBlockReason ? (
        <div className="alert warn" role="alert" style={{ marginBottom: 12 }}>
          {locationBlockReason}
        </div>
      ) : null}
      <Disclosure icon={<Plus className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />} title={copy(pageContract, "form.add_goat.title")}>
        <form action={addSourceGoatAction} style={{ maxWidth: 620 }}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.source_tag")}</label>
              <input name="source_tag" placeholder={copy(pageContract, "placeholder.source_tag")} />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.source_rfid")}</label>
              <input name="source_rfid" placeholder={copy(pageContract, "placeholder.source_rfid")} />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.temporary_id")}</label>
              <input name="temporary_id" placeholder={copy(pageContract, "placeholder.temporary_id")} />
            </div>
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.selection_state")}</label>
              <select name="selection_state" defaultValue="source_only">
                <SelectOptions pageContract={pageContract} groupId="proc_selection_state" />
              </select>
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.health_state")}</label>
              <select name="health_state" defaultValue="pending">
                <SelectOptions pageContract={pageContract} groupId="proc_health_state" />
              </select>
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.ownership")}</label>
              <select name="ownership_state" defaultValue="pending">
                <SelectOptions pageContract={pageContract} groupId="proc_ownership_state" />
              </select>
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 150 }}>
              <label>{copy(pageContract, "field.purpose")}</label>
              <select name="purpose" defaultValue="unspecified">
                <SelectOptions pageContract={pageContract} groupId="proc_purpose" />
              </select>
            </div>
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ width: 140 }}>
              <label>{copy(pageContract, "field.warmup_days")}</label>
              <input name="warmup_days" type="number" min={0} placeholder={copy(pageContract, "placeholder.warmup_days")} />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>{copy(pageContract, "field.holding_location_id")}</label>
              <input name="holding_location_id" placeholder={copy(pageContract, "placeholder.holding_location_id")} />
            </div>
          </div>
          <div className="muted small" style={{ marginBottom: 8 }}>{copy(pageContract, "note.source_goat_identity")}</div>
          <button type="submit" className="btn p">
            {copy(pageContract, "action.add_source_goat")}
          </button>
        </form>
      </Disclosure>

      {/* Holding-farm vaccination evidence — this is the mock's supplier-warmup vaccination action surface.
          It stays in Procurement Source Entry, not PHC / Vaccination. Imported+trusted evidence is later
          consumed by the accepted-intake handoff/no-double-dose path. */}
      <Disclosure
        id="hf-evidence"
        defaultOpen
        icon={<HeartPulse className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />}
        title={copy(pageContract, "form.hf_evidence.title")}
      >
        {goats.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "empty.add_goats_first")}
          </p>
        ) : (
          <div style={{ display: "grid", gap: 14 }}>
            <form action={recordHFVaccinationEvidenceAction} style={{ maxWidth: 780 }}>
              <IdempotencyKeyField />
              <input type="hidden" name="return_to" value={returnTo} />
              <input type="hidden" name="load_id" value={loadId} />
              <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
                <div className="fld" style={{ flex: 1, minWidth: 190 }}>
                  <label>{copy(pageContract, "field.goat_in_load")}</label>
                  <select name="goat_id" required aria-label={copy(pageContract, "field.goat_in_load")}>
                    {goats.map((goat) => (
                      <option key={goat.load_goat_id} value={goat.goat_id}>
                        {goatLabel(goat)} · {optionLabel(pageContract, "proc_purpose", goat.purpose)}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="fld" style={{ flex: 1, minWidth: 180 }}>
                  <label>{copy(pageContract, "field.dose_code")}</label>
                  <input name="dose_code" required placeholder={copy(pageContract, "placeholder.dose_code")} />
                </div>
                <div className="fld" style={{ flex: 1, minWidth: 190 }}>
                  <label>{copy(pageContract, "field.administered_at_hf")}</label>
                  <input name="administered_at" type="datetime-local" required aria-label={copy(pageContract, "field.administered_at_hf")} />
                </div>
              </div>
              <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
                <div className="fld" style={{ flex: 1, minWidth: 220 }}>
                  <label>{copy(pageContract, "field.protocol_version_id")}</label>
                  <input name="protocol_version_id" required placeholder={copy(pageContract, "placeholder.protocol_version_id")} />
                </div>
                <div className="fld" style={{ flex: 1, minWidth: 220 }}>
                  <label>{copy(pageContract, "field.rule_id")}</label>
                  <input name="rule_id" required placeholder={copy(pageContract, "placeholder.rule_id")} />
                </div>
              </div>
              <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
                <div className="fld" style={{ flex: 1, minWidth: 180 }}>
                  <label>{copy(pageContract, "field.vaccine_name")}</label>
                  <input name="vaccine_name" placeholder={copy(pageContract, "placeholder.optional")} />
                </div>
                <div className="fld" style={{ flex: 1, minWidth: 160 }}>
                  <label>{copy(pageContract, "field.lot_number")}</label>
                  <input name="lot_number" placeholder={copy(pageContract, "placeholder.optional")} />
                </div>
                <div className="fld" style={{ flex: 1, minWidth: 200 }}>
                  <label>{copy(pageContract, "field.proof_ref_id")}</label>
                  <input name="proof_ref_id" placeholder={copy(pageContract, "placeholder.proof_ref_id")} />
                </div>
              </div>
              <div className="fld">
                <label>{copy(pageContract, "field.source_ref")}</label>
                <input name="source_ref" placeholder={copy(pageContract, "placeholder.source_ref")} />
              </div>
              <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
                <button type="submit" className="btn p">{copy(pageContract, "action.import_hf_evidence")}</button>
                <DisabledMediaProof pageContract={pageContract} />
              </div>
            </form>

            {hfEvidence.length === 0 ? (
              <div className="note" style={{ margin: 0 }}>
                {copy(pageContract, "empty.hf_evidence")}
              </div>
            ) : (
              <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.hf_evidence.aria")}>
                <table>
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "table.hf_evidence.goat")}</th>
                      <th>{copy(pageContract, "table.hf_evidence.dose")}</th>
                      <th>{copy(pageContract, "table.hf_evidence.administered")}</th>
                      <th>{copy(pageContract, "table.hf_evidence.evidence")}</th>
                      <th>{copy(pageContract, "table.hf_evidence.review")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {hfEvidence.map((evidence) => (
                      <tr key={evidence.evidence_id}>
                        <td>
                          <span className="gid">{evidence.goat_id.slice(0, 8)}</span>
                        </td>
                        <td>
                          <b>{evidence.dose_code}</b>
                          <div className="muted small">{evidence.vaccine_name || copy(pageContract, "label.vaccine_name_not_set")}</div>
                        </td>
                        <td className="muted">{fmtDate(evidence.administered_at)}</td>
                        <td className="muted small">
                          {evidence.proof_ref_id ? `${copy(pageContract, "label.proof")} ${evidence.proof_ref_id.slice(0, 8)}` : copy(pageContract, "label.proof_ref_not_set")}
                        </td>
                        <td>
                          {evidence.review_status === "trusted" ? (
                            <span className="muted small">{copy(pageContract, "label.trusted_locked")}</span>
                          ) : (
                            <form action={reviewHFVaccinationEvidenceAction} style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
                              <IdempotencyKeyField />
                              <input type="hidden" name="return_to" value={returnTo} />
                              <input type="hidden" name="load_id" value={loadId} />
                              <input type="hidden" name="evidence_id" value={evidence.evidence_id} />
                              <input type="hidden" name="expected_row_version" value={evidence.row_version} />
                              <select name="review_status" defaultValue="trusted" className="tsize" aria-label={copy(pageContract, "field.review_status")}>
                                <SelectOptions pageContract={pageContract} groupId="proc_hf_review_status" />
                              </select>
                              <input name="review_reason" placeholder={copy(pageContract, "placeholder.reason")} style={{ maxWidth: 170 }} />
                              <button type="submit" className="btn sm">{copy(pageContract, "action.review")}</button>
                            </form>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}
      </Disclosure>

      {/* Pre-dispatch section — per-goat source health + accept/reject-before-truck/defer/block. */}
      <Disclosure icon={<Flag className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />} title={copy(pageContract, "form.pre_dispatch.title")}>
        {actionableGoats.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "empty.pre_dispatch")}
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
                    <IdempotencyKeyField />
                    <input type="hidden" name="return_to" value={returnTo} />
                    <input type="hidden" name="load_id" value={loadId} />
                    <input type="hidden" name="goat_id" value={goat.goat_id} />
                    <div className="fld" style={{ width: 130, marginBottom: 0 }}>
                      <label>{copy(pageContract, "field.source_health")}</label>
                      <select name="health_state" defaultValue="passed">
                        <SelectOptions pageContract={pageContract} groupId="proc_health_state" />
                      </select>
                    </div>
                    <div className="fld" style={{ width: 160, marginBottom: 0 }}>
                      <label>{copy(pageContract, "field.reason")}</label>
                      <input name="reason" placeholder={copy(pageContract, "placeholder.optional")} />
                    </div>
                    <button type="submit" className="btn sm">{copy(pageContract, "action.record_health")}</button>
                  </form>
                  {/* Pre-dispatch decision */}
                  <form action={preDispatchDecisionAction} style={{ display: "flex", gap: 8, alignItems: "flex-end", flexWrap: "wrap" }}>
                    <IdempotencyKeyField />
                    <input type="hidden" name="return_to" value={returnTo} />
                    <input type="hidden" name="load_id" value={loadId} />
                    <input type="hidden" name="goat_id" value={goat.goat_id} />
                    <div className="fld" style={{ width: 150, marginBottom: 0 }}>
                      <label>{copy(pageContract, "field.pre_dispatch")}</label>
                      <select name="decision_type" defaultValue="accepted">
                        <SelectOptions pageContract={pageContract} groupId="proc_decision_type" />
                      </select>
                    </div>
                    <div className="fld" style={{ width: 160, marginBottom: 0 }}>
                      <label>{copy(pageContract, "field.reason")}</label>
                      <input name="reason" placeholder={copy(pageContract, "placeholder.optional")} />
                    </div>
                    <ConfirmSubmitButton className="btn sm" message={`${copy(pageContract, "confirm.pre_dispatch.prefix")} ${goatLabel(goat)}? ${copy(pageContract, "confirm.pre_dispatch.suffix")}`}>
                      {copy(pageContract, "action.record_decision")}
                    </ConfirmSubmitButton>
                  </form>
                </div>
              </div>
            ))}
          </div>
        )}
      </Disclosure>

      {/* Dispatch / transit */}
      <Disclosure icon={<Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />} title={copy(pageContract, "form.dispatch.title")}>
        <form action={dispatchLoadAction} style={{ maxWidth: 620 }}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>{copy(pageContract, "field.to_location_id")}</label>
              <ParkLocationSelect name="to_location_id" parks={locations.parks} pageContract={pageContract} />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 180 }}>
              <label>{copy(pageContract, "field.from_location_id")}</label>
              <OptionalLocationSelect name="from_location_id" locations={locations.origins} pageContract={pageContract} defaultValue={defaultFromLocationId} />
            </div>
          </div>
          <div className="fld">
            <label>{copy(pageContract, "field.goat_ids")}</label>
            <input name="goat_ids" placeholder={copy(pageContract, "placeholder.goat_ids_dispatch")} />
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ maxWidth: 260 }}>
              <label>{copy(pageContract, "field.dispatched_at")}</label>
              <input name="dispatched_at" type="datetime-local" />
            </div>
            <div className="fld" style={{ flex: 1, minWidth: 220 }}>
              <label>{copy(pageContract, "field.dispatch_proof_ref_id")}</label>
              <input name="proof_ref_id" placeholder={copy(pageContract, "placeholder.dispatch_proof_ref_id")} />
            </div>
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <button type="submit" className="btn p" disabled={parkActionDisabled} title={locationBlockReason || undefined}>{copy(pageContract, "action.record_dispatch")}</button>
            <DisabledMediaProof pageContract={pageContract} />
          </div>
        </form>
      </Disclosure>

      {/* Arrival gate review */}
      <Disclosure icon={<Flag className="ic" style={{ color: "var(--purple)" }} aria-hidden="true" />} title={copy(pageContract, "form.arrival_review.title")}>
        <form action={arrivalReviewAction} style={{ maxWidth: 720 }}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div className="fld">
            <label>{copy(pageContract, "field.park_location_id")}</label>
            <ParkLocationSelect name="park_location_id" parks={locations.parks} pageContract={pageContract} />
          </div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {optionGroup(pageContract, "proc_arrival_counts").map((count) => (
              <div className="fld" key={count.key} style={{ width: 96, marginBottom: 8 }}>
                <label>{count.label}</label>
                <input name={count.key} type="number" min={0} placeholder={copy(pageContract, "placeholder.zero")} />
              </div>
            ))}
          </div>
          <div className="fld" style={{ maxWidth: 220 }}>
            <label>{copy(pageContract, "field.review_status")}</label>
            <select name="status" defaultValue="pending">
              <SelectOptions pageContract={pageContract} groupId="proc_arrival_status" />
            </select>
          </div>
          <div className="fld">
            <label>{copy(pageContract, "field.arrival_rows")}</label>
            <textarea
              name="goats"
              rows={4}
              placeholder={copy(pageContract, "placeholder.arrival_rows")}
              style={{ fontFamily: "var(--mono, monospace)" }}
            />
            <span className="muted small">
              {copy(pageContract, "note.arrival_rows")}
            </span>
          </div>
          <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
            <button type="submit" className="btn p" disabled={parkActionDisabled} title={locationBlockReason || undefined}>{copy(pageContract, "action.record_arrival_review")}</button>
            <DisabledMediaProof pageContract={pageContract} />
          </div>
        </form>
      </Disclosure>

      {/* Accept intake (load-level) */}
      <Disclosure icon={<PackageCheck className="ic" style={{ color: "var(--brand-d)" }} aria-hidden="true" />} title={copy(pageContract, "form.accept_intake.title")}>
        <form action={acceptIntakeAction} style={{ maxWidth: 620 }}>
          <IdempotencyKeyField />
          <input type="hidden" name="return_to" value={returnTo} />
          <input type="hidden" name="load_id" value={loadId} />
          <div className="fld">
            <label>{copy(pageContract, "field.goat_ids")}</label>
            <input name="goat_ids" placeholder={copy(pageContract, "placeholder.goat_ids_intake")} />
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <ParkShedLocationSelects parks={locations.parks} sheds={locations.sheds} pageContract={pageContract} />
          </div>
          <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
            <div className="fld" style={{ width: 180 }}>
              <label>{copy(pageContract, "field.entry_date")}</label>
              <input name="entry_date" type="date" />
            </div>
            <div className="fld" style={{ width: 180 }}>
              <label>{copy(pageContract, "field.intake_health_signal")}</label>
              <select name="intake_health_signal" defaultValue="clear">
                <SelectOptions pageContract={pageContract} groupId="proc_intake_signal" />
              </select>
            </div>
          </div>
          <div className="muted small" style={{ marginBottom: 8 }}>
            {copy(pageContract, "label.phc_handoff_note")}
          </div>
          <ConfirmSubmitButton className="btn p" message={copy(pageContract, "confirm.accept_intake")} disabled={intakeDisabled} title={intakeBlockReason || undefined}>
            {copy(pageContract, "action.accept_intake")}
          </ConfirmSubmitButton>
        </form>
      </Disclosure>
    </>
  );
}
