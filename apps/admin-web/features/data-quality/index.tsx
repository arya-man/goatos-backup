import Link from "next/link";
import { randomUUID } from "node:crypto";
import { ClipboardCheck, GitBranch } from "lucide-react";
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
import { dateTime, dash, shortId } from "@/lib/format";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import {
  adminListCorrectionRequests,
  firstAuthRequiredError,
  getConflictDetail,
  listCandidates,
  listConflicts,
  type ConflictState,
  type ConflictType,
  type CorrectionRequestState,
} from "@/lib/api/server";
import { createCorrectionRequestAction, rejectCandidateAction, resolveConflictAction, resolveCorrectionRequestAction } from "./actions";

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
  const state = normalizeState(one(searchParams, "state"));
  const correctionState = normalizeCorrectionState(one(searchParams, "correction_state"));
  const conflictType = normalizeConflictType(one(searchParams, "conflict_type"));
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
    redirect("/login");
  }

  return (
    <>
      <PageHeader
        eyebrow="Data Quality"
        title="Review Queues"
        description="Live conflict, match-candidate, and correction request queues. Defined Phase 1 decisions are wired; approve/create-goat semantics remain blocked until their contracts are written."
      />
      <ActionNotice status={actionStatus} message={actionMessage} />
      <div className="grid gap-5 xl:grid-cols-[1.05fr_0.95fr]">
        <Panel title="Identity Conflicts" description="Open and field-check conflicts for the selected filters.">
          <form className="mb-4 grid gap-3 sm:grid-cols-4" action="/data-quality">
            <Select name="state" label="State" defaultValue={state ?? ""} options={conflictStates} />
            <Select name="conflict_type" label="Type" defaultValue={conflictType ?? ""} options={conflictTypes} />
            <RowsPerPageSelect name="conflict_limit" defaultValue={String(conflictLimit)} />
            <div className="flex items-end">
              <button className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Apply</button>
            </div>
          </form>
          {!conflicts.ok ? (
            <ErrorPanel error={conflicts.error} />
          ) : conflicts.data.items.length === 0 ? (
            <EmptyPanel message="No conflicts returned for these filters." />
          ) : (
            <div className="space-y-3">
              {conflicts.data.items.map((conflict) => (
                <Link
                  key={conflict.conflict_id}
                  href={`/data-quality?conflict_id=${encodeURIComponent(conflict.conflict_id)}&conflict_limit=${conflictLimit}&candidate_limit=${candidateLimit}`}
                  className="block rounded-md border border-[#293241] bg-[#10141b] p-3 hover:border-[#14f1d9]/60"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="font-semibold text-white">{conflict.conflict_type}</div>
                      <div className="mt-1 text-sm text-[#93a4b8]">{conflict.identifier ? `${conflict.identifier.identifier_type} · ${conflict.identifier.scope_key}` : "No identifier"}</div>
                    </div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{conflict.state}</span>
                  </div>
                  <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-[#c7d1dc]">
                    <span>goats {conflict.goat_count}</span>
                    <span>sources {conflict.source_record_count}</span>
                    <span>{dateTime(conflict.created_at)}</span>
                  </div>
                </Link>
              ))}
              <NextPageLink
                href={hrefWithPagedCursor("/data-quality", searchParams, "conflict_cursor", conflicts.data.next_cursor, "conflict_page")}
                previousHref={hrefPreviousPagedCursor("/data-quality", searchParams, "conflict_cursor", "conflict_page")}
                currentPage={conflictPage}
                pageSize={conflictLimit}
                itemCount={conflicts.data.items.length}
              />
              <div className="text-xs text-[#93a4b8]">Trace {conflicts.data.trace_id}</div>
            </div>
          )}
        </Panel>

        <Panel title="Match Candidates" description="Actionable proposed and needs_review candidate queue.">
          {!candidates.ok ? (
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
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{candidate.state}</span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-[#c7d1dc] sm:grid-cols-2">
                    <span>proposed {shortId(candidate.proposed_goat_id)}</span>
                    <span>candidate {shortId(candidate.candidate_goat_id)}</span>
                    <span>by {candidate.created_by}</span>
                    <span>row v{candidate.row_version}</span>
                  </div>
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
                      <p className="text-xs text-[#93a4b8]">Reject records a candidate decision only; candidate approve remains contract-blocked.</p>
                      <ConfirmSubmitButton
                        message="Reject this candidate match?"
                        className="h-9 rounded-md border border-[#7f1d1d] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#1d1214]"
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
              <div className="text-xs text-[#93a4b8]">Trace {candidates.data.trace_id}</div>
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-5">
        <Panel title="Correction Requests" description="Create correction requests and resolve existing requests through the defined Phase 1 review service.">
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
              <button className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Create correction request</button>
            </div>
          </form>
          <form className="mb-4 grid gap-3 sm:grid-cols-3" action="/data-quality">
            <Select name="correction_state" label="State" defaultValue={correctionState ?? ""} options={correctionStates} />
            <RowsPerPageSelect name="correction_limit" defaultValue={String(correctionLimit)} />
            <div className="flex items-end">
              <button className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Apply</button>
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
                      <div className="font-semibold text-white">{correction.request_type}</div>
                      <div className="mt-1 text-sm text-[#93a4b8]">{correction.description}</div>
                    </div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{correction.state}</span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-[#c7d1dc] sm:grid-cols-4">
                    <span>{correction.goat_id ? `goat ${shortId(correction.goat_id)}` : "no goat"}</span>
                    <span>{correction.identifier_type ?? "no identifier"}</span>
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
                          className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]"
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
              <div className="text-xs text-[#93a4b8]">Trace {corrections.data.trace_id}</div>
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-5">
        <Panel title="Conflict Detail" description="Select a conflict to inspect goats, source records, and available decision options.">
          {!conflictId ? (
            <EmptyPanel message="No conflict selected." />
          ) : detail && !detail.ok ? (
            <ErrorPanel error={detail.error} />
          ) : detail && detail.ok ? (
            <div className="space-y-4">
              <ValueList
                values={[
                  ["conflict", <Mono key="conflict">{detail.data.conflict.conflict_id}</Mono>],
                  ["state", detail.data.conflict.state],
                  ["severity", detail.data.conflict.severity],
                  ["row version", detail.data.conflict.row_version],
                  ["decision options", detail.data.decision_options.join(", ")],
                ]}
              />
              <div className="grid gap-4 lg:grid-cols-2">
                {detail.data.goats.map((item) => (
                  <div key={item.goat.goat_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                    <div className="flex items-center gap-2 font-semibold text-white">
                      <GitBranch className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
                      {item.goat.display_id}
                    </div>
                    <div className="mt-2 text-sm text-[#93a4b8]">{dash(item.goat.location_path)}</div>
                    <div className="mt-3 flex flex-wrap gap-2 text-xs">
                      <span className="rounded border border-[#334155] px-2 py-1">row v{item.row_version}</span>
                      <span className="rounded border border-[#334155] px-2 py-1">{item.identifiers.length} identifiers</span>
                      <span className="rounded border border-[#334155] px-2 py-1">{item.evidence_refs.length} evidence</span>
                    </div>
                  </div>
                ))}
              </div>
              <div className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                <div className="flex items-center gap-2 font-semibold text-white">
                  <ClipboardCheck className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
                  Source records
                </div>
                <div className="mt-2 text-sm text-[#93a4b8]">{detail.data.source_records.length} linked source records returned.</div>
              </div>
              {detail.data.conflict.state === "open" || detail.data.conflict.state === "needs_field_check" ? (
                <div className="grid gap-4 lg:grid-cols-2">
                  <form action={resolveConflictAction} className="rounded-md border border-[#334155] bg-[#10141b] p-3">
                    <input type="hidden" name="conflict_action" value="reject" />
                    <input type="hidden" name="conflict_id" value={detail.data.conflict.conflict_id} />
                    <input type="hidden" name="row_version" value={detail.data.conflict.row_version} />
                    <input type="hidden" name="affected_goat_ids" value={detail.data.goats.map((item) => item.goat.goat_id).join(",")} />
                    <input type="hidden" name="idempotency_key" value={randomUUID()} />
                    <input type="hidden" name="return_to" value={returnTo} />
                    <h3 className="text-sm font-semibold text-white">Reject match</h3>
                    <p className="mt-1 text-xs text-[#93a4b8]">Records this conflict as not the same goat. It does not mutate goat identifiers.</p>
                    <div className="mt-3">
                      <EvidenceFields defaultType="conflict" defaultID={detail.data.conflict.conflict_id} />
                    </div>
                    <div className="mt-3">
                      <FormTextArea name="reason" label="Reason" required rows={2} />
                    </div>
                    <div className="mt-3 flex justify-end">
                      <ConfirmSubmitButton
                        message="Reject this conflict match?"
                        className="h-9 rounded-md border border-[#7f1d1d] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#1d1214]"
                      >
                        Reject match
                      </ConfirmSubmitButton>
                    </div>
                  </form>
                  {detail.data.goats.length > 1 ? (
                    <form action={resolveConflictAction} className="rounded-md border border-[#334155] bg-[#10141b] p-3">
                      <input type="hidden" name="conflict_action" value="merge" />
                      <input type="hidden" name="conflict_id" value={detail.data.conflict.conflict_id} />
                      <input type="hidden" name="row_version" value={detail.data.conflict.row_version} />
                      <input type="hidden" name="affected_goat_ids" value={detail.data.goats.map((item) => item.goat.goat_id).join(",")} />
                      <input type="hidden" name="idempotency_key" value={randomUUID()} />
                      <input type="hidden" name="return_to" value={returnTo} />
                      <h3 className="text-sm font-semibold text-white">Merge goats</h3>
                      <p className="mt-1 text-xs text-[#93a4b8]">Uses the existing merge service. The selected survivor wins; non-transferred identifiers are retired historically.</p>
                      <div className="mt-3 grid gap-3">
                        <FormSelect
                          name="survivor_goat_id"
                          label="Survivor goat"
                          options={detail.data.goats.map((item) => item.goat.goat_id)}
                          required
                          emptyLabel="Select survivor"
                        />
                        <EvidenceFields defaultType="conflict" defaultID={detail.data.conflict.conflict_id} />
                        <FormTextArea name="reason" label="Reason" required rows={2} />
                      </div>
                      <div className="mt-3 flex justify-end">
                        <ConfirmSubmitButton
                          message="Merge these goats? This is a canonical identity mutation."
                          className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]"
                        >
                          Merge
                        </ConfirmSubmitButton>
                      </div>
                    </form>
                  ) : null}
                </div>
              ) : null}
            </div>
          ) : null}
        </Panel>
      </div>
    </>
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
        className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        <option value="">Any</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  );
}

const evidenceTypes = ["source_record", "identifier", "goat", "event", "media", "decision", "import_run", "conflict", "location", "actor"];

function normalizeState(value: string | undefined): ConflictState | undefined {
  return conflictStates.includes(value as ConflictState) ? (value as ConflictState) : undefined;
}

function normalizeConflictType(value: string | undefined): ConflictType | undefined {
  return conflictTypes.includes(value as ConflictType) ? (value as ConflictType) : undefined;
}

function normalizeCorrectionState(value: string | undefined): CorrectionRequestState | undefined {
  return correctionStates.includes(value as CorrectionRequestState) ? (value as CorrectionRequestState) : undefined;
}
