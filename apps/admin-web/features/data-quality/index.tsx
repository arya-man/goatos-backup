import Link from "next/link";
import { randomUUID } from "node:crypto";
import type { ReactNode } from "react";
import { AlertTriangle, CircleHelp, ClipboardCheck, GitBranch, ShieldAlert } from "lucide-react";
import { redirect } from "next/navigation";
import {
  ActionNotice,
  EmptyPanel,
  ErrorPanel,
  FormField,
  FormSelect,
  FormTextArea,
  Mono,
  NextPageLink,
  PageHeader,
  Panel,
  RowsPerPageSelect,
  ValueList,
} from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { DialogModal } from "@/components/dialog-modal";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, shortId } from "@/lib/format";
import { formatLabel } from "@/lib/display-utils";
import { formAction } from "@/lib/routes";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import {
  adminListCorrectionRequests,
  firstAuthRequiredError,
  getConflictDetail,
  listCandidates,
  listConflicts,
  type ConflictDetailResponse,
  type ConflictState,
  type ConflictType,
  type CorrectionRequestState,
} from "@/lib/api/server";
import { approveCandidateAction, createCorrectionRequestAction, rejectCandidateAction, resolveConflictAction, resolveCorrectionRequestAction } from "./actions";
import { ConflictBulkReview, type BulkReviewRow } from "./bulk-review";
import { ReviewQueuesLayout } from "./review-queues-layout";
import { isReviewGroup, reviewGroupFilterOptions, reviewGroupLabel, type ReviewGroup } from "./review-groups";

export { DataQualityReviewGuidePage } from "./review-guide";

const conflictStates: ConflictState[] = ["open", "needs_field_check", "resolved", "rejected", "closed"];
const correctionStates: CorrectionRequestState[] = ["open", "assigned", "needs_field_check", "approved", "rejected", "closed"];
const resolveCorrectionStates: CorrectionRequestState[] = ["approved", "rejected", "needs_field_check", "closed"];
const identifierTypes = ["old_tag", "rfid", "visual_tag", "sheet_row_id", "purchase_load_id", "temp_field_id", "external_system_id"];
const correctionRequestTypes = [
  "missing_tag",
  "tag_reused",
  "rfid_conflict",
  "possible_duplicate",
  "wrong_location",
  "wrong_status",
  "field_verification_result",
  "identifier_seen_but_not_attached",
];
const conflictTypes: ConflictType[] = [
  "duplicate_active_identifier",
  "missing_required_identifier",
  "tagless_goat_review",
  "rfid_already_linked",
  "old_tag_reused",
  "possible_duplicate_goat",
  "location_mismatch",
  "status_mismatch",
];

export async function DataQualityPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const conflictLimit = boundedInt(one(searchParams, "conflict_limit"), 25, 1, 100);
  const candidateLimit = boundedInt(one(searchParams, "candidate_limit"), 25, 1, 100);
  const correctionLimit = boundedInt(one(searchParams, "correction_limit"), 25, 1, 100);
  const conflictPage = boundedInt(one(searchParams, "conflict_page"), 1, 1, 1000000);
  const candidatePage = boundedInt(one(searchParams, "candidate_page"), 1, 1, 1000000);
  const correctionPage = boundedInt(one(searchParams, "correction_page"), 1, 1, 1000000);
  const stateParam = one(searchParams, "state");
  const state = stateParam === undefined ? "open" : normalizeState(stateParam);
  const correctionState = normalizeCorrectionState(one(searchParams, "correction_state"));
  const conflictType = normalizeConflictType(one(searchParams, "conflict_type"));
  const reviewGroup = normalizeReviewGroup(one(searchParams, "review_group"));
  const conflictId = one(searchParams, "conflict_id");
  const returnTo = hrefWithoutAction("/data-quality", searchParams);
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const [conflicts, candidates, corrections, detail] = await Promise.all([
    listConflicts({
      limit: conflictLimit,
      cursor: one(searchParams, "conflict_cursor"),
      state,
      conflict_type: conflictType,
      review_group: reviewGroup,
    }),
    listCandidates({
      limit: candidateLimit,
      cursor: one(searchParams, "candidate_cursor"),
    }),
    adminListCorrectionRequests({
      limit: correctionLimit,
      cursor: one(searchParams, "correction_cursor"),
      state: correctionState,
    }),
    conflictId ? getConflictDetail(conflictId) : Promise.resolve(null),
  ]);
  const authError = firstAuthRequiredError(conflicts, candidates, corrections, detail);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  return (
    <>
      <PageHeader
        eyebrow="Data Quality"
        title="Review Queues"
        description="The cleanup desk for goats whose records don't fully agree. Conflicts = records disagree, pick what's correct. Match candidates = are these the same goat? Correction requests = manually flag something to fix. Empty queues mean no pending work."
        actions={
          <Link
            href="/data-quality/review-guide"
            className="inline-flex h-10 items-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white"
          >
            <CircleHelp className="h-4 w-4" aria-hidden="true" />
            Review guide
          </Link>
        }
      />
      <ActionNotice status={actionStatus} message={actionMessage} />
      <DialogModal open={Boolean(conflictId)} closeHref={withoutConflictSelection(returnTo)} label="Conflict workbench">
        <ConflictResolver detail={detail} conflictId={conflictId} returnTo={returnTo} closeHref={withoutConflictSelection(returnTo)} />
      </DialogModal>
      <ReviewQueuesLayout
        candidateCount={candidates.ok ? candidates.data.items.length : 0}
        defaultMatchOpen={candidates.ok && candidates.data.items.length > 0}
        conflicts={
        <Panel title="Identity Conflicts" description="Legacy-versus-passport disagreements grouped for audited review decisions.">
          <form className="mb-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-5" action={formAction("/data-quality")}>
            <Select name="state" label="State" defaultValue={state ?? ""} options={conflictStates} />
            <Select name="conflict_type" label="Type" defaultValue={conflictType ?? ""} options={conflictTypes} />
            <ReviewGroupSelect defaultValue={reviewGroup ?? ""} />
            <RowsPerPageSelect name="conflict_limit" defaultValue={String(conflictLimit)} />
            <div className="flex items-end">
              <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Apply</button>
            </div>
          </form>
          {!conflicts.ok ? (
            <ErrorPanel error={conflicts.error} />
          ) : conflicts.data.items.length === 0 ? (
            <EmptyPanel message="No conflicts returned for these filters." />
          ) : (
            <div className="space-y-3">
              <ConflictBulkReview
                key={conflicts.data.items.map((conflict) => `${conflict.conflict_id}:${conflict.row_version}`).join("|")}
                rows={conflicts.data.items.map((conflict): BulkReviewRow => ({
                  conflictId: conflict.conflict_id,
                  rowVersion: conflict.row_version,
                  conflictTypeLabel: formatLabel(conflict.conflict_type),
                  identifierLabel: conflictIdentifierText(conflict.identifier),
                  reviewGroup: conflict.review_group,
                  state: conflict.state,
                  stateLabel: formatLabel(conflict.state),
                  goatCount: conflict.goat_count,
                  createdAtLabel: dateTime(conflict.created_at),
                  detailHref: conflictDetailHref(conflict.conflict_id, searchParams, conflictLimit, candidateLimit, correctionLimit),
                }))}
                returnTo={returnTo}
              />
              <NextPageLink
                href={hrefWithPagedCursor("/data-quality", searchParams, "conflict_cursor", conflicts.data.next_cursor, "conflict_page")}
                previousHref={hrefPreviousPagedCursor("/data-quality", searchParams, "conflict_cursor", "conflict_page")}
                currentPage={conflictPage}
                pageSize={conflictLimit}
                itemCount={conflicts.data.items.length}
              />
            </div>
          )}
        </Panel>
        }
        candidatesBody={
          !candidates.ok ? (
            <ErrorPanel error={candidates.error} />
          ) : candidates.data.items.length === 0 ? (
            <EmptyPanel message="No candidates returned." />
          ) : (
            <div className="space-y-3">
              {candidates.data.items.map((candidate) => (
                <div key={candidate.candidate_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="font-semibold text-white">Score {candidate.match_score.toFixed(3)}</div>
                      <div className="mt-1 text-sm text-[#93a4b8]">{candidate.match_reasons.join(" · ") || "No reasons returned"}</div>
                    </div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{formatLabel(candidate.state)}</span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-[#c7d1dc] sm:grid-cols-2">
                    <span>proposed {shortId(candidate.proposed_goat_id)}</span>
                    <span>candidate {shortId(candidate.candidate_goat_id)}</span>
                    <span>by {candidate.created_by}</span>
                    <span>row v{candidate.row_version}</span>
                  </div>
                  {candidate.proposed_goat_id && candidate.candidate_goat_id && candidate.proposed_goat_id !== candidate.candidate_goat_id ? (
                    <form action={approveCandidateAction} className="mt-4 rounded-md border border-[#14f1d9]/40 bg-[#0f1115] p-3">
                      <input type="hidden" name="candidate_id" value={candidate.candidate_id} />
                      <input type="hidden" name="row_version" value={candidate.row_version} />
                      <input type="hidden" name="affected_goat_ids" value={`${candidate.proposed_goat_id},${candidate.candidate_goat_id}`} />
                      <input type="hidden" name="idempotency_key" value={randomUUID()} />
                      <input type="hidden" name="return_to" value={returnTo} />
                      <p className="text-sm font-semibold text-white">Confirm same goat → merge</p>
                      <p className="mt-1 text-xs text-[#93a4b8]">Keep one passport as the survivor; the other is merged into it through the audited backend decision. Reversal needs a separate reviewed correction. To instead attach an identifier to one existing goat (the approve attach_identifier outcome), use that goat&apos;s passport identifier panel.</p>
                      <label className="mt-3 block">
                        <span className="text-xs uppercase text-[#93a4b8]">Survivor passport</span>
                        <select
                          name="survivor_goat_id"
                          required
                          defaultValue=""
                          className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
                        >
                          <option value="">Select survivor</option>
                          <option value={candidate.proposed_goat_id}>proposed · {shortId(candidate.proposed_goat_id)}</option>
                          <option value={candidate.candidate_goat_id}>candidate · {shortId(candidate.candidate_goat_id)}</option>
                        </select>
                      </label>
                      <div className="mt-3">
                        <EvidenceFields defaultType="goat" defaultID={candidate.candidate_goat_id ?? candidate.proposed_goat_id ?? candidate.candidate_id} />
                      </div>
                      <div className="mt-3">
                        <FormTextArea name="reason" label="Merge reason" required placeholder="Why these records are the same goat." rows={2} />
                      </div>
                      <div className="mt-3 flex justify-end">
                        <ConfirmSubmitButton
                          message="Approve and merge these passports? This is a sensitive canonical change."
                          className="h-10 rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]"
                        >
                          Approve &amp; merge
                        </ConfirmSubmitButton>
                      </div>
                    </form>
                  ) : null}
                  <form action={rejectCandidateAction} className="mt-4 rounded-md border border-[#334155] bg-[#0f1115] p-3">
                    <input type="hidden" name="candidate_id" value={candidate.candidate_id} />
                    <input type="hidden" name="row_version" value={candidate.row_version} />
                    <input type="hidden" name="idempotency_key" value={randomUUID()} />
                    <input type="hidden" name="return_to" value={returnTo} />
                    <EvidenceFields defaultType="goat" defaultID={candidate.candidate_goat_id ?? candidate.proposed_goat_id ?? candidate.candidate_id} />
                    <div className="mt-3">
                      <FormTextArea name="reason" label="Reject reason" required placeholder="Why this candidate is not the same goat." rows={2} />
                    </div>
                    <div className="mt-3 flex items-center justify-between gap-3">
                      <p className="text-xs text-[#93a4b8]">Reject records a candidate decision only (these are different goats). Approve &amp; merge confirms they are the same goat.</p>
                      <ConfirmSubmitButton
                        message="Reject this candidate match?"
                        className="h-10 rounded-md border border-[#7f1d1d] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#1d1214]"
                      >
                        Reject
                      </ConfirmSubmitButton>
                    </div>
                  </form>
                </div>
              ))}
              <NextPageLink
                href={hrefWithPagedCursor("/data-quality", searchParams, "candidate_cursor", candidates.data.next_cursor, "candidate_page")}
                previousHref={hrefPreviousPagedCursor("/data-quality", searchParams, "candidate_cursor", "candidate_page")}
                currentPage={candidatePage}
                pageSize={candidateLimit}
                itemCount={candidates.data.items.length}
              />
            </div>
          )
        }
      />

      <div className="mt-5">
        <Panel title="Correction Requests" description="Manually flag a goat record to fix or re-check (wrong tag, wrong location, possible duplicate) when the system did not auto-catch it - then resolve it here.">
          <form action={createCorrectionRequestAction} className="mb-5 rounded-md border border-[#334155] bg-[#10141b] p-3">
            <input type="hidden" name="idempotency_key" value={randomUUID()} />
            <input type="hidden" name="return_to" value={returnTo} />
            <div className="grid gap-3 lg:grid-cols-4">
              <FormSelect name="request_type" label="Request type" options={correctionRequestTypes} required emptyLabel="Select" />
              <FormField name="goat_id" label="Goat ID" placeholder="optional goat UUID" />
              <FormSelect name="identifier_type" label="Identifier type" options={identifierTypes} emptyLabel="None" />
              <FormField name="identifier_value" label="Identifier value" placeholder="optional tag/RFID" />
            </div>
            <div className="mt-3">
              <FormTextArea name="description" label="Description" required placeholder="What needs review or correction?" rows={2} />
            </div>
            <div className="mt-3">
              <EvidenceFields defaultType="source_record" defaultID="manual-admin-review" />
            </div>
            <div className="mt-3 flex justify-end">
              <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Create correction request</button>
            </div>
          </form>
          <form className="mb-4 grid gap-3 sm:grid-cols-3" action={formAction("/data-quality")}>
            <Select name="correction_state" label="State" defaultValue={correctionState ?? ""} options={correctionStates} />
            <RowsPerPageSelect name="correction_limit" defaultValue={String(correctionLimit)} />
            <div className="flex items-end">
              <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Apply</button>
            </div>
          </form>
          {!corrections.ok ? (
            <ErrorPanel error={corrections.error} />
          ) : corrections.data.items.length === 0 ? (
            <EmptyPanel message="No correction requests returned for these filters." />
          ) : (
            <div className="space-y-3">
              {corrections.data.items.map((correction) => (
                <div key={correction.correction_request_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div>
                      <div className="font-semibold text-white">{formatLabel(correction.request_type)}</div>
                      <div className="mt-1 text-sm text-[#93a4b8]">{correction.description}</div>
                    </div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{formatLabel(correction.state)}</span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-[#c7d1dc] sm:grid-cols-4">
                    <span>{correction.goat_id ? `goat ${shortId(correction.goat_id)}` : "no goat"}</span>
                    <span>{correction.identifier_type ? formatLabel(correction.identifier_type) : "no identifier"}</span>
                    <span>evidence {correction.evidence_refs.length}</span>
                    <span>row v{correction.row_version}</span>
                  </div>
                  {["open", "assigned", "needs_field_check"].includes(correction.state) ? (
                    <form action={resolveCorrectionRequestAction} className="mt-4 rounded-md border border-[#334155] bg-[#0f1115] p-3">
                      <input type="hidden" name="correction_request_id" value={correction.correction_request_id} />
                      <input type="hidden" name="row_version" value={correction.row_version} />
                      <input type="hidden" name="idempotency_key" value={randomUUID()} />
                      <input type="hidden" name="return_to" value={returnTo} />
                      <div className="grid gap-3 lg:grid-cols-[0.7fr_1.3fr]">
                        <FormSelect name="state" label="Decision" options={resolveCorrectionStates} required emptyLabel="Select" />
                        <EvidenceFields defaultType="source_record" defaultID={correction.correction_request_id} />
                      </div>
                      <div className="mt-3">
                        <FormTextArea name="reason" label="Decision reason" required rows={2} />
                      </div>
                      <div className="mt-3 flex justify-end">
                        <ConfirmSubmitButton
                          message="Resolve this correction request?"
                          className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]"
                        >
                          Resolve
                        </ConfirmSubmitButton>
                      </div>
                    </form>
                  ) : null}
                </div>
              ))}
              <NextPageLink
                href={hrefWithPagedCursor("/data-quality", searchParams, "correction_cursor", corrections.data.next_cursor, "correction_page")}
                previousHref={hrefPreviousPagedCursor("/data-quality", searchParams, "correction_cursor", "correction_page")}
                currentPage={correctionPage}
                pageSize={correctionLimit}
                itemCount={corrections.data.items.length}
              />
            </div>
          )}
        </Panel>
      </div>

    </>
  );
}

function ConflictResolver({
  detail,
  conflictId,
  returnTo,
  closeHref,
}: {
  detail: Awaited<ReturnType<typeof getConflictDetail>> | null;
  conflictId?: string;
  returnTo: string;
  closeHref: string;
}) {
  if (!conflictId) {
    // The workbench only exists inside the popup modal; nothing to render when
    // no conflict is selected.
    return null;
  }

  if (!detail?.ok) {
    return (
      <div id="conflict-resolver">
        <Panel
          title="Conflict workbench"
          description="The selected conflict could not be loaded."
          action={
            <Link href={closeHref} className="rounded-md border border-[#334155] px-3 py-2 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white">
              Close
            </Link>
          }
        >
          <ErrorPanel
            error={
              detail?.error ?? {
                kind: "not_found",
                status: 404,
                message: "Conflict not found.",
              }
            }
          />
        </Panel>
      </div>
    );
  }

  const data = detail.data;
  const affectedIDs = data.goats.map((item) => item.goat.goat_id);
  const affectedValue = affectedIDs.join(",");
  const evidenceDefaultID = data.conflict.conflict_id;
  const decisionReturnTo = ensureHash(returnTo, "conflict-resolver");
  const identifiers = data.goats.flatMap((item) =>
    item.identifiers.map((identifier) => ({
      ...identifier,
      goat_display_id: item.goat.display_id,
      goat_id: item.goat.goat_id,
    })),
  );
  const decisionOptions = new Set(data.decision_options);
  const supportsFieldCheck = decisionOptions.has("needs_field_verification");
  const supportsDispute = decisionOptions.has("different_goats_mark_identifier_disputed");
  const supportsMerge = decisionOptions.has("same_goat_merge");
  const supportsReject = decisionOptions.has("reject_candidate");
  const supportsCreate = decisionOptions.has("create_new_goat");
  const canDispute = supportsDispute && identifiers.length > 0;
  const canMerge = supportsMerge && data.goats.length > 1;
  const isActionable = data.conflict.state === "open" || data.conflict.state === "needs_field_check";
  const canRequestFieldCheck = supportsFieldCheck && data.conflict.state !== "needs_field_check";

  return (
    <div id="conflict-resolver">
      <Panel
        title="Conflict workbench"
        description="Review the goat evidence below and record your decision. Ambiguous passport splits and new-passport creation stay blocked until their backend contract is written."
        action={
          <Link href={closeHref} className="rounded-md border border-[#334155] px-3 py-2 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white">
            Close
          </Link>
        }
      >
        <div className="mb-4">
          <LegacyEvidencePanel evidence={data.legacy_evidence} />
        </div>

        <div className="grid gap-4 xl:grid-cols-[1fr_1.05fr]">
          <div className="space-y-4">
            <div className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="text-xs font-semibold uppercase text-[#14f1d9]">{formatLabel(data.conflict.conflict_type)}</div>
                  <h3 className="mt-1 text-xl font-bold text-white">{formatLabel(data.conflict.state)}</h3>
                </div>
                <span className="rounded-md border border-[#a16207] px-3 py-2 text-sm font-semibold text-[#facc15]">{formatLabel(data.conflict.severity)}</span>
              </div>
              <ValueList
                values={[
                  ["Conflict", <Mono key="conflict">{shortId(data.conflict.conflict_id)}</Mono>],
                  ["Identifier", conflictIdentifierLabel(data.conflict.identifier)],
                  ["Goats", data.conflict.goat_count],
                  ["Sources", data.conflict.source_record_count],
                  ["Row version", data.conflict.row_version],
                  ["Created", dateTime(data.conflict.created_at)],
                  ["Allowed decisions", data.decision_options.length ? data.decision_options.map(formatLabel).join(" · ") : "None returned"],
                ]}
              />
              <p className="mt-3 text-xs text-[#93a4b8]">
                Mesha stores review decisions in its own DB and audit trail. Future source syncs preserve resolved decisions and only reopen a case when evidence changes. Legacy Sync run details show the Source & Correction Log for later source disagreements.
              </p>
            </div>

            <div className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
              <div className="flex items-center gap-2 text-sm font-bold text-white">
                <ClipboardCheck className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
                Evidence
              </div>
              {data.source_records.length === 0 ? (
                <p className="mt-3 text-sm text-[#93a4b8]">No source records returned for this conflict.</p>
              ) : (
                <div className="mt-3 space-y-2">
                  {data.source_records.map((record) => (
                    <div key={`${record.source_system}:${record.source_record_id}`} className="rounded-md border border-[#293241] bg-[#0f1115] p-3 text-sm">
                      <div className="font-semibold text-white">{record.source_system}</div>
                      <div className="mt-1 text-[#c7d1dc]">{record.source_record_id}</div>
                      <div className="mt-2 text-xs text-[#93a4b8]">evidence refs {record.evidence_refs.length}</div>
                    </div>
                  ))}
                </div>
              )}
              {data.trace_id ? <p className="mt-3 font-mono text-xs text-[#64748b]">trace {data.trace_id}</p> : null}
            </div>
          </div>

          <div className="space-y-4">
            <div className="grid gap-3">
              {data.goats.map((item) => (
                <GoatReviewCard key={item.goat.goat_id} item={item} />
              ))}
            </div>
          </div>
        </div>

        {isActionable ? (
          <div className="mt-5 grid gap-4 xl:grid-cols-2">
            {canRequestFieldCheck ? (
              <DecisionForm
                icon={<ShieldAlert className="h-5 w-5 text-[#facc15]" aria-hidden="true" />}
                title="Request field check"
                description="Use when the evidence is contradictory or a worker/admin must inspect the goat before changing the passport."
                action="field_check"
                conflictID={data.conflict.conflict_id}
                rowVersion={data.conflict.row_version}
                affectedGoatIDs={affectedValue}
                returnTo={decisionReturnTo}
                evidenceDefaultID={evidenceDefaultID}
                reasonPlaceholder="What exactly should be checked before resolving this conflict?"
                submitLabel="Request field check"
                confirmMessage="Send this conflict for field verification?"
              />
            ) : supportsFieldCheck ? (
              <DecisionUnavailable
                icon={<ShieldAlert className="h-5 w-5 text-[#facc15]" aria-hidden="true" />}
                title="Field check already requested"
                description="This conflict is already waiting on field verification. Resolve it when new evidence is available."
              />
            ) : (
              <DecisionUnavailable
                icon={<ShieldAlert className="h-5 w-5 text-[#facc15]" aria-hidden="true" />}
                title="Request field check"
                description="The backend did not advertise field-check as an allowed decision for this conflict."
              />
            )}

          {canDispute ? (
            <form action={resolveConflictAction} className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
              <ConflictHiddenFields
                action="dispute_identifier"
                conflictID={data.conflict.conflict_id}
                rowVersion={data.conflict.row_version}
                affectedGoatIDs={affectedValue}
                returnTo={decisionReturnTo}
              />
              <div className="flex items-start gap-3">
                <GitBranch className="mt-0.5 h-5 w-5 text-[#14f1d9]" aria-hidden="true" />
                <div>
                  <h3 className="text-sm font-bold text-white">Mark identifier disputed</h3>
                  <p className="mt-1 text-sm leading-6 text-[#93a4b8]">Use when RFID, old tag, or source history points to different goats. This preserves the passport and sends the identifier issue to review.</p>
                </div>
              </div>
              <div className="mt-4 space-y-2">
                {identifiers.map((identifier) => (
                  <label key={`${identifier.goat_id}:${identifier.identifier_id}`} className="flex items-start gap-3 rounded-md border border-[#293241] bg-[#0f1115] p-3 text-sm text-[#c7d1dc]">
                    <input type="checkbox" name="identifier_action_id" value={identifier.identifier_id} className="mt-1 h-4 w-4 accent-[#14f1d9]" />
                    <span>
                      <span className="block font-semibold text-white">{formatLabel(identifier.identifier_type)} · {identifier.identifier_value}</span>
                      <span className="block text-xs text-[#93a4b8]">{identifier.goat_display_id} · {identifier.scope_key || "global"} · {identifier.status}</span>
                    </span>
                  </label>
                ))}
              </div>
              <div className="mt-4">
                <EvidenceFields defaultType="conflict" defaultID={evidenceDefaultID} />
              </div>
              <div className="mt-3">
                <FormTextArea name="reason" label="Decision reason" required rows={2} placeholder="Why this identifier should be disputed instead of trusted." />
              </div>
              <div className="mt-3 flex justify-end">
                <ConfirmSubmitButton
                  message="Mark the selected identifiers disputed?"
                  className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Mark disputed
                </ConfirmSubmitButton>
              </div>
            </form>
          ) : (
            <DecisionUnavailable
              icon={<GitBranch className="h-5 w-5 text-[#14f1d9]" aria-hidden="true" />}
              title="Mark identifier disputed"
              description={supportsDispute ? "No identifiers were returned for this conflict." : "The backend did not advertise identifier dispute as an allowed decision for this conflict."}
            />
          )}

          {canMerge ? (
            <form action={resolveConflictAction} className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
              <ConflictHiddenFields
                action="merge"
                conflictID={data.conflict.conflict_id}
                rowVersion={data.conflict.row_version}
                affectedGoatIDs={affectedValue}
                returnTo={decisionReturnTo}
              />
              <div className="flex items-start gap-3">
                <GitBranch className="mt-0.5 h-5 w-5 text-[#14f1d9]" aria-hidden="true" />
                <div>
                  <h3 className="text-sm font-bold text-white">Merge duplicate passports</h3>
                  <p className="mt-1 text-sm leading-6 text-[#93a4b8]">Only use when the listed passports are the same physical goat. The survivor keeps the canonical record.</p>
                </div>
              </div>
              <div className="mt-4">
                <label>
                  <span className="text-xs uppercase text-[#93a4b8]">Survivor passport</span>
                  <select
                    name="survivor_goat_id"
                    required
                    defaultValue=""
                    className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
                  >
                    <option value="">Select survivor</option>
                    {data.goats.map((item) => (
                      <option key={item.goat.goat_id} value={item.goat.goat_id}>
                        {goatOptionLabel(item)}
                      </option>
                    ))}
                  </select>
                </label>
                <p className="mt-2 text-xs text-[#93a4b8]">The survivor keeps the passport. Other listed passports are merged through the audited backend decision; reversal requires a separate reviewed correction.</p>
              </div>
              <div className="mt-4">
                <EvidenceFields defaultType="conflict" defaultID={evidenceDefaultID} />
              </div>
              <div className="mt-3">
                <FormTextArea name="reason" label="Merge reason" required rows={2} placeholder="Why these passports are the same goat." />
              </div>
              <div className="mt-3 flex justify-end">
                <ConfirmSubmitButton
                  message="Merge these passports? This is a sensitive canonical change."
                  className="h-10 rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]"
                >
                  Merge
                </ConfirmSubmitButton>
              </div>
            </form>
          ) : (
            <DecisionUnavailable
              icon={<GitBranch className="h-5 w-5 text-[#14f1d9]" aria-hidden="true" />}
              title="Merge duplicate passports"
              description={supportsMerge ? "Merge needs at least two passports in the selected conflict." : "The backend did not advertise merge as an allowed decision for this conflict."}
            />
          )}

          {supportsReject ? (
            <DecisionForm
              icon={<AlertTriangle className="h-5 w-5 text-[#f87171]" aria-hidden="true" />}
              title="Reject this match"
              description="Use when this conflict is a false positive and should be closed without changing goats or identifiers."
              action="reject"
              conflictID={data.conflict.conflict_id}
              rowVersion={data.conflict.row_version}
              affectedGoatIDs={affectedValue}
              returnTo={decisionReturnTo}
              evidenceDefaultID={evidenceDefaultID}
              reasonPlaceholder="Why this conflict can be rejected."
              submitLabel="Reject conflict"
              confirmMessage="Reject this conflict as not actionable?"
              danger
            />
          ) : (
            <DecisionUnavailable
              icon={<AlertTriangle className="h-5 w-5 text-[#f87171]" aria-hidden="true" />}
              title="Reject this match"
              description="The backend did not advertise rejection as an allowed decision for this conflict."
            />
          )}
        </div>
        ) : (
          <div className="mt-5 rounded-lg border border-[#334155] bg-[#10141b] p-4">
            <h3 className="text-sm font-bold text-white">Conflict already {formatLabel(data.conflict.state)}</h3>
            <p className="mt-1 text-sm leading-6 text-[#93a4b8]">
              This conflict is closed to new decisions. Open a fresh conflict or correction request if the evidence changed; the audit trail keeps the recorded decision.
            </p>
          </div>
        )}

        <div className="mt-5 grid gap-3 lg:grid-cols-3">
          <BlockedDecision title="Split into multiple passports" description="Blocked until the split contract defines how many passports to create, how identifiers move, and how audit/undo works." />
          <BlockedDecision
            title="Create a new passport"
            description={
              supportsCreate
                ? "The backend advertises new-passport creation, but this UI keeps it blocked until the admin creation/split field set is approved."
                : "Blocked until the admin creation field set is approved. The backend rejects new-passport decisions for now."
            }
          />
          <BlockedDecision title="Direct field overwrite" description="Use field check or correction request. Breed, sex, lifecycle, and location disagreements should not be overwritten silently." />
        </div>
      </Panel>
    </div>
  );
}

function LegacyEvidencePanel({ evidence }: { evidence?: ConflictDetailResponse["legacy_evidence"] }) {
  if (!evidence) {
    return (
      <div className="rounded-lg border border-dashed border-[#334155] bg-[#10141b] p-4">
        <div className="text-sm font-bold text-white">Legacy evidence</div>
        <p className="mt-2 text-sm leading-6 text-[#93a4b8]">No structured legacy evidence metadata was returned for this conflict.</p>
      </div>
    );
  }
  const conflicts = evidence.conflicts ?? [];
  return (
    <div className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
      <div className="flex items-center gap-2 text-sm font-bold text-white">
        <ClipboardCheck className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
        Legacy evidence
      </div>
      <p className="mt-2 text-sm leading-6 text-[#93a4b8]">
        Use this to look up the same identifier in legacy source rows before changing the Mesha passport. Legacy self-conflicts stay review-required; they are not auto-closed.
      </p>
      <ValueList
        values={[
          ["Source", dash(evidence.source_system ? legacyEvidenceLabel(evidence.source_system) : null)],
          ["Context", dash(evidence.source_context ? legacyEvidenceLabel(evidence.source_context) : null)],
          ["Latest event", dash(evidence.latest_event_date)],
          ["Farm", dash(evidence.latest_farm_code)],
          ["Destination", dash(evidence.latest_destination)],
          ["Matched keys", evidence.matched_keys?.length ? evidence.matched_keys.map((key) => <Mono key={key}>{key}</Mono>) : dash(null)],
        ]}
      />
      {evidence.review_note ? <p className="mt-3 text-xs text-[#93a4b8]">{legacyEvidenceText(evidence.review_note)}</p> : null}
      {conflicts.length === 0 ? (
        <p className="mt-3 text-sm text-[#93a4b8]">No field-level legacy conflict rows were returned.</p>
      ) : (
        <div className="mt-3 space-y-3">
          {conflicts.map((item, index) => (
            <div key={`${item.reason ?? "legacy"}:${item.identifier_key ?? index}`} className="rounded-md border border-[#293241] bg-[#0f1115] p-3">
              <div className="text-sm font-semibold text-white">{friendlyEvidenceReason(item.reason)}</div>
              <ValueList
                values={[
                  ["Attribute", dash(item.attribute ? formatLabel(item.attribute) : null)],
                  ["Mesha value", dash(item.passport_value)],
                  ["Legacy value", dash(item.legacy_value)],
                  ["RFID lifecycle", dash(item.rfid_lifecycle ? formatLabel(item.rfid_lifecycle) : null)],
                  ["Old-tag lifecycle", dash(item.old_tag_lifecycle ? formatLabel(item.old_tag_lifecycle) : null)],
                  ["Identifier", item.identifier_key ? <Mono>{item.identifier_key}</Mono> : dash(null)],
                  ["Identifier kind", dash(item.identifier_kind ? formatLabel(item.identifier_kind) : null)],
                  ["Event date", dash(item.latest_event_date)],
                ]}
              />
              {item.recommended_action ? <p className="mt-3 text-xs leading-5 text-[#93a4b8]">{legacyEvidenceText(item.recommended_action)}</p> : null}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function DecisionUnavailable({ icon, title, description }: { icon: ReactNode; title: string; description: string }) {
  return (
    <div className="rounded-lg border border-dashed border-[#334155] bg-[#0f1115] p-4">
      <div className="flex items-start gap-3">
        {icon}
        <div>
          <h3 className="text-sm font-bold text-white">{title}</h3>
          <p className="mt-1 text-sm leading-6 text-[#93a4b8]">{description}</p>
          <p className="mt-3 text-xs font-semibold uppercase text-[#64748b]">Unavailable for this conflict</p>
        </div>
      </div>
    </div>
  );
}

function GoatReviewCard({ item }: { item: ConflictDetailResponse["goats"][number] }) {
  const goat = item.goat;
  return (
    <div className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="text-xs font-semibold uppercase text-[#14f1d9]">Passport</div>
          <Link href={`/goats/${goat.goat_id}`} className="mt-1 block text-xl font-bold text-white hover:text-[#14f1d9]">
            {goat.display_id}
          </Link>
        </div>
        <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{formatLabel(goat.identity_state)}</span>
      </div>
      <ValueList
        values={[
          ["Breed", dash(goat.breed)],
          ["Sex", dash(goat.sex ? formatLabel(goat.sex) : null)],
          ["Lifecycle", dash(goat.lifecycle_status ? formatLabel(goat.lifecycle_status) : null)],
          ["Location", dash(goat.location_path.display)],
          ["RFID", dash(goat.rfid)],
          ["Old tag", dash(goat.primary_old_tag)],
        ]}
      />
      <div className="mt-3 rounded-md border border-[#293241] bg-[#0f1115] p-3">
        <div className="text-xs font-semibold uppercase text-[#93a4b8]">Identifiers</div>
        {item.identifiers.length === 0 ? (
          <p className="mt-2 text-sm text-[#93a4b8]">No identifiers returned.</p>
        ) : (
          <div className="mt-2 grid gap-2 sm:grid-cols-2">
            {item.identifiers.map((identifier) => (
              <div key={identifier.identifier_id} className="rounded border border-[#293241] px-3 py-2 text-sm text-[#c7d1dc]">
                <div className="font-semibold text-white">{formatLabel(identifier.identifier_type)}</div>
                <div className="break-all">{identifier.identifier_value}</div>
                <div className="mt-1 text-xs text-[#93a4b8]">{identifier.scope_key || "global"} · {identifier.status}</div>
              </div>
            ))}
          </div>
        )}
      </div>
      <div className="mt-3 text-xs text-[#93a4b8]">goat {goat.goat_id} · row v{item.row_version} · evidence {item.evidence_refs.length}</div>
    </div>
  );
}

function DecisionForm({
  icon,
  title,
  description,
  action,
  conflictID,
  rowVersion,
  affectedGoatIDs,
  returnTo,
  evidenceDefaultID,
  reasonPlaceholder,
  submitLabel,
  confirmMessage,
  danger,
}: {
  icon: ReactNode;
  title: string;
  description: string;
  action: "field_check" | "reject";
  conflictID: string;
  rowVersion: number;
  affectedGoatIDs: string;
  returnTo: string;
  evidenceDefaultID: string;
  reasonPlaceholder: string;
  submitLabel: string;
  confirmMessage: string;
  danger?: boolean;
}) {
  return (
    <form action={resolveConflictAction} className="rounded-lg border border-[#334155] bg-[#10141b] p-4">
      <ConflictHiddenFields action={action} conflictID={conflictID} rowVersion={rowVersion} affectedGoatIDs={affectedGoatIDs} returnTo={returnTo} />
      <div className="flex items-start gap-3">
        {icon}
        <div>
          <h3 className="text-sm font-bold text-white">{title}</h3>
          <p className="mt-1 text-sm leading-6 text-[#93a4b8]">{description}</p>
        </div>
      </div>
      <div className="mt-4">
        <EvidenceFields defaultType="conflict" defaultID={evidenceDefaultID} />
      </div>
      <div className="mt-3">
        <FormTextArea name="reason" label="Reason" required rows={2} placeholder={reasonPlaceholder} />
      </div>
      <div className="mt-3 flex justify-end">
        <ConfirmSubmitButton
          message={confirmMessage}
          className={
            danger
              ? "h-10 rounded-md border border-[#7f1d1d] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#1d1214]"
              : "h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]"
          }
        >
          {submitLabel}
        </ConfirmSubmitButton>
      </div>
    </form>
  );
}

function ConflictHiddenFields({
  action,
  conflictID,
  rowVersion,
  affectedGoatIDs,
  returnTo,
}: {
  action: string;
  conflictID: string;
  rowVersion: number;
  affectedGoatIDs: string;
  returnTo: string;
}) {
  return (
    <>
      <input type="hidden" name="conflict_action" value={action} />
      <input type="hidden" name="conflict_id" value={conflictID} />
      <input type="hidden" name="row_version" value={rowVersion} />
      <input type="hidden" name="affected_goat_ids" value={affectedGoatIDs} />
      <input type="hidden" name="idempotency_key" value={randomUUID()} />
      <input type="hidden" name="return_to" value={returnTo} />
    </>
  );
}

function BlockedDecision({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-lg border border-dashed border-[#334155] bg-[#0f1115] p-4">
      <div className="text-sm font-bold text-white">{title}</div>
      <p className="mt-2 text-sm leading-6 text-[#93a4b8]">{description}</p>
    </div>
  );
}

function EvidenceFields({ defaultType, defaultID }: { defaultType: string; defaultID: string }) {
  const safeDefaultID = defaultID || "manual-admin-review";
  return (
    <div className="grid gap-3 sm:grid-cols-3">
      <FormSelect name="evidence_type" label="Evidence type" defaultValue={defaultType} options={evidenceTypes} required emptyLabel="Select" />
      <FormField name="evidence_id" label="Evidence ID" defaultValue={safeDefaultID} required />
      <FormField name="evidence_source_system" label="Evidence source" placeholder="optional" />
    </div>
  );
}

function Select({ name, label, defaultValue, options }: { name: string; label: string; defaultValue: string; options: string[] }) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <select
        name={name}
        defaultValue={defaultValue}
        className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        <option value="">Any</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {formatLabel(option)}
          </option>
        ))}
      </select>
    </label>
  );
}

const evidenceTypes = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"];

function ReviewGroupSelect({ defaultValue }: { defaultValue: string }) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">Review group</span>
      <select
        name="review_group"
        defaultValue={defaultValue}
        className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        <option value="">Any</option>
        {reviewGroupFilterOptions.map((group) => (
          <option key={group} value={group}>
            {reviewGroupLabel(group)}
          </option>
        ))}
      </select>
    </label>
  );
}

function normalizeState(value: string | undefined): ConflictState | undefined {
  return conflictStates.includes(value as ConflictState) ? (value as ConflictState) : undefined;
}

function normalizeConflictType(value: string | undefined): ConflictType | undefined {
  return conflictTypes.includes(value as ConflictType) ? (value as ConflictType) : undefined;
}

function normalizeReviewGroup(value: string | undefined): ReviewGroup | undefined {
  return isReviewGroup(value) ? value : undefined;
}

function normalizeCorrectionState(value: string | undefined): CorrectionRequestState | undefined {
  return correctionStates.includes(value as CorrectionRequestState) ? (value as CorrectionRequestState) : undefined;
}

function goatOptionLabel(item: ConflictDetailResponse["goats"][number]): string {
  const goat = item.goat;
  return [
    goat.display_id,
    goat.breed || "unknown breed",
    goat.sex ? formatLabel(goat.sex) : "unknown sex",
    goat.lifecycle_status ? formatLabel(goat.lifecycle_status) : "unknown status",
    goat.rfid || goat.primary_old_tag || shortId(goat.goat_id),
  ].join(" · ");
}

function friendlyEvidenceReason(reason?: string): string {
  switch (reason) {
    case "bq_gender_self_conflict":
      return "Legacy has multiple gender values";
    case "bq_breed_self_conflict":
      return "Legacy has multiple breed values";
    case "legacy_changed_after_human_review":
      return "Legacy changed after human review";
    case "gender_mismatch":
      return "Legacy gender disagrees with Mesha";
    case "breed_mismatch":
      return "Legacy breed disagrees with Mesha";
    case "identifier_lifecycle_disagreement":
      return "RFID lifecycle disagrees with old-tag lifecycle";
    case "death_then_later_activity":
      return "Legacy has activity after a death event";
    case "sale_then_later_nonpurchase_activity":
      return "Legacy has activity after a sale event";
    default:
      return reason ? formatLabel(reason) : "Legacy evidence mismatch";
  }
}

function legacyEvidenceLabel(value: string): string {
  switch (value) {
    case "legacy_bigquery":
      return "Legacy data";
    case "bq_reconcile_attribute_conflict":
      return "Legacy attribute disagreement";
    case "bq_reconcile_identifier_lifecycle_conflict":
      return "Legacy lifecycle disagreement";
    default:
      break;
  }
  return legacyEvidenceText(formatLabel(value));
}

function legacyEvidenceText(value: string): string {
  return value
    .replace(/\bBQ\b/g, "Legacy")
    .replace(/\bBq\b/g, "Legacy")
    .replace(/\bBigQuery\b/g, "legacy data")
    .replace(/\bBigquery\b/g, "legacy data")
    .replace(/\bGoat\s+OS\b/g, "Mesha");
}

function conflictIdentifierLabel(identifier: ConflictDetailResponse["conflict"]["identifier"]): ReactNode {
  if (!identifier) return "No identifier";
  return `${formatLabel(identifier.identifier_type)} · ${identifier.identifier_value} · ${identifier.scope_key || "global"}`;
}

function conflictIdentifierText(identifier: ConflictDetailResponse["conflict"]["identifier"]): string {
  if (!identifier) return "No identifier";
  return `${formatLabel(identifier.identifier_type)} · ${identifier.identifier_value} · ${identifier.scope_key || "global"}`;
}

function conflictDetailHref(
  conflictID: string,
  params: RouteSearchParams,
  conflictLimit: number,
  candidateLimit: number,
  correctionLimit: number,
): string {
  const next = new URLSearchParams();
  copyParam(params, next, "state");
  copyParam(params, next, "conflict_type");
  copyParam(params, next, "review_group");
  copyParam(params, next, "conflict_cursor");
  copyParam(params, next, "conflict_page");
  copyParam(params, next, "candidate_cursor");
  copyParam(params, next, "candidate_page");
  copyParam(params, next, "correction_state");
  copyParam(params, next, "correction_cursor");
  copyParam(params, next, "correction_page");
  next.set("conflict_limit", String(conflictLimit));
  next.set("candidate_limit", String(candidateLimit));
  next.set("correction_limit", String(correctionLimit));
  next.set("conflict_id", conflictID);
  return `/data-quality?${next.toString()}`;
}

function copyParam(source: RouteSearchParams, target: URLSearchParams, key: string) {
  const value = one(source, key);
  if (value) target.set(key, value);
}

function ensureHash(path: string, hash: string): string {
  return path.includes("#") ? path : `${path}#${hash}`;
}

function withoutConflictSelection(path: string): string {
  const [pathWithoutHash] = path.split("#", 2);
  const [pathname, query = ""] = pathWithoutHash.split("?", 2);
  const params = new URLSearchParams(query);
  params.delete("conflict_id");
  const qs = params.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}
