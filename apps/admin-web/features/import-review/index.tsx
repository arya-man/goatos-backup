import { randomUUID } from "node:crypto";
import { AlertTriangle, Download, FileWarning, History } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ActionNotice, EmptyPanel, ErrorPanel, FormField, FormTextArea, Mono, NextPageLink, PageHeader, Panel, RowsPerPageSelect, ValueList } from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { dateTime, dash, shortId } from "@/lib/format";
import { formatLabel } from "@/lib/display-utils";
import { formAction } from "@/lib/routes";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import { firstAuthRequiredError, getImportRun, listImportRunRows, listImportRuns, type ImportRowState, type ImportRunRowsResponse } from "@/lib/api/server";
import { reviewImportRowAction } from "./actions";

type ReviewRow = ImportRunRowsResponse["items"][number];

const rowStates: ImportRowState[] = ["pending", "auto_linked", "created_goat", "needs_review", "rejected", "error"];
const reasonOptions = [
  "blank_old_tag_suffix",
  "species_or_breed_requires_review",
  "blank_gender",
  "duplicate_old_tag_same_scope",
  "unknown_status_mapping",
  "rfid_already_linked",
] as const;

export async function ImportReviewPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const importRunId = one(searchParams, "import_run_id")?.trim() || process.env.GOATOS_IMPORT_RUN_ID?.trim();
  const limit = boundedInt(one(searchParams, "limit"), 50, 1, 500);
  const page = boundedInt(one(searchParams, "page"), 1, 1, 1000000);
  const processingState = normalizeRowState(one(searchParams, "processing_state"));
  const reasonCode = normalizeReason(one(searchParams, "reason_code"));

  if (!importRunId) {
    const runs = await listImportRuns({ limit: 10 });
    const authError = firstAuthRequiredError(runs);
    if (authError) {
      redirect(INTERNAL_LOGIN_PATH);
    }
    return (
      <>
        <PageHeader
          eyebrow="Import Review"
          title="RFID Import Review"
          description="Open a recent import run to review staged rows and messy-data reasons."
        />
        <div className="space-y-5">
          <Panel title="Recent import runs" description="Use the latest completed run; the full UUID is shown only as a secondary reference.">
            {!runs.ok ? (
              <ErrorPanel error={runs.error} />
            ) : runs.data.items.length === 0 ? (
              <EmptyPanel message="No import runs are available for this tenant yet." />
            ) : (
              <div className="grid gap-3 lg:grid-cols-2">
                {runs.data.items.map((run, index) => (
                  <a
                    key={run.import_run_id}
                    href={formAction(`/import-review?import_run_id=${encodeURIComponent(run.import_run_id)}`)}
                    className="block rounded-md border border-[#334155] bg-[#10141b] p-4 transition hover:border-[#14f1d9] hover:bg-[#151b22]"
                  >
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <div className="flex items-center gap-2 text-sm font-semibold text-white">
                          <History className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
                          {index === 0 ? "Latest run" : `Run ${index + 1}`}
                          <span className="rounded border border-[#334155] px-2 py-0.5 text-xs font-medium text-[#93a4b8]">
                            {formatLabel(run.status)}
                          </span>
                        </div>
                        <div className="mt-2 text-sm text-[#c7d1dc]">
                          {run.source_system} · {run.source_dataset}
                        </div>
                        <div className="mt-1 text-xs text-[#93a4b8]">
                          Started {dateTime(run.created_at)} · run {shortId(run.import_run_id)}
                        </div>
                      </div>
                      <span className="rounded-md bg-[#14f1d9] px-3 py-2 text-sm font-semibold text-[#081015]">Open</span>
                    </div>
                    <div className="mt-4 grid gap-2 text-sm sm:grid-cols-3">
                      <MiniStat label="goats" value={run.summary.goats_created} tone="good" />
                      <MiniStat label="review" value={run.summary.rows_needing_review} tone="warn" />
                      <MiniStat label="errors" value={run.summary.error_count} />
                    </div>
                  </a>
                ))}
              </div>
            )}
          </Panel>

          <Panel title="Open by run id" description="Use this only when you need a specific older run that is not in the recent list.">
            <form className="grid gap-3 md:grid-cols-[1fr_auto]" action={formAction("/import-review")}>
              <label>
                <span className="text-xs uppercase text-[#93a4b8]">import run id</span>
                <input
                  name="import_run_id"
                  placeholder="paste older run UUID"
                  className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 font-mono text-sm text-white outline-none focus:border-[#14f1d9]"
                />
              </label>
              <div className="flex items-end">
                <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Open</button>
              </div>
            </form>
          </Panel>
        </div>
      </>
    );
  }

  const [summary, rows] = await Promise.all([
    getImportRun(importRunId),
    listImportRunRows({
      importRunId,
      limit,
      cursor: one(searchParams, "cursor"),
      processing_state: processingState,
      reason_code: reasonCode,
    }),
  ]);
  const authError = firstAuthRequiredError(summary, rows);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }
  const returnTo = hrefWithoutAction("/import-review", searchParams);
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");

  return (
    <>
      <PageHeader
        eyebrow="Import Review"
        title="RFID Import Review"
        description="Live view of staged import rows and messy-data review reasons. CSV downloads use the same protected backend row API as this screen."
        actions={
          <div className="flex flex-wrap gap-2">
            <DownloadLink href={exportHref(importRunId, "messy")} label="Download messy CSV" />
            <DownloadLink
              href={exportHref(importRunId, "current", {
                processing_state: processingState,
                reason_code: reasonCode,
              })}
              label="Download current CSV"
            />
          </div>
        }
      />
      <ActionNotice status={actionStatus} message={actionMessage} />

      {!summary.ok ? (
        <ErrorPanel error={summary.error} />
      ) : (
        <div className="space-y-5">
          <Panel title="Import run summary" description="Reason buckets can overlap; counts by reason do not necessarily sum to rows needing review. Use a summary count as a row-table filter.">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <SummaryStatLink
                label="rows processed"
                value={summary.data.import_run.summary.rows_processed}
                href={rowsFilterHref(importRunId)}
                active={!processingState && !reasonCode}
              />
              <SummaryStatLink
                label="goats created"
                value={summary.data.import_run.summary.goats_created}
                href={rowsFilterHref(importRunId, { processing_state: "created_goat" })}
                tone="good"
                active={processingState === "created_goat"}
              />
              <SummaryStatLink
                label="needs review"
                value={summary.data.import_run.summary.rows_needing_review}
                href={rowsFilterHref(importRunId, { processing_state: "needs_review" })}
                tone="warn"
                active={processingState === "needs_review"}
              />
              <SummaryStatLink
                label="errors"
                value={summary.data.import_run.summary.error_count}
                href={rowsFilterHref(importRunId, { processing_state: "error" })}
                tone={summary.data.import_run.summary.error_count > 0 ? "warn" : "neutral"}
                active={processingState === "error"}
              />
            </div>
          </Panel>

          <Panel
            action={
              processingState || reasonCode ? (
                  <Link className="text-sm font-semibold text-[#14f1d9] hover:text-white" href={rowsFilterHref(importRunId)} scroll={false}>
                    Clear filters
                  </Link>
                ) : null
            }
            title="Review rows"
            description={rowsDescription(processingState, reasonCode)}
          >
            <form className="mb-4 grid gap-3 md:grid-cols-[1.2fr_1fr_0.6fr_auto]" action={formAction("/import-review")}>
              <input type="hidden" name="import_run_id" value={importRunId} />
              <Select name="processing_state" label="State" defaultValue={processingState ?? ""} options={rowStates} />
              <Select name="reason_code" label="Reason" defaultValue={reasonCode ?? ""} options={reasonOptions} />
              <RowsPerPageSelect defaultValue={String(limit)} options={[25, 50, 100, 250, 500]} />
              <div className="flex items-end">
                <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Apply</button>
              </div>
            </form>

            {!rows.ok ? (
              <ErrorPanel error={rows.error} />
            ) : rows.data.items.length === 0 ? (
              <EmptyPanel message="No import rows returned for these filters." />
            ) : (
              <div className="space-y-3">
                {rows.data.items.map((row) => (
                  <div key={row.import_row_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                    <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                      <div>
                        <div className="flex items-center gap-2 font-semibold text-white">
                          <FileWarning className="h-4 w-4 text-[#facc15]" aria-hidden="true" />
                          Row {row.row_number}
                        </div>
                        <div className="mt-1 flex flex-wrap gap-2 text-xs text-[#c7d1dc]">
                          <span className="rounded border border-[#334155] px-2 py-1">{formatLabel(row.row_state)}</span>
                          {row.review_reasons.map((reason) => (
                            <span key={reason} className="rounded border border-[#a16207] px-2 py-1 text-[#facc15]" title={reason}>
                              {formatLabel(reason)}
                            </span>
                          ))}
                        </div>
                      </div>
                      <div className="text-xs text-[#93a4b8]">
                        <Mono>{row.source_row_key_ref ?? row.import_row_id}</Mono>
                      </div>
                    </div>
                    <div className="mt-3 grid gap-2 text-sm sm:grid-cols-2 xl:grid-cols-4">
                      <Cell label="RFID" value={row.rfid} />
                      <Cell label="Old tag" value={row.old_tag} />
                      <Cell label="Breed" value={row.breed} />
                      <Cell label="Gender" value={row.gender} />
                      <Cell label="Farm" value={row.farm} />
                      <Cell label="Shed" value={row.shed} />
                      <Cell label="Partition" value={row.partition} />
                      <Cell label="Matched goat" value={shortId(row.matched_goat_id)} />
                    </div>
                    {row.error_reason ? (
                      <div className="mt-3 flex items-start gap-2 rounded-md border border-[#7f1d1d] bg-[#1d1214] p-2 text-sm text-[#fecaca]">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
                        {row.error_reason}
                      </div>
                    ) : null}
                    <RowReviewActions row={row} importRunId={importRunId} returnTo={returnTo} />
                  </div>
                ))}
                <NextPageLink
                  href={hrefWithCursor("/import-review", searchParams, rows.data.next_cursor)}
                  previousHref={hrefPreviousCursor("/import-review", searchParams)}
                  currentPage={page}
                  pageSize={limit}
                  itemCount={rows.data.items.length}
                />
              </div>
            )}
          </Panel>

          <Panel title="Import run details" description="Operational metadata for this import run. These values do not control the row filters above.">
            <ValueList
              values={[
                ["import run", <span key="run">Run {shortId(summary.data.import_run.import_run_id)} · <Mono>{summary.data.import_run.import_run_id}</Mono></span>],
                ["status", formatLabel(summary.data.import_run.status)],
                ["source", `${summary.data.import_run.source_system} · ${summary.data.import_run.source_dataset}`],
                ["policy", summary.data.import_run.policy_version],
                ["started", dateTime(summary.data.import_run.created_at)],
                ["completed", dateTime(summary.data.import_run.completed_at)],
                ["identifiers added", tracked(summary.data.import_run.summary.identifiers_added)],
                ["clean matches", tracked(summary.data.import_run.summary.clean_matches)],
                ["duplicates found", tracked(summary.data.import_run.summary.duplicates_found)],
                ["missing required fields", tracked(summary.data.import_run.summary.missing_required_fields)],
                ["conflicts opened", summary.data.import_run.summary.conflicts_opened],
              ]}
            />
          </Panel>
        </div>
      )}
    </>
  );
}

function RowReviewActions({ row, importRunId, returnTo }: { row: ReviewRow; importRunId: string; returnTo: string }) {
  const eligibleState = row.row_state === "needs_review" || row.row_state === "error" || row.row_state === "pending";
  const canReapply = row.row_state === "needs_review" && !row.matched_goat_id;
  const canFix = row.row_state === "needs_review";
  if (!eligibleState) return null;
  return (
    <div className="mt-3 grid gap-3 lg:grid-cols-3">
      <RowActionForm
        row={row}
        importRunId={importRunId}
        returnTo={returnTo}
        action="reject"
        title="Reject row"
        intent="Terminally reject this staged row. No goat is created or changed."
        submitLabel="Reject"
        danger
      />
      {canFix ? (
        <RowActionForm
          row={row}
          importRunId={importRunId}
          returnTo={returnTo}
          action="fix"
          title="Fix sex / breed"
          intent="Patch the staged sex and/or breed only. The row stays in review."
          submitLabel="Apply fix"
        >
          <div className="mt-2 grid grid-cols-2 gap-2">
            <FormField name="sex" label="Sex" placeholder="leave blank to keep" />
            <FormField name="breed" label="Breed" placeholder="leave blank to keep" />
          </div>
        </RowActionForm>
      ) : null}
      {canReapply ? (
        <RowActionForm
          row={row}
          importRunId={importRunId}
          returnTo={returnTo}
          action="reapply"
          title="Re-apply row"
          intent="Requeue this row to pending so the approved RFID apply path retries it. The apply path mints the goat, not this action."
          submitLabel="Re-apply"
        />
      ) : null}
    </div>
  );
}

function RowActionForm({
  row,
  importRunId,
  returnTo,
  action,
  title,
  intent,
  submitLabel,
  danger,
  children,
}: {
  row: ReviewRow;
  importRunId: string;
  returnTo: string;
  action: "reject" | "fix" | "reapply";
  title: string;
  intent: string;
  submitLabel: string;
  danger?: boolean;
  children?: React.ReactNode;
}) {
  return (
    <form action={reviewImportRowAction} className="rounded-md border border-[#334155] bg-[#0f1115] p-3">
      <input type="hidden" name="action" value={action} />
      <input type="hidden" name="import_run_id" value={importRunId} />
      <input type="hidden" name="import_row_id" value={row.import_row_id} />
      <input type="hidden" name="row_version" value={row.row_version} />
      <input type="hidden" name="idempotency_key" value={randomUUID()} />
      <input type="hidden" name="return_to" value={returnTo} />
      <input type="hidden" name="evidence_type" value="import_run" />
      <input type="hidden" name="evidence_id" value={importRunId} />
      <div className="text-sm font-semibold text-white">{title}</div>
      <p className="mt-1 text-xs text-[#93a4b8]">{intent}</p>
      {children}
      <div className="mt-2">
        <FormTextArea name="reason" label="Reason" required rows={2} placeholder="Why this action is correct." />
      </div>
      <div className="mt-2 flex justify-end">
        <ConfirmSubmitButton
          message={`Apply "${submitLabel}" to row ${row.row_number}?`}
          className={
            danger
              ? "h-9 rounded-md border border-[#7f1d1d] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#1d1214]"
              : "h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]"
          }
        >
          {submitLabel}
        </ConfirmSubmitButton>
      </div>
    </form>
  );
}

function tracked(value: number | null): string | number {
  return value === null ? "Not tracked" : value;
}

function SummaryStatLink({
  label,
  value,
  href,
  tone = "neutral",
  active = false,
}: {
  label: string;
  value: React.ReactNode;
  href: string;
  tone?: "neutral" | "good" | "warn";
  active?: boolean;
}) {
  const tones = {
    neutral: "border-[#334155] text-[#c7d1dc]",
    good: "border-[#1f8f65] text-[#7dd3a7]",
    warn: "border-[#a16207] text-[#facc15]",
  };
  const activeClass = active ? "bg-[#151d25] ring-1 ring-[#14f1d9]" : "bg-transparent hover:bg-[#151b22]";
  const actionLabel = active ? "Selected" : "Filter";
  const className = `group block rounded-md border px-3 py-2.5 transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[#14f1d9] ${tones[tone]} ${activeClass} ${
    active ? "cursor-default" : "hover:-translate-y-0.5 hover:shadow-[0_0_0_1px_rgba(20,241,217,0.2)]"
  }`;
  const content = (
    <div className="flex items-start justify-between gap-3">
      <div>
        <div className="text-xs uppercase text-[#93a4b8]">{label}</div>
        <div className="mt-1 text-lg font-semibold">{value}</div>
      </div>
      <span className="mt-1 whitespace-nowrap rounded border border-current/30 px-2 py-1 text-xs font-semibold">
        {actionLabel}
      </span>
    </div>
  );

  if (active) {
    return (
      <div className={className} aria-current="true">
        {content}
      </div>
    );
  }

  return (
    <Link
      href={href}
      scroll={false}
      className={className}
      aria-label={`Filter review rows by ${label}; ${String(value)} rows`}
    >
      {content}
    </Link>
  );
}

function MiniStat({ label, value, tone = "neutral" }: { label: string; value: number; tone?: "neutral" | "good" | "warn" }) {
  const toneClass =
    tone === "good" ? "text-[#7ddda4]" : tone === "warn" ? "text-[#facc15]" : "text-[#f8fafc]";
  return (
    <div className="rounded-md border border-[#293241] bg-[#0f1115] p-2">
      <div className="text-xs uppercase text-[#93a4b8]">{label}</div>
      <div className={`mt-1 text-lg font-semibold ${toneClass}`}>{value}</div>
    </div>
  );
}

function normalizeRowState(value: string | undefined): ImportRowState | undefined {
  if (!value) return undefined;
  return rowStates.includes(value as ImportRowState) ? (value as ImportRowState) : undefined;
}

function normalizeReason(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed || undefined;
}

function rowsFilterHref(importRunId: string, filters: { processing_state?: ImportRowState; reason_code?: string } = {}) {
  const params = new URLSearchParams({ import_run_id: importRunId });
  if (filters.processing_state) params.set("processing_state", filters.processing_state);
  if (filters.reason_code) params.set("reason_code", filters.reason_code);
  return `/import-review?${params.toString()}`;
}

function rowsDescription(processingState?: ImportRowState, reasonCode?: string): string {
  const filters = [
    processingState ? `state ${processingState}` : null,
    reasonCode ? `reason ${reasonCode}` : null,
  ].filter(Boolean);

  if (filters.length === 0) {
    return "Rows are tenant-scoped and paginated. Use the summary counts or filters to narrow this run.";
  }

  return `Showing ${filters.join(" and ")} rows for this run.`;
}

function Cell({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-md border border-[#293241] bg-[#0f1115] p-2">
      <div className="text-xs uppercase text-[#93a4b8]">{label}</div>
      <div className="mt-1 break-words text-[#f8fafc]">{dash(value)}</div>
    </div>
  );
}

function DownloadLink({ href, label }: { href: string; label: string }) {
  return (
    <a
      href={formAction(href)}
      download
      className="inline-flex h-10 items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#f8fafc] hover:bg-[#22262E]"
    >
      <Download className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
      {label}
    </a>
  );
}

function exportHref(
  importRunId: string,
  scope: "messy" | "current",
  filters: { processing_state?: string; reason_code?: string } = {},
) {
  const params = new URLSearchParams({
    import_run_id: importRunId,
    scope,
  });
  if (filters.processing_state) params.set("processing_state", filters.processing_state);
  if (filters.reason_code) params.set("reason_code", filters.reason_code);
  return `/import-review/export?${params.toString()}`;
}

function Select({ name, label, defaultValue, options }: { name: string; label: string; defaultValue: string; options: readonly string[] }) {
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
