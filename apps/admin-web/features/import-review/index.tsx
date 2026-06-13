import { AlertTriangle, Download, FileWarning } from "lucide-react";
import { redirect } from "next/navigation";
import { EmptyPanel, ErrorPanel, Mono, NextPageLink, PageHeader, Panel, RowsPerPageSelect, StatPill, ValueList } from "@/components/admin-primitives";
import { dateTime, dash, shortId } from "@/lib/format";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { firstAuthRequiredError, getImportRun, listImportRunRows, type ImportRowState } from "@/lib/api/server";

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
  const importRunId = one(searchParams, "import_run_id")?.trim();
  const limit = boundedInt(one(searchParams, "limit"), 50, 1, 500);
  const page = boundedInt(one(searchParams, "page"), 1, 1, 1000000);
  const processingState = normalizeRowState(one(searchParams, "processing_state"));
  const reasonCode = normalizeReason(one(searchParams, "reason_code"));

  if (!importRunId) {
    return (
      <>
        <PageHeader
          eyebrow="Import Review"
          title="RFID Import Review"
          description="Live import rows appear when an import run id is provided."
        />
        <Panel title="Select import run">
          <form className="grid gap-3 md:grid-cols-[1fr_auto]" action="/import-review">
            <label>
              <span className="text-xs uppercase text-[#93a4b8]">import_run_id</span>
              <input
                name="import_run_id"
                placeholder="00000000-0000-4000-8000-000000000000"
                className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 font-mono text-sm text-white outline-none focus:border-[#14f1d9]"
              />
            </label>
            <div className="flex items-end">
              <button className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Open</button>
            </div>
          </form>
          <div className="mt-4">
            <EmptyPanel message="Select or provide an import run id to load live review rows." />
          </div>
        </Panel>
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
    redirect("/login");
  }

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

      {!summary.ok ? (
        <ErrorPanel error={summary.error} />
      ) : (
        <div className="space-y-5">
          <Panel title="Import run summary" description="Reason buckets can overlap; counts by reason do not necessarily sum to rows needing review.">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
              <StatPill label="rows processed" value={summary.data.import_run.summary.rows_processed} />
              <StatPill label="goats created" value={summary.data.import_run.summary.goats_created} tone="good" />
              <StatPill label="needs review" value={summary.data.import_run.summary.rows_needing_review} tone="warn" />
              <StatPill label="rejected" value={summary.data.import_run.summary.rows_rejected} />
              <StatPill label="errors" value={summary.data.import_run.summary.error_count} tone={summary.data.import_run.summary.error_count > 0 ? "warn" : "neutral"} />
            </div>
            <div className="mt-4">
              <ValueList
                values={[
                  ["import run", <Mono key="run">{summary.data.import_run.import_run_id}</Mono>],
                  ["status", summary.data.import_run.status],
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
            </div>
          </Panel>

          <Panel
            title="Review rows"
            description="Rows are tenant-scoped and paginated. The page reads the selected run only."
          >
            <form className="mb-4 grid gap-3 md:grid-cols-[1.2fr_1fr_0.6fr_auto]" action="/import-review">
              <input type="hidden" name="import_run_id" value={importRunId} />
              <Select name="processing_state" label="State" defaultValue={processingState ?? ""} options={rowStates} />
              <Select name="reason_code" label="Reason" defaultValue={reasonCode ?? ""} options={reasonOptions} />
              <RowsPerPageSelect defaultValue={String(limit)} options={[25, 50, 100, 250, 500]} />
              <div className="flex items-end">
                <button className="h-9 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Apply</button>
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
                          <span className="rounded border border-[#334155] px-2 py-1">{row.row_state}</span>
                          {row.review_reasons.map((reason) => (
                            <span key={reason} className="rounded border border-[#a16207] px-2 py-1 text-[#facc15]">
                              {reason}
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
        </div>
      )}
    </>
  );
}

function tracked(value: number | null): string | number {
  return value === null ? "Not tracked" : value;
}

function normalizeRowState(value: string | undefined): ImportRowState | undefined {
  if (!value) return undefined;
  return rowStates.includes(value as ImportRowState) ? (value as ImportRowState) : undefined;
}

function normalizeReason(value: string | undefined): string | undefined {
  const trimmed = value?.trim();
  return trimmed || undefined;
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
      href={href}
      download
      className="inline-flex h-9 items-center gap-2 rounded-lg border border-[#334155] px-3 text-sm font-semibold text-[#f8fafc] hover:bg-[#22262E]"
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
