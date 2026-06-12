import Link from "next/link";
import { ClipboardCheck, GitBranch } from "lucide-react";
import { EmptyPanel, ErrorPanel, Mono, NextPageLink, PageHeader, Panel, ValueList } from "@/components/admin-primitives";
import { dateTime, dash, shortId } from "@/lib/format";
import { boundedInt, hrefWithParam, one, type RouteSearchParams } from "@/lib/search-params";
import {
  adminListCorrectionRequests,
  getConflictDetail,
  listCandidates,
  listConflicts,
  type ConflictState,
  type ConflictType,
  type CorrectionRequestState,
} from "@/lib/api/server";

const conflictStates: ConflictState[] = ["open", "needs_field_check", "resolved", "rejected", "closed"];
const correctionStates: CorrectionRequestState[] = ["open", "assigned", "needs_field_check", "approved", "rejected", "closed"];
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
  const state = normalizeState(one(searchParams, "state"));
  const correctionState = normalizeCorrectionState(one(searchParams, "correction_state"));
  const conflictType = normalizeConflictType(one(searchParams, "conflict_type"));
  const conflictId = one(searchParams, "conflict_id");
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

  return (
    <>
      <PageHeader
        eyebrow="Data Quality"
        title="Review Queues"
        description="Conflict, match-candidate, and correction request queues for identity review. Actions remain disabled here."
      />
      <div className="grid gap-5 xl:grid-cols-[1.05fr_0.95fr]">
        <Panel title="Identity Conflicts" description="Open and field-check conflicts for the selected filters.">
          <form className="mb-4 grid gap-3 sm:grid-cols-4" action="/data-quality">
            <Select name="state" label="State" defaultValue={state ?? ""} options={conflictStates} />
            <Select name="conflict_type" label="Type" defaultValue={conflictType ?? ""} options={conflictTypes} />
            <Field name="conflict_limit" label="Limit" defaultValue={String(conflictLimit)} min="1" max="100" />
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
              <NextPageLink href={hrefWithParam("/data-quality", searchParams, "conflict_cursor", conflicts.data.next_cursor)} />
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
                </div>
              ))}
              <NextPageLink href={hrefWithParam("/data-quality", searchParams, "candidate_cursor", candidates.data.next_cursor)} />
              <div className="text-xs text-[#93a4b8]">Trace {candidates.data.trace_id}</div>
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-5">
        <Panel title="Correction Requests" description="Live read-only correction queue. Review actions remain a later write slice.">
          <form className="mb-4 grid gap-3 sm:grid-cols-3" action="/data-quality">
            <Select name="correction_state" label="State" defaultValue={correctionState ?? ""} options={correctionStates} />
            <Field name="correction_limit" label="Limit" defaultValue={String(correctionLimit)} min="1" max="100" />
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
                </div>
              ))}
              <NextPageLink href={hrefWithParam("/data-quality", searchParams, "correction_cursor", corrections.data.next_cursor)} />
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
            </div>
          ) : null}
        </Panel>
      </div>
    </>
  );
}

function Field({ name, label, defaultValue, min, max }: { name: string; label: string; defaultValue: string; min: string; max: string }) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <input
        name={name}
        type="number"
        min={min}
        max={max}
        defaultValue={defaultValue}
        className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      />
    </label>
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

function normalizeState(value: string | undefined): ConflictState | undefined {
  return conflictStates.includes(value as ConflictState) ? (value as ConflictState) : undefined;
}

function normalizeConflictType(value: string | undefined): ConflictType | undefined {
  return conflictTypes.includes(value as ConflictType) ? (value as ConflictType) : undefined;
}

function normalizeCorrectionState(value: string | undefined): CorrectionRequestState | undefined {
  return correctionStates.includes(value as CorrectionRequestState) ? (value as CorrectionRequestState) : undefined;
}
